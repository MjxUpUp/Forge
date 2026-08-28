# system-architecture — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221ed7ca0f4-40639dc1] accept

- **Skill**: system-architecture
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与语义错标：forge auto-build 不存在；forge skills audit 被误标为架构评审（audit 只审 skill 文件规范+安全）；forge skills validate --skill=c4-model 指向不存在的 skill；references/ 目录不存在

### Revision

提交前必跑：auto-build 替换为 go build ./...；删除 audit 行与 c4-model validate 行；删除 references/ 占位句

### Evidence

grep internal/cli 确认无 auto-build；ls skills/ 确认无 c4-model；skills_audit.go 确认 audit 语义为 skill 文件审查

## [d-18c7e4715781431c-21327188] accept

- **Skill**: system-architecture
- **DecidedAt**: 2026-08-02T05:02:32Z

### Diagnosis

审计判决'改进（大幅瘦身）'：当前是 DDD/C4/12-factor/Well-Architected 教科书合订本；§2.5 ADR 模板与 architecture-decision-record 模板双头分叉（两套字段不同）；§2.6 12-factor 逐条表和 §2.3 DDD 六种关系是纯知识复述；§6 提交前必跑是 go build 张冠李戴（架构交付物是文档/图）；§8/§9 为完整而完整，参考链接混入 Web Vitals

### Revision

245 行压到 ~130 行：§2.5 ADR 模板整节删除改指针到 architecture-decision-record（全库唯一模板）；§2.6 12-factor 压成一行官方链接；C4 表删（保留画图硬规则进 §2.1）；§2.3 删六种关系复述保留边界识别步骤；§6 改为 ADR 索引检查+forge review pass；§8/§9 整节删除；保留带阈值判断规则（拆微服务 5 信号≥2、集成模式决策树、负向约束、Gotchas）

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e622d9c71d58-269da35f] accept

- **Skill**: system-architecture
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca070108736354-0c21bc17] accept

- **Skill**: system-architecture
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

## [d-18cbf6ad864ad2c4-d118a52b] accept

- **Skill**: system-architecture
- **DecidedAt**: 2026-08-15T11:21:42Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口

## [d-18d0a2f127149-e127149f4] accept

- **Skill**: system-architecture
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

## [d-18cfffef06a1f24c-80a2463c] accept

- **Skill**: system-architecture
- **DecidedAt**: 2026-08-28T14:56:19Z
- **By**: claude-code

### Diagnosis

doc-review L2 发现「其 forge 条件块负责盖章」悬空指针（code-review-gate 的 forge 块已随零反向依赖迁移删除），读者循指针找不到目标

### Revision

改为「宿主有审查盖章机制时由其标记已审」，与 code-review-gate:161 的宿主盖章措辞对齐

### Evidence

L2 复审 PASS 96/100（C2 resolved）；双树 grep 零「forge 条件块」残留
