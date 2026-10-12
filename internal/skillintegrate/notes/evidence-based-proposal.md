# evidence-based-proposal · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 方案核查：`forge task artifact --check --file <方案>`（或已登记 design 产物时不带 `--file`）逐条核实方案点名的仓库路径、`file:line` 行号范围、`path:Symbol` 的 Go 声明、Go 模块是否在 go.mod（主模块或 require）。同一行带 `new:` / `new-dep:` 的引用视为计划新建，不要求存在；代码围栏内是示例，不核查。结果落 `checklog:design-check`（advisory，不阻断）。
- 度量写成验收：`accept[REQ-n]: <测量命令> :: <期望值>`，经 `forge task artifact --extract` 编译进验收门禁，`forge task verify-acceptance` 实跑。
