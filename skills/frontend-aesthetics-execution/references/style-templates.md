# 6 种风格 CSS Token 模板（可直接 copy）

SKILL.md 阶段 1 各风格的完整 token 模板。**token 值来自 2026 调研实测，非编造。** 定位/适用/陷阱等决策信息见主文件阶段 1，此处不重复。

## 风格 A — 暗色精致 SaaS（Linear/Vercel/Raycast 风）

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

## 风格 B — Apple Liquid Glass（WWDC 2025/2026）

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

## 风格 C — Neo-brutalism

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

## 风格 D — Bento Grid

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

## 风格 E — Dopamine / 高饱和

```css
@theme {
  --color-dopamine-1: oklch(75% 0.28 0);     /* 饱和橙 */
  --color-dopamine-2: oklch(70% 0.25 320);   /* 饱和紫 */
  --color-dopamine-3: oklch(80% 0.22 150);   /* 饱和绿 */
  /* 搭配大量留白 + 一个主饱和色，避免视觉过载 */
}
```

## 风格 F — Human Touch / Anti-AI Crafting（2026 元叙事）

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
