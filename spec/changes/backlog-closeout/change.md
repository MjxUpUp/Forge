# change：backlog-closeout

## 改动

1. **红快照绑开工时刻**（`internal/clitask/task_redfirst.go`）：红记录的 `Head` 必须等于任务开工时的 `HeadCommit`；`commitsOlderThan` 发现开工后出现早于 `StartedAt` 的提交即拒绝（「事后补拍」），文案点明可疑。
2. **实现者身份**（`internal/tasktypes/types.go`、`internal/clitask/task.go`、`internal/cli/review.go`）：`task start --agent <id>` 记 `Implementer`；`review pass` 拒绝与实现者同一 id；`task report` 的评审连续性行披露「实现者 X（独立 / 与实现者同一 id（独立性未通过）/ 未记录）」。
3. **幻影路径收口**（`internal/scoring/git.go`、`internal/cli/docs.go`）：`changedFiles` 与 `changedMarkdownSince` 复用 `internal/ghostpath.IsReserved`，`nul/…` 这类 Windows 保留名不再进变更集（评审与 change 证据早已过滤，这两处是最后的漏点）。
4. **clarify 口径**（`internal/change/verify.go`、`internal/change/gate.go`）：`requireClarify` 导出为 `RequireClarify`，供入口/门禁共用；入口实测已由 `ComputePhase` 停在 clarify 阶段，补一条测试把「缺 clarify → 入口拒绝」钉住。
5. 工单收口：`wi_mwlq21bel3b1d`（红快照时序）、`wi_mwlq25omtr9q1`（实现者身份）、`wi_mwlhow845lryh`（幻影/口径）关闭；另确认 4 个早前工单已随 2.0 删除宿主层或后续修复失效，一并关闭。

## 影响

- 兼容：不登记 `--agent` 时报告「实现者未记录（独立性未验证）」，不阻断；红记录只对**新任务**有绑定要求（老任务证据已封印）。
- 行为面：红快照事后补拍从「可通过」变为「拒绝」——这是本 change 的目的（review-rfe-1 P2-a 的 waived 项）。

## 验证

acceptance.md 8 条围栏；其中第 3 条含 `TestReviewIndependence_RejectsImplementer`，第 2 条含 `TestRedAttempts_BackdatedCommits` 与 `TestRedAttempts_CheckRedFirst`，第 8 条在临时仓库真跑红先绿 e2e。