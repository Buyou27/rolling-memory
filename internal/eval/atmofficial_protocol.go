package eval

// ATM-Bench official protocol pins and verification.
//
// The official evaluator is an external, published metric contract. This file
// pins every moving part that can silently change the reported QS:
//   1. the two prompt constants already pinned by VerifyATMProtocol;
//   2. the upstream evaluator source files (sha1, informational provenance);
//   3. the historical scoring and answer-payload fixture hashes (provenance);
//   4. the SGM corpus and the two official question tiers (sha256).
//
// Fixture data is not part of the runtime. Scorer tests use synthetic inputs.

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

const (
	// ATMOfficialScoringProtocol is the official ATM-Bench protocol id.
	ATMOfficialScoringProtocol = "atmbench-official-v1"
	// ATMOfficialScoringVersion is the upstream evaluator revision this port was
	// taken from (fetch date; upstream has no semantic version).
	ATMOfficialScoringVersion = "2026-11-09"
)

const (
	// PinATMOfficialScoringFixtureSHA256 pins internal/eval/testdata/atm_official_scoring_vectors.json.
	PinATMOfficialScoringFixtureSHA256 = "a9feda7cd9af9d8af2620f526edf84e1b5d717a82140bb0c842ef9c8e67a2795"

	// PinATMOfficialAnswerPayloadFixtureSHA256 pins internal/eval/testdata/atm_official_answer_payload_vectors.json.
	PinATMOfficialAnswerPayloadFixtureSHA256 = "0c2f3bb860fec690abf939c14acca0ea1d759083114463c997481734d0b9ea5e"

	// Upstream evaluator source provenance (JingbiaoMei/ATM-Bench@main, fetched 2026-11-09).
	PinATMOfficialNormalizerSHA1 = "ab703050dc18c0b96a8ea042f56b4210e4774a64"
	PinATMOfficialEvaluateQASHA1 = "bf940c0ba32c888033e22b6ceba8b42e9b759533"
	PinATMOfficialQTypeUtilsSHA1 = "dc888169c8c5f925a47a5e7fbeab4ec2b0e25ba8"
	PinATMOfficialConfigSHA1     = "a7126e059ce6745e84453f1fc1dd183656479807"

	// SGM corpus and the two official question tiers (sha256).
	PinATMCorpusSHA256 = "c9c4cc55dd3be147638121765b5dc7b8321a5e292b90909b68ae7fe8a57ee3a7"
	PinATM1013SHA256   = "3c4cbf84deeb158c78d515626a540908878d304ce0cda5c8e2ae7e52c6b878a2"
	PinATMHardSHA256   = "f6f63ff196a9f1de5715e35b328d4e80ca80311f2e2eae8028e7cb60e87ad37e"

	// PinATMCorpusStreamSHA256 是 corpus.jsonl.gz 解压后的 JSONL 字节流摘要。
	// 它与 gzip 压缩器实现无关：同一内容在不同 Go 工具链下重压出的容器字节可能不同，
	// 解压流必须一致。
	PinATMCorpusStreamSHA256 = "5841ef086954ac9fb75b45f0a45e54e81b1b5c6c04c958a1ab88678c3b266f4d"
	// PinATMCorpusSemanticSHA256 是按语料顺序逐条对「ItemID + 0x00 + FullText + 0x0A」
	// 求得的摘要，与容器字节和 JSON 序列化细节都无关，用于判定评测输入是否为同一语料。
	PinATMCorpusSemanticSHA256 = "79bad7ee4002cbc35106df18c071e20b52b780d1145ee229a3234c29a942cd5e"
)

// ATMOfficialScoringFixture returns nil because the historical fixture is not distributed.
func ATMOfficialScoringFixture() []byte {
	return nil
}

// ATMOfficialAnswerPayloadFixture returns nil because the historical fixture is not distributed.
func ATMOfficialAnswerPayloadFixture() []byte {
	return nil
}

// VerifyATMOfficialProtocol checks the runtime prompt pins. Corpus pins are
// verified separately against the caller's data directory.
func VerifyATMOfficialProtocol() error {
	return VerifyATMProtocol()
}

// ATMOfficialSourceSHA1s returns the upstream evaluator source pins.
func ATMOfficialSourceSHA1s() map[string]string {
	return map[string]string{
		"normalizer.py":  PinATMOfficialNormalizerSHA1,
		"evaluate_qa.py": PinATMOfficialEvaluateQASHA1,
		"qtype_utils.py": PinATMOfficialQTypeUtilsSHA1,
		"config.py":      PinATMOfficialConfigSHA1,
	}
}

// ATMOfficialCorpusSHA256s returns the corpus/tier pins.
func ATMOfficialCorpusSHA256s() map[string]string {
	return map[string]string{
		"corpus.jsonl.gz":           PinATMCorpusSHA256,
		"tiers/questions-1013.json": PinATM1013SHA256,
		"tiers/questions-hard.json": PinATMHardSHA256,
	}
}

// VerifyATMCorpusPins checks the on-disk corpus and question tiers against the
// official pins. It resolves the ATM data directory the same way the evaluator
// does; a missing directory is an error (fail-closed), not a silent pass.
//
// The corpus accepts three equivalent proofs, in decreasing strength: the exact
// gzip container bytes, the decompressed JSONL stream, or the per-item semantic
// digest. The container depends on the Go toolchain's flate implementation, so
// a rebuild on a newer toolchain can differ in bytes while carrying the same
// 11,034 items in the same order. Question tiers stay strictly byte-pinned.
func VerifyATMCorpusPins(dataDir string) error {
	return verifyATMCorpusPinsWith(dataDir, ATMOfficialCorpusSHA256s(), PinATMCorpusStreamSHA256, PinATMCorpusSemanticSHA256)
}

func verifyATMCorpusPinsWith(dataDir string, pins map[string]string, streamPin, semanticPin string) error {
	dir, err := ResolveATMDataDir(dataDir)
	if err != nil {
		return err
	}
	for rel, want := range pins {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		got, err := sha256File(path)
		if err != nil {
			return fmt.Errorf("ATM-Bench 官方语料 pin 读取失败 (%s): %w", rel, err)
		}
		if got == want {
			continue
		}
		if rel == "corpus.jsonl.gz" && atmCorpusContentMatches(path, streamPin, semanticPin) {
			continue
		}
		return fmt.Errorf("ATM-Bench 官方语料 pin 不符 (%s)：期望 %s，实际 %s", rel, want, got)
	}
	return nil
}

// atmCorpusContentMatches 报告在容器字节不同时，解压流或条目内容是否仍与官方 pin 一致。
func atmCorpusContentMatches(gzPath, streamPin, semanticPin string) bool {
	if streamPin != "" {
		if got, err := sha256GzStream(gzPath); err == nil && got == streamPin {
			return true
		}
	}
	if semanticPin != "" {
		if got, err := ATMCorpusSemanticDigestFromGz(gzPath); err == nil && got == semanticPin {
			return true
		}
	}
	return false
}

// sha256GzStream 计算 gzip 解压后的字节流摘要（容器无关）。
func sha256GzStream(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, gz); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func sha256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// ATMOfficialPinStatus is a display-friendly snapshot for the admin console.
type ATMOfficialPinStatus struct {
	Protocol              string            `json:"protocol"`
	Version               string            `json:"version"`
	PromptPins            map[string]string `json:"prompt_pins"`
	PromptPinsVerified    bool              `json:"prompt_pins_verified"`
	FixturePin            string            `json:"fixture_pin"`
	FixtureVerified       bool              `json:"fixture_verified"`
	AnswerPayloadPin      string            `json:"answer_payload_fixture_pin"`
	AnswerPayloadVerified bool              `json:"answer_payload_fixture_verified"`
	SourcePins            map[string]string `json:"source_pins"`
	CorpusPins            map[string]string `json:"corpus_pins"`
	CorpusPinsVerified    bool              `json:"corpus_pins_verified"`
	CorpusPinsError       string            `json:"corpus_pins_error,omitempty"`
}

// ATMOfficialPinSnapshot builds the admin-facing pin status. Historical fixture
// pins are provenance only, so their verified fields remain false. Corpus pin
// verification is best-effort; the CLI/gate path calls VerifyATMCorpusPins directly.
func ATMOfficialPinSnapshot(dataDir string) ATMOfficialPinStatus {
	status := ATMOfficialPinStatus{
		Protocol:         ATMOfficialScoringProtocol,
		Version:          ATMOfficialScoringVersion,
		PromptPins:       map[string]string{"answer_system_sha1": PinATMSystemSHA1, "answer_user_template_sha1": PinATMEvidencePromptSHA1, "judge_template_sha1": PinATMJudgeTPLSHA1},
		FixturePin:       PinATMOfficialScoringFixtureSHA256,
		AnswerPayloadPin: PinATMOfficialAnswerPayloadFixtureSHA256,
		SourcePins:       ATMOfficialSourceSHA1s(),
		CorpusPins:       ATMOfficialCorpusSHA256s(),
	}
	status.PromptPinsVerified = Sha1Hex(ATMSystemPrompt) == PinATMSystemSHA1 && Sha1Hex(ATMEvidencePromptTPL) == PinATMEvidencePromptSHA1 && Sha1Hex(ATMJudgePromptTPL) == PinATMJudgeTPLSHA1
	if err := VerifyATMCorpusPins(dataDir); err != nil {
		status.CorpusPinsError = err.Error()
	} else {
		status.CorpusPinsVerified = true
	}
	return status
}

// ATMOfficialPinIndex returns sorted "name=value" lines for logs/receipts.
func ATMOfficialPinIndex() []string {
	lines := make([]string, 0, 12)
	for name, value := range ATMOfficialSourceSHA1s() {
		lines = append(lines, "evaluator/"+name+"="+value)
	}
	for name, value := range ATMOfficialCorpusSHA256s() {
		lines = append(lines, "corpus/"+name+"="+value)
	}
	lines = append(lines, "fixture/sha256="+PinATMOfficialScoringFixtureSHA256)
	lines = append(lines, "fixture/answer_payload_sha256="+PinATMOfficialAnswerPayloadFixtureSHA256)
	lines = append(lines,
		"prompt/answer_system="+PinATMSystemSHA1,
		"prompt/answer_user="+PinATMEvidencePromptSHA1,
		"prompt/judge="+PinATMJudgeTPLSHA1,
	)
	sort.Strings(lines)
	return lines
}
