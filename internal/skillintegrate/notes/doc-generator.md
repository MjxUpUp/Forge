# doc-generator · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

- 落盘产物 lint：`forge docs lint`（禁令短语/必填章节/结论枚举机器可查，扫 skills/ docs/ plugins/）。
- 历史文件路径迁移注记：2026-08 前 forge 环境的历史写在 `~/.forge/doc-generator/history.jsonl`，现统一为 `~/.doc-generator/history.jsonl`（skill 自有命名空间）——老路径如有存量数据可手动合并。

- 技术方案：模板顶部档位表对应 `forge task start --tier`；落盘名含 `tech-plan` / `技术方案` / `design-doc` 或产物链的 `design.md` 时，`forge docs lint` 按 design 类型检查（TL;DR 前置、Non-goals 与验证必有、frontmatter `tier` 分档篇幅；观察窗口内全部 advisory）。写完 `forge task artifact --check --file <方案>` 核查路径/`file:line`/Go 符号/Go 模块是否存在（`new:` / `new-dep:` 行豁免）；登记为 design 产物后触点自动写入 PlanScope。
