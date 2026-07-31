---
name: ai-ui-generation-workflow
description: "用 AI 工具（v0/Bolt.new/Lovable/Replit/Cursor）生成能上生产的前端 UI 的工程化工作流，对抗'原型即生产'伪命题。涵盖 Figma-to-code（Builder.io Visual Copilot）与 Pixso-to-code（官方 Pixso MCP，AI 原生路径）。Use when: 用户要用 v0/Bolt/Lovable/Replit 生成页面或应用、prompt-to-UI、用 Cursor 写前端、AI 出原型然后改、Figma-to-code、Pixso-to-code、Pixso MCP 接入、问'怎么用 AI 工具做这个 UI'、vibe coding 时。SKIP: 审查已生成的 AI 代码（用 ai-generated-ui-review）、手写组件（用 frontend-feature-development）、选技术栈（用 frontend-stack-selection）、纯调研 AI 工具（用 research-workflow）。"
metadata:
  pattern: pipeline + gate
  domain: frontend
  composes: [ai-generated-ui-review, frontend-feature-development, frontend-stack-selection]
---

# AI UI 生成工作流

用 AI 工具生成**能上生产**的前端 UI，而非停留在原型级。核心立场：**"原型即生产"是伪命题**——arXiv 2603.28592 分析 302,579 个 AI 生成 commit 发现 89.3% code smell；Lovable 官方文档自己承认"不要复用共享组件"；AI 代码相关 CVE 从 2026-01 的 6 个涨到 74 个。

AI 生成 = 起点，不是终点。本 skill 教如何用 SDD（spec-driven development）+ spec 先行 + 正确工具分工，把原型变成可维护的生产代码。

## 铁律：Addy Osmani 2026 LLM 工作流（Google Chrome 团队定调）

verbatim 核心立场：

> "treat the LLM as a powerful pair programmer that requires clear direction, context and oversight rather than autonomous judgment."

**Anthropic 内部约 90% 的 Claude Code 代码由 Claude Code 自己写——但仍有 oversight。**

五步工作流（生成任何 UI 前对照）：
1. **spec 先于代码**：先写 spec.md（要做什么、约束、验收标准），再让 AI 生成
2. **小步迭代**：一次生成一个组件/一个功能，不一次性生成整个应用
3. **context packing**：把设计系统/已有组件/Token/约定作为 context 喂给 AI
4. **绝不盲信输出**：把 AI 输出当 junior developer 代码审查
5. **验证再验证**：生成后跑 a11y/构建/类型检查，过了才用

## 阶段 0 — 选对工具（分工明确）

不同工具产出层级不同，选错工具 = 白干：

| 工具 | 定位 | 产出层级 | 最佳场景 |
|---|---|---|---|
| **v0.app** (Vercel) | 组件 + agentic 全栈 | 组件级最强（shadcn/Radix/Framer 锁定） | 单个组件/区块生成 |
| **Bolt.new** (StackBlitz) | 浏览器 WebContainer 全栈 | 完整全栈应用 | 从零全栈 MVP（无需本地环境） |
| **Lovable** | Chat + Supabase + 可视化编辑 | 设计导向全栈 SaaS | 非技术 PM + 设计导向 MVP |
| **Replit Agent 3** | 云端 IDE + Agent + 托管 + DB | 完整应用 + 部署 | 一站式（含 DB/托管） |
| **Cursor** | 桌面 IDE，VS Code fork | 现有代码库 agentic 编辑 | **生产化改造的主力**（改 AI 生成的代码） |

**选型决策**：从零原型 → v0（组件）/ Bolt/Lovable（全栈）；改造 AI 产出上生产 → **Cursor**（必用）。

→ 选型详细对比见 **frontend-stack-selection**。

## 阶段 1 — Spec 先行（SDD 闭环）

**生成任何 UI 前，先写 spec.md。** 这是 spec-driven development，对抗 vibe coding 的核心纪律。

spec.md 至少含：
- **功能描述**：用户操作 → 预期结果（一句话）
- **约束**：技术栈（React/Vue）、组件库（shadcn/Radix）、design token 来源
- **验收标准**：a11y 要求、响应式断点、交互态、性能预算
- **不复用清单**：明确哪些已有组件**不要**改动（防止 AI 乱改全局）

SDD 工具（任选其一对接 Cursor/Claude Code/Cline）：
- **spec-kit**（GitHub，114K stars，开源，对接 Copilot/Claude Code/Gemini CLI）
- **Amazon Kiro**（AWS，商业化）
- **Tessl**（商业化）

四阶段闭环：`spec.md → plan → codegen → 验证`。**没写 spec 就让 AI 生成 = vibe coding = 制造技术债。**

## 阶段 2 — Prompt 模板（实操）

### 设计 context 来源（生成前先定，三选一）

生成 UI 前先确定 design token / 风格 context 从哪来，否则 AI 会瞎编色值：

| 场景 | context 来源 |
|---|---|
| 项目已有 token | 喂 `@/styles/tokens.css`（模板 A/B 默认） |
| 有 Figma / Pixso 设计稿 | 走模板 C（设计稿转代码） |
| **无 token 无设计稿，但要某品牌风格** | 复制对应品牌 `DESIGN.md` 到项目根目录，prompt 指明"按根目录 `DESIGN.md` 的 token 生成" |

第三种场景的品牌 DESIGN.md 取自 awesome-design-md 仓库（VoltAgent/awesome-design-md）的 `design-md/<slug>/DESIGN.md`（74 个真实品牌，slug 查 **frontend-aesthetics-execution** 阶段 1.5 索引）。发现方式：环境变量 `DESIGN_MD_ROOT` 指向仓库克隆根，或查找当前工作区/常用代码目录下的 `awesome-design-md`；未克隆时先 `git clone https://github.com/VoltAgent/awesome-design-md`；仓库不可用或品牌未命中时 fallback 到 **frontend-aesthetics-execution** 阶段 1 的通用风格模板——这是 awesome-design-md 官方用法（"Copy a site's DESIGN.md into your project root, tell your AI agent to use it"）。

**注意**：DESIGN.md 是 hex + Stitch YAML，不是项目 token。生成后必须走阶段 3 步骤 1 抽 token → **design-system-workflow** 转 OKLCH，**别让 AI 把 DESIGN.md 的 hex 硬编码进组件**。

### 模板 A：组件级生成（v0/Cursor）

```text
生成一个 [组件名] 组件，用于 [场景]。

技术约束：
- React 18+ / TypeScript / Tailwind v4 / shadcn/ui（Radix 底层）
- 复用项目已有组件：[列出 @/components/ui/ 下的可用组件]
- design token 从 @/styles/tokens.css 取（色值用 var(--color-*)，不硬编码）

功能要求：
- [交互态：hover/focus/active/disabled/loading/error]
- [响应式：mobile-first，断点 sm/md/lg]
- [a11y：键盘导航 + ARIA + prefers-reduced-motion]

验收标准：
- axe-core 0 violation
- TypeScript strict 通过
- 不引入新依赖（除非 [列出允许的]）

不要：
- 不要改 [@/components/ui/ 下已有组件]
- 不要用 any / @ts-ignore
```

### 模板 B：全栈生成（Bolt/Lovable/Replit）

```text
构建一个 [应用名]，[一句话功能]。

技术栈：[Next.js/Remix] + [Supabase/Drizzle] + shadcn/ui + Tailwind v4

核心功能（按优先级）：
1. [P0 功能 + 验收标准]
2. [P1 功能]
3. [P2 功能]

数据模型：[描述实体 + 关系]

安全约束（必守）：
- API key 只在服务端，绝不写前端代码
- 数据库访问必须经过认证层
- 用户输入必须校验（zod）

不要：
- 不要复用跨 role 的共享组件（除非显式允许）
- 不要生成 mock data 当生产数据
```

### 模板 C：设计稿转代码（按设计工具选路径）

设计稿转代码工具**依赖设计工具**，不能混用：

| 设计工具 | AI 转码路径 | 说明 |
|---|---|---|
| **Figma** | **Builder.io Visual Copilot**（专用 LLM 插件） | 仅 Figma，识别 button/card/nav 模式 + 设计系统上下文；配合 Cursor 二次编辑 |
| **Pixso** | **官方 Pixso MCP**（AI 原生，推荐） | MCP 是 Cursor/Claude/Cline 原生协议，比插件式集成更顺；详见下方接入 |
| **PenPot** | 导出 SVG/HTML 起点手写 | 无成熟 AI 转码工具 |

#### Pixso MCP 接入（2026 实证，国内团队首选）

Pixso 有**官方 MCP**，两条路：

**路径 A：本地 MCP（18 工具，需 Pixso 桌面端运行）**
- Server: `http://127.0.0.1:3667/mcp`（HTTP + SSE，mcp-session-id 鉴权）
- 18 个工具含 `design_to_code` / `get_node_dsl` / `get_variables` / `get_variable_sets` / `get_local_styles` / `get_all_components` / `create_instance` / `set_bound_variables` / `code_to_design` / `refine_generated_code` 等
- 支持框架：React / Vue / HTML / Flutter / ArkUI
- 可用 jiaweiwei1961/pixso-design-skill（Claude Code skill 封装，含 CLI）

**路径 B：云端 MCP（6 工具，不开 Pixso 也能用）**
- Server: `https://pixso.cn/api/mcp/mcp`（Streamable HTTP，Token Header 鉴权）
- 需 Personal Access Token（Pixso web → 用户中心 → Personal Access Tokens）
- 6 个核心工具：`getCode` / `getNodeDSL` / `get_local_styles` / `get_variable_sets` / `get_variables` / `get_variants`
- 可用 jiaweiwei1961/pixso-remote-skill

**4 IDE 配置（实证可用）**：

```bash
# Claude Code
claude mcp add --transport http --header "Token:YOUR_TOKEN" pixso-remote https://pixso.cn/api/mcp/mcp
```
```json
// Cursor (~/.cursor/mcp.json)
{"mcpServers":{"pixso-remote":{"url":"https://pixso.cn/api/mcp/mcp","headers":{"Token":"YOUR_TOKEN"}}}}
```
```json
// Cline (VS Code) — autoApprove getCode/getNodeDSL
{"pixso-remote":{"type":"streamableHttp","url":"https://pixso.cn/api/mcp/mcp","headers":{"Token":"YOUR_TOKEN"}}}
```
```json
// Windsurf (~/.codeium/windsurf/mcp_config.json)
{"mcpServers":{"pixso-remote":{"serverUrl":"https://pixso.cn/api/mcp/mcp","headers":{"Token":"YOUR_TOKEN"}}}}
```

**为什么 Pixso MCP 比 Builder.io 更贴 AI coding**：MCP 是 Cursor/Claude/Cline 原生协议，agent 直接调工具读设计稿（含 tokens/styles/DSL），不需复制粘贴设计链接到插件；Figma 还在追赶 MCP（Figma Dev Mode 付费 + 第三方 Figma MCP）。但 Pixso MCP 生态较新（社区 repo 多为 2026 新建、0★），生产前需验证稳定性。

**通用纪律**："一键完美产出"不存在，无论 Figma 还是 Pixso 路径，设计稿转代码都应作为起点而非终点，配合 Cursor/Onlook 二次编辑。

### 模板 D：反向路径（代码 → 设计图供审核）

双向能力**真实存在但有硬限制**，常用于把现有前端反向导入设计工具供用户评审/批注。

#### 按设计工具选反向工具

| 设计工具 | 反向工具 | 输入 | 质量 |
|---|---|---|---|
| **Figma** | **html.to.design**（Builder.io 旗下，付费插件最成熟）或 **Builder.io HTML to Design** | URL / HTML / Chrome 扩展捕获 | 多 viewport + dark/light 主题，近原品质 |
| **Figma（开源）** | **Yueyin-Tql/htmlToFigma**（MCP server，对标 html.to.design） / **sergcen/html-to-figma** / **Floristeady/html-to-figma**（含 Cursor 集成） | URL / HTML/CSS，Puppeteer 渲染 | Flexbox → Auto Layout，SVG/字体自动转 |
| **Pixso** | **官方 Pixso MCP `code_to_design`**（本地 18 工具之一） + `pixso_import.py` 批量脚本（出自外部仓库 jiaweiwei1961/pixso-design-skill 的 `scripts/pixso_import.py`） | HTML 字符串 / HTML 目录 / ZIP | 需处理 95KB 分块，输出扁平图层 |

#### 通用硬限制（反向路径的核心约束）

1. **只吃 HTML，不吃 React/Vue 源码**——反向工具全部要求静态 HTML。React/TSX 组件必须先 build 成静态 HTML 才能导入。
   - 纯静态页（落地页/文档站）：直接抓 HTML 顺畅
   - 动态 SPA：先用 Playwright/Puppeteer 跑各路由抓快照，或 `vite build` + 静态渲染
   - **Tauri 桌面应用（如 DevWorkBench 类）最麻烦**：要用 Tauri webview 截图或独立 Web 构建再抓 HTML
2. **Pixso MCP 请求大小限制 ~95KB**（超 101KB 返回 HTTP 413）——复杂页面需 `pixso_import.py`（外部仓库 jiaweiwei1961/pixso-design-skill 的 `scripts/pixso_import.py`）自动分批（会话过期自动刷新 + 重试 3 次）
3. **转换质量取决于 HTML 语义化**——内联 style 最准；class-based CSS 靠正则提取会丢部分继承关系
4. **输出是扁平化图层，不是交互组件**——导入后是 Frame/Text/Rectangle，不是设计工具的 Component/Variant，要手动封装才可复用

#### 完整反向工作流（React/Tauri 项目 → 设计图审核）

```
现有 React/TSX 代码
  ↓ vite build + 静态渲染（或 Playwright 抓路由 HTML）
静态 HTML 文件（每路由一个）
  ├─ Figma: html.to.design 插件粘贴 URL/HTML，或 htmlToFigma MCP
  └─ Pixso: pixso_import.py ./html-pages（jiaweiwei1961/pixso-design-skill `scripts/pixso_import.py`，自动分批+打标签+垂直排列）
设计工具设计稿（可编辑）
  ↓ 用户在设计工具里批注/审核/调整细节
审核反馈 → 代码改造
```

**关键判定**：反向是"评审/快照"用途，不是"代码→设计→再回代码"的循环。导入的设计图是当前代码的静态映射，后续改动仍以代码为准、定期重新快照重新导入。

## 阶段 3 — 生产化改造（原型 → 生产，必经 5 步）

AI 生成的原型要上生产，必经以下改造（在 Cursor 里做）：

1. **抽 token**：把所有硬编码色值/间距替换成 design token（→ design-system-workflow）
2. **修 DRY 违反**：AI 生成倾向重复组件（Lovable 官方承认"不要复用"）；手动抽公共组件
3. **补 a11y**：AI 生成普遍缺键盘导航/ARIA/reduced-motion（→ frontend-feature-development 阶段 1.1）
4. **安全审查**：查 API key 前端暴露/无认证/SQL 拼接（→ ai-generated-ui-review）
5. **接 spec-kit 验证**：用 spec.md 反向校验产出是否满足验收标准

**红线**：把 v0/Bolt 原型直接 push 到生产。VentureBeat 报道 v0 早期"原型无法上生产"，Vercel 自己承认需 "rebuild v0 to tackle the 90% problem"。

## 阶段 4 — 可维护性自检（声称"能用"前）

生成 + 改造后，过 ai-generated-ui-review 的 6 类可维护性评估清单（不在此重复，加载该 skill）。核心指标：

- **重复率**：相同/近似组件出现次数（AI 反 DRY 倾向）
- **圈复杂度**：单个组件 > 10 警告
- **包大小**：引入的依赖是否必要（Framer Motion 125KB / Spline runtime 544KB）
- **安全**：SAST 扫描（Veracode/Endor Labs/Cycode 嵌 CI）

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "v0 生成的直接能用" | 89.3% code smell；原型级，必经生产化改造 5 步 |
| "AI 写得比我快，不用审查" | LLM 是"需监督的 pair programmer"（Addy Osmani）；CVE 6→74 |
| "先 vibe coding 出原型，以后重构" | "以后"= 永不；技术债只增不减；SDD 从一开始更省 |
| "Lovable 说不用复用组件，那就照做" | 那是承认它的生成有结构性反 DRY 问题；你要手动抽公共组件 |
| "一次性生成整个应用更快" | 得到"10 devs 没沟通各写各的"不一致代码；小步迭代 |
| "Figma-to-code / Pixso-to-code 一键搞定" | 准确率仍需大量手工修正；作为起点配合 Cursor 二次编辑 |

## Red Flags（我在 rationalize 的信号）

- 没写 spec 就让 AI 生成
- 一次性 prompt 让 AI 生成整个应用
- 把 v0/Bolt 原型直接 commit 到生产分支
- 不查 API key 暴露就用 AI 生成的全栈代码
- AI 生成的组件不抽 token、不修 DRY
- 用"AI 生成的"当借口跳过 a11y/安全审查

## Gotchas

- **v0 UI 质量可能退化**：Vercel Community 帖 "Is it just me, or has the UI quality in v0 gotten worse?"；生成后必跑可维护性评估（jscpd 重复率 + grep 密钥 + 圈复杂度），不靠眼看——详见 ai-generated-ui-review 的 6 类清单
- **shadcn registry 供应链风险**：第三方 registry 组件绕开 npm 审计，有 RCE 攻击面；只用可信 registry
- **AI 生成代码的"看起来对"陷阱**：通过功能测试 ≠ 适合生产（arXiv 2508.14727）；必跑 SAST 扫描（gitleaks/semgrep 免费，或 Veracode/Endor Labs 商用）+ 按 ai-generated-ui-review 6 类逐项判定
- **Cursor Memories 替代了 Notepads**：2025Q4 起 Cursor 用 Memories 持久化项目 context；配合 `.cursor/commands/` + `.mdc` rules 文件
- **Lovable 早期无后端/无代码导出**：限制全栈用途；选它要确认当前版本能力
- **Pixso MCP 生态较新**：官方 MCP 真实存在但社区封装 repo（pixso-design-skill / pixso-remote-skill / pixso-advanced-mcp）多为 2026 新建、0★，生产前需跑通验证（token 鉴权、大 frame 性能、style 提取完整度）；pishikin/pixso-advanced-mcp 是因官方 MCP 大 frame 卡顿而生的本地替代
- **Pixso MCP 本地 vs 云端取舍**：本地 18 工具功能全但需桌面端运行；云端 6 工具不开应用但功能子集 + 需 token + 依赖网络

## 与其他 skill 的分工

- 审查 AI 生成的代码（可维护性/安全/DRY） → **ai-generated-ui-review**（生成后必走）
- 手写或改造组件 → **frontend-feature-development**
- 选 AI 工具 + 技术栈 → **frontend-stack-selection**
- 建 design token（AI 产出的 token 抽取） → **design-system-workflow**
- 提交前最终审查 → **frontend-code-review**
- 通用 AI 作弊指纹（断言弱化/错误吞没） → **code-review-gate** 轨道 A

## 参考

- Addy Osmani 原文：https://addyosmani.com/blog/ai-coding-workflow
