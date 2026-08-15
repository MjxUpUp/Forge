# project-acceptance — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771dc2a936dd8-62958c67] accept

- **Skill**: project-acceptance
- **DecidedAt**: 2026-07-31T18:02:47Z

### Diagnosis

整体审查发现 :40 'grep -nE fn .{50,}|if .{5,}' 伪启发式无判别力（函数名长度/if 条件长度与圈复杂度无关）

### Revision

删除该 grep，只保留 plato/complexity-report/lizard 等真工具

### Evidence

该正则按字符长度匹配，无法反映嵌套深度或分支数，必然大量误报/漏报

## [d-18c7e5a649ee0a38-7e9ae24d] accept

- **Skill**: project-acceptance
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

SKIP 补 review-batch 和 release-readiness；输出改默认打印、落盘带日期文件名；塔借口错别字修

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620b8c69edc-e8912f3e] accept

- **Skill**: project-acceptance
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6ad743394b8-2d135c63] accept

- **Skill**: project-acceptance
- **DecidedAt**: 2026-08-15T11:21:42Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口
