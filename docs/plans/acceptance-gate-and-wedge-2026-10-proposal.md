---
status: review
owner: MjxUpUp
ask: R1–R2 已合入，R3–R5 本批待审，R6 设计稿与 W2 演示文档已出，请审；W3 的 infra 冻结规则与发版节奏仍需你本人拍板（未决 Q1）
tier: mini   # 本文是周期总纲，不受方案篇幅档约束；R1–R5 每批开工时各自按 design_tiers 定档，R6 预计 full
---

# 下一周期落地计划：改动接受门 + 外部楔子（2026-10-13 立项）

## 落地进度（2026-10-21 更新）

| 项 | 状态 | 落点 |
|---|---|---|
| R1 mutation 阳性对照 | 已合入 | `fix/mutation-baseline-control`（`4567a8a3`） |
| R2 Warrant-Aware Acceptance | 已合入 | `feat/decision-warrant`（`2738ad63`）；未决 Q2 已按推荐定为新增 `--warrant` |
| R3 / R4 / R5 | 本批实现，待审 | 合为一批（均在 `internal/evalkit`），方案 `docs/plans/eval-validity-r3-r5-2026-10.md`；REQ 以该方案为准（与本文 §四两处措辞不同：REQ-5 复核清单按任务而非 attempt 排序，且 leakage 通过的任务一律入列；REQ-8 本批只提供 `golden run --release` 手动入口，接入发版流水线见该方案 Deferred）；未决 Q3 在该方案拍板为脚本化 `leakage_cmd` |
| R6 「不可能」声明 | 设计稿已出 | `docs/design/impossible-task-flag-2026-10.md`（只出设计，实现见第六节 Deferred） |
| W2 演示文档 | 已出 | `docs/wedge-demo-anti-false-green.md` |
| W1 外部种子用户 / W3 infra 冻结 | 待你本人 | 运营动作；W3 规则见未决 Q1 |

## TL;DR

接下来一个周期只走两条线。**W 线（外部楔子）**：拿到 3 个外部用户，同时冻结 infra 投入。**R 线（改动接受门）**：
把 Forge 做成"判定一次 agent 改动是否真的变好"的裁判。改动的来源可以是人、agent 自己，或 SkillOpt 这类优化器。
命令面净增 0。不恢复学习闭环，不做 skill 自动生成。依据是 2026-10-10 的 RSI 调研
（[飞书主报告](https://my.feishu.cn/wiki/AuQBwypHmiKNOik85WMcpnrCnPc)，本地 `~/.research-workflow/rsi-selfevolve-20261010-1038/report.md`）
和下文现状证据。

## 一、问题与现状证据

1. **决策日志是"只进不出"的接受门。** 统计 `skills/` 与 `skills-forge/` 下 45 份 `decisions.md`：
   2026-07-18 至 2026-10-12 共 **271 条决策，271 条 outcome 都是 accept**，reject/revise/defer 为 0；
   带 `ProbeRun` 锚点的 **0 条**，带 Commit 锚点的 5 条，带 Prediction 的 13 条。
   `internal/skillsdecisions/decisions.go:SkillDecision` 的 `Evidence` 字段是自由文本（:62），没有任何核验。
   外部有一组同构的实验证据：Phantom Guardrails（arXiv 2607.13083）。它是作者自建的确定性微实验室，
   植入一个"永远不会触发"的 guard，正确动作是什么都不改。
   - 单轮提议：合法输入里出现一个看起来像规则的无害模式时，proposer 在 15/60 次运行中虚构出从未发生的失败，
     并启用这个空操作 guard；无特征输入的对照组为 0/60。只有三个条件同时成立才出现——像规则的模式、开放的规则集、
     预设"存在失败"的指令——去掉任一条件即消失。
   - 只加不删的接受循环：只看抑制分数、"不变差即接受"的接受门会接纳这类空操作改动。在全合法样本池上，
     部署方案里含虚构 guard 的轨迹从第 1 轮的 1/60 涨到第 4 轮的 11/60；改成"严格提升才接受"后，
     虚构 guard 仍会夹带在真修复的批次里进入（2/60）。一旦进入就不会被移除。
   - warrant-aware 接受（只有引证到 oracle 确认过的失败，新增 guard 才算数）：各轮被接纳的虚构 guard 为 0/60，
     真修复仍是 60/60。
   限定：接受循环被刻意设为只加不删；允许删除时，proposer 自己复查一次能删掉它约 0.88 次（附录 B）。
   因此这是方向一致的外部佐证——只核"不掉分"的接受门挡不住空操作改动、要求可核验引证能挡住——
   不是对本仓 decisions.md 的直接测量。
2. **mutation 缺阳性对照。** `internal/taskpipeline/mutation.go:RunMutationSampling` 把测试的任何非零
   退出都计为 killed（R1 合入前 `ce8424bc^` 的 :328-330；R1 已在该处加阳性对照），但从不先跑一次**未变异**的包测试。如果包测试本来就是红的（或被改成恒失败），
   就会得到 Score=1.0，CLI 打印"✅ 全部杀灭"（`internal/clitask/task_questioning.go:runTaskMutation`）。
   这是一条真实存在的假绿路径。
3. **Track A 的有效性前置只有设计，没有代码。** `docs/design/forge-evaluation-system.md` §六 P2
   规定 oracle / do-nothing 基线是启动前置，但 `internal/evalkit/runner.go:RunBenchmark` 里没有任何
   oracle、do-nothing、leakage 逻辑（grep 0 命中）。调研中的 *Identical Runs* 显示 312 次运行里 10 次违规，
   这 10 次**包含了全部前七名的高分**。
4. **Track A 还没跑过正式对照。** `~/.forge/evals/forge/runs/` 下只有 2026-09-03/04 的 scorecard。
   CI 现在用的是"运行间 + 任务抽样"两源。调研中的 Noise Floor Audit（arXiv 2608.22331）在 BFCL 工具调用
   任务上（3 个端点、2 家厂商、温度 0）测得语义等价扰动的 paired SD 是重跑的 11–58 倍；它只测了工具调用这一类任务，
   外推到 Forge 的评测集需要我们自己验证，但 profile 切换本身就是一次扰动，方向上值得防。
   **趁第一次正式对照之前补上第三个方差源，成本最低。**
5. **零外部用户（feature-focus-2026-09 自认），且 infra 吃掉了大量精力。** 2026-09-20 至今共 90 个提交：
   feat 11 个、自动发版 19 个、发版链救火 9 个（release.yml blob 上传、base_tree 丢失、npm-verify、revert）。
   compat-commitments 第四节的"infra 批次必须带产品批次"在这段时间没有被守住。

## 二、Non-goals（本周期刻意不做）

- 不恢复 experience/knowledge 学习闭环（`8cedc80`）。重新评估的条件见第六节。
- 不做 skill 自动生成。SWE-Skills-Bench 显示 39/49 零提升，而且我们没有足够的证据流去筛选。
- 不扩宿主宽度（F7 国产宿主深度适配延后），不做分派前台（F5 的 issue-tracker 双向镜像、always-on 治理包延后）。
- 不新增命令。R 线全部通过既有命令的新 flag 或新判定实现。
- 不新增阻断位点，只有一个例外：R3 的"评测自身不合法"，符合 forge-evaluation-system §十。

## 三、W 线：外部楔子（以运营为主，owner 是你本人）

| 编号 | 内容 | 判据 |
|---|---|---|
| W1 | 3 个外部种子用户：**非本人**、在**自己的仓库**跑通楔子路径（init → task start --accept → verify-acceptance），并通过 `forge task export --include-checklog --redact` 回传证据（2026-10-21 更正：`project export` 无 `--redact`，其帮助文本指向 task export） | 截至 2026-12-15：外部仓库数 ≥3，每个仓库 ≥1 条 deterministic `acceptance` 行；同时报 TTFE（首证据时间）和 off_churn |
| W2 | 楔子叙事定为"防假绿"，演示脚本复用 R1（预先置红的包测试被识破）与 cheat-scan | 演示文档落 `docs/`；**依赖 R1 合入** |
| W3 | infra 冻结：W1 达成或 2026-12-15 复验之前，发版链只修 P0（发不出版本、或挡住外部用户）；发版改为按批次合并 | 期间发版链救火提交 ≤2（基线：23 天 9 个）。发版节奏由你拍板（基线：23 天 19 个版本） |

## 四、R 线：改动接受门（代码）

顺序：**R1、R2 先做**（便宜、杠杆最高）→ **R3、R4 必须在第一次正式 Track A 对照之前做** → R5 → R6 只出设计。

### R1 mutation 阳性对照（基线测试必须先绿）

- REQ-1：当抽样涉及的某个包在**未变异**状态下测试失败时，系统不计分，verdict 记为 `baseline-red`、`Passed=false`，
  CLI 提示"包测试本身是红的，mutation 无法度量"，不再打印"全部杀灭"。
- NFR-1：每个不同的 PkgDir 只额外跑 1 次基线测试（按包缓存）。
- 触点：`internal/taskpipeline/mutation.go:RunMutationSampling`、`internal/taskpipeline/mutation.go:recordMutationRow`、
  `internal/taskpipeline/mutation.go:mutationVerdict`、`internal/clitask/task_questioning.go:runTaskMutation`。
- accept[REQ-1]: go test ./internal/taskpipeline/ -run TestMutationBaselineRed -count=1 :: ok

### R2 Warrant-Aware Acceptance（决策必须带可核验的理由）

- REQ-2：当 `forge skills decide --outcome accept` 没有带可核验锚点时，系统输出 WARN，并落一条 checklog
  `decision-warrant` 行（advisory，**首发不阻断**）。锚点按强弱分三类：
  `run:<id>`（经 `internal/skillseval/runs.go:LoadRunByID` 解析，且与 baseline 做了有效比对——baseline 可加载且不是锚点本身、有重叠 case、未缺 case、版本/模型/description 可比——并无回归）为强，比对做不成时降为弱并说明原因（2026-10-13 审查收紧，已随 R2 合入）；
  `commit:<sha>`（git 可解析）与 `doc:<repo 内路径>`（文件存在）为弱。
- REQ-3：当 accept 决策引用的 `run:` 锚点相对 baseline 有回归时，WARN 中列出回归项。
- 触点：`internal/cliskills/skills_decide.go:runSkillsDecide`、`internal/skillsdecisions/decisions.go:SkillDecision`；
  新增 checklog 名进 roster 并配 guard test；新增逃生 env 进 `compat.EscapeEnvs`。
- compat：新增 `--warrant` flag 属纯新增，援引 compat-commitments 2026-09-20 裁决；存量决策逐字节兼容。
- 升格条件见第六节。
- accept[REQ-2]: go test ./internal/cliskills/ -run TestSkillsDecideWarrant -count=1 :: ok
- accept[REQ-3]: go test ./internal/cliskills/ -run TestSkillsDecideWarrantRegression -count=1 :: ok

### R3 Track A 有效性前置（补齐设计欠账 + leakage + 高分复核）

- REQ-4：`RunBenchmark` 启动前依次检查：oracle 必须通过全部任务（否则说明环境坏了），do-nothing 必须得 0 分（否则说明判分定义有缺陷）。
  任一不满足就拒绝启动、exit 2。这是本周期唯一的 BLOCKED，与设计 §十"只阻断评测自身不合法"一致。
- REQ-5：leakage 基线（只读题面、不读代码的退化 runner）的得分写进 Scorecard，并与主分数并列渲染；
  得分最高的 N 个 attempt 列入"需复核"清单。
- 触点：`internal/evalkit/runner.go:RunBenchmark`、`internal/evalkit/runner.go:Scorecard`、`internal/evalkit/runner.go:TaskRunner`。
- accept[REQ-4]: go test ./internal/evalkit/ -run TestRunBenchmarkPreflight -count=1 :: ok
- accept[REQ-5]: go test ./internal/evalkit/ -run TestScorecardLeakage -count=1 :: ok

### R4 扰动臂（第三个方差源）

- REQ-6：当 RunSpec 声明 ≥3 个语义等价的 prompt 变体时，profile_delta 的 bootstrap CI 把 variant 作为第三个方差分量纳入；
  未声明时，Scorecard 标注 `perturbation=unmeasured`，`metrics.yaml` 的 misuse_note 写明"CI 可能偏窄"。
- REQ-7：null 结论必须同时给出 MDE："在 n=X 次运行下未检测到显著差异；最小可检测效应 Y pp"。
- 触点：`internal/evalkit/runner.go:RunSpec`、`internal/evalkit/stats.go`（新增函数）、`evals/forge/metrics.yaml`。
- accept[REQ-6]: go test ./internal/evalkit/ -run TestPerturbationVariance -count=1 :: ok
- accept[REQ-7]: go test ./internal/evalkit/ -run TestNullResultMDE -count=1 :: ok

### R5 评测集分层 + judge position-swap

- REQ-8：golden 用例增加分层标记（holdout / test）。test 层只在发版时运行，每次运行都给查询计数器 +1 并落审计行。
- REQ-9：judge-audit 新增 position-swap 审计：同一对候选交换顺序各判一次，翻转率 >10% 时判为 high systematic bias。
- 触点：`internal/evalkit/golden.go:GoldenCase`、`internal/evalkit/golden.go:RunGolden`、`internal/evalkit/judgeaudit.go:JudgeAuditEntry`、
  `internal/evalkit/judgeaudit.go:RunJudgeAudit`。
- accept[REQ-8]: go test ./internal/evalkit/ -run TestGoldenTestLayerQueryCount -count=1 :: ok
- accept[REQ-9]: go test ./internal/evalkit/ -run TestJudgePositionSwap -count=1 :: ok

### R6 "不可能"声明（本周期只出设计）

ImpossibleBench（arXiv 2510.20270）：在 Impossible-SWEbench 上给 agent 一个中止出口——发现任务有错时提交
`flag_for_human_intervention`，随即终止——GPT-5 的作弊率从 54% 降到 9%，o3 从 49% 降到 12%；
但原文指出对 Claude Opus 4.1 **效果弱得多**，而 Forge 的主力宿主正是 Claude。原基准上的性能损失较小。
设计稿要按 Claude 的实测效果评估收益，不能直接套用 GPT-5 的降幅。
这一项触及评分语义（`ScoreTask`），预计按 full 档处理。本周期只产出设计稿，要回答三个问题：
它和既有 `forge task question`（input-required 暂停）是什么关系；声明如何转为 `forge task report` 的未验证面披露行；
怎样防止它被当成逃生舱滥用。

## 五、成功度量（事前声明，复验时在同一表面实测）

| 度量 | 表面 | 基线（2026-10-13 实测） | 目标 | 复验时刻 |
|---|---|---|---|---|
| 新增 accept 决策中带可核验锚点（强或弱，`decision-warrant` 行 Passed=true）的比例 | 45 份 canonical decisions.md + checklog `decision-warrant` 行 | 0/271 | R2 合入 30 天后 ≥50% | R2 合入 +30 天 |
| 上项中强锚点的占比（仅观测，不设目标） | checklog `decision-warrant` 行 meta.verdict | 0 | 只记录分布与 meta.note 中的降弱原因 | 同上 |
| 决策日志的非 accept 比例 | 同上 | 0/271 | >0，并逐条核查 reject 是否真实 | 同上 |
| 预先置红的包上 mutation 打印"全部杀灭" | `forge task mutation` | 会打印（假绿） | 不再打印，verdict=`baseline-red` | R1 合入时 |
| Track A 启动是否校验 oracle / do-nothing | `RunBenchmark` | 不校验 | 违例即 exit 2 | R3 合入时 |
| 外部仓库数 | `task export --redact` 回传 | 0 | ≥3 | 2026-12-15 |
| 发版链救火提交 | git log | 23 天 9 个 | 冻结期 ≤2 | 2026-12-15 |

## 六、Deferred 承诺（触发条件 / 复验日期 / 消费方）

| 事项 | 触发条件 | 复验日期 | 到期处置 |
|---|---|---|---|
| R2 从 WARN 升为必填 | 合入 30 天后可核验锚点（强或弱）比例 ≥80%，且 WARN 误报经人工核查 ≤10%；升格须按 compat-commitments 第二节第 1 条①、第 2 条（≥2 minor 预告）与第 3 条（逃生舱） | 2026-12-15 | 满足则升格；不满足则带理由续期或放弃 |
| L4 数据飞轮导出 schema | W1 有 ≥1 个外部用户 | 2026-12-15 | 只定 schema 并钉进 compat，不做实现 |
| F7 国产宿主深度适配 / F5 分派前台 | 有外部用户点名要 | 2026-12-15 | 无需求则显式 dismiss |
| R6 实现 | R6 设计稿获批 | 2026-12-15 | — |
| 学习闭环重新评估 | 基座在 Forge 任务集上的组合准确率达标（2604.27707 下界失效的前提，见调研报告 5.3 节） | 2027-04-13 | 没有可测判据就不评估 |

消费方统一是周度回顾（session-retrospective）扫描 open deferred 项，口径同 `docs/design/evolution-discipline-norms.md` 第二节②。

## 七、备选对比

| 方案 | 收益 | 代价/风险 | 结论 |
|---|---|---|---|
| 什么都不做（继续按 feature-focus F1–F8 推进） | 零切换成本 | 271/271 的接受门与 mutation 假绿路径继续存在；仍无外部验证 | 否 |
| 只做 R 线 | 判定层变硬 | 零用户状态下，所有误报率和价值判断仍不可验证 | 否 |
| 只做 W 线 | 拿到外部信号 | 楔子"防假绿"本身就有一个已知假绿路径（R1），对外讲不硬 | 否 |
| **W + R（本方案）** | 楔子有硬货可讲，判定层有外部数据可校准 | 需要你本人投入运营时间 | **采纳** |
| 恢复学习闭环 | — | 外部实证同向反对，且 MINJA 注入成功率 98.2% 把一次性劫持升级为持久劫持 | 否 |

## 八、风险与未决问题

- **风险**：R2 的 WARN 噪声大，用户学会无视它。对策：第五节把误报率作为升格门槛，不达标不升。
- **风险**：W1 依赖运营动作，不是写代码就能完成。owner 必须是你本人，agent 只能准备演示与文档。
- **未决 Q1（阻断项，需你拍板）**：W3 的冻结规则与发版节奏。
- **未决 Q2**：R2 是新增 `--warrant`（锚点带类型，推荐），还是只复用 `--probe-run`（只能表达 run 锚点，覆盖不到审计类决策）？
- **未决 Q3**：R3 的 leakage runner 用什么实现形态（复用 `ExecRunner`、给定退化 prompt，还是脚本化）？在 R3 开工时的 mini 方案里定。
