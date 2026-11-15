# 任务：入口路由与阶段机

change: `entry-routing`

完成标准只有一条：`forge change verify entry-routing` 封印通过。下面的勾选项是给人的
进度记录，不是判据——勾选不产生证据，也不参与封印。

- [x] 1. `next` 决策表接入 spec 状态：`NextContext`、四象限路由、ready change 直接给 `task start --change`（判据：acceptance #2）
- [x] 2. task start 只做「链接检查」：不带 `--change` 放行；带 `--change` 才要求 ready（判据：acceptance #3）
- [x] 3. change 阶段推导：`ComputePhase`/`UnboundTasks`/`ParseClarify` 与 `change phase`、`change clarify` 子命令（含 fast 跳 design/tasks、老 change 跳 goal）（判据：acceptance #4）
- [x] 4. e2e 真实链路：脏树 → next 建议 → 直接开工 → 后补 change → ready 后带 `--change` 开工（判据：acceptance #5）
- [x] 5. 模板与文档：AGENTS 托管块改为 task-first、clarify 模板说明、cli.md 区分 list STAGE 与 phase、README 快速开始可照做（判据：acceptance #6 和 #7）
- [x] 6. 静态门禁：build、gofmt、deadcode（判据：acceptance #1 和 #8 和 #9）