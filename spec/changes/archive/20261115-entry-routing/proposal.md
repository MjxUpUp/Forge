# 提案：入口路由与阶段机（任务优先，change 可后补）

change: `entry-routing`

## 问题

JustForMe 2026-11-15：9 个任务、0 个 change。agent 遵守了所有明文规则：

1. 先跑 `forge next`；
2. `next` 给出 `forge task start --ref <ref> --branch --title <title>`，理由是「工作区有未归属变更而无活跃任务」；
3. 硬规则要求「只执行 next 给的下一步」，于是直接建任务开工。

问题不在 agent，而在结构：`next` 的决策表只有 Task 状态（`internal/taskpipeline/next.go` 不 import `specroot`），change 是并行的一套可选产物；`task start --change` 是可选参数；goal 契约只在 implement 门禁做 advisory 校验。三处都没有「记录单元与执行单元的关系」。

第一版修复把关系定成「change 是 task 的前置条件」，实测反而堵路：小修与「先干活后补记录」都被拒，agent 用 `--kind task` 绕；fast change 因缺 design/tasks 永远到不了 ready。结论：**顺序反了**。任务才是执行单元，change 是记录单元。

## 提案

按真实工作方式定关系：

- **A1 路由**：`next` 知道仓库是否声明 spec、有哪些未归档 change、各自阶段。声明 spec + 脏树 + 无活跃任务时：
  - 有 ready 的 change → 直接给 `task start --change <id>`；
  - 有未 ready 的 change → 给 `change phase <id>`（提示也可直接 `task start`）；
  - 没有 change → 建议 `change new <id>`（理由说明小修可加 `--kind task`）。
  `task start` 永远可达：不带 `--change` 一律放行。
- **A1 链接检查**：显式带 `--change` 时，该 change 必须 `ready`（否则钉进去的 goal 契约与阶段不符）。这条与「开工许可」无关，不因 `--kind task` 短路。
- **A2 阶段机**：`change phase <id>` 从文件推导阶段并给出下一步；不写状态。
  - `specify`：goal 五段未填完（没有 goal.md 的老 change 跳过这一段）
  - `clarify`：缺 `clarify.md` 或缺「人确认」行（`forge change clarify --by-human --note` 生成）
  - `plan`：proposal / design 缺失或仍是脚手架
  - `tasks`：acceptance / tasks 缺失、没任务、或有任务没写判据
  - `ready`：以上都齐。fast change 跳过 design/tasks。
- **A3 任务绑定**：`tasks.md` 每条任务必须写 `（判据：acceptance #N）`；`change gate --task N` 与阶段推导共用同一解析。

## 与 LoopSpec 的关系

采用它的「入口是状态」这一结构（阶段从产物推导、下一步由工具给），不采用它的 plan 批准与逐节点报告。全流程只在 `clarify` 停一次（问人边界/非目标），人往返不因本 change 增加。

## 影响面

- `internal/taskpipeline/next.go`（`NextContext`/`ReadyChanges` 路由）、`internal/cli/next.go`（注入 spec 状态与阶段）
- `internal/clitask/task_entry.go`（链接检查）、`internal/clitask/task.go`（`--kind task`、`--change`）
- `internal/change/phase.go`（新）、`internal/cli/change_phase.go`（`phase`、`clarify`）
- 模板与文档：`internal/change/templates/agents-protocol.md`、`docs/reference/cli.md`、README、`docs/design/loopspec-alignment-roadmap.md`
- 测试：`next_test.go`、`task_entry_test.go`、`phase_test.go`、e2e 两条真实链路

## 不做

- fragment/profile 编译（R1）；
- plan digest 批准；
- 对未声明 spec 的仓库改变行为；
- 把 change 做成开工前置（本版明确反向）。

## 落地顺序

A1 路由 → A1 链接检查 → A2 阶段 → A3 绑定 → 模板/文档 → e2e 真实跑一遍 → 发布。