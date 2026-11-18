// plugin: rolling-memory v0.0.58
package rollingmemory

import (
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestGraphHandlerRelaysEngineProjection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ROLLING_MEMORY_BIN", os.Args[0])
	previous := engineCommandRunner
	engineCommandRunner = func(_ context.Context, _ string, op string, _ []string, _ []byte) ([]byte, error) {
		if op != "graph" {
			t.Fatalf("unexpected engine op %q", op)
		}
		return []byte(`{
			"graph_schema_version":2,
			"source":"local-vault",
			"scope_key":"default",
			"scopes":[],
			"nodes":[
				{"id":1,"node_key":"unit:ep_1","summary":"unit","kind":"unit","layer":"episodic","free":false,"primary_cluster_key":"cluster:episode:1","degree":1,"weight":1,"certainty":"confirmed (structural)","intention":"","status":"active","confidence":1,"observed_at":"","valid_from":"","valid_to":"","anchor":"","span":""},
				{"id":2,"node_key":"cluster:episode:1","summary":"episode","kind":"cluster","layer":"episodic","cluster_type":"episode","member_count":2,"energy_nats":1.5,"degree":1,"weight":2,"certainty":"confirmed (structural)","intention":"","status":"active","confidence":1,"observed_at":"","valid_from":"","valid_to":"","anchor":"","span":""},
				{"id":3,"node_key":"word:0000000000000001","summary":"alpha","kind":"word","layer":"mixed","degree":1,"weight":2,"certainty":"confirmed (structural)","intention":"","status":"active","confidence":1,"observed_at":"","valid_from":"","valid_to":"","anchor":"","span":""}
			],
			"edges":[
				{"id":1,"assertion_id":"unit:ep_1|member_of|cluster:episode:1","from":1,"to":2,"from_key":"unit:ep_1","to_key":"cluster:episode:1","from_label":"unit","to_label":"episode","relation":"member_of","layer":"episodic","warrant":"primary","certainty":"confirmed","confidence":1,"status":"active","observed_at":"","valid_from":"","anchor":"","span":"","weight":1}
			],
			"relations":[{"relation":"member_of","count":1,"classes":[]}],
			"stats":{
				"event_total":1,"link_total":1,"node_shown":3,"edge_shown":1,"isolated_shown":0,
				"avg_entity_degree":1,"multi_edge_entity_ratio":0,"avg_relation_reuse":1,"object_node_ratio":100,
				"kind_counts":{"units":1,"words":1,"phrases":0,"clusters":1,"free_units":0},
				"cluster_type_counts":{"episode":1,"entity":0,"source":0},
				"word_total":1,"phrase_total":0,"cluster_total":1,"free_unit_total":0
			},
			"truncated":{"nodes":false,"edges":false}
		}`), nil
	}
	t.Cleanup(func() { engineCommandRunner = previous })

	raw := callPluginHandler(t, graphHandler, "/graph", `{"scope":"default","limit":16}`)
	var envelope struct {
		Code int                `json:"code"`
		Data GraphQueryResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("decode graph envelope: %v (%s)", err, raw)
	}
	if envelope.Code != 0 {
		t.Fatalf("graph handler failed: %s", raw)
	}
	if envelope.Data.GraphSchemaVersion != 2 || envelope.Data.Source != "local-vault" {
		t.Fatalf("graph envelope = %+v", envelope.Data)
	}
	if len(envelope.Data.Nodes) != 3 || len(envelope.Data.Edges) != 1 {
		t.Fatalf("graph payload sizes = %d/%d", len(envelope.Data.Nodes), len(envelope.Data.Edges))
	}
	var cluster *GraphNodeItem
	for i := range envelope.Data.Nodes {
		if envelope.Data.Nodes[i].Kind == "cluster" {
			cluster = &envelope.Data.Nodes[i]
		}
	}
	if cluster == nil || cluster.ClusterType != "episode" || cluster.MemberCount != 2 {
		t.Fatalf("cluster node lost engine fields: %+v", cluster)
	}
	if envelope.Data.Stats.KindCounts.Clusters != 1 || envelope.Data.Stats.ClusterTypeCounts.Episode != 1 {
		t.Fatalf("stats lost cluster counters: %+v", envelope.Data.Stats)
	}
}
