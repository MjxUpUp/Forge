# systematic-debugging — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c613d2042c60dc-02ce4e83] accept

- **Skill**: systematic-debugging
- **DecidedAt**: 2026-07-27T07:08:15Z
- **By**: claude-code

### Diagnosis

接入通用 skill-trigger 框架(feat/skill-trigger): 声明式 metadata.triggers 让通用 hook 在事件点主动驱动本 skill, 解决 dogfood 量化的质量/流程 skill 显式触发=0、靠 agent 自觉必漏问题(code-review-gate 因有 review-stop hook 独活)

### Revision

metadata.triggers 加 [{"event":"PostToolUse","match":"Bash","when":"test_command_failed","cooldown":120}]

### Evidence

dogfood-findings-2026-07-09(testing×17 全低分, 质量 skill 0 显式触发) + plan flickering-bubbling-bonbon.md(triggers schema 表)

## [d-18c7718fe574dbac-9b8584be] accept

- **Skill**: systematic-debugging
- **DecidedAt**: 2026-07-31T17:57:20Z

### Diagnosis

整体审查发现 SKILL.md:146 引用不存在的 verification-before-completion（幻觉 skill 名）

### Revision

改为库内真实存在的 verification-driver

### Evidence

skills/ 目录无 verification-before-completion；verification-driver 存在且语义吻合
