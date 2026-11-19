# 改动：complete/report/list 只报事实，不再产出合成分

## 改动

1. 新增 `internal/tasktypes/evidence.go`：`TaskEvidence`（判据数、红先绿条数、评审轮次/分数、
   逃生舱次数、未测区域、剩余风险），JSON 键 `evidence`。
2. `internal/taskpipeline/evidence.go`：`BuildTaskEvidence(root, state)`——复用现有的未测区域/
   剩余风险口径（逃生激活时按无视逃生的配对口径现算），逃生舱从 `state.Overrides` 与 checklog
   的逃生条目计数，评审轮次/分数从 `state.ReviewRounds`/`ReviewScore` 取。
3. `internal/clitask/task_complete.go`：完成时不再调用 `ScoreTask`；打印一行事实摘要
   （判据 N、红先绿 M、评审 R 轮、逃生 E 次、未测 K 项），并提示 `forge task report` 看全量。
   `state.Score` 保留为只读兼容字段（老 state 里有，新流程不再写）。
4. `internal/clitask/task_report.go`：JSON 去掉 `score`/`grade`/`dim_scores` 等键，加 `evidence`；
   文本在「审查」行后打印事实清单。
5. `internal/clitask/task_list.go`：去掉等级列。
6. 测试：删/改对 grade 的断言；新增 `TestBuildTaskEvidence`、`TestTaskReport_EvidenceNotScore`、
   `TestTaskComplete_PrintsFactsNotGrade`。

## 影响

- 用户可见：complete/report/list 输出变化；JSON 消费者若读 `score` 需要改读 `evidence`。
- 门禁：无变化（从来没有任何门禁读分数）。
- 历史数据：老 state 的 `score` 字段仍可解析（结构体保留），新完成的任务不再写它。

## 验证

`go test ./internal/clitask/ ./internal/tasktypes/`、`go test ./internal/cli/ -run 'TestTask'`、
`gofmt-clean.sh`、`deadcode-zero.sh`，以及一次手工 complete（断言输出不含 Score）。