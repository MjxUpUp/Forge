# resilience-and-observability — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221e559aca0-edfd7a78] accept

- **Skill**: resilience-and-observability
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与幽灵指针：forge integration-test 不存在（internal/cli 无此命令），forge skills validate 被误当韧性静态检查，references/ 目录不存在，§2 标题节数写错（5 实为 7）

### Revision

提交前必跑删除 validate 与 integration-test 两行（静态检查改为靠 §4 自查清单人工核对）；删除 references/ 占位句；标题改为 7 路径规范

### Evidence

grep internal/cli 确认 Use 列表无 integration-test；SKILL.md 标题枚举确认 §2.1–2.7 共 7 节

## [d-18c7e4ad63f373b8-bf8c05c2] accept

- **Skill**: resilience-and-observability
- **DecidedAt**: 2026-08-02T05:06:50Z

### Diagnosis

审计判决'改进（瘦身+淘汰过期工具）'：§2.6 三处推荐 Hystrix（2018 年起维护模式，推荐它是负价值）；§2.1 SLO 停机时间数值错位（99% 对应月停机 7.2h 而非 43min）；§6 用未定义的 $COLLECTOR_OTLP 环境变量不是可执行步骤；§8 表标题提'阿里'但表内零阿里内容

### Revision

§2.6 模式 1/3 删 Hystrix 换现役生态（resilience4j/Polly/opossum/gobreaker/Sentinel/Envoy outlier detection）并显式标注勿用 Hystrix；§2.1 SLO 数值修正为 30 天窗口 99%/99.9%/99.99% = 7.2h/43min/4.3min；§6 改可执行检查清单（OTel 4318 联通 curl + SLO 告警规则 grep + review pass）；§8 标题改'实践来源对照'

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e622effdecc8-526f9d8f] accept

- **Skill**: resilience-and-observability
- **DecidedAt**: 2026-08-02T05:33:35Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca070103f01ee4-9d2804f0] accept

- **Skill**: resilience-and-observability
- **DecidedAt**: 2026-08-09T03:58:23Z
- **By**: claude-code

### Diagnosis

该 skill 无声明式 trigger，纯靠 agent 自觉加载——dogfood transcript 证明 0 命中，skill 形同被动文档从未注入

### Revision

在 SKILL.md frontmatter metadata 加 triggers 声明（事件 + keywords 或 when condition + cooldown），让 skill-trigger 框架在匹配事件时主动注入加载指引

### Evidence

forge skills validate R1-R17 全 49 通过；trigger 覆盖 5→15（31%）；dry-run 验证 research-workflow/secure-coding 匹配 prompt 正确触发

### Rationale

扩展 trigger 覆盖是 2026-08 审计 P1 优化项；声明式触发是把 skill 从被动文档转主动注入的唯一可靠手段（见 dogfood 发现）

## [d-18d0a2f101531-e101531f4] accept

- **Skill**: resilience-and-observability
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

checklist/bash 示例里的 forge review pass 顺带提及与 code-review-gate 盖章职责重复

### Revision

指针化：改指 code-review-gate 门控（其 forge 条件块负责盖章），正文零 forge 命令引用

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令
