# 提案：入口路由与阶段机

change: `entry-routing`

## 问题

JustForMe 2026-11-15：9 个任务、0 个 change。agent 遵守了所有明文规则：

1. 先跑 `forge next`；
2. `next` 给出 `forge task start --ref <ref> --branch --title <title>`，理由是「工作区有未归属变更而无活跃任务」；
3. 硬规则要求「只执行 next 给的下一步」，于是直接建任务开工。

问题不在 agent，也不在某一条文案，而在结构：`next` 的决策表只有 Task 状态（`internal/taskpipeline/next.go` 不 import `specroot`），change 是并行的一套可选产物；`task start --change` 是可选参数；goal 契约只在 implement 门禁做 advisory 校验。三处都没有「没有 change 就不能开工」的状态。

## 提案

把入口做成状态机，而不是再加一条建议：

- **A1 路由**：`next` 知道仓库是否声明 spec、有没有打开中的 change、工作区是否脏。
  声明了 spec + 脏树 + 无活跃任务 + 没有打开 change → 下一步是 `forge change new <id>`（理由说明功能改动走 change，小修可用 `--kind task`）。
  有打开 change → 下一步指向该 change 的阶段命令。
- **A1 守卫**：`task start` 在 spec 仓库里，没有 `--change` 且没有 `--kind task` → 直接报错，并给出两条正确出路。
- **A2 阶段机**：change 的阶段由文件推导，`forge change phase <id>` 打印当前阶段与下一步；`task start --change` 要求阶段为 `ready`。
  - `specify`：goal 五段未填完
  - `clarify`：goal 完成，但缺少 `clarify.md`（人或 agent 经人确认后 `forge change clarify --by-human --note` 生成）
  - `plan`：clarify 完成，design 未填完
  - `tasks`：design 完成，tasks 缺失或有任务没写判据
  - `ready`：以上都齐，可以 `task start --change`
- **A3 任务绑定**：`tasks.md` 每条任务必须写 `（判据：acceptance #N）`；`change gate --task N` 与阶段推导共用同一解析，没有绑定的任务不能进入 ready。

## 与 LoopSpec 的关系

采用它的「入口是状态」这一结构，不采用它的 plan 批准与逐节点报告。阶段由文件推导、plan/design 仍只是文档，因此不增加人往返：全流程只在 `clarify` 停一次（问人边界/非目标）。

## 影响面

- `internal/taskpipeline/next.go`（决策表 + 新参数）、`internal/cli/next.go`（注入 spec 状态）
- `internal/clitask/task.go`（`--kind task` 守卫、`--change` 阶段校验）
- `internal/change/stage.go`（新）、`internal/change/tasks.go`（绑定解析，若已有则复用）
- `internal/cli/change.go`（`stage`、`clarify` 两个子命令）
- 模板与文档：`internal/change/templates/agents-protocol.md`、`templates/clarify.md`、`docs/reference/cli.md`、README
- 测试：`next_test.go`、`task_*_test.go`、`stage_test.go`、e2e 两条真实路由

## 不做

- fragment/profile 编译（R1）；
- plan digest 批准；
- 对未声明 spec 的仓库改变行为。

## 落地顺序

A1 路由 → A1 守卫 → A2 阶段 → A3 绑定 → 模板/文档 → e2e 真实跑一遍 → 发布。