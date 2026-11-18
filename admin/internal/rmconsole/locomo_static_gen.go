// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LocomoDataDump 用于固化首屏 3D 图谱所需的完整计算状态，无需连接 RB 后端即可本地展示与召回。
type LocomoDataDump struct {
	Fingerprint string                      `json:"fingerprint"`
	ActiveScope string                      `json:"active_scope"`
	Units       map[string]locomoVaultEntry `json:"units"`
	Nodes       []GraphNodeItem             `json:"nodes"`
	Edges       []GraphEdgeItem             `json:"edges"`
	Questions   []LocomoPublicQuestion      `json:"questions"`
	AtomTotal   int                         `json:"atom_total"`
	MolTotal    int                         `json:"mol_total"`
	DimerTotal  int                         `json:"dimer_total"`
	TopAtoms    []GraphAtomParticle         `json:"top_atoms"`
	TopMols     []GraphMoleculeFragment     `json:"top_mols"`
}

func resolveLocomoDemoSnapshotPaths(home string) []string {
	var paths []string
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(wd, "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
			filepath.Join(wd, "testdata", "locomo_home_snapshot.json"),
			filepath.Join(wd, "admin", "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
		)
	}
	if home != "" {
		paths = append(paths,
			filepath.Join(home, "Rolling-Memory", "admin", "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
			filepath.Join(home, ".local", "share", "rolling-memory", "admin", "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
		)
	}
	for _, path := range paths {
		if filepath.Base(path) == "locomo_home_snapshot.json" {
			paths = append(paths, path+".gz")
		}
	}
	return paths
}

func resolveLocomoSnapshotPaths(home string) []string {
	var paths []string
	if wd, err := os.Getwd(); err == nil {
		paths = append(paths,
			filepath.Join(wd, "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
			filepath.Join(wd, "testdata", "locomo_home_snapshot.json"),
			filepath.Join(wd, "admin", "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
		)
	}
	if home != "" {
		paths = append(paths,
			filepath.Join(home, "Rolling-Memory", "admin", "internal", "rmconsole", "testdata", "locomo_home_snapshot.json"),
			filepath.Join(home, ".local", "state", "rolling-memory", "eval", "locomo-static-snapshot.json"),
		)
	}
	// Preserve the existing JSON search order before trying the private gzip snapshot.
	for _, path := range paths {
		if filepath.Base(path) == "locomo_home_snapshot.json" {
			paths = append(paths, path+".gz")
		}
	}
	return paths
}

func readLocomoSnapshot(path string) (*LocomoDataDump, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var r io.Reader = f
	var zr *gzip.Reader
	if strings.HasSuffix(path, ".gz") {
		zr, err = gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		r = zr
	}
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	var dump LocomoDataDump
	if err := json.Unmarshal(raw, &dump); err != nil {
		return nil, err
	}
	return &dump, nil
}

// GenerateLocomoStaticSnapshotTo writes a snapshot to a new file.
func GenerateLocomoStaticSnapshotTo(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("snapshot output path required")
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("create snapshot: %w", os.ErrExist)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect snapshot output: %w", err)
	}
	home, _ := os.UserHomeDir()
	vaultPath := resolveLocomoVaultPath(home)
	fp := computeLocomoCorpusFingerprint(home, vaultPath)

	corpus, err := loadLocomoCorpusWithSnapshots(false)
	if err != nil {
		return err
	}

	var nodes []GraphNodeItem
	for _, k := range corpus.keyOrder {
		if n := corpus.nodesByKey[k]; n != nil {
			nodes = append(nodes, *n)
		}
	}

	dump := LocomoDataDump{
		Fingerprint: fp,
		ActiveScope: corpus.activeScope,
		Units:       corpus.units,
		Nodes:       nodes,
		Edges:       corpus.rawEdges,
		Questions:   corpus.questions,
		AtomTotal:   corpus.atomTotal,
		MolTotal:    corpus.molTotal,
		DimerTotal:  corpus.dimerTotal,
		TopAtoms:    corpus.topAtoms,
		TopMols:     corpus.topMols,
	}

	b, err := json.Marshal(dump)
	if err != nil {
		return err
	}

	return writeNewLocomoSnapshot(path, b)
}

func writeNewLocomoSnapshot(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	var written int
	if strings.HasSuffix(path, ".gz") {
		zw := gzip.NewWriter(f)
		written, err = zw.Write(raw)
		if closeErr := zw.Close(); err == nil {
			err = closeErr
		}
	} else {
		written, err = f.Write(raw)
	}
	if err == nil && written != len(raw) {
		err = io.ErrShortWrite
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

func buildCorpusFromSnapshot(dump *LocomoDataDump) *locomoCachedCorpus {
	units := dump.Units
	if units == nil {
		units = make(map[string]locomoVaultEntry)
	}
	nodesByKey := make(map[string]*GraphNodeItem, len(dump.Nodes))
	keyOrder := make([]string, 0, len(dump.Nodes))
	unitOrder := make([]string, 0, len(units))

	for i := range dump.Nodes {
		n := dump.Nodes[i]
		nodesByKey[n.NodeKey] = &n
		keyOrder = append(keyOrder, n.NodeKey)
		if n.Kind == "unit" && n.Layer != "noetic" {
			uid := strings.TrimPrefix(n.NodeKey, "unit:")
			if _, ok := units[uid]; !ok {
				units[uid] = locomoVaultEntry{
					DocID:      uid,
					RawText:    n.Span,
					AddressURI: n.Anchor,
				}
			}
		}
	}
	for k := range units {
		unitOrder = append(unitOrder, k)
	}
	sort.Strings(unitOrder)

	cellMembers := make(map[string][]*GraphNodeItem)
	unitCells := make(map[string][]string)
	citesTargets := make(map[string][]string)
	seenPair := make(map[string]bool)

	for _, e := range dump.Edges {
		switch e.Relation {
		case "member_of":
			uNode := nodesByKey[e.FromKey]
			cNode := nodesByKey[e.ToKey]
			if uNode != nil && uNode.Kind == "unit" && cNode != nil && cNode.Kind == "cell" {
				cellMembers[cNode.NodeKey] = append(cellMembers[cNode.NodeKey], uNode)
				pk := uNode.NodeKey + "|" + cNode.NodeKey
				if !seenPair[pk] {
					seenPair[pk] = true
					unitCells[uNode.NodeKey] = append(unitCells[uNode.NodeKey], cNode.NodeKey)
				}
			}
		case "cites":
			citesTargets[e.FromKey] = append(citesTargets[e.FromKey], e.ToKey)
		}
	}

	atomTotal, molTotal, dimerTotal := dump.AtomTotal, dump.MolTotal, dump.DimerTotal
	topAtoms, topMols := dump.TopAtoms, dump.TopMols
	if atomTotal == 0 && molTotal == 0 {
		atomTotal, molTotal, dimerTotal, topAtoms, topMols = enrichGraphWithAtomsAndMolecules(nodesByKey, keyOrder, cellMembers, unitCells)
	}

	return &locomoCachedCorpus{
		loadedAt:     time.Now(),
		fingerprint:  dump.Fingerprint,
		activeScope:  dump.ActiveScope,
		units:        units,
		unitOrder:    unitOrder,
		questions:    dump.Questions,
		nodesByKey:   nodesByKey,
		keyOrder:     keyOrder,
		rawEdges:     dump.Edges,
		unitCells:    unitCells,
		cellMembers:  cellMembers,
		citesTargets: citesTargets,
		totalCells:   len(cellMembers),
		atomTotal:    atomTotal,
		molTotal:     molTotal,
		dimerTotal:   dimerTotal,
		topAtoms:     topAtoms,
		topMols:      topMols,
	}
}
