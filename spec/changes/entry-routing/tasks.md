# 任务：入口路由与阶段机

change: `entry-routing`

完成标准只有一条：`forge change verify entry-routing` 封印通过。下面的勾选项是给人的
进度记录，不是判据——勾选不产生证据，也不参与封印。

- [x] 1. `next` 决策表接入 spec 状态：新增 `NextContext`、四象限路由、`requires_human` 字段（判据：acceptance #2）
- [x] 2. `task start` 守卫：spec 仓库缺 `--change` 且 `Kind != task` 时拒绝；新增 `TaskKindTask` 与 `--kind task` 语义（判据：acceptance #3）
- [x] 3. change 阶段推导：`ComputePhase`/`UnboundTasks`/`ParseClarify` 与 `change phase`、`change clarify` 子命令（判据：acceptance #4）
- [x] 4. e2e 真实路由：脏树 → change new → phase → task start 全链；goal 未完成时 task start --change 必须失败（判据：acceptance #5）
- [x] 5. 模板与文档：AGENTS 硬规则、clarify 模板、cli.md、README（判据：acceptance #7）
- [x] 6. 静态门禁：build、deadcode 零（判据：acceptance #1 和 #6）