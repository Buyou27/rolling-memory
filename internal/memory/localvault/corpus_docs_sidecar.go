// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package localvault

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// The docs sidecar is the read-optimised metadata view that ships with a
// published cluster. It removes the O(N) canonical table scan from cold start
// and post-build snapshot construction; the change log keeps it current.
const corpusDocsSidecarSuffix = ".zcsr.docs"

type corpusDocsSidecarHeader struct {
	Version   int    `json:"v"`
	Signature string `json:"signature"`
	ChangeSeq int64  `json:"change_seq"`
	Count     int    `json:"count"`
}

type corpusDocSidecar struct {
	ID        string `json:"id"`
	Layer     string `json:"layer"`
	Occ       int64  `json:"occ"`
	DocHash   uint64 `json:"doc_hash"`
	BlockHash uint64 `json:"block_hash"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	AddrURI   string `json:"addr"`
	Coord64   uint64 `json:"coord64"`
	WaveBar   string `json:"wave"`
	Locus     string `json:"locus"`
	RefPtr    string `json:"ref"`
	DocLen    int    `json:"doc_len"`
}

func corpusDocsSidecarPath(dbPath string) string {
	return dbPath + corpusDocsSidecarSuffix
}

func sidecarFromDoc(doc vaultDocEntry) corpusDocSidecar {
	return corpusDocSidecar{
		ID:        doc.docID,
		Layer:     doc.layer,
		Occ:       doc.occ,
		DocHash:   doc.docHash,
		BlockHash: doc.blockHash,
		LineStart: doc.lineStart,
		LineEnd:   doc.lineEnd,
		AddrURI:   doc.addrURI,
		Coord64:   doc.coord64,
		WaveBar:   doc.waveBar,
		Locus:     doc.locus,
		RefPtr:    doc.refPointer,
		DocLen:    doc.docLen,
	}
}

func docFromSidecar(doc corpusDocSidecar) vaultDocEntry {
	docLen := doc.DocLen
	if docLen < 1 {
		docLen = 1
	}
	return vaultDocEntry{
		docID:      doc.ID,
		layer:      doc.Layer,
		occ:        doc.Occ,
		docHash:    doc.DocHash,
		blockHash:  doc.BlockHash,
		lineStart:  doc.LineStart,
		lineEnd:    doc.LineEnd,
		addrURI:    doc.AddrURI,
		coord64:    doc.Coord64,
		waveBar:    doc.WaveBar,
		locus:      doc.Locus,
		refPointer: doc.RefPtr,
		docLen:     docLen,
	}
}

// writeCorpusDocsSidecar publishes the docs view atomically next to the
// legacy cluster. A failure is non-fatal: the reader falls back to the
// canonical scan.
func writeCorpusDocsSidecar(dbPath, signature string, changeSeq int64, docs []vaultDocEntry) error {
	if strings.TrimSpace(dbPath) == "" {
		return nil
	}
	return writeCorpusDocsSidecarTo(corpusDocsSidecarPath(dbPath), signature, changeSeq, docs)
}

// writeCorpusDocsSidecarTo writes the docs view to an explicit path.
func writeCorpusDocsSidecarTo(path, signature string, changeSeq int64, docs []vaultDocEntry) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, ".zcsr.docs-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	writer := bufio.NewWriterSize(temp, 256*1024)
	header, err := json.Marshal(corpusDocsSidecarHeader{
		Version:   1,
		Signature: signature,
		ChangeSeq: changeSeq,
		Count:     len(docs),
	})
	if err == nil {
		_, err = writer.Write(append(header, '\n'))
	}
	if err == nil {
		encoder := json.NewEncoder(writer)
		for _, doc := range docs {
			if err = encoder.Encode(sidecarFromDoc(doc)); err != nil {
				break
			}
		}
	}
	if err == nil {
		err = writer.Flush()
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tempName)
		return err
	}
	if err := os.Rename(tempName, path); err != nil {
		_ = os.Remove(tempName)
		return err
	}
	return nil
}

// loadCorpusDocsSidecar reads the published docs view. The caller validates the
// codec and sequence against the live state; the sidecar may be older than the
// current state because the change log supplies everything after its watermark.
func loadCorpusDocsSidecar(dbPath string) ([]vaultDocEntry, corpusDocsSidecarHeader, bool) {
	var header corpusDocsSidecarHeader
	_, _, _, docsPath, _, ok := resolvePublishedSnapshotPaths(dbPath)
	if !ok {
		return nil, header, false
	}
	return loadCorpusDocsSidecarAt(docsPath)
}

// loadCorpusDocsSidecarAt reads a docs view from an explicit path.
func loadCorpusDocsSidecarAt(path string) ([]vaultDocEntry, corpusDocsSidecarHeader, bool) {
	var header corpusDocsSidecarHeader
	if strings.TrimSpace(path) == "" {
		return nil, header, false
	}
	handle, err := os.Open(path)
	if err != nil {
		return nil, header, false
	}
	defer handle.Close()
	reader := bufio.NewReaderSize(handle, 256*1024)
	headerLine, err := reader.ReadBytes('\n')
	if err != nil {
		return nil, header, false
	}
	if err := json.Unmarshal(headerLine, &header); err != nil || header.Version != 1 || header.Count < 0 {
		return nil, header, false
	}
	docs := make([]vaultDocEntry, 0, header.Count)
	decoder := json.NewDecoder(reader)
	for i := 0; i < header.Count; i++ {
		var doc corpusDocSidecar
		if err := decoder.Decode(&doc); err != nil {
			return nil, header, false
		}
		docs = append(docs, docFromSidecar(doc))
	}
	return docs, header, true
}

// corpusDocsSidecarMatches is a cheap probe used by tests and diagnostics.
func corpusDocsSidecarMatches(dbPath, expectedSignature string) bool {
	_, header, ok := loadCorpusDocsSidecar(dbPath)
	return ok && header.Signature == expectedSignature
}
