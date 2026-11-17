// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package rollingmemory

import (
	"strings"

	"github.com/ai-go-hub/ai-go-admin/internal/kit/httpx"
	"github.com/gin-gonic/gin"
)

// demoQuestion and its links are authored examples, not retrieval results.
type demoQuestion struct {
	ID        string   `json:"id"`
	Question  string   `json:"question"`
	Answer    string   `json:"answer"`
	NoeticKey string   `json:"noetic_key"`
	NoeticID  int      `json:"noetic_id"`
	CitedKeys []string `json:"cited_keys"`
	CitedIDs  []int    `json:"cited_ids"`
}

type demoGraphResponse struct {
	GraphQueryResponse
	SyntheticDemo bool           `json:"synthetic_demo"`
	Questions     []demoQuestion `json:"questions"`
}

type demoRecallHit struct {
	UnitID  string `json:"unit_id"`
	NodeKey string `json:"node_key"`
	NodeID  int    `json:"node_id"`
	Summary string `json:"summary"`
	RawText string `json:"raw_text"`
}

type demoRecallResponse struct {
	SyntheticDemo bool            `json:"synthetic_demo"`
	Query         string          `json:"q"`
	QuestionID    string          `json:"question_id"`
	Answer        string          `json:"answer"`
	SeedNodeID    int             `json:"seed_node_id"`
	SeedNodeKey   string          `json:"seed_node_key"`
	HitNodeIDs    []int           `json:"hit_node_ids"`
	Hits          []demoRecallHit `json:"hits"`
}

// syntheticDemoGraph is constructed entirely from fictional, authored examples.
func syntheticDemoGraph() demoGraphResponse {
	nodes := []GraphNodeItem{
		{NodeKey: "cell:demo-garden", Kind: "cell", Layer: "background", Summary: "The rooftop garden", Span: "A fictional rooftop garden shared by Mira and Teo."},
		{NodeKey: "unit:demo-plan", Kind: "unit", Layer: "background", Summary: "A garden for the neighborhood", Span: "Mira and Teo imagined a small garden on their library roof."},
		{NodeKey: "unit:demo-box", Kind: "unit", Layer: "episodic", Summary: "The blue seed box", Span: "Mira placed the seeds in a blue box beside the window."},
		{NodeKey: "unit:demo-water", Kind: "unit", Layer: "episodic", Summary: "Watering on Friday", Span: "Teo offered to water the garden every Friday."},
		{NodeKey: "unit:demo-weather", Kind: "unit", Layer: "episodic", Summary: "A rainy afternoon", Span: "Rain filled the garden's little water barrel."},
		{NodeKey: "cell:demo-festival", Kind: "cell", Layer: "background", Summary: "The lantern evening", Span: "A fictional evening gathering in the garden."},
		{NodeKey: "unit:demo-lantern", Kind: "unit", Layer: "episodic", Summary: "Paper lanterns", Span: "Teo made paper lanterns for the garden gathering."},
		{NodeKey: "unit:demo-picnic", Kind: "unit", Layer: "episodic", Summary: "A rooftop picnic", Span: "Mira brought pear cakes to the lantern evening."},
		{NodeKey: "unit:demo-q-box", Kind: "unit", Layer: "noetic", Summary: "Where were the seeds?", Span: "Q: Where did Mira put the seeds?\nA: In a blue box beside the window."},
		{NodeKey: "unit:demo-q-water", Kind: "unit", Layer: "noetic", Summary: "Who watered the garden?", Span: "Q: Who offered to water the garden?\nA: Teo, every Friday."},
		{NodeKey: "unit:demo-q-evening", Kind: "unit", Layer: "noetic", Summary: "What did Mira bring?", Span: "Q: What did Mira bring to the lantern evening?\nA: Pear cakes."},
	}
	byKey := make(map[string]*GraphNodeItem, len(nodes))
	for i := range nodes {
		nodes[i].ID = deterministicIntID(nodes[i].NodeKey)
		nodes[i].Anchor = nodes[i].NodeKey
		nodes[i].Status = "active"
		nodes[i].Aliases = []string{}
		nodes[i].Weight = 1
		byKey[nodes[i].NodeKey] = &nodes[i]
	}
	links := []struct{ from, to, relation string }{
		{"unit:demo-plan", "cell:demo-garden", "member_of"},
		{"unit:demo-box", "cell:demo-garden", "member_of"},
		{"unit:demo-water", "cell:demo-garden", "member_of"},
		{"unit:demo-weather", "cell:demo-garden", "member_of"},
		{"unit:demo-lantern", "cell:demo-festival", "member_of"},
		{"unit:demo-picnic", "cell:demo-festival", "member_of"},
		{"unit:demo-q-box", "unit:demo-box", "cites"},
		{"unit:demo-q-water", "unit:demo-water", "cites"},
		{"unit:demo-q-evening", "unit:demo-picnic", "cites"},
		{"cell:demo-garden", "cell:demo-festival", "weak_edge"},
	}
	edges := make([]GraphEdgeItem, 0, len(links))
	counts := make(map[string]int)
	for _, link := range links {
		from, to := byKey[link.from], byKey[link.to]
		from.Degree++
		to.Degree++
		counts[link.relation]++
		edges = append(edges, GraphEdgeItem{
			ID: len(edges) + 1, Assertion: link.from + "->" + link.to,
			From: from.ID, To: to.ID, FromKey: from.NodeKey, ToKey: to.NodeKey,
			FromLabel: from.Summary, ToLabel: to.Summary, Relation: link.relation,
			Layer: from.Layer, Status: "active", Weight: 1,
		})
	}
	question := func(id, text, answer, seed, cited string) demoQuestion {
		return demoQuestion{
			ID: id, Question: text, Answer: answer,
			NoeticKey: seed, NoeticID: byKey[seed].ID,
			CitedKeys: []string{cited}, CitedIDs: []int{byKey[cited].ID},
		}
	}
	questions := []demoQuestion{
		question("demo-box", "Where did Mira put the seeds?", "In a blue box beside the window.", "unit:demo-q-box", "unit:demo-box"),
		question("demo-water", "Who offered to water the garden?", "Teo, every Friday.", "unit:demo-q-water", "unit:demo-water"),
		question("demo-evening", "What did Mira bring to the lantern evening?", "Pear cakes.", "unit:demo-q-evening", "unit:demo-picnic"),
	}
	relations := make([]GraphRelationOption, 0, len(counts))
	for _, rel := range []string{"member_of", "cites", "weak_edge"} {
		relations = append(relations, GraphRelationOption{Relation: rel, Count: counts[rel], Classes: []GraphRelationClass{}})
	}
	return demoGraphResponse{
		GraphQueryResponse: GraphQueryResponse{
			Source: "synthetic_demo", ScopeKey: "synthetic_demo", Scopes: []GraphScopeItem{},
			Nodes: nodes, Edges: edges, Relations: relations,
			Stats: GraphStats{EventTotal: len(nodes), LinkTotal: len(edges), NodeShown: len(nodes), EdgeShown: len(edges)},
		},
		SyntheticDemo: true,
		Questions:     questions,
	}
}

func demoGraphHandler(c *gin.Context) {
	var req GraphQueryRequest
	if !bindJSON(c, &req) {
		return
	}
	graph := syntheticDemoGraph()
	limit := req.Limit
	if limit <= 0 || limit > len(graph.Nodes) {
		limit = len(graph.Nodes)
	}
	graph.Truncated.Nodes = limit < len(graph.Nodes)
	graph.Nodes = graph.Nodes[:limit]
	kept := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		kept[node.NodeKey] = true
	}
	edges := make([]GraphEdgeItem, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if kept[edge.FromKey] && kept[edge.ToKey] {
			edges = append(edges, edge)
		}
	}
	graph.Edges = edges
	graph.Stats.NodeShown, graph.Stats.EdgeShown = len(graph.Nodes), len(edges)
	httpx.Success(c, httpx.WithData(graph))
}

func demoExpandHandler(c *gin.Context) {
	var req GraphExpandRequest
	if !bindJSON(c, &req) {
		return
	}
	graph := syntheticDemoGraph()
	available := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		available[node.NodeKey] = true
	}
	visited := make(map[string]bool, len(graph.Nodes))
	frontier := make([]string, 0, len(graph.Nodes))
	for _, key := range req.SeedKeys {
		if key = strings.TrimSpace(key); available[key] && !visited[key] {
			visited[key] = true
			frontier = append(frontier, key)
		}
	}
	depth := req.Depth
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}
	for step := 0; step < depth && len(frontier) > 0; step++ {
		var next []string
		for _, key := range frontier {
			for _, edge := range graph.Edges {
				neighbor := ""
				if edge.FromKey == key {
					neighbor = edge.ToKey
				} else if edge.ToKey == key {
					neighbor = edge.FromKey
				}
				if neighbor != "" && !visited[neighbor] {
					visited[neighbor] = true
					next = append(next, neighbor)
				}
			}
		}
		frontier = next
	}
	nodes := make([]GraphNodeItem, 0, len(graph.Nodes))
	kept := make(map[string]bool)
	for _, node := range graph.Nodes {
		if !visited[node.NodeKey] {
			continue
		}
		if req.Limit > 0 && len(nodes) >= req.Limit {
			graph.Truncated.Nodes = true
			continue
		}
		nodes = append(nodes, node)
		kept[node.NodeKey] = true
	}
	edges := make([]GraphEdgeItem, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if kept[edge.FromKey] && kept[edge.ToKey] {
			edges = append(edges, edge)
		}
	}
	graph.Nodes, graph.Edges = nodes, edges
	graph.Stats.NodeShown, graph.Stats.EdgeShown = len(nodes), len(edges)
	httpx.Success(c, httpx.WithData(graph))
}

func demoRecallHandler(c *gin.Context) {
	var req struct {
		QuestionID string `json:"question_id"`
	}
	if !bindJSON(c, &req) {
		return
	}
	graph := syntheticDemoGraph()
	result := demoRecallResponse{SyntheticDemo: true, HitNodeIDs: []int{}, Hits: []demoRecallHit{}}
	for _, question := range graph.Questions {
		if question.ID != req.QuestionID {
			continue
		}
		result.Query, result.QuestionID, result.Answer = question.Question, question.ID, question.Answer
		result.SeedNodeID, result.SeedNodeKey = question.NoeticID, question.NoeticKey
		result.HitNodeIDs = append(result.HitNodeIDs, question.NoeticID)
		for _, node := range graph.Nodes {
			if node.NodeKey != question.CitedKeys[0] {
				continue
			}
			result.HitNodeIDs = append(result.HitNodeIDs, node.ID)
			result.Hits = append(result.Hits, demoRecallHit{
				UnitID: strings.TrimPrefix(node.NodeKey, "unit:"), NodeKey: node.NodeKey,
				NodeID: node.ID, Summary: node.Summary, RawText: node.Span,
			})
		}
		break
	}
	httpx.Success(c, httpx.WithData(result))
}
