# session-retrospective · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

## 步骤 0 的结构化结论（替代手工事实锚）

- `forge act show`（最新任务结论）/ `forge act list --json`（全量）；看 **Strength / Ratio**——Unverified（零实跑）/Weak（声明主要靠自述）= 完成没真验证，优先回顾的盲区；**LowDimensions**（<70 的维度即回顾靶点）；结论带 RetrospectiveNudge 时按其指令回顾根因。Strong 且 ≥70 的干净完成不强制回顾证据，仍走 5 步看协作类教训。
- 跨任务系统性模式跑 `forge health`——证据盲区率高 = 系统性「声明完成却没真验证」；复发低分维度（×N）= 共性缺口，优先沉淀守卫测试/铁律。

## 步骤 5 的 skill 载体验证

- `forge skills validate` 过 R1-R18、`forge skills audit` 过安全扫描。

## 步骤 6 的回检数据源与升级落点

- 素材源：`forge task finding` 里 Source=doc-review 的未决项、DocReviewHistory 轮次得分趋势（两轮之间 Critical/rubric 不收敛即异常）。
- 升级落点：禁令短语/结构/篇幅规则进 `internal/doclint`（D1-D7 表）、模板缺章节进 doc-generator 对应模板。
- 升级后两查都绿才算不误伤：`forge docs lint` 扫 skills/ docs/ plugins/ 存量 + `go test ./internal/doclint/` 验规则行为。

## 行动

- 有 forge 环境时按上述命令采集结构化结论替代手工事实锚；无则跳过，skill 正文的手工锚路径已完整。
