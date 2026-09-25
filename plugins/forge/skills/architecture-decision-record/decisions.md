# architecture-decision-record — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771b89205b8dc-ad20ca5b] accept

- **Skill**: architecture-decision-record
- **DecidedAt**: 2026-07-31T18:00:14Z

### Diagnosis

整体审查发现 frontmatter steps: 7 与正文不符（计数漂移）

### Revision

正文阶段 1 实际为步骤 1-5 共 5 步，steps 改为 5

### Evidence

grep 章节标题确认正文仅 步骤 1：研究备选方案 … 步骤 5：建立索引 共 5 步

## [d-18c7e4c4ecfb234c-5d3dc403] accept

- **Skill**: architecture-decision-record
- **DecidedAt**: 2026-08-02T05:08:31Z

### Diagnosis

审计判决'保留'带微改进建议：'何时创建'判断与 domain-modeling 的 ADR 三条件语义重叠两处维护（domain-modeling 版本更精确）；步骤 2 决策矩阵示例玩具化；system-architecture §2.5 ADR 模板与本 skill 模板双头分叉需单源化

### Revision

'何时创建'改指针对齐 domain-modeling 三条件（只留一处定义）；删玩具决策矩阵示例改真实约束对比；步骤 3 明确声明 templates/adr-template.md 是全库唯一 ADR 模板（system-architecture §2.5 已删模板改指针，已确认库内无第二套模板）

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e622e1372754-128a18eb] accept

- **Skill**: architecture-decision-record
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad501e714c-60c8025a] accept

- **Skill**: architecture-decision-record
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口
