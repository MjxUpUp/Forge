# design-artifact-standards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77808f2fa5868-ee7bbb99] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-07-31T19:55:57Z

### Diagnosis

批①将 AllDesignPhases 降私有为 allDesignPhases（零跨包调用），SKILL.md 两处引用未同步

### Revision

SKILL.md :41/:95 引用改为 allDesignPhases

### Evidence

grep 确认符号已改名，编译通过

## [d-18c7e5a68fe85534-d074f0d2] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

实测跨 skill .. 链接可达性(Kimi 宿主裸 .. 断链)并写入解析约定+绝对路径兜底；删路由表核心维度列

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620c454689c-e955e800] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf713f4b632d8-6304a859] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-15T11:29:02Z

### Diagnosis

研究族合并连带引用修复:fact-research/web-search-bridge 已并入 research-workflow,本skill对二者的 SKIP/分工/降级链引用悬空

### Revision

引用改指 research-workflow 轻量档(Phase L)/通用搜索桥接节;dev-lookup 的 curl-sourcing 相对路径改 ../research-workflow/

### Evidence

forge skills validate 51/51 + TestSkills_NoDanglingSkillRefs 守卫
