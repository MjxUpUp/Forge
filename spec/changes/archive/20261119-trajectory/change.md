# Change：轨迹观测

## 改动

1. `internal/checklog/types.go`：`Entry` 加 `DurationMS`、`Attempt`、`Steps`（均 omitempty）。
2. `internal/clitask/task_gate.go`：验收实跑计时，聚合条目写 `DurationMS` 与 `Steps=len(Acceptance)`。
3. `internal/cli/review.go`：评审落章写 `Attempt=len(ReviewRounds)`、`Steps=len(findings)`。
4. `internal/clitask/task_report.go`：新增 `TrajectoryLine`（文本 + JSON `trajectory_line`），从 checklog 汇总验收/评审轮数与末轮耗时。

## 影响

- 只读新增：报告多一行，旧条目没有 duration_ms 时显示「未记录」。
- 不影响门禁判定；不改 compat 的 check 名单。
- 现有 checklog 条目 JSON 向后兼容（新字段 omitempty）。

## 验证

- `TestBuildTrajectoryLine`：两条验收 + 一条评审 → 「验收 2 轮」「4.2s」「评审 1 轮」；无耗时任务 → 「未记录」。
- `TestRunTaskVerifyAcceptanceAt_RecordsDeterministic` 等既有验收测试仍绿（新增字段不破坏既有断言）。
- gofmt / deadcode / no-removed-checks 守卫。
