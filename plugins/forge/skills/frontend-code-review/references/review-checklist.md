# 前端审查清单（6 维度）

本文件是 `frontend-code-review` SKILL.md 步骤 2 加载的完整审查清单。按 6 维度 × 严重性分级。

## 目录

- [严重性定义](#严重性定义)
- [维度 1：a11y（无障碍，最高优先）](#维度-1a11y无障碍最高优先)
- [维度 2：组件 API](#维度-2组件-api)
- [维度 3：Tailwind 规范](#维度-3tailwind-规范)
- [维度 4：Design Token 一致性](#维度-4design-token-一致性)
- [维度 5：性能](#维度-5性能)
- [维度 6：TypeScript strict](#维度-6typescript-strict)
- [审查技巧](#审查技巧)

## 严重性定义

| 级别 | 含义 | 处理 |
|---|---|---|
| **block** | 必须修复才能提交（a11y 法律红线、安全、类型 bug） | 拦截合并 |
| **fix** | 应当修复（规范偏离、维护负担） | 建议修复后合并 |
| **suggest** | 建议考虑（优化、风格） | 记录，不阻塞 |

---

## 维度 1：a11y（无障碍，最高优先）

### Block
- **交互元素用非语义标签**：`<div onClick>` / `<span onClick>` 处理点击 → 改 `<button>`/`<a>`
- **动效无 reduced-motion 分支**：违反 WCAG 2.2 SC 2.3.3；Framer Motion 用 `useReducedMotion()`，CSS 用 `@media (prefers-reduced-motion: reduce)`
- **键盘不可达**：交互元素无 `tabindex`、无键盘事件处理
- **焦点丢失**：modal/dialog 打开后焦点未移入，关闭后未归还
- **表单无 label**：input 无关联 `<label>` 或 `aria-label`

### Fix
- **滥用 aria-label**：元素有可见文字还加 `aria-label`（冗余）
- **错误 ARIA role**：违反 WAI-ARIA APG（如 menu 用 `role="list"` 而非 `role="menu"`）
- **对比度不足**：文字 < 4.5:1（WCAG AA）；OKLCH 注意 sRGB gamut mapping 退化
- **图片无 alt**：装饰图 `alt=""`，内容图有意义的 alt

### Suggest
- **landmark 角色**：用 `<main>`/`<nav>`/`<aside>` 而非全 `<div>`
- **跳过导航链接**：长页面加 "skip to content"

---

## 维度 2：组件 API

### Fix
- **props 巨型化**：>8 个 props 该拆 compound components（`<Card>`/`<Card.Header>`/`<Card.Body>`）
- **polymorphic `as` 类型不正确**：`as` prop 缺泛型约束
- **吞掉 native 属性**：没用 `{...props}` 透传，导致 `onClick`/`className` 等丢失
- **slot 模式缺失**：可定制子节点（如 Avatar fallback/image）未实现

### Suggest
- **shadcn copy-in 漂移**：copy 的组件改过，建议 `shadcn diff` 追上游修复（issue #3579 sonner 失效案例）
- **forwardRef 缺失**：组件未 forwardRef，外部无法取 ref（React 19 ref as prop 后注意新写法）

---

## 维度 3：Tailwind 规范

### Fix
- **任意值滥用**：`bg-[#5e6ad2]` 而非 `bg-brand-primary`（应走 @theme token）
- **inline class 体积**：重复 ≥3 次的 class 组合未抽 `@apply` 或组件（调研：57.6% 页面体积来自 inline class）
- **响应式只用 @media**：组件级响应式应优先 container query（Tailwind v4 一等公民）

### Suggest
- **class 未排序**：未开 Biome `useSortedClasses` 或 prettier-plugin-tailwindcss
- **@theme 定义不规范**：自定义色彩/间距未在 @theme 集中定义

---

## 维度 4：Design Token 一致性

### Block
- **新增值未入 token**："临时硬编码，以后抽"（"以后"= 永不）→ 先加 token 再用

### Fix
- **硬编码色值**：`#5e6ad2` → `var(--color-brand-primary)`
- **硬编码间距/圆角/阴影**：`16px` / `8px` / `0 4px 12px rgba(...)` → 对应 token
- **设计工具 ↔ code token 命名不一致**：Figma/Pixso/PenPot Variables 与 CSS 变量 strict one-to-one mapping 漂移

### Suggest
- **token 层级缺失**：只有 global token 无 alias token（应 global + alias 两层最低）

---

## 维度 5：性能

### Fix
- **整包引入**：`import _ from 'lodash'` → `import debounce from 'lodash/debounce'`
- **Framer Motion 滥用**：简单动效引 125KB → 用 CSS `transition`/`@keyframes`
- **RSC 项目用运行时 CSS-in-JS**：styled-components/Emotion 在 RSC 下 hydration 双倍执行 → 换零运行时或 Tailwind
- **图片未优化**：未用 next/image、未设 width/height（CLS）、未做懒加载
- **列表无 key 或用 index 作 key**：重渲染 bug

### Suggest
- **bundle 未 code-split**：大路由未 `React.lazy`
- **memo 滥用或缺失**：昂贵组件未 memo，简单组件过度 memo

---

## 维度 6：TypeScript strict

### Block
- **`@ts-ignore`/`@ts-expect-error` 无注释**：藏类型 bug → 要么修要么注释说明原因
- **类型断言 `as` 绕过检查**：应用类型守卫（`typeof`/`in`/自定义 guard）

### Fix
- **`any` 滥用**：该用 `unknown` + 类型守卫，或具体类型
- **缺 strict 防护**：数组访问未防 `undefined`（`noUncheckedIndexedAccess` 场景）
- **可选链缺失**：可能 null/undefined 的访问未用 `?.`
- **exactOptionalPropertyTypes 违规**：`x?: T` 误传 `undefined`

### Suggest
- **类型定义冗余**：可用 `satisfies` 或 `infer` 简化

---

## 审查技巧

### 同时查 `+` 行和 `-` 行
AI 作弊常在删除行里：
- 删 a11y 属性（aria-*/tabindex/role）
- 删 reduced-motion 分支
- 删测试里的 a11y 断言
- 降级匹配器（`toBe(x)` → `toBeTruthy()`）

```bash
git diff -- '*.tsx' | grep -E '^-' | grep -iE 'aria|tabindex|role|reduced-motion'
```

### shadcn copy-in 组件特殊审查
copy 的代码归你维护：
- 是否漏了上游安全修复
- 改过的部分是否破坏了 a11y
- 用 `shadcn diff` 检测与上游漂移

### client component 边界（Next.js App Router）
- `'use client'` 是否过度（污染服务端边界）
- ThemeProvider/Context 是否不必要地标记 client
- 数据获取是否留在 server component
