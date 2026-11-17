# evidence-based-proposal · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 方案核查：由 review 人工逐条核对方案点名的仓库路径、`file:line` 行号范围、`path:Symbol` 的 Go 声明、Go 模块是否在 go.mod；同一行带 `new:` / `new-dep:` 的引用视为计划新建。
- 度量写成验收：`forge task start --accept "<测量命令> :: <期望值>"`，由 `forge task verify-acceptance` 实跑。
