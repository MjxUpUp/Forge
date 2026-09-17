# 纪律第一防线：门禁时间再分布（2026-09-17 立项）

> 背景（remin，`~/.forge/projects/9fe95966d4ec`，2026-09-17 单会话 7 个 task_ref，
> checklog 300 行）：按本节口径，门禁类 check ∈ {test-coverage-gate, scope-drift,
> cheat-scan, unused-scan, test-capability-scan, acceptance, artifact-chain,
> plan-first, unused-gate, doc-gate, doc-lint, review-pass, branch-unmerged} 共触发
> 115 次，其中 96 次（83%）落在 task-started/task-verify/task-complete 锚点 ±60s
> 内；m0 编码 26 分钟门禁零反馈，verify 时 12+ 门禁 15 秒内倾泻，报
> "missing tests for 8 files" 后花 ~31 分钟补救性补测。结论：门禁系统只有两种时间
> 模式——工作期低语（可忽略）、终点一次性咆哮（为时已晚）。

## 原则（本 spec 的全部改动不得违反）

1. **纪律是结构，不是自觉**：守纪律必须是摩擦最小的路径。每次门禁拦截都要能倒推
   第一防线缺了什么。
2. **门禁永不停止计算，只降低打断成本**：任何"静默化"改动不得删掉 checklog 审计
   落盘——变薄的是打断预算，不是审计轨迹。
3. **调整优先于新增**：优先重定时/重分级已有资产（test-nudge、artifact-chain、
   plan-first、markers、节流文件）；只有度量（P1-A）与镜像入口（P2）允许新增。
4. **防 gaming**：任何"门禁更安静"的改动必须伴随可复核的度量（discovery rate），
   且质量遥测（assertion density、cheat-scan）不得恶化。

## 度量定义（P1-A 落地后即可回测）

| 指标 | 定义 | 数据源 | 基线 | 目标 |
|---|---|---|---|---|
| D1 discovery rate | `discovery/(discovery+confirmation)`（分母只含已分类的失败条目；未分类=历史/通过，排除。verify 与 complete backstop 两相位同口径落 outcome——BLOCK 层不缺位） | checklog `outcome` 字段 | 无字段（本 spec 前不可测） | 周切片环比不升，连续 4 周趋势下降 |
| D2 nudge 分档命中率 | nudge 跨档时的 `unpaired_files` vs 同任务 verify 条目 `missing_files`（两侧均结构化 Meta 键） | checklog `unpaired_files` vs `missing_files`/`missing_list` | 无（nudge 数写入事件不数文件） | tier3 触发数 ≥ verify missing≥8 任务数（nudge 先于门禁看到同一事实） |
| D3 纪律债兑现率 | `confirmation/(discovery+confirmation)`（与 D1 同分母；confirmation 是**事实级**——第一防线清单与该批 missing 有文件交集，task 级"曾有过信号"不算，防早 nudge 兑现后尾段新欠虚高 D3） | checklog `outcome` | 无 | 观察指标：升=信号有效但未被执行，降+D1 降=双好 |
| 防伪护栏 | assertion density（scoring.CollectAssertionDensity）、cheat-scan 命中率不得恶化；test-nudge 触发总量 ≤ 同期非测试源码 Write/Edit 事件数 × 0.5（activity 度量=checklog/toollog 的源码写入事件；振荡路径（配一个再加一个）每轮重触发同档、**无上界**——守护审计 P1-1 实证，故护栏必须按事件比值实算，不得用"每任务至多 N 次"的假上界） | scoring / checklog | 合入后首月用 `forge eval harness-audit` 钉数值基线并落盘 evals/（不预填数字） | 护栏越线 → 对应改动 reject |

**P1-A 分类覆盖清单（v1）**：仅 `test-coverage-gate`（对 test-nudge）与
`scope-drift` 的**失败**条目盖 outcome 章；cheat-scan / unused-scan /
conventions-lint / cross-repo-impact 的失败条目 v1 不分类（D1 分母自动排除，
不为它们虚标 discovery——补分类属 P3/P4 范围）。

## 分期

- **P1（已落地，2026-09-17，90/A）**：度量地基 + test-nudge 从"事件计数器"升级为
  "文件级跨档信号"。
- **P2（本文件落地范围，契约见下）**：`forge selfcheck pairing|scope` 镜像命令 +
  selfcheck 条目接入 outcome 分类 + next-hint 时机训练提示。
- **P3（本文件落地范围，契约见下）**：artifact-chain 的 task start 开工前 advisory。
  修正记录：plan-first **已在** implement gate（executor.go:465 shift-left advisory，
  每任务一次）——remin 时间线里的 plan-first 条目实为 implement 时刻产物，原始
  诊断"挤在 verify 批"对该项误读，无需迁移。
- **P4（本文件落地范围，契约见下）**：同任务重复 verify 的 advisory 折叠 +
  BLOCKED 还债指引。原 P4 的「clean streak N → 一行绿」**显式暂缓**：pass 态
  在现 verify 输出形态下本已静默（无逐 check 绿行），无证据表明需要 streak
  行；D1 数据积累一个窗口后重估（守护监督清单 P4-1 的静默腐化风险——escape
  与 confirmation 均不断 streak——也要求等数据再上）。
- **P5（暂不立项）**：RED 运行证据、配对测试脚手架——按本 spec 回测流程，待 D1
  discovery rate 积累一个窗口（30 天或 25 任务）后由数据决定；无数据前不新增
  执法能力（原则 3：调整优先于新增）。

### P2 契约

1. **命令**：`forge selfcheck pairing` 与 `forge selfcheck scope`（顶级命令，
   internal/cli/selfcheck.go）。对**当前活跃任务**的改动集跑与门禁同一代码路径的
   纯计算：pairing = `taskChangedFiles` + `coveragePairing`；scope =
   `taskChangedFiles` + `ScopeDrift(PlanScope)`。无活跃任务 → 报错退出 1（无任务
   即无门禁可预演）。
2. **行为**：发现项逐行输出 + 计数汇总 + 下一步指引（写配对测试 / scope add）；
   干净输出一行确认。退出码：干净 0，有发现 1（agent 可感知）——selfcheck 是
   自检不是门禁，退出码只反映事实。
3. **落痕**：checklog 新 Check 名 `selfcheck-pairing` / `selfcheck-scope`。
   pairing 侧与门禁条目同口径 Meta（`missing_files`/`missing_list`）；scope 侧
   `drift`/`drift_files` 是 **selfcheck 侧键**——门禁的 scope-drift 条目 v1 无
   结构化 Meta（仅 Detail 散文，回测脚本不得依赖；补齐属后续项）。
   carve-out：PlanScope 未声明时 scope 探针空转**不落痕**（无可镜像的声明——
   与 pairing 侧 0 文件也落 Passed=true 不对称，是刻意省略）。
   Passed 如实、TaskRef 必带、deterministic 源。
4. **outcome 接入（事实级）**：confirmation 判定 = 已送达 test-nudge 的 files
   清单 **或** selfcheck-pairing 条目的 missing_list 清单，与该批 missing 文件
   **有交集**（守护监督 P2-1：任意结果计入 = agent 跑一次干净 selfcheck 即给
   全任务镀 confirmation 层，D3 变可刷量指标——交集封死该通道）。干净自检后
   漂移的新文件落 discovery：agent 从未被告知过它们。
5. **时机训练**：NextHint 对 NextDecision 判定为 verify-acceptance 待跑的任务
   （且 task 无 selfcheck-pairing 条目时）在 Reason 追加自检提示：建议先
   `forge selfcheck pairing` 镜像自检。Next 命令本体不变（每行恰一条命令的
   纪律保持）；判定锚 `res.Next`（NextDecision 归一化后的事实分支）。

验收（accept 围栏在「P2-P4 总验收」）：

```
Run: go test ./internal/cli/ -run TestSelfcheck
Expected: PASS（无任务拒绝；配对发现项输出+落痕+退出码 1；干净输出+退出码 0）
```

```
Run: go test ./internal/taskpipeline/ -run TestOutcomeSelfcheck
Expected: PASS（selfcheck 条目在场 → verify 失败 outcome=confirmation）
```

### P3 契约

1. `forge task start` 输出尾部追加开工前 advisory：按 protocol 产物链（与
   implement gate 的 CheckArtifactChainGate 同一真相源）列出**预期而未登记**的
   节点，提示"开工前产出最便宜"；链完整或无链要求则不输出。只提示不阻断、
   不置 ArtifactAdvisoryFired 标记（implement 轮的一次性语义不变）。**每个新
   任务必发一次是设计而非噪声**：产物链是任务纪律的组成部分，start 提示
   是其开工引导（守护监督 P3-2 的 false-positive 担忧按此口径解读）。
2. 验收：`go test ./internal/clitask/ -run TestTaskStart_ArtifactHint` PASS。

### P4 契约

1. **重复 advisory 折叠（v1 收窄至 test-coverage；scope-drift 等其他 advisory
   暂不折叠**——remin 实证只复现 test-coverage 的 3 连跑噪音）：同一 task 的
   verify 里，与**最近一次**披露的**全量 missing 集合**相同（排序后比对，
   >8 文件一律不折叠）才折叠为一行 unchanged 摘要（含 N 计数与「修复后输出
   将恢复完整」提示）——Detail
   在 >3 文件时只含前 3 名，作折叠键会假折叠换血文件（复审 P2-1）。checklog
   照记（审计轨迹不变薄，打断预算下降——原则 2）。
2. **BLOCKED 还债指引**：`GateBlocked` 消息统一追加一行 skill-evolution 还债
   提示（`forge skills decide` 记教训），把拦截转化为纪律资产输入。
3. 验收：`go test ./internal/taskpipeline/ -run TestFoldRepeatAdvisory` 与
   `go test ./internal/taskpipeline/ -run TestGateBlockedDebtHint` PASS。

### P2-P4 总验收（verify-acceptance 实跑口径）

```accept
go test ./internal/cli/ -run TestSelfcheck
go test ./internal/taskpipeline/ -run "TestOutcomeSelfcheck|TestFoldRepeatAdvisory|TestGateBlockedDebtHint"
go test ./internal/clitask/ -run TestTaskStart
go test ./...
go vet ./...
```

## 回测流程

## P1-A checklog `outcome` 字段（confirmation / discovery）

**语义**（只盖在**失败**的 verify 期门禁条目上；通过/历史条目留空=未分类）：

- `discovery`：门禁是此事实对 agent 的第一披露点——task 内此前无已送达（Delivered）
  的同类第一防线信号。含义：第一防线缺位（无信号或信号死了）。
- `confirmation`：task 内此前存在已送达的同类信号（如 test-nudge），agent 被告知过
  但未行动，门禁确认了纪律债兑现。含义：第一防线在投放，纪律未被执行。

**改动点**：

1. `internal/checklog/types.go`：`type GateOutcome string` + 常量
   `OutcomeDiscovery` / `OutcomeConfirmation`；`Entry` 增 `Outcome GateOutcome
   json:"outcome,omitempty"`（位置随 Source/Level 一组；omitempty 兼容历史行）。
2. `internal/taskpipeline/executor_check_verify.go`：`checkVerifyTestCoverage` 在
   `!ok` 时查 `checklog.LoadForTask` 中是否存在 `CheckTestNudge` 且 `Delivered=true`
   的先于本条目的记录——有则 `confirmation`，无则 `discovery`。
   `checkVerifyScopeDrift` 失败时恒 `discovery`（今日无上游信号——如实记录第一
   防线缺口，为 P3 的 nudge 前移提供依据）。test-coverage 条目同步补结构化
   Meta（`missing_files` 计数 + `missing_list` 清单 ≤8 截断）——D2 度量的门禁侧
   数据源，回测脚本不解析无契约的 Detail 散文。

验收命令见下方「P1 总验收」的 accept 围栏（verify-acceptance 实跑口径）。

## P1-B test-nudge：事件计数器 → 文件级跨档信号

**病灶**（remin 实证）：nudge 数的是 Write/Edit **事件**而非**未配对文件**——19 次触发
全部显示 "3 source writes"，agent 写一个测试文件即全量重置，8 个文件照旧无配对直达
verify。措辞无文件名、无档位、无后果，重复即噪声。

**新契约**：

1. **状态改文件集**：`testNudgeState` 持 `UnpairedFiles []string`（仓库相对路径，
   去重——同一文件反复 Edit 不膨胀）+ `FiredTier int`。源码写入（`ClassifyChangedPath`
   判 source）追加；测试写入按配对约定移除匹配源文件（新导出
   `taskpipeline.TestPairsSource(testRel, srcRel) bool`，覆盖 hasMatchingTest 的同目录
   直接配对约定：`_test.go/_test.rs/.test.ts/.spec.ts/.test.js/.spec.jsx/test_*.py/
   foo_test.py/FooTest.java/foo_spec.rb/foo_test.zig` 等）。
2. **跨档触发**：档位 = f(未配对文件数)，阈值 3/5/8（8 对齐 remin m0 verify 实报
   missing 数）。仅在 `tier > FiredTier` 时触发一次；配对使档位回落时 FiredTier
   随之下调（归零即完全重新武装）——不逐写刷屏，只在状态**跨档**时说话。
3. **措辞升级**（保持事实性、非祈使、自然语言引用 skill 的既有纪律）：
   - tier1（3）："N unpaired source files in this task: a.go, b.go, c.go …
     (task gate task-verify checks this pairing; whitelist: entry
     points/generated/pure types). Load the test-discipline skill …"
   - tier2（5）：同上 + 未配对文件名换新。
   - tier3（8）：追加门禁后果事实（与 taskpipeline 真实规则同步锚定：
     `testCoverageHardGateThreshold`，当前 3，BLOCK 点在 task-complete 兜底）——
     "at >=3 untested source files with zero assertions the task-complete
     backstop BLOCKs the task"。
4. **Meta**：`unpaired_files`（数）、`tier`、`files`（逗号清单，≤8）。
   `Detail`：`test-nudge: N unpaired source files (tier T): a.go, b.go …`。
5. **不变式**：活跃任务门控（任务外静默不落状态文件）、永不 block、每档一次、
   Delivered 章按宿主通道如实盖。

验收命令见下方「P1 总验收」的 accept 围栏（verify-acceptance 实跑口径）。

## 已知限制（如实披露，直接影响 D2 解读）

1. **无锁读-改-写**（`hook_track.go` 状态文件）：并发 PostToolUse 的丢失窗口
   最坏漏一次跨档触发（档位低报）——fail-open 方向，main 既有模式，未加锁。
2. **Go 配对单向不对称**：门禁 `hasMatchingTest` 有 Go package 级兜底（同目录
   任意 `_test.go` 即覆盖），nudge 侧 `TestPairsSource` 刻意不镜像（方向安全：
   nudge 命中 ⊆ 门禁命中）——**nudge 的 unpaired 系统性 ≥ 门禁的 missing**，
   D2 两侧对比时须计入此偏高。
3. **SessionID 键控**：状态按会话隔离，同任务多会话并发时各自只见部分写入
   （nudge 低报方向；task 内单会话为主流形态，remin 即如此）。
4. **1.58 同步义务**：tier3 事实句锚定 `testCoverageHardGateThreshold`（当前
   3，1.58 计划降 2）且 BLOCK 点在 task-complete 兜底——阈值变更必须同步
   `hook_track.go` 的 tier3 文案与注释，否则事实性通道违规（审查 P1-1 已修一次）。
5. **逃生路径计数为 0**：test-coverage 逃生激活（per-task override 或
   FORGE_TEST_COVERAGE）时 verify 条目 `missing_files` 恒为 0——D2 逐任务对比
   必须按 CheckEscapeHatch 条目的 `escape.gate=test-coverage` 过滤逃生任务，
   否则会把「逃生」误读为「agent 已配对」（selfcheck 在逃生下仍报事实并显式
   标注口径差，见 internal/cli 的 TestSelfcheckPairingEscapeActive）。
6. **观察通道 ≠ 门禁通道**（守护审计 P1-3）：nudge 只观察 PostToolUse
   Write|Edit，门禁看 git 全量——Bash 写入（heredoc/sed/cat>）的源码对 nudge
   完全不可见，纯 Bash 写文件的任务 nudge=0 而 missing=8，方向可反转；这同时
   是 evasion 通道（零 tier 触发、全部洗成 discovery）。D2 解读须把 Bash 写入
   量列为混杂变量；代码级修复（Bash 写入观察）另立项。
7. **队列宿主的结构性偏差**（守护审计 P2-2）：kimi 上 nudge 恒
   Delivered=false（advisory-queue 攒发且无回填章）——队列宿主的 D1/D3 被
   host 构成主导。回测首切片维度定为 channel（checklog `channel` 键）。
8. **事实级交集的截断盲区**：nudge 的 files Meta 与 selfcheck 的 missing_list
   均 ≤8 截断——>8 文件时交集可能漏报，方向偏 discovery（保守：不虚增
   confirmation）。
9. **大小写不敏感文件系统**：nudge 状态按精确路径去重，a.go/A.go 在
   case-insensitive FS 上是同一文件却计 2 个未配对（幽灵点名）。不归一化是
   刻意的（case-sensitive FS 上归一会误合并真不同文件）。

## P1 总验收（verify-acceptance 实跑口径；裸命令 = 退出码 0 判定）

```accept
go test ./internal/checklog/ -run TestEntryOutcomeRoundTrip
go test ./internal/taskpipeline/ -run TestCheckVerifyTestCoverage
go test ./internal/taskpipeline/ -run TestFirstLineHoldsFact
go test ./internal/hookdispatch/ -run TestRunTestNudgeHook
go test ./internal/taskpipeline/ -run TestTestPairsSource
go test ./...
go vet ./...
```

lint 口径：`golangci-lint run` 对本任务 diff **零新增**（存量 issues 为 main 既有
债务，不在本任务范围——仓库纪律：lint 只修它标记当前 diff 的部分）。复算命令
（对照组实测 65 issues，2026-09-17）：`git worktree add /tmp/x main` 后在
`/tmp/x` 运行 `golangci-lint run`，与本分支同命令输出比对——新增数 = 分支数 −
main 数，本任务实测为 0。

## 回测流程

1. 合入后首个 30 天或 25 个完成任务（先到为准）用 checklog 全量算 D1-D3（一次性
   脚本即可，字段已结构化）。注意：`internal/aatout` 的条目也带裸 `outcome`
   JSON 键（不同语义）——回测 join 必须按 `check` 名 + `outcome` 双键过滤。
2. D1 无下降 + D3 上升 → 说明信号送达但执行断裂，优先评估 P2（selfcheck 降低守纪
   摩擦）而非加信号。
3. 防伪护栏任一越线 → 按 skill-evolution reject 记决策并 scoped revert 对应改动。
