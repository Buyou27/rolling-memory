// Copyright 2026 Rocareer & Rolling & PsychologyLake.org.cn
// Author: Albert <albert@rocareer.com>

package delivery

// 本文件 = 槽位标签的「未消解第一人称领属限定语」去语境化（纯形式层 · 0 语义推断）：
//   - 与 PMAS M11（`hasUnresolvedFirstPerson`）共用同一封闭形式类，避免判据与规范化漂移；
//   - 只剥限定语、不推断领属人（身份判定权属 LLM，见设计稿文档）。
//   - 限定语后必须仍有实义字符（字母/表意字），否则原样返回（防 "our" / "我们的" 这类纯限定语空壳）。

// 架构契约「代词 / 指代 / 关系 / 词面闸口 100% 归 LLM，代码不处理语义与词句」：
// 第一人称领属限定语的静态词表（原 {"my ","our ","我的","我们的"}）已废止——属代码语义猜测。
// 以下三枚 API 保留形状供调用侧零改动，但恒定不命中：未消解的第一人称由 LLM 解算，判不出落 gap。

// HasFirstPersonPossessivePrefix 已废止：恒定 false（第一人称领属判定归 LLM）。
func HasFirstPersonPossessivePrefix(string) bool { return false }
