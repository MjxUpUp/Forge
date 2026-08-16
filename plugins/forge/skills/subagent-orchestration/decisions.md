# subagent-orchestration — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18cc4bedde917718-88bd5118] accept

- **Skill**: subagent-orchestration
- **DecidedAt**: 2026-08-16T13:23:57Z

### Diagnosis

近月协作扫描:子agent失控为高频事故类(10+次runaway:上下文爆掉/死循环/产出丢失),本次扫描自身2/3子agent也因context耗尽死亡——缺派发前纪律与收拢契约

### Revision

新建skill:派生前四问gate(输入体积>2MB禁/独立性/输出契约/兜底)+决策树+fan-out上限3~5+部分失败容忍+checklist+6条真实事故Gotcha;triggers=PreToolUse match Agent|Task(cooldown 300)+UserPromptSubmit关键词(cooldown 600)

### Evidence

668条用户消息:子agent失控~10次显式事故;与agent-delegation(Forge任务分派)/transcript-forensics(事后取证)分工互补无重叠
