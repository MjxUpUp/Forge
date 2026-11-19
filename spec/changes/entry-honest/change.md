# 入口诚实：改动

change: `entry-honest`

## 改动

1. 删除 `CheckAcceptanceQuality` 及其调用点：它靠 `npm test`/`go test` 之类命令名前缀猜「考卷合不合格」，补一个补不完一个；LoopSpec 的做法是证据绑定 `plan_digest + baseline + scope_digest`、由评审给 verdict，从不猜命令语义。Forge 保留的可执行证据是：判据登记、实跑回扣、新鲜度、红先绿、评审认领判据。
2. `forge next`：ready change 只有最近 72 小时内有改动才进入推荐；陈旧 change 单独放进 `stale_changes`，此时给新任务入口并说明「有 N 个陈旧 change 未推进（change list 可查，别再盲目续旧）」。
3. 协议与文档：`internal/change/templates/agents-protocol.md` 的硬规则改为「用户提新需求 → 直接 `change new` / `task start`；只有继续或接手现有任务、或不知道当前该干什么时才跑 `forge next`」；`docs/reference/cli.md` 的 next 与 task complete 段同步。

## 影响

- 用户开新任务时不再被旧 change 劫持（这次 Claude 会话在 2h17m 里先被 next 引向 `doudou-play`）。
- 门禁不再对语言/框架做枚举判断；判据门槛回到「命令真的跑过、真的先红后绿、评审真的认领」。
- 旧 change 不会消失：`stage` 里显式列出，`stale_changes` 计入 JSON，归档仍由人决定。

## 验证

- `internal/taskpipeline/next_test.go` 新增「陈旧 ready change 不推荐、给 change new」「新鲜 ready change 仍推荐 task start --change」两态。
- `internal/taskpipeline/acceptance_register_test.go` 删除质量门用例；`internal/clitask/task_complete_test.go` 不再构造「无测试类命令」的失败预期。
- docs lint、gofmt、deadcode、全量测试。
