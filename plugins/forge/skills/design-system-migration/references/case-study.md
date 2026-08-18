# 案例实录：单一 Tauri 桌面项目的设计系统迁移全程

一次真实迁移的完整记录（Tauri 桌面应用，React + CSS Modules）。**这是案例不是规范**——可迁移的方法论在主 SKILL.md，此处细节供落地时对照参考，色板/组件名/范式选择换成你自己项目的。

## 目录

- [Phase 0 实录 — 地基（token 改造）](#phase-0-实录--地基token-改造)
- [Phase 1 实录 — 原子组件库（签名原子）](#phase-1-实录--原子组件库签名原子)
- [Phase 2 实录 — 布局外壳（自动继承）](#phase-2-实录--布局外壳自动继承)
- [Phase 3 实录 — 逐页重构](#phase-3-实录--逐页重构)
- [Phase N 实录 — 多套正交主题（tide/ink/moss × light/dark）](#phase-n-实录--多套正交主题tideinkmoss--lightdark)
- [实录中的陷阱现场](#实录中的陷阱现场)

## Phase 0 实录 — 地基（token 改造）

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

## Phase 1 实录 — 原子组件库（签名原子）

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

## Phase 2 实录 — 布局外壳（自动继承）

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

## Phase 3 实录 — 逐页重构

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

## Phase N 实录 — 多套正交主题（tide/ink/moss × light/dark）

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

## 实录中的陷阱现场

- **active-stripe 用错场景**：双色 62% 硬切用在 StatusBar 顶部 40px 固定宽度装饰条成立；曾误用于 BudgetBar 变宽进度条 fill——硬切段出现在进度条中间任意位置，像断裂。修复：进度条用纯 `--accent` 色。
- **同名类型分叉**：`settingsStore.ts` 自己定义了 `AppSettings`（与 `types/index.ts` 重复），修改 type 时两边都要同步。
