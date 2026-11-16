# design

## TL;DR

gate 与 complete 共享 `CheckDocGate`；交付单增加文档回检行；change 契约下产物链提醒改口径；空期望在声明期提示并写进文档。

## 1. gate/complete 同口径

`internal/taskpipeline/executor.go` 的 `GateComplete` 分支在 review prereqs 之后调用 `CheckDocGate(root, state)`，不通即 `GateBlocked`（文案指向 doc-review 流程与逃生舱）。complete 侧已有同一检查，故两侧共享同一真相源，不再出现「gate 绿、complete 红」。

## 2. 交付单披露文档回检

`taskReportJSON` 增加 `DocReviewLine`；从 `state.DocReview` 渲染「L2 score N（round n，reviewer）」；`SelfReview` 或未记录评审者时追加「自查（独立性未校验）」。`renderTaskReport` 在「审查」行下输出。

## 3. 产物链提醒尊重 change 契约

抽出 `artifactAdvisoryDetail(state, missing, hint)`：已钉 change（`GoalContract != nil && Change != ""`）时输出「已钉 change 目标契约（X）——task 级产物链可省略，以 change 产物为准」，否则（无契约，或 `--goal-file`/`--goal` 钉了 goal 但没有 change id）保留原文案。

## 4. 空期望提示与文档

`task start` 统计无期望且无断言的验收标准，打印一次「只校验退出码；期望是子串匹配」；cli.md 与 agents-protocol 写明 `strings.Contains` 语义与空期望行为。

## 影响面

- 代码：`internal/taskpipeline/executor.go`、`artifactchain_gate.go`、`internal/clitask/task.go`、`task_report.go`。
- 测试：`internal/taskpipeline/complete_docgate_parity_test.go`、`artifact_advisory_test.go`、`internal/clitask/task_report_docreview_test.go`。
- 文档：`docs/reference/cli.md`、`internal/change/templates/agents-protocol.md`（随之同步 AGENTS.md）。

## 风险

gate 提前拦截会让「文档改动但还没评审」的任务更早停下——这是有意的；误伤面仅限确实改了 markdown 的任务。

## 8. 不做（Non-goals）

- 不强制校验评审者独立性（宿主无法证明来源），只做披露。
- 不改评分模型、evidence 强度规则与 acceptance 语法。
- 不改 `--accept` 的匹配算法（子串匹配是既有语义）。

## 9. 验证（Verification）

- `internal/taskpipeline/complete_docgate_parity_test.go`：有/无 markdown 变更两条路径。
- `internal/taskpipeline/artifact_advisory_test.go`：契约与无契约两种文案。
- `internal/clitask/task_report_docreview_test.go`：三种披露形态（正常/无评审者/自查）。
- `internal/cli/task_start_hint_test.go`：空期望提示与不提示两条路径。
- 全量：`go build ./...`、`go test ./internal/taskpipeline/ ./internal/clitask/ ./internal/cli/`、gofmt-clean、deadcode-zero、docs lint。
