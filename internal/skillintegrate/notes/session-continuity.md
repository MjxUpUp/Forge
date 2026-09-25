# session-continuity · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

## 阶段 1 恢复源优先级（forge 实现）

- **优先 `forge task resume [--ref <ref>]`**：拉回结构化接续真相源（目标/计划/已确认决策/下一步/阻塞/参与工具 + 门禁进度 + git 已改未提交），比 parse markdown/转录更快更可靠、抗压缩丢失、跨工具双向锚定。git/文档/转录 pipeline 作为补充。

## HANDOFF 双写（forge 实现）

- forge task 是真相源，HANDOFF.md 是导出视图：决策/下一步/阻塞优先 `forge task decide/next/block`（用户级 DataDir 持久化，跨会话/跨工具 resume 拉回）；`HANDOFF.md` 用 `forge task context` 文本导出，供人类阅读/无 forge 环境。
