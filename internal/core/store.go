// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLiteSchema 是端侧标准持久化 schema：文档表 + 特征词项倒排表（仅哈希/计数/{b,f,roleExt} 声明字段与紧凑位置流，
// 无能量列）。rm_terms_unit_idx 支撑按 UnitID 整行替换的删除路径，不参与任何能量计算。
// 文档表同时承载可选单元元信息（说话人/三时态/同源文档块/行区间/记忆层）。
const SQLiteSchema = `
CREATE TABLE IF NOT EXISTS rm_docs (
    unit_id     TEXT PRIMARY KEY,
    occurred_at INTEGER NOT NULL DEFAULT 0,
    narrated_at INTEGER NOT NULL DEFAULT 0,
    ingested_at INTEGER NOT NULL DEFAULT 0,
    author_hash INTEGER NOT NULL DEFAULT 0,
    doc_hash    INTEGER NOT NULL DEFAULT 0,
    block_hash  INTEGER NOT NULL DEFAULT 0,
    line_start  INTEGER NOT NULL DEFAULT 0,
    line_end    INTEGER NOT NULL DEFAULT 0,
    layer       TEXT NOT NULL DEFAULT '',
    term_count  INTEGER NOT NULL DEFAULT 0,
    raw_text    TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS rm_terms (
    term_hash   INTEGER NOT NULL,
    unit_id     TEXT NOT NULL,
    bits_x10    INTEGER NOT NULL,
    class_flags INTEGER NOT NULL DEFAULT 0,
    group_hash  INTEGER NOT NULL DEFAULT 0,
    occurrences INTEGER NOT NULL DEFAULT 1,
    pos_blob    BLOB,
    PRIMARY KEY (term_hash, unit_id)
);
CREATE INDEX IF NOT EXISTS rm_terms_unit_idx ON rm_terms(unit_id);
CREATE TABLE IF NOT EXISTS rm_citations (
    unit_id  TEXT NOT NULL,
    cited_id TEXT NOT NULL,
    ord      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (unit_id, ord)
);
`

// migrateLegacyCitationsPrimaryKey rebuilds the pre-(unit_id, ord) citations
// table in place. The legacy (unit_id, cited_id) key collapsed duplicate, self
// and empty citations at write time, so persisted snapshots diverged from
// BuildIndex metadata; every surviving row is carried over as-is.
func migrateLegacyCitationsPrimaryKey(db *sql.DB) error {
	var ddl sql.NullString
	if err := db.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'rm_citations'`).Scan(&ddl); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if !strings.Contains(ddl.String, "PRIMARY KEY (unit_id, cited_id)") {
		return nil
	}
	for _, stmt := range []string{
		`CREATE TABLE rm_citations_migrate (unit_id TEXT NOT NULL, cited_id TEXT NOT NULL, ord INTEGER NOT NULL DEFAULT 0, PRIMARY KEY (unit_id, ord))`,
		`INSERT INTO rm_citations_migrate(unit_id, cited_id, ord) SELECT unit_id, cited_id, ord FROM rm_citations`,
		`DROP TABLE rm_citations`,
		`ALTER TABLE rm_citations_migrate RENAME TO rm_citations`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("rm/store: migrate legacy rm_citations key: %w", err)
		}
	}
	return nil
}

// EncodePositionsBlob encodes sentence and slot positions for storage.
func EncodePositionsBlob(posList []TermPosition) []byte {
	if len(posList) == 0 {
		return nil
	}
	buf := make([]byte, 0, len(posList)*4)
	var scratch [binary.MaxVarintLen64]byte
	for _, pos := range posList {
		sent := pos.Sentence
		if sent < 0 {
			sent = 0
		}
		slot := pos.Slot
		if slot < 0 {
			slot = 0
		}
		n := binary.PutVarint(scratch[:], int64(sent))
		buf = append(buf, scratch[:n]...)
		n = binary.PutVarint(scratch[:], int64(slot))
		buf = append(buf, scratch[:n]...)
	}
	return buf
}

// DecodePositionsIntoArena decodes positions into the supplied reusable arena.
func DecodePositionsIntoArena(blob []byte, arena []TermPosition) ([]TermPosition, []TermPosition, error) {
	if len(blob) == 0 {
		return nil, arena, nil
	}
	start := len(arena)
	for len(blob) > 0 {
		sent, n1 := binary.Varint(blob)
		if n1 <= 0 {
			return nil, arena[:start], fmt.Errorf("rm/store: corrupt sentence varint in pos_blob")
		}
		blob = blob[n1:]
		slot, n2 := binary.Varint(blob)
		if n2 <= 0 {
			return nil, arena[:start], fmt.Errorf("rm/store: corrupt slot varint in pos_blob")
		}
		blob = blob[n2:]
		arena = append(arena, TermPosition{Sentence: int(sent), Slot: int(slot)})
	}
	end := len(arena)
	if end == start {
		return nil, arena, nil
	}
	return arena[start:end:end], arena, nil
}

// StoreBackend is the persistence contract used by a wrapped store.
// Close releases the backend according to its own connection ownership policy.
type StoreBackend interface {
	Dialect() StoreDialect
	TenantScope() (string, string)
	DeleteDocs(unitIDs ...string) error
	Ingest(docs []Doc) error
	DocCount() (int, error)
	LoadIndex() (*Index, error)
	LoadRawText(unitID string) (string, error)
	Close() error
}

// StoreDialect identifies the local store's database dialect.
type StoreDialect string

const DialectSQLite StoreDialect = "sqlite"

// StoreOptions configures a SQLite store around an existing connection pool.
type StoreOptions struct {
	Dialect    StoreDialect
	OwnsDB     bool
	SkipSchema bool
}

// Store 是本地 SQLite 明文存储句柄，也可包装实现存储契约的其他后端。
type Store struct {
	db      *sql.DB
	ownsDB  bool
	backend StoreBackend
}

// OpenStoreWithOptions wraps an existing SQLite connection pool.
func OpenStoreWithOptions(db *sql.DB, opts StoreOptions) (*Store, error) {
	if db == nil {
		return nil, errors.New("rm/store: nil *sql.DB")
	}
	if opts.Dialect != "" && opts.Dialect != DialectSQLite {
		return nil, fmt.Errorf("rm/store: unsupported dialect %q", opts.Dialect)
	}
	if !opts.SkipSchema {
		if _, err := db.Exec(SQLiteSchema); err != nil {
			return nil, fmt.Errorf("rm/store: apply %s schema: %w", DialectSQLite, err)
		}
		if err := migrateLegacyCitationsPrimaryKey(db); err != nil {
			return nil, err
		}
	}
	return &Store{db: db, ownsDB: opts.OwnsDB}, nil
}

// WrapStoreBackend adapts a backend to the Store API without taking ownership
// of any connection other than through the backend's Close method.
func WrapStoreBackend(backend StoreBackend) (*Store, error) {
	if backend == nil {
		return nil, errors.New("rm/store: nil backend")
	}
	value := reflect.ValueOf(backend)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		if value.IsNil() {
			return nil, errors.New("rm/store: nil backend")
		}
	}
	return &Store{backend: backend}, nil
}

// Dialect returns the backend's dialect, or the SQLite dialect for a local store.
func (s *Store) Dialect() StoreDialect {
	if s != nil && s.backend != nil {
		return s.backend.Dialect()
	}
	return DialectSQLite
}

// TenantScope returns the backend's partition coordinate, or the local default.
func (s *Store) TenantScope() (string, string) {
	if s == nil {
		return "", ""
	}
	if s.backend != nil {
		return s.backend.TenantScope()
	}
	return "local", "default"
}

// OpenStore 打开（必要时创建）单用户 SQLite 库并建表（开启 WAL 模式保障读写并发快照一致性）。
func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	_, _ = db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`)
	if _, err := db.Exec(SQLiteSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := migrateLegacyCitationsPrimaryKey(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{
		db:     db,
		ownsDB: true,
	}, nil
}

// LoadRawText 读取本地 SQLite 存储中指定记忆单元的原始明文。
func (s *Store) LoadRawText(unitID string) (string, error) {
	if s == nil || unitID == "" {
		return "", nil
	}
	if s.backend != nil {
		return s.backend.LoadRawText(unitID)
	}
	if s.db == nil {
		return "", nil
	}
	var raw string
	err := s.db.QueryRow(`SELECT raw_text FROM rm_docs WHERE unit_id = ?`, unitID).Scan(&raw)
	return raw, err
}

// DeleteDocs 在单个事务内按 unitIDs 从存储中精确删除指定记忆单元及其词项倒排行。
func (s *Store) DeleteDocs(unitIDs ...string) error {
	if s == nil || len(unitIDs) == 0 {
		return nil
	}
	if s.backend != nil {
		return s.backend.DeleteDocs(unitIDs...)
	}
	if s.db == nil {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	delTerms, err := tx.Prepare(`DELETE FROM rm_terms WHERE unit_id = ?`)
	if err != nil {
		return err
	}
	defer delTerms.Close()
	delDocs, err := tx.Prepare(`DELETE FROM rm_docs WHERE unit_id = ?`)
	if err != nil {
		return err
	}
	defer delDocs.Close()
	delCites, err := tx.Prepare(`DELETE FROM rm_citations WHERE unit_id = ?`)
	if err != nil {
		return err
	}
	defer delCites.Close()
	for _, uid := range unitIDs {
		if uid == "" {
			continue
		}
		if _, err := delTerms.Exec(uid); err != nil {
			return err
		}
		if _, err := delCites.Exec(uid); err != nil {
			return err
		}
		if _, err := delDocs.Exec(uid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Close 关闭存储（若为外部传入的共享连接池且 ownsDB==false，则不关闭底层连接池）。
func (s *Store) Close() error {
	if s == nil {
		return nil
	}
	if s.backend != nil {
		return s.backend.Close()
	}
	if s.db == nil || !s.ownsDB {
		return nil
	}
	return s.db.Close()
}

// PackTermPersistFlags packs the persistable term columns into the class_flags
// integer: bits 0-7 ClassFlags, bits 8-15 roleExt (Role + low 3 ExtFlags bits),
// bits 16-31 the full 16-bit ExtFlags. Rows written before the high-ext
// persistence keep those bits at zero, so the layout stays backward compatible
// without a schema change.
func PackTermPersistFlags(flags uint8, role TermRole, extFlags uint16) int64 {
	return int64(flags) | (int64(PackTermRoleExt(role, extFlags)) << 8) | (int64(extFlags) << 16)
}

// TermPersistExtFlags returns the full 16-bit ExtFlags carried in the persisted
// class_flags value (bits 16-31).
func TermPersistExtFlags(packed int64) uint16 { return uint16(packed >> 16) }

// Ingest 在单个事务内批量写入文档与原子倒排行。
// 对同一 UnitID 的重复入库按最后一份文档整体替换（与 BuildIndex 的「最后一份为准」
// 语义一致）：先清该单元的旧原子行，再写新行，避免旧哈希残留污染 df 与倒排。
func (s *Store) Ingest(docs []Doc) error {
	if len(docs) == 0 {
		return nil
	}
	if s.backend != nil {
		return s.backend.Ingest(docs)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.ingestTx(tx, docs); err != nil {
		return err
	}
	return tx.Commit()
}

// IngestTx writes the same batch through a caller-owned transaction, so the
// ingest and any accompanying bookkeeping succeed or roll back as one durable
// unit. It is SQLite-only: a wrapped backend owns its own transaction and has
// none to join.
func (s *Store) IngestTx(tx *sql.Tx, docs []Doc) error {
	if s == nil || s.backend != nil {
		return errors.New("rm/store: caller-owned ingest requires a local SQLite store")
	}
	if tx == nil {
		return errors.New("rm/store: nil ingest transaction")
	}
	if len(docs) == 0 {
		return nil
	}
	return s.ingestTx(tx, docs)
}

// ingestTx applies the batch on an open transaction without committing it.
func (s *Store) ingestTx(tx *sql.Tx, docs []Doc) error {
	docStmt, err := tx.Prepare(`INSERT OR REPLACE INTO rm_docs(
        unit_id, occurred_at, narrated_at, ingested_at, author_hash, doc_hash, block_hash, line_start, line_end, layer, term_count, raw_text
    ) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer docStmt.Close()
	deleteTermsStmt, err := tx.Prepare(`DELETE FROM rm_terms WHERE unit_id = ?`)
	if err != nil {
		return err
	}
	defer deleteTermsStmt.Close()
	termStmt, err := tx.Prepare(`INSERT OR REPLACE INTO rm_terms(term_hash, unit_id, bits_x10, class_flags, group_hash, occurrences, pos_blob) VALUES(?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer termStmt.Close()
	deleteCitesStmt, err := tx.Prepare(`DELETE FROM rm_citations WHERE unit_id = ?`)
	if err != nil {
		return err
	}
	defer deleteCitesStmt.Close()
	citeStmt, err := tx.Prepare(`INSERT OR REPLACE INTO rm_citations(unit_id, cited_id, ord) VALUES(?,?,?)`)
	if err != nil {
		return err
	}
	defer citeStmt.Close()

	for _, d := range docs {
		if d.UnitID == "" {
			continue
		}
		if _, err := deleteTermsStmt.Exec(d.UnitID); err != nil {
			return err
		}
		if _, err := deleteCitesStmt.Exec(d.UnitID); err != nil {
			return err
		}
		if _, err := docStmt.Exec(
			d.UnitID, d.OccurredAt, d.NarratedAt, d.IngestedAt,
			int64(d.AuthorHash), int64(d.DocHash), int64(d.BlockHash),
			d.LineStart, d.LineEnd, d.Layer, len(d.Terms), d.RawText,
		); err != nil {
			return err
		}
		for ord, cid := range d.CitedIDs {
			if _, err := citeStmt.Exec(d.UnitID, cid, ord); err != nil {
				return err
			}
		}
		for hash, term := range d.Terms {
			occ := d.Counts[hash]
			if occ < 1 {
				occ = 1
			}
			var posBlob []byte
			if len(d.Positions) > 0 {
				posBlob = EncodePositionsBlob(d.Positions[hash])
			} else if term.Sent > 0 && term.Slot > 0 {
				posBlob = EncodePositionsBlob([]TermPosition{{Sentence: int(term.Sent - 1), Slot: int(term.Slot - 1)}})
			}
			packedFlags := PackTermPersistFlags(term.Flags, term.Role, term.ExtFlags)
			if _, err := termStmt.Exec(int64(hash), d.UnitID, int64(term.Bits), packedFlags, int64(term.Group), int64(occ), posBlob); err != nil {
				return err
			}
		}
	}
	return nil
}

// DocCount 返回当前存储内的文档数。
func (s *Store) DocCount() (int, error) {
	if s.backend != nil {
		return s.backend.DocCount()
	}
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM rm_docs`).Scan(&n)
	return n, err
}

// LoadIndex 从存储一次性重建内存倒排视图并计算原子信息能量（在单个事务快照内读取文档与词项表）。
func (s *Store) LoadIndex() (*Index, error) {
	if s.backend != nil {
		return s.backend.LoadIndex()
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	idx := &Index{
		UnitOf:    make(map[string]int32, 12000),
		Mass:      make(map[uint64]float64, 80000),
		DF:        make(map[uint64]int, 80000),
		Postings:  make(map[uint64][]Posting, 80000),
		TermBits:  make(map[uint64]uint16, 80000),
		TermFlags: make(map[uint64]uint8, 80000),
	}

	docRows, err := tx.Query(`SELECT unit_id, occurred_at, narrated_at, ingested_at, author_hash, doc_hash, block_hash, line_start, line_end, layer, term_count FROM rm_docs ORDER BY unit_id`)
	if err != nil {
		return nil, err
	}
	var metas []DocMeta
	var termCounts []int
	var totalTermPostings int
	for docRows.Next() {
		var unitID string
		var occurredAt, narratedAt, ingestedAt, authorHash, docHash, blockHash int64
		var lineStart, lineEnd, termCount int64
		var layer string
		if err := docRows.Scan(&unitID, &occurredAt, &narratedAt, &ingestedAt, &authorHash, &docHash, &blockHash, &lineStart, &lineEnd, &layer, &termCount); err != nil {
			docRows.Close()
			return nil, err
		}
		idx.UnitOf[unitID] = int32(len(idx.UnitIDs))
		idx.UnitIDs = append(idx.UnitIDs, unitID)
		metas = append(metas, DocMeta{
			OccurredAt: occurredAt,
			NarratedAt: narratedAt,
			IngestedAt: ingestedAt,
			AuthorHash: uint64(authorHash),
			DocHash:    uint64(docHash),
			BlockHash:  uint64(blockHash),
			LineStart:  int(lineStart),
			LineEnd:    int(lineEnd),
			Layer:      layer,
		})
		tc := int(termCount)
		if tc < 4 {
			tc = 4
		}
		termCounts = append(termCounts, tc)
		totalTermPostings += tc
	}
	docRows.Close()
	if err := docRows.Err(); err != nil {
		return nil, err
	}
	idx.N = len(idx.UnitIDs)
	idx.DocMass = make([]float64, idx.N)
	idx.DocTerms = make([][]uint64, idx.N)
	idx.UnitMeta = make([]DocMeta, idx.N)
	idx.UnitPositions = make([]map[uint64][]TermPosition, idx.N)
	idx.UnitBits = make([]map[uint64]uint16, idx.N)
	idx.UnitFlags = make([]map[uint64]uint8, idx.N)
	idx.UnitRoleExt = make([]map[uint64]uint8, idx.N)
	idx.UnitExtFlags = make([]map[uint64]uint16, idx.N)
	idx.UnitGroups = make([]map[uint64]uint64, idx.N)
	for at, stored := range metas {
		idx.UnitMeta[at] = stored
	}

	citeRows, err := tx.Query(`SELECT unit_id, cited_id FROM rm_citations ORDER BY unit_id, ord, cited_id`)
	if err != nil {
		return nil, err
	}
	for citeRows.Next() {
		var unitID, citedID string
		if err := citeRows.Scan(&unitID, &citedID); err != nil {
			citeRows.Close()
			return nil, err
		}
		if u, ok := idx.UnitOf[unitID]; ok {
			idx.UnitMeta[u].CitedIDs = append(idx.UnitMeta[u].CitedIDs, citedID)
		}
	}
	citeRows.Close()
	if err := citeRows.Err(); err != nil {
		return nil, err
	}

	if totalTermPostings < 1024 {
		totalTermPostings = 1024
	}
	postingArena := make([]Posting, 0, totalTermPostings)
	posArena := make([]TermPosition, 0, totalTermPostings*2)
	var curHash uint64
	var hasCurHash bool
	var runStart int

	termRows, err := tx.Query(`SELECT term_hash, unit_id, bits_x10, class_flags, group_hash, occurrences, pos_blob FROM rm_terms ORDER BY term_hash, unit_id`)
	if err != nil {
		return nil, err
	}
	for termRows.Next() {
		var hash int64
		var unitID string
		var bitsX10, classFlags, groupHash, occ int64
		var posBlob []byte
		if err := termRows.Scan(&hash, &unitID, &bitsX10, &classFlags, &groupHash, &occ, &posBlob); err != nil {
			termRows.Close()
			return nil, err
		}
		unit, ok := idx.UnitOf[unitID]
		if !ok {
			termRows.Close()
			return idx, fmt.Errorf("rm/store: orphan term row for unknown unit %q", unitID)
		}
		h := uint64(hash)
		if !hasCurHash {
			curHash = h
			hasCurHash = true
			runStart = 0
		} else if h != curHash {
			runEnd := len(postingArena)
			idx.Postings[curHash] = postingArena[runStart:runEnd:runEnd]
			idx.DF[curHash] = runEnd - runStart
			curHash = h
			runStart = runEnd
		}
		if occ < 1 {
			occ = 1
		} else if occ > math.MaxInt32 {
			occ = math.MaxInt32
		}
		postingArena = append(postingArena, Posting{Unit: unit, Occ: int32(occ)})
		hint := termCounts[unit]
		if groupHash != 0 {
			if idx.UnitGroups[unit] == nil {
				idx.UnitGroups[unit] = make(map[uint64]uint64, hint)
			}
			idx.UnitGroups[unit][h] = uint64(groupHash)
		}
		if idx.UnitBits[unit] == nil {
			idx.UnitBits[unit] = make(map[uint64]uint16, hint)
		}
		if bitsX10 > 0 {
			b16 := uint16(bitsX10)
			if b16 > idx.TermBits[h] {
				idx.TermBits[h] = b16
			}
			if b16 > idx.UnitBits[unit][h] {
				idx.UnitBits[unit][h] = b16
			}
		} else if _, exists := idx.UnitBits[unit][h]; !exists {
			idx.UnitBits[unit][h] = 0
		}
		if idx.UnitFlags[unit] == nil {
			idx.UnitFlags[unit] = make(map[uint64]uint8, hint/2+1)
		}
		fl8 := uint8(classFlags)
		idx.UnitFlags[unit][h] |= fl8
		if fl8 != 0 {
			idx.TermFlags[h] |= fl8
		}
		if re8 := uint8(classFlags >> 8); re8 != 0 {
			if idx.UnitRoleExt[unit] == nil {
				idx.UnitRoleExt[unit] = make(map[uint64]uint8, hint/2+1)
			}
			idx.UnitRoleExt[unit][h] = re8
		}
		// Restore the full 16-bit ExtFlags (bits 16-31); low-3-only values stay out of
		// UnitExtFlags, matching how BuildIndex only records high-carrying entries.
		if ext := TermPersistExtFlags(classFlags); ext&^uint16(0x07) != 0 {
			if idx.UnitExtFlags[unit] == nil {
				idx.UnitExtFlags[unit] = make(map[uint64]uint16, hint/2+1)
			}
			idx.UnitExtFlags[unit][h] |= ext
		}
		if len(posBlob) > 0 {
			if idx.UnitPositions[unit] == nil {
				idx.UnitPositions[unit] = make(map[uint64][]TermPosition, hint)
			}
			var posSlice []TermPosition
			var decErr error
			posSlice, posArena, decErr = DecodePositionsIntoArena(posBlob, posArena)
			if decErr != nil {
				termRows.Close()
				return idx, decErr
			}
			idx.UnitPositions[unit][h] = posSlice
		}
	}
	if hasCurHash {
		runEnd := len(postingArena)
		idx.Postings[curHash] = postingArena[runStart:runEnd:runEnd]
		idx.DF[curHash] = runEnd - runStart
	}
	termRows.Close()
	if err := termRows.Err(); err != nil {
		return nil, err
	}

	idx.FinalizeMasses()
	return idx, nil
}
