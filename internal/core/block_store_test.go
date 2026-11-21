// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package core

import (
	"math"
	"os"
	"path/filepath"
	"testing"
)

func assertTermBlockEqual(t *testing.T, got, want *termBlock) {
	t.Helper()
	if got.lo != want.lo || got.hi != want.hi {
		t.Fatalf("term block range [%d,%d) != [%d,%d)", got.lo, got.hi, want.lo, want.hi)
	}
	if len(got.keys) != len(want.keys) || len(got.units) != len(want.units) {
		t.Fatalf("term block counts keys %d/%d units %d/%d", len(got.keys), len(want.keys), len(got.units), len(want.units))
	}
	for i := range want.keys {
		if got.keys[i] != want.keys[i] {
			t.Fatalf("term block key %d %x != %x", i, got.keys[i], want.keys[i])
		}
	}
	for i := range want.rowPtr {
		if got.rowPtr[i] != want.rowPtr[i] {
			t.Fatalf("term block rowPtr %d %d != %d", i, got.rowPtr[i], want.rowPtr[i])
		}
	}
	for i := range want.units {
		if got.units[i] != want.units[i] {
			t.Fatalf("term block unit %d %d != %d", i, got.units[i], want.units[i])
		}
	}
	for i := range want.df {
		if got.df[i] != want.df[i] || got.cf[i] != want.cf[i] || got.propCnt[i] != want.propCnt[i] ||
			got.parentGrp[i] != want.parentGrp[i] || got.bits[i] != want.bits[i] ||
			got.flags[i] != want.flags[i] || got.primary[i] != want.primary[i] {
			t.Fatalf("term block stats diverge at column %d", i)
		}
	}
	for _, key := range want.keys {
		gotCol, gotOK := got.index(key)
		wantCol, wantOK := want.index(key)
		if gotOK != wantOK || gotCol != wantCol {
			t.Fatalf("term block index(%x) = (%d,%v), want (%d,%v)", key, gotCol, gotOK, wantCol, wantOK)
		}
	}
}

func assertDocBlockEqual(t *testing.T, got, want *docBlock) {
	t.Helper()
	if got.lo != want.lo || got.hi != want.hi {
		t.Fatalf("doc block range [%d,%d) != [%d,%d)", got.lo, got.hi, want.lo, want.hi)
	}
	if len(got.rowPtr) != len(want.rowPtr) || len(got.entries) != len(want.entries) || len(got.posPool) != len(want.posPool) {
		t.Fatalf("doc block shape rowPtr %d/%d entries %d/%d posPool %d/%d",
			len(got.rowPtr), len(want.rowPtr), len(got.entries), len(want.entries), len(got.posPool), len(want.posPool))
	}
	for i := range want.rowPtr {
		if got.rowPtr[i] != want.rowPtr[i] {
			t.Fatalf("doc block rowPtr %d %d != %d", i, got.rowPtr[i], want.rowPtr[i])
		}
	}
	for i := range want.entries {
		if got.entries[i] != want.entries[i] {
			t.Fatalf("doc block entry %d %+v != %+v", i, got.entries[i], want.entries[i])
		}
	}
	for i := range want.posPool {
		if got.posPool[i] != want.posPool[i] {
			t.Fatalf("doc block posPool %d %d != %d", i, got.posPool[i], want.posPool[i])
		}
	}
	if len(got.widePos) != len(want.widePos) {
		t.Fatalf("doc block widePos %d != %d", len(got.widePos), len(want.widePos))
	}
	for offset, position := range want.widePos {
		if got.widePos[offset] != position {
			t.Fatalf("doc block widePos[%d] %+v != %+v", offset, got.widePos[offset], position)
		}
	}
}

func TestBlockStoreRoundTrip(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(96))
	st := NewEngine(idx).ensureClusterState()
	if st == nil {
		t.Fatal("cluster state is nil")
	}
	tb := buildTermBlocks(idx, 24)
	db := buildDocBlocksFromState(st, idx, 24)
	if tb == nil || db == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen)
	if err != nil {
		t.Fatalf("write block store: %v", err)
	}
	if len(manifest.Term) != len(tb.blocks) || len(manifest.Doc) != len(db.blocks) {
		t.Fatalf("manifest blocks term=%d doc=%d", len(manifest.Term), len(manifest.Doc))
	}
	if _, err := os.Stat(filepath.Join(dir, blockManifestTmp)); !os.IsNotExist(err) {
		t.Fatal("manifest tmp file survived the publish")
	}
	gotTB, gotDB, err := readBlockStore(dir)
	if err != nil {
		t.Fatalf("read block store: %v", err)
	}
	if gotTB == nil || gotDB == nil {
		t.Fatal("reloaded block views are nil")
	}
	if gotTB.n != tb.n || gotTB.gen != tb.gen {
		t.Fatalf("reloaded header n=%d gen=%d, want n=%d gen=%d", gotTB.n, gotTB.gen, tb.n, tb.gen)
	}
	if len(gotTB.blocks) != len(tb.blocks) || len(gotDB.blocks) != len(db.blocks) {
		t.Fatalf("reloaded blocks term=%d doc=%d", len(gotTB.blocks), len(gotDB.blocks))
	}
	for i := range tb.blocks {
		assertTermBlockEqual(t, gotTB.blocks[i], tb.blocks[i])
	}
	for i := range db.blocks {
		assertDocBlockEqual(t, gotDB.blocks[i], db.blocks[i])
	}
	for _, key := range idx.TermKeys {
		wantStats, wantOK := tb.termStats(key)
		gotStats, gotOK := gotTB.termStats(key)
		if wantOK != gotOK || wantStats != gotStats {
			t.Fatalf("term %x stats %+v/%v != %+v/%v", key, gotStats, gotOK, wantStats, wantOK)
		}
		wantSeq := seg2dBlockVisited(tb, key)
		gotSeq := seg2dBlockVisited(gotTB, key)
		if len(gotSeq) != len(wantSeq) {
			t.Fatalf("term %x postings %d != %d", key, len(gotSeq), len(wantSeq))
		}
		for i := range wantSeq {
			if gotSeq[i] != wantSeq[i] {
				t.Fatalf("term %x posting %d %d != %d", key, i, gotSeq[i], wantSeq[i])
			}
		}
	}
	for u := int32(0); u < int32(idx.N); u++ {
		wantEntries := st.docEntriesOf(u)
		gotEntries, ok := gotDB.docEntriesOf(u)
		if !ok || len(gotEntries) != len(wantEntries) {
			t.Fatalf("unit %d entries ok=%v len=%d want %d", u, ok, len(gotEntries), len(wantEntries))
		}
		for i := range wantEntries {
			gotPos, ok := gotDB.positionsOf(u, gotEntries[i])
			if !ok {
				t.Fatalf("unit %d entry %d positions missing", u, i)
			}
			wantPos := st.entryPositionsOf(wantEntries[i])
			if len(gotPos) != len(wantPos) {
				t.Fatalf("unit %d entry %d positions %d != %d", u, i, len(gotPos), len(wantPos))
			}
			for j := range wantPos {
				if gotPos[j] != wantPos[j] {
					t.Fatalf("unit %d entry %d position %d %d != %d", u, i, j, gotPos[j], wantPos[j])
				}
			}
		}
		mass, ok := gotDB.docMassOf(idx, u)
		if !ok || math.Float64bits(mass) != math.Float64bits(idx.DocMass[u]) {
			t.Fatalf("unit %d docMass mismatch", u)
		}
	}
	t.Logf("block store round-trip: term blocks=%d doc blocks=%d units=%d", len(tb.blocks), len(db.blocks), idx.N)
}

func TestBlockStoreReloadBindsUnitIDsForTailRefresh(t *testing.T) {
	corpus := seg2dBlockCorpusWithPositions(96)
	idxHalf := BuildIndex(corpus[:48])
	stHalf := NewEngine(idxHalf).ensureClusterState()
	tbHalf := buildTermBlocks(idxHalf, 24)
	dbHalf := buildDocBlocksFromState(stHalf, idxHalf, 24)
	idxFull := BuildIndex(corpus)
	if tbHalf == nil || dbHalf == nil {
		t.Fatal("half block views are nil")
	}
	dir := t.TempDir()
	if _, err := appendBlockStore(dir, nil, tbHalf.blocks, dbHalf.blocks, tbHalf.n, tbHalf.gen); err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	gotTB, _, err := readBlockStore(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(gotTB.unitIDs) != 0 {
		t.Fatalf("reloaded term blocks unexpectedly carried %d unit IDs", len(gotTB.unitIDs))
	}
	if next, ok := gotTB.appendTail(idxFull, 24, corpus[48:], false); ok || next != nil {
		t.Fatal("unbound reloaded blocks must not pass the tail predicate")
	}
	bound := gotTB.withUnitIDs(idxHalf.UnitIDs)
	if bound == nil {
		t.Fatal("binding the matching unit-ID snapshot failed")
	}
	next, ok := bound.appendTail(idxFull, 24, corpus[48:], false)
	if !ok || next == nil {
		t.Fatal("bound reloaded blocks rejected a valid tail append")
	}
	if len(next.blocks) != len(tbHalf.blocks)+2 {
		t.Fatalf("reloaded tail blocks=%d want %d", len(next.blocks), len(tbHalf.blocks)+2)
	}
	for i := range gotTB.blocks {
		if next.blocks[i] != gotTB.blocks[i] {
			t.Fatalf("tail refresh replaced reloaded block %d", i)
		}
	}
}

func TestBlockStoreUsesOwnerOnlyPermissions(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(48))
	st := NewEngine(idx).ensureClusterState()
	tb := buildTermBlocks(idx, 24)
	db := buildDocBlocksFromState(st, idx, 24)
	if tb == nil || db == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen)
	if err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != blockStoreDir {
		t.Fatalf("store directory mode %04o != %04o", got, blockStoreDir)
	}
	for _, ref := range append(append([]blockFileRef(nil), manifest.Term...), manifest.Doc...) {
		info, err := os.Stat(filepath.Join(dir, ref.File))
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != blockStoreFile {
			t.Fatalf("block %s mode %04o != %04o", ref.File, got, blockStoreFile)
		}
	}
}

func TestBlockStoreSerializesConcurrentPublishers(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(48))
	st := NewEngine(idx).ensureClusterState()
	tb := buildTermBlocks(idx, 24)
	db := buildDocBlocksFromState(st, idx, 24)
	if tb == nil || db == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	entered := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		unlock, err := lockBlockStore(dir)
		if err == nil {
			close(entered)
			<-release
			unlock()
		}
		firstDone <- err
	}()
	<-entered
	secondDone := make(chan error, 1)
	go func() {
		_, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen)
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("second publisher finished while lock was held: %v", err)
	default:
	}
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first publisher lock: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("second publisher: %v", err)
	}
	if _, _, err := readBlockStore(dir); err != nil {
		t.Fatalf("read published store: %v", err)
	}
}

func TestBlockStoreRejectsStaleManifestPublication(t *testing.T) {
	corpus := seg2dBlockCorpusWithPositions(96)
	idxHalf := BuildIndex(corpus[:48])
	stHalf := NewEngine(idxHalf).ensureClusterState()
	tbHalf := buildTermBlocks(idxHalf, 24)
	dbHalf := buildDocBlocksFromState(stHalf, idxHalf, 24)
	idxFull := BuildIndex(corpus)
	stFull := NewEngine(idxFull).ensureClusterState()
	tbFull := buildTermBlocks(idxFull, 24)
	dbFull := buildDocBlocksFromState(stFull, idxFull, 24)
	if tbHalf == nil || dbHalf == nil || tbFull == nil || dbFull == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tbHalf.blocks, dbHalf.blocks, tbHalf.n, tbHalf.gen)
	if err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	if _, err := appendBlockStore(dir, manifest, tbFull.blocks[2:], dbFull.blocks[2:], tbFull.n, tbFull.gen); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if _, err := appendBlockStore(dir, manifest, tbFull.blocks[2:], dbFull.blocks[2:], tbFull.n, tbFull.gen); err == nil {
		t.Fatal("stale pre-append manifest unexpectedly published again")
	}
}

func TestBlockStoreRejectsRepeatedGenerationPublication(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(48))
	st := NewEngine(idx).ensureClusterState()
	tb := buildTermBlocks(idx, 24)
	db := buildDocBlocksFromState(st, idx, 24)
	if tb == nil || db == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen)
	if err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	old := make(map[string][]byte, len(manifest.Term)+len(manifest.Doc))
	for _, ref := range append(append([]blockFileRef(nil), manifest.Term...), manifest.Doc...) {
		data, err := os.ReadFile(filepath.Join(dir, ref.File))
		if err != nil {
			t.Fatal(err)
		}
		old[ref.File] = data
	}
	if _, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen); err == nil {
		t.Fatal("repeated full publication with the same generation succeeded")
	}
	for name, before := range old {
		after, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("repeated publication changed block size %s", name)
		}
		for i := range before {
			if after[i] != before[i] {
				t.Fatalf("repeated publication changed block %s at byte %d", name, i)
			}
		}
	}
}

func TestBlockStoreAppendReusesImmutableBlocks(t *testing.T) {
	corpus := seg2dBlockCorpusWithPositions(96)
	idxHalf := BuildIndex(corpus[:48])
	stHalf := NewEngine(idxHalf).ensureClusterState()
	tbHalf := buildTermBlocks(idxHalf, 24)
	dbHalf := buildDocBlocksFromState(stHalf, idxHalf, 24)
	idxFull := BuildIndex(corpus)
	stFull := NewEngine(idxFull).ensureClusterState()
	tbFull := buildTermBlocks(idxFull, 24)
	dbFull := buildDocBlocksFromState(stFull, idxFull, 24)
	if tbHalf == nil || dbHalf == nil || tbFull == nil || dbFull == nil {
		t.Fatal("block views are nil")
	}
	if len(tbHalf.blocks) != 2 || len(tbFull.blocks) != 4 {
		t.Fatalf("block counts half=%d full=%d", len(tbHalf.blocks), len(tbFull.blocks))
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tbHalf.blocks, dbHalf.blocks, tbHalf.n, tbHalf.gen)
	if err != nil {
		t.Fatalf("initial publish: %v", err)
	}
	old := map[string][]byte{}
	for _, ref := range append(append([]blockFileRef(nil), manifest.Term...), manifest.Doc...) {
		data, err := os.ReadFile(filepath.Join(dir, ref.File))
		if err != nil {
			t.Fatal(err)
		}
		old[ref.File] = data
	}
	if _, err := appendBlockStore(dir, manifest, tbFull.blocks[2:], dbFull.blocks[2:], tbFull.n, tbFull.gen); err != nil {
		t.Fatalf("append publish: %v", err)
	}
	for name, before := range old {
		after, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if len(after) != len(before) {
			t.Fatalf("append rewrote old block %s", name)
		}
		for i := range before {
			if after[i] != before[i] {
				t.Fatalf("append rewrote old block %s at byte %d", name, i)
			}
		}
	}
	gotTB, gotDB, err := readBlockStore(dir)
	if err != nil {
		t.Fatalf("read appended store: %v", err)
	}
	if len(gotTB.blocks) != 4 || len(gotDB.blocks) != 4 || gotTB.n != tbFull.n {
		t.Fatalf("appended store blocks term=%d doc=%d n=%d", len(gotTB.blocks), len(gotDB.blocks), gotTB.n)
	}
	for i := range tbFull.blocks {
		assertTermBlockEqual(t, gotTB.blocks[i], tbFull.blocks[i])
	}
	for i := range dbFull.blocks {
		assertDocBlockEqual(t, gotDB.blocks[i], dbFull.blocks[i])
	}
}

func TestBlockStoreRejectsManifestShapeAndPathTraversal(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(48))
	st := NewEngine(idx).ensureClusterState()
	tb := buildTermBlocks(idx, 24)
	db := buildDocBlocksFromState(st, idx, 24)
	if tb == nil || db == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen)
	if err != nil {
		t.Fatalf("write block store: %v", err)
	}
	if len(manifest.Term) < 2 || len(manifest.Doc) < 2 {
		t.Fatal("fixture needs two term and doc blocks")
	}
	badRange := *manifest
	badRange.Term = append([]blockFileRef(nil), manifest.Term...)
	badRange.Term[1].Lo++
	if err := writeBlockManifest(dir, &badRange); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBlockStore(dir); err == nil {
		t.Fatal("non-contiguous term manifest was accepted")
	}
	badPath := *manifest
	badPath.Doc = append([]blockFileRef(nil), manifest.Doc...)
	badPath.Doc[0].File = "../outside.bin"
	if err := writeBlockManifest(dir, &badPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBlockStore(dir); err == nil {
		t.Fatal("manifest path traversal was accepted")
	}
}

func TestBlockStoreDocMassAfterTransientRelease(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(48))
	eng := NewEngine(idx)
	st := eng.ensureClusterState()
	db := buildDocBlocksFromState(st, idx, 24)
	if db == nil {
		t.Fatal("doc blocks are nil")
	}
	want := append([]float64(nil), idx.DocMass...)
	idx.ReleaseTransientUnitMaps()
	for u := int32(0); u < int32(idx.N); u++ {
		got, ok := db.docMassOf(idx, u)
		if !ok || math.Float64bits(got) != math.Float64bits(want[u]) {
			t.Fatalf("unit %d docMass after release got=%016x ok=%v want=%016x", u, math.Float64bits(got), ok, math.Float64bits(want[u]))
		}
	}
}

func TestBlockStoreRejectsCorruption(t *testing.T) {
	idx := BuildIndex(seg2dBlockCorpusWithPositions(48))
	st := NewEngine(idx).ensureClusterState()
	tb := buildTermBlocks(idx, 24)
	db := buildDocBlocksFromState(st, idx, 24)
	if tb == nil || db == nil {
		t.Fatal("block views are nil")
	}
	dir := t.TempDir()
	manifest, err := appendBlockStore(dir, nil, tb.blocks, db.blocks, tb.n, tb.gen)
	if err != nil {
		t.Fatalf("write block store: %v", err)
	}
	target := filepath.Join(dir, manifest.Term[0].File)
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)/2] ^= 0xFF
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBlockStore(dir); err == nil {
		t.Fatal("corrupted block file was accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, blockManifestName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readBlockStore(dir); err == nil {
		t.Fatal("truncated manifest was accepted")
	}
}
