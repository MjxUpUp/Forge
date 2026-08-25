---
name: frontend-aesthetics-execution
description: "UI 美化/风格迁移/高级感动效的审美工程——6 种主流风格 token 模板、动效与工具选型、反 AI 同质化手法。Use when: 做好看/美化 UI、做成 Linear/Apple/Vercel 风、用品牌 DESIGN.md 还原风格、做高级感动效、反 AI 同质化、选 Rive/Spline/Framer Motion、做 micro-interaction、页面太丑时。SKIP: 选技术栈（frontend-stack-selection）、写组件规范（frontend-feature-development）、提交前审查（frontend-code-review）、建 design token 体系（design-system-workflow）。"
metadata:
  pattern: inversion + pipeline
  domain: frontend
  composes: [design-system-workflow, frontend-feature-development, frontend-code-review]
  triggers: [{"event":"UserPromptSubmit","keywords":["做好看","美化","风格迁移","高级感","太丑","审美","micro-interaction","动效","Bento","Neo-brutalism","Stripe"],"cooldown":600}]
---

# 前端审美执行：做好看与风格迁移

把"审美"从主观感觉变成可执行的 token 工程。**核心立场：好看不是玄学，是 surface ladder / 阴影哲学 / 字体字距 / 动效曲线 / 间距节奏的可复现组合。** 2026 调研拆解了 Linear / Vercel / Raycast / Arc 四家的 design token，发现它们在结构层高度收敛且可互译——这意味着"做成某风格"= 套用对应 token 模板 + 在装饰层差异化。

## 铁律：动效与风格必须过 a11y 门

**任何动效/风格调整必须过 `prefers-reduced-motion`（WCAG 2.2 SC 2.3.3）。** 这不是建议是法律红线（欧盟 EAA 2025-06 强制）。前端审美不能以牺牲无障碍为代价。本 skill 阶段 4 是 reduced-motion/WCAG 2.3.3 实现模板的唯一权威版本——其他 skill 引用不复制；审查侧见 frontend-code-review 维度 1。

## 阶段 0 — 确认风格意图（Inversion）

**动键盘前必须确认，缺一不写：**

1. **目标风格**：用户要的是哪种？给 6 种主流选项（见阶段 1），用户没明说就贴参考图/竞品 URL 让 ta 选。**用户点名具体品牌**（Linear/Stripe/Apple…）→ 查阶段 1.5 索引拿 slug，`Read` 该品牌 `DESIGN.md` 取精确 token（pattern 管结构，instance 管精确值）
2. **品牌约束**：有没有现成 brand color / 字体 / logo？还是从零定？
3. **平台与受众**：Web only 还是含桌面/移动？受众是开发者（接受暗色克制）/ 大众（要亲和）/ 品牌 agency（要独特）？
4. **性能预算**：允许引动效库吗？Lighthouse Performance 分有底线吗？（Spline runtime 544KB gzip 会拖垮）

→ 选技术栈用 frontend-stack-selection；建 token 体系用 design-system-workflow。

## 阶段 1 — 6 种风格 Token 模板（核心，可直接 copy）

每种风格给：定位 / surface ladder / 阴影 / 字体 / 适用场景 / 陷阱。**token 值来自调研实测，非编造。** 完整 CSS token 模板（可直接 copy，含全部注释细节）见 [references/style-templates.md](references/style-templates.md)；本节留风格定位与适用/陷阱决策信息。

### 风格 A — 暗色精致 SaaS（Linear/Vercel/Raycast 风）

**定位**：开发者工具/B2B SaaS 的安全基线。2026 调研发现四家在此结构层收敛，是"做得不像 AI"的最稳选择。
**适用**：开发者工具、B2B SaaS、命令行式高密度 UI。
**陷阱**：① 这套已是"新同质化"——四家长得像，必须在 brand accent / 内容 / 动效层差异化才不沦为 AI slop；② dark-first 是工效学选择（NN/g：long session/frequent/low-light/little media 四条件命中），非纯审美。

### 风格 B — Apple Liquid Glass（WWDC 2025/2026）

**定位**：跨平台统一语言，web 端 glassmorphism 2.0 的合法依据。Apple WWDC 2026 自我修订 reduced default transparency + 推出 slider——承认默认过激。
**适用**：跨平台应用、需要"系统融合感"的桌面/Web 应用。
**陷阱**：① `backdrop-filter` 在 sRGB 设备 + 高对比度模式下可能冲突——必须测对比度；② WWDC 2026 的 reduced transparency 是官方信号，别把透明度拉满；③ 性能成本高，低端设备 fallback 静态背景。

### 风格 C — Neo-brutalism

**定位**：raw / high-contrast / visible grids / 厚边框 / 生阴影。Figma Trend 12。**品牌/机构站工具，B2B SaaS 主产品慎用。**
**适用**：创意机构站、个人作品集、品牌营销页、实验性产品。
**陷阱**：① storifyagency 原话 "high-contrast colors and grid structures that feel more like controlled chaos... It's not for the faint of heart"——企业级产品不用；② 厚黑边 + 硬阴影对 a11y 对比度友好（高对比），但动效要克制否则视觉过载。

### 风格 D — Bento Grid

**定位**：Apple 产品宣传页带火的模块化布局。信息密度高、模块独立、视觉有序。
**适用**：产品宣传页、功能展示、dashboard 卡片墙。
**陷阱**：① 别为 Bento 而 Bento——模块内容不独立时强行切格子反而割裂；② 响应式断点要测，移动端通常退化为单列。

### 风格 E — Dopamine / 高饱和

**定位**：高饱和、情绪化、抓眼球。Figma Trend 3。**2026 下半场出现疲劳反信号**（jasminedirectory："by late 2026, dopamine color fatigue"）。
**适用**：消费类品牌、活动营销页、情绪化产品（健身/社交）。
**陷阱**：① 疲劳信号已现——用 1 个主饱和色 + 中性辅色，别全饱和；② 对比度要测，高饱和色对白字常不达 4.5:1。

### 风格 F — Human Touch / Anti-AI Crafting（2026 元叙事）

**定位**：反 AI 同质化的系统化手法。designmagazine 称 "$50M Handmade Rebellion"。**核心：把 imperfection 编码进 design token，而非贴 scribble overlay 装饰。**
**适用**：品牌站、agency、奢侈品、需要差异化的产品。
**陷阱**：① 低段位做法（贴 scribble overlay）无法 scale 且仍是"AI 打底 + 人补丁"；② 高段位（token 编码 imperfection）才系统化——判定标准：手工痕迹能在 token 文件被命名并跨页复用。

## 阶段 1.5 — 品牌 DESIGN.md 实例库（pattern ↔ instance）

阶段 1 的 6 种模板是抽象 **pattern**。当用户**点名具体品牌**（"做 Linear 风""做成 Stripe 那样"）时，先取该品牌的真实 DESIGN.md 作为精确 token 来源，再套对应模板——pattern 管结构（surface ladder / 阴影哲学 / 字距哲学），instance 管精确色值 / 字距 / 圆角。

品牌资产库（VoltAgent/awesome-design-md，74 个真实品牌）的发现方式与 fallback、用法步骤、完整 slug 索引、6 模板 ← 代表品牌映射、DESIGN.md 格式 gap（hex + 自定义 YAML，不能直接当 DTCG token 消费）——全部见 [references/brand-index.md](references/brand-index.md)。

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

**门控**：步骤 8 不过不算完成。OKLCH 主题切换会因 gamut mapping 静默改变对比度（警告全文唯一真相源：design-system-workflow「阶段 3 — OKLCH 双主题切换」节），必须在真实设备复测。

## 阶段 3 — 动效与微交互（"高级感"的关键）

### 3.1 动效曲线 token 与微交互范式

2026 调研发现四家 design token 文件**均无 motion/easing token**——这是 token 体系的结构性缺口，需自建。自建 token 的 CSS 模板、四家收敛的微交互参数（hover/focus/状态切换/页面转场）见 [references/motion.md](references/motion.md)。

**反 AI 信号**：① 避开 `linear` 和默认 `ease`（最算法）；② spring 给"质量感"；③ 微交互 duration 0.15-0.2s。

### 3.2 动效工具选型决策（调研实测 bundle）

**本节为动效库选型表的唯一权威版本——其他 skill（frontend-stack-selection 等）引用本节，不复制。**

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

## 阶段 4 — 动效 a11y 门（WCAG 2.2 SC 2.3.3 强制）

**本节为 reduced-motion/WCAG 2.3.3 实现模板的唯一权威版本——其他 skill 引用本节，不复制。**

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
