---
status: implemented
owner: MjxUpUp
ask: 已合入 4373f278（#127）。原评审请求：审方向与验收；R3 新增一个 BLOCKED 位点（评测自身不合法），按 compat-commitments 第二节②首发即 blocked
tier: mini
---

# 技术方案 | 评测有效性前置：R3 preflight + leakage、R4 扰动臂 + MDE、R5 分层 + position-swap（2026-10）

## TL;DR
落地周期总纲 `docs/plans/acceptance-gate-and-wedge-2026-10-proposal.md` 的 R3、R4、R5（R1、R2 已合入）。三项都在 `internal/evalkit`，合成一批：
Track A 启动前先跑 oracle / do-nothing 两条基线，评测自身不合法就拒绝启动（exit 2）；leakage 基线与主分数并列，高分任务进复核清单；
profile_delta 有了真正的 bootstrap CI，声明 ≥3 个 prompt 变体时把变体纳入第三个方差源，null 结论必带 MDE；
golden 用例分 holdout / test 两层，test 层只在 `--release` 时跑并计数；judge-audit 新增成对位置交换审计。命令面净增 0。

## 问题与现状证据（基线 `517f6243`）
- `internal/evalkit/runner.go:RunBenchmark` 没有任何 oracle / do-nothing / leakage 逻辑（`grep -n "oracle\|DoNothing\|leakage" internal/evalkit/runner.go` 0 命中）。设计 `docs/design/forge-evaluation-system.md` §六 P2 把它们列为 run 启动前置，§十把"oracle 不可解、do-nothing 击穿判分"列为合法 BLOCKED 之一（§十共列六类，都属"评测自身不合法"）。
- 现有 manifest `evals/forge/manifests/tb-frozen-example.yaml` 的判分命令 `test -f /etc/os-release` 在什么都不做时也通过——正是 do-nothing 要抓的判分缺陷。
- `profile_delta` 在 `evals/forge/metrics.yaml` 里声明"配对差 + bootstrap CI"，但 `internal/evalkit/stats.go` 没有 bootstrap 函数，`internal/evalkit/decompose.go:RenderDecomposeMarkdown` 只渲染点差，并写着"是否显著以置信区间为准"——区间从未算过。
- `internal/evalkit/golden.go:GoldenCase` 无分层字段，所有用例每次 `golden run` 都跑；`internal/evalkit/judgeaudit.go` 的 position bias 只比均分差（|Δ|≥5），不比成对判定是否翻转。

## Non-goals
- 不做模型驱动的 runner。leakage 臂由 manifest 声明的 `leakage_cmd` 实现（总纲 Q3 在此拍板为脚本化，对比见下方"备选"），"只读题面"由 manifest 作者保证，forge 只负责跑、计分、披露。
- 不给 command-suite 任务（只有 `command`、没有独立判分命令）强加 oracle / do-nothing：它们的命令既是解法又是判分，基线无定义，scorecard 如实标 `preflight=n/a`。
- 不改发版流水线（W3 冻结 infra）：`golden run --release` 只提供入口，不接进 release.yml。
- 不迁移现有 golden 用例到 test 层：本批只交付机制，首批 test 层用例随下次 `golden harvest` 补（见 Deferred）。
- 不改 `RunBenchmark` / `RunGolden` / `RunJudgeAudit` 的函数签名。

## 需求与验收
- REQ-4 当 Track A runner 支持基线（`BaselineRunner`）且任务带独立判分命令（`test_cmd`）时，`RunBenchmark` 先跑基线：oracle 臂（`oracle_cmd && test_cmd`）任一任务不通过、或判分任务未声明 `oracle_cmd`、或 do-nothing 臂（只跑 `test_cmd`）任一任务通过，即返回 `*PreflightError`，不跑主评测；CLI 打印 `BLOCKED:` 并 exit 2，落 checklog `eval-preflight`（Passed=false）。runner 不支持基线或任务无 `test_cmd` 时不阻断，Scorecard `preflight.status=n/a` 并进披露清单。`FORGE_EVAL_PREFLIGHT=skip` 跳过 preflight：落 escape-hatch 行，Scorecard `preflight.status=skipped`、Note 写明证据封顶 Weak。
  accept[REQ-4, INV-1, NFR-1]: go test ./internal/evalkit/ -run ^TestRunBenchmarkPreflight$ -count=1 :: --- PASS: TestRunBenchmarkPreflight (
- REQ-5 当任务声明 `leakage_cmd` 时，preflight 额外跑 leakage 臂（`leakage_cmd && test_cmd`），其通过率写进 `Scorecard.Leakage` 并在 `eval run` 输出里紧跟 pass@1 渲染；无声明时 `leakage.status=unmeasured`。Scorecard 带 `review_queue`：leakage 臂通过的任务全部入列（原因 `leakage-pass`），再按任务通过率降序、token 升序补足到 5 条（原因 `top-score`）。
  accept[REQ-5]: go test ./internal/evalkit/ -run ^TestScorecardLeakage$ -count=1 :: --- PASS: TestScorecardLeakage (
- REQ-6 RunSpec 声明了 prompt 变体（`--variants`）时，`RunBenchmark` 按变体 × 重复执行；≥3 个时 `PairedDeltaCI` 的分层 bootstrap 依次重采任务、变体（两臂共用同一组变体抽样——变体是两个 profile 共享的）、重复三层；变体 <3 时只重采任务与重复两层，Scorecard 与 decompose 报告标 `perturbation=unmeasured`，`metrics.yaml` 的 profile_delta misuse_note 写明"CI 可能偏窄"。
  accept[REQ-6]: go test ./internal/evalkit/ -run ^TestPerturbationVariance$ -count=1 :: --- PASS: TestPerturbationVariance (
- REQ-7 当配对差的 95% CI 跨零时，结论文本固定为"在 n=X 次运行下未检测到显著差异；最小可检测效应 Y pp"（Y = 2.8 × bootstrap SE × 100，对应 α=0.05 双侧、power 0.8；n 只数两臂共同任务上的 attempt）；不跨零时给出差值与区间；bootstrap 方差为 0 时不给 MDE，改为提示按设计 §五作计算异常复核。decompose 报告三档差值全部改用该文本。
  accept[REQ-7]: go test ./internal/evalkit/ -run ^TestNullResultMDE$ -count=1 :: --- PASS: TestNullResultMDE (
- REQ-8 golden 用例新增 `layer: holdout|test`（缺省 = holdout）。`golden run` 默认不跑 test 层；`--release` 才跑，且每次带 test 层的报告落盘都给 `<evalDir>/forge/golden-test-queries.json` 的计数 +1，并落 checklog `eval-golden-test-query` 行（带当次计数与 test 层用例数）。
  accept[REQ-8, INV-2]: go test ./internal/evalkit/ -run ^TestGoldenTestLayerQueryCount$ -count=1 :: --- PASS: TestGoldenTestLayerQueryCount (
- REQ-9 judge-audit 分数文件可选 `pair_original` / `pair_swapped`（取值 `a|b|tie`，按候选身份记录胜者，两者须同时出现）。两次判定不一致即一次翻转；翻转率 >10% 时报告 `position_swap_bias=high` 并出 finding "high systematic bias"；未提供时翻转率记 -1（未运行）。
  accept[REQ-9]: go test ./internal/evalkit/ -run ^TestJudgePositionSwap$ -count=1 :: --- PASS: TestJudgePositionSwap (
- INV-1 只有 `command` 的 manifest（如 `evals/forge/manifests/smoke-v1.yaml`）与未实现 `BaselineRunner` 的 runner，`RunBenchmark` 行为与基线一致（不阻断、pass@1 不变），只多 `preflight.status=n/a`。由 REQ-4 的测试断言。
- INV-2 无 `layer` 字段的用例集指纹与基线逐字节一致（已钉的 `evals/forge/golden/MANIFEST.sha256` 不失效）。由 REQ-8 的测试断言。
- NFR-1 preflight 每个判分任务至多多跑 3 次（oracle、do-nothing、声明了 leakage 时再加 1），与 repeats 无关。`eval decompose` 整张网格只跑一次 preflight、各格复用，口径不变。由 REQ-4 的测试按调用计数断言（含 decompose 子测试）。
- 守卫：新 checklog 名进 roster、新逃生 env 进 `compat.EscapeEnvs`、新 flag 进 README，相关守卫测试全绿。
  accept: go test ./internal/compat/ ./internal/checklog/ ./internal/cli/ -count=1

## 设计
### 触点
- `internal/evalkit/runner.go`：`BenchTask` 加 `oracle_cmd` / `leakage_cmd`；`RunSpec` 加 `variants`（yaml/json）与当前变体 `Variant`（`json:"-"`）；`Attempt` 加 `variant`；`Scorecard` 加 `attempts`、`preflight`、`leakage`、`review_queue`、`perturbation`；`RunBenchmark` 在主循环前调 preflight、按变体 × 重复执行；`ExecRunner` / `DockerRunner` 把当前变体经 `FORGE_EVAL_VARIANT` 交给命令；`DockerRunner` 实现 `BaselineRunner`。
- new: `internal/evalkit/preflight.go`：`BaselineArm`、`BaselineRunner`、`PreflightReport`、`PreflightError`、`runPreflight`、`buildReviewQueue`、`RecordPreflightBlock`、`PreflightDisableEnv = "FORGE_EVAL_PREFLIGHT"`。
- `internal/evalkit/stats.go`：new `PairedDeltaCI`、`DeltaCI`、`NullResultStatement`（纯函数，固定种子）。
- `internal/evalkit/decompose.go`：preflight 整张网格只跑一次并进报告；格结果保留 attempts，`ModelDelta` 加 `ci`，格均值与 `delta` 统一用全部 attempt 的通过率（方差分解与差值同口径），有 CI 时 `delta` 取 CI 的点估计，渲染改用 `NullResultStatement`，报告标 perturbation。
- `internal/evalkit/golden.go`：`GoldenCase.Layer`、校验、指纹（仅非空时追加 `layer:`）；`GoldenOptions.IncludeTestLayer`；`GoldenReport` 加 `test_layer_cases` / `test_layer_excluded`；`PersistGoldenReport` 在含 test 层时调 new `recordTestLayerQuery`。
- `internal/evalkit/judgeaudit.go`：`JudgeAuditEntry` 加 `pair_original` / `pair_swapped`；`JudgeAuditReport` 加 `position_swap_flip_rate` / `position_swap_pairs` / `position_swap_bias`；`LoadJudgeScores` 校验取值与成对出现。
- `internal/cli/eval.go`：`eval run` 与 `eval decompose` 加 `--variants`，两者都经 `evalRunError` 把 `*PreflightError` 映射为 exit 2（`hardExitError` 哨兵）、经 `notePreflightEscape` 处理逃生 env；`eval run` 渲染 leakage / 复核清单；`golden run` 加 `--release`；judge-audit 渲染翻转率。
- `internal/checklog/types.go` + `escape.go`：new `eval-preflight`、`eval-golden-test-query` 进 roster；`internal/dashboard/feed.go` 的 eval 投影同步两名。
- `internal/compat/compat.go` `EscapeEnvs` + `compat_test.go` `rosterExtraSources` 登记 `FORGE_EVAL_PREFLIGHT`；`compat.snapshot.json` 重钉。
- `evals/forge/metrics.yaml` profile_delta 定义与 misuse_note；`evals/forge/manifests/tb-frozen-example.yaml` 改成判分有效的示例（补 `oracle_cmd`，判分改为检查 run 的产物）。
- `docs/design/compat-commitments.md` 第三节加本批裁决记录；`docs/design/forge-evaluation-system.md` §五第 2 条改写为已实现的 CI 口径；`README.md` 命令参考补三个 flag。

### 接口/契约
```go
type BaselineArm string // oracle | do-nothing | leakage
type BaselineRunner interface {
    // applicable=false：该任务在本后端无此基线（无 test_cmd / 未声明 leakage_cmd）。
    RunBaseline(ctx context.Context, spec RunSpec, task BenchTask, arm BaselineArm) (res TaskResult, applicable bool, err error)
}
type PreflightReport struct {
    Status          string   `json:"status"` // passed | n/a | skipped；违例不设状态值，直接返回 PreflightError
    Checked         int      `json:"checked"`
    OracleFailed    []string `json:"oracle_failed,omitempty"`
    OracleMissing   []string `json:"oracle_missing,omitempty"`
    DoNothingPassed []string `json:"do_nothing_passed,omitempty"`
}
type DeltaCI struct {
    Delta, Lo, Hi, SE, MDE float64
    NTasks, NRuns          int
    Perturbation           string // measured | unmeasured
    CrossesZero            bool
}
func PairedDeltaCI(a, b []Attempt, iters int, seed int64) (*DeltaCI, error)
func NullResultStatement(ci *DeltaCI) string
```
- 退出码契约不变：preflight 违例 = BLOCKED = 2。拒绝文案带"首发即 blocked，见 docs/design/compat-commitments.md"。
- 数据面只增键：Scorecard / GoldenReport / JudgeAuditReport / DecomposeReport JSON 只追加字段；无新字段的 golden 用例、judge 分数文件、manifest 照旧可读。

### 备选（总纲 Q3：leakage 臂的实现形态）
| 方案 | 代价 | 结论 |
|---|---|---|
| 复用 `ExecRunner`，给被测模型一个只含题面的退化 prompt | 需要模型驱动后端，而当前 runner 只有命令套件与容器两种后端，等于本批先造模型后端 | 否：超出本批范围，列入 Deferred |
| 脚本化：manifest 按任务声明 `leakage_cmd`，由同一后端执行后判分 | "不读代码"靠 manifest 作者自律，forge 不强制隔离 | 采纳：零新后端，与 oracle 臂同一执行路径，限制写进披露 |
| 什么都不做（只做 oracle / do-nothing） | 高分是否来自题面泄漏无从知道，复核清单只能按高分排 | 否：总纲 R3 要求 leakage 与主分数并列 |

### 失败模式
- 基线执行本身报错（docker 起不来）：按执行错误上抛（与主循环一致），不伪装成违例。
- oracle 或 do-nothing 臂超出墙钟预算：无法判定，按执行错误上抛（exit 1，提示调大 `--wallclock`），不计为违例；leakage 臂超时不进分母，计入 `leakage.timed_out` 并在输出里说明。
- bootstrap 输入为空或两臂无共同任务：返回错误，decompose 渲染退回"区间不可得"并说明原因，不出点差结论。
- 计数文件损坏：按 0 起算并在 checklog 行 Detail 注明"计数文件不可读，已重置"，不阻断 golden run。

## 测试计划
全部离线、表驱动、同目录。preflight 用桩 runner（实现 `BaselineRunner`，按任务预设各臂结果并计调用次数）；bootstrap 用构造数据：变体效应大、重复内无方差的格子，三源 CI 宽度必须明显大于两源；MDE 文本用固定 SE 校验数字；golden 分层用既有假 forge 二进制范式（`writeFakeForge`）；judge 翻转率用 10 对里 2 对翻转（0.2 > 0.1）与 1 对翻转（0.1 不触发）两档边界。

## Deferred
| 事项 | 触发条件 | 复验日期 | 消费方 |
|---|---|---|---|
| 首批 golden test 层用例 | 下次 `forge eval golden harvest` 产出新用例 | 2026-12-15 | 周度回顾 |
| `golden run --release` 接入 release.yml | W3 infra 冻结解除 | 2026-12-15 | 周度回顾 |
| 模型驱动 leakage runner（替代脚本化 `leakage_cmd`） | 第一个模型驱动 Track A 后端合入 | 2027-01-15 | R6 设计评审 |
