# implementation-discipline · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 反第 0 级（注释标识 ≠ 解决）的处置一「转任务跟踪」：`forge task start --ref <ref> --title <描述>`（被门禁追踪，task-guard/task-complete 门禁链生效）。
- 债务注释的机械检测：taskpipeline cheat-scan 的 comment-as-debt 模式检测新增 TODO/FIXME/XXX/HACK 类债务注释（advisory，不阻断）。

## 门控顺序（forge 实现）

- `forge task gate task-implement → task-verify → task-complete` 三道门禁过完再 `git commit`——**commit 必须在 `forge task complete` 之前**（complete 会清空 active task ref，之后提交源码会被 quarantine 拦截）。
