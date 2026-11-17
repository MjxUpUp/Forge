# project-acceptance · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 维度 1 需求覆盖：`forge task verify-acceptance` 实跑验收并记确定性证据；`forge task report` 披露影响面（文档 / 无测试源码）。
- 维度 2 方案外改动：scope-drift 机械检查已删除；评审者对照 `forge impact` 的改动清单与 change.md 的「影响」段，核对有没有触点之外的源文件被改。
