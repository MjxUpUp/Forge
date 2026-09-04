# release-readiness — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771dc26c2c0f0-ae213ff9] accept

- **Skill**: release-readiness
- **DecidedAt**: 2026-07-31T18:02:47Z

### Diagnosis

整体审查发现两处命令错误：:144 嵌套引号 grep 实际只匹配开引号一个字符（正常非空默认值全命中，假阳性淹没 M5）；:70 awk 有 \>? typo 且 rule1 置 flag 后同行 rule2 即 exit，该命令实际从不输出任何内容

### Revision

:144 拆成明确匹配空串的两条简单 grep（空双引号/空单引号各一）；:70 typo 统一为 \[<NEW_VER>\]? 并加 next 修复同行 exit bug、去掉从未赋值的 seen 变量

### Evidence

在 /tmp 用样例 CHANGELOG 实测：原写法与仅修 typo 均无输出，加 next 后正确打印目标章节；两条 grep 对空串默认值得命中、非空值不误报

## [d-18c7729c6f953bd0-2c0abe08] accept

- **Skill**: release-readiness
- **DecidedAt**: 2026-07-31T18:16:33Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e49b6c653ca0-6f3e6a2b] accept

- **Skill**: release-readiness
- **DecidedAt**: 2026-08-02T05:05:33Z

### Diagnosis

审计发现：382 行过长——Gotchas/Rationalizations/Red Flags 共约 40 行纯文本经验库正是该拆 references 的部分；M5 secrets 扫描与 secure-coding 有重叠但分工节未列

### Revision

Gotchas/Rationalizations/Red Flags 拆 references/gotchas-and-rationalizations.md；checklist 模板拆 references/checklist-template.md；R1-R5 完整检查命令拆 references/recommended-checks.md（正文留摘要表）；决策树完整版拆 references/decision-tree.md（正文留四分支压缩版）；防注水自检节改为指针（规则唯一真相源 skill-authoring-standard「防注水自检」节）；M5 与「与其他 skill 的分工」补 secure-coding 划界（编码期安全基线归它，M5 只做发布前增量扫描）；382 行瘦身至 235 行

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计（release-readiness 详评 + 改进清单项 9/13）

## [d-18c7e620bb9fa0a4-7e78bb21] accept

- **Skill**: release-readiness
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca0700faa852fc-0aa94648] accept

- **Skill**: release-readiness
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
