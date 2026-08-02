# reverse-engineering-patterns — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c5ec633075c854-9ddf6dc2] accept

- **Skill**: reverse-engineering-patterns
- **DecidedAt**: 2026-07-26T19:05:38Z
- **By**: claude-code

### Diagnosis

注释卫生任务要求去除"借鉴/抄"等字眼，skill 原文多处用"借鉴"表述参考行为

### Revision

SKILL.md 7 处"借鉴"→"参考"（阶段3标题借鉴分析→参考分析、Why值得借鉴→值得参考、借鉴原则→参考原则、不借鉴看起来优雅→不参考），references/claude-code-example.md 同步 4 行

### Evidence

grep 确认 SKILL.md + references 无"借鉴/抄袭/抄来"残留

### Rationale

措辞中性化，避免暗示抄袭，符合注释卫生要求

## [d-18c7719fb949ebc0-3e7067c7] accept

- **Skill**: reverse-engineering-patterns
- **DecidedAt**: 2026-07-31T17:58:28Z

### Diagnosis

整体审查发现自相矛盾：正文:12 说'从 Codex 源码逆向'，:104 参考节把 Codex/Claude Code 两名字缝在一行，但实际 reference 是 claude-code-example.md（Claude Code 逆向）

### Revision

:12 与 :104 统一为 Claude Code（以实际 reference 为准）

### Evidence

references/claude-code-example.md 标题即'Claude Code 逆向实例'，内容含 Coordinator/Worker、4 层压缩，与正文描述吻合

## [d-18c7e49ffdad692c-85e26f51] accept

- **Skill**: reverse-engineering-patterns
- **DecidedAt**: 2026-08-02T05:05:53Z

### Diagnosis

审计发现：description 承诺「逆向分析编译产物或混淆代码」但正文 90% 是源码阅读方法论，承诺与交付落差大；内容偏方法论说教缺可验证步骤（没有任何具体命令）；正文「~45 个工具」数字宣称无 references 佐证

### Revision

description 收窄到「源码架构分析与模式提取」（删编译产物/混淆代码承诺）；三个阶段各补可执行侦察命令（rg/find 文件清单动作）；选「删正文具体数字宣称」分支：删「~45 个工具」，保留 example 文件可佐证的 4 层压缩与 Coordinator/Worker 并指向 references

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e622ff690ecc-45097db9] accept

- **Skill**: reverse-engineering-patterns
- **DecidedAt**: 2026-08-02T05:33:35Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md
