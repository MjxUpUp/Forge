---
status: implemented
owner: MjxUpUp
tier: mini
baseline: 4373f278
---

# 技术方案 | 死代码第二轮清扫（无需 compat 预告档，2026-10）

## TL;DR
只清不需要 compat 预告的一档：删掉 2026-09 清扫删命令时漏下的库代码（`skills mine` / `skills analyze` 的实现与测试）、收敛 `health.Summarize`、删 7 组生产不可达函数、把两处已恒为真的版本门改成"1.x 发布版即阻断"、把两个退役 skill 的决策存档并入 `research-workflow` 后删目录。需要预告的项（`act remigrate`、MCP 清理垫片、init no-op flags、`PhasePassRate` 等）不在本批。

依据：`~/.research-workflow/harness-landscape-20261022-0938/internal_deprecated.md` 第二节 2.3 A/B、第一节第 11、13 项、第五节首行，以及末尾"建议立即删"一档。

## Non-goals
- 不动 2.3 C 档已注释为有意保留的不可达函数（hlc 三个、`LatestByCheckForSession*`、`docsconsistency.AllFlags/DanglingSkillRefs`、测试观测点与 `forgedatatest`）。
- 不删任何 CLI 命令、flag、JSON 键或 checklog 检查名。
- 不改 `internal/skillgen/claudemd.go`（协议文本冲突修复另有任务）。

## 需求与验收
- REQ-1 `internal/skillseval/mine.go`、`internal/skillmetrics/weakness.go` 及其测试删除，仓内无残留引用，相关包编译与 vet 通过。
  accept[REQ-1]: go vet ./internal/skillseval/ ./internal/skillmetrics/ ./internal/health/ ./internal/cli/
- REQ-2 `health.Summarize` 删除；`forge health` 与 `forge status` 改调 `health.SummarizeAt(cs, time.Now())`。行为变化：两者 JSON 里的 `nudge_recent` / `nudge_actionable` 从全量计数改为 14 天窗口计数，与 pulse 面板同一窗口（pulse 另扣已 ack 的 nudge，health/status 不扣）；`nudge_count` 仍是全量；其余字段不变。
  accept[REQ-2]: go test ./internal/health/ -run ^TestSummarizeAt_NudgeRecentWindow$ -count=1 :: --- PASS: TestSummarizeAt_NudgeRecentWindow (
  accept[REQ-2]: go test ./internal/cli/ -run ^TestHealthJSON_NudgeWindowed$ -count=1 :: --- PASS: TestHealthJSON_NudgeWindowed (
- REQ-3 以下生产不可达函数删除，测试改调本体或内联：`checklog.EscapeOwnerOf`、`evalkit.LoadCard`、`projectsync.sha256Hex`、`protocol.WordSeverityLabel` / `MustShouldLabel` / `AlwaysPreferLabel`、`skillsdist.hashTree`、`taskpipeline.VerifyAcceptance`、`taskpipeline.VerifyArtifact`。仓内 gates-card 资产校验、渲染格式钉子、产物漂移探测、验收实跑的测试覆盖保留。
  accept[REQ-3]: go test ./internal/evalkit/ -run ^TestRepoGatesCardAsset$ -count=1 :: --- PASS: TestRepoGatesCardAsset (
  accept[REQ-3]: go test ./internal/protocol/ -run ^TestRenderSessionRulesPinsLines$ -count=1 :: --- PASS: TestRenderSessionRulesPinsLines (
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestWriteArtifact_ReferenceTriangle$ -count=1 :: --- PASS: TestWriteArtifact_ReferenceTriangle (
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestVerifyAcceptance_RunsAndClassifies$ -count=1 :: --- PASS: TestVerifyAcceptance_RunsAndClassifies (
  accept[REQ-3]: go test ./internal/checklog/ -run ^TestEscapeHatchEntryRoundTrip$ -count=1 :: --- PASS: TestEscapeHatchEntryRoundTrip (
  accept[REQ-3]: go test ./internal/projectsync/ -run ^TestUnpack_RejectsTamperedContent$ -count=1 :: --- PASS: TestUnpack_RejectsTamperedContent (
- REQ-4 gate-cmd-form（原门 1.67.0）与 task-drift（原门 1.66.0）的版本门改为同一个判定：版本串剥离构建后缀后以 `1.` 开头即阻断。现存 1.x 发布版都高于原门，判定结果不变；`dev`、测试用的非版本串、2.x 仍回落 advisory。task-drift 顺带获得构建后缀剥离（发布构建的版本串形如 `1.83.0 (commit: …, built: …)`）。逃生 env 与会话阻断上限不变。
  accept[REQ-4]: go test ./internal/hookdispatch/ -run ^TestRatchetBlocksVersion$ -count=1 :: --- PASS: TestRatchetBlocksVersion (
  accept[REQ-4]: go test ./internal/hookdispatch/ -run ^TestTaskDriftBlockRatchet$ -count=1 :: --- PASS: TestTaskDriftBlockRatchet (
  accept[REQ-4]: go test ./internal/hookdispatch/ -run ^TestGateCmdFormHook_BlockRatchet$ -count=1 :: --- PASS: TestGateCmdFormHook_BlockRatchet (
  accept[REQ-4]: go test ./internal/hookdispatch/ -run ^TestGateCmdFormHook_AdvisoryAndSilence$ -count=1 :: --- PASS: TestGateCmdFormHook_AdvisoryAndSilence (
- REQ-5 `skills/fact-research/decisions.md`、`skills/web-search-bridge/decisions.md` 追加到 `skills/research-workflow/decisions.md` 末尾的"历史来源"节，标题各降一级（`## [d-` → `### [d-`），正文逐字保留，避免被解析成 research-workflow 自己的决策；两个目录删除；`skills/CONVENTIONS.md` 的退役存档约定改为"并入承接 skill 后删目录"；r18 棘轮测试对无 SKILL.md 目录的跳过改为报错。
  accept[REQ-5]: go test ./internal/skillsqa/ -run ^TestR18_Grandfathered_Exact$ -count=1 :: --- PASS: TestR18_Grandfathered_Exact (
- REQ-6 「改 SKILL.md 须记决策」门禁的条目计数（`internal/taskpipeline/executor_skill_decisions.go:countDecisionEntries`）改为只认行首 `## [d-`，与 `skillsdecisions.parseDecisions` 同口径；降级迁入的 `### [d-` 历史条目与行内标记不再被算作新记决策（独立审查发现：原子串计数会让 REQ-5 的迁入方式绕过门禁）。
  accept[REQ-6]: go test ./internal/taskpipeline/ -run ^TestCountDecisionEntries$ -count=1 :: --- PASS: TestCountDecisionEntries (
  accept[REQ-6]: go test ./internal/taskpipeline/ -run ^TestSkillDecisionsRecorded$ -count=1 :: --- PASS: TestSkillDecisionsRecorded (

完成判据（人工核对，不进 accept）：`deadcode ./...` 只剩 2.3 C 档已注释保留的函数；`go vet ./...`、`go test ./...` 全过。

## 设计
### 触点
- `internal/skillseval/mine.go` 删除（连同 `mine_test.go`）
- `internal/skillmetrics/weakness.go` 删除（连同 `weakness_test.go`）
- `internal/health/summary.go` 删 `Summarize`，`SummarizeAt` 注释去掉对它的引用
- `internal/health/summary_test.go` 用例改调 `SummarizeAt`
- `internal/cli/health.go` 改调 `SummarizeAt(cs, time.Now())`
- `internal/cli/status.go` 同上
- `internal/cli/health_test.go` 新增 `TestHealthJSON_NudgeWindowed`（端到端钉住窗口口径）
- `internal/checklog/escape.go` 删 `EscapeOwnerOf`
- `internal/checklog/escape_test.go` 直接断言 `Meta["escape.owner"]`
- `internal/evalkit/card.go` 删 `LoadCard`
- `internal/evalkit/card_test.go` 资产测试内联读取 + 解析 + 校验，改名 `TestRepoGatesCardAsset`
- `internal/projectsync/manifest.go` 删 `sha256Hex`
- `internal/projectsync/sync_test.go` 内联哈希
- `internal/protocol/render.go` 删三个 label 函数，更新"五个 host 渲染器"的过期注释
- `internal/protocol/render_test.go` 用内联 label 函数保留格式钉子
- `internal/protocol/loader.go` 注释里的 `WordSeverityLabel` 引用改掉
- `internal/skillsdist/install.go` 删 `hashTree`
- `internal/skillsdist/install_test.go` 改调 `hashTreeWithSkippedDirs`
- `internal/taskpipeline/acceptance.go` 删 `VerifyAcceptance`，合并注释到 `VerifyAcceptanceWithVerdicts`
- `internal/taskpipeline/acceptance_test.go` 改调 `VerifyAcceptanceWithVerdicts`
- `internal/taskpipeline/acceptance_freshness_test.go` 同上
- `internal/taskpipeline/assertion_judge.go` 注释引用更新
- `internal/taskpipeline/heldout.go` 注释引用更新
- `internal/taskpipeline/heldout_test.go` 注释引用更新
- `internal/taskpipeline/specs.go` 删 `VerifyArtifact`
- `internal/taskpipeline/specs_test.go` 改用 `ArtifactCurrentHash` 比对
- `internal/tasktypes/types.go` 注释引用改为 `ArtifactCurrentHash`
- `internal/checklog/types.go` 注释引用同上
- `internal/hookdispatch/hook_task_drift.go` 删 `taskDriftBlockVersion`，新增共用判定 `ratchetBlocksVersion`
- `internal/hookdispatch/hook_task_drift_test.go` 版本用例改为 dev / 非版本串 / 带构建后缀的发布串，新增 `TestRatchetBlocksVersion`
- `internal/hookdispatch/hook_gate_cmd_form.go` 删 `gateCmdFormBlockVersion`，改调共用判定
- `internal/hookdispatch/hook_gate_cmd_form_test.go` advisory 用例改用 dev 版本
- `skills/research-workflow/decisions.md` 追加历史来源节
- `plugins/forge/skills/research-workflow/decisions.md` 由 `forge plugin pack` 重新生成
- `skills/CONVENTIONS.md` 退役存档约定
- `internal/skillsqa/r18_ratchet_test.go` 去掉无 SKILL.md 目录的跳过
- 删除 `skills/fact-research/`、`skills/web-search-bridge/`
- `internal/taskpipeline/executor_skill_decisions.go` 计数改行首匹配（REQ-6）
- `internal/taskpipeline/executor_skill_decisions_test.go` 新增 `TestCountDecisionEntries`，`TestSkillDecisionsRecorded` 加迁入用例
- 审查发现的过期注释同步：`internal/tasktypes/types.go`、`internal/clitask/task_gate.go`、`internal/clitask/task_oracle_test.go`、`internal/clitask/task_acceptance_test.go`（`VerifyAcceptance` 改名）；`internal/skillmetrics/funnel.go`、`internal/skillmetrics/usage.go`、`internal/skillmetrics/direction_test.go`、`internal/util/redact.go`（mine.go / weakness 已删）；`internal/cliskills/skills_inventory.go`（退役存档目录已不存在）

### 风险
- REQ-2 改变 `health --json` / `status --json` 两个计数的口径。键不增不删，`nudge_count` 保留全量真相；仓内没有读这两个键做判定的消费方（`grep nudge_recent` 只命中 health 包与 pulse），按"语义推导规则可随版本演进"处理，在 commit 信息与 PR 描述里写明。
- REQ-4 里低于原门的 1.x 版本串（如 1.65.0）现在也判阻断。这些版本的二进制自带旧代码，不会跑到这段逻辑，只影响测试用例，测试同步改掉。
