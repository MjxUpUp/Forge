# 组件实现细节参考

frontend-feature-development 阶段 2 实现时的速查：状态归属、性能、命名、自查清单（合并自原 frontend-development 的规范速查）。a11y / token-only / 组件 API 范式见主 SKILL.md 阶段 1，此处不重复。

## 改现有组件 — 不破坏契约（read-then-modify）

改现有组件（非新建）时，先读完全部现有 props/state/effect，再动。改前/后运行 `forge review pass` 对比。

**禁止**：
- 悄悄改 props 形状（加必填 prop / 改 prop 类型 / 改默认值）——破坏调用方
- 改组件副作用（新增/移除 effect、改 effect 依赖）——破坏行为契约
- 改对外 className（调用方靠 className 做样式覆盖）——破坏样式契约

> 与 frontend-feature-development 阶段 2「创建流程 7 步」互补：阶段 2 管"新建怎么写对"，本节管"修改怎么不破坏已有的对外契约"。

## 命名

- kebab-case + 功能描述：不叫 `MyCard` / `Card1`，叫 `user-avatar-card`

## 状态决策树

```
状态该谁拥有？
├─ 仅本组件用 → useState/useReducer（local）
├─ 父子/兄弟 1 层 → lifted（props + callback）
├─ 跨 3+ 组件 → context 或全局 store（Zustand/Redux/Pinia 看 stack）
├─ 服务端真相来源 → React Query/SWR/Vue Query（不复制到 local）
└─ 表单态独立 → react-hook-form/formik（不污染业务 state）
```

**禁止**：全局 store 当 local 用（一个组件 toggle 也 setGlobal）；local state 跨组件（prop drilling 3 层还不抽）。

## 性能自检清单（每改一个组件跑一遍）

- [ ] 列表/循环有 key（不要 index 当 key）
- [ ] `useEffect` / `useMemo` / `useCallback` 依赖数组完整（无遗漏警告）
- [ ] 大列表虚拟化（>1000 行考虑 react-window / vue-virtual-scroller）
- [ ] 大图 lazy load + `width/height` 锁住 CLS
- [ ] 路由级 code-split（`lazy()` + `Suspense`）
- [ ] CSS bundle 体积监控（CSS > 50KB 警告）

## 测试策略（前端 layer/tool 映射）

| 层 | 工具 | 测什么 |
|---|---|---|
| 单元 | Vitest/Jest | 纯函数、reducer、组件 props→output |
| 组件 | Testing Library | 行为（不测实现）：用户输入→输出/副作用 |
| 集成 | Testing Library + MSW | 多组件协作 + API mock |
| E2E | Playwright | 关键路径（登录/支付/转换）|
| 视觉 | Percy/Chromatic | UI 回归（可选，团队规模小时不必）|

**铁律**：不测实现细节（state 名/函数引用次数），测用户行为。测试质量守卫（防弱断言/假阳性）由 `test-discipline` 管。

## Post-Generation 自查清单（每完成一个组件跑一次）

- [ ] 文件 < 200 行（不超 300）
- [ ] props 接口显式 + 必填项有 JSDoc
- [ ] 无 `console.log` 残留（`grep -rn "console\." <file>`）
- [ ] 无未用 import + 无未用 state
- [ ] a11y 自动化测试通过（axe-core 0 violations）
- [ ] unit/component test 覆盖率对该组件 ≥ 80%
- [ ] 运行 `forge review pass` 通过

## 负向约束（与主 SKILL.md Rationalizations 不重复的项）

| 不要做 ❌ | 应该做 ✅ |
|---|---|
| `index` 当列表 key | 业务稳定 id（slug/UUID/`item.id`） |
| `useEffect` 里 setState 触发父更新 | 用派生 state 或在事件 handler 内 set |
| 复制粘贴多份相似组件 | 抽公共 prop + 抽 slot/children |
| 全局 store 存每个组件开关 | 局部 useState；真正全局才上 store |

## Gotchas（实操易错点）

**G1**: 改组件前没 grep 同名组件复用 → 重复造轮子。预防：`grep -rn "import.*Card" src/` 先看是否已存在。

**G2**: token 没真生效（写在 `tailwind.config` 但 `theme.extend` 漏写）→ 改了等于没改。预防：跑 `pnpm build` 看 bundle CSS 变量真有。

**G3**: a11y 测试只在桌面跑 → 移动端焦点环/键盘失效。预防：CI 强制 mobile emulation。

**G4**: 状态从 props init 后忘了 sync → 父更新子不更新。预防：用 `key` 强制重 mount 或用 React Query `useQuery` 自动 sync。

**G5**: 错误边界 missing → 后端崩则整页白屏。预防：根 `<ErrorBoundary>` 包 + 关键组件独立 fallback。
