// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package cli

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// codedFailure 模拟携带稳定错误码的应用错误（评测 verify 的确定性失配族）。
type codedFailure struct {
	code string
	msg  string
}

func (e *codedFailure) Error() string { return e.msg }

func (e *codedFailure) ErrorCode() string { return e.code }

// TestFailEnvelopeCarriesStableErrorCode 锁定 rmem --json 的失败信封协议：
// 携带 ErrorCode() 的错误必须原样透出 error_code（调用方按码分类，不匹配文案），
// 未携带错误码的错误保持原有 {ok, error} 两字段形态。
func TestFailEnvelopeCarriesStableErrorCode(t *testing.T) {
	jsonArgs := func() *Args {
		return &Args{values: map[string]any{"json": true}, provided: map[string]bool{"json": true}}
	}

	var codedOut bytes.Buffer
	app := &application{stdout: &codedOut, stderr: &bytes.Buffer{}, args: jsonArgs()}
	if code := app.fail(&codedFailure{code: "manifest_corpus_mismatch", msg: "cloud evaluation manifest does not match the requested corpus"}); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	envelope := codedOut.String()
	for _, want := range []string{`"ok": false`, `"error": "cloud evaluation manifest does not match the requested corpus"`, `"error_code": "manifest_corpus_mismatch"`} {
		if !strings.Contains(envelope, want) {
			t.Fatalf("failure envelope missing %q:\n%s", want, envelope)
		}
	}

	var plainOut bytes.Buffer
	app = &application{stdout: &plainOut, stderr: &bytes.Buffer{}, args: jsonArgs()}
	if code := app.fail(errors.New("plain failure")); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if envelope := plainOut.String(); strings.Contains(envelope, "error_code") {
		t.Fatalf("uncoded failure must not grow an error_code field:\n%s", envelope)
	}
}
