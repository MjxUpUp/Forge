# ai-generated-ui-review — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc898ac41f08-de352f63] accept

- **Skill**: ai-generated-ui-review
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/ai-generated-ui-review

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c771b01570cc74-a28539ed] accept

- **Skill**: ai-generated-ui-review
- **DecidedAt**: 2026-07-31T17:59:38Z

### Diagnosis

整体审查发现编辑残留：:133 与 :169 两个同名 '## Common Rationalizations' 章节（:169 还有'塔借口'错别字），:18-26 与 :161-167 '与其他 skill 的分工'出现两次

### Revision

两 Rationalizations 表合并为一张（10 行无重复，修错别字）；两分工节合并为一节（顶部表补齐 ai-ui-generation-workflow/design-system-workflow/frontend-feature-development 三行），删重复节

### Evidence

合并前全文 grep 确认两表无重复行；合并后章节名唯一

## [d-18c7729c1e52a230-86b0a45b] accept

- **Skill**: ai-generated-ui-review
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a645aa7808-2937d793] accept

- **Skill**: ai-generated-ui-review
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

删与 frontend-code-review 重复维度 1/3/4 改指针，保留四个独有块；解除对 ai-ui-generation-workflow 循环 composes(D2)；arXiv/CVE 数据核实补链接

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620b5e24658-92b20ce6] accept

- **Skill**: ai-generated-ui-review
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md
