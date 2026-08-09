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

## [d-18c7e5a6717cc404-b329e2f2] accept

- **Skill**: systematic-debugging
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

When to Use 移除构建失败消除与 SKIP 矛盾；test_command_failed 独占；限流案例改指针

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e622bc3327dc-1d03c10b] accept

- **Skill**: systematic-debugging
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18c7e622bc3327dc-audit0809] accept

- **Skill**: systematic-debugging
- **DecidedAt**: 2026-08-09T00:00:00Z
- **By**: claude-code

### Diagnosis

weekly-audit-2026-08-09 发现 trigger `test_command_failed` 生产 0 触发（5/50 trigger skill 唯一死配置）：与 test-discipline 在测试失败场景语义重叠 + 依赖 hook 侧 `ToolOutput.exit_code` 传递（condTestCommandFailed 缺 exit_code 保守返 false，conditions.go:87-90）

### Revision

trigger 从 `PostToolUse test_command_failed` 改为 `UserPromptSubmit keywords`（用户表达调试困境：卡住了 / 为什么还不行 / still failing / keeps failing / can't figure out 等）cooldown 300；贴合 SKILL 设计意图（用户说"卡住了""为什么还不行"时），避开撞车与 exit_code 依赖

### Evidence

forge-weekly-audit-2026-08-09（systematic-debugging 生产触发 0 次）；docs/skills-value-audit-2026-08-02.md:51（撞车已诊断）；internal/skilltrigger/conditions.go:87-90
