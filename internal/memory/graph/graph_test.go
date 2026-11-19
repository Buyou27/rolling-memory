// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package graph_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rocareer/rolling-memory/internal/graphmodel"
	"github.com/rocareer/rolling-memory/internal/memory/graph"
	"github.com/rocareer/rolling-memory/internal/memory/thinclient"
	_ "modernc.org/sqlite"
)

func TestGraphViewProjectsWordsPhrasesClustersAndCitations(t *testing.T) {
	db, err := sql.Open("sqlite", "file:graph_projection_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	secret := []byte("0123456789abcdef0123456789abcdef")
	vault, err := thinclient.NewVault(db, secret)
	if err != nil {
		t.Fatal(err)
	}
	vault.SetIdentity(thinclient.VaultIdentity{Locus: "test"})
	const alphaText = "Alpha project graph memory units."
	for i, id := range []string{"ep_a1", "ep_a2", "ep_a3", "ep_a4", "ep_a5", "ep_a6"} {
		if err := vault.SaveLocalPlaintext(id, 1700000000+int64(i)*100, alphaText, nil, thinclient.IngestResponse{
			Layer:      "episodic",
			AddressURI: "C6:L1:LOCAL#" + id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := vault.SaveLocalPlaintext("ep_free", 0, "", nil, thinclient.IngestResponse{Layer: "episodic"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO rm_noetic(noetic_id, occurred_at, inserted_at, status, query_text, answer_text, cited_unit_ids_json)
		VALUES ('nt_alpha', 1700000600, 1700000600, 'active', 'What does the Alpha project use?', 'Graph memory units.', '["ep_a1","ep_a2"]')`); err != nil {
		t.Fatal(err)
	}

	builder := graph.NewBuilder(vault)
	resp, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 256})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GraphSchemaVersion != graphmodel.GraphSchemaVersion || resp.Source != "local-vault" {
		t.Fatalf("unexpected envelope: %+v", resp)
	}
	byKey := map[string]graphmodel.Node{}
	for _, node := range resp.Nodes {
		byKey[node.NodeKey] = node
	}
	if _, ok := byKey["unit:ep_a1"]; !ok {
		t.Fatalf("missing alpha unit: %d nodes", len(resp.Nodes))
	}
	if _, ok := byKey["unit:nt_alpha"]; !ok {
		t.Fatalf("missing noetic unit: %d nodes", len(resp.Nodes))
	}
	if node, ok := byKey["unit:ep_free"]; !ok || node.Kind != graphmodel.KindFree {
		t.Fatalf("empty unit must be free: %+v", node)
	}

	var episodeCluster, entityCluster string
	var wordNode, phraseNode string
	for _, node := range resp.Nodes {
		switch {
		case node.Kind == graphmodel.KindCluster && node.ClusterType == graphmodel.ClusterEpisode:
			episodeCluster = node.NodeKey
		case node.Kind == graphmodel.KindCluster && node.ClusterType == graphmodel.ClusterEntity:
			entityCluster = node.NodeKey
		case node.Kind == graphmodel.KindWord && wordNode == "" && node.Label != "" && !strings.HasPrefix(node.Label, "词族"):
			wordNode = node.NodeKey
		case node.Kind == graphmodel.KindPhrase && phraseNode == "":
			phraseNode = node.NodeKey
		}
	}
	if episodeCluster == "" {
		t.Fatalf("expected an episode cluster, nodes=%d", len(resp.Nodes))
	}
	if entityCluster == "" {
		t.Fatalf("expected an entity cluster, nodes=%d", len(resp.Nodes))
	}
	if wordNode == "" {
		t.Fatalf("expected a labelled word node, nodes=%d", len(resp.Nodes))
	}
	if phraseNode == "" {
		t.Fatalf("expected a phrase node, nodes=%d", len(resp.Nodes))
	}
	if resp.Stats.KindCounts.Clusters == 0 || resp.Stats.KindCounts.Words == 0 || resp.Stats.KindCounts.Phrases == 0 {
		t.Fatalf("kind counts incomplete: %+v", resp.Stats.KindCounts)
	}
	if resp.Stats.ClusterTypeCounts.Episode == 0 || resp.Stats.ClusterTypeCounts.Entity == 0 {
		t.Fatalf("cluster type counts incomplete: %+v", resp.Stats.ClusterTypeCounts)
	}
	var cites, memberOf, wordOf, phraseOf bool
	for _, edge := range resp.Edges {
		switch edge.Relation {
		case graphmodel.RelationCites:
			if edge.FromKey == "unit:nt_alpha" && (edge.ToKey == "unit:ep_a1" || edge.ToKey == "unit:ep_a2") {
				cites = true
			}
		case graphmodel.RelationMemberOf:
			memberOf = true
		case graphmodel.RelationWordOf:
			wordOf = true
		case graphmodel.RelationPhraseOf:
			phraseOf = true
		}
	}
	if !cites || !memberOf || !wordOf || !phraseOf {
		t.Fatalf("missing expected edges: cites=%v member_of=%v word_of=%v phrase_of=%v", cites, memberOf, wordOf, phraseOf)
	}
}

func TestGraphViewLabelsCJKSurfaces(t *testing.T) {
	db, err := sql.Open("sqlite", "file:graph_cjk_label_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	const cjkText = "记忆图谱使用词族和短语连接记忆单元。"
	for i, id := range []string{"ep_cjk_1", "ep_cjk_2", "ep_cjk_3"} {
		if err := vault.SaveLocalPlaintext(id, 1700000000+int64(i)*100, cjkText, nil, thinclient.IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatal(err)
		}
	}
	builder := graph.NewBuilder(vault)
	resp, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 128})
	if err != nil {
		t.Fatal(err)
	}
	labeled := 0
	for _, node := range resp.Nodes {
		if node.Kind == graphmodel.KindWord && !strings.HasPrefix(node.Label, "词族") {
			labeled++
		}
	}
	if labeled == 0 || resp.Stats.LabelCoveragePct == 0 {
		t.Fatalf("CJK word surfaces must be recovered, labeled=%d coverage=%.2f", labeled, resp.Stats.LabelCoveragePct)
	}
}

func TestGraphViewGroupsChunksBySourceProvenance(t *testing.T) {
	db, err := sql.Open("sqlite", "file:graph_source_cluster_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pathA := filepath.Join(dir, "source-a.md")
	pathB := filepath.Join(dir, "source-b.md")
	if err := os.WriteFile(pathA, []byte(strings.Repeat("alpha source line\n", 4000)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, []byte(strings.Repeat("beta other line\n", 5)), 0o600); err != nil {
		t.Fatal(err)
	}
	client := thinclient.NewLocalClient(vault)
	if _, err := client.AddBackgroundDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SyncBackgroundDirs(context.Background()); err != nil {
		t.Fatal(err)
	}
	builder := graph.NewBuilder(vault)
	rows, err := vault.GraphUnits(context.Background())
	if err != nil || len(rows) == 0 {
		t.Fatalf("graph rows = %d, err=%v", len(rows), err)
	}
	resp, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 64})
	if err != nil {
		t.Fatal(err)
	}
	var sourceClusters []graphmodel.Node
	for _, node := range resp.Nodes {
		if node.Kind == graphmodel.KindCluster && node.ClusterType == graphmodel.ClusterSource {
			sourceClusters = append(sourceClusters, node)
		}
	}
	if len(sourceClusters) != 2 {
		t.Fatalf("expected two source clusters (one per DocHash), got %d", len(sourceClusters))
	}
	var grouped bool
	for _, node := range sourceClusters {
		if node.MemberCount > 1 {
			grouped = true
		}
	}
	if !grouped {
		t.Fatalf("chunks sharing one DocHash must land in one source cluster: %+v", sourceClusters)
	}
	if resp.Stats.ClusterTypeCounts.Source != 2 {
		t.Fatalf("source cluster count = %d, want 2", resp.Stats.ClusterTypeCounts.Source)
	}
}

func TestGraphViewIsDeterministic(t *testing.T) {
	db, err := sql.Open("sqlite", "file:graph_determinism_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	vault, err := thinclient.NewVault(db, []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	for i, doc := range []struct{ id, text string }{
		{"ep_a", "Graph words and phrases repeat in memory."},
		{"ep_b", "Graph words and phrases repeat in clusters."},
	} {
		if err := vault.SaveLocalPlaintext(doc.id, 1700000000+int64(i)*100, doc.text, nil, thinclient.IngestResponse{Layer: "episodic"}); err != nil {
			t.Fatal(err)
		}
	}
	builder := graph.NewBuilder(vault)
	first, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 256})
	if err != nil {
		t.Fatal(err)
	}
	second, err := builder.View(context.Background(), graphmodel.ViewRequest{Limit: 256})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Nodes) != len(second.Nodes) || len(first.Edges) != len(second.Edges) {
		t.Fatalf("view sizes diverged: %d/%d vs %d/%d", len(first.Nodes), len(first.Edges), len(second.Nodes), len(second.Edges))
	}
	for i := range first.Nodes {
		if first.Nodes[i].NodeKey != second.Nodes[i].NodeKey || first.Nodes[i].ID != second.Nodes[i].ID {
			t.Fatalf("node order/id diverged at %d: %+v vs %+v", i, first.Nodes[i], second.Nodes[i])
		}
	}
	for i := range first.Edges {
		if first.Edges[i].Assertion != second.Edges[i].Assertion || first.Edges[i].ID != second.Edges[i].ID {
			t.Fatalf("edge order/id diverged at %d: %+v vs %+v", i, first.Edges[i], second.Edges[i])
		}
	}
}
