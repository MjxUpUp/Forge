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
  composes: [frontend-feature-development, test-discipline, verification-driver]
  triggers: [{"event":"UserPromptSubmit","keywords":["迁移设计系统","旧设计系统","换色板","改主题色","token 迁移","design system migration","migrate tokens","rebrand","全套重做","重做主题","新建组件库","换设计系统","AI 味 UI 升级","风格统一"],"cooldown":300}]
---

# 设计系统迁移与组件库升级方法论

从旧 design token 到新设计系统的完整工程化流程。**核心思想：地基先行（token）+ 签名组件原子化 + 逐页确认门控**。

本文只留可迁移方法论。一次完整的单项目迁移实录（parchment/moonstone 自然材质色板、Frame/LiveDot/Stripe 签名组件、chat block 三段折叠、tide/ink/moss 正交主题）见 [references/case-study.md](references/case-study.md)——落地时对照取细节，别把实录当规范抄。

## 核心纪律（贯穿全程）

### 纪律 1：token-only（禁止硬编码）

规则唯一真相源：design-system-workflow「铁律：token-only 是底线」节，此处不复制。迁移场景的执行要点：新 token 先定义再使用，不临时写死 hex。

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

规则唯一真相源：frontend-feature-development「1.3 组件 API 范式」节，此处不复制。迁移场景的执行要点：所有新组件必须透传 `...props` 到根元素（含 `HTMLAttributes`），确保 `data-testid` 等 native 属性不丢。

### 纪律 5：test 行为契约不弱化

组件重构时维护语义等价：新选择器（`data-testid`）替代旧 class 选择器但**断言意图不变**。不因 CSS module hash 改弱断言或降级为宽松匹配。

## Phase 抽象框架

### Phase 0 — 地基（token 改造）

**目标**：把旧色板换成新设计系统，同时不破坏任何现有引用。

1. **变量重映射**：新增命名色板（自然材质命名，反 AI 同质化），旧变量名保留、值重映射到新色板（纪律 2），light/dark 各自一套
2. **签名 token 新增**（不进旧变量）：装饰条/纹理/网格/品牌字体等设计系统独有签名
3. **字体加载**：`@font-face` 声明品牌字体 → 全局生效
4. **签名动效**：设计系统独有动效（如步进闪烁），全局 `prefers-reduced-motion` 门覆盖——实现模板唯一真相源：frontend-aesthetics-execution「阶段 4 — 动效 a11y 门」节
5. **body 质感**：纹理/网格叠加，非纯色底的材质感

**验证**：类型检查零错误；全量测试通过；浏览器看 computed style 确认旧变量值已换成新色板；body 背景看到质感层。

### Phase 1 — 原子组件库（签名原子）

**目标**：把设计系统的独有视觉签名做成可复用的原子组件——compound + variant 化，透传 `HTMLAttributes`；再把签名挂接到现有组件（伪元素装饰条、variant 扩展）。**验证**：所有原子组件有测试（至少渲染 + variant + a11y）；现有组件测试不破。

### Phase 2 — 布局外壳（自动继承）

**目标**：TitleBar/StatusBar/Sidebar 等外壳风格统一。这一步通常是**改动最小**的——Phase 0 的 token 改造让已 token-driven 的外壳自动继承新色板，只补签名装饰（品牌字、装饰条、签名动效状态点）。**验证**：类型检查 + 全量测试通过；HTML 确认页展示完整布局。

### Phase 3-N — 逐页重构（最高业务价值优先）

**每页重构流程**：读现有组件 → 确定改造边界 → 新建/更新组件 + CSS → 跑测试 → 生成 HTML 确认页 → **用户确认才进下一页**。

原则：
- 业务逻辑零改动，只换视觉层
- 旧 class 选择器迁移到 `[data-testid="..."]`（绕开 CSS module hash），断言意图不变
- 组件提取优先采用成熟范式（如 chat block 流 → 三段折叠），不从零发明

### Phase N — 多套正交主题（palette × light/dark）

**目标**：不止一套设计系统，而是 N 套风格主题 × 亮暗模式正交组合。

- `data-palette` × `data-theme` 属性组合选择器：每 palette 一套变量覆盖，palette × dark 再覆盖
- Settings 类型加 `palette` 字段 + 切换 UI（风格选择器 + 亮/暗 segmented），切换即时生效
- OKLCH 双主题的色彩工程（gamut mapping 退化警告）唯一真相源：design-system-workflow「阶段 3 — OKLCH 双主题切换」节——与本 Phase 是同一工程问题，此处不重复写法

## 常见陷阱与自查清单（Gotchas）

### 陷阱 1：签名单用错场景

硬切签名条（双色一刀切渐变）只适合**固定宽度装饰条**，**不能用于变宽度进度条**——硬切段会出现在进度条中间任意位置，像断裂。→ 进度条用纯 accent 色，hard-cut stripe 只给固定宽度元素。

### 陷阱 2：CSS module `composes:` 不兼容 lightningcss

Vite 8 用 lightningcss 作 CSS minifier，不支持 CSS Modules 的 `composes:` 语法。→ 避免使用 `composes:`，用普通 CSS 类组合。

### 陷阱 3：同名类型多处定义不同步

store 文件与 types 文件各自定义同一个 interface，改一处漏另一处，类型悄然分叉。→ 改类型前 `grep` 找所有定义处。

### 陷阱 4：props 不透传导致 data-testid 丢失

新组件忘记 spread `...props` → `data-testid` 不生效 → 测试失败。→ 所有新组件必须 `& HTMLAttributes<HTMLDivElement>` + spread `...props` 到根元素。

### 陷阱 5：切暗色后边框不可见

暗色 `--border-default` alpha < 0.35 在深背景上几乎隐身。→ 暗色 border alpha 至少 0.40+；subtle variant 角标在暗色下额外提亮。

## 与其他 skill 的分工

- 动手写组件前 → **frontend-feature-development**（a11y/token/compound API 纪律）
- 断言守卫 + test 迁移 → **test-discipline**
- 端到端验证 → **verification-driver**
- 初次建 design token → **design-system-workflow**（本 skill 假设已有旧 token 要迁移，不是从零建；OKLCH 双主题色彩工程也归它）
- 选组件库底层 → **frontend-stack-selection**
