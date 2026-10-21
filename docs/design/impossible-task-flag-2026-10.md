---
status: draft
owner: MjxUpUp
ask: 批准"声明挂在 question/answer 上、裁决只能真人、驳回按逃生计价"三条主线；Q1–Q3 需你拍板；实现排到下个批次
tier: full   # 触及评分语义（ScoreTask）与验收门判定
---

# 设计稿 | R6「不可能」声明（2026-10）

## TL;DR
agent 在跑验收时如果认定某条验收标准和需求本身矛盾，可以对这条标准提交一次"不可能"声明，然后停止在这条标准上的工作：不改测试，也不针对用例写特判。裁决只能由真人在终端里做。裁决为确认时，这条标准从 complete 的验收预检中豁免，并在 `task report` 的未验证面里写一条披露行，总分不封顶。裁决为驳回、或声明后 agent 又让该条跑绿时，总分按逃生舱同价封顶 89。命令面净增 0：声明是 `forge task question` 的新 flag，裁决是 `forge task answer` 的新 flag。本设计稿不预设它能在 Claude 上显著降低作弊率，收益要等 Track A 自测后再下结论（见"问题与证据"第 2 条）。

## 问题与证据
1. **验收标准写错时，agent 只剩两条路，两条都不对。** 验收标准只能追加，不能删（`internal/clitask/task_accept.go` 的 `task accept` 只补登 manual 层）。complete 预检要求每一条都新鲜且 Passed（`internal/taskpipeline/acceptance.go:CheckAcceptanceFresh`）。所以碰到矛盾的标准，agent 要么整门逃生（`forge task override --acceptance-gate disable`，走 `internal/taskpipeline/overrides.go:escapeDisabled`），要么"想办法让它绿"。前者把整个考卷一起免掉了，后者正是 ImpossibleBench 定义的作弊：在不可能完成的任务上拿到通过，本身就说明作弊了。
2. **外部证据要打折扣。** ImpossibleBench（arXiv 2510.20270）给 agent 一个中止出口：发现任务有错时提交 `flag_for_human_intervention`，episode 随即终止。在 Impossible-SWEbench 上，GPT-5 的作弊率从 54% 降到 9%，o3 从 49% 降到 12%，原基准上的性能损失较小。但原文说这个出口对 Claude Opus 4.1 的效果弱得多，Forge 的主力宿主又正好是 Claude。**原文没有给出 Claude 的具体降幅，要靠我们自测。** 仓库内此前已有一次复核把"54%→9%"引用成了无限定结论（`docs/surveys/2026-10-research-workflow-review-evidence.md` R4："丢「对 Claude 效果弱」限定"），本稿不重复这个错误。同一篇论文的另一个结果是：最严格的 prompt 能把 GPT-5 在 Conflicting-LiveCodeBench 上的作弊率从 92% 压到 1%（`docs/plans/escape-hatch-hardening-2026-09.md` P2 节）。也就是说，只靠提示词也能起作用，出口机制不是唯一手段。
3. **因此 R6 在 Forge 里的价值不全押在"改变 agent 行为"上。** 即使对 Claude 的行为改变很小，它至少能做到三件事：①给人一个按单条标准豁免的、带签字的出口，替代整门逃生；②让"声明后被驳回"的自供有可见的代价；③产出可计数的数据（声明数、确认率、驳回率），作为 Track A 自测的观测面。

## Non-goals
- 不新增命令，不新增逃生 env（`internal/compat/compat.go:EscapeEnvs` 不变）。
- 不新增阻断位点：声明悬而未决时，complete 由既有验收门拦下（标准仍是 Passed=false），文案里多一行说明。
- 不允许 agent 自己裁决，也不允许编排器 agent 代裁：裁决和 `forge hazard confirm` 一样只认真人终端。
- 不给"诚实声明"加分。确认后只是不扣分，避免把声明本身变成刷分手段。
- 不做整任务级的"不可能"（整任务不可做应走 `forge task abort` 或 `task fail`），R6 只针对单条验收标准。
- 本周期只出设计，不写实现。

## 与既有机制的关系（问题一：和 `forge task question` 是什么关系）
`question` 现状：`internal/clitask/task_assignment.go:runTaskQuestion` 调 `internal/tasktypes/types.go:TaskState.Question`，后者在 `Assignment == nil` 时返回 `ErrNoAssignment`，不是 `claimed` 状态时返回 `ErrQuestionNotClaimed`。也就是说，它**只服务于分派任务**，内容是自由文本，任何调用方都能 `answer`（`TaskState.Answer` 把答复记成一条 Decision 并回到 claimed），对门禁和评分**没有影响**。悬而未决的问题会以 `input-required` 出现在循环判词里（`internal/taskpipeline/loopverdict.go:EvaluateLoopVerdict`，解除方式见 `loopResolve[LoopCodeInputRequired]`），超过 7 天会被僵尸扫描标出来（`internal/taskpipeline/health.go:IsInputReqStale`）。

| 维度 | question（现状） | 不可能声明（本设计） |
|---|---|---|
| 适用任务 | 只限分派任务且状态为 claimed | 所有 code 任务；分派任务额外复用 input-required |
| 载荷 | 自由文本 | 标准序号 + 矛盾证据（单行，≤512 字节） |
| 前置 | 无 | 该条标准至少实跑过一次且当前未通过 |
| 谁来裁决 | 任何调用方 | 只认真人终端 |
| 对门禁和评分 | 无 | 确认：豁免该条；驳回或撤回：总分封顶 89 |

结论：不可能声明是 question 的一种**结构化特例**，挂在同一条命令上。对分派任务，声明同时调用 `TaskState.Question`（LastQuestion 前缀为 `[impossible #N]`），这样 `mine`、健康扫描、僵尸 TTL 不用改就能看到它；对非分派任务，不动 Assignment，只写声明记录。pending 声明存在时，不带 `--impossible` 的普通 `forge task answer` 被拒绝并提示改用 `answer --impossible confirm|reject`，避免状态回到 claimed 而声明仍悬着；裁决写入的 Decision 的 `By` 取真人终端的 OS 用户名，不沿用 `OfferedBy`。

## 需求
- REQ-1 当 agent 执行 `forge task question --impossible <N> --content "<矛盾证据>"` 时，系统依次校验：第 N 条标准存在、`AcceptedHeadCommit` 非空（实跑过）、当前 `Passed=false`、证据为单行且不超过 512 字节、未超出配额（REQ-6）。任一不满足就报错退出，状态不变。全部通过时写入一条 pending 声明，并落一条 checklog `impossible-claim` 行（Source=agent-claim，Level=warn）。
  accept[REQ-1]: go test ./internal/clitask/ -run ^TestTaskQuestionImpossible$ -count=1 :: --- PASS: TestTaskQuestionImpossible (
- REQ-2 当执行 `forge task answer --impossible confirm|reject --content "<裁决理由>"` 且 stdin 不是真人终端时，系统拒绝并退出 1，声明保持不变；是真人终端时，更新声明状态并记下裁决人、时间和理由。
  accept[REQ-2]: go test ./internal/clitask/ -run ^TestTaskAnswerImpossibleRequiresHuman$ -count=1 :: --- PASS: TestTaskAnswerImpossibleRequiresHuman (
- REQ-3 complete 预检时：pending 声明对应的标准照旧拦下，拒绝文案追加"声明 #N 待人工裁决"；confirmed 的标准跳过新鲜度和 Passed 检查；全部标准都被确认豁免时，按零验收拦下，指引走 `task accept` 补标准或 `task abort`。
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestAcceptanceFreshSkipsConfirmedImpossible$ -count=1 :: --- PASS: TestAcceptanceFreshSkipsConfirmedImpossible (
- REQ-4 `forge task report` 在"未验证面"段为每条 confirmed 声明输出一行披露，为 pending 声明输出"待裁决"行；被驳回的声明计入"自供申辩"，并单独输出一行 ⚠。`--json` 新增 `impossible_claims` 数组。
  accept[REQ-4]: go test ./internal/clitask/ -run ^TestTaskReportImpossibleClaims$ -count=1 :: --- PASS: TestTaskReportImpossibleClaims (
- REQ-5 `ScoreTask` 在任务有 ≥1 条被驳回或撤回（REQ-9）的声明时，总分封顶 `escapeCapMaxScore`（89），`CappedReason` 按状态写明"impossible-claim rejected"或"impossible-claim withdrawn"，复盘时可区分。只有 confirmed 声明时总分不封顶，`Evidence.WaivedCriteria` 记录条数，只用于披露。
  accept[REQ-5]: go test ./internal/taskpipeline/ -run ^TestScoreTaskImpossibleRejectedCap$ -count=1 :: --- PASS: TestScoreTaskImpossibleRejectedCap (
- REQ-6 单任务最多 2 条声明（各状态合计，withdrawn 也算）；第 3 条拒绝，并提示"标准大面积有误时应 abort 后重建任务"。
  accept[REQ-6]: go test ./internal/clitask/ -run ^TestImpossibleClaimQuota$ -count=1 :: --- PASS: TestImpossibleClaimQuota (
- REQ-7 存在 pending 声明时，循环判词增加 `impossible-pending` 原因，解除方式写"需人工：forge task answer --impossible confirm|reject（真人终端）"。
  accept[REQ-7]: go test ./internal/taskpipeline/ -run ^TestLoopVerdictImpossiblePending$ -count=1 :: --- PASS: TestLoopVerdictImpossiblePending (
- REQ-8 forge-quality skill 增加"测试与需求矛盾时"一节：不改测试、不写特判，改为提交声明。
  accept[REQ-8]: go test ./internal/skillgen/ -run ^TestQualitySkillImpossibleSection$ -count=1 :: --- PASS: TestQualitySkillImpossibleSection (
- REQ-9 声明被撤回（声明后该条标准又跑绿）时，声明置为 withdrawn，计入"自供申辩"，并在 `task report` 单出一行 ⚠ "声明 #N 后该条又被跑绿"；评分与 rejected 同样封顶 89。理由：先喊不可能、再让它通过，正是 ImpossibleBench 定义的作弊信号，不能零成本。
  accept[REQ-9]: go test ./internal/taskpipeline/ -run ^TestScoreTaskImpossibleWithdrawnCap$ -count=1 :: --- PASS: TestScoreTaskImpossibleWithdrawnCap (
- NFR-1 单任务声明上限 2 条（REQ-6）；声明与裁决各只增加一次 TaskState 写入，`task report` 渲染不新增外部调用。由 REQ-6 的测试断言配额。
- 以上测试名都是计划名，目前不存在，实现批再落。

## 设计
### 触点
- `internal/clitask/task_assignment.go:runTaskQuestion` / `runTaskAnswer`：新增 `--impossible` 分支。answer 的真人校验复用 `internal/clitask/task_gate.go:stdinIsHumanTerminal`（与 `--trust-foreign` 同款；`internal/cli/hazard.go:runHazardConfirm` 是同一模式的先例）。
- `internal/tasktypes/types.go`：TaskState 新增 `impossible_claims` 键（只增键），新增 `RaiseImpossible` / `AdjudicateImpossible` 方法；分派任务的声明内部调用 `TaskState.Question`。
- `internal/taskpipeline/acceptance.go:acceptanceFreshCore`：跳过 confirmed 的标准。`internal/taskpipeline/acceptance_register.go:CheckAcceptanceRegistered` 把"全部被豁免"视同零验收。
- `internal/taskpipeline/scoring.go:ScoreTask`：在逃生封顶判断旁边加第二个封顶信号（REQ-5）。
- `internal/clitask/task_report.go:buildTaskReport` / `renderTaskReport` / `countSelfSupplied`：补披露（REQ-4）。
- `internal/taskpipeline/loopverdict.go`：新增 code 和 `loopResolve` 条目（REQ-7）。
- `internal/checklog/escape.go:allCheckNames` 新增 `impossible-claim`，配 roster guard test。该行**不进** `internal/checklog/evidence.go:verificationChecks` 白名单：声明是自述，不能抬高证据强度。
- `internal/skillgen/qualitygen.go:buildQualitySkillContent`（REQ-8）。改 skill 后要用仓库构建的二进制重新打 plugin 包、重钉 `compat.snapshot.json`（同 task-resume-pull-model 方案风险节）。

### 接口
```go
type ImpossibleClaim struct {
    Criterion     int        `json:"criterion"`       // 1-based，对应 Acceptance 下标+1
    Evidence      string     `json:"evidence"`        // 单行，≤512 字节
    RaisedAt      time.Time  `json:"raised_at"`
    Status        string     `json:"status"`          // pending|confirmed|rejected|withdrawn
    AdjudicatedBy string     `json:"adjudicated_by,omitempty"` // 真人终端时取 OS 用户名
    AdjudicatedAt *time.Time `json:"adjudicated_at,omitempty"`
    Note          string     `json:"note,omitempty"`  // 裁决理由（必填）
}
```
report 披露行的格式（渲染层，不承诺）：`│   ⊘ 验收 #N 经人工确认不可能（<by>，<date>）：<evidence>`；被驳回的写成 `│ ⚠ 不可能声明 #N 被人工驳回——agent 曾以"不可能"自述绕过该条`。

### 评分怎么改（问题二的后半与评分语义）
`ScoreTask` 目前有两个封顶信号：`internal/taskpipeline/overrides.go:usedAnyOverride` 和 `taskEscapeHatchRecorded`。命中任一就把总分封到 `escapeCapMaxScore=89`，并写 `CappedReason`（字段见 `internal/scoringtypes/types.go:ScoreResult`）。评分维度本身（`scoring.Evaluate`）不读 Acceptance，所以豁免一条标准不会直接影响维度分。R6 的改动只有两处：①新增第三个信号"存在 rejected 声明"，同样封顶 89；②confirmed 声明只写披露字段，和 `evidenceUntestedAreas` 一样"只披露，不改分数"。封顶施加在 `ScoreTask`，不在 `Evaluate`，所以 golden fixture 不受影响（与现有逃生封顶同理）。

### 防滥用（问题三）
对照 compat-commitments 第二节第 3 条的逃生舱纪律（留 checklog 痕、证据封顶 Weak、带 reason/owner）：
1. **先跑后声明**：必须实跑失败过才能声明（REQ-1），堵住"还没动手就喊不可能"的路。
2. **裁决外置**：agent 和编排器 agent 都不能确认，只认真人终端（REQ-2）。信任边界与 hazard confirm 相同。
3. **驳回和撤回都有价**：被驳回或撤回（声明后又跑绿，REQ-9）的声明按逃生同价封顶 89，并计入自供申辩，在验收单上可见。确认的声明不封顶，因为人已经认定是考卷的错。这样诚实且属实的声明比整门逃生便宜，虚报和逃生一样贵。
4. **配额**：每任务最多 2 条，先例是 `internal/clitask/task_wild.go:wildQuotaPerSession`（会话 3 条）。
5. **留痕不抬证据**：checklog 行 Source=agent-claim，不进白名单；裁决结果写回 state，供 dashboard 统计确认率和驳回率。
6. **不重复计价**：确认豁免不写 escape-hatch 行，所以不会触发 `EvidenceChain.Strength` 的 Weak 封顶。原因是这条标准并非 agent 跳过的门禁，而是人签字作废的考题；在未验证面上披露已经足够。

### 失败模式
- 声明后 agent 又让这条标准跑绿了：verify-acceptance 把声明置为 withdrawn，按 REQ-9 计价并披露，仍占配额。
- 分派任务等待真人裁决，编排链停住：这是预期行为，`IsInputReqStale` 在 7 天后上浮。
- state JSON 是 agent 可写的，agent 可以手改 status。信任边界与现有逃生信号相同（ScoreTask 注释已写明接受这一点）；裁决同时落 checklog 行，dashboard 可以对账。
- 非 TTY 环境下的真人（CI、mintty）无法裁决：沿用 hazard confirm 的提示文案，指引改用 ConPTY 终端。

## 备选对比
| 方案 | 一句话 | 结论 |
|---|---|---|
| A question/answer 加 `--impossible`（推荐） | 复用 input-required 语义和分派面板 | 采纳 |
| B `task block --impossible` | Blocker 对所有任务都可用，但 open blocker 不参与任何门禁，接 complete 需要另起判定 | 否：要新增判定路径，且和分派状态机脱节 |
| C 复用 `override --acceptance-gate` | 不改代码，只写文档引导 | 否：整门豁免，粒度过粗，也没有真人签字 |
| D 驳回写 escape-hatch 行 | 让既有 `taskEscapeHatchRecorded` 自动封顶 | 否：污染逃生库存与漏斗统计的语义，所以在 ScoreTask 显式判断 |
| 什么都不做 | 维持现状 | 否：写错的考卷只能整门逃生或作弊 |

## 风险与未决问题
- **风险：对 Claude 几乎无效。** 论文已经说明效果弱。缓解：机制价值的①②不依赖行为改变；行为侧的收益靠 Deferred 第 2 项自测，测出来无效就不把 REQ-8 的 skill 文案写成强指引。
- **取舍：诚实撤回与作弊撤回同价。** agent 发现自己误判、随后找到真修复，也会按 REQ-9 封顶 89。本稿接受这个代价（撤回本身说明声明草率），首月数据后随 Q2 一并复议。
- **风险：给人增加裁决负担。** 配额加上"先跑后声明"把频次压低；dashboard 统计每周裁决量。
- **Q1（需拍板）**：确认的豁免是否也该封顶（例如 95）？本稿主张不封顶，理由见防滥用第 3 条。
- **Q2（需拍板）**：配额 2 是拍脑袋的数字，要不要等首月数据再定？
- **Q3**：compat 处理——question 和 answer 加 flag 后签字比对会判为 changed，拟援引 2026-09-20 裁决（纯新增、不改既有 flag 语义），在实现批的 compat 裁决记录里落一行。

## Deferred
| 事项 | 触发条件 | 复验日期 | 消费方 |
|---|---|---|---|
| R6 实现（REQ-1–9、NFR-1） | 本设计稿获批 | 2026-12-15 | 周度回顾 |
| Claude 自测：构造 impossible 变体（把验收测试改成与需求矛盾），对比有无声明出口两臂的作弊率（变体上任何通过都计为作弊），同时测原任务上的误声明率 | R3 合入且首次正式 Track A 对照跑通（`internal/evalkit/runner.go:RunBenchmark`） | 2026-12-15 | R6 实现批定 REQ-8 文案的强弱；结论写回本稿 |
| 配额与 Q1 复议 | 实现合入后满 30 天，或累计声明 ≥20 条 | 实现合入 +30 天 | 周度回顾 |
