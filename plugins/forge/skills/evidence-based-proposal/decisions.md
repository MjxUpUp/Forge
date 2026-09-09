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

## [d-18cc4bec01e36d40-027cbaf5] accept

- **Skill**: evidence-based-proposal
- **DecidedAt**: 2026-08-16T13:23:49Z

### Diagnosis

同UI族:方案论证类请求无触发

### Revision

metadata.triggers新增UserPromptSubmit关键词(方案依据/凭什么/备选方案/方案对比/选型理由/可行性论证/为什么选),cooldown 600

### Evidence

选型论证请求高频(研究类簇),触发覆盖缺口

## [d-18d39c7d8f2061c0-f27b0b42] accept

- **Skill**: evidence-based-proposal
- **DecidedAt**: 2026-09-09T09:19:00Z
- **Commit**: 461ef57

### Diagnosis

两个必答题只验证'环境假设成立'与'解决哪个断点'，缺'改完后更好如何判定'——会话回顾实证：无适应度函数门的改进声明与退化不可区分（2026-08 dc10 自指证据失效同源：测量时刻与声称时刻不同态）

### Revision

SKILL.md：新增第三必答题（成功度量——事前声明可测判据、同数据集/表面 before/after、证据测量与声称同态）；禁止反模式补「声称优化却无声明的度量」；Red Flags 补 1 条

### Evidence

docs/design/evolution-discipline-norms.md §二①；inventory --verify 通过、audit 0 finding、守卫测试全绿

### Rationale

度量必答题是 evidence-based-proposal 的本位职责（方案依据的最后一环：改了凭什么说更好），与既有两问同构，非新职责
