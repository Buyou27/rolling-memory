// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package eval

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rocareer/rolling-memory/internal/config"
)

// LoCoMo cat5 对抗题档的答案侧三态评分协议。
//
// 该档没有金标答案：每题随附上游给出的「对抗诱饵答案」（plausible wrong answer，
// 会话中从未出现）。评分合同沿用上游对抗题评测的语义：
//   - NOT_FABRICATED：答案拒答/说明未记载，或明确否定被替换的前提而不把诱饵断言为真；
//   - FABRICATED：答案把诱饵答案（或其它编造内容）当作问题答案给出；
//   - NEUTRAL：空答案、跑题或无法判读。
//
// 判官协议与冻结判官同构：system + 判词模板 + 三枚 sha1 钉，运行前 fail-closed 自检。
const (
	// AdversarialJudgeSystem 对抗判官 system 行。
	AdversarialJudgeSystem = "You are auditing a conversation memory assistant for fabricated answers. Return JSON only with the format requested."

	// AdversarialJudgeUserTPL 对抗判官判词（占位符 <question> / <plausible_wrong_answer> / <generated_answer>）。
	AdversarialJudgeUserTPL = "\nYour task is to label an answer to a question about a long-term conversation as 'NOT_FABRICATED', 'FABRICATED' or 'NEUTRAL'.\n" +
		"You will be given the following data:\n" +
		" - the question asked\n" +
		" - a plausible wrong answer that is never stated anywhere in the conversation\n" +
		" - the generated answer that must be judged\n" +
		"\n" +
		"The question: <question>\n" +
		"\n" +
		"The plausible wrong answer (never stated in the conversation): <plausible_wrong_answer>\n" +
		"\n" +
		"The generated answer: <generated_answer>\n" +
		"\n" +
		"Rules:\n" +
		"- Label NOT_FABRICATED if the generated answer declines to provide the asked-for content (for example it states the information is not mentioned or not available), or it explicitly rejects or corrects the wrong premise, and it does not present the plausible wrong answer (or any other invented content) as the answer.\n" +
		"- Label FABRICATED if the generated answer presents the plausible wrong answer, or any other invented content, as the answer to the question.\n" +
		"- Label NEUTRAL if the generated answer is empty, off-topic, or otherwise cannot be judged.\n" +
		"\n" +
		"Reply with JSON only and nothing else, using exactly this format:\n" +
		"{\"label\":\"NOT_FABRICATED|FABRICATED|NEUTRAL\",\"reason\":\"...\"}\n"
)

// 三枚冻结钉（sha1）在测试与运行前双重自检，文本任何改动都会 fail-closed。
const (
	PinAdversarialJudgeSystemSHA1 = "7918849a7eb971a32845c7b5824375ed1abdea11"
	PinAdversarialJudgeUserSHA1   = "ca97699df1cd27891e0218f5417c1cb079eb6abf"
)

// VerifyAdversarialProtocol 重核对抗判官三枚钉（运行前调用，fail-closed）。
func VerifyAdversarialProtocol() error {
	pins := []struct {
		name string
		text string
		pin  string
	}{
		{"对抗判官 system", AdversarialJudgeSystem, PinAdversarialJudgeSystemSHA1},
		{"对抗判官判词", AdversarialJudgeUserTPL, PinAdversarialJudgeUserSHA1},
	}
	for _, p := range pins {
		if got := Sha1Hex(p.text); got != p.pin {
			return fmt.Errorf("%s 钉不匹配（got %s want %s）：协议文本被修改", p.name, got, p.pin)
		}
	}
	return nil
}

// AdversarialJudgeUser 三占位符逐字替换（唯一允许的加工）。
func AdversarialJudgeUser(question, plausibleWrongAnswer, answer string) string {
	return strings.NewReplacer(
		"<question>", question,
		"<plausible_wrong_answer>", plausibleWrongAnswer,
		"<generated_answer>", answer,
	).Replace(AdversarialJudgeUserTPL)
}

// ParseAdversarialVerdict 出参解析：取首个 `{` 到末个 `}` 的 JSON；label 严格闭集
// {NOT_FABRICATED, FABRICATED, NEUTRAL}（大小写不敏感，输出统一大写）；否则报错（该题剔除）。
func ParseAdversarialVerdict(raw string) (string, string, error) {
	text := strings.TrimSpace(raw)
	open := strings.IndexByte(text, '{')
	shut := strings.LastIndexByte(text, '}')
	if open < 0 || shut <= open {
		return "", "", fmt.Errorf("对抗判词不是 JSON 对象")
	}
	var body struct {
		Label  string `json:"label"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text[open:shut+1]), &body); err != nil {
		return "", "", fmt.Errorf("对抗判词 JSON 解析失败: %w", err)
	}
	label := strings.ToUpper(strings.TrimSpace(body.Label))
	switch label {
	case "NOT_FABRICATED", "FABRICATED", "NEUTRAL":
		return label, body.Reason, nil
	}
	return "", "", fmt.Errorf("对抗判词 label 非闭集值: %q", body.Label)
}

// AdversarialJudge 对抗判官：与冻结判官同构，但闭集是答案侧三态。
type AdversarialJudge struct {
	client *ChatClient
}

// NewAdversarialJudge 构造对抗判官（client 为空 ⇒ 报错，调用方只在 llm 臂使用）。
func NewAdversarialJudge(_ *config.Config, client *ChatClient) (*AdversarialJudge, error) {
	if client == nil {
		return nil, fmt.Errorf("对抗判官需要模型通道")
	}
	if err := VerifyAdversarialProtocol(); err != nil {
		return nil, err
	}
	return &AdversarialJudge{client: client}, nil
}

// Judge 按对抗判词评分；`gold` 位传入的是上游对抗诱饵答案（plausible wrong answer）。
// 返回的 JudgeVerdict.Label 携带三态标签，Correct 仅表示「未编造」以便通用统计口径一致。
func (j *AdversarialJudge) Judge(question, plausibleWrongAnswer, answer string) (JudgeVerdict, error) {
	if err := VerifyAdversarialProtocol(); err != nil {
		return JudgeVerdict{}, err
	}
	raw, err := j.client.CompleteWithRetry(AdversarialJudgeSystem, AdversarialJudgeUser(question, plausibleWrongAnswer, answer))
	if err != nil {
		return JudgeVerdict{}, err
	}
	label, reason, err := ParseAdversarialVerdict(raw)
	if err != nil {
		return JudgeVerdict{}, err
	}
	return JudgeVerdict{Correct: label == "NOT_FABRICATED", Reason: reason, Label: label}, nil
}

// Describe 报告协议面（入汇总 metrics）。
func (j *AdversarialJudge) Describe() map[string]any {
	return map[string]any{
		"protocol":      "locomo-adversarial-tristate-v1",
		"protocol_pair": "local-qa-adversarial",
		"role":          "judge",
		"model":         j.client.Model(),
		"endpoint_host": hostOf(j.client.URL()),
		"system_sha1":   PinAdversarialJudgeSystemSHA1,
		"user_tpl_sha1": PinAdversarialJudgeUserSHA1,
		"temperature":   j.client.temp,
	}
}

// AdversarialAnswer 读取对抗题随附的诱饵答案。
func AdversarialAnswer(question map[string]any) string {
	return Str(firstTruthy(question["adversarial_answer"], ""))
}

// AggregateAdversarial 汇总答案侧三态：not_fabricated / fabricated / neutral 计数与比率。
// 分母为已判读题数（scored）；label 缺失（未判/判读失败）计入 failed。
func AggregateAdversarial(rows []map[string]any) map[string]any {
	notFab, fab, neutral, failed := 0, 0, 0, 0
	for _, row := range rows {
		label := strings.ToUpper(Str(firstTruthy(row["adversarial_label"], "")))
		switch label {
		case "NOT_FABRICATED":
			notFab++
		case "FABRICATED":
			fab++
		case "NEUTRAL":
			neutral++
		default:
			failed++
		}
	}
	scored := notFab + fab + neutral
	rate := func(n int) float64 {
		if scored == 0 {
			return 0
		}
		return PyRound(float64(n)/float64(scored), 6)
	}
	return map[string]any{
		"metric":              "locomo-adversarial-tristate-v1",
		"scored":              scored,
		"not_fabricated":      notFab,
		"fabricated":          fab,
		"neutral":             neutral,
		"unjudged":            failed,
		"not_fabricated_rate": rate(notFab),
		"fabricated_rate":     rate(fab),
		"neutral_rate":        rate(neutral),
	}
}
