# research-workflow — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e41b47482098-ced1e77a] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-02T04:56:23Z

### Diagnosis

审计发现：模型降级链写死个人栈型号（glm-4.6/doubao/deepseek/glm-4-flash）已陈旧且对他人无意义；SKIP 节三层调研量级路由表为 4 份复制之一，drift 已开始

### Revision

模型降级链改通用表述（按当前可用模型配置降级链，主力模型→任一可用备选，不写死具体型号）；SKIP 节三层量级表删除，改指针指向 fact-research「三层调研量级（路由依据）」节（canonical）

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e62b2368ab04-0d03f50d] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 边界补全 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca0700edf20ae4-bdb77a6f] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-09T03:58:22Z
- **By**: claude-code

### Diagnosis

该 skill 无声明式 trigger，纯靠 agent 自觉加载——dogfood transcript 证明 0 命中，skill 形同被动文档从未注入

### Revision

在 SKILL.md frontmatter metadata 加 triggers 声明（事件 + keywords 或 when condition + cooldown），让 skill-trigger 框架在匹配事件时主动注入加载指引

### Evidence

forge skills validate R1-R17 全 49 通过；trigger 覆盖 5→15（31%）；dry-run 验证 research-workflow/secure-coding 匹配 prompt 正确触发

### Rationale

扩展 trigger 覆盖是 2026-08 审计 P1 优化项；声明式触发是把 skill 从被动文档转主动注入的唯一可靠手段（见 dogfood 发现）
