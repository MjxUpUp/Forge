# compile-fix-loop — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c613d20823f524-5a2dd71b] accept

- **Skill**: compile-fix-loop
- **DecidedAt**: 2026-07-27T07:08:15Z
- **By**: claude-code

### Diagnosis

接入通用 skill-trigger 框架(feat/skill-trigger): 声明式 metadata.triggers 让通用 hook 在事件点主动驱动本 skill, 解决 dogfood 量化的质量/流程 skill 显式触发=0、靠 agent 自觉必漏问题(code-review-gate 因有 review-stop hook 独活)

### Revision

metadata.triggers 加 [{"event":"PostToolUse","match":"Bash","keywords":["compile error","build failed","undefined reference","cannot find","语法错误"],"cooldown":60}]

### Evidence

dogfood-findings-2026-07-09(testing×17 全低分, 质量 skill 0 显式触发) + plan flickering-bubbling-bonbon.md(triggers schema 表)

## [d-18c772220dd8b464-39086b81] accept

- **Skill**: compile-fix-loop
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现 auto-compile hook 参考条目无可定位信息（未说明 hook 语义与权威来源，读者无法查证）

### Revision

参考节补充定位：auto-compile 自 v0.25 起为 advisory 提醒（编译通过由 agent 自检），指向项目 AGENTS.md Forge 质量协议节

### Evidence

项目 AGENTS.md Forge 质量协议节明确写 auto-compile hook 仅 advisory 提醒；internal/checklog 确认 auto-compile hook 存在

## [d-18c7e5a66d218f70-0047e5f2] accept

- **Skill**: compile-fix-loop
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

SKIP 方向修正(测试失败→systematic-debugging)；补构建失败/测试运行失败分界句

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e622b4e9bbd0-adf43b27] accept

- **Skill**: compile-fix-loop
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ce9b939ec6c708-4188f90e] accept

- **Skill**: compile-fix-loop
- **DecidedAt**: 2026-08-24T02:06:00Z
- **By**: zcode

### Diagnosis

audit DC-10：TypeScript 编译命令 `npx`+tsc 形态 1 处 MEDIUM——npx 运行时可能即时拉注册表 tsc 而非项目锁定的 TypeScript 版本，检查结果与项目实际版本脱钩，且 SKILL.md 行是逐字 agent 指令（供应链执行面）

### Revision

改 `npm exec -- tsc --noEmit -p .`（项目本地依赖、lockfile 锁定版本；本地缺失时先装 typescript）

### Evidence

forge skills audit 全库 finding 9→0（含本条决策文本规避自回引后复扫）；validate 52/52；eval-report vs baseline（run-1787537020-c7b56032）：trigger 100%→100%、not-trigger 100%→100%、净回归 0（机器判据 accept）
