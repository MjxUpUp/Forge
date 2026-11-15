# 设计：入口路由与阶段机（任务优先，change 可后补）

change: `entry-routing`

## TL;DR

任务先开工是合法的；change 是记录单元，可后补。`next` 按阶段给建议，`task start --change` 只检查「链接的 change 已 ready」，其余不拦。

## 0. 决策

**任务是一等入口，change 是记录单元。** 机器只在两处做硬事：

1. **路由**：`forge next` 在「声明了 spec 的仓库 + 脏树 + 没有活跃任务」时，按现有 change 的阶段给下一步（ready → 直接开工；未完成 → 先看 phase；没有 → 建议立 change），而不是无条件 `task start`。
2. **链接检查**：`task start --change <id>` 时该 change 必须 `ready`，否则钉进去的 goal 哈希与阶段不符。

除此之外不拦：不带 `--change` 的 `task start` 一律成功（小修、先干活后补记录都走这条）。

第一版曾把「先立 change」做成 `task start` 的前置（`--kind task` 作逃生舱），实测把两条正路堵死，且 fast change 因缺 design/tasks 永远到不了 ready。本设计是反向修订，保留的约束只有「链接了就一致」。

## 1. 路由（A1）

`internal/taskpipeline/next.go`：

```go
type NextContext struct {
    Branch        string
    Dirty         bool
    BehindRemote  int
    SpecDeclared  bool
    OpenChanges   []string   // 未归档 change 的 id（字母序）
    ReadyChanges  []string   // 其中阶段已 ready 的 id
}
```

决策表（脏树、无活跃任务）：

| 条件 | 下一步命令 | Reason 要点 |
| --- | --- | --- |
| dirty && `len(ReadyChanges)>0` | `forge task start --ref feat/<id> --branch --title "<title>" --change <id>` | 已有 ready 的 change，直接带契约开工 |
| dirty && `len(OpenChanges)>0` | `forge change phase <id>` | 已有未完成 change，看还缺什么；也可先不带 `--change` 开工 |
| dirty && SpecDeclared && no change | `forge change new <id>` | 建议先立 change；小修可 `--kind task` 直接开工 |
| dirty && !SpecDeclared | `forge task start …`（原样） | 未声明契约，行为不变 |

`internal/cli/next.go` 的 `nextContext` 负责填 `SpecDeclared`（`specroot.Declared`）、`OpenChanges`（`change.List`，过滤 `c.Archive`）与 `ReadyChanges`（逐个 `change.ComputePhase == PhaseReady`）。`--json` 的 `state` 新增 `ready_changes`，旧字段不变。

## 2. 链接检查（A1 守卫）

`internal/clitask/task_entry.go`：

```go
func guardEntryRouting(root, kind, changeID string) error {
    if changeID == "" { return nil }            // 任务是一等入口
    phase, hints, err := change.ComputePhase(root, changeID)
    if err != nil { return err }
    if phase != change.PhaseReady {
        return fmt.Errorf("change %q 还停在 %s 阶段——先补齐产物再链接（%s）；也可以先不带 --change 开工，事后再补 change", ...)
    }
    return nil
}
```

- `changeID != ""` 时**不因 `--kind task` 短路**：显式链接了 change，就得满足它的阶段。
- 调用点在 `task.go` 里读完 `--worktree` 之后、任何磁盘副作用之前。
- 老 change（无 goal.md）阶段规则见 §3：可以到达 `ready`，但不能带 `--change` 起任务（钉契约需要 goal.md）；`change phase` 的下一步会据此改口径，`next` 也不会推荐它。

## 3. 阶段机（A2）

`internal/change/phase.go`，阶段从文件推导、不写状态：

| 阶段 | 判据 |
| --- | --- |
| `specify` | `HasGoal \|\| GoalRequired` 且 `ParseGoal` 失败（缺段/空段/残留 `forge:fill`）；没有 goal.md 的老 change 跳过这一段 |
| `clarify` | `clarify.md` 不存在，或 `ParseClarify` 失败（缺 `## 边界` 正文或缺 `人确认：`） |
| `plan` | `proposal.md`/`design.md` 缺失、仍是脚手架（`forge:fill` 标记） |
| `tasks` | `acceptance.md`/`tasks.md` 缺失或脚手架、0 条任务、或有任务没写 `Criteria` |
| `ready` | 以上都齐；`fast` change 跳过 design/tasks |

```go
type Phase string
const (
    PhaseSpecify Phase = "specify"
    PhaseClarify Phase = "clarify"
    PhasePlan    Phase = "plan"
    PhaseTasks   Phase = "tasks"
    PhaseReady   Phase = "ready"
)
```

- `ComputePhase(root, id) (Phase, []string, error)`：`ValidID` → 目录存在（归档目录单独报「已归档」）→ 逐级判定；`hints` 是「缺什么」的人话列表，供 `change phase` 与 `change start` 报错复用。
- `UnboundTasks` 与 `change gate --task N` 共用 `ParseTasks` 的 `Criteria` 解析。
- `change status` 的 `Status` 增加 `phase`（omitempty），`nextSteps` 按阶段给下一步；`list` 的 `STAGE`（交付/证据阶段）与 `phase`（开工前缺什么）是两套概念，不互相替代。

### clarify 记录

`forge change clarify <id> --by-human --note "…"`：

- `--by-human` 必需（agent 不得自批）；
- `--note` 至少 10 个字；`--note-file` 必须在仓库内；
- 渲染 `## 边界`、`## 非目标`（总是取 goal.md 的 Out of Scope）、`人确认：<RFC3339>`；
- 已存在要 `--force`；解析器接受 `non-goals`/`non_goals` 变体与空格分隔的时间戳。

## 4. 任务绑定（A3）

- `tasks.md` 每条任务写 `（判据：acceptance #N）`；
- 未绑定的任务让阶段停在 `tasks`（不是 `ready`）；
- `change gate --task N` 复用同一解析，不存在/已勾选/没判据编号都拒绝。

## 5. 测试

| 覆盖 | 测试 |
| --- | --- |
| next 决策表 | `TestNextDecision_EntryRouting`（四象限 + ready change 前进） |
| task start 一等入口 | `TestTaskStart_NoChangeAllowedInSpecRepo`、`TestGuardEntryRouting_AllowsWithoutChange` |
| 链接检查 | `TestTaskStart_ChangeMustBeReady`（`--kind task` 不能绕过显式链接） |
| 阶段推导 | `TestStage_Progression`、`TestStage_UnboundTaskBlocksReady`、`TestStage_FastChangeReadyAfterClarify`、`TestStage_LegacyChangeWithoutGoal`、`TestClarify_NonGoalsHyphenVariant`、`TestUnboundTasks` |
| e2e 真实链路 | `TestEntryRoutingTaskFirstThenChange`（脏树 → 建议 → 直接开工 → abort → 立 change → 未 ready 拒 → 补齐 → ready → next 前进 → 带 `--change` 开工）、`TestChangeStageBlocksEarlyTask` |
| 文档/托管块 | `TestAgentsProtocol*`、`TestHelpSurface_*` |

## 6. 影响面

- 源码：`internal/taskpipeline/next.go`、`internal/cli/next.go`、`internal/clitask/task_entry.go`、`internal/clitask/task.go`、`internal/change/phase.go`（新）、`internal/change/status.go`、`internal/cli/change_phase.go`（新）。
- 产物/测试：`spec/changes/entry-routing/*`、`internal/{change,clitask,taskpipeline,e2e}/*_test.go`、`internal/evalkit/{artifact,wedgedrill}.go`、`evals/forge/resume/*.yaml`、`scripts/checks/gofmt-clean.sh`。
- 文档：`docs/reference/cli.md`、`README.md`、`internal/change/templates/agents-protocol.md`、`docs/design/loopspec-alignment-roadmap.md`；`compat.snapshot.json` 重钉（194 命令）。

## 7. 风险

| 风险 | 缓解 |
| --- | --- |
| 路由把「先干活后补记录」堵回去 | 显式规则：不带 `--change` 永远放行；e2e 第一条路径就钉住它 |
| fast change 永远到不了 ready | 阶段表里 fast 跳过 design/tasks；`TestStage_FastChangeReadyAfterClarify` |
| 老 change 被误判 | 没有 goal.md 跳过 specify；`TestStage_LegacyChangeWithoutGoal` |
| 阶段与证据混淆 | 阶段只描述「开工前还缺什么」，verify 仍按 acceptance 实跑；文档明确两条轴 |
| 提示文案与实现漂移 | 守卫报错引用同一份 hints；`TestHelpSurface` 钉住 cli.md 的关键串 |
## 8. 不做（Non-goals）

- 不引入 plan digest 批准、逐节点 PASS/FAIL 报告（LoopSpec 的耗时来源）。
- 不做 fragment/profile 编译（roadmap 的 R1–R4），本 change 只做 A1+A2+A3。
- 不改 acceptance 围栏语法与 verify 的判定方式。
- 不把 `--kind task` 做成旁路：它只是标记，门禁不减。
- 不自动创建 change 骨架之外的产物；clarify 必须有人回答。

## 9. 验证（Verification）

- `forge change verify entry-routing` 的 9 条围栏（build、next、task-first/link、phase/clarify、e2e、托管块、help 文档、gofmt、deadcode）。
- 变异验证：`task_entry.go` 的 `return nil`、`phase.go` 的 `false &&`、`next.go` 的 `false &&` 三处注入均让对应用例变红。
- 双评审：代码评审对 diff 与规格逐条核对；文档评审按 L2 打分（≥75 才记录）。
- 发布后用子 agent 在真实仓库跑一次「脏树 → next → task start → change 后补 → complete」，观察每一步是否与文档一致。
