# Goal：工作树存档点（checkpoint / rollback）

## Objective

agent 大范围改代码时，需要一个「动手前的存档点」：改乱了能一步退回，而不是靠重读文件手工还原。
git 自己做得到（stash），但状态机需要记账：谁存的、什么时候、回过哪次。

## Out of Scope

- 不替代 git commit，也不做自动回滚（回滚必须人/agent 显式触发）。
- 不覆盖构建产物、数据库等工作树之外的副作用。
- 不做跨机器的存档点同步（只记在本地任务状态里）。

## Check

- `forge task checkpoint --label` 在脏工作树上存出快照，干净工作树明确拒绝。
- `forge task rollback <id>` 能原样放回内容；未知 id 报错并列出已有存档点。
- 存档点保留最近 20 个，`--list` 可查且标出最近回滚目标。
