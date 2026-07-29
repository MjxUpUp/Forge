---
name: frontend-development
description: "前端开发强制规范：UI 组件 / 状态 / Design Token / a11y / 测试 / 性能 / 审美 / AI 生成 UI 审查。Use when: 写前端组件/页面/应用、改 UI 状态管理、加 a11y、补前端测试、调性能、选 Design Token/技术栈、把控样式审美、AI 工具生成 UI 落地审查时、给 agent 出前端任务 SOP 时。SKIP: 纯样式一次性手写改动（用 code-review-gate 简单 review 即可；但 AI 工具生成的 UI 走 §2.10 不免审）/ 全栈 API 设计（用 backend-development）/ 数据库 schema（用 database-design）。"
metadata:
  pattern: tool-wrapper
  domain: frontend
  composes: [code-review-gate, test-discipline, tdd-cycle, integration-test-architecture, on-demand-guards]
---

# 前端开发规范

> **本 skill 不重复**: 提交前审查 → `code-review-gate`。Design Token 选型 / 样式审美 / AI 生成 UI 审查已内联本 skill（§2.8 / §2.9 / §2.10）。本 skill 解决"按 SOP 写出/改前端代码"的工作流纪律。

## 1. 决策树（前端开发路径）

```
任务是什么？
├─ 新建组件/页面 → §2.1 组件创建 7 步（命名→spec→Token→a11y→state→测试→code-review）
├─ 改现有组件 → §2.2 改不破坏（read-then-modify + 反向契约保留）
├─ 状态管理 → §2.3 状态决策树（local vs lifted vs global store）
├─ 性能调优 → §2.4 性能自检清单（re-render/瀑布/打包/bundle）
├─ a11y 补强 → §2.5 a11y 必做清单
├─ 测试 → §2.6 测试策略（单元/集成/E2E/视觉回归）
├─ 整页布局/Design Token → §2.7 设计语言边界（什么改/不改）
├─ 选 Design Token/技术栈 → §2.8 Token 与技术栈选型
├─ 样式审美把控 → §2.9 审美执行清单
└─ AI 工具生成 UI → §2.10 AI 生成 UI 安全审查（必经）
```

## 2. 路径规范

### 2.1 新建组件 7 步（按顺序）

1. **命名**：kebab-case + 功能描述（不叫 `MyCard`/`Card1`，叫 `user-avatar-card`）
2. **Spec**：先写 props 接口 + 受控/非受控决策（不要边写边猜）
3. **Design Token**：所有颜色/间距/字号/圆角用 token，不写裸值（`text-color-primary` 不是 `#1a1a1a`）
4. **a11y 基线**：语义化标签 + 键盘可达 + ARIA + 焦点管理（不只"看得见"）
5. **State**：本地 vs lifted vs global store（§2.3）
6. **测试**：unit（行为）+ visual regression（如适用）+ a11y assertion
7. **Code-review**：提交前触发 `code-review-gate`，过 commit-quality-assertion

### 2.2 改现有组件 — 不破坏契约

读完全部现有 props/state/effect，再动。改前/后运行：
```bash
forge review pass
```
**禁止**：悄悄改 props 形状、改组件副作用、改对外 className。

### 2.3 状态决策树

```
状态该谁拥有？
├─ 仅本组件用 → useState/useReducer（local）
├─ 父子/兄弟 1 层 → lifted（props + callback）
├─ 跨 3+ 组件 → context 或全局 store（Zustand/Redux/Pinia 看 stack）
├─ 服务端真相来源 → React Query/SWR/Vue Query（不复制到 local）
└─ 表单态独立 → react-hook-form/formik（不污染业务 state）
```

**禁止**：全局 store 当 local 用（一个组件 toggle 也 setGlobal）；local state 跨组件（prop drilling 3 层还不抽）。

### 2.4 性能自检清单（每改一个组件跑一遍）

- [ ] 列表/循环有 key（不要 index 当 key）
- [ ] `useEffect`/`useMemo`/`useCallback` 依赖数组完整（无遗漏警告）
- [ ] 大列表虚拟化（>1000 行考虑 react-window/vue-virtual-scroller）
- [ ] 大图 lazy load + `width/height` 锁住 CLS
- [ ] 路由级 code-split（`lazy()` + `Suspense`）
- [ ] CSS bundle 体积监控（CSS > 50KB 警告）

### 2.5 a11y 必做（提交前 checklist）

- [ ] 语义 HTML 优先（button 不用 div 加 onClick、nav 不用 div）
- [ ] 键盘可操作（Tab 顺序合理、Enter/Space/Arrow 按预期）
- [ ] ARIA 仅在语义 HTML 不够时补（不滥用）
- [ ] `alt` 必填（装饰图 `alt=""`）
- [ ] 颜色对比 ≥ 4.5:1（自动化 axe-core）
- [ ] 焦点环可见（不 `outline: none` 不补 focus style）

### 2.6 测试策略

| 层 | 工具 | 测什么 |
|---|---|---|
| 单元 | Vitest/Jest | 纯函数、reducer、组件 props→output |
| 组件 | Testing Library | 行为（不测实现）：用户输入→输出/副作用 |
| 集成 | Testing Library + MSW | 多组件协作 + API mock |
| E2E | Playwright | 关键路径（登录/支付/转换）|
| 视觉 | Percy/Chromatic | UI 回归（可选，团队规模小时不必）|

**铁律**：不测实现细节（state 名/函数引用次数），测用户行为。

### 2.7 设计语言边界

```
改不改？这是 design system 决定，不是开发者决定。
├─ 颜色/字号/间距 → 改 token，不改样式
├─ 组件 API 形状 → 提 PR 到设计系统 owners，不在业务仓改
├─ 临时样式（一次性）→ 标 `// TODO(design-system)` + 开 issue
└─ 框架升级 → 走 §2.8 选型 + ADR（`architecture-decision-record`），不是开发者私自升
```

### 2.8 Design Token 与技术栈选型

选型是 design system / 团队约定决定，不是开发者自决。

**Token 选型**：
- 用既有设计系统的 token 体系（颜色/间距/字号/圆角/阴影），不自造新 token
- 新 token 须设计系统 owner 批准并记 ADR（`architecture-decision-record`）
- token 命名语义化（`text-color-primary` 非 `color1`），不绑死值

**技术栈/框架选型**（CSS 方案/状态库/组件库/major 升级）：
- 按团队既有约定，不私自换栈（Tailwind ↔ CSS-in-JS 之类）
- major 框架升级 / 换栈走 ADR：为何选 / 替代方案 / 迁移成本，不在业务仓私自升
- 新 UI 库/样式方案引入须评审（包体积/维护/许可证）

**禁止**：开发者私自引新 UI 库、换样式方案、升 major 框架。

### 2.9 样式审美执行（审美与性能 go together）

审美不是凭感觉，是可 checklist 化的一致性纪律。

- [ ] 间距用 token 倍数（4/8 基线），不裸写任意 px
- [ ] 对齐：相邻元素基线/网格对齐（不差 1-2px）
- [ ] 视觉层次：大小/粗细/颜色对比体现优先级（主操作最醒目）
- [ ] 一致性：同类组件同间距/同圆角/同交互态（hover/active/disabled 统一）
- [ ] 响应式：断点处不破版（不只桌面好看）
- [ ] 暗色模式：token 驱动，不硬编码颜色

**审美与性能**：动画用 `transform`/`opacity`（不触发 layout），大图压缩 + lazy，CSS 体积监控（§2.4）。

### 2.10 AI 生成 UI 安全审查（v0/bolt/Cursor/Claude 出 UI 后必经）

AI 生成 UI 省时但高频埋雷，落地前必过此清单。

**安全**：
- [ ] XSS：审 `dangerouslySetInnerHTML`/`v-html`/动态渲染点，AI 常直接插未转义用户输入
- [ ] 敏感信息：AI 可能硬编码 key/token/密钥进前端（`grep -rniE "key|token|secret|password"` 排查）
- [ ] 依赖安全：AI 引入的包是否可信（查 npm 下载量/维护/已知 CVE），不盲装

**正确性**：
- [ ] 幻觉 API：AI 调用的库 API/props/组件名是否真存在（查官方文档，不信 AI 记忆）
- [ ] 类型：AI 生成的 TS 真过 `tsc --noEmit`（不靠 AI 自述"类型对"）

**a11y（AI 最易漏）**：
- [ ] AI 常生成 `<div onClick>` 当按钮 → 改语义标签 + 键盘事件（§2.5）
- [ ] `alt`/ARIA/焦点管理是否齐全

**纪律**：AI 生成 ≠ 验证过。过完清单才提交，再触发 `code-review-gate`。

## 3. 负向约束 + 替代方案

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| 裸写 `#fff` / `14px` / `8px` | 引用 `var(--color-primary)` / token 名 |
| `<div onClick={...}>` 当按钮 | `<button>` + 键盘事件 |
| `index` 当列表 key | 业务稳定 id（slug/UUID/`item.id`） |
| `useEffect` 里 setState 触发父更新 | 用派生 state 或在事件 handler 内 set |
| 复制粘贴多份相似组件 | 抽公共 prop + 抽 slot/children |
| 全局 store 存每个组件开关 | 局部 useState；真正全局才上 store |
| `any` prop 类型 / 不写 TS | 严格 props interface + JSDoc |

## 4. Post-Generation 自查清单（每完成一个组件跑一次）

- [ ] 文件 < 200 行（不超 300）
- [ ] props 接口显式 + 必填项有 JSDoc
- [ ] 无 `console.log` 残留（`grep -rn "console\." <file>`）
- [ ] 无未用 import + 无未用 state
- [ ] a11y 自动化测试通过（axe-core 0 violations）
- [ ] unit/component test 覆盖率对该组件 ≥ 80%
- [ ] 运行 `forge review pass` 通过

## 5. Gotchas（实操易错点）

**G1**: 改组件前没 grep 同名组件复用 → 重复造轮子。预防：`grep -rn "import.*Card" src/` 先看是否已存在。

**G2**: token 没真生效（写在 `tailwind.config` 但 `theme.extend` 漏写）→ 改了等于没改。预防：跑 `pnpm build` 看 bundle CSS 变量真有。

**G3**: a11y 测试只在桌面跑 → 移动端焦点环/键盘失效。预防：CI 强制 mobile emulation。

**G4**: 状态从 props init 后忘了 sync → 父更新子不更新。预防：用 `key` 强制重 mount 或用 React Query `useQuery` 自动 sync。

**G5**: 错误边界 missing → 后端崩则整页白屏。预防：根 `<ErrorBoundary>` 包 + 关键组件独立 fallback。

## 6. 提交前必跑

```bash
# 1. 静态分析（类型检查；auto-compile hook 会自动 advisory）
pnpm typecheck                        # tsc --noEmit（TS/Vue/Svelte）

# 2. 测试 + 覆盖率
pnpm test --coverage --coverageThreshold=80

# 3. a11y
pnpm test:a11y                          # axe-core

# 4. 代码审查 + commit 守卫
forge review pass                        # 触发 code-review-gate
```

不过 → 看 §4 自查清单补足；过 → commit。

## 7. 与其他 skill 的协作

- **架构设计层**：`architecture-decision-record` — 写大改 ADR（多组件/多 store 改动）
- **测试层**：`tdd-cycle` + `test-discipline` — TDD 流程纪律
- **审查层**：`code-review-gate` — 提交前审查门禁
- **AI 生成 UI**：见 §2.10 — 用 AI 工具出 UI 后必经安全审查
- **审美与性能**：见 §2.9 — 审美执行清单，性能与审美 go together

## 参考

- 完整 references 进 `references/` 目录（stack 选项/性能案例库/Tailwind token 模板）
- 写法参照 `skill-authoring-standard`
- 规范有效性靠 `forge skills validate` + `forge skills audit` 双验证
