# project-acceptance · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 维度 1 需求覆盖表：`forge task verify-acceptance`（实跑后附覆盖表）或 `forge task artifact --verify`（只对账不实跑）输出「已验证 / 未验证 / 孤儿」三栏；`forge task report` 的未验证面同样列出。
- 维度 2 方案外改动：登记过 design 产物的任务，task-verify 的 `scope-drift` 行以「方案外改动」开头，列出触点之外被改的源文件（`forge trace <ref>` 可见）。
