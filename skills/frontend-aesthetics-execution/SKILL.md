---
name: frontend-aesthetics-execution
description: "把 UI 做好看、做风格迁移、做高级感动效的可执行审美工程——给 6 种主流风格（暗色精致 SaaS/Apple Liquid Glass/Neo-brutalism/Bento/Dopamine/Human Touch）的 token 模板，动效曲线与工具选型决策，反 AI 同质化的系统化手法。Use when: 把 UI 做好看、改前端风格、做成 Linear/Apple/Vercel/Stripe 风、用 DESIGN.md / awesome-design-md 还原某具体品牌风格、做高级感动效、UI 重塑/美化、审美调优、反 AI 同质化、选 Rive/Spline/Framer Motion、做 micro-interaction、页面太丑怎么调时。SKIP: 选技术栈（用 frontend-stack-selection）、写组件不出错的规范（用 frontend-feature-development）、提交前审查（用 frontend-code-review）、建 design token 体系（用 design-system-workflow）。"
metadata:
  pattern: inversion + pipeline
  domain: frontend
  composes: [design-system-workflow, frontend-feature-development, frontend-code-review]
---

# 前端审美执行：做好看与风格迁移

把"审美"从主观感觉变成可执行的 token 工程。**核心立场：好看不是玄学，是 surface ladder / 阴影哲学 / 字体字距 / 动效曲线 / 间距节奏的可复现组合。** 2026 调研拆解了 Linear / Vercel / Raycast / Arc 四家的 design token，发现它们在结构层高度收敛且可互译——这意味着"做成某风格"= 套用对应 token 模板 + 在装饰层差异化。

## 铁律：动效与风格必须过 a11y 门

**任何动效/风格调整必须过 `prefers-reduced-motion`（WCAG 2.2 SC 2.3.3）。** 这不是建议是法律红线（欧盟 EAA 2025-06 强制）。前端审美不能以牺牲无障碍为代价。详见本 skill 阶段 4 + frontend-code-review 维度 1。

## 阶段 0 — 确认风格意图（Inversion）

**动键盘前必须确认，缺一不写：**

1. **目标风格**：用户要的是哪种？给 6 种主流选项（见阶段 1），用户没明说就贴参考图/竞品 URL 让 ta 选。**用户点名具体品牌**（Linear/Stripe/Apple…）→ 查阶段 1.5 索引拿 slug，`Read` 该品牌 `DESIGN.md` 取精确 token（pattern 管结构，instance 管精确值）
2. **品牌约束**：有没有现成 brand color / 字体 / logo？还是从零定？
3. **平台与受众**：Web only 还是含桌面/移动？受众是开发者（接受暗色克制）/ 大众（要亲和）/ 品牌 agency（要独特）？
4. **性能预算**：允许引动效库吗？Lighthouse Performance 分有底线吗？（Spline runtime 544KB gzip 会拖垮）

→ 选技术栈用 frontend-stack-selection；建 token 体系用 design-system-workflow。

## 阶段 1 — 6 种风格 Token 模板（核心，可直接 copy）

每种风格给：定位 / surface ladder / 阴影 / 字体 / 适用场景 / 陷阱。**token 值来自调研实测，非编造。**

### 风格 A — 暗色精致 SaaS（Linear/Vercel/Raycast 风）

**定位**：开发者工具/B2B SaaS 的安全基线。2026 调研发现四家在此结构层收敛，是"做得不像 AI"的最稳选择。

```css
@theme {
  /* Surface ladder — near-black base + 灰阶分层 */
  --color-canvas: oklch(8% 0 0);          /* #08090a 级，最暗 */
  --color-surface: oklch(13% 0 0);        /* 卡/面板 */
  --color-surface-elevated: oklch(18% 0 0);
  --color-ink: oklch(98% 0 0);            /* 最亮 / CTA 白 */
  --color-ink-secondary: oklch(65% 0 0);  /* 文字 4 档灰阶 */
  --color-ink-tertiary: oklch(45% 0 0);
  --color-brand-accent: oklch(62% 0.19 264);  /* indigo，Linear #5e6ad2 */

  /* 阴影哲学 — 二选一 */
  /* A1. Linear/Raycast 派：无装饰阴影，靠 hairline border */
  --border-hairline: oklch(20% 0 0);
  /* A2. Vercel 派：shadow-as-border（零偏移零模糊 1px spread）*/
  --shadow-border: 0 0 0 1px rgba(0,0,0,0.08);
  --shadow-elevation: 0 2px 2px rgba(0,0,0,0.04);

  /* 字体 — Inter 系 + 负字距（Vercel 最激进 -2.88px）*/
  --font-sans: 'Inter Variable', system-ui, sans-serif;
  --font-mono: 'Berkeley Mono', ui-monospace, monospace;
  --tracking-display: -0.04em;   /* hero 大字负字距 */
  --tracking-body: -0.011em;

  /* 圆角 — 6-10px 收敛 */
  --radius-card: 8px;
  --radius-pill: 9999px;
}
```

**适用**：开发者工具、B2B SaaS、命令行式高密度 UI。
**陷阱**：① 这套已是"新同质化"——四家长得像，必须在 brand accent / 内容 / 动效层差异化才不沦为 AI slop；② dark-first 是工效学选择（NN/g：long session/frequent/low-light/little media 四条件命中），非纯审美。

### 风格 B — Apple Liquid Glass（WWDC 2025/2026）

**定位**：跨平台统一语言，web 端 glassmorphism 2.0 的合法依据。Apple WWDC 2026 自我修订 reduced default transparency + 推出 slider——承认默认过激。

```css
@theme {
  --color-bg: oklch(95% 0.02 250);       /* 浅色底 */
  --color-glass-tint: oklch(100% 0 0 / 0.6);  /* 半透明玻璃 */
  --blur-glass: 24px saturate(180%);
  --border-glass: 1px solid rgba(255,255,255,0.3);
  --shadow-glass: 0 8px 32px rgba(0,0,0,0.12);
}
.glass-panel {
  background: var(--color-glass-tint);
  backdrop-filter: var(--blur-glass);
  border: var(--border-glass);
  box-shadow: var(--shadow-glass);
}
```

**适用**：跨平台应用、需要"系统融合感"的桌面/Web 应用。
**陷阱**：① `backdrop-filter` 在 sRGB 设备 + 高对比度模式下可能冲突——必须测对比度；② WWDC 2026 的 reduced transparency 是官方信号，别把透明度拉满；③ 性能成本高，低端设备 fallback 静态背景。

### 风格 C — Neo-brutalism

**定位**：raw / high-contrast / visible grids / 厚边框 / 生阴影。Figma Trend 12。**品牌/机构站工具，B2B SaaS 主产品慎用。**

```css
@theme {
  --color-bg: #fef3c7;            /* 高饱和黄底 */
  --color-ink: #000000;
  --color-accent: #ef4444;        /* 高对比红 */
  --border-brutal: 3px solid #000;
  --shadow-brutal: 6px 6px 0 #000; /* 硬阴影，无模糊 */
  --radius-brutal: 0;             /* 直角或极小圆角 */
}
```

**适用**：创意机构站、个人作品集、品牌营销页、实验性产品。
**陷阱**：① storifyagency 原话 "high-contrast colors and grid structures that feel more like controlled chaos... It's not for the faint of heart"——企业级产品不用；② 厚黑边 + 硬阴影对 a11y 对比度友好（高对比），但动效要克制否则视觉过载。

### 风格 D — Bento Grid

**定位**：Apple 产品宣传页带火的模块化布局。信息密度高、模块独立、视觉有序。

```css
@theme {
  --grid-gap: 12px;
  --grid-col-min: 280px;
  --radius-card: 16px;       /* Apple 风，大圆角 */
  --shadow-card: 0 1px 3px rgba(0,0,0,0.1);
}
.bento {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(var(--grid-col-min), 1fr));
  gap: var(--grid-gap);
  grid-auto-rows: minmax(180px, auto);
}
.bento > * { border-radius: var(--radius-card); }
```

**适用**：产品宣传页、功能展示、dashboard 卡片墙。
**陷阱**：① 别为 Bento 而 Bento——模块内容不独立时强行切格子反而割裂；② 响应式断点要测，移动端通常退化为单列。

### 风格 E — Dopamine / 高饱和

**定位**：高饱和、情绪化、抓眼球。Figma Trend 3。**2026 下半场出现疲劳反信号**（jasminedirectory："by late 2026, dopamine color fatigue"）。

```css
@theme {
  --color-dopamine-1: oklch(75% 0.28 0);     /* 饱和橙 */
  --color-dopamine-2: oklch(70% 0.25 320);   /* 饱和紫 */
  --color-dopamine-3: oklch(80% 0.22 150);   /* 饱和绿 */
  /* 搭配大量留白 + 一个主饱和色，避免视觉过载 */
}
```

**适用**：消费类品牌、活动营销页、情绪化产品（健身/社交）。
**陷阱**：① 疲劳信号已现——用 1 个主饱和色 + 中性辅色，别全饱和；② 对比度要测，高饱和色对白字常不达 4.5:1。

### 风格 F — Human Touch / Anti-AI Crafting（2026 元叙事）

**定位**：反 AI 同质化的系统化手法。designmagazine 称 "$50M Handmade Rebellion"。**核心：把 imperfection 编码进 design token，而非贴 scribble overlay 装饰。**

```css
@theme {
  /* A. 颜色 — 避开 AI safe palette（muted 蓝灰 + 单 accent）*/
  --color-found-moss: oklch(58% 0.08 140);   /* 带情感命名，slightly too warm */
  --color-weathered-paper: oklch(92% 0.02 80);

  /* B. 字体 — variable font 非整数 axis（反算法）*/
  --font-display-handcrafted: 'Fraunces', serif;
  --wght-display: 510;   /* 非 500/600 整数，Linear 做法 */
  --opsz-hero: 64;       /* opsz 锚定 px，字形真的在变 */

  /* C. texture 作为 token（不是每页贴）*/
  --texture-grain-opacity: 0.04;
  --rotate-handplaced: -0.6deg;   /* 略微歪，可控变量 */

  /* D. font-feature 开 ss03（Raycast 全站开）*/
  --font-feature-display: "ss03", "liga";
}
```

**适用**：品牌站、agency、奢侈品、需要差异化的产品。
**陷阱**：① 低段位做法（贴 scribble overlay）无法 scale 且仍是"AI 打底 + 人补丁"；② 高段位（token 编码 imperfection）才系统化——判定标准：手工痕迹能在 token 文件被命名并跨页复用。

## 阶段 1.5 — 品牌 DESIGN.md 实例库（pattern ↔ instance）

阶段 1 的 6 种模板是抽象 **pattern**。当用户**点名具体品牌**（"做 Linear 风""做成 Stripe 那样"）时，先取该品牌的真实 DESIGN.md 作为精确 token 来源，再套对应模板——pattern 管结构（surface ladder / 阴影哲学 / 字距哲学），instance 管精确色值 / 字距 / 圆角。

**品牌资产库**：VoltAgent/awesome-design-md 仓库的 `design-md/<slug>/DESIGN.md`（74 个真实品牌，`git pull` 更新——引用路径而非搬文件）。发现方式：环境变量 `DESIGN_MD_ROOT` 指向仓库克隆根，或查找当前工作区/常用代码目录下的 `awesome-design-md`；未克隆时先 `git clone https://github.com/VoltAgent/awesome-design-md`；仓库不可用或品牌未命中时 fallback 到阶段 1 的 6 种通用风格模板。

**用法**：
1. 用户点名品牌 → 列 `design-md/` 目录拿 **slug**（注意带点 / 连字符的目录名），或查 [references/brand-index.md](references/brand-index.md) → `Read` 该 `DESIGN.md`
2. 从其 front matter 取 `colors`（hex）/ `typography`（px + weight + letterSpacing）/ `rounded` / `spacing` / `components` 的精确值
3. 套阶段 1 对应模板的**结构**，用 DESIGN.md 的精确值替换模板占位值
4. hex → OKLCH 转换走 **design-system-workflow**（见下方 gap）

### 6 模板 ← 代表品牌

| 阶段 1 模板 | 代表品牌（slug） |
|---|---|
| A 暗色精致 SaaS | `linear.app` · `vercel` · `raycast` · `cursor` · `superhuman` · `warp` · `resend` |
| B Liquid Glass | `apple` |
| C Neo-brutalism | `dell-1996` · `nintendo-2001` |
| D Bento 模块化 | `meta` · `playstation` |
| E Dopamine 高饱和 | `spotify` · `binance` · `figma` · `slack` |
| F Human Touch 编辑暖色 | `notion` · `stripe` · `airbnb` · `cal` · `mastercard` |

> 未命中 6 模板的品牌（`ferrari`/`bugatti`/`lamborghini` 极致黑金汽车、`wired`/`theverge` 编辑媒体、`ibm` Carbon 等）直接 `Read` DESIGN.md 取值，**不强行套模板**。

### 完整品牌索引

74 个品牌按领域的完整 slug 索引已下沉到 [references/brand-index.md](references/brand-index.md)；也可以直接列 `design-md/` 目录拿 slug（slug 即目录名）。

### 格式 gap（DESIGN.md 不能直接当 token 消费）

- DESIGN.md front matter 是 **hex + 自定义 YAML**（`colors`/`typography`/`rounded`/`spacing`/`components`，用 `{colors.primary}` 引用），**不是 DTCG tokens.json**——不能直接喂 Style Dictionary
- hex → **OKLCH** 转换、YAML → DTCG `$type/$value` 重组走 **design-system-workflow** 阶段 3
- `preview.html` / `preview-dark.html`：README 声称每个站点有，**仓库实际不存在**（只在 getdesign.md 网站）——只能用 DESIGN.md 文本，别声称能看预览
- 字体多为品牌私有（Linear Display / Stripe Sans / Apple SF Pro）——DESIGN.md 自带 fallback 与开源替代（Inter / Geist），用替代值即可

## 阶段 2 — 风格迁移工作流（Pipeline）

把现有页面从 A 风格迁到 B 风格的步骤：

```
风格迁移：
- [ ] 1. 盘点现有 token（grep 硬编码色值/间距/阴影/圆角）
- [ ] 2. 建新风格 token 集（从阶段 1 选模板，按品牌调）
- [ ] 3. 全局替换硬编码 → token（design-system-workflow）
- [ ] 4. 调阴影/边框哲学（如 flat→shadow-as-border）
- [ ] 5. 调字体字距/字重（display 负字距是高级感关键）
- [ ] 6. 调圆角/间距节奏（radius 6-10px 是 SaaS 收敛值）
- [ ] 7. 加/调动效（阶段 3）
- [ ] 8. a11y 复测（对比度/reduced-motion）
```

**门控**：步骤 8 不过不算完成。OKLCH 主题切换会因 gamut mapping 静默改变对比度，必须在真实设备复测。

## 阶段 3 — 动效与微交互（"高级感"的关键）

### 3.1 动效曲线 token（四家都未公开，需自建）

2026 调研发现四家 design token 文件**均无 motion/easing token**——这是 token 体系的结构性缺口。建议自建：

```css
@theme {
  /* 进入动画 — ease-out-expo（Framer Motion 默认）*/
  --ease-enter: cubic-bezier(0.16, 1, 0.3, 1);
  /* 状态切换 — spring 而非 keyframe tween */
  --spring-stiffness: 300;
  --spring-damping: 30;
  /* hover/微交互 — 快速 */
  --duration-micro: 0.15s;
  --ease-micro: cubic-bezier(0.4, 0, 0.2, 1);
}
```

**反 AI 信号**：① 避开 `linear` 和默认 `ease`（最算法）；② spring 给"质量感"；③ 微交互 duration 0.15-0.2s。

### 3.2 动效工具选型决策（调研实测 bundle）

| 你要做的是 | 选 | 理由 |
|---|---|---|
| App 内 micro-interaction（按钮/loading/icon 动画） | **Rive** | state machine + ~200KB gzip + 文件比 Lottie 小 10-15 倍 + 原生 ARIA |
| 营销页 hero 轻量 brand 动画 | **Rive** | 16KB 级文件秒开 |
| 营销页沉浸式 3D（产品模型/场景） | **Spline** | no-code，designer 可交付 |
| 需 3D 但有性能预算 | **React Three Fiber** | tree-shake three.js ~150KB gzip |
| 需复杂响应式动画（hover 改多物体/物理） | **R3F 或 Rive** | Spline 复杂行为受限 |
| 团队无 JS 3D 能力只有 designer | **Spline** | 唯一 no-code |
| 跨平台一致（Web+iOS+Android+游戏） | **Rive** | 唯一全平台 runtime |
| 对 Core Web Vitals 敏感（电商/内容站） | **Rive**（首选）/ R3F（次） | Spline 544KB gzip 几乎必然拖垮 Performance |

**反直觉**：Spline runtime.js 实测 544KB gzip **比 three.js 全量还重**（不可 tree-shake，导出含完整 runtime）。"用 Spline 省事"在性能预算紧的页面是反向选择。

### 3.3 微交互范式（四家收敛规律）

- **hover**：duration 0.15s，背景色微变（surface 升一档）或 scale 1.02
- **focus**：ring（`box-shadow: 0 0 0 2px var(--color-ring)`），不用 outline
- **状态切换**：spring（stiffness 300/damping 30），不用 tween
- **页面转场**：duration 0.3s ease-out-expo，淡入 + 轻微位移（y: 8px → 0）

## 阶段 4 — 动效 a11y 门（WCAG 2.2 SC 2.3.3 强制）

**universal reset（pope.tech 推荐生产级写法）：**

```css
@media (prefers-reduced-motion: reduce) {
  * {
    animation: none !important;
    transition: none !important;
    scroll-behavior: auto !important;
  }
}
```

JS 侧（Rive/Spline/R3F 非 CSS 动画）：

```js
const prefersReducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)');
if (prefersReducedMotion.matches) {
  // 切静态 state / 停止 JS 动画
}
prefersReducedMotion.addEventListener('change', (e) => {
  if (e.matches) { /* 切静态 */ } else { /* 恢复 */ }
});
```

**静态等价物四类**（动画承载信息时不能简单关）：
| 用途 | reduced-motion fallback |
|---|---|
| 传达状态（loader/脉冲点） | 静态文字"Loading…"/实心图标 |
| 揭示内容（淡入 alert） | 默认就可见，不依赖 motion |
| 唯一导航（自动轮播） | 第一张静态 + 手动 prev/next |
| 帮助理解（错误抖动） | 持久文字/图标传达 |

**测试**：DevTools → Rendering → Emulate `prefers-reduced-motion: reduce`。

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "审美太主观，没法系统化" | 四家 token 已证明收敛可互译；做某风格 = 套 token 模板 + 装饰层差异化 |
| "做 Linear 风就抄它配色" | 结构层可抄（surface ladder/hairline），brand accent/内容/动效必须自己定否则成新 AI slop |
| "动效好看就行不管 reduced-motion" | 违反 WCAG 2.2 SC 2.3.3（欧盟法律红线）；一行 media query 的事 |
| "贴 scribble overlay 就是 Human Touch" | 低段位装饰层，无法 scale；要编码进 token（texture/非整数 axis/hand-drawn 组件类目） |
| "Spline 3D 轻量又好看" | runtime 544KB gzip 不可 tree-shake，比 three.js 重；性能预算紧别用 |
| "dopamine 配色抓眼球全用上" | 2026 下半场疲劳信号已现；1 个主饱和 + 中性辅色 |
| "Framer Motion 引就引了" | 简单 fade-in 用 CSS transition；125KB 不值 |

## Red Flags（我在 rationalize 的信号）

- 没确认风格意图就动手
- 抄竞品配色不调 brand accent
- 动效没写 reduced-motion 分支
- 贴 scribble/grain overlay 而非 token 化
- 全饱和 dopamine 配色
- 性能敏感页引 Spline/Framer Motion
- 调完不复测对比度（OKLCH gamut mapping 陷阱）

## Gotchas

- **暗色 SaaS 收敛是"新同质化"双刃剑**：结构层复用省力，但必须在装饰层差异化，否则被批 "soulless plastic look"
- **Vercel 的 shadow-as-border 不是噱头**：用 `box-shadow: 0 0 0 1px` 模拟 border，绕开 box-model 对 border-radius 的裁剪，圆角处不出锯齿——这是四家里唯一有技术含量的一招
- **Inter 系是"便宜的中性高质量字"**：三家用 Inter，Vercel 自研 Geist 也是"Inter 的工程师重写版"——字形基因同源，差异在字距/feature
- **variable font 非整数 weight 是反 AI 信号**：`wght: 510` 比 `500` 更像人为决策；opsz axis 锚 px 让字形真的随字号变
- **opsz 锚定**：`font-variation-settings: 'opsz' 64` 给 hero、`'opsz' 14` 给 caption（Microsoft OpenType 规范）
- **font-feature 开 ss03**：Raycast 全站开 ss03 stylistic set，是"声明看过字体细节"的信号
- **Human Touch 的判定**：手工痕迹能在 token 文件被命名（`--texture-*`/`--rotate-handplaced`/非整数 axis）并跨页复用 = 系统层；只在某张图图层里 = 装饰层

## 与其他 skill 的分工

- **选技术栈**（React/Vue + 组件库 + CSS 方案）→ `frontend-stack-selection`（风格选定前先定栈）
- **建 design token 体系**（DTCG 链路/OKLCH 主题/shadcn registry）→ `design-system-workflow`（本 skill 给风格模板，它管 token 工程化）
- **写组件不出错**（a11y/token-only/组件 API）→ `frontend-feature-development`（本 skill 是"做得好看"，它是"做得不出错"）
- **提交前审查**（含 a11y/动效合规）→ `frontend-code-review`
- **用 AI 工具生成 UI** → `ai-ui-generation-workflow`
- 一手源：VoltAgent/awesome-design-md（Raycast/Linear DESIGN.md）、explainx.ai（Vercel）、pixelripple.ai（Anti-AI 框架）、w3.org/WAI/WCAG22/Techniques/css/C39
