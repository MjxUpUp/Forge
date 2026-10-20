---
status: approved
owner: MjxUpUp
ask: Q1–Q3 已于 2026-10-20 按推荐拍板，Q4 留到 B3 开工前再定（见第八节）；本文是 B0 起各批的设计依据
tier: full   # B2 新增命令、B3 新增阻断位点与新 hook；B0、B1 开工时可各自按 mini 落档
baseline: 65a0e810
---

# Loop 契约：把 Forge 接进无人值守的 agent 循环（2026-10 提案）

## TL;DR

**推荐本周期只做 B0 + B1，排在 R2 之后、R3 之前；B2、B3 排到 2026-12-15 复验之后。**
B0 和 B1 都不新增命令和阻断位点，能与当前周期
（acceptance-gate-and-wedge 提案，`docs/plans/acceptance-gate-and-wedge-2026-10-proposal.md`，截至本文尚未入库，相关 non-goals 已摘录在第七节）的 non-goals 共存。
B2 和 B3 各自与该提案的一条 non-goal 冲突（见第七节），排期上要等它复验。

术语（均出自该提案）：**R 线**是"改动接受门"代码线，其中 R2 = 决策必须带可核验锚点（decision-warrant），R3 = Track A 评测的有效性前置，R6 = "不可能"声明（本周期只出设计）；**W 线**是拿外部用户的楔子运营线，"防假绿"是它对外的叙事。

Forge 自称"coding agent 循环的验证与状态层"，但这只在单个任务内部成立。放进 Claude Code `/loop`、定时任务或 Codex automation 这类**无人值守循环**后，有三处会失效：

1. 循环不知道什么时候该停；
2. 交给人的动作要求人当场在终端前；
3. 已经证伪的方案记不住。

| 批次 | 内容 | 命令净增 | 新阻断 | 时间窗 |
|---|---|---|---|---|
| **B0** | 修回环耗尽的两处放行漏洞（`--reset-loop` 无效、首次检出放行一次） | 0 | 0 | 本周期 |
| **B1** | 验收同因复发判定 + 循环判词 `loop_verdict` + `protocol.yml` 的 `loop:` 预算段 + 死路记忆 + loop 操作规程 | 0 | 0（全部 advisory） | 本周期 |
| **B2** | 跨项目待办收件箱 `forge inbox` + hazard 留存确认 | +1 | 0 | 2026-12-15 后 |
| **B3** | 任务自主度分档 `autonomy-guard` | +1 | +1（只对显式声明档位的任务生效） | 2026-12-15 后 |

依据：
- 外部文章两篇：
  - [《Loop engineering：把 agent 放进工程循环》](https://mp.weixin.qq.com/s/RxRzTsRvmZJMmtQjQM79_g)（大淘宝技术，2026-09-21）：一个能工作的循环需要六个动作，其中第 5 步"写入状态"、第 6 步"判断停止条件"，Forge 只覆盖了一半。
  - [《AI 驱动研发体系的实践和思考》](https://mp.weixin.qq.com/s/OSYCiNajJw6c5-hGFSAfYw)（大淘宝技术，2026-09-02）：分工是"人负责决策，Agent 负责执行"，高风险动作不因概括性同意而自动执行。
- 内部证据：下文的代码考古（基线 `65a0e810`），以及三轮独立技术评审（意见已逐条修订进本版）。1.4 节的两处漏放已由 `internal/taskpipeline/loopedge_test.go` 的 `TestLoopExhaustionNoLeak` 在旧实现上复现。

## 一、问题与现状证据

### 1.1 停不下来：停止条件分散，验收失败没有复发判定

| 已有停止机制 | 位置 | 局限 |
|---|---|---|
| review 回边轮次预算 + finding 指纹复发 | `internal/taskpipeline/loopedge.go:75`、`:147` | 只管 review finding |
| token 熔断 | `internal/toolusage/store.go:366`，未导出常量 500000，`TaskTokenBreaker` 不收阈值参数 | 只打 stderr，不写 checklog，不改状态 |
| 停滞检测 | `internal/clitask/task_watchdog.go:28`（`--stall` 45m） | 要人主动运行；会落 `task-stalled` 行，但不进任何判词 |
| safe-halt | `internal/hazard/halt.go:25`（连续 3 次拦截） | 只在 gate 时往 stderr 打印 |

文章里最常用的停止条件是"**连续两次失败原因相同 → 停，交给人**"，Forge 对验收失败没有这条判定：
- `AcceptanceCriterion`（`internal/tasktypes/types.go:74-151`）只保留最近一次的 `Passed` 和 `Output`。`Output` 是**按原始字节截取的最后 500 字节**（`truncateAcceptanceOutput`）。
- 退出码不落盘，只存在于临时的 `judgeContext.exitCode`（`internal/taskpipeline/assertion_judge.go:154`）。
- 跨运行的历史只有 checklog 里每次运行一行 `k/n 通过` 的汇总（`internal/clitask/task_gate.go:451`）。

这些信号也没有汇总成一个**给外层循环读的判词**。

### 1.2 交不出去：人工检查点假设人在终端前

| 检查点 | 怎么区分人和 agent | 没人响应时 |
|---|---|---|
| hazard 拦截 → `forge hazard confirm` | stdin 必须是字符设备（`internal/cli/hazard.go:439`） | 一直挂账并挡住 complete（`internal/taskpipeline/hazardpending.go:37`）；确认标记 5 分钟过期（`internal/hazard/stamp.go:36`） |
| input-required → `forge task answer` | 不区分 | `LastQuestion` 只被 `Answer()` 读进 Decision（`types.go:1442`），**没有任何面向人的展示**；7 天后才在 Pulse 里显示为僵尸（`internal/dashboard/feed.go:324`） |
| 回环耗尽 → `--reset-loop` | 不区分，只要求 `--note` | 一直挡住 complete，而且重置本身失效（见 1.4） |

在无人值守循环里，这些出口要么卡死，要么逼 agent 自设 `FORGE_HAZARD_PENDING=disable` 这类逃生 env。
另外，`internal/evalkit/telemetry.go:199` 的 `wait_turns` 明确标注"无确认等待数据源"：等人的成本目前不可观测。

### 1.3 记不住死路

交接字段（`internal/tasktypes/types.go:580-588`）有 Goal、Plan、Decisions、NextSteps、Blockers、Findings，没有"试过、失败了、附证据"。
`ArchiveAttempt` / `PriorAttemptsSummary`（`internal/taskpipeline/specs.go:182`、`:217`）存的是 review 打回的 findings，不是方案。

### 1.4 回环耗尽的两处放行漏洞（已由 `TestLoopExhaustionNoLeak` 在旧实现上复现）

`CheckLoopExhausted`（`loopedge.go:185`）重算出耗尽后，只把标记写回盘：`MergeOrPersistTaskState` 不回写传入的内存对象。随后它用**旧的内存 state** 做判断，所以：

1. **首次检出**：耗尽首次在 complete 预检里被算出来时，这一次放行。
2. **重置之后**：`ResetLoopExhaustion`（`:231`）只把 `LoopExhausted` 置为 nil，不重置轮龄。
   - 下一次预检时，仍有轮龄 ≥ 预算的 open finding，于是重算命中、写盘，同时又因为第 1 条放行一次。
   - 再下一次预检就会拦住。
   - 注释写的是"清除耗尽态与轮龄"，CLI 打印的是"轮次清零"（`internal/clitask/task_continuity.go:914`），代码里都没有清轮龄这一步。

两条合起来，"耗尽即升级人工"这条硬约束各有一次漏放。人工裁决之后，也拿不到一份真正的新预算。

### 1.5 动作没有授权分级

hazard-guard 只拦破坏性动作（`internal/hooks/embed_guard.go:621-626`）。普通的 `git push`、`git merge`、`gh pr merge`、`npm publish` 都不拦，Forge 也没有按任务授权的概念（grep `autonomy` 零命中）。

## 二、目标与 Non-goals

**目标**：外层循环每一轮都能从 Forge 拿到：
- 一个可枚举的判词：`continue` / `escalate` / `done`，每条原因附带**解除方式**；
- 一份不会随压缩丢失的死路清单；
- 一个人可以稍后异步处理的待办入口（B2）。

需要时，还能把本轮可执行的动作限制在声明的档位内（B3）。

**Non-goals**：
- **不做调度器。** 唤醒、节奏、重试由宿主负责。task_watchdog 注释里的红线"不替代 agent 循环"继续有效。
- **不削弱人机判别。** 异步化只改变"何时确认"，不改变"谁能确认"，不新增 agent 可以自助的确认路径。
- **死路记忆只在单个任务内**，不跨任务继承。与当前周期"不恢复学习闭环"一致。
- 不做合入/发布后回查、控制件效能账、项目知识层（见第六节）。
- 不改 hazard-guard 的 bash 规则表。

## 三、设计

### B0 修复回环耗尽的两处放行

1. `CheckLoopExhausted` 重算命中后，**先**设置内存里的 `state.LoopExhausted = marker`，再写盘；本次直接返回阻断。写盘失败时同样阻断（按失败即拦处理），只是标记要等下一次重算才落盘。修掉 1.4 第 1 条。complete 把 `loadTaskOrActive` 读出的 state 原样传进来（`internal/clitask/task_complete.go:160`），所以在这个对象上置标记就能让本次 complete 报错退出。
2. `TaskState` 新增 `LoopResetRound int`（json `loop_reset_round,omitempty`）。
   - `ResetLoopExhaustion` 写入 `len(ReviewRounds) + 1`，同时写 `LoopResetAt *time.Time`（json `loop_reset_at,omitempty`；用指针是为了让 omitempty 对零值生效）。
   - 两个字段都只在本机有效：同步会合并 ReviewRounds，但不合并这两个字段（与 `LoopExhausted` 一样以本地为准）。并入他机轮次后预算可能提前耗尽，他机的重置也不会传过来，两种偏差都偏严，不会漏放。
   - `CheckLoopExhausted` 落盘时对锁内重载的副本重新判定，避免用调用方的旧快照把一次并发的人工重置覆盖回耗尽。
   - `findingLoopAge` 的起算轮改为 `max(f.Round, LoopResetRound)`，等于人工裁决后发一份完整的新预算。
   - `LoopResetAt` 同时作为 B1 各计数器的窗口起点（见 B1-2 解除表）。
3. `ResolvedPrints` 不动：已修问题复活，仍按复发处理。
4. 注释和 CLI 文案改成与行为一致。新字段在 `internal/taskpipeline/seed_schema.go` 里填非零值。
5. `--reset-loop` 到 B1 还要负责清空 FailHistory、重开各预算窗口，但它挂在 `task finding` 下面。B0 先把它的 help 文案改成"人工裁决：重开回环预算"，B1 再补上新增的职责；行为变化在 CHANGELOG 的行为变更节声明。命令和 flag 本身不变，compat 不记 changed。

REQ-0（前提 `max_rounds ≥ 2`；`max_rounds = 1` 是合法配置，见 `internal/artifactchain/artifactchain.go:267`，此时 reset 后轮龄立刻 = 1 ≥ 预算，按设计应当立即再耗尽，单独一条用例钉住）：
- 构造一个已有超龄 open finding 的任务，**连续两次**重载 state 并执行预检，**两次都拦**（修掉首次放行）。
- 执行 `--reset-loop` 后，连续两次重载预检**都放行**。
- 再经过 `max_rounds` 轮 review，预检重新拦截。

### B1-1 验收同因复发判定

**指纹在完整输出上算，不在截断尾部上算。**

在 `runAndJudgeCriterion`（`internal/taskpipeline/assertion_judge.go:146`）还拿着完整输出和退出码时，按顺序处理：

1. `NormalizeFailureOutput(full)`，新纯函数，放在 `internal/taskpipeline/failnorm.go`。去掉以下噪音：
   - 耗时 `\d+(\.\d+)?(ms|s)`；
   - RFC3339 和 `HH:MM:SS` 时间戳；
   - `0x[0-9a-f]+` 地址；
   - goroutine 编号；
   - `$TMPDIR` 和随机临时目录段。
2. 按行取最后 20 个非空行。
3. 用 `LoopFingerprint`（`loopedge.go:35`，已导出，直接复用）算指纹。

指纹和退出码通过判定结果结构透传出来，不再丢在临时变量里。现有 500 字节的 `Output` 语义不变。

**数据**：`AcceptanceCriterion` 新增 `FailHistory []AcceptanceFailure`（json `fail_history,omitempty`）。

```go
type AcceptanceFailure struct {
    At          time.Time `json:"at"`
    ExitCode    int       `json:"exit_code"`
    Fingerprint string    `json:"fingerprint"`
    ChangeHash  string    `json:"change_hash"` // 跑命令【之前】算的 TaskFingerprint
    Changed     map[string]string `json:"changed,omitempty"` // 当时的 TaskChangedFiles：repo 相对路径 → 内容 sha256 前 12 位（最多 50 条）
}
```

写入规则：
- 在 `MergeAcceptanceResults`（`internal/taskpipeline/acceptance.go:333`）里，基于盘上的权威 state 追加，避免合并时丢失。
- 只保留最近 5 条。
- **一旦通过就清空**。
- `--reset-loop` 也清空：人工裁决后重新起算。
- 不经过 heldout 共享路径。

**判定**：最近 N 条失败（N = `loop.recurrence_threshold`，默认 2）同时满足两个条件才算复发：
- 指纹全部相同；
- **相邻两条的 `ChangeHash` 都不同**，即每次之间都改过代码，而失败没变。

**`ChangeHash` 的挂点**：在 `VerifyAcceptanceWithVerdicts` 的循环开始之前（与 `changed` 同一处）用 `TaskFingerprint` 算一次，整次验收共用，经不落盘的临时字段（`json:"-"`）透传给各条验收。不能挂在 `runAndJudgeCriterion` 里：它的签名是 `(root, c, changed)`，拿不到 state，而且和 heldout 共用。

**两个 hash 并存，各管各的**：

| 字段 | 计算时机 | 用途 | 本提案是否改动 |
|---|---|---|---|
| `AcceptanceFailure.ChangeHash` | 跑命令**前** | 判断两次失败之间有没有改过代码 | 新增 |
| `AcceptedChangeHash` | 跑命令**后**（现状） | complete 时 `CheckAcceptanceFresh` 重算比对新鲜度 | **不动**。改成跑前的话，会改源码的验收命令会被误判为"验收后又改了代码"而硬阻断 |

跑前计算能减少、但不能消除验收命令自身改源码带来的干扰，见下面盲区表的最后一行。

**已知盲区**（写进 checklog 的 meta，并在第五节度量）：

| 情况 | 原因 | 处理 |
|---|---|---|
| 只改了非源码文件（yaml、json、Makefile 等） | `TaskFingerprint` 只统计源码扩展名 | 漏判；记 meta `change-scope=src-only` |
| 某一侧 `ChangeHash` 为空（非 git 仓库、无 HEAD、基线不可达） | 无法比较 | 不判；记 meta `recurrence=unmeasurable` |
| 其他任务 complete 后排除集变化，导致 hash 变了 | 外部变化被当成本任务改动 | 可能多计一次尝试；B1 只出 advisory，这类误报由第五节的精确度度量兜底 |
| 验收命令第一次运行时改了源码（`go generate` 输出 `.go` 且不命中 genMarks 排除，`jest -u`、golden `-update`） | 第二次跑前算出的 hash 已经变了，没改代码的第一次重跑也被算作一次尝试 | 命令输出稳定时**最多多计一次**；记 meta `self-mutating=suspect`，条件是：两次之间有内容 hash 变化的文件，**全部**出现在上一次验收命令执行后、下一次 agent 编辑前就已经变化的集合里。实现方式是在上一次验收命令执行后再快照一次 `Changed`，与执行前对比；同一文件被 agent 反复修改不会命中这个条件 |

**效果（advisory，退出码契约不变）**：
- 打印 `ADVISORY: 验收 #N 改动后同因失败 ×N——本轮应停止并升级人工`。
- 落新 checklog 行 `acceptance-recurrence`：
  - 加进 `internal/checklog/types.go` 常量和 `escape.go:79` 名单，重钉 compat checks 面快照；
  - **不进 evidence 白名单**，先例是 decision-warrant。
- 自动写一条死路记录（B1-4）。
- 在判词里贡献一条 `escalate`。

REQ-1：同一验收在两个不同 `ChangeHash` 下、带不同耗时噪音、输出超过 500 字节时，仍判为同因复发；`ChangeHash` 相同的重跑不计数；中间夹一次通过后重新计数。

### B1-2 循环判词 `loop_verdict`

新增纯函数 `EvaluateLoopVerdict(in LoopInputs) LoopVerdict`，放在 `internal/taskpipeline/loopverdict.go`，**不写盘、不落审计**。它只调用纯计算函数（例如 `evaluateLoopExhaustion`），不调用 `CheckLoopExhausted`。

```go
type LoopVerdict struct {
    Verdict string       `json:"verdict"` // escalate | done | continue（优先级从高到低）
    Reasons []LoopReason `json:"reasons"`
    Next    string       `json:"next"`    // NextDecision 给出的下一条确切命令
}
type LoopReason struct {
    Code    string `json:"code"`    // 封闭枚举，见下表
    Detail  string `json:"detail"`
    Resolve string `json:"resolve"` // 解除命令；人执行的会标注「需人工」
}
```

**判词定义**（`done` 不能要求三道门禁全过，否则在循环里永远等不到）：

| 判词 | 条件 |
|---|---|
| `escalate` | 至少有一条下表里的原因 |
| `done` | 没有 escalate 原因 + **至少登记了一条验收** + 每条验收的 `Passed` 都为 true + 验收新鲜。含义是"验收面已收敛，进入收尾序列"，不要求门禁已过。零条验收永远不判 `done`，判词为 `continue`，`Next` 走 `NextDecision` 的"未登记验收"分支（提示 `forge task accept`）；否则循环第一轮什么都没做就会判 `done` |
| `continue` | 其余情况 |

`LoopVerdict.Next` 直接取 `NextDecision`（`internal/taskpipeline/next.go:36`，纯函数，与 gate/status 的 next 行同源），给出收尾序列里下一条确切命令。它的另外三个输入 branch、dirty、behindRemote 由调用方用 `GitBranchDirty(root)`、`GitBehindRemote(root)` 算好放进 `LoopInputs`（只跑本地 `rev-list`，不访问网络）。**必须调 `NextDecision`，不能调 `NextHint`**：后者会落 `next-hint` checklog 行，判词就不再无副作用。真实顺序是：implement →（验收未跑则 verify-acceptance）→ `forge task gate task-verify` → `forge review pass` → `forge task gate task-complete` → `forge task complete`。所以 `done` 在循环里一定能达到：它只依赖验收结果，门禁留给收尾序列逐步推进。

新鲜度检查要求无写入、无审计。`CheckAcceptanceFresh`（`internal/taskpipeline/acceptance.go:435`）的逃生分支会落审计行（`:438-444`），所以 B1 把它的判定部分抽成 `acceptanceFreshCore`，原函数改为调用它，判词也调用它。`acceptanceFreshCore` 会调 `GetHeadCommit` 和 `TaskFingerprint` 读 git，所以只能叫"无写入、无审计"，不是严格的纯函数。

另外两点：
- 非 git 目录下 `head == ""` 时，它直接返回 ok、不看 `Passed`。所以判词**单独逐条检查 `Passed`**，不依赖它兜底。
- 不新鲜（验收通过后又改过源码）的验收不算通过，判词回到 `continue`。

**原因、解除方式与清除条件**（没有解除方式，循环会在 escalate 上空转）：

| Code | 来源 | Resolve | 清除条件 |
|---|---|---|---|
| `acceptance-recurrence` | B1-1 | 需人工：`forge task finding --reset-loop --note "<新方向>"` | 该验收通过，或者 reset-loop 清空 FailHistory |
| `loop-exhausted` | `evaluateLoopExhaustion` 或持久标记 | 需人工：同上 | reset-loop（B0 后真正生效） |
| `hazard-pending` | `hazard.PendingBlocksFor` 的 Own > 0 | 需人工：`forge hazard confirm --last`（B1 只有计数；B2 引入 `PendingByFingerprint` 后改为逐指纹 `--fingerprint <fp>`） | 确认 |
| `input-required` | `Assignment.Status` | 需人工：`forge task answer --ref <ref> --content` | answer |
| `budget-failed-verifies` | `LoopResetAt` 之后失败的 verify-acceptance 次数 ≥ 预算 | 需人工：reset-loop，或调高预算 | reset-loop 重开窗口 |
| `budget-tokens` | `LoopResetAt` 之后的 `SumEstTokens` ≥ 预算 | 需人工：同上 | 同上 |
| `budget-changed-files` | 改动文件数 ≥ 预算（口径同 `TaskChangedFiles`） | 需人工：拆分任务，或调高预算 | 文件数回落，或预算调高 |
| `no-progress` | 连续 K 次 verify-acceptance（K = `max_verifies_without_progress`）通过条数都没有增加 | 需人工：reset-loop | reset-loop 重开窗口 |
| `gate-repeat-blocked` | 从 `LoopResetAt` 起，同一个门禁在 `History` 里连续两条 `Passed=false`（`forge task gate` 失败时由 `RecordGateResult` 写入，见 `internal/clitask/task_gate.go:121`、`types.go:1028`；先例是 `ReworkRounds()`，`types.go:1015`） | 需人工：reset-loop | 该门禁通过，或 reset-loop 重开窗口 |
| `autonomy-denied` | B3 | 需人工：B3 的提档命令 | 提档，或放弃该动作 |

`gate-repeat-blocked` 只统计 `forge task gate` 命令。`forge task complete` 的预检拒绝和 `forge review pass` 的拒绝不进 `History`，B1 不计入；如果要计入，需要先给这两处补 checklog 行，列为 Deferred。

`no-progress` 用"有验证但没进展"衡量，**不用空闲时长**。定时循环每次唤醒之间本来就空闲，按空闲时长判断会误报。

人工裁决只有一个出口 `--reset-loop`，所有窗口计数器共用 `LoopResetAt`。

**暴露面（不新增命令）**：

1. `forge task status --json`：改用嵌入结构体 `struct{ *TaskState; LoopVerdict *LoopVerdict \`json:"loop_verdict"\` }` 输出。原有键顺序不变，新键追加在末尾。
   - 现有消费方：eval 演练里的 `expect_contains`、`skillintegrate/notes/code-review-gate.md` 的 grep、两处报错文案。都不解析 JSON 结构。
   - 无活跃任务的分支 `{"task_ref": null, ...}` 保持不变，不输出判词。
   - 代价：status 每次要跑一次 git 并扫 checklog/toollog，会变慢。验收要求 status 耗时增量 < 300ms。
2. `forge task verify-acceptance` 末行打印 `loop: <verdict>（<codes>）`。
3. resume 的 tl;dr：`renderTldr`（`internal/clitask/task_continuity.go:1288`）改为接收预先算好的判词，加一行 `循环判词`。

REQ-2：表驱动测试覆盖：
- 上表每个 Code 的判词、Resolve 和清除条件（含 `gate-repeat-blocked`），以及 escalate > done > continue 的优先级；
- 门禁一道未过、验收全过且新鲜时判词为 `done`，`Next` 与 `NextDecision` 一致；
- 验收通过后再改源码，判词回到 `continue`；
- 零条验收时判词为 `continue`；
- 非 git 目录下有一条 `Passed=false` 时，判词不是 `done`；
- `task status --json` 原有键集合与顺序不变。

### B1-3 `protocol.yml` 的 `loop:` 预算段

```yaml
loop:
  recurrence_threshold: 2            # 0 = 关闭复发判定
  max_failed_verifies: 0             # 0 = 不限
  max_changed_files: 0               # 0 = 不限
  token_budget: 0                    # 0 = 沿用 500000
  max_verifies_without_progress: 0   # 0 = 不判；循环场景建议 3
```

- 按 `design_tiers` 的现有做法落地（`internal/protocol/types.go`、`internal/taskpipeline/designtier.go:93`）：`Loop *LoopConfig`，`omitempty`；nil 表示内置默认；读取失败不阻断；`DefaultProtocol()` 不写这一段。
- `token_budget` 需要给 `TaskTokenBreaker` 加阈值参数，原调用方传入默认值，行为不变。
- 不配置的项目只多出一条复发 advisory。预算只影响判词，B1 不阻断任何门禁。

REQ-3：配置 `max_changed_files: 3`、改动 4 个文件时，判词为 `escalate`，含 `budget-changed-files`；不配置时，不出现任何 `budget-*` Code。

### B1-4 死路记忆

**载体**：复用 `Decision`，新增 `Kind string`（json `kind,omitempty`）。
- 空值表示普通决策；
- `dead-end` 表示已证伪方案：`Content` 写方案，`Rationale` 写失败证据。

| 方案 | 代价 | 结论 |
|---|---|---|
| 新字段 `DeadEnds` + 新命令 `task deadend` | 命令 +1，违反当前周期"命令净增 0"；同步链要改 5 处（`merge.go:70`/`:80`、`merge_converge.go:201`/`:349`、`task_port.go` 的 redactTask、`seed_schema.go`） | 否 |
| 复用 `Finding` + `Tag=dead-end` | Tag 是对齐 doc-review 的封闭枚举（`types.go:289`）；open finding 会计入回环轮龄（`loopedge.go:75`），还会被列进证据束的剩余风险（`scoring.go:315`） | 否 |
| **复用 `Decision` + `Kind`** | 同步路径按 ID 去重（`UnionDecisions`、`decisionKey`、`resolveByID`），Kind 随条目走；只多一个嵌套键 | **采纳** |

**写入**：
- `forge task decide --rejected --content "<方案>" --rationale "<失败证据>"`。新 flag 属于纯新增，援引 compat-commitments 2026-09-20 裁决：CHANGELOG 预先声明，并重钉快照。
- **`task decide` 的内容级幂等键加上 Kind**（`task_continuity.go:796`，现在是 Content+By）。否则先否决再采纳同一方案会被当作重复跳过，语义反转。
- B1-1 触发时自动写一条，`By=forge`。内容包含指纹、两次的 ChangeHash，以及两次失败记录之间**内容 hash 变化过的文件**（新增、删除，或同一路径 hash 不同），让下一轮看得出"动了哪些文件仍然失败"。按内容 hash 比较，agent 反复改同一个文件也能显示出来；只比路径列表的话，这种最常见的情况会显示成"没动任何文件"。不用 `git diff --stat`：两次失败时工作区多半未提交，ChangeHash 只是哈希，没有快照可以拿来 diff。

**渲染**：
- 单独成节【已证伪方案（不要重试）】，放在【已确认决策】之前，最多 8 条，每条 200 rune。
- 【已确认决策】节排除 dead-end 条目。
- 全局注入上限 9500 rune 从尾部截断（`internal/hookdispatch/hook_emitters.go:48`），前面各节不封顶时这一节仍可能被截掉。所以 **tl;dr 直接列最近 3 条已证伪方案**，tl;dr 本身是封顶的。
- reinject 的空交接提示（`task_continuity.go:731`）把 `decide --rejected` 列进去。

REQ-4：
- dead-end 条目渲染在独立节，并出现在 tl;dr，不出现在【已确认决策】节；
- 先 `--rejected` 后普通 `decide` 同一内容，会产生两条记录；
- 经过 sync merge 和 import 往返后 Kind 保持不变；
- redact 会替换 Content/Rationale（`task_port.go:390`），但保留 Kind。

### B1-5 loop 操作规程（feedforward）

forge-quality skill 由 `internal/skillgen/qualitygen.go` 的 `buildQualitySkillContent` 生成，没有独立的 skills 目录。所以改动方式是在生成器里加一节"Loop 模式"，并补 golden 测试。规程要点：

1. 每轮先运行 `forge task resume`，读【已证伪方案】和 `循环判词`。
2. `escalate` → 用 `forge task block` 写清卡在哪一条 Code，结束本轮。**不设任何逃生 env，不自己执行标注为"需人工"的 Resolve。**
3. 只做一个最小改动。换方案前，先用 `decide --rejected` 记下被放弃的方案和证据。
4. 运行 `forge task verify-acceptance`，读末行 `loop:`。`continue` → 回到第 3 步。
5. `done` → 进入收尾序列，每一步都执行判词 `Next` 给出的那条命令，执行后重新读 `Next`。正常顺序是：`git commit` → `forge task gate task-implement` → `forge task gate task-verify` → `forge review pass` → `forge task gate task-complete` → `forge task complete`。
6. 收尾中任何一条命令输出 `BLOCKED:`（非零退出）→ 按拒绝原因修复后回到第 4 步。同一门禁连续两次 BLOCKED 时，判词会由 Forge 自己数出来并给出 `gate-repeat-blocked`，不靠 agent 自觉，按第 2 步处理即可。

另附一份 `/loop` prompt 模板，按文章里的 CI triage 示例改写：只处理可稳定复现的失败；每轮最多改 3 个文件；同因失败两次就停。

**与 R6 的边界**："不可能"声明是 **agent 主动声明**任务本身有问题；`escalate` 是 **Forge 机械检出**循环应当停止。R6 的设计稿应把它的声明接入同一个判词通道，新增一个 Code，而不是另开一个出口。

### B2-1 `forge inbox`（命令 +1；2026-12-15 之后）

- **数据源**：不新增存储。遍历注册表 `ListManaged()`，汇总四类待办：hazard 待确认、`input-required`（第一次给 `LastQuestion` 面向人的展示）、回环耗尽、判词为 `escalate` 的活跃任务。
- 每项显示：项目路径、任务、Code、起始时间、详情、Resolve。Resolve 直接取判词里的 `LoopReason.Resolve`，单一真相源。forge 没有全局 `-C`，所以输出里带 `cd <项目路径> &&` 前缀。
- **修正 hazard 少报**：`PendingBlocksFor` 遇到任何一次确认就整账清零（`internal/hazard/halt.go:80`）。收件箱改用新纯函数 `PendingByFingerprint`，按指纹列出未确认项。complete 门禁仍沿用原语义，不改动。
- **展示面**：Pulse 增加"待人处理"面板，调用同一个函数；SessionStart 横幅在待办数 > 0 时加一行。
- **新指标** `escalation_wait`：从入箱到处理的时长。不复用 `wait_turns`，因为单位不同。`Answer()` 补记 `AnsweredAt`。

REQ-5：两个注册项目里分别有 hazard 待确认、input-required 任务时，`forge inbox --json` 列出这两项，各带非空且带项目路径前缀的 `resolve`；确认 A 指纹后，B 指纹仍然列出。

### B2-2 hazard 留存确认（2026-12-15 之后）

`forge hazard confirm --fingerprint <fp> --hold <dur>`，新 flag 属于纯新增，上限 72h。不带 `--hold` 时行为完全不变。

设计约束（防伪造、消费时机、执行中时间窗）：

1. **防伪造不弱化**。agent 对 DataDir 有写权限，现有 5 分钟上界是防止手写标记的唯一防线（`internal/hazard/stamp.go:185-217`）。留存标记写到独立文件 `<fp>.hold.json`，与普通确认标记分开、不互相覆盖。校验时必须在 `events.jsonl` 里找到匹配的 `EventConfirm`，且带 `hold` 字段、时间和指纹一致，才承认这个标记。只有标记文件、没有对应事件的，一律不认。
2. **在命令真正执行之后才消费，查询时不消费**。PreToolUse 只读查询，不消费。命令执行后落一条 `hold-consumed` 事件，标记从此失效。这样即使放行之后被别的 hook 或权限弹窗拒掉，标记也不会被白白用掉；B3 的 autonomy-guard 查同一个指纹时，读写的是同一份标记；两者在 inflight 上如何互不阻挡，见下面"执行中时间窗"一条。
   - **消费点必须同时挂 PostToolUse 和 PostToolUseFailure**。在 Claude Code、cursor、copilot 上，Bash 失败走 PostToolUseFailure，不触发 PostToolUse（`internal/hooks/settings.go:176-186`）。像 `rm -rf x && false` 这种危险部分已经执行、退出码却非零的命令，如果只挂 PostToolUse 就不会被消费，72h 内可以反复重放。
   - 指纹从 payload 的原始命令重算，与 hazard-guard 的 `HEREDOC_ORIG` 口径一致（`internal/hooks/embed_guard.go:1219`）。
   - **宿主覆盖矩阵**：claude-code、cursor、copilot 接两个事件；codex 把失败折进 PostToolUse，只接一个；kimi、windsurf、cline 是事件白名单，B2 开工时逐个确认。**接不上消费点的宿主上，`--hold` 直接拒绝**，回退到 5 分钟窗口。
   - **执行中时间窗**：从 PreToolUse 放行到消费之间，并行发出的同指纹命令，或命令还在跑时的重试，都会再次放行。处理方式是加一个 `<fp>.inflight` 标记，规则如下：
     - **只对 `.hold` 留存标记生效**。普通 5 分钟确认不写 inflight，"窗口内重试放行"的现有语义不变。
     - inflight 记录本次工具调用的 `tool_use_id`。同一个 `tool_use_id` 下的多个 hook 都放行（hazard-guard 和 B3 的 autonomy-guard 并行查同一指纹，互不阻挡）；只有**不同** `tool_use_id` 的同指纹调用才拦截。B2 开工时逐个宿主确认 hook 输入里有这个字段，没有的宿主按"接不上消费点"处理，即拒绝 `--hold`。
     - 写 inflight 的是 `forge hazard confirmed --hold-claim <tool_use_id>`。这是一条显式的独立写操作，原来的 `forge hazard confirmed` 查询仍然只读。
     - 清除时机：消费事件（PostToolUse 或 PostToolUseFailure）到来时清除。权限弹窗拒绝时不会有 Post 事件，所以 inflight 的上限设为该宿主的工具超时时间，而不是固定 10 分钟。这样长命令不会中途过期，被拒的调用最多占用到一个超时周期。
3. `hold-consumed` 事件**不让** `PendingBlocksFor` 归零。
4. 旧版二进制读到留存标记，会因超出上界而拒绝，属于安全方向（fail-closed）。

**残余风险**：下一轮重试的命令只要写法有变化，指纹就对不上，留存确认就无效。规程里要求 agent 原样重放被拦截的命令；收件箱展示原命令文本。

REQ-6：`--hold 24h` 确认后，同指纹命令执行一次后再次被拦，**执行成功和执行失败（PostToolUseFailure）两条路径都要覆盖**；只有手写 `.hold.json`、没有对应事件时不放行；inflight 期间，同 `tool_use_id` 的第二个 hook 放行、不同 `tool_use_id` 的同指纹调用被拦；普通 5 分钟确认不写 inflight；不带 `--hold` 的现有测试不改。

### B3 自主度分档（新阻断位点，full 档；2026-12-15 之后）

**威胁模型**：这是**防止误越权的护栏，不是沙箱**。`make release`、`bash -c`、先写脚本再执行，都能绕过。目标是挡住 agent 在正常工作流里顺手做出越权动作，不防恶意规避。

**声明**：
- `forge task start --autonomy observe|propose|act|autonomous`，写入 `TaskState.Autonomy`。
- 空值等于 `autonomous`，也就是现有行为，零迁移。
- **改档用新命令 `forge task autonomy --set <tier>`**（B3 计入下个 minor 的预算 1/2）。提档要求 TTY（复用 `stdinIsHumanTerminalForHazard`），降档任何人都可以。不借用 `task override`，因为那里全是逃生舱，且都没有 TTY 判别，混进来会让语义不清。

**执行**：新增 Go 进程内 hook `autonomy-guard`，挂在 PreToolUse 的 Bash 和 Write|Edit 上，模板是 `internal/hookdispatch/hook_task_drift.go`。需要两个新的解析能力：

1. **push 解析器 `classifyGitPush`**。`gitBoundaryVerb`（`hook_task_drift.go:37`）只识别 commit/merge/branch/checkout/switch，不认 push，所以要新写：
   - 不带参数的 `git push`：用 `git rev-parse --symbolic-full-name @{push}` 解析目标（不加该参数时返回的是 SHA）。
   - `git push origin`（写了远端、没写 refspec）同样走 `@{push}`；但 `@{push}` 用的是分支的 pushRemote，如果它解析出的远端和命令里写的远端不一致，归为严格类别。
   - `push.default=matching` 会推所有同名分支，而 `@{push}` 只反映当前分支，所以 matching 一律归为 push-base。
   - 处理 `HEAD:main` 这类 refspec；`--all`、`--mirror`、`--tags` 一律归为 push-base。
   - `origin v1.2` 用 `git show-ref` 区分分支和 tag。
   - **cwd 形态**：`git -C <dir> push` 在 `<dir>` 下解析；`cd x && git push` 这类改变 cwd 的复合命令无法可靠判断执行目录，归为严格类别。
   - **默认分支判定**：读 `refs/remotes/<remote>/HEAD`。不是 clone 出来的仓库常常没有这个引用，此时依次回退到 `init.defaultBranch` 配置和 `main`/`master` 两个惯例名，三者都判断不出来时，才把所有推送按 push-base 处理。这种情况下 propose 档推功能分支会被误拒，拒绝文案提示执行 `git remote set-head <remote> --auto`。
   - **无法判定时，按更严格的类别处理**，只对显式声明了档位的任务生效。
2. **写入判定**：Go 侧已有 `bashWritesFiles`（`internal/taskpipeline/independence.go:51`），但它服务于 reviewer 独立性判定，是按"误报代价低"调的：只要出现 `<<` 就算写入，引号里的 `>` 也算（`awk '$1 > 3'`、`git commit -F - <<EOF`）。拿它做首发阻断，会挡住只读命令。所以 B3 新写 `classifyBashWrite`：先剥离引号和 heredoc 正文（思路同 hazard-guard 的 `strip_quotes`），再判断写入形态。`bashWritesFiles` 原样保留，不改它的语义。**observe 档的 bash 写入判定首发只出 advisory**，Write/Edit 工具调用照常拦截；误报率 ≤5% 后再升格。

能力表放在 Go 里，作为单一真相源：

| 能力 | 识别方式 | observe | propose | act | autonomous |
|---|---|---|---|---|---|
| write | Write/Edit 工具调用拦截；bash 写入（`classifyBashWrite`）首发只出 advisory | 拒 | 放 | 放 | 放 |
| push-feature | push 到非默认分支；`gh pr create` | 拒 | 放 | 放 | 放 |
| push-base | push 到默认分支；无法判定的 push | 拒 | 拒 | 需确认 | 放 |
| merge-base | `git merge` 进默认分支；`gh pr merge`；`forge task worktree finish --merge-to`（Go 侧在 `task_worktree.go:243` 直接检查） | 拒 | 拒 | 需确认 | 放 |
| publish | npm/cargo publish、goreleaser、docker push、gh release create、推送 tag | 拒 | 拒 | 需确认 | 放 |

- **与 hazard 的关系**：autonomy 的拒绝**不写** hazard block 事件，不计入 safe-halt，也不计入 hazard-pending。它落自己的 checklog 行 `autonomy-guard`，在判词里体现为 `autonomy-denied`。
- "需确认"只复用 hazard 的**确认凭证**：同一套指纹、TTY 确认、B2-2 的留存确认。
- **逃生**：`FORGE_AUTONOMY_GUARD=disable` 登记进 `compat.EscapeEnvs`。使用时落 escape-hatch 审计，Strength 降为 Weak，**同时进收件箱**。agent 自己就能设这个 env，所以必须让人看得见。
- **compat**：分两部分处理，都只对显式声明了档位的任务生效。
  - Write/Edit 工具调用拦截，以及 push-base、merge-base、publish 的拒绝：**首发即 blocked**。拒绝文案指向本文档，符合 compat-commitments 第二节②。
  - bash 写入判定（`classifyBashWrite`）：**以 advisory 首发**。升格为阻断时按第二节①执行，并满足第 2 条"至少 2 个 minor 预告期"；升格的前提是误报率 ≤5%。

REQ-7：
- `propose` 档的任务：`gh pr merge 1` 被拒并落 checklog；`git push origin feat/x` 放行；不带参数的 `git push`，当上游是 main 时被拒。
- 未声明档位的任务：以上全部放行。
- 测试夹具显式设置 `refs/remotes/origin/HEAD`；另有一条用例，在没有这个引用、也没有 `init.defaultBranch` 的情况下，钉住按惯例名回退的行为。

## 四、触点汇总

| 批次 | 文件 |
|---|---|
| B0 | `internal/taskpipeline/loopedge.go`（CheckLoopExhausted 内存同步、ResetLoopExhaustion、findingLoopAge）、`internal/tasktypes/types.go`、`internal/clitask/task_continuity.go:914`、`seed_schema.go` |
| B1 | `assertion_judge.go`（透传指纹和退出码）、`acceptance.go`（MergeAcceptanceResults）、新 `failnorm.go`、新 `loopverdict.go`、`internal/protocol/types.go`、`internal/toolusage/store.go`（breaker 加参数）、`internal/clitask/task_misc.go`（status 嵌入结构体）、`task_gate.go`、`task_continuity.go`（渲染、decide 幂等键、tl;dr）、`internal/tasktypes/types.go`（AcceptanceFailure、Decision.Kind；LoopResetAt 已在 B0 落地）、`internal/checklog/types.go` + `escape.go`、`internal/skillgen/qualitygen.go`、`acceptance.go:435`（抽出纯函数 `acceptanceFreshCore`） |
| B2 | 新 `internal/cli/inbox.go`、`internal/hazard/halt.go`（PendingByFingerprint）、`stamp.go`、`event.go`（hold、hold-consumed）、`internal/cli/hazard.go`、PostToolUse 与 PostToolUseFailure 消费点（`internal/hooks/settings.go` 的 `ForgeHookSpec`）、`internal/dashboard/pulse.go`、`internal/evalkit/telemetry.go` |
| B3 | 新 `internal/hookdispatch/hook_autonomy.go`（含 `classifyGitPush`、`classifyBashWrite`）、`hook.go:292`/`:640`、`internal/hooks/settings.go`、`profile.go`、`internal/clitask/task.go` 及新的 autonomy 子命令、`task_worktree.go:243`、`internal/compat/compat.go` |

checklog 检查名名单：`acceptance-recurrence`（B1）和 `autonomy-guard`（B3）都定义在 `checklog/types.go`，进入名单并重钉快照。
说明：`acceptance`、`acceptance-assert` 等约 30 个检查名定义在 taskpipeline、不在名单里。这是设计如此（名单只对照 `types.go` 的源码扫描），本提案不改。

## 五、成功度量（事前声明）

| 度量 | 表面 | 基线 | 目标 | 复验时间 |
|---|---|---|---|---|
| 耗尽被放行的次数 | checklog 的 `loop-exhausted` 行 × complete 成功事件 | 首次检出放行一次，reset 后再放行一次（探针实测） | 0 | B0 合入时 |
| `acceptance-recurrence` 的精确度：人工抽检确实该停的比例 | checklog + 人工抽检 | 无 | ≥80%，至少抽检 10 条 | B1 合入 +30 天 |
| 复发判定因 `unmeasurable` / `src-only` 跳过的比例 | checklog meta | 无 | 只观测；>30% 时重新评估 ChangeHash 的口径 | 同上 |
| 判词为 escalate 后，agent 仍然设逃生 env 的次数 | checklog 的 escape-hatch 行 × 判词 | 不可测 | 0 | 同上 |
| 待办从入箱到处理的时长 p50 | `escalation_wait` | 无数据源 | 只观测 | B2 合入 +30 天 |

B1 的预算从 advisory 升为门禁，条件是精确度 ≥80% 且误报 ≤10%。升格时按 compat-commitments 第二节执行：至少提前 2 个 minor 预告，并配逃生舱。

## 六、Deferred

| 事项 | 来源 | 触发条件 | 复验 |
|---|---|---|---|
| 合入/发布后回查；"取不到稳定证据就算人工验证"的显式分类 | 文章二 | R6 的设计稿获批 | 2026-12-15 |
| 控制件效能账（每个 hook/门禁的命中、误报、拦住了什么） | 文章一的 harness 维护循环 | R2 的 decision-warrant 攒够 30 天数据 | 2026-12-15 |
| answer、approve、reset-loop 补 TTY 判别 | 1.2 | B2 合入后，收件箱里出现被 agent 自己回答的项 | B2 +30 天 |
| `gate-repeat-blocked` 计入 complete 预检拒绝和 review pass 拒绝（需先给这两处补 checklog 行） | B1-2 | B1 合入后，出现循环卡在这两处却没有 escalate 的实例 | B1 +30 天 |
| 项目知识层 | 文章二 | 不承诺 | — |

## 七、与当前周期的关系和备选对比

**与 acceptance-gate-and-wedge 提案 non-goals 的对照**：

| 该提案的 non-goal | B0+B1 | B2 | B3 |
|---|---|---|---|
| 命令面净增 0 | 满足（只加 flag） | 冲突（+1） | 冲突（+1） |
| 不新增阻断位点 | 满足 | 满足 | 冲突（+1） |
| 延后 always-on 治理包和分派前台 | 满足 | 冲突（收件箱、Pulse 面板、横幅） | — |

所以 B2、B3 排在 2026-12-15 复验之后，届时再按当时的 non-goals 重排。

**备选对比**：

| 方案 | 收益 | 代价/风险 | 结论 |
|---|---|---|---|
| 什么都不做 | 零成本 | 放进 `/loop` 后要么卡死在 TTY 确认上，要么被逃生 env 绕过；"loop 验证层"的定位站不住 | 否 |
| Forge 自己做调度器（`forge loop run`） | 端到端可控 | 越过"不替代 agent 循环"的红线，与宿主能力重复 | 否 |
| 只做判词 | 最小 | 停下之后人看不到，下一轮也记不住，循环原地打转 | 否 |
| **B0–B3 分批，本周期只做 B0+B1** | 每批独立交付、可度量，不违反当前约束 | 占用 R3 之前的时间；B3 需要 full 档评审 | **采纳** |

## 八、风险与待拍板

- **风险：指纹规范化不足或过度。** 对策：`NormalizeFailureOutput` 用表驱动测试覆盖 Go、pytest、jest 三类输出，样本超过 500 字节且带耗时噪音；B1 只做 advisory，用精确度决定是否升格。
- **风险：status 变慢。** 对策：REQ-2 写明耗时增量上限；判词计算不写盘。
- **风险：B3 被绕过。** 威胁模型已写明这是护栏，不是沙箱；逃生必然进收件箱。
**2026-10-20 拍板：Q1–Q3 按下文推荐执行。** B0+B1 插在 R2 之后、R3 之前；死路记忆用 `Decision.Kind`；收件箱做新命令 `forge inbox`。Q4 文中没有推荐项，留到 B3 开工前再定，不影响 B0–B2。

- **Q1（阻断项）**：B0+B1 是否插到 R2 之后、R3 之前？推荐插入：两批都满足本周期约束，而且 B0 修的是"防假绿"楔子自身的漏放。
- **Q2**：死路记忆用 `Decision.Kind`（推荐）还是独立的 `DeadEnds` 字段？
- **Q3**：B2 的收件箱做成新命令（推荐），还是给 `task mine --all-projects` 加 flag？后者的语义是"按 agent 分派"，不是"待人处理"。
- **Q4**：B3 的 `act` 档，是否允许用 B2-2 的留存确认做批量预授权，还是必须逐次确认？

**单文件多批次提案的需求追溯约定（2026-10-20，B1 开工时定）**：需求覆盖表从任务登记的**全部**产物（proposal/spec/design/plan）里收集 REQ 声明（`TaskRequirementCoverage`）。把整份提案登记为任何一个 stage，都会让本批没做的 REQ 算进"未验证"，hard 档 spec 再配合带追溯的验收就会拦下 complete。所以按批次开工时：
- 不把整份提案登记为任务产物，只在批次 spec 里用路径引用它；
- 每批用 `forge task artifact --set spec --stdin` 登记一份**批次 spec**，正文只声明本批的 REQ（其他批的 REQ 只能出现在代码围栏内，围栏内的 ID 不计声明）；
- 验收行用行尾 `#REQ-n` 追溯本批的 REQ，覆盖表的分母就只有本批。
B0 当时没有这个约定，验收没有打标记，覆盖表显示 0/8，属于已知偏差。

## 九、验收

期望值用 `--- PASS: <测试名>`，不用 `ok`：测试函数还不存在时，`go test -run` 也会输出 `ok ... [no tests to run]` 并以 0 退出，`ok` 会空跑通过（已实测）。`EnsureGoTestVerbose` 会自动补 `-v`。

- accept[REQ-0]: go test ./internal/taskpipeline/ -run TestLoopExhaustionNoLeak -count=1 :: --- PASS: TestLoopExhaustionNoLeak
- accept[REQ-1]: go test ./internal/taskpipeline/ -run TestAcceptanceRecurrence -count=1 :: --- PASS: TestAcceptanceRecurrence
- accept[REQ-2]: go test ./internal/taskpipeline/ -run TestEvaluateLoopVerdict -count=1 :: --- PASS: TestEvaluateLoopVerdict
- accept[REQ-3]: go test ./internal/taskpipeline/ -run TestLoopBudgetVerdict -count=1 :: --- PASS: TestLoopBudgetVerdict
- accept[REQ-4]: go test ./internal/clitask/ -run TestResumeRendersDeadEnds -count=1 :: --- PASS: TestResumeRendersDeadEnds
- accept[REQ-5]: go test ./internal/cli/ -run TestInboxAggregates -count=1 :: --- PASS: TestInboxAggregates
- accept[REQ-6]: go test ./internal/hazard/ -run TestConfirmHoldConsumedAfterRun -count=1 :: --- PASS: TestConfirmHoldConsumedAfterRun
- accept[REQ-7]: go test ./internal/hookdispatch/ -run TestAutonomyGuardTiers -count=1 :: --- PASS: TestAutonomyGuardTiers
