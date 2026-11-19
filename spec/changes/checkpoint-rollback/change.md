# change：工作树存档点

## 改动

1. `internal/tasktypes`：`TaskState` 增 `Checkpoints []Checkpoint` 与 `RolledBackTo`；`Checkpoint{ID,Ref,Head,At,Label}`。
2. `internal/clitask/task_checkpoint.go`：`forge task checkpoint [--label|--list]`（`git stash create` 生成不可达提交、记账、保留最近 20 个）与 `forge task rollback <id>`（校验对象可达后 `git stash apply`，记录 `rolled_back_to`）。
3. 文档：cli.md 两行、README 命令表一行。

## 影响

- 只加命令与状态字段（json omitempty，老 state 可读）；不改任何门禁。
- 快照对象不进分支历史，但会阻止 gc 回收该对象，直到状态被清理（最多 20 个，体积可控）。

## 验证

- `internal/cli/task_checkpoint_test.go`：`checkpoint → checkout 丢弃 → rollback` 内容还原；干净工作树拒绝；未知 id 报错；`--list` 标出最近回滚目标。
- 手工 e2e（/c/tmp/ck）：init → task start → 改文件 → checkpoint → 丢弃 → rollback → 内容回到 v2。
