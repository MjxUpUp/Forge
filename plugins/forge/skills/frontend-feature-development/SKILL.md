---
name: frontend-feature-development
description: "前端功能开发全程纪律：从 spec 到组件实现到自验证，把 a11y/token-only/组件 API 范式内建到生成时而非事后审查。Use when: 写 React/Vue/Svelte 组件、实现前端功能、做 UI 界面、加交互、用 Tailwind 实现布局、生成组件、写前端页面、改造前端代码时。SKIP: 跨 Rust+前端全栈（用 dev-workflow 编排 + backend-development 处理后端）、选什么技术栈（用 frontend-stack-selection）、用 v0/Bolt/Lovable 等 AI 工具生成（用 ai-ui-generation-workflow）、提交前审查（用 frontend-code-review）。"
metadata:
  pattern: inversion + pipeline + gate
  domain: frontend
  composes: [frontend-stack-selection, frontend-code-review, test-discipline, verification-driver]
---

# 前端功能开发纪律

动手写前端组件/页面/功能的全程门控。**核心思想：把规范内建到生成时，而不是写完被 frontend-code-review 打回。** a11y、token-only、组件 API 范式、reduced-motion——第一行代码就写对。

## 阶段 0 — 写前确认（Inversion 门控）

**动键盘前确认 4 件事，缺一不可：**

1. **设计稿/交互态来源**：有 Figma/Pixso/PenPot/截图，还是文字描述？复杂交互（drag/combobox/date-picker）是否有明确的状态机？
2. **design token 来源**：项目已有 design token（CSS 变量/Tailwind @theme）吗？色值/间距/阴影从哪取？**没有 token 先建 token，不临时硬编码。** 项目无 token 但用户指定了品牌风格（Linear/Stripe/Apple…）→ 从 awesome-design-md 仓库的 `design-md/<slug>/DESIGN.md`（slug 与仓库发现方式/fallback 查 **frontend-aesthetics-execution** 阶段 1.5）提取 hex 作 token 起点，但**必须先进 token 体系**（→ design-system-workflow 转 OKLCH）再写组件，绝不把 DESIGN.md 的 hex 直接硬编码进组件
3. **复用 vs 新建**：项目里有没有类似组件可复用/扩展？先 `grep` 搜，不重复造。
4. **响应式 + 交互态边界**：断点有哪些？hover/focus/active/disabled/loading/error 怎么处理？a11y 要求（键盘导航/ARIA）明确吗？

**确认前不要写实现。** → 建 design token 用 **design-system-workflow**；选组件库底层用 **frontend-stack-selection**。

## 阶段 1 — 生成时强约束（核心，区别于事后审查）

写每一行代码时遵守以下约束。这些是 frontend-code-review 的事后检查项**前移到生成时**：

### 1.1 a11y-by-default（最高优先）

- **语义化 HTML 优先**：用 `<button>` 不用 `<div onClick>`；用 `<nav>/<main>/<article>` 不用全 `<div>`
- **键盘导航**：所有交互元素可 Tab 到、可 Enter/Space 触发；自定义组件实现 `tabindex` + 键盘事件
- **ARIA 正确性**：参照 WAI-ARIA APG（https://www.w3.org/WAI/ARIA/apg/）的 dialog/menu/combobox 等复杂 pattern；不滥用 `aria-label`（有可见文字别加）
- **reduced-motion 必写**：所有动效用 `@media (prefers-reduced-motion: reduce)` 包裹，或用 Framer Motion 的 `useReducedMotion()`；完整实现模板（CSS universal reset + JS matchMedia + 静态等价物）唯一真相源：frontend-aesthetics-execution「阶段 4 — 动效 a11y 门」节，此处不复制
- **对比度**：文字 ≥ 4.5:1（WCAG AA）；OKLCH 的 sRGB gamut mapping 退化警告唯一真相源：design-system-workflow「阶段 3 — OKLCH 双主题切换」节，此处不复制

### 1.2 token-only（禁止硬编码）

```css
/* ❌ 硬编码 */
.card { background: #5e6ad2; padding: 16px; box-shadow: 0 4px 12px rgba(0,0,0,0.1); }

/* ✅ token */
.card { background: var(--color-brand-primary); padding: var(--space-4); box-shadow: var(--shadow-md); }
```

- 色值 / 间距 / 圆角 / 阴影 / 字号 / 动效曲线——**全部从 design token 取**
- 没有 token 的值，先加 token（→ design-system-workflow），不临时写死
- Tailwind 项目：用 `@theme` 定义 token，组件用 `bg-brand-primary` 等语义类
- 铁律唯一真相源：design-system-workflow「铁律：token-only 是底线」节；本节是生成时的执行约束

### 1.3 组件 API 范式（参照 Base UI/Radix）

- **compound components**：复杂组件拆 `<Card>`/`<Card.Header>`/`<Card.Body>`，不靠 props 巨型化
- **polymorphic `as` prop**：需要渲染成不同元素时（如 Button 可渲染成 `<a>`），用 `as` prop + 正确类型
- **props 透传**：用 `{...props}` 透传到根元素，不吞掉 native HTML 属性；或用 Radix 的 `asChild` 模式
- **slot pattern**：可定制子节点渲染（如 Avatar 的 fallback/image）

### 1.4 Tailwind v4 规范

- **@theme 定义 token**：所有自定义色彩/间距在 `@theme` 里定义，组件用语义类引用
- **class 排序**：开 Biome `useSortedClasses` 或 prettier-plugin-tailwindcss，避免 class 顺序混乱
- **避免 inline class 体积陷阱**：重复 ≥3 次的 class 组合抽成 `@apply` 或组件（体积数据论据唯一真相源：frontend-stack-selection「反主流 framing」节，此处不复制）
- **container query 优先**：响应式用 `@container`（Tailwind v4 一等公民），不只靠 `@media`

### 1.5 RSC 兼容（Next.js App Router 项目）

- **client component 推到叶子节点**：`'use client'` 只加在最小必要的交互组件上，不污染服务端边界
- **Context 只在 client 用**：ThemeProvider 等 Context Provider 是 client component，其子树自动 deopt
- **数据获取在 server**：fetch/DB 操作留在 server component，client 只接收 props

## 阶段 2 — 实现流程（Pipeline）

```
前端功能：
- [ ] 1. 类型定义（props/state/事件签名）
- [ ] 2. 结构（语义化 HTML + compound components）
- [ ] 3. 样式（token-only + Tailwind v4 规范）
- [ ] 4. 交互（状态管理 + 事件处理 + 键盘导航）
- [ ] 5. a11y（ARIA + reduced-motion + 对比度）
- [ ] 6. 响应式（container query + 断点）
- [ ] 7. 自验证（Lighthouse + axe-core + 键盘实测）
```

**门控**：每步过门才进下一步。a11y（步骤 5）没做完不算完成，不能进响应式。

## 阶段 3 — 自验证门控（声称"完成"前必跑）

**门控：声称组件完成前，必须有刚跑出来的证据。**

- **a11y**：跑 `axe-core` 或浏览器 Lighthouse Accessibility 评分；或键盘 Tab 遍历所有交互元素实测
- **响应式**：至少 3 个断点（mobile/tablet/desktop）实测，不只靠 DevTools 模拟
- **TypeScript**：`tsc --noEmit` 通过，无 `any` 滥用、无 `@ts-ignore`
- **构建**：`npm run build` 通过，无 console error/warning

**红线**：用"应该没问题"代替实际运行 / 跳过 a11y 自检 / 只在桌面 Chrome 测就说"响应式做好了"。

→ 详细端到端验证用 **verification-driver**；断言守卫用 **test-discipline**。

## 实现细节参考（references）

阶段 2 实现时的状态归属、性能、命名、自查清单速查见 [references/component-checklist.md](references/component-checklist.md)（合并自原 frontend-development 的规范速查）：
- 状态决策树（local / lifted / global store / 服务端缓存 / 表单态）
- 性能自检清单（key / hooks 依赖 / 虚拟化 / lazy load / bundle）
- 命名（kebab-case + 功能描述）+ Post-Generation 自查 + 易错 Gotchas

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "a11y 以后再加" | 事后补 a11y 成本是生成时的 10 倍；且容易漏（reduced-motion/键盘导航） |
| "这个色值先用着，以后抽 token" | "以后" = 永不；硬编码一旦散落，重构要 grep 全仓库 |
| "用 `<div onClick>` 简单点" | a11y 灾难（不可 Tab/无键盘事件/屏幕阅读器不识别）；用 `<button>` |
| "动效很好看，先不管 reduced-motion" | 违反 WCAG 2.2 SC 2.3.3（法律红线）；Framer Motion 一行 `useReducedMotion()` |
| "组件不复杂，不用 compound" | props 巨型化是维护地狱；3 个以上子区域就拆 compound |
| "client component 包整个页面省事" | 污染服务端边界，丢掉 RSC 零 JS 优势；推到叶子节点 |
| "TypeScript 报错先 @ts-ignore" | 藏类型 bug；查清根因或正确标注 unknown |

## Red Flags（我在 rationalize 的信号）

- 没确认 token 来源就开始写样式
- 没搜复用就新建组件
- 用 `<div onClick>` / `<span onClick>` 处理交互
- 动效没写 reduced-motion 分支
- 色值/间距写死而非走 token
- 只在桌面测响应式就说"做好了"
- `@ts-ignore` / `any` 滥用

## Gotchas

- **shadcn copy-in 组件改了要追上游**：copy 进来的组件如果上游修了 bug，你不会自动得到；定期 diff 同步（用 `shadcn diff`）
- **Radix 的 `asChild` 与 React 19 的 ref forwarding**：React 19 ref as prop 后，`asChild` 模式需更新写法
- **Framer Motion 125KB 是纯负担**（未复用场景）：简单动效用 CSS `transition`/`@keyframes`，别为 fade-in 引整个 Framer Motion
- **OKLCH gamut mapping 静默退化**：警告全文唯一真相源：design-system-workflow「阶段 3 — OKLCH 双主题切换」节，此处不复制；双主题必须实测对比度
- **container query 不是银弹**：组件级响应式用它，页面级还是 @media（"想用 vs 真用"落差数据唯一真相源：design-system-workflow「Gotchas」节，此处不复制）

## 与其他 skill 的分工

- 选技术栈/组件库底层 → **frontend-stack-selection**（动手前先定）
- 建项目 design token → **design-system-workflow**
- 写完提交前审查 → **frontend-code-review**
- 用 AI 工具生成组件 → **ai-ui-generation-workflow**
- 跨 Rust+前端全栈 → **dev-workflow** 编排 + **backend-development** 处理后端
- 验证方法学 → **verification-driver**
