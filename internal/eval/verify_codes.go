// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package eval

// 跨进程稳定判据：rmem CLI 的 JSON 失败信封（{ok:false, error, error_code}）原样透出
// ErrorCode()，Admin 的 prepare 据此区分「语料确定性失配（repair + 断点续灌可自愈）」
// 与瞬时故障（网络/健康检查失败，必须 fail-closed 不重灌）。
const (
	// EvalVerifyCodeManifestMismatch：manifest 与当前请求的语料逐字段不等
	// （scope / suite / corpus_sha256 / documents），复用不可能、重灌即自愈。
	EvalVerifyCodeManifestMismatch = "manifest_corpus_mismatch"
	// EvalVerifyCodeManifestIdentityMismatch：manifest 的写入身份（endpoint / tenant / user、
	// 切词臂）与现役客户端不一致——旧版本写下的库，复用不可能；语料本身仍可按现役装配
	// 原地重灌自愈（断点续灌封条同样按身份对账，不会跳过旧单元）。脉冲模式属契约轴，
	// 失配仍由 Prepare fail-closed 拒绝，绝不触发清库。
	EvalVerifyCodeManifestIdentityMismatch = "manifest_identity_mismatch"
)

// EvalVerifyError 为评测验证类失效提供稳定错误码。
type EvalVerifyError struct {
	Code string
	Msg  string
}

func (e *EvalVerifyError) Error() string { return e.Msg }

// ErrorCode 供 CLI 失败信封与 Admin 分类使用（稳定机器可读判据，不依赖文案）。
func (e *EvalVerifyError) ErrorCode() string { return e.Code }
