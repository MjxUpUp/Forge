# rust-code-review — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc89a8927a20-fec3c98f] accept

- **Skill**: rust-code-review
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/rust-code-review

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

### Rationale

接受纳入 canonical 的理由：该 skill 被 canonical 内其他 skill（frontend-development 等）互引，缺失会造成断链；内容本身符合 Forge skill 规范（metadata.pattern/domain/composes 互引成体系），validate 与 audit 均通过，故按原样纳入，不改动历史内容。

## [d-18c7718fed06e4c8-b9236183] accept

- **Skill**: rust-code-review
- **DecidedAt**: 2026-07-31T17:57:20Z

### Diagnosis

整体审查发现三处问题：description SKIP 引用原宿主私有概念'内置 code-review / --low'；:48/:131 用 Go 术语 t.Fatal→t.Log 表述断言弱化；decisions.md 首条决策缺 ### Rationale 段

### Revision

SKIP 改指 canonical 存在的 code-review-gate + fmt/clippy 直跑；t.Fatal→t.Log 本地化为 Rust 表述（assert! 弱化/恒真/#[ignore] 跳过）；decisions.md 补 ### Rationale 段（不改历史内容）

### Evidence

skills/ 内无内置 code-review 概念；本 skill 面向 Rust 代码审查，Go 术语与上下文语言不一致
