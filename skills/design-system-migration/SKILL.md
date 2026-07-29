---
name: design-system-migration
description: >
  前端设计系统迁移与组件库升级的完整工程化方法论。Use when: 要把旧 design token 迁移到新色板时、从 AI 味 UI 升级到有签名的设计系统时、做"全套重做"风格统一时、引入多套正交主题（palette × light/dark）时、
  把散落的 HTML class 收敛为 compound 组件库时、将纯视觉组件升级为带交互签名的组件时（如四角取景框/CRT 闪烁/双色硬切条）、
  用三段折叠范式（thinking/工具调用/结论）重构 chat block 流时、用户说"新建组件库""重做主题""换设计系统""token 迁移"时。
  SKIP: 单页微调（直接改）、从零新建设计系统/Token pipeline 无旧系统（用 design-system-workflow）、只用 Tailwind 不建 design token 的项目、非前端项目。
metadata:
  pattern: pipeline + gate
  domain: frontend
  steps: 7
  composes: frontend-feature-development, test-discipline, verification-driver
---

# 设计系统迁移与组件库升级方法论

从旧 design token 到新设计系统的完整工程化流程。**核心思想：地基先行（token）+ 签名组件原子化 + 逐页确认门控**。

---

## 核心纪律（贯穿全程）

### 纪律 1：token-only（禁止硬编码）

```css
/* ❌ 硬编码 */
.card { background: #5457d6; padding: 16px; }

/* ✅ token */
.card { background: var(--accent); padding: var(--space-4); }
```

- 色值/间距/圆角/阴影/字号/动效曲线 —— 全部从 CSS 变量取
- 新 token 先定义再使用，不临时写死 hex

### 纪律 2：保留旧变量名，只改值

**不改变量名**。旧代码引用 `--accent`/`--bg-canvas`/`--text-primary` 等不变，只把值映射到新色板。**变量名是 API 契约**，数量只增不减。

```css
/* ✅ 对：保留旧名，重映射值 */
:root {
  --accent: var(--color-tidal-blue);  /* 旧名新值 */
  --bg-canvas: var(--color-moonstone);
}

/* ❌ 错：删旧变量换新名（破坏所有引用） */
:root {
  --accent-new: #4b607c;  /* 旧代码全崩 */
}
```

### 纪律 3：增量确认门控

**每完成一个 Phase，生成独立 HTML 确认页给用户浏览器确认**，确认后才进下一个 Phase。不能凭"应该没问题"跳到下一步。

### 纪律 4：props 透传

所有新组件必须透传 `...props` 到根元素（含 `HTMLAttributes`），确保 `data-testid` 等 native 属性不丢。

```tsx
export function MyComponent({ ...props }: Props & HTMLAttributes<HTMLDivElement>) {
  return <div {...props}>...</div>;
}
```

### 纪律 5：test 行为契约不弱化

组件重构时维护语义等价：新选择器（`data-testid`）替代旧 class 选择器但**断言意图不变**。不因 CSS module hash 改弱 `t.Fatal` 或降级为宽松匹配。

---

## Phase 0 — 地基（token 改造）

**目标**：把旧色板换成新设计系统，同时不破坏任何现有引用。

### 0.1 变量重映射

```css
/* 新增命名色板（自然材质命名，反 AI 同质化）*/
:root {
  --color-parchment: #dacbc2;
  --color-moonstone: #ebe7e4;
  --color-tidal-blue: #4b607c;
  --color-terracotta: #844f3b;
  --color-sunkissed: #e1b06e;
  --color-sage: #a3a473;
  --color-success: #5db87a;
  --color-warning: #e8993a;
  --color-error: #e8704f;
  /* ...更多命名色 */

  /* 旧变量名保留，值重映射到新色板 */
  --palette-accent: var(--color-tidal-blue);
  --accent: var(--color-tidal-blue);
  --bg-canvas: var(--color-moonstone);
  --text-primary: rgba(37, 47, 61, 0.96);
  /* ... */
}

[data-theme="dark"] {
  --bg-canvas: #161d27;
  --accent: #6a9fcc;
  --text-primary: #ebe7e4;
  /* ... */
}
```

### 0.2 签名 token（新增，不进旧变量）

```css
:root {
  --active-stripe: linear-gradient(90deg, var(--accent) 0%, var(--accent) 100%);
  --paper-grain: radial-gradient(...);
  --page-grid-minor: rgba(...);
  --font-serif: 'Newsreader', Georgia, serif;
  --font-mono: 'JetBrains Mono', monospace;
}

[data-theme="dark"] {
  /* 暗色 active-stripe 双色硬切（62% 处一刀切，品牌签名示例）*/
  --active-stripe: linear-gradient(90deg, #6a9fcc 0 62%, #4b607c 62% 100%);
}
```

### 0.3 字体加载

`@font-face` 声明衬线 + 等宽双字体（Newsreader + JetBrains Mono，替代系统默认 sans）→ 全局生效。

### 0.4 动效

CRT 步进闪烁（`steps(1,end)`，非 smooth opacity）→ 签名动效。全局 `prefers-reduced-motion` 门覆盖。

### 0.5 body 质感

`background-image` 叠加纸纹（径向渐变）+ 页面网格（极低 alpha 线条）→ 非纯色底的材质感。

### 验证

- `tsc --noEmit` 零错误
- 全量测试通过
- 浏览器看 computed style：确认 `--accent`/`--bg-canvas` 等值已换成新色板
- body 背景看到纸纹 + 网格

---

## Phase 1 — 原子组件库（签名原子）

**目标**：把设计系统的独有视觉签名（四角取景框/CRT 闪烁/双色硬切条）做成可复用的原子组件。

### 1.1 Frame（四角取景框）

- compound 组件，5 种 variant：default / highlight / subtle / success / danger
- 用 4 个绝对定位 `span` 渲染四角标记（非 border，是品牌签名示例：四角取景框）
- 透传 `HTMLAttributes` + `...props`

### 1.2 LiveDot（CRT 步进闪烁）

- `animation: crt-blink 1.25s steps(1, end) infinite`（非 smooth）
- 5 种 status × 3 种 size
- idle 态不闪

### 1.3 Stripe（双色硬切激活条）

- `background: var(--active-stripe)`
- light 单色 / dark 62% 硬切
- 3 种高度

### 1.4-1.6 现有组件改造

- Button primary variant 加 active-stripe 底部装饰条（`::after` 伪元素）
- Modal 加四角取景框 + danger variant（陶土色）
- ghost button 加回淡边框（`--border-default`）

### 验证

- 所有原子组件有测试（至少渲染 + variant + a11y）
- 现有 Button/Modal 测试不破

---

## Phase 2 — 布局外壳（自动继承）

**目标**：TitleBar/StatusBar/Sidebar 风格统一。这一步通常是**改动最小**的——因为 Phase 0 的 token 改造让它们自动继承新色板。

### 2.1 TitleBar

- 品牌标 "DW" 用衬线斜体（`var(--font-serif)`）
- 面包屑首段用衬线

### 2.2 StatusBar

- 左上角加 40px active-stripe 装饰条（`::before` 伪元素）
- running 状态点改用 CRT 步进闪烁（替换原 smooth `pulse-dot`）

### 2.3 Sidebar / AgentStatusBar / WindowControls

- **零改动**——它们已完全 token-driven，随 Phase 0 自动继承新风格

### 验证

- tsc + 全量测试通过
- HTML 确认页展示完整布局

---

## Phase 3-N — 逐页重构（最高业务价值优先）

**每页重构流程**：读现有组件 → 确定改造边界 → 新建/更新组件 + CSS → 跑测试 → 生成 HTML 确认页 → **用户确认才进下一页**。

### 3.1 组件提取模式

Chat block 流 → 三段折叠（Cursor 3.0 / Codex app 范式）：

```
thinking block → L1Thinking（默认折叠 + CRT 闪烁）
tool_use block → L2ToolPill（✓/▸/✕ 三态 pill）
tool_result → L2ToolPill（success/error 态）
text block → Frame + Markdown
result → 状态行
```

**原则**：业务逻辑零改动（`normalizeEvents` 等保留），只换视觉层。

### 3.2 测试迁移

旧选择器 `.chat-block-*` → 新选择器 `[data-testid="chat-block-*"]`（CSS module hash 问题）。断言意图不变。

### 3.3 Steering 模式（运行中插话）

Composer 加 `steering` prop：运行中时不禁用 textarea，Enter = 插话，Shift+Enter = 排队。提示条用 sunkissed 警示色。

### 3.4 分支树

从 ChatView 内联分支切换器抽成独立 `BranchTree` 组件：树连接符 + active chain 高亮 + leaf CRT 闪烁 + fork 分叉标记 + checkpoint ◆ 标记。

---

## Phase N — 多套正交主题（palette × light/dark）

**目标**：不止一套设计系统，而是 N 套风格主题 + 亮暗模式正交组合。

### 实现方式

```html
<!-- palette 和 theme 正交 -->
<html data-theme="light" data-palette="tide">
<html data-theme="dark" data-palette="ink">
<!-- 6 种组合：3 palette × 2 theme -->
```

### CSS 方案

```css
/* A 主题（默认，:root 已定义）*/

/* B 主题覆盖 */
[data-palette="ink"] { --bg-canvas: #f5f0e6; --accent: #8b2820; ... }

/* B 主题 + 暗色 */
[data-theme="dark"][data-palette="ink"] { --bg-canvas: #1a1a1c; --accent: #d4665a; ... }

/* C 主题同理 */
[data-palette="moss"] { ... }
[data-theme="dark"][data-palette="moss"] { ... }
```

### AppSettings 扩展

```typescript
interface AppSettings {
  theme: 'light' | 'dark' | 'auto';
  palette: 'tide' | 'ink' | 'moss';  // 新增（示例品牌名，替换为你的）
}
```

### 切换 UI

SettingsView → AppearanceSection：风格主题 3 卡选择器 + 亮/暗 segmented。切换即时生效（`applyPalette()` 写 `data-palette` 属性）。

---

## 常见陷阱与自查清单（Gotchas）

### 陷阱 1：签名单用错场景

`active-stripe`（双色 62% 硬切）只适合**固定宽度装饰条**（StatusBar 顶部 40px），**不能用于变宽度进度条**（BudgetBar fill）——硬切段会出现在进度条中间任意位置，像断裂。

→ 修复：进度条用纯 `--accent` 色，hard-cut stripe 只给固定宽度元素。

### 陷阱 2：CSS module `composes:` 不兼容 lightningcss

Vite 8 用 lightningcss 作 CSS minifier，不支持 CSS Modules 的 `composes:` 语法。

→ 避免使用 `composes:`，用普通 CSS 类组合。

### 陷阱 3：旧变量名映射后仍被覆盖

`settingsStore.ts` 自己定义了 `AppSettings`（与 `types/index.ts` 重复），修改 type 时两边都要同步。

→ 改类型前 `grep` 找所有定义处。

### 陷阱 4：props 不透传导致 data-testid 丢失

新组件忘记 spread `...props` → `data-testid` 不生效 → 测试失败。

→ 所有新组件必须 `& HTMLAttributes<HTMLDivElement>` + spread `...props` 到根元素。

### 陷阱 5：切暗色后边框不可见

暗色 `--border-default` alpha < 0.35 在深背景上几乎隐身。

→ 暗色 border alpha 至少 0.40+；subtle variant 角标在暗色下额外提亮。

---

## 与其他 skill 的分工

- 动手写组件前 → **frontend-feature-development**（a11y/token/compound API 纪律）
- 断言守卫 + test 迁移 → **test-discipline**
- 端到端验证 → **verification-driver**
- 初次建 design token → **design-system-workflow**（本 skill 假设已有旧 token 要迁移，不是从零建）
- 选组件库底层 → **frontend-stack-selection**
