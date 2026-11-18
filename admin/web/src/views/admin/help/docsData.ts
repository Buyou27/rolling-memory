/**
 * 帮助中心 · 开源文档数据层（通过相对软连接 + Vite ?raw 直接加载仓库根目录与 docs/ 下的真实全文）。
 * 修改 docs/*.md 或根级 README.md / DECLARATION.md / LICENSE 即刻在 Web 后台热更新生效，零手工拷贝。
 */

import rawChangelog from './content/CHANGELOG.md?raw'
import rawDeclarationEn from './content/DECLARATION.md?raw'
import rawDeclarationZh from './content/docs/zh-CN/DECLARATION.md?raw'
import rawLicense from './content/LICENSE?raw'
import rawNotice from './content/NOTICE?raw'
import rawReadmeEn from './content/README.md?raw'
import rawReadmeZh from './content/docs/zh-CN/README.zh-CN.md?raw'
import rawBenchmarksZh from './content/docs/zh-CN/benchmarks.md?raw'
import rawBenchmarksEn from './content/docs/benchmarks.md?raw'
import rawFaqZh from './content/docs/zh-CN/faq.md?raw'
import rawFaqEn from './content/docs/faq.md?raw'
import rawHarnessZh from './content/docs/zh-CN/harness.md?raw'
import rawHarnessEn from './content/docs/harness.md?raw'
import rawMcpZh from './content/docs/zh-CN/mcp.md?raw'
import rawMcpEn from './content/docs/mcp.md?raw'
import rawPluginConsoleZh from './content/docs/zh-CN/plugin-console.md?raw'
import rawPluginConsoleEn from './content/docs/plugin-console.md?raw'
import rawConsoleTerminalZh from './content/docs/zh-CN/console-terminal.md?raw'
import rawConsoleTerminalEn from './content/docs/console-terminal.md?raw'
import rawPrivacyZh from './content/docs/zh-CN/privacy.md?raw'
import rawPrivacyEn from './content/docs/privacy.md?raw'
import rawTokenEconomicsZh from './content/docs/zh-CN/token-economics.md?raw'
import rawTokenEconomicsEn from './content/docs/token-economics.md?raw'
import rawBackgroundVsGrepZh from './content/docs/zh-CN/background-vs-grep.md?raw'
import rawBackgroundVsGrepEn from './content/docs/background-vs-grep.md?raw'
import rawUsbPortableZh from './content/docs/zh-CN/usb-portable.md?raw'
import rawUsbPortableEn from './content/docs/usb-portable.md?raw'
import rawEssaySimilarityZh from './content/docs/zh-CN/essays/memory-retrieval-is-not-similarity-search.md?raw'
import rawEssaySimilarityEn from './content/docs/essays/memory-retrieval-is-not-similarity-search.md?raw'
import rawEssayBridgingZh from './content/docs/zh-CN/essays/semantic-bridging-creates-no-new-information.md?raw'
import rawEssayBridgingEn from './content/docs/essays/semantic-bridging-creates-no-new-information.md?raw'

/** 帮助中心文档图片资源：content/docs/assets/ 下的图片经 Vite 打包为本地 URL，离线可显。 */
const docAssets = import.meta.glob('./content/docs/assets/*', { eager: true, query: '?url', import: 'default' }) as Record<string, string>

/** 文档内相对图片路径（`docs/assets/x.png` / `assets/x.png` / `../assets/x.png`）解析为可访问 URL；未打包资源回退公开仓库 raw 地址。 */
export const resolveDocAsset = (src: string): string => {
    if (/^(https?:)?\/\//i.test(src) || src.startsWith('data:')) return src
    const normalized = src.replace(/^(\.\.?\/)+/, '')
    const repoPath = normalized.startsWith('docs/') ? normalized : `docs/${normalized}`
    const bundled = docAssets[`./content/docs/${repoPath.slice('docs/'.length)}`]
    return bundled || `https://raw.githubusercontent.com/rocareer/rolling-memory/main/${repoPath}`
}

export type DocScopeKey = 'root' | 'docs'

export type DocCategoryKey = 'overview' | 'guide' | 'architecture' | 'legal'

export interface OpenSourceDocItem {
    id: string
    scope: DocScopeKey
    repoFullName: string
    path: string
    icon: string
    category: DocCategoryKey
    isPlainLicense?: boolean
    badgeZh: string
    badgeEn: string
    url: string
    titleZh: string
    titleEn: string
    shortTitleZh: string
    shortTitleEn: string
    summaryZh: string
    summaryEn: string
    sectionsZh: string[]
    sectionsEn: string[]
    rawZh: string
    rawEn: string
}

export const MAIN_REPO_URL = 'https://github.com/rocareer/rolling-memory'
export const MAIN_DOCS_URL = 'https://github.com/rocareer/rolling-memory/tree/main/docs'
export const ROCAREER_ORG_URL = 'https://github.com/rocareer'

export const OPEN_SOURCE_DOCS: OpenSourceDocItem[] = [
    // ==================== 一、根级总览、立场声明与开源许可文档 ====================
    {
        id: 'root-readme',
        scope: 'root',
        repoFullName: 'rocareer/rolling-memory',
        path: 'README.md',
        icon: 'lucide-compass',
        category: 'overview',
        badgeZh: '必读入口',
        badgeEn: 'Start Here',
        url: `${MAIN_REPO_URL}/blob/main/README.md`,
        shortTitleZh: '项目总览与快速开始',
        shortTitleEn: 'Overview & Quick Start',
        titleZh: 'README.md · 项目总览与快速开始',
        titleEn: 'README.md · Project Overview & Quick Start',
        summaryZh:
            'Rolling Memory 主入口：阐明 AI 记忆普惠与平权初衷（真免费 · 实时无需等待建库抽取 · 安全无第三方知晓代码与信息）、原话保真与本地词法统计检索、100k–400k 长程上下文省往返算账与多宿主一键接入。',
        summaryEn:
            'Main entry point for Rolling Memory: accessible and genuinely free local memory (zero hidden bills, real-time indexing, no third party seeing your code), verbatim storage with local lexical-statistical retrieval, 100k–400k turn-saving math, and multi-harness setup.',
        sectionsZh: [
            '核心体验：真免费（0 后台模型与向量账单）、实时可用（无需等待建库抽取）、安全私密（无第三方经手代码与信息）',
            '日常实际效果：在 100k–400k 长程上下文下减少盲搜往返（能省一点是一点），附本机 24h 与整周真实使用账单',
            '为什么保留原话 + 本地词法统计检索：避免 LLM 抽取总结“错上加错”与写入理解税，契合 Coding Agent 术语一致性',
            '公开基准实测与局限边界：剑桥 ATM-Bench / LoCoMo 复算数据、~20% 漏召回分析与 5 项适用边界',
            '快速开始、10 大 Harness 一键接入与宿主自带记忆的互补关系',
        ],
        sectionsEn: [
            'Core Experience: Genuinely free ($0 model/vector bills), real-time (no extraction queue), and private (no third party in the middle)',
            'Daily Impact: Cutting blind search round-trips in 100k–400k long-context sessions, with our audited 24h & 7-day usage ledger',
            'Why Verbatim + Local Lexical-Statistical Search: Avoiding LLM summarization error-compounding and upfront comprehension tax',
            'Benchmarks & Honest Boundaries: Cambridge ATM-Bench / LoCoMo recomputations, ~20% miss analysis, and 5 scope limits',
            'Quick Start, 10-Harness Setup & How It Complements Built-in Harness Memory',
        ],
        rawZh: rawReadmeZh,
        rawEn: rawReadmeEn,
    },
    {
        id: 'root-declaration',
        scope: 'root',
        repoFullName: 'rocareer/rolling-memory',
        path: 'DECLARATION.md',
        icon: 'lucide-award',
        category: 'legal',
        badgeZh: '立场与作者',
        badgeEn: 'Position & Author',
        url: `${MAIN_REPO_URL}/blob/main/DECLARATION.md`,
        shortTitleZh: '立场、作者与诚实免责声明',
        shortTitleEn: 'Declaration & Author',
        titleZh: 'DECLARATION.md · 立场、关于作者与诚实免责声明',
        titleEn: 'DECLARATION.md · Position, About the Author & Honest Disclaimer',
        summaryZh:
            '阐明我们是谁（Rocareer 与 Rolling-Memory 组织）、发起人兼首席架构师 Albert 简介、开发初衷、人机协作披露与按原样（AS IS）免责声明。',
        summaryEn:
            'States Who We Are (Rocareer & Rolling-Memory), About the Author (Albert <albert@rocareer.com>), motivation, honest disclosure, and AS-IS disclaimer.',
        sectionsZh: [
            '我们是谁：代表 Rocareer (https://github.com/rocareer) 与 Rolling-Memory 组织的独立开发者与研究人员',
            '关于作者：Albert (albert@rocareer.com) —— Rocareer 组织创始人、Rolling-Memory 组织联合创始人兼首席架构师，曾任人工智能头部企业技术合伙人 / CTO',
            '初衷：将日常科研与工程中实际使用的本地优先图记忆核打磨为单二进制与多宿主插件，免费开放给个人开发者与高校科研人员',
            '诚实披露与免责条款：人机协作模式披露、评测密码学锁定反作弊、按“原样（AS IS）”无担保提供、使用者风险自担',
        ],
        sectionsEn: [
            'Who We Are: Independent developers and researchers representing Rocareer and the Rolling-Memory Organization',
            'About the Author: Albert (albert@rocareer.com) — Founder of Rocareer, Co-Founder & Chief Architect of Rolling-Memory, Former Technical Partner / CTO at a leading AI enterprise',
            'Why We Built This: Packaging our daily-use verbatim-anchored graph memory core for individual developers and academic researchers',
            'Honest Disclosure & Disclaimer: Human-defined invariants + AI-assisted implementation, cryptographic benchmark pinning, AS-IS no warranty',
        ],
        rawZh: rawDeclarationZh,
        rawEn: rawDeclarationEn,
    },
    {
        id: 'root-license',
        scope: 'root',
        repoFullName: 'rocareer/rolling-memory',
        path: 'LICENSE',
        icon: 'lucide-scale',
        category: 'legal',
        isPlainLicense: true,
        badgeZh: 'PolyForm-NC-1.0.0',
        badgeEn: 'PolyForm-NC-1.0.0',
        url: `${MAIN_REPO_URL}/blob/main/LICENSE`,
        shortTitleZh: 'PolyForm Noncommercial 1.0.0 许可证',
        shortTitleEn: 'PolyForm Noncommercial 1.0.0 License',
        titleZh: 'LICENSE · PolyForm Noncommercial License 1.0.0',
        titleEn: 'LICENSE · PolyForm Noncommercial License 1.0.0',
        summaryZh:
            'PolyForm Noncommercial 1.0.0：一切非商业用途（个人、学术、公益、教育）免费使用、修改与再分发；商业用途须另行获取书面授权（sdk/ 目录下客户端 SDK 单独采用 Apache-2.0）。',
        summaryEn:
            'PolyForm Noncommercial 1.0.0: free to use, modify, and redistribute for any noncommercial purpose (personal, academic, research, education); commercial use requires a separate written license (client SDKs under sdk/ are Apache-2.0).',
        sectionsZh: [
            '第 1–4 条：定义、许可授予（版权与专利）、再分发条件与 NOTICE 保留',
            '第 5–9 条：贡献许可、商标、免责声明、责任限制与许可终止/恢复',
            '第三方组件署名：见根 NOTICE 与 admin/NOTICE',
        ],
        sectionsEn: [
            'Sections 1–4: Definitions, Grant of Copyright and Patent License, Redistribution Conditions, and NOTICE retention',
            'Sections 5–9: Contributions, Trademarks, Disclaimer of Warranty, Limitation of Liability, and Termination/Recovery',
            'Third-party components and attribution: see root NOTICE and admin/NOTICE',
        ],
        rawZh: rawLicense,
        rawEn: rawLicense,
    },
    {
        id: 'root-notice',
        scope: 'root',
        repoFullName: 'rocareer/rolling-memory',
        path: 'NOTICE',
        icon: 'lucide-file-check',
        category: 'legal',
        isPlainLicense: true,
        badgeZh: '合规归属',
        badgeEn: 'Attributions',
        url: `${MAIN_REPO_URL}/blob/main/NOTICE`,
        shortTitleZh: '第三方开源组件归属声明',
        shortTitleEn: 'Third-Party Notices',
        titleZh: 'NOTICE · 第三方开源组件合规归属声明',
        titleEn: 'NOTICE · Third-Party Open-Source Component Notices',
        summaryZh:
            '完整列出 Go 核心运行时依赖（modernc.org/sqlite BSD-3-Clause、go-toml MIT 等）与可选 Web 管理控制台（ai-go-admin / BuildAdmin Apache-2.0、ECharts Apache-2.0、Vue/Element Plus MIT）的许可证归属。',
        summaryEn:
            'Lists attributions for Go core dependencies (modernc.org/sqlite BSD-3-Clause, go-toml MIT) and optional Web admin console components (Apache-2.0, MIT).',
        sectionsZh: [
            'Go 核心运行时依赖归属（modernc.org/sqlite BSD-3-Clause、golang.org/x BSD-3-Clause、go-toml/v2 MIT、google/uuid BSD-3-Clause）',
            '可选 Web 管理框架与可视化组件归属（ai-go-admin / BuildAdmin Apache-2.0、Apache ECharts Apache-2.0、Vue 3 / Element Plus / Pinia / Vite MIT）',
        ],
        sectionsEn: [
            'Go Core Runtime Dependencies (modernc.org/sqlite BSD-3-Clause, golang.org/x BSD-3-Clause, go-toml/v2 MIT, google/uuid BSD-3-Clause)',
            'Optional Web Administration Framework & Visualization Components (ai-go-admin / BuildAdmin Apache-2.0, Apache ECharts Apache-2.0, Vue 3 / Element Plus MIT)',
        ],
        rawZh: rawNotice,
        rawEn: rawNotice,
    },
    {
        id: 'root-changelog',
        scope: 'root',
        repoFullName: 'rocareer/rolling-memory',
        path: 'CHANGELOG.md',
        icon: 'lucide-scroll-text',
        category: 'overview',
        badgeZh: 'v0.0.1',
        badgeEn: 'v0.0.1',
        url: `${MAIN_REPO_URL}/blob/main/CHANGELOG.md`,
        shortTitleZh: '公开产品版本变更日志',
        shortTitleEn: 'Release Changelog',
        titleZh: 'CHANGELOG.md · 开源产品版本变更日志',
        titleEn: 'CHANGELOG.md · Public Release Changelog',
        summaryZh: '记录对外发布的产品版本特性、CLI/MCP 契约演进、多宿主插件能力与评测基准更新。',
        summaryEn: 'Records public product release features, CLI/MCP contract updates, multi-harness plugin capabilities, and benchmark milestones.',
        sectionsZh: [
            'v0.0.1：开源首版——本地明文金库（local_plain · 零向量/零大模型/零外联）、三层记忆（L1 背景挂载 / L2 会话捕获 / L3 结晶）、单工具 MCP recall 与八宿主一键安装、Web 管理后台与评测实验室（LoCoMo 1,540 全量 85.26% · ATM-Bench 官方协议）',
            '后续版本按发布时间倒序登记',
        ],
        sectionsEn: [
            'v0.0.1: Initial public release — local plaintext vault (local_plain · zero vectors / zero LLM / zero egress), three-layer memory (L1 background mounts / L2 episodic capture / L3 noetic crystallization), single-tool MCP recall with one-command installers for eight harnesses, web console and evaluation workbench (LoCoMo full set 85.26% · ATM-Bench official protocol)',
            'Subsequent releases are listed in reverse chronological order',
        ],
        rawZh: rawChangelog,
        rawEn: rawChangelog,
    },

    // ==================== 二、docs/ 目录核心技术与使用文档 ====================
    {
        id: 'docs-token-economics',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/token-economics.md',
        icon: 'lucide-calculator',
        category: 'overview',
        badgeZh: '算账与实测',
        badgeEn: 'Token Math & Log',
        url: `${MAIN_REPO_URL}/blob/main/docs/token-economics.md`,
        shortTitleZh: '省 Token 算账、证伪与一周真实使用记录',
        shortTitleEn: 'Token Math, Falsification & 7-Day Real Log',
        titleZh: 'docs/zh-CN/token-economics.md · 本地记忆是怎么帮你省（或亏）Token 与时间的（附本机真实一周记录）',
        titleEn: 'docs/token-economics.md · How Local Memory Saves (or Costs) Tokens and Time (with 7-Day Real Usage Report)',
        summaryZh:
            '用谁都能算清的加减乘除拆解 100k–400k 长程上下文下减少一次盲搜往返能省多少 Token、三类反而亏 Token 的证伪反例与盈亏公式，并附上我们主力开发机最近 24 小时及推算整周的真实使用账单（对照 GPT-6 / Claude Fable / Gemini 4 官网最新价格）。',
        summaryEn:
            'Plain arithmetic showing how eliminating a blind search turn saves (or costs) tokens in 100k–400k long-context sessions, three falsification counterexamples, the break-even equation, and our own audited 24h & projected 7-day usage ledger under GPT-6 / Claude Fable / Gemini 4 pricing.',
        sectionsZh: [
            '基本事实与正向算账：在 100k–400k 长程上下文下，少走 1 次盲搜往返即省下 10 万～40 万输入 Token 与等待时间',
            '反向证伪与盈亏平衡公式 (p * k > 1 + m * R / C)：空跑未命中、短上下文已知符号、大体积注入三类反例',
            '为什么不在写入时让 LLM 做抽取总结：消费时省几百 Token 占比不到 0.15%，却要在写入时先付高昂的 LLM 理解税',
            '本机最近 24 小时（76 份严苛自评）与推算整周真实账单：GPT-6 Astra / Claude Fable 5.2 / Gemini 4 Argon 官网价格折算',
        ],
        sectionsEn: [
            'Turn-Elimination Math: Why avoiding 1 blind turn in a 100k–400k context saves 100k–400k billed input tokens',
            'Falsification Counterexamples & Break-Even Equation (p * k > 1 + m * R / C): When calling memory loses tokens',
            'Why Not Summarize with an LLM at Write Time: Tiny (<0.15%) read-time savings vs. hefty upfront LLM comprehension cost',
            'Audited 24-Hour & Projected 7-Day Real Usage Report: Dollar savings across GPT-6 Astra, Claude Fable 5.2, and Gemini 4 Argon',
        ],
        rawZh: rawTokenEconomicsZh,
        rawEn: rawTokenEconomicsEn,
    },
    {
        id: 'docs-background-vs-grep',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/background-vs-grep.md',
        icon: 'lucide-folder-search',
        category: 'overview',
        badgeZh: '边界与自证',
        badgeEn: 'vs. grep & Proof',
        url: `${MAIN_REPO_URL}/blob/main/docs/background-vs-grep.md`,
        shortTitleZh: 'recall 与 Background 挂载 vs. grep（边界、价值与自证）',
        shortTitleEn: 'recall & Background Mounts vs. grep (Boundaries & Proof)',
        titleZh: 'docs/zh-CN/background-vs-grep.md · 既然 Harness 自带 grep，为什么还要用 recall 与 Background 目录挂载？',
        titleEn: 'docs/background-vs-grep.md · Why Use recall and Background Directory Mounts When Your Harness Already Has grep?',
        summaryZh:
            '正面解答“把代码仓挂进 Background 对比直接用 grep 是不是多此一举”：划清何时直接用 grep、单靠 grep 做不到的 4 项核心价值（症状→代码符号桥接、否定性排障教训、跨仓库与 .docx/.xlsx/.pptx/.pdf 原生解析、多线索共鸣排序），以及 3 套本机可复现的验证方法。',
        summaryEn:
            'Directly answers whether mounting directories or calling recall is redundant when your harness has grep: defines when to use grep directly, the 4 capabilities grep lacks (symptom-to-symbol bridging, negative knowledge, cross-repo & binary office doc mounts, multi-cue ranking), and 3 reproducible proofs.',
        sectionsZh: [
            '先立边界：当前工作区 + 已知精确符号直接用 grep，不要把整个当前代码仓无脑全量挂进 Background 替代 grep',
            'grep 做不到的 4 件事：症状→代码符号跨层桥接、留存“为什么不能那样改”的否定性知识、跨仓库与 .docx/.xlsx/.pptx/.pdf 原位按需解析、多线索信息增益排序',
            '3 套本机自证方法：零模型确定性事后采纳审计（ROI 监控屏）、剑桥 ATM-Bench / LoCoMo 断网一键复算、10 秒本机 grep vs. recall 同台对比',
        ],
        sectionsEn: [
            'The Boundary First: Use grep directly for known symbols in the current workspace; do not mount an active codebase just to replace grep',
            'Four Things grep Cannot Do: Symptom-to-symbol bridging, negative knowledge, cross-repo & native .docx/.xlsx/.pptx/.pdf parsing, and multi-cue ranking',
            'Three Reproducible Proofs: Zero-LLM post-hoc adoption audit, offline Cambridge ATM-Bench / LoCoMo reproduction, and a 10-second side-by-side test',
        ],
        rawZh: rawBackgroundVsGrepZh,
        rawEn: rawBackgroundVsGrepEn,
    },
    {
        id: 'docs-faq',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/faq.md',
        icon: 'lucide-circle-help',
        category: 'overview',
        badgeZh: '常见问答',
        badgeEn: 'FAQ',
        url: `${MAIN_REPO_URL}/blob/main/docs/zh-CN/faq.md`,
        shortTitleZh: '常见问答（与 grep / 向量 RAG / 宿主自带记忆的区别）',
        shortTitleEn: 'FAQ (vs. grep, Vector RAG & Built-in Memory)',
        titleZh: 'docs/zh-CN/faq.md · 常见问答（与 grep、向量 RAG 及各家 Harness 自带记忆的区别与配合）',
        titleEn: 'docs/faq.md · Frequently Asked Questions (vs. grep, Vector RAG & Built-in Harness Memory)',
        summaryZh:
            '平实解答 Rolling Memory 与普通代码扫描（grep / ripgrep）、向量数据库（RAG）、LLM 抽取服务以及各家 Harness 厂商自带记忆功能（CLAUDE.md / MEMORY.md / Rules）的区别与互补配合。',
        summaryEn:
            'Practical answers on how Rolling Memory relates to grep/ripgrep, Vector RAG, LLM-extraction memory services, and built-in harness memory features (CLAUDE.md / MEMORY.md / Rules).',
        sectionsZh: [
            'Q1：与 grep / ripgrep 的区别与配合（只记得症状不记得变量名、留存排障教训、跨天串联对话）',
            'Q2：与向量 RAG 及 LLM 抽取服务的区别（真免费实时、不改写事实防错上加错、契合 Coding Agent 术语一致性）',
            'Q3：各家 Harness 厂商都有自带记忆功能了，为什么还要用它（桌面便签纸 vs. 全量历史黑匣子、跨 10 大宿主共享）',
            'Q4–Q6：本地数据安全与边界、云端能力申请制预览、随包基准与省 Token 账单复现',
        ],
        sectionsEn: [
            'Q1: How it complements grep/ripgrep (symptom lookup, negative knowledge, cross-session context)',
            'Q2: Differences vs. Vector RAG & LLM extraction (genuinely free, no fact-rewriting error compounding, coding terminology fit)',
            'Q3: Why use this when every harness vendor already has built-in memory (sticky note vs. full-history black-box across 10 harnesses)',
            'Q4–Q6: Local data safety & boundaries, the cloud capability preview, and benchmark / token-math verification',
        ],
        rawZh: rawFaqZh,
        rawEn: rawFaqEn,
    },
    {
        id: 'docs-mcp',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/mcp.md',
        icon: 'lucide-blocks',
        category: 'architecture',
        badgeZh: '单工具契约',
        badgeEn: '1-Tool API',
        url: `${MAIN_REPO_URL}/blob/main/docs/mcp.md`,
        shortTitleZh: 'MCP 单工具契约 (recall · 回忆)',
        shortTitleEn: 'MCP Single-Tool Contract (recall)',
        titleZh: 'docs/zh-CN/mcp.md · MCP 单工具契约（recall · 回忆即记忆）',
        titleEn: 'docs/mcp.md · MCP Single-Tool Contract (recall · Recall IS Memory)',
        summaryZh: '定义 MCP stdio server 的唯一核心工具 recall（回忆）的完整 JSON Schema、参数说明、返回结构与实时 Noetic 结晶机制。',
        summaryEn:
            'Specifies the JSON Schema, parameters, response receipts, and real-time Noetic crystallization for the single MCP tool: recall (回忆).',
        sectionsZh: [
            '为什么只暴露单一 MCP 工具 recall（回忆）：对话原文由后台自动同步入库（L2 Episodic），每次回忆自动巩固结晶 L3 Noetic（回忆即记忆）',
            'recall 输入与交付：cue / mode(full|probe) / result_unit / rank / asof / time_ref / window 与 bands × chains 响应结构',
            '单一轻量 Hook（UserPromptSubmit）与后台自动同步、零出网',
        ],
        sectionsEn: [
            'Why a Single MCP Tool (recall · 回忆): Conversation turns are captured automatically by the background watcher (L2 Episodic) and every recall crystallizes L3 Noetic (Recall IS Memory)',
            'recall Input & Delivery: cue, mode(full|probe), result_unit, rank, asof, time_ref, window, and bands × chains payload',
            'Single Lightweight Hook (UserPromptSubmit), Background Auto-Sync & Zero Egress',
        ],
        rawZh: rawMcpZh,
        rawEn: rawMcpEn,
    },
    {
        id: 'docs-harness',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/harness.md',
        icon: 'lucide-plug-zap',
        category: 'guide',
        badgeZh: '10 大宿主',
        badgeEn: '10 Harnesses',
        url: `${MAIN_REPO_URL}/blob/main/docs/harness.md`,
        shortTitleZh: '多 Harness 接入与插件机制',
        shortTitleEn: 'Multi-Harness & Plugins',
        titleZh: 'docs/zh-CN/harness.md · 多 Harness 一键接入与插件机制指南',
        titleEn: 'docs/harness.md · Multi-Harness Integration & Plugin Guide',
        summaryZh:
            '说明 rolling-memory install 如何一键适配 ZCode、Claude Code、Codex、Cursor、Windsurf、Cline、dsh、CodeBuddy、WorkBuddy、豆包，以及 Hooks 自动流转与单路去重机制。',
        summaryEn:
            'Explains one-command setup for ZCode, Claude Code, Codex, Cursor, Windsurf, Cline, dsh, CodeBuddy, WorkBuddy, and Doubao, including lifecycle hooks and deduplication.',
        sectionsZh: [
            '支持的宿主矩阵：ZCode / Claude Code / Codex / dsh / CodeBuddy 及扩展宿主（Cursor / Windsurf / Cline / WorkBuddy / 豆包）',
            '安装与自检命令：rolling-memory install / --list / --check / --dry-run / --uninstall',
            '单一轻量 Hook：UserPromptSubmit（按需唤起回忆）；Episodic 由后台自动同步写入',
            '原生插件与全局 CLI 单路去重机制',
        ],
        sectionsEn: [
            'Supported Harness Matrix: ZCode / Claude Code / Codex / dsh / CodeBuddy & Cursor / Windsurf / Cline / WorkBuddy / Doubao',
            'Installer & Verification Commands: rolling-memory install / --list / --check / --dry-run / --uninstall',
            'Single Lightweight Hook: UserPromptSubmit (on-demand recall); Episodic capture via background auto-sync',
            'Native Plugin vs Global CLI Single-Path Deduplication',
        ],
        rawZh: rawHarnessZh,
        rawEn: rawHarnessEn,
    },
    {
        id: 'docs-plugin-console',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/plugin-console.md',
        icon: 'lucide-layout-dashboard',
        category: 'guide',
        badgeZh: 'Web 控制台',
        badgeEn: 'Web Console',
        url: `${MAIN_REPO_URL}/blob/main/docs/plugin-console.md`,
        shortTitleZh: 'Web 控制台与插件频道指南',
        shortTitleEn: 'Web Console & Plugin Guide',
        titleZh: 'docs/zh-CN/plugin-console.md · Web 管理控制台与插件频道指南',
        titleEn: 'docs/plugin-console.md · Web Console & Plugin Channel Guide',
        summaryZh:
            '介绍随仓内置的 SQLite 版 Web 管理控制台七大频道（控制台[实时控制台+ROI监控屏+记忆图谱+独立记忆控制台]、记忆管理[检索调试置顶]、评测、帮助中心，以及沉底的权限、常规、插件管理面）、零轮询 WebSocket 实时流及无缝档位切换。',
        summaryEn:
            'Covers the seven top-level channels of the built-in SQLite Web Administration Console (day-to-day channels first — including the Recall Debugger and the dedicated Memory Console & Zero-Poll WebSocket stream — with administration channels at the bottom), seamless tier transitions, and the plugin lifecycle gate.',
        sectionsZh: [
            '七大一级频道总览：控制台（实时控制台 + ROI 监控屏 + 记忆图谱 + 独立记忆控制台）、记忆管理（检索调试 + 三层记忆）、评测、帮助中心，管理面（权限管理、常规管理、插件）沉底',
            '零轮询 WebSocket 实时状态流（GET /ws）与三层认知记忆（Background × Episodic × Noetic）实时监控',
            '插件频道与环境门禁（dev vs prod）：源码插件落位安全保护与 L1/L2 插件形态说明',
        ],
        sectionsEn: [
            'Seven Top-Level Channels: Dashboard (Live Console + ROI Monitor + Memory Graph + Memory Console), Memory Management (Recall Debugger + three layers), Evaluation, Help Center, with administration channels (Permissions, Routine, Plugins) at the bottom',
            'Zero-Poll WebSocket Real-Time Stream (GET /ws) & Live Replay/Reindex Progress (items/s & ETA)',
            'Plugin Channel & Environment Gate (dev vs prod): Source-tree protection and L1/L2 plugin kinds',
        ],
        rawZh: rawPluginConsoleZh,
        rawEn: rawPluginConsoleEn,
    },
    {
        id: 'docs-console-terminal',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/console-terminal.md',
        icon: 'lucide-terminal',
        category: 'guide',
        badgeZh: '控制台 CLI',
        badgeEn: 'Console CLI',
        url: `${MAIN_REPO_URL}/blob/main/docs/console-terminal.md`,
        shortTitleZh: 'Web 控制台内置命令行（终端）',
        shortTitleEn: 'Built-in Console Command Line',
        titleZh: 'docs/zh-CN/console-terminal.md · Web 控制台内置命令行（终端）使用指南',
        titleEn: 'docs/console-terminal.md · Built-in Console Command Line (Terminal) Guide',
        summaryZh:
            '管理后台内置终端的使用指南：入口与界面构成、实时流与水合回显纪律、快捷命令与常用命令对照表（status / doctor / recent / check / sync prune 只读预演），以及白名单执行与破坏性操作的分工边界。',
        summaryEn:
            'How to use the admin console built-in terminal: entry points and screen layout, the live stream and hydrated-text display discipline, quick commands and the common command reference (status / doctor / recent / check / sync prune preview), plus allow-listed execution and where destructive operations live.',
        sectionsZh: [
            '打开方式：顶栏终端图标 / 桌面浮标 / 记忆总览页按钮；展开状态跨页面保持',
            '界面构成：recall 与 Hook 实时流、最近写入单元回显（水合后正文）、任务卡片（重跑/清理）与流过滤',
            '常用命令对照表：status / doctor / recent / check / sync prune（只读预演）/ recall / watch 控制 / 执行清理与自愈的入口分工',
            '安全边界：白名单执行、子进程剥离云端凭据（本地模式零外呼）、回执路径与页面协同刷新',
        ],
        sectionsEn: [
            'Entry points: header terminal icon / desktop widget / overview page buttons; expanded state survives page switches',
            'Screen layout: recall & hook live stream, recent ingest echo (hydrated text), task cards (re-run / clean) and stream filters',
            'Common command reference: status / doctor / recent / check / sync prune (read-only preview) / recall / watch controls, and where applying maintenance lives',
            'Safety boundaries: allow-listed execution, credential-stripped child processes (zero egress in local mode), receipt paths and cross-page refresh',
        ],
        rawZh: rawConsoleTerminalZh,
        rawEn: rawConsoleTerminalEn,
    },
    {
        id: 'docs-benchmarks',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/benchmarks.md',
        icon: 'lucide-flask-conical',
        category: 'guide',
        badgeZh: '双基准 · 端侧复现',
        badgeEn: '2 Suites · On-Device Repro',
        url: `${MAIN_REPO_URL}/blob/main/docs/benchmarks.md`,
        shortTitleZh: '剑桥 ATM-Bench / LoCoMo 复现指南',
        shortTitleEn: 'Cambridge ATM-Bench / LoCoMo Repro Guide',
        titleZh: 'docs/zh-CN/benchmarks.md · 剑桥 ATM-Bench 与 LoCoMo 基准复现指南',
        titleEn: 'docs/benchmarks.md · Cambridge ATM-Bench & LoCoMo Reproducibility Guide',
        summaryZh:
            '默认评测臂为 local_plain（本地明文金库 + 本地检索，0 网络请求）；评测在专属评测命名空间内自动隔离 scope，并记录 corpus SHA-256、commit、逐题 Unit ID 与原始 JSONL。',
        summaryEn:
            'The default evaluation arm is local_plain (local plaintext vault + local retrieval, zero network requests). Runs are isolated in a dedicated evaluation namespace, recording corpus SHA-256, commit, per-question Unit IDs, and raw JSONL.',
        sectionsZh: [
            '默认臂 local_plain：本地明文金库 + FTS5/BM25 本地检索，0 网络请求、完全离线可复现',
            '评测命名空间：每次运行分配独立世代 scope，空闲自动回收；绝不触碰你的个人记忆空间',
            '溯源与复算：corpus SHA-256、commit、逐题 Unit ID 与原始 JSONL 独立复算；关闭 Answer/Judge 不等于离线',
        ],
        sectionsEn: [
            'Default arm local_plain: local plaintext vault + FTS5/BM25 local retrieval; zero network requests, reproducible fully offline',
            'Evaluation namespaces: each run gets its own generation scope, idle generations are reclaimed, and your personal memory space is never touched',
            'Provenance and recomputation: corpus SHA-256, commit, per-question Unit IDs, and independent raw JSONL recomputation; disabling Answer/Judge is not an offline run',
        ],
        rawZh: rawBenchmarksZh,
        rawEn: rawBenchmarksEn,
    },
    {
        id: 'docs-privacy',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/privacy.md',
        icon: 'lucide-lock-keyhole',
        category: 'legal',
        badgeZh: '本地隐私',
        badgeEn: 'Local Privacy',
        url: `${MAIN_REPO_URL}/blob/main/docs/privacy.md`,
        shortTitleZh: '隐私与本地数据说明',
        shortTitleEn: 'Privacy & Local Data',
        titleZh: 'docs/zh-CN/privacy.md · 隐私与本地数据说明（本地优先 + 云端能力预览）',
        titleEn: 'docs/privacy.md · Privacy & Local Data Statement (Local-First + Cloud Capability Preview)',
        summaryZh:
            '说明哪些数据永不离开本机（明文/沉淀记忆/主密钥 100% 留在本机 SQLite 金库）、本地金库纵深防御、四级受众隔离与一键自证命令，以及云端能力的申请制预览（端到端密文 · 面向高级与企业用户）。',
        summaryEn:
            'Explains what never leaves the machine (plaintext, consolidated memory, and the local key stay in the local SQLite vault), local vault defense-in-depth, audience isolation, and one-command self-verification, plus the application-based cloud capability preview (ciphertext-only, for advanced & enterprise users).',
        sectionsZh: [
            '哪些数据永不离开本机：原始对话、沉淀记忆、主密钥与本地索引全部留在本机 SQLite 金库（0 出网）',
            '本地金库纵深防御：单文件 SQLite + 0700/0600 权限 + 全盘加密继承 + 原位流式读取 + 四级受众隔离',
            '一键自证与数据主权：doctor / check / export / go test 自证命令，以及导出、备份、删除、迁移规程',
        ],
        sectionsEn: [
            'What Never Leaves Your Machine: raw conversations, consolidated memory, the local key, and local indexes all stay in the local SQLite vault (zero egress)',
            'Local Vault Defense-in-Depth: single-file SQLite with 0700/0600 permissions, full-disk encryption inheritance, source-in-place reads, and four audience tiers',
            'Self-Verification & Data Sovereignty: doctor / check / export / go test, plus export, backup, delete, and migrate procedures',
        ],
        rawZh: rawPrivacyZh,
        rawEn: rawPrivacyEn,
    },
    {
        id: 'docs-usb-portable',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/usb-portable.md',
        icon: 'lucide-hard-drive',
        category: 'guide',
        badgeZh: 'U 盘便携',
        badgeEn: 'USB Portable',
        url: `${MAIN_REPO_URL}/blob/main/docs/usb-portable.md`,
        shortTitleZh: 'U 盘便携模式与安全拔盘指南',
        shortTitleEn: 'USB Portable Mode & Safe Eject Guide',
        titleZh: 'docs/zh-CN/usb-portable.md · U 盘便携模式、自愈锚点与安全拔盘指南',
        titleEn: 'docs/usb-portable.md · USB Portable Mode, Host Anchor & Safe Eject Guide',
        summaryZh:
            '详解 rmem init --portable 一键制盘、跨机器挂载点漂移自愈（Host Anchor）、usb 强同步存储档位（FULL 同步、禁 mmap）与 rmem eject 安全拔盘规程。',
        summaryEn:
            'Covers one-command USB payload assembly (rmem init --portable), mount-drift-healing Host Anchor, the usb synchronous storage profile, and rmem eject.',
        sectionsZh: [
            '为什么不能直接复制源码仓：便携载荷（USB Payload）× 宿主自愈锚点（Host Anchor）两层架构',
            '三步上手：rmem init --portable 制盘、setup 一键接入与 rmem eject 安全拔盘',
            'usb 强同步存储档位（synchronous=FULL · mmap_size=0 · TRUNCATE 截断）与加密建议',
        ],
        sectionsEn: [
            'Two-Layer Architecture: Self-Contained USB Payload × Mount-Drift-Healing Host Anchor',
            'Three-Step Workflow: rmem init --portable, setup wiring, and rmem eject',
            'USB Synchronous Storage Profile (synchronous=FULL, mmap_size=0, WAL TRUNCATE) & Encryption Advice',
        ],
        rawZh: rawUsbPortableZh,
        rawEn: rawUsbPortableEn,
    },
    {
        id: 'docs-essay-memory-retrieval',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/essays/memory-retrieval-is-not-similarity-search.md',
        icon: 'lucide-lightbulb',
        category: 'overview',
        badgeZh: '理念随笔',
        badgeEn: 'Essay',
        url: `${MAIN_REPO_URL}/blob/main/docs/essays/memory-retrieval-is-not-similarity-search.md`,
        shortTitleZh: '记忆检索不是相似度检索',
        shortTitleEn: 'Memory Retrieval Is Not Similarity Search',
        titleZh: 'docs/zh-CN/essays/memory-retrieval-is-not-similarity-search.md · 记忆检索不是相似度检索（理念随笔）',
        titleEn: 'docs/essays/memory-retrieval-is-not-similarity-search.md · Memory Retrieval Is Not Similarity Search (Essay)',
        summaryZh:
            '为什么相似度不该做记忆检索的主键：提问与答案的不对称、复读对相似度的膨胀效应、记忆问题的事件结构，以及把某个具体的模型版本嵌进记忆格式的长期代价——以及按判别力排序的替代路线。',
        summaryEn:
            'Why similarity should not be the primary key of memory retrieval: the asymmetry of questions and answers, repetition inflation, the event structure of memory questions, and the long-term cost of embedding a model version into the memory format — plus the alternative of ranking by discriminating power.',
        sectionsZh: [
            '从“切块—嵌入—余弦排序”的默认管线讲起：它为什么答不对那类最重要的问题',
            '对称的相似度 vs. 不对称的问答：回声记录为何天然压倒携带新答案的解释记录',
            '记忆的问题是事件问题：时间、主体、变化应当长在索引里，而不是事后撒在距离上',
            '替代路线：按判别力（语料自信息 × 跨形式确定性对齐）排序——可质问、可复核、不绑定模型版本',
        ],
        sectionsEn: [
            'Starting from the chunk-embed-cosine default pipeline: why it misses the questions that matter most',
            'Symmetric similarity vs. asymmetric questions and answers: why echoing records naturally beat the record carrying the new answer',
            'Memory questions are event questions: time, actors, and change belong in the index, not sprinkled on top of a distance',
            'The alternative: ranking by discriminating power (corpus self-information × deterministic cross-form alignment) — interrogable, re-checkable, tied to no model version',
        ],
        rawZh: rawEssaySimilarityZh,
        rawEn: rawEssaySimilarityEn,
    },
    {
        id: 'docs-essay-semantic-bridging',
        scope: 'docs',
        repoFullName: 'rocareer/rolling-memory',
        path: 'docs/essays/semantic-bridging-creates-no-new-information.md',
        icon: 'lucide-book-open',
        category: 'overview',
        badgeZh: '理念随笔',
        badgeEn: 'Essay',
        url: `${MAIN_REPO_URL}/blob/main/docs/essays/semantic-bridging-creates-no-new-information.md`,
        shortTitleZh: '语义搭桥不产生新信息',
        shortTitleEn: 'Semantic Bridging Creates No New Information',
        titleZh: 'docs/zh-CN/essays/semantic-bridging-creates-no-new-information.md · 语义搭桥不产生新信息（理念随笔）',
        titleEn: 'docs/essays/semantic-bridging-creates-no-new-information.md · Semantic Bridging Creates No New Information (Essay)',
        summaryZh:
            '图谱、摘要与指代消解都在做同一件事——语义搭桥；桥不产生新信息，理解该发生在读取那一刻：数据处理不等式、预消化悖论，以及“摘要和原文从来不是两个证人”。',
        summaryEn:
            'Graphs, summaries, and coreference resolution all do one thing — semantic bridging; a bridge creates no new information, and comprehension belongs to the moment the question arrives: the data processing inequality, the pre-digestion paradox, and why a summary and its original are not two witnesses.',
        sectionsZh: [
            '什么算搭桥：嵌入、摘要、图谱、指代消解——把同一件事的不同说法拉近',
            '信息学上桥不生产信息：数据处理不等式、投影只换坐标系、私人指称不在任何模型的世界知识里',
            '指代消解是最好的例子：消解等于读上下文；查询里的指代由握有当前会话的宿主解决',
            '预消化悖论：写在前面为未知问题预付理解成本，写在读取那一刻只付一次——含“派生不增加独立证据”',
        ],
        sectionsEn: [
            'What counts as a bridge: embeddings, summaries, graphs, coreference — pulling different statements of the same thing closer together',
            'In information terms a bridge produces nothing: the data processing inequality, projections that change coordinates but not content, and private referents that no pretrained model knows',
            'Coreference is the best example: resolution equals reading context; references in queries are resolved by the host holding the current session',
            'The pre-digestion paradox: paying comprehension early for unknown questions vs. paying once at read time — with derived explanations adding no independent evidence',
        ],
        rawZh: rawEssayBridgingZh,
        rawEn: rawEssayBridgingEn,
    },
]
