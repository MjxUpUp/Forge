# proposal: 1.87.3 收尾

## 为什么做

1.87.2 的 dogfood（独立子代理在隔离目录真实跑任务与 change）确认四项修复全部生效，但同时记录了三处会误导 agent 的残留：

- `forge version` 报 `unknown command`，只有 `forge --version` 可用；
- `task start --change c1`（c1 已 ready）仍打印「本任务产物链预期节点未登记」，而 implement gate 说「已钉 change 目标契约——以 change 产物为准」；
- `task report` 在没有任何 `*_test.go` 的仓库写「未验证面：无（改动源码均有配对测试）」。

## 怎样算解决

三处都按事实与一致性修掉，并各有一条测试钉住；不改任何门禁判定。

## 影响面

`internal/cli/version.go`（新）、`internal/clitask/task.go`、`internal/clitask/task_report.go`、`docs/reference/cli.md`、两个测试文件。
