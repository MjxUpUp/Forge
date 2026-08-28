# Forge 集成（仅 forge 项目适用）

本文件收纳 doc-review 的 forge 专属机制：L1 lint、doc gate 落档与门禁、逃生口。非 forge 项目无需阅读——正文对应位置的条件化指针直接跳过即可（无 forge 时本 skill 退化为纯方法论：结构清单人工过 + 独立子代理评分 + 报告直接交付）。

## 三层约束定位

| 层 | 载体 | 管什么 |
|---|---|---|
| L1（机器可判） | `forge docs lint`（D1-D7 规则） | 禁令短语、必填章节、结论枚举、篇幅上限——exit 0=过 / 2=硬失败 |
| L2（本 skill） | rubric-docs.md 四维评分 | L1 判不了的「重点是否前置、详略是否得当、证据是否可追溯、受众是否匹配」 |
| 门禁（流程） | task-complete doc gate | 变更了 markdown 产物的任务，complete 前须有 fresh 的 L2 回检记录 |
| L3（不自动化） | 显式标注 | 业务价值、对外措辞——模板标「需人工确认」，评分跳过 |

## 完整流程（L1 → L2 → 落档）

1. **L1**：落盘 .md 跑 `forge docs lint <paths>`，硬失败先修（PR 描述/commit body 不落盘，按模板结构核对）。
2. **L2**：按本 skill 正文流程派独立子代理评分（产出者不能自检），产出四维得分 + 分级发现。
3. **落档**：`forge task doc-review --passed pass --score <N>`（`--passed failed --note "<原因>"` 记录未过），自动带轮次与证据关联。

## doc gate 规则（complete 前的硬前置）

以下任一命中即拒绝 `forge task complete`：

- 任务变更了 markdown 产物但**无 L2 记录**，或记录**过期**（产物在记录之后又改过）
- L2 **得分 < 75**（阈值与 rubric-docs.md 对齐，真相源在本 skill）
- **未决 Critical** 发现

`task-verify`（task complete 前的自检）会提前发 advisory 提醒未过文档的清单，不必等到 complete 被拒才发现。

## 逃生口（显式自我承担）

`forge task override --doc-gate disable`——3 轮未过后须人工确认；override 会把任务 evidence 降到 Weak（诚实代价：跳过回检的任务在审查时按弱证据对待）。

## rubric 进化回路

同类文档发现打回 ≥3 次 → 把该模式提炼成新的 L1 规则或类型特化行（机制见 session-retrospective 的「文档回检模式提炼」节）——L2 是 L1 规则的孵化器。
