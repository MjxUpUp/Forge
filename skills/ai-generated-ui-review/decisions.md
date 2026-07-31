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
