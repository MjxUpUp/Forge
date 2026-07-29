---
name: frontend-stack-selection
description: "前端技术栈选型决策器，强制先问场景再推荐，反对无脑套用 shadcn+Tailwind+v0 主流组合。Use when: 新前端项目选型、选组件库、Tauri vs Electron、Biome vs ESLint、CSS-in-JS 还能用吗、要不要 Tailwind、设计系统怎么搭、重构前端技术栈、用户问'前端用什么/组件库选哪个/CSS 方案选哪个/桌面端用什么'时。SKIP: 单点查某库 API 签名（用 dev-lookup）、通用技术方案论证（用 evidence-based-proposal）、已有项目代码审查（用 frontend-code-review）、动手写组件（用 frontend-feature-development）。"
metadata:
  pattern: inversion + gate
  domain: frontend
---

# 前端技术栈选型决策器

选型不是"选最流行的"，是"选当前场景最合适的"。2025-2026 主流 framing（shadcn+Tailwind+v0）在多个场景下都有硬反方证据，无脑套用 = 制造未来维护负担。

## 铁律：开工前必须问 4 个场景问题（Inversion 门控）

**推荐技术栈前，先确认以下 4 个问题，缺一不可。用户没答全 → 问完再推荐，不猜：**

1. **团队规模 + 维护周期**：<5 人 / 短期 MVP，还是 10+ 人 / 多年长期产品？
2. **合规约束**：是否面向欧盟（EAA 2025-06 强制）/ 金融 / 医疗 / 政务？（WCAG 2.2 是法律红线）
3. **多框架需求**：纯 React，还是需要 Vue / Svelte / Solid 多框架？
4. **平台**：Web only，还是桌面端（Tauri/Electron）/ 移动端？

**用户确认前不要给推荐栈。** 这是 gate，不是建议。

## 反主流 framing（最高信号）

主流说"shadcn+Tailwind+v0 是最佳实践"，但调研（2026-06，12 子代理/450 次搜索）发现每个都有硬反方证据：

| 主流说法 | 反方证据 |
|---|---|
| "shadcn 适合所有项目" | 企业级 50+ 组件长期维护有同步成本税（discussion #9756 无解、issue #3579 追不上游、registry RCE 风险）；Design Systems Collective："Shadcn Isn't Ready for Enterprise Design Systems" |
| "Tailwind 让 bundle 更小" | 实测 57.6% 页面体积是 inline class；CSS 45→8KB 但 HTML 120→340KB 净增 183KB |
| "v0/Bolt 生成能上生产" | arXiv 2603.28592：89.3% code smell；Lovable 官方承认"不要复用共享组件"；CVE 6→74 |
| "Tauri 比 Electron 省内存" | Hello World 是；复杂应用反超 16-141%（系统 WebView 子进程未计入，issue #5889） |

**选型永远实测目标平台的真实复杂场景，不信营销基准。** "营销基准 vs 真实复杂场景"的系统性偏差是本次调研最重要的方法论发现。

## 6 场景决策表

| 场景 | 推荐技术栈 | 理由 |
|---|---|---|
| 创业 MVP / 中小项目（React） | **shadcn/ui + Base UI（新）或 Radix（成熟）+ Tailwind v4 + Biome** | 控制权 + 轻量 + 生态最广；shadcn 117K stars |
| 企业级 / 多技术栈 / 受监管 | **IBM Carbon（6 框架）/ MUI / Mantine** | 多框架 + a11y 持续审计 + 供应链安全；shadcn 企业级未达标 |
| WCAG 法律严苛（欧盟 EAA） | **React Aria（headless）+ Tailwind** | a11y 金标准 "does not compromise"；Radix 2025 仍未修完审计 |
| 多框架（Vue/Solid/Svelte） | **Ark UI + Park UI / Nuxt UI v3 / daisyUI** | shadcn 绑定 Radix 仅 React；跨框架选 Zag.js 系 |
| 桌面端 | **Tauri v2（体积启动敏感）/ Electron（复杂应用）** | Tauri bundle 小 96% 但复杂应用内存反超；先实测目标平台 WebView |
| 内容站 / 非 React / 零 JS | **daisyUI（35 主题 + 多框架 + 零 JS 开销）** | 内容站无需组件交互重量 |

## 维度子决策

### CSS / 样式架构
- 新项目默认：**Tailwind v4（@theme CSS-first）+ CSS 变量做 design token + OKLCH 色彩**
- RSC 项目 / 设计系统：**零运行时 vanilla-extract 或 Panda CSS**（styled-components 2025-03 进维护模式）
- 超大规模（Meta 级，数千人共改）：**StyleX**（last wins 确定性合并治理）
- 原生 CSS 已可用：`:has()` / nesting / `color-mix()` / `@layer` / `@container`（2025-08 Baseline）

### 代码规范工具链
- 新项目 / 性能敏感：**Biome（lint+format 一体）+ TS strict 三件套（strict+noUncheckedIndexedAccess+exactOptionalPropertyTypes）+ Lefthook + pnpm + Turborepo**
- 重度 typescript-eslint 规则依赖：**ESLint v10 flat config + Prettier + typescript-eslint**（可叠加 Oxlint 做"快速过滤 + ESLint 收尾"双层）

### 组件库 primitives 层（2025-12 起）
- 新 React 项目底层：**Base UI**（Radix+MUI+Floating UI 三团队共建，v1 2025-12，有势头）
- 已有 Radix 项目：**不迁移**（pkgpulse："don't migrate"，生态成熟）
- 多框架：**Ark UI**（Zag.js 状态机，React/Vue/Solid/Svelte）

### 动效
- 轻交互：**Rive**（state machine，~200KB gzip，文件比 Lottie 小 10-15 倍）
- 3D 沉浸：**Spline**（no-code，runtime 544KB）/ **React Three Fiber**（代码控制，three.js ~150KB）
- **必须过 prefers-reduced-motion**（WCAG 2.2 SC 2.3.3 强制）

## Common Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "大家都用 shadcn，肯定没错" | 调研发现 shadcn 企业级有同步成本税；多框架/合规场景 Carbon/MUI/Mantine 更稳 |
| "Tailwind 让 bundle 更小" | 57.6% 页面体积转到了 HTML；看 gzip 后 total bytes + HTML parse 成本 |
| "v0 生成的能直接上生产" | 89.3% code smell；Lovable 官方承认反 DRY；当 junior 代码审查 |
| "Tauri 比 Electron 省内存" | Hello World 是，复杂应用反超 16-141%（WebView 子进程未计入） |
| "选最流行的准没错（star 数高）" | 性能已赢生态未赢；选型看场景不看 star 数 |
| "Biome 已经替代 ESLint 了" | 性能碾压但 type-aware 仍是 typescript-eslint 主场；重度规则依赖别迁 |
| "CSS-in-JS 已死" | 过度简化；Emotion 1265 万/wk；styled-components v6.3 RSC 兼容；是"维护者劝退新项目"非技术废止 |

## Red Flags（我在 rationalize 的信号）

- 没问团队规模 / 合规 / 多框架 / 平台就推荐
- 只凭 star 数或 npm 下载量推荐
- 引用工具官方营销话术而非实测 / 独立评测
- 对所有场景推荐同一套（shadcn+Tailwind+v0）
- 用"行业最佳实践"代替场景化判断

## Gotchas

- **shadcn 的"NOT a component library"定位**：2025-05 起官方改为 "code distribution platform"，是源码分发不是 npm 依赖；选它意味着接受"组件源码归你，同步成本也归你"
- **Base UI vs Radix 不是非此即彼**：shadcn 2025-12 起底层二选一；新项目选 Base UI（势头），存量 Radix 别动
- **OKLCH 在 sRGB 设备会静默 gamut mapping**：改变亮度致对比度退化，dark/light 双主题需复测对比度
- **Tauri 三平台 WebView 质量不均**：Linux WebKitGTK 渲染最差；选 Tauri 前实测目标平台
- **WCAG 2.2 SC 2.3.3 不只是"加个 media query"**：relying on 'Reduce Motion' is NOT enough（r/accessibility 共识），需主动动效克制

## 与其他 skill 的分工

- 选完技术栈，动手写代码 → **frontend-feature-development**
- 用 AI 工具生成 UI → **ai-ui-generation-workflow**
- 建设计系统 / Design Token → **design-system-workflow**
- 提交前审查前端代码 → **frontend-code-review**
- 选型方案要正式论证 → **evidence-based-proposal**
- 单点查某库 API 签名 → **dev-lookup**
