# tdd-cycle — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c613d2003546d8-0f257aa3] accept

- **Skill**: tdd-cycle
- **DecidedAt**: 2026-07-27T07:08:14Z
- **By**: claude-code

### Diagnosis

接入通用 skill-trigger 框架(feat/skill-trigger): 声明式 metadata.triggers 让通用 hook 在事件点主动驱动本 skill, 解决 dogfood 量化的质量/流程 skill 显式触发=0、靠 agent 自觉必漏问题(code-review-gate 因有 review-stop hook 独活)

### Revision

metadata.triggers 加 [{"event":"UserPromptSubmit","keywords":["TDD","先写测试","red green","测试驱动"]}]

### Evidence

dogfood-findings-2026-07-09(testing×17 全低分, 质量 skill 0 显式触发) + plan flickering-bubbling-bonbon.md(triggers schema 表)

## [d-18c7e5a66034de70-6fcbc2f7] accept

- **Skill**: tdd-cycle
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

RED 坏例子换真反例(原例站不住)；新增存量代码无测试补救节(Iron Law 例外路径)；description 补补测试触发词

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e62296f01ee4-f1b23d56] accept

- **Skill**: tdd-cycle
- **DecidedAt**: 2026-08-02T05:33:33Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md
