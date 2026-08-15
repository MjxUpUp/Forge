# evidence-based-proposal — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e5a6a5add59c-01659694] accept

- **Skill**: evidence-based-proposal
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

failure-cases.md 补 3 个非 Rust/LLM 案例(前端/Go/基础设施)；检查清单与两个必答题合并去重

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e622f7e52f78-6bb36a83] accept

- **Skill**: evidence-based-proposal
- **DecidedAt**: 2026-08-02T05:33:35Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 补 dev-lookup SKIP 边界 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf713f0161798-39b0c8ca] accept

- **Skill**: evidence-based-proposal
- **DecidedAt**: 2026-08-15T11:29:02Z

### Diagnosis

研究族合并连带引用修复:fact-research/web-search-bridge 已并入 research-workflow,本skill对二者的 SKIP/分工/降级链引用悬空

### Revision

引用改指 research-workflow 轻量档(Phase L)/通用搜索桥接节;dev-lookup 的 curl-sourcing 相对路径改 ../research-workflow/

### Evidence

forge skills validate 51/51 + TestSkills_NoDanglingSkillRefs 守卫
