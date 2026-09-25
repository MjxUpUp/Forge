# requirement-clarification — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e5a6a13b57b4-209bc96c] accept

- **Skill**: requirement-clarification
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

拆 references(pitfalls/spec-template) 304→212 行；补一句话可验收时的轻量短路

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620a76c41f0-dff2af5f] accept

- **Skill**: requirement-clarification
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad5da00844-f1d3eb7d] accept

- **Skill**: requirement-clarification
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口

## [d-undloop-b1-20260920] accept

- **Skill**: requirement-clarification
- **DecidedAt**: 2026-09-20T18:00:00Z
- **By**: feat/understanding-loop（理解侧闭环批次）

### Diagnosis

规格模板的验收条件只有叙述性行——无样例对锚定 ground truth，形容词（"专业/流畅"）无处对账；约束节是四项自由列表，不区分「能进 CI 实跑 / 只能评审判定 / 只能人工」——下游拿到约束不知投递到哪条执法通道（人-AI 沟通调研的偏差根因①配置错觉与②批准粒度错位）。

### Revision

spec-template.md 验收条件后新增「样例对（Ground Truth）」节（≥2 正例 + ≥1 反例，可命令化样例落 accept: 行）；约束节改造为「三分类投递」（可执行→accept 行 / 可命题→短规则+正反例 / 不可命题→显式人工）；SKILL.md 维度 2/4、决策树、输出规格模板段同步；自查清单加样例对与三分类两项。description 不动（避免 eval case 集 DescHash 漂移）。

### Evidence

调研会话五步管道第①②步（喂样例不喂形容词 / 约束三分类）；ADR-0001 三分类↔执法通道映射；accept: 行是验收产物提取通道的机械声明形态（v1.68.0 起可配 v2 断言）。
