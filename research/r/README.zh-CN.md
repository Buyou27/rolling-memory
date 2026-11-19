# R 研究者入口

**[简体中文](README.zh-CN.md) · [English](README.md)**

这里给用 R 做统计与建模的人准备了一个最小工作台：**不引入 R SDK**，R 通过命令行调用本地二进制、解析 JSON，其余分析全部在 R 里完成。检索与存储都在本机，记忆层没有网络请求。

## 依赖

- R ≥ 4.1（只用 base R，加一个 `jsonlite`）
- `rolling-memory` 本地版（预编译二进制，不需要 Go）

```bash
# 安装二进制：落在 ~/.local/share/rolling-memory/bin，并链接到 ~/.local/bin
curl -fsSL https://github.com/rocareer/rolling-memory/raw/main/install.sh | bash
export PATH="$HOME/.local/bin:$PATH"

# R 侧唯一依赖
Rscript -e 'install.packages("jsonlite")'
```

## 配置

三个环境变量优先，未设置时走默认值：

| 变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `ROLLING_MEMORY_BIN` | 自动探测 `PATH` 与安装目录 | 二进制路径 |
| `ROLLING_MEMORY_DB` | `~/.local/share/rolling-memory/memory.db` | 库文件（单个 SQLite） |
| `ROLLING_MEMORY_SCOPE` | `default` | 分区名 |

**scope 在 `init` 时绑定到库**：同一个库只能用建库时的 scope 查询，换 scope 会报
`vault scope does not match its binding`。要分主题就建多个库，各带各的 scope。

想持久化而不设环境变量：

```bash
rolling-memory config set db /path/to/memory.db
rolling-memory config set scope my-research
rolling-memory config show
```

## 快速开始

```r
source("rmemory.R")

rm_paths()                       # 看当前解析到的 bin / db / scope
bands <- rm_bands("为什么放弃向量化")
bands[, c("score", "title", "content")]
```

`score` 的单位是 **Nats**（零假设下的负对数碰撞概率），是**可加的证据量**，不是 $[0,1]$ 的相似度。跨查询比较分数没有意义，同一查询内比较才有。

## 目录里的文件

| 文件 | 用途 |
| :--- | :--- |
| [`rmemory.R`](rmemory.R) | 可 `source()` 的薄封装：`rm_init` / `rm_import` / `rm_status` / `rm_doctor` / `rm_recall` / `rm_bands` |
| [`demo.R`](demo.R) | 端到端演示：建临时库 → 导入 4 轮对话 → 2 个查询 → 打印命中（不碰你的真实库） |
| [`occupancy-null.R`](occupancy-null.R) | 纯 base R 复算：占据天数零分布、$\le 1/M$ 尾界、Monte Carlo 对拍与作图 |

```bash
Rscript demo.R            # 跑完自动清理临时库
Rscript demo.R --keep     # 保留临时库，便于继续检查
Rscript occupancy-null.R  # 打印尾界核对结果，并生成一张图
```

## 导入自己的对话

`import` 接受 JSONL，每行一条消息：

```jsonl
{"type":"user","message":{"role":"user","content":"我们决定不做向量化，全部走本地词法统计。"}}
{"type":"assistant","message":{"role":"assistant","stop_reason":"end_turn","content":"为什么？"}}
```

**assistant 行必须带 `"stop_reason":"end_turn"`**。没有这个完成标记的轮次会被判为"未完成的尾轮"而跳过——这是导入时最常见的"看起来没报错但一条都没进"的原因。

```bash
rolling-memory import ~/notes/session.jsonl --db ~/vault/research.db --scope research
```

## 排错

| 现象 | 原因 | 处理 |
| :--- | :--- | :--- |
| `rolling-memory binary not found` | 没装或不在 `PATH` | 设 `ROLLING_MEMORY_BIN` 指向绝对路径 |
| `import: 0 messages · 0 new units` | assistant 行缺 `stop_reason` | 补上完成标记 |
| `vault scope does not match its binding` | 用 A 建库、用 B 查询 | 统一 `--scope`，或另建库 |
| `recall` 返回 `absent: true` | 库里确实没有相关记录 | 先 `import`，或换更贴近原文的说法 |
| 中文乱码 | 终端/文件编码不是 UTF-8 | `file -I` 检查，导出时选 UTF-8 |

## 边界

- 本地版是**纯本地明文**：库就是一个 SQLite 文件，记忆层零网络请求、零向量、零后台大模型调用。
- `recall` 返回的原文会进入你（或你的智能体）的上下文；如果你的下游模型在远端，这段文本会随 prompt 离开本机。
- 系统只判断"档案里有没有"，不判断"世界上是不是真的"；真伪裁决留给你和下游模型。

## 许可

跟随仓库根目录 [LICENSE](../../LICENSE)。
