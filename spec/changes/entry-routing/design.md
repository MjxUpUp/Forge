# 设计：入口路由与阶段机

change: `entry-routing`

## 一句话

把「要不要走 change」从文案变成状态：`next` 读 change 状态，`task start` 缺省拒绝，change 阶段由文件推导且卡住未完成的产物。

## 1. 入口路由（A1）

### 决策表

`NextDecision` 增加上下文（新增 `NextContext`，保留旧签名的薄包装避免测试大改）：

```go
type NextContext struct {
    Branch       string
    Dirty        bool
    BehindRemote int
    // SpecDeclared 表示仓库有 spec/forge.yaml；未声明时所有新规则不生效。
    SpecDeclared bool
    // OpenChanges 是未归档 change 的 id（按字母序）；>0 时下一步先看它的阶段。
    OpenChanges []string
}
```

无活跃任务时（`st == nil`）：

| 条件 | Next | Reason |
| --- | --- | --- |
| dirty && SpecDeclared && len(OpenChanges)==0 | `forge change new <id>` | 「功能改动先立 change（goal+acceptance）；一句话小修可 `forge task start --kind task`」 |
| dirty && SpecDeclared && len(OpenChanges)>0 | `forge change phase <id>` | 「已有未完成的 change，先按它的阶段补产物」 |
| dirty（未声明 spec） | `forge task start …`（不变） | 不变 |
| 不 dirty && behindRemote>0 | `git pull --ff-only`（不变） | 不变 |
| 其余 | `forge status`（不变） | 不变 |

`forge next` 的 JSON 增加 `"requires_human": true`（新增字段，不改旧字段）用于 change 创建与 clarify 两处。

### task start 守卫

`runTaskStart` 在 spec 仓库（`specroot.Declared(root)`）且 `state.Change == ""` 且 `Kind != TaskKindTask` 时：

```
错误：这个仓库用 spec 流程（spec/forge.yaml）。功能改动请先 forge change new <id>，
然后 task start --change <id>；确实是一句话小修，请显式加 --kind task 重跑。
```

`--kind task` 是新的 kind 值：走 3 道门禁，但豁免 change 要求（`TaskKindTask = "task"`；`IsGeneric` 不受影响）。

## 2. 阶段机（A2）

### 阶段推导（`internal/change/phase.go`）

```go
type Phase string
const (
    PhaseSpecify Phase = "specify"
    PhaseClarify Phase = "clarify"
    PhasePlan    Phase = "plan"
    PhaseTasks   Phase = "tasks"
    PhaseReady   Phase = "ready"
)

// ComputePhase 只读文件；missing 是给人看的下一步说明。
func ComputePhase(root, id string) (Phase, []string, error)
```

推导规则（短路，先满足先返回）：

| 阶段 | 判据 |
| --- | --- |
| specify | `goal.md` 不存在 / `ParseGoal` 失败（缺段、空段、残留 `forge:fill`） |
| clarify | `clarify.md` 不存在或 `ParseClarify` 失败（缺 `## 边界` 正文或缺 `人确认：` 行） |
| plan | `design.md` 不存在或仍是脚手架（含 `forge:fill` 标记） |
| tasks | `tasks.md` 不存在 / 0 条任务 / 任一条任务 `Criteria` 为空 |
| ready | 以上都过 |

`forge change phase <id>` 输出当前阶段、缺失项、下一步命令；`--json` 给结构体。
**不写状态文件**：阶段永远可从文件重建，所以没有「阶段文件过期」这种漂移。

### clarify 记录

`forge change clarify <id> --by-human --note "边界/非目标"`：

- 缺 `--by-human` → 报错「先问用户，得到明确回答再加 --by-human」；
- 渲染 `internal/change/templates/clarify.md`：`## 边界`（note 正文）、`## 非目标`（从 goal 的 Out of Scope 抄一份）、`人确认：<UTC 时间>`；
- 已存在时要求 `--force` 覆盖（避免把历史澄清冲掉）。

### 与 task start 的连接

`task start --change <id>` 前置：`ComputePhase == ready`，否则报错并给出 `forge change phase <id>` 的具体缺失。`--kind task` 不检查 change。

## 3. 任务绑定（A3）

- 绑定语法沿用 `ParseTasks`（`（判据：acceptance #N）`），不新增语法；
- `ComputePhase` 把「任意任务无判据」判为 `tasks` 阶段未完成；
- `forge change status` 在 tasks 未绑定时给出同样的提示（复用 same helper `UnboundTasks`）。

## 4. 测试

| 层 | 用例 |
| --- | --- |
| next 决策表 | `TestNextDecision_EntryRouting`（四象限：声明/未声明 × 有无 change） |
| task start 守卫 | `TestTaskStart_RequiresChangeOrKindTask`、`TestTaskStart_ExplicitKindTaskAllowed`（隔离 HOME，spec 仓库） |
| 阶段推导 | `TestStage_Progression`（specify→clarify→plan→tasks→ready 逐级）、`TestStage_UnboundTaskBlocksReady`、`TestClarifyRecord_RequiresByHuman` |
| e2e | `TestEntryRoutingChangeFirst`（脏树 → next 给 change new → 建 change → next 给 phase → 补齐 → task start 通过）、`TestChangeStageBlocksEarlyTask`（goal 没填完就 task start --change 必须失败） |
| 文档 | `TestAgentsProtocol_MentionsEntryRouting`、`TestHelpSurface_NoHostLayerWording` 保持绿 |

## 5. 兼容性

- 未声明 spec 的仓库：`next` / `task start` 行为与 1.86 完全一致；
- 已声明但不想走 change 的仓库：`--kind task` 显式出口；
- 老的打开 change（五份产物没有 clarify.md）会停在 `clarify` 阶段——这是预期：`forge change clarify --by-human --note …` 一次即可放行，`change verify` 不受影响。

## 6. 风险

| 风险 | 对冲 |
| --- | --- |
| 有人嫌 clarify 多一步 | clarify 只要求一段话 + 人确认；`--kind task` 是造反例的出口 |
| next 给出的 change id 占位符让 agent 困惑 | 文案里给 `forge change new <id>`，并说明「id 用短横线命名」 |
| 阶段推导把老 change 卡死 | 老 change 只在 `task start --change` 时检查；`change verify`/`gate` 不检查 phase |
| 与 goal 哈希检查重复 | 不重复：phase 管「开工前」，goal 哈希管「开工后漂移」 |