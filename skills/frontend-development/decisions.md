# frontend-development — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6acaec316bce8-9fff9ac4] accept

- **Skill**: frontend-development
- **DecidedAt**: 2026-07-29T05:49:28Z

### Diagnosis

转交3职责(Design Token选型/样式审美/AI生成UI审查)到不存在的skill(frontend-stack-selection/frontend-aesthetics-execution/ai-generated-ui-review)致悬空断链——canonical无此skill,仅docs/skillhub-archive历史归档提及,agent触发后转交无skill可用

### Revision

3职责内联为本skill §2.8(Token与技术栈选型)/§2.9(审美执行清单)/§2.10(AI生成UI安全审查);移除line12转交声明+line97 stack-selection引用+line157-158协作区引用;决策树加3路径

### Evidence

全局断链扫描(grep反引号skill名 vs skills/目录)3名不在canonical;用户决策全内联不新建skill;grep验证内联后无残留引用

## [d-18c6bc897bfaffa0-e758d2d6] accept

- **Skill**: frontend-development
- **DecidedAt**: 2026-07-29T10:40:00Z
- **By**: claude-code

### Diagnosis

5c59ff7内联(§2.8/2.9/2.10)因canonical无specialist误判断链;现11 specialist纳入canonical,断链前提消除

### Revision

SKILL.md从215行内联版回退164行引用版;第97行stack-selection规范为反引号frontend-stack-selection

### Evidence

守卫CTestSkills_NoDanglingSkillRefs全绿;forge skills validate R1-R11通过
