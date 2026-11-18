// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package thinclient

import (
	"context"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	core "github.com/rocareer/rolling-memory/internal/core"
)

// maxCSRDeltaDocs 是允许在已有 .zcsr 零拷贝基线引擎之上叠加内存增量微引擎（deltaEngine）的
// 最大新增单元数：日常守护进程运行期间，后台 autowatch 新增的对话在达到该上限前全部走
// 毫秒级、兆字节级的 deltaEngine，避免写 1 条对话就作废 .zcsr 重跑数分钟全量构建。
const maxCSRDeltaDocs = 2048

// 本地检索引擎的 .zcsr 磁盘簇缓存（零拷贝冷启动）。
//
// 背景与取舍：核心引擎的一次全量构建（core.BuildIndex + MaterializeClusterState）在
// 数万单元规模上以分钟计，而核心自带 .zcsr 只读磁盘段（WriteDiskClusterCSR /
// OpenDiskClusterEngine，mmap 零拷贝、毫秒级冷启动）。本适配器只做「语料新鲜度指纹 -> 磁盘簇」
// 的缓存槽与代际校验，不重写任何核心算子：
//   - 指纹一致：直接 OpenDiskClusterEngine，跳过引擎全量构建与簇物化；
//   - 纯追加漂移（≤ maxCSRDeltaDocs 个新单元）：基线 mmap + 小增量内存引擎双层检索；
//   - 其余情况：全量构建一次，随后原子写回 .zcsr 与指纹档（core 侧 tmp->fsync->rename）。
//
// 新鲜度指纹（`v4|` 行）覆盖引擎索引的全部输入维度：文档表四元聚合（行数/行号和/最大行号/
// 文本总长）+ docs_rev + 背景块四元聚合 + bg_rev + episodic_revision + episodic 索引行数 +
// 编解码键十六进制。触发器修订号是纯追加代数（Δrev == Δcnt）的唯一依据：聚合指纹对
// 「等长 UPDATE」盲视，只有修订号能证明表自上次构建以来只发生过 INSERT。
const (
	vaultCSRSuffix      = ".zcsr"
	vaultCSRGenSuffix   = ".zcsr.gen"
	vaultCSRUnitsSuffix = ".zcsr.units"
)

// vaultCSRPaths 返回磁盘簇与指纹档的固定路径（与主库同目录）。
func vaultCSRPaths(dbPath string) (string, string) {
	return dbPath + vaultCSRSuffix, dbPath + vaultCSRGenSuffix
}

// vaultCSRUnitPaths 返回单元指纹旁车路径（与指纹档同目录）。
func vaultCSRUnitPaths(dbPath string) string {
	return dbPath + vaultCSRUnitsSuffix
}

// 单元内容指纹：基线单元与当前行的规范身份比较。凡指纹变化或缺失于旁车的单元一律进入
// 增量层，与写入模式（INSERT / UPDATE / 删除后新建 / 重切块 / 重编码世代）无关，语义始终
// 以当前行为准。fnv64a 在数万单元规模上的生日碰撞概率可忽略（≈4×10^8 / 2^65）。
func vaultUnitFingerprintInline(layer string, occ int64, rawText string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("i|"))
	_, _ = h.Write([]byte(layer))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(strconv.FormatInt(occ, 10)))
	_, _ = h.Write([]byte("|"))
	_, _ = h.Write([]byte(rawText))
	return h.Sum64()
}

func vaultUnitFingerprintBackground(relPath, absPath, anchor string, occ, coord64 int64, waveBar string, slotsLen int64) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(fmt.Sprintf("b|%s|%s|%s|%d|%d|%s|%d", relPath, absPath, anchor, occ, coord64, waveBar, slotsLen)))
	return h.Sum64()
}

func vaultUnitFingerprintEpisodic(digest []byte) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("e|"))
	_, _ = h.Write([]byte(hex.EncodeToString(digest)))
	return h.Sum64()
}

// thinTableExists 报告可选薄表是否存在（`thin_vault_bg_chunks` 等表在首次挂载/首写前
// 惰性创建，旁车构建必须把缺表视为空集而非失败）。
func (v *Vault) thinTableExists(name string) bool {
	var exists int
	if err := v.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?)`, name).Scan(&exists); err != nil {
		return false
	}
	return exists != 0
}

// persistCSRUnitFingerprints 从表内当前行生成单元指纹旁车（仅扫描轻量身份列；在分钟级引擎
// 构建之后执行，数万行的元数据读取可忽略）。返回旁车路径与是否写入成功。
func (v *Vault) persistCSRUnitFingerprints(dbPath string, codecKey []byte) (string, bool) {
	if v == nil || v.db == nil {
		return "", false
	}
	unitPath := vaultCSRUnitPaths(dbPath)
	var b strings.Builder

	rows, err := v.db.Query(`SELECT doc_id, layer, occurred_at, raw_text FROM thin_vault_docs WHERE substr(trim(raw_text), 1, 5) != '@ref:'`)
	if err != nil {
		return "", false
	}
	for rows.Next() {
		var docID, layer, rawText string
		var occ int64
		if rows.Scan(&docID, &layer, &occ, &rawText) != nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rawText), "@ref:") {
			continue
		}
		fmt.Fprintf(&b, "%s\t%016x\n", docID, vaultUnitFingerprintInline(layer, occ, rawText))
	}
	if rows.Err() != nil {
		rows.Close()
		return "", false
	}
	rows.Close()

	if v.thinTableExists("thin_vault_bg_chunks") {
		bgRows, err := v.db.Query(`SELECT unit_id, COALESCE(rel_path, ''), COALESCE(abs_path, ''), COALESCE(anchor, ''), occurred_at, coord64, waveform_bar, LENGTH(slots_json) FROM thin_vault_bg_chunks`)
		if err != nil {
			return "", false
		}
		for bgRows.Next() {
			var unitID, relPath, absPath, anchor, waveBar string
			var occ, coord64, slotsLen int64
			if bgRows.Scan(&unitID, &relPath, &absPath, &anchor, &occ, &coord64, &waveBar, &slotsLen) != nil {
				continue
			}
			fmt.Fprintf(&b, "%s\t%016x\n", unitID, vaultUnitFingerprintBackground(relPath, absPath, anchor, occ, coord64, waveBar, slotsLen))
		}
		if bgRows.Err() != nil {
			bgRows.Close()
			return "", false
		}
		bgRows.Close()
	}

	if len(codecKey) > 0 && v.thinTableExists("thin_vault_episodic_index") {
		epiRows, err := v.db.Query(
			`SELECT d.doc_id, i.content_digest FROM thin_vault_docs d JOIN thin_vault_episodic_index i ON i.doc_id = d.doc_id AND i.ref_pointer = d.raw_text WHERE d.layer = 'episodic' ORDER BY d.doc_id, (i.codec_key = ?) DESC, i.rowid DESC`,
			codecKey,
		)
		if err != nil {
			return "", false
		}
		seen := make(map[string]bool, 4096)
		for epiRows.Next() {
			var docID string
			var digest []byte
			if epiRows.Scan(&docID, &digest) != nil {
				continue
			}
			if seen[docID] {
				continue
			}
			seen[docID] = true
			fmt.Fprintf(&b, "%s\t%016x\n", docID, vaultUnitFingerprintEpisodic(digest))
		}
		if epiRows.Err() != nil {
			epiRows.Close()
			return "", false
		}
		epiRows.Close()
	}

	tmp := unitPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return "", false
	}
	if err := os.Rename(tmp, unitPath); err != nil {
		return "", false
	}
	return unitPath, true
}

// loadCSRUnitFingerprints 读取单元指纹旁车（按指纹档身份缓存于 snapRef；数万行解析在毫秒级）。
func (v *Vault) loadCSRUnitFingerprints(dbPath, genLine string) (map[string]uint64, bool) {
	if v == nil || strings.TrimSpace(dbPath) == "" || genLine == "" {
		return nil, false
	}
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.RLock()
		cached := v.snapRef.csrUnits
		cachedGen := v.snapRef.csrUnitsGen
		v.slotCacheMu.RUnlock()
		if cached != nil && cachedGen == genLine {
			return cached, true
		}
	}
	raw, err := os.ReadFile(vaultCSRUnitPaths(dbPath))
	if err != nil {
		return nil, false
	}
	units := make(map[string]uint64, 4096)
	for _, line := range strings.Split(string(raw), "\n") {
		if line == "" {
			continue
		}
		tab := strings.IndexByte(line, '\t')
		if tab <= 0 || tab+1 >= len(line) {
			continue
		}
		fp, err := strconv.ParseUint(line[tab+1:], 16, 64)
		if err != nil {
			continue
		}
		units[line[:tab]] = fp
	}
	if len(units) == 0 {
		return nil, false
	}
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.Lock()
		v.snapRef.csrUnits = units
		v.snapRef.csrUnitsGen = genLine
		v.slotCacheMu.Unlock()
	}
	return units, true
}

// mainDBPath 返回主数据库的文件路径（内存库、便携覆盖或查询失败时为空串）。
// 记忆化一次：召回入口每次都会问一次，不让 PRAGMA 查询落到热路径上。
func (v *Vault) mainDBPath() string {
	if v == nil || v.db == nil {
		return ""
	}
	v.dbPathOnce.Do(func() {
		var file string
		if err := v.db.QueryRow(`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&file); err != nil {
			return
		}
		file = strings.TrimSpace(file)
		if file == "" {
			return
		}
		if abs, err := filepath.Abs(file); err == nil {
			file = abs
		}
		v.dbPath = file
	})
	return v.dbPath
}

// corpusFreshnessLine 生成 v4 新鲜度指纹行（codecKeyHex 为编解码键的十六进制编码，避免
// 键内任意字节污染行结构）。行内字段顺序固定，比较即逐位相等比较。
func corpusFreshnessLine(
	docCnt, docRowSum, docMaxRowID, docTextLen, docsRev int64,
	bgCnt, bgMaxRowID, bgOccSum, bgSlotsLen, bgRev int64,
	episodicRevision, epiCnt int64,
	codecKeyHex string,
) string {
	return fmt.Sprintf("v4|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%d|%s",
		docCnt, docRowSum, docMaxRowID, docTextLen, docsRev,
		bgCnt, bgMaxRowID, bgOccSum, bgSlotsLen, bgRev,
		episodicRevision, epiCnt, codecKeyHex)
}

// freshnessLine 以快照自身记录的语料维度重建 v4 指纹行（落盘与复用校验用）。
func (s *vaultCorpusSnapshot) freshnessLine() string {
	if s == nil {
		return ""
	}
	return corpusFreshnessLine(
		s.docCnt, s.docRowSum, s.maxRowID, s.docTextLen, s.docsRev,
		s.bgCnt, s.bgMaxRowID, s.bgOccSum, s.bgSlotsLen, s.bgRev,
		s.episodicRevision, s.epiCnt, hex.EncodeToString([]byte(s.episodicCodecKey)),
	)
}

// csrFreshness 是 v4 指纹行的解析结果。
type csrFreshness struct {
	docCnt           int64
	docRowSum        int64
	docMaxRowID      int64
	docTextLen       int64
	docsRev          int64
	bgCnt            int64
	bgMaxRowID       int64
	bgOccSum         int64
	bgSlotsLen       int64
	bgRev            int64
	episodicRevision int64
	epiCnt           int64
	codecKeyHex      string
}

// parseCSRFreshnessLine 解析 v4 指纹行；历史 v3 签名档与损坏内容一律判否（回退全量构建）。
func parseCSRFreshnessLine(line string) (csrFreshness, bool) {
	parts := strings.Split(strings.TrimSpace(line), "|")
	if len(parts) != 14 || parts[0] != "v4" {
		return csrFreshness{}, false
	}
	nums := make([]int64, 12)
	for i := range nums {
		n, err := strconv.ParseInt(parts[i+1], 10, 64)
		if err != nil {
			return csrFreshness{}, false
		}
		nums[i] = n
	}
	return csrFreshness{
		docCnt: nums[0], docRowSum: nums[1], docMaxRowID: nums[2], docTextLen: nums[3], docsRev: nums[4],
		bgCnt: nums[5], bgMaxRowID: nums[6], bgOccSum: nums[7], bgSlotsLen: nums[8], bgRev: nums[9],
		episodicRevision: nums[10], epiCnt: nums[11],
		codecKeyHex: parts[13],
	}, true
}

// acquireCachedCSREngine 打开或复用与主库同目录的 .zcsr 零拷贝基线引擎（按文件 mtime+size
// 缓存句柄，避免在增量写入后重复 mmap 同一个数 GB 的 .zcsr 文件）。
func (v *Vault) acquireCachedCSREngine(dbPath string) (*core.Engine, string, bool) {
	if v == nil || strings.TrimSpace(dbPath) == "" {
		return nil, "", false
	}
	csrPath, genPath := vaultCSRPaths(dbPath)
	rawGen, err := os.ReadFile(genPath)
	if err != nil {
		return nil, "", false
	}
	genLine := strings.TrimSpace(string(rawGen))
	if _, ok := parseCSRFreshnessLine(genLine); !ok {
		return nil, "", false
	}
	st, err := os.Stat(csrPath)
	if err != nil || st.Size() <= 0 {
		return nil, "", false
	}
	mtimeNS, fsize := st.ModTime().UnixNano(), st.Size()
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.RLock()
		cachedEng := v.snapRef.csrEngine
		sameFile := cachedEng != nil && cachedEng.Index != nil && cachedEng.Index.N > 0 &&
			v.snapRef.csrFileMTimeNS == mtimeNS && v.snapRef.csrFileSize == fsize
		v.slotCacheMu.RUnlock()
		if sameFile {
			return cachedEng, genLine, true
		}
	}
	eng, err := core.OpenDiskClusterEngine(csrPath)
	if err != nil || eng == nil || eng.Index == nil || eng.Index.N <= 0 {
		return nil, "", false
	}
	if v.slotCacheMu != nil && v.snapRef != nil {
		v.slotCacheMu.Lock()
		v.snapRef.csrEngine = eng
		v.snapRef.csrFileMTimeNS = mtimeNS
		v.snapRef.csrFileSize = fsize
		v.slotCacheMu.Unlock()
	}
	return eng, genLine, true
}

// buildCorpusSnapshotFromCSR 尝试用同目录的 .zcsr 磁盘簇建立零拷贝快照（纯基线，或
// 「基线 mmap + 纯追加增量微引擎」双层检索），跳过对数万条存量背景块与历史对话
// 数百 MB 载荷的逐行解码与 Go Map 分配。仅在两类证明成立时才复用：
//  1. 指纹逐位一致（零写入）；或
//  2. 纯追加代数成立（docs 与 episodic 索引的 Δrev == Δcnt、背景块全等，且增量 ≤ 上限）。
//
// 任何其余漂移（UPDATE/DELETE/超量追加/编解码键变更）一律返回 false，由调用方回退全量构建。
func (v *Vault) buildCorpusSnapshotFromCSR(
	ctx context.Context,
	docCnt, docRowSum, docMaxRowID, docTextLen, docsRev, bgRev, episodicRevision int64,
	codecKey []byte,
	bgCnt, bgMaxRowID, bgOccSum, bgSlotsLen int64,
	revGateOK bool,
) (*vaultCorpusSnapshot, bool) {
	if v == nil || v.db == nil {
		return nil, false
	}
	dbPath := v.mainDBPath()
	if strings.TrimSpace(dbPath) == "" {
		return nil, false
	}
	eng, storedLine, ok := v.acquireCachedCSREngine(dbPath)
	if !ok || eng == nil || eng.Index == nil {
		return nil, false
	}
	stored, parsed := parseCSRFreshnessLine(storedLine)
	if !parsed {
		return nil, false
	}

	var epiCnt int64
	_ = v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM thin_vault_episodic_index`).Scan(&epiCnt)
	codecKeyHex := hex.EncodeToString(codecKey)
	currentLine := corpusFreshnessLine(
		docCnt, docRowSum, docMaxRowID, docTextLen, docsRev,
		bgCnt, bgMaxRowID, bgOccSum, bgSlotsLen, bgRev,
		episodicRevision, epiCnt, codecKeyHex,
	)
	isExact := currentLine == storedLine

	// 非精确匹配：以单元级内容指纹判定漂移范围（旁车缺失或编解码键变更一律保守回退全量构建）。
	var baseUnits map[string]uint64
	if !isExact {
		var unitsOK bool
		baseUnits, unitsOK = v.loadCSRUnitFingerprints(dbPath, storedLine)
		if !unitsOK || stored.codecKeyHex != codecKeyHex {
			return nil, false
		}
	}
	var deltaUnits []string
	deltaSeen := make(map[string]bool, 64)
	checkUnit := func(id string, fp uint64) bool {
		_, inBase := eng.Index.LookupUnit(id)
		if isExact {
			return inBase
		}
		if inBase {
			if prev, seen := baseUnits[id]; seen && prev == fp {
				return true
			}
		}
		if !deltaSeen[id] {
			deltaSeen[id] = true
			deltaUnits = append(deltaUnits, id)
		}
		return len(deltaUnits) <= maxCSRDeltaDocs
	}

	var inlineDocs []vaultDocEntry
	rows, err := v.db.QueryContext(ctx, `SELECT doc_id, layer, occurred_at, raw_text, address_uri, coord64, waveform_bar, COALESCE(locus, '') FROM thin_vault_docs WHERE substr(trim(raw_text), 1, 5) != '@ref:'`)
	if err != nil {
		return nil, false
	}
	for rows.Next() {
		if ctx.Err() != nil {
			rows.Close()
			return nil, false
		}
		var docID, layer, rawText, addrURI, waveBar, locus string
		var occ, coord64 int64
		if rows.Scan(&docID, &layer, &occ, &rawText, &addrURI, &coord64, &waveBar, &locus) != nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rawText), "@ref:") {
			continue
		}
		if !checkUnit(docID, vaultUnitFingerprintInline(layer, occ, rawText)) {
			rows.Close()
			return nil, false
		}
		inlineDocs = append(inlineDocs, vaultDocEntry{
			docID:   docID,
			layer:   layer,
			occ:     occ,
			addrURI: addrURI,
			coord64: uint64(coord64),
			waveBar: waveBar,
			locus:   strings.TrimSpace(locus),
			docLen:  1,
		})
	}
	if rows.Err() != nil {
		rows.Close()
		return nil, false
	}
	rows.Close()

	var bgDocs []vaultDocEntry
	if bgCnt > 0 {
		bgRows, bgErr := v.db.QueryContext(ctx, `SELECT unit_id, occurred_at, address_uri, coord64, waveform_bar, COALESCE(rel_path, ''), COALESCE(abs_path, ''), COALESCE(anchor, ''), LENGTH(slots_json) FROM thin_vault_bg_chunks`)
		if bgErr != nil {
			return nil, false
		}
		bgDocs = make([]vaultDocEntry, 0, int(bgCnt))
		for bgRows.Next() {
			if ctx.Err() != nil {
				bgRows.Close()
				return nil, false
			}
			var unitID, addrURI, waveBar, relPath, absPath, anchor string
			var occ, coord64, slotsLen int64
			if bgRows.Scan(&unitID, &occ, &addrURI, &coord64, &waveBar, &relPath, &absPath, &anchor, &slotsLen) != nil {
				continue
			}
			if !checkUnit(unitID, vaultUnitFingerprintBackground(relPath, absPath, anchor, occ, coord64, waveBar, slotsLen)) {
				bgRows.Close()
				return nil, false
			}
			bgDocs = append(bgDocs, vaultDocEntry{
				docID:   unitID,
				layer:   LayerBackground,
				occ:     occ,
				addrURI: addrURI,
				coord64: uint64(coord64),
				waveBar: waveBar,
				docLen:  1,
			})
		}
		if bgRows.Err() != nil {
			bgRows.Close()
			return nil, false
		}
		bgRows.Close()
	}

	var episodicDocs []vaultDocEntry
	if len(codecKey) > 0 {
		epiRows, epiErr := v.db.QueryContext(ctx,
			`SELECT d.doc_id, d.layer, d.occurred_at, d.address_uri, d.coord64, d.waveform_bar, COALESCE(d.locus, ''), i.ref_pointer, i.content_digest FROM thin_vault_docs d JOIN thin_vault_episodic_index i ON i.doc_id = d.doc_id AND i.ref_pointer = d.raw_text WHERE d.layer = 'episodic' ORDER BY d.doc_id, (i.codec_key = ?) DESC, i.rowid DESC`,
			codecKey,
		)
		if epiErr != nil {
			return nil, false
		}
		seenEpi := make(map[string]bool, 4096)
		for epiRows.Next() {
			if ctx.Err() != nil {
				epiRows.Close()
				return nil, false
			}
			var docID, layer, addrURI, waveBar, locus, refPointer string
			var occ, coord64 int64
			var digest []byte
			if epiRows.Scan(&docID, &layer, &occ, &addrURI, &coord64, &waveBar, &locus, &refPointer, &digest) != nil {
				continue
			}
			if seenEpi[docID] {
				continue
			}
			seenEpi[docID] = true
			if !checkUnit(docID, vaultUnitFingerprintEpisodic(digest)) {
				epiRows.Close()
				return nil, false
			}
			episodicDocs = append(episodicDocs, vaultDocEntry{
				docID:         docID,
				layer:         layer,
				occ:           occ,
				addrURI:       addrURI,
				coord64:       uint64(coord64),
				waveBar:       waveBar,
				locus:         strings.TrimSpace(locus),
				refPointer:    refPointer,
				contentDigest: digest,
				docLen:        1,
			})
		}
		if epiRows.Err() != nil {
			epiRows.Close()
			return nil, false
		}
		epiRows.Close()
	}

	totalDocs := len(inlineDocs) + len(bgDocs) + len(episodicDocs)
	if totalDocs == 0 {
		return nil, false
	}
	if isExact {
		// 零写入证明下，扫描到的全集必须与基线单元数逐位相等（并发写入会使扫描超出基线，
		// 此时保守回退全量构建）。
		if totalDocs != eng.Index.N {
			return nil, false
		}
	}

	docs := make([]vaultDocEntry, 0, totalDocs)
	docs = append(docs, inlineDocs...)
	docs = append(docs, bgDocs...)
	docs = append(docs, episodicDocs...)

	validEpisodicIDs := make(map[string]struct{}, len(episodicDocs))
	for _, ed := range episodicDocs {
		validEpisodicIDs[ed.docID] = struct{}{}
	}
	docIdxByID := make(map[string]int, len(docs))
	for idx, d := range docs {
		docIdxByID[d.docID] = idx
	}

	var maskedUnits []string
	if !isExact {
		for baseID := range baseUnits {
			if _, alive := docIdxByID[baseID]; !alive {
				maskedUnits = append(maskedUnits, baseID)
				if len(deltaUnits)+len(maskedUnits) > maxCSRDeltaDocs {
					return nil, false
				}
			}
		}
		for _, id := range deltaUnits {
			maskedUnits = append(maskedUnits, id)
		}
	}

	var deltaDocs []core.Doc
	if len(deltaUnits) > 0 {
		var okDelta bool
		deltaDocs, okDelta = v.loadDeltaCoreDocs(ctx, deltaUnits)
		if !okDelta {
			return nil, false
		}
		validDelta := make(map[string]bool, len(deltaDocs))
		for _, dd := range deltaDocs {
			validDelta[dd.UnitID] = true
		}
		for _, id := range deltaUnits {
			if !validDelta[id] {
				delete(validEpisodicIDs, id)
			}
		}
	}

	snapEng := eng
	if len(deltaDocs) > 0 || len(maskedUnits) > 0 {
		snapEng = eng.OverlayDelta(deltaDocs, maskedUnits)
	}

	snap := &vaultCorpusSnapshot{
		docCnt:           docCnt,
		docRowSum:        docRowSum,
		maxRowID:         docMaxRowID,
		docTextLen:       docTextLen,
		docsRev:          docsRev,
		bgRev:            bgRev,
		epiCnt:           epiCnt,
		episodicRevision: episodicRevision,
		episodicCodecKey: string(codecKey),
		bgCnt:            bgCnt,
		bgMaxRowID:       bgMaxRowID,
		bgOccSum:         bgOccSum,
		bgSlotsLen:       bgSlotsLen,
		inlineCount:      len(inlineDocs),
		docs:             docs,
		episodicDocs:     episodicDocs,
		validEpisodicIDs: validEpisodicIDs,
		docIdxByID:       docIdxByID,
		coreEngine:       snapEng,
	}
	snap.coreOnce.Do(func() {})
	return snap, true
}

// loadDeltaCoreDocs 仅为增量新增的少量单元（≤ maxCSRDeltaDocs）从 SQLite 加载并构建
// core.Doc，耗时在毫秒级、内存占用在兆字节级。
func (v *Vault) loadDeltaCoreDocs(ctx context.Context, unitIDs []string) ([]core.Doc, bool) {
	if len(unitIDs) == 0 {
		return nil, true
	}
	te := v.newVaultTermExtractor()
	out := make([]core.Doc, 0, len(unitIDs))
	for _, id := range unitIDs {
		if ctx.Err() != nil {
			return nil, false
		}
		if strings.HasPrefix(id, "bg:") {
			var occ int64
			var relPath, absPath, anchor string
			var slotsRaw []byte
			if err := v.db.QueryRowContext(ctx,
				`SELECT occurred_at, COALESCE(rel_path, ''), COALESCE(abs_path, ''), COALESCE(anchor, ''), slots_json FROM thin_vault_bg_chunks WHERE unit_id = ?`,
				id,
			).Scan(&occ, &relPath, &absPath, &anchor, &slotsRaw); err != nil {
				continue
			}
			tMeta, tCounts, tPos, _, ok := decodeAtomSlotCoreTerms(slotsRaw)
			if !ok {
				continue
			}
			fileKey := strings.TrimSpace(relPath)
			if fileKey == "" {
				fileKey = strings.TrimSpace(absPath)
			}
			var docH, blockH uint64
			if fileKey != "" {
				docH = v.KeyedHash64(fileKey)
			}
			if anc := strings.TrimSpace(anchor); anc != "" && fileKey != "" {
				blockH = v.KeyedHash64(fileKey + "#" + anc)
			} else {
				blockH = v.KeyedHash64(id)
			}
			lStart, lEnd := parseBackgroundAnchorLines(anchor)
			out = append(out, core.Doc{
				UnitID:    id,
				Layer:     LayerBackground,
				DocHash:   docH,
				BlockHash: blockH,
				LineStart: lStart,
				LineEnd:   lEnd,
				Terms:     tMeta,
				Counts:    tCounts,
				Positions: tPos,
			})
			continue
		}
		var layer, rawText string
		var occ int64
		if err := v.db.QueryRowContext(ctx,
			`SELECT layer, occurred_at, raw_text FROM thin_vault_docs WHERE doc_id = ?`,
			id,
		).Scan(&layer, &occ, &rawText); err != nil {
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rawText), "@ref:") {
			var refPtr string
			var storedKey, payload, digest, mac []byte
			if err := v.db.QueryRowContext(ctx,
				`SELECT ref_pointer, codec_key, payload, content_digest, payload_mac FROM thin_vault_episodic_index WHERE doc_id = ? AND ref_pointer = ? ORDER BY rowid DESC LIMIT 1`,
				id, rawText,
			).Scan(&refPtr, &storedKey, &payload, &digest, &mac); err != nil {
				continue
			}
			if len(storedKey) == 0 || !v.validEpisodicPayloadMAC(id, refPtr, storedKey, payload, digest, mac) {
				continue
			}
			pkt, err := v.decodeEpisodicIndex(id, storedKey, payload, storedKey)
			if err != nil {
				continue
			}
			tMeta, tCounts, tPos := coreTermsFromPacket(pkt.Slots, pkt.SentenceFrames)
			out = append(out, core.Doc{
				UnitID:     id,
				OccurredAt: occ,
				Layer:      layer,
				Terms:      tMeta,
				Counts:     tCounts,
				Positions:  tPos,
			})
			continue
		}
		cDoc := v.extractVaultCoreDoc(te, id, occ, rawText)
		cDoc.Layer = layer
		out = append(out, cDoc)
	}
	return out, true
}

// searchSnapshotCoreEngines 委托给 core.Engine 原生检索（含基线 .zcsr 与 core.Engine 内建的
// Delta Overlay 自动融合与遮蔽）。
func (s *vaultCorpusSnapshot) searchSnapshotCoreEngines(v *Vault, queryText string, queryPkt *BlindPulsePacket, topK int) []core.Hit {
	if s == nil || len(s.docs) == 0 {
		return nil
	}
	eng := s.ensureSnapshotCoreEngine(v)
	if eng == nil {
		return nil
	}
	s.coreMu.Lock()
	defer s.coreMu.Unlock()
	if queryPkt != nil {
		qTerms, qPos := v.extractQueryFromBlindPacket(eng.Index, *queryPkt)
		if len(qTerms) == 0 {
			return nil
		}
		return eng.SearchWithPositions(qTerms, qPos, topK)
	}
	if v != nil && v.segOpts.Segmenter != nil {
		qPkt, _ := v.PackBlindPacket("Q-DEFAULT", queryText, 0)
		qTerms, qPos := v.extractQueryFromBlindPacket(eng.Index, qPkt)
		if len(qTerms) == 0 {
			return nil
		}
		return eng.SearchWithPositions(qTerms, qPos, topK)
	}
	te := v.newVaultTermExtractor()
	return eng.Query(te, queryText, topK)
}

// openSnapshotCoreEngineFromCSR 尝试按指纹行复用磁盘簇；任何缺失/不一致/损坏都返回 false，
// 由调用方回退全量构建（fail-open 到慢路径，绝不 fail-closed 掉召回）。
func openSnapshotCoreEngineFromCSR(dbPath, freshnessLine string) (*core.Engine, bool) {
	if strings.TrimSpace(dbPath) == "" || strings.TrimSpace(freshnessLine) == "" {
		return nil, false
	}
	csrPath, genPath := vaultCSRPaths(dbPath)
	raw, err := os.ReadFile(genPath)
	if err != nil || strings.TrimSpace(string(raw)) != strings.TrimSpace(freshnessLine) {
		return nil, false
	}
	eng, err := core.OpenDiskClusterEngine(csrPath)
	if err != nil || eng == nil || eng.Index == nil || eng.Index.N <= 0 {
		return nil, false
	}
	return eng, true
}

// persistSnapshotCoreCSR 原子写回磁盘簇、单元指纹旁车与指纹档（core 侧 tmp->fsync->rename 已原子），
// 返回磁盘簇路径与是否落盘成功（失败静默降级：下一次仍走全量构建，正确性不受影响）。
// 旁车写失败不阻断落盘（退化为「仅指纹逐位一致可复用」，保守但正确）。
//
// 同步调用：写回发生在引擎构建（分钟级）之后、发布之前；CLI 等短命进程也会把缓存落盘，
// 而长驻守护进程的后续构建同样受益。
func persistSnapshotCoreCSR(dbPath, freshnessLine string, eng *core.Engine, v *Vault, codecKey []byte) (string, bool) {
	if strings.TrimSpace(dbPath) == "" || strings.TrimSpace(freshnessLine) == "" || eng == nil {
		return "", false
	}
	if eng.Index != nil {
		// Release transient unit maps (UnitPositions/UnitBits/UnitFlags/Postings/DocTerms)
		// before allocating the contiguous .zcsr serialization buffer so the two multi-GB
		// structures never coexist on the Go heap.
		eng.Index.ReleaseTransientUnitMaps()
	}
	csrPath, genPath := vaultCSRPaths(dbPath)
	if err := core.WriteDiskClusterCSR(csrPath, eng); err != nil {
		return "", false
	}
	if v != nil {
		_, _ = v.persistCSRUnitFingerprints(dbPath, codecKey)
	}
	if err := os.WriteFile(genPath, []byte(strings.TrimSpace(freshnessLine)+"\n"), 0o600); err != nil {
		return "", false
	}
	return csrPath, true
}
