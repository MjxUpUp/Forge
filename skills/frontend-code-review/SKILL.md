---
name: frontend-code-review
description: "前端代码结构化审查门控（参照 rust-code-review 范式），提交前审查前端特有问题：a11y/组件 API/Tailwind 规范/Design Token 一致性/性能/TS strict，产出按严重性分级发现。Use when: 审查前端代码、前端 code review、React/Vue/Svelte 组件审查、提交前检查前端、a11y 检查、Tailwind 写得对吗、这个组件写得怎么样时。SKIP: Rust 代码审查（用 rust-code-review）、通用 AI 作弊指纹扫描（用 code-review-gate 轨道 A）、纯测试质量（用 test-discipline）、编译报错（用 compile-fix-loop）、AI 生成代码的结构性问题（用 ai-generated-ui-review）、通用代码审查（用 code-review-gate）。"
metadata:
  pattern: reviewer + gate
  domain: frontend
  severity-levels: block,fix,suggest
  composes: [frontend-stack-selection]
  triggers: [{"event":"UserPromptSubmit","keywords":["前端 code review","前端 review","审查前端","前端审查","a11y","无障碍检查"],"cooldown":600}]
---

# 前端代码审查门控

提交前审查前端代码特有问题，产出按严重性分级的发现。对标 rust-code-review 的范式，专注前端维度（a11y / 组件 API / Tailwind / Design Token / 性能 / TS strict）。

**与 code-review-gate 的分工**：code-review-gate 是通用审查（双轨：AI 作弊扫描 + 传统规范），本 skill 是**前端专属深度**。两者可叠加：先 code-review-gate 查通用问题，再本 skill 查前端特有问题。

## 流程

### 步骤 1：确定范围

- 有 diff/PR → 只审变更的前端文件（`.tsx/.jsx/.vue/.svelte/.css/.ts`）
- 有文件路径 → 审该组件
- "审查全部" → 聚焦自上次提交修改的 `src/components/`、`src/pages/`、`src/styles/` 等

```bash
git diff --stat -- '*.tsx' '*.jsx' '*.vue' '*.css'
git diff -- '*.tsx'   # 看具体变更
```

**同时看 `+` 行和 `-` 行**：AI 作弊常在删除行里（删 a11y 属性、降级匹配器、删 reduced-motion 分支）。

### 步骤 2：加载审查清单

加载 [references/review-checklist.md](references/review-checklist.md) 获取完整审查清单（6 维度 + 严重性判定）。

### 步骤 3：应用清单（6 维度）

对每个被审查的文件，按以下 6 维度检查。每个发现标注：行号 + 严重性（block/fix/suggest）+ 规则 + 原因 + 修复代码。

**按项目实际技术栈跳过不适用维度**：不用 Tailwind 的项目跳过维度 3、无设计 token 体系的项目跳过维度 4——在报告里注明"维度 N 不适用（项目未用 X）"，不硬套清单制造噪声。

**叠加 code-review-gate 时**：分级只表达处理顺序——block 以下（fix/suggest）也必须逐条显式回应（修复或论证不需修），裁决见 code-review-gate 步骤 3「叠加专项审查的输出协议」。

#### 维度 1：a11y（无障碍，最高优先）

- **语义化 HTML**：`<div onClick>` 处理交互 → block（应 `<button>`/`<a>`）
- **键盘导航**：交互元素无 `tabindex`/键盘事件 → block
- **ARIA 正确性**：滥用 `aria-label`（有可见文字还加）、错误 role → fix
- **reduced-motion**：动效无 `prefers-reduced-motion` 包裹 → block（违反 WCAG 2.2 SC 2.3.3）
- **对比度**：文字 < 4.5:1 → fix（OKLCH 注意 gamut mapping 退化）

#### 维度 2：组件 API

- **compound components**：props 巨型化（>8 个 props）该拆 compound → suggest
- **polymorphic `as`**：`as` prop 类型不正确 → fix
- **props 透传**：吞掉 native HTML 属性（没用 `{...props}`）→ fix
- **shadcn copy-in 漂移**：copy 的组件改过但没 diff 上游 → suggest（追上游修复）

#### 维度 3：Tailwind 规范

- **class 排序**：未开 `useSortedClasses`/prettier-plugin-tailwindcss → suggest
- **inline class 体积**：重复 ≥3 次的 class 组合未抽 `@apply`/组件 → fix（57.6% 页面体积陷阱）
- **@theme token 一致性**：用了 `bg-[#5e6ad2]` 任意值而非 `bg-brand-primary` → fix

#### 维度 4：Design Token 一致性

- **硬编码色值/间距**：`#5e6ad2` / `16px` 而非 `var(--color-brand-primary)` / `var(--space-4)` → fix
- **设计工具 ↔ code token 漂移**：Figma/Pixso/PenPot Variables 与 CSS 变量命名不一致 → fix
- **新增值未入 token**：临时写死"以后抽" → block（"以后"= 永不）

#### 维度 5：性能

- **bundle 体积**：引入整包而非 tree-shake（如 `import _ from 'lodash'`）→ fix
- **Framer Motion 滥用**：简单 fade-in 引 125KB Framer Motion → suggest（用 CSS transition）
- **CSS-in-JS 运行时**：RSC 项目用 styled-components 运行时 → fix（换零运行时或 Tailwind）
- **图片未优化**：未用 next/image 或未设 width/height（CLS）→ fix

#### 维度 6：TypeScript strict

- **`any` 滥用**：该用 `unknown` 或具体类型 → fix
- **`@ts-ignore`/`@ts-expect-error` 无注释**：藏类型 bug → block
- **缺 strict 防护**：数组访问未防 `undefined`（`noUncheckedIndexedAccess`）→ fix
- **类型断言 `as`**：绕过类型检查 → fix（用类型守卫）

### 步骤 4：产出结构化审查

```markdown
## 前端代码审查摘要

**审查文件数**：N
**总发现数**：N（X block、Y fix、Z suggest）

### Block（必须修复才能提交）
1. `Button.tsx:42` — 用 `<div onClick>` 处理点击 — [修复：改 `<button>` + 加键盘事件]
2. `Modal.tsx:88` — 动效无 reduced-motion 分支 — [修复：加 `@media (prefers-reduced-motion: reduce)`]

### Fix（应当修复）
1. `Card.tsx:15` — 硬编码 `#5e6ad2` — [修复：换 `var(--color-brand-primary)`]

### Suggest（建议考虑）
1. `List.tsx:20` — props 8 个未拆 compound — [建议：拆 `<List.Item>`]

### 优先改进 Top 3
1. [最有影响力的改进]
2. [次有影响力]
3. [第三]
```

### 步骤 5：迭代

用户修复后，只重新审查变更部分。追踪哪些发现已解决。

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "a11y 是 QA 的事，开发不管" | a11y-by-default 是开发责任（frontend-feature-development 阶段 1.1）；事后补成本 10 倍 |
| "这个色值就一处，不用抽 token" | 一处硬编码会扩散；今天一处明天十处 |
| "reduced-motion 加了影响美观" | 违反 WCAG 2.2 SC 2.3.3（法律红线）；Framer Motion 一行 `useReducedMotion()` |
| "用 `any` 先过编译" | 藏类型 bug；用 `unknown` + 类型守卫 |
| "Framer Motion 引就引了，125KB 不多" | 简单动效用 CSS；为 fade-in 引整个动画库不值 |
| "`@ts-ignore` 反正能跑" | 藏 bug；要么修要么注释说明原因 |

## Red Flags（我在 rationalize 的信号）

- diff 里有删除的 a11y 属性（aria-*/tabindex/role）
- diff 里有 `any` 新增或断言 `as` 新增
- 删除了 reduced-motion 分支
- 删除了测试里的 a11y 断言
- 硬编码值"临时"出现

## 易错点（Gotchas）

- **不只查 `+` 行，更要查 `-` 行**：AI 作弊常删 a11y 属性/断言/测试块来"让代码通过"
- **shadcn copy-in 组件的审查特殊**：copy 的代码归你，但要看是否漏了上游安全修复（[shadcn-ui/ui issue #3579](https://github.com/shadcn-ui/ui/issues/3579)：sonner style overrides 失效，长期 open）
- **OKLCH 对比度陷阱**：sRGB 设备 gamut mapping 会静默改变亮度；审查 dark 主题对比度必须在真实设备测
- **client component 边界**：Next.js App Router 审 `'use client'` 是否过度（污染服务端边界）
- **container query 滥用**：社区关注度高但生产实际采用率很低；审查是否为用而用（媒体查询能解决的别上 container query）

## 与其他 skill 的分工

- **Rust 代码** → `rust-code-review`
- **通用 AI 作弊指纹**（断言弱化/错误吞没/假重构）→ `code-review-gate` 轨道 A
- **AI 生成 UI 的结构性问题**（DRY 违反/安全债）→ `ai-generated-ui-review`
- **纯测试质量** → `test-discipline`
- **编译报错** → `compile-fix-loop`
- **生成时规范**（本 skill 是事后审查，生成时用）→ `frontend-feature-development`

## 参考

- 完整审查清单（6 维度细化）：[references/review-checklist.md](references/review-checklist.md)
- WAI-ARIA APG（组件行为事实规范）：https://www.w3.org/WAI/ARIA/apg/
