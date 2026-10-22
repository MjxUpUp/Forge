---
status: implemented
owner: MjxUpUp
ask: 审协议文本改正与阻断正当性分类表的口径
tier: mini
---

# 修复方案 | 协议文本与代码对齐 + 阻断正当性单一真相表（2026-10）

## TL;DR
生成的项目级协议、forge-quality skill、hazard-guard 提示与 README 有 5 处与代码矛盾，agent 照文档做会失败。本批只改文本、注释与测试，让它们陈述代码的现行行为；另在 compat-commitments 第二节加一张阻断正当性分类表作为单一真相源。零新增阻断位点、命令与逃生 env。

## 问题与目标
来源：2026-10-22 全景盘点的内部冲突清单（高严重度 5 条；调研产物在本机 `~/.research-workflow/harness-landscape-20261022-0938/internal_conflicts.md`，未入库）。

| 文本说 | 代码实际 |
|---|---|
| task-verify Stop「只 advisory 不阻塞」 | `internal/hooks/embed_quality.go` 活跃任务 + 未提交变更时 exit 2，每会话 ≤3 次 |
| gate-cmd-form「自 1.58 起 BLOCKED + per-task override」 | `internal/hookdispatch/hook_gate_cmd_form.go` 的 `gateCmdFormBlockVersion = "1.67.0"`，只有 `FORGE_GATE_CMD_FORM=0` |
| test-coverage「自 1.58 起阈值 3→2」 | `internal/taskpipeline/testcoverage.go` 的 `testCoverageHardGateThreshold = 3`，预告挂了 25 个 minor |
| 让 agent 自行 `forge hazard confirm --last` | `internal/cli/hazard.go` 拒绝非真人终端 |
| init-suggest「自动 forge init」 | `internal/userconfig/userconfig.go` 的 `TakeoverAsk` 是出厂默认 |

另：四份设计文档对「何时可以 BLOCK」各有一套说法——`docs/design/compat-commitments.md` 第二节第 3 条（每个 BLOCKED 必须有逃生舱）、`docs/design/focus-batches.md:8`（BLOCKED 仅用于证据造假）、`docs/design/leverage-points-landing.md:5`（HARD 只守事实）、`docs/design/forge-evaluation-system.md:211`（BLOCKED 只出现在评测自身不合法）——新门禁提案无法判断自己能不能阻断。

## Non-goals
- 不改任何门禁行为：不兑现 test-coverage 3→2 收紧，不新增 override，不动 `hook_gate_cmd_form.go` 的版本门（另一个会话的死代码清扫任务正在独立 worktree 里改这处）。
- 不处理中、低严重度冲突（命令面记账、lite 档准入等）。
- 阻断正当性表列出的「已知例外」（task-verify Stop 逃生无审计行、bundle 验签无逃生舱等）只登记，不在本批修：修它们是行为变更。
- 不改历史依据行：`artifact-chain-workflow.md` 等文档引用旧总原则「HARD 只守事实」的依据行记录的是当时的口径，保持原样。

## 需求与验收
- REQ-1 生成的项目级协议陈述 task-verify Stop 有界阻断、gate-cmd-form 自 1.67 起 BLOCKED 且只有 env 逃生、init-suggest 默认 ask
  accept[REQ-1]: go test ./internal/skillgen/ -run ^TestClaudeMDSectionContract$ -count=1 :: --- PASS: TestClaudeMDSectionContract (
- REQ-2 hazard-guard 拦截提示、Stop 时的 HITL 提示、`forge hazard --help` 与 README 改为请用户在自己的终端运行 confirm，不再出现授权判定后 agent 自助 confirm 的路径
  accept[REQ-2]: go test ./internal/hooks/ -run ^TestHazardGuardScript_BlockGuidanceCopy$ -count=1 :: --- PASS: TestHazardGuardScript_BlockGuidanceCopy (
  accept[REQ-2]: go test ./internal/cli/ -run ^TestHazardCmd_LongGuidanceCopy$ -count=1 :: --- PASS: TestHazardCmd_LongGuidanceCopy (
  accept[REQ-2]: go test ./internal/cli/ -run ^TestHazardConfirm_ChainedRefused$ -count=1 :: --- PASS: TestHazardConfirm_ChainedRefused (
  accept[REQ-2]: go test ./internal/hooks/ -run ^TestHazardGuardScript_ConfirmChainedBlocks$ -count=1 :: --- PASS: TestHazardGuardScript_ConfirmChainedBlocks (
  accept[REQ-2]: grep -n 用户在自己的终端运行 README.md :: 用户在自己的终端运行
- REQ-3 forge-quality skill 的会话结束节与 README hook 表陈述 Stop 有界阻断（上限与逃生 env）。守卫测试名沿用历史的 `TestQualitySkillTaskVerifyIsAdvisory`，内容已改为钉住有界阻断
  accept[REQ-3]: go test ./internal/skillgen/ -run ^TestQualitySkillTaskVerifyIsAdvisory$ -count=1 :: --- PASS: TestQualitySkillTaskVerifyIsAdvisory (
  accept[REQ-3]: grep -n FORGE_TASK_VERIFY_STOP=0 README.md :: FORGE_TASK_VERIFY_STOP=0
- REQ-4 test-coverage advisory 文案只陈述现行规则，阈值取自 `testCoverageHardGateThreshold`
  accept[REQ-4]: go test ./internal/taskpipeline/ -run ^TestTestCoverageDetail_StatesCurrentBackstop$ -count=1 :: --- PASS: TestTestCoverageDetail_StatesCurrentBackstop (
- REQ-5 compat-commitments 第二节第 5 条给出阻断正当性分类与判别顺序，§三记录三条裁决
  accept[REQ-5]: grep -n 阻断正当性分类（单一真相源 docs/design/compat-commitments.md :: 阻断正当性分类（单一真相源
  accept[REQ-5]: grep -n 协议文本与代码对齐批 docs/design/compat-commitments.md :: 协议文本与代码对齐批
  accept[REQ-5]: grep -n 判别顺序 docs/design/compat-commitments.md :: 判别顺序
  accept[REQ-5]: grep -n 已知例外 docs/design/compat-commitments.md :: 已知例外
  accept[REQ-5]: grep -n PromoteAdvisory docs/design/compat-commitments.md :: PromoteAdvisory
  accept[REQ-5]: grep -n 真人化不设独立逃生舱 docs/design/compat-commitments.md :: 真人化不设独立逃生舱
- REQ-6 test-nudge tier3 提示的阈值取自导出常量 `taskpipeline.TestCoverageHardGateThreshold`，不再手抄数字
  accept[REQ-6]: go test ./internal/hookdispatch/ -run ^TestRunTestNudgeHook_Tier3GateConsequence$ -count=1 :: --- PASS: TestRunTestNudgeHook_Tier3GateConsequence (
- INV-1 skill 改动后 plugin 镜像与 compat 快照一致
  accept[INV-1]: go test ./internal/agentbridge/ -run ^TestPluginPack_CommittedSkillsMatchGenerator$ -count=1 :: --- PASS: TestPluginPack_CommittedSkillsMatchGenerator (
  accept[INV-1]: go test ./internal/cli/ -run ^TestCompatSnapshotMatchesGolden$ -count=1 :: --- PASS: TestCompatSnapshotMatchesGolden (

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。）
- `internal/hazard/stamp.go`
- `internal/hazard/stamp_test.go`
- `internal/hazard/event.go`
- `internal/cli/hazard_chain_test.go`
- `internal/hooks/hazard_f1_f2a_script_test.go`
- `internal/hooks/settings_test.go`
- `internal/e2e/datadir_hook_test.go`
- `internal/skillgen/claudemd.go`
- `internal/skillgen/claudemd_test.go`
- `internal/skillgen/qualitygen.go`
- `internal/skillgen/qualitygen_test.go`
- `internal/hooks/embed_guard.go`
- `internal/hooks/embed_quality.go`
- `internal/hooks/hazard_script_test.go`
- `internal/cli/hazard.go`
- `internal/cli/hazard_test.go`
- `internal/agentbridge/assets/plugin_readme.md`
- `plugins/forge/README.md`
- `homepage/index.md`
- `docs/plans/discipline-first-gates-2026-09.md`
- `internal/taskpipeline/testcoverage.go`
- `internal/taskpipeline/testcoverage_test.go`
- `internal/hookdispatch/hook.go`
- `internal/hookdispatch/hook_track.go`
- `internal/hookdispatch/hook_track_test.go`
- `internal/checklog/types.go`
- `skills/on-demand-guards/SKILL.md`
- `skills/on-demand-guards/decisions.md`
- `plugins/forge/skills/on-demand-guards/SKILL.md`
- `plugins/forge/skills/on-demand-guards/decisions.md`
- `internal/skillintegrate/notes/on-demand-guards.md`
- `README.md`
- `npm/README.md`
- `compat.snapshot.json`
- `docs/design/compat-commitments.md`
- `docs/design/focus-batches.md`
- `docs/design/leverage-points-landing.md`
- `docs/design/forge-evaluation-system.md`
- `docs/design/harness-fixes-a-g-2026-09.md`
- `docs/plans/protocol-text-code-drift-2026-10.md`

### 关键取舍
- 不做（维持现状）：否决。agent 按文本自行 hazard confirm 必然被拒，按 1.58 预告会误判测试阈值，按「不阻塞」会在 Stop 被拦时困惑。
- test-coverage 预告选择撤回而不是兑现：兑现是收紧门禁的行为变更，应按 compat 第二节重新走预告；本批只纠正文本。
- 守卫测试用字面值钉 1.67，不引用 `hookdispatch` 的未导出常量：该常量所在文件正被并行的死代码清扫任务改动，避免两边耦合。

## 验证与回滚
- `go vet ./...` 无输出。
- `go test ./... -count=1 -timeout 30m`：全部 `ok`。internal/cli 单包约 606 秒，超过 Go 默认的 10 分钟上限，本地全量须加 `-timeout`。
- 回滚即 revert 本分支的提交，不涉及数据面。
