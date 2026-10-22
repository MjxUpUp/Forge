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
来源：2026-10-22 全景盘点的内部冲突清单（高严重度 5 条）。

| 文本说 | 代码实际 |
|---|---|
| task-verify Stop「只 advisory 不阻塞」 | `internal/hooks/embed_quality.go` 活跃任务 + 未提交变更时 exit 2，每会话 ≤3 次 |
| gate-cmd-form「自 1.58 起 BLOCKED + per-task override」 | `gateCmdFormBlockVersion = "1.67.0"`，只有 `FORGE_GATE_CMD_FORM=0` |
| test-coverage「自 1.58 起阈值 3→2」 | `testCoverageHardGateThreshold = 3`，预告挂了 25 个 minor |
| 让 agent 自行 `forge hazard confirm --last` | `internal/cli/hazard.go` 拒绝非真人终端 |
| init-suggest「自动 forge init」 | 出厂默认 ask |

另：四份设计文档对「何时可以 BLOCK」各有一套说法，新门禁提案无法判断自己能不能阻断。

## Non-goals
- 不改任何门禁行为：不兑现 test-coverage 3→2 收紧，不新增 override，不动 `hook_gate_cmd_form.go` 的版本门（另一会话的死代码清扫在处理）。
- 不处理中、低严重度冲突（命令面记账、lite 档准入等）。

## 需求与验收
- REQ-1 生成的项目级协议陈述 task-verify Stop 有界阻断、gate-cmd-form 自 1.67 起 BLOCKED 且只有 env 逃生、init-suggest 默认 ask
  accept[REQ-1]: go test ./internal/skillgen/ -run ^TestClaudeMDSectionContract$ -count=1 :: --- PASS: TestClaudeMDSectionContract (
- REQ-2 hazard-guard 拦截提示改为请用户在自己的终端运行 confirm，不再出现授权判定后 agent 自助 confirm 的路径
  accept[REQ-2]: go test ./internal/hooks/ -run ^TestHazardGuardScript_BlockGuidanceCopy$ -count=1 :: --- PASS: TestHazardGuardScript_BlockGuidanceCopy (
- REQ-3 forge-quality skill 的会话结束节陈述 Stop 有界阻断（上限与逃生 env）
  accept[REQ-3]: go test ./internal/skillgen/ -run ^TestQualitySkillTaskVerifyIsAdvisory$ -count=1 :: --- PASS: TestQualitySkillTaskVerifyIsAdvisory (
- REQ-4 test-coverage advisory 文案只陈述现行规则，阈值取自 `testCoverageHardGateThreshold`
  accept[REQ-4]: go test ./internal/taskpipeline/ -run ^TestTestCoverageDetail_StatesCurrentBackstop$ -count=1 :: --- PASS: TestTestCoverageDetail_StatesCurrentBackstop (
- REQ-5 compat-commitments 第二节第 5 条给出阻断正当性分类，§三记录三条裁决
  accept[REQ-5]: grep -n 阻断正当性分类（单一真相源 docs/design/compat-commitments.md :: 阻断正当性分类（单一真相源
- INV-1 skill 改动后 plugin 镜像与 compat 快照一致
  accept[INV-1]: go test ./internal/agentbridge/ -run ^TestPluginPack_CommittedSkillsMatchGenerator$ -count=1 :: --- PASS: TestPluginPack_CommittedSkillsMatchGenerator (
  accept[INV-1]: go test ./internal/cli/ -run ^TestCompatSnapshotMatchesGolden$ -count=1 :: --- PASS: TestCompatSnapshotMatchesGolden (

## 设计
### 触点
- `internal/skillgen/claudemd.go`
- `internal/skillgen/claudemd_test.go`
- `internal/skillgen/qualitygen.go`
- `internal/skillgen/qualitygen_test.go`
- `internal/hooks/embed_guard.go`
- `internal/hooks/hazard_script_test.go`
- `internal/taskpipeline/testcoverage.go`
- `internal/taskpipeline/testcoverage_test.go`
- `internal/hookdispatch/hook.go`
- `internal/hookdispatch/hook_track.go`
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
- test-coverage 预告选择撤回而不是兑现：兑现是收紧门禁的行为变更，应按 compat 第二节重新走预告；本批只纠正文本。
- 守卫测试用字面值钉 1.67，不引用 `hookdispatch` 的未导出常量：该常量所在文件正被另一会话改动，避免两边耦合。

## 验证与回滚
全量 `go vet ./...`、`go test ./...` 通过。回滚即 revert 本提交，不涉及数据面。
