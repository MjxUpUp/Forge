# test-discipline — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c613d1f7b4dd84-e2874a47] accept

- **Skill**: test-discipline
- **DecidedAt**: 2026-07-27T07:08:14Z
- **By**: claude-code

### Diagnosis

接入通用 skill-trigger 框架(feat/skill-trigger): 声明式 metadata.triggers 让通用 hook 在事件点主动驱动本 skill, 解决 dogfood 量化的质量/流程 skill 显式触发=0、靠 agent 自觉必漏问题(code-review-gate 因有 review-stop hook 独活)

### Revision

metadata.triggers 加 [{"event":"PostToolUse","match":"Bash","when":"test_command_failed","cooldown":120}]

### Evidence

dogfood-findings-2026-07-09(testing×17 全低分, 质量 skill 0 显式触发) + plan flickering-bubbling-bonbon.md(triggers schema 表)

## [d-18c7e5a664804618-ced0c98d] accept

- **Skill**: test-discipline
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

trigger 让渡 test_command_failed 给 systematic-debugging、改 commit 前守卫场景；httptest RemoteAddr 案例两处改指针(含 anti-patterns.md 第5处)

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e6229ed702a8-6cd7af6e] accept

- **Skill**: test-discipline
- **DecidedAt**: 2026-08-02T05:33:33Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md
