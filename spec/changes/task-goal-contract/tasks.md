# 任务拆分：goal 契约

change: `task-goal-contract`

- [x] 1. `internal/change/goal.go` + 模板：五段解析、哈希、`New` 生成 goal.md，`DocProblems`/status 接入（判据：acceptance #1 和 #2）
- [x] 2. `internal/tasktypes` 的 `GoalContract`、`internal/taskpipeline.checkGoalContract` 与 implement 门禁接线（判据：acceptance #3）
- [x] 3. `task start --change/--goal-file` 与 `change new --by-human`（判据：acceptance #4 和 #5）
- [x] 4. 文档与 compat 快照同步（判据：acceptance #6 和 #7）
- [x] 5. 全量验证与 deadcode 零（判据：acceptance #1 和 #8）