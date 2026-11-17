// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

// Package llm provides the optional OpenAI-compatible query-hint client used
// only when legacy deep-recall hint expansion is explicitly requested.
package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rocareer/rolling-memory/internal/config"
	"github.com/rocareer/rolling-memory/internal/httpclient"
)

const querySystem = `You rewrite one memory query into short retrieval hints.
Return exactly one JSON object: {"s":"...","x":"...","o":"..."}.
Use the entity names, relation label and value that appear in the query's language.
Leave a field as an empty string when the query does not identify it.
Do not answer the question. Do not add facts. No markdown.`

// Extraction is retained for golden test harness compatibility.
type Extraction struct {
	Assertions   []map[string]any
	Aliases      []map[string]any
	Unresolved   []map[string]any
	AnchorFailed int
	Extractor    string
	Warning      string
}

// Extractor is the write-side extraction seam retained for golden test harness compatibility.
type Extractor interface {
	Extract(text, speaker string, timeRef int64) (Extraction, error)
	Name() string
}

// HintsProvider is the optional read-side query rewrite seam (deep recall).
type HintsProvider interface {
	QueryHints(cue string) (map[string]string, error)
}

// LLMError marks a failed model call.
type LLMError struct{ message string }

func (e *LLMError) Error() string { return e.message }

func llmErrorf(format string, args ...any) error {
	return &LLMError{message: fmt.Sprintf(format, args...)}
}

var (
	fenceStartRE = regexp.MustCompile(`^` + "```" + `[a-zA-Z0-9_-]*\s*`)
	fenceEndRE   = regexp.MustCompile(`\s*` + "```" + `$`)
)

// OpenAILLM is an optional chat-completions client for deep-recall query hints.
type OpenAILLM struct {
	config  *config.Config
	name    string
	headers map[string]string
	timeout time.Duration
}

// NewOpenAILLM builds the client and resolves the API key.
func NewOpenAILLM(cfg *config.Config) (*OpenAILLM, error) {
	key, err := cfg.LLMKey()
	if err != nil {
		return nil, err
	}
	if key == "" {
		return nil, configError("llm provider 'openai' requires an API key")
	}
	headers := httpclient.Bearer(key)
	timeout := time.Duration(cfg.LLM.TimeoutSec * float64(time.Second))
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &OpenAILLM{
		config:  cfg,
		name:    "openai:" + cfg.LLM.Model,
		headers: headers,
		timeout: timeout,
	}, nil
}

// Name returns the provider identifier.
func (o *OpenAILLM) Name() string { return o.name }

// Extract is a no-op stub satisfying Extractor for legacy callers.
func (o *OpenAILLM) Extract(text, speaker string, timeRef int64) (Extraction, error) {
	return Extraction{Extractor: o.name}, nil
}

// QueryHints is the one explicit LLM rewrite for recall(deep=true).
func (o *OpenAILLM) QueryHints(cue string) (map[string]string, error) {
	if o.config == nil || !strings.EqualFold(strings.TrimSpace(o.config.Mode), "cloud") {
		return nil, llmErrorf("local_plain recall blocks remote query rewriting")
	}
	prompt := strings.TrimSpace(cue)
	if prompt == "" {
		return map[string]string{}, nil
	}
	runes := []rune(prompt)
	if len(runes) > 2000 {
		prompt = string(runes[:2000])
	}
	parsed, err := o.chat(map[string]any{
		"messages": []map[string]any{
			{"role": "system", "content": querySystem},
			{"role": "user", "content": prompt},
		},
		"max_tokens": 200,
	})
	if err != nil {
		return nil, err
	}
	hints := map[string]string{}
	for _, key := range []string{"s", "x", "o"} {
		value := parsed[key]
		if item, ok := value.(map[string]any); ok {
			if label, ok := item["label"].(string); ok {
				value = label
			} else if literal, ok := item["literal"]; ok {
				value = literal
			}
		}
		if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
			hints[key] = strings.TrimSpace(text)
		}
	}
	return hints, nil
}

func (o *OpenAILLM) chat(body map[string]any) (map[string]any, error) {
	endpoint := strings.TrimRight(o.config.LLM.BaseURL, "/") + "/chat/completions"
	if err := o.config.CheckEgress(endpoint); err != nil {
		return nil, err
	}
	payload := map[string]any{
		"model":       o.config.LLM.Model,
		"temperature": 0,
	}
	for key, value := range body {
		payload[key] = value
	}
	if _, ok := payload["max_tokens"]; !ok {
		payload["max_tokens"] = o.config.LLM.MaxTokens
	}
	if _, ok := payload["response_format"]; !ok {
		payload["response_format"] = map[string]any{"type": "json_object"}
	}
	response, err := httpclient.PostJSON(endpoint, payload, o.headers, o.timeout, 1)
	if httpErr := (*httpclient.HTTPError)(nil); errors.As(err, &httpErr) &&
		httpErr.Status == 400 && strings.Contains(strings.ToLower(httpErr.Body), "response_format") {
		delete(payload, "response_format")
		response, err = httpclient.PostJSON(endpoint, payload, o.headers, o.timeout, 1)
	}
	if err != nil {
		return nil, llmErrorf("%s", err.Error())
	}
	choices, ok := response["choices"].([]any)
	if !ok || len(choices) == 0 {
		return nil, llmErrorf("LLM response has no choices")
	}
	first, _ := choices[0].(map[string]any)
	message, _ := first["message"].(map[string]any)
	content, _ := message["content"].(string)
	if strings.TrimSpace(content) == "" {
		return nil, llmErrorf("LLM response has no content")
	}
	parsed := ParseJSONObject(content)
	if parsed == nil {
		return nil, llmErrorf("LLM response is not a JSON object")
	}
	return parsed, nil
}

// BuildLLM returns (nil, warning) when no model is configured or reachable.
func BuildLLM(cfg *config.Config) (Extractor, *string) {
	if cfg.LLM.Provider == "none" {
		return nil, nil
	}
	client, err := NewOpenAILLM(cfg)
	if err != nil {
		message := "LLM unavailable, using local rule extractor: " + err.Error()
		return nil, &message
	}
	return client, nil
}

// ParseJSONObject strips code fences and decodes the first JSON object.
func ParseJSONObject(content string) map[string]any {
	text := strings.TrimSpace(content)
	if strings.HasPrefix(text, "```") {
		text = fenceStartRE.ReplaceAllString(text, "")
		text = fenceEndRE.ReplaceAllString(text, "")
	}
	parsed := map[string]any{}
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		return parsed
	}
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil
	}
	parsed = map[string]any{}
	if err := json.Unmarshal([]byte(text[start:end+1]), &parsed); err != nil {
		return nil
	}
	return parsed
}

func configError(message string) error {
	return fmt.Errorf("%s", message)
}
