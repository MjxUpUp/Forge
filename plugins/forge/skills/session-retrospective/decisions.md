# session-retrospective — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e5a6ae6a71e0-f797088f] accept

- **Skill**: session-retrospective
- **DecidedAt**: 2026-08-02T05:24:41Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

步骤 0 补无 forge 降级路径(git log+验证命令作事实锚)；分工段加 skill-evolution 双向互链；门槛四问改指针消除双份维护

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62b37c9f65c-ffd188df] accept

- **Skill**: session-retrospective
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 边界补全 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cc4be5816c1adc-fa4c8db4] accept

- **Skill**: session-retrospective
- **DecidedAt**: 2026-08-16T13:23:21Z

### Diagnosis

近月协作扫描发现周度复盘类请求出现过4次但skill零触发——95%触达靠hook,纯声明无trigger的skill不会被加载

### Revision

SKILL.md metadata.triggers新增UserPromptSubmit关键词组(回流审查/周度复盘/近一周/协作效果/协作记录/使用情况审查/沉淀经验),cooldown 7200(周度仪式,重复注入纯噪音)

### Evidence

2026-08-16扫描668条用户消息:该类请求4次全靠agent自觉;触发框架落地后47/51 skill已有trigger,本skill在缺失名单

## [d-18ccd749b1e30938-e940995a] accept

- **Skill**: session-retrospective
- **DecidedAt**: 2026-08-18T07:57:43Z
- **By**: kimi

### Diagnosis

触发缺口：仅 UserPromptSubmit 关键词触发=靠人记得问；2026-08-18 用户显式问『是否该沉淀成自动触发』暴露该缺口（知识层已全，触发层靠运气）

### Revision

triggers keywords 补 沉淀成skill/可复用/总结教训/经验回流（关键词兜底层）；deterministic 主载体落在 forge task complete 的 sedimentReminder（cli/task.go，宿主无关不赌 Stop 通道）；引擎级 session_had_commits condition + Stop trigger 记为 follow-up

### Evidence

skillsqa/rules.go 合法 when 词表核查（5 个均不匹配 Stop 时刻语义）；checklog Delivered 观测 codex Stop 死通道

### Rationale

按本 skill 自己的原则『沉淀的目标是未来不靠人记』：deterministic 机器提醒优于概率性模型触发
