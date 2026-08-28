# database-design — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221fdcfa898-ac7d3e50] accept

- **Skill**: database-design
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与高危假命令：forge skills eval 不存在（只有 eval-gen/cases/record/report/baseline）；forge integration-test 不存在；psql -f up.sql --dry-run 是假 dry-run——psql 无 --dry-run，-f 会真实执行 migration 可能毁数据；references/ 目录不存在

### Revision

eval 改为真实命令 forge skills eval-cases --skill database-design；删除 integration-test 行（perf 回归改指 §2.4 慢查询流程）；psql 假 dry-run 改为 BEGIN/\i up.sql/ROLLBACK 事务包裹演练并加 -f 无 dry-run 警告；删除 references/ 占位句

### Evidence

grep internal/cli 确认无 eval/integration-test；psql 官方文档无 --dry-run 选项

## [d-18c7729c77bfe01c-37e3804f] accept

- **Skill**: database-design
- **DecidedAt**: 2026-07-31T18:16:33Z

### Diagnosis

复审发现 eval-cases 在无 case 集时非零退出，作为必跑步骤缺前置说明

### Revision

eval-cases 行补注释：首次使用先 eval-gen --skill database-design --save 建 case 集

### Evidence

internal/cli/skills_eval_cases.go:105 无 case 集时报错并提示 eval-gen

## [d-18c7e45989aa10d4-15148f6d] accept

- **Skill**: database-design
- **DecidedAt**: 2026-08-02T05:00:50Z

### Diagnosis

审计判决'改进（中度瘦身）'：§2.1 schema 7 步/§2.7 备份表/§2.5 ORM 排序/§8 SQL-NoSQL 适配表偏教科书，ORM 排序无依据标注；§4 与 code-review-gate phase-database.md 条目级重复；§6 提交前必跑混入 forge skills eval-cases（位置误导）

### Revision

§2.1 砍为决策点密度；§2.5 删无依据语言排序保留查询场景+反 ORM 纪律；§2.7 删 RPO/RTO 备份表保留恢复演练铁律；§8 适配表整节删除；§4 机械可检项改指针到 phase-database.md；§6 移除 eval-cases；expand/contract 迁移纪律与 psql BEGIN/ROLLBACK 演练原样保留

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e622cb2d8520-5f5eda86] accept

- **Skill**: database-design
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cc4be84149b204-fbf51f38] accept

- **Skill**: database-design
- **DecidedAt**: 2026-08-16T13:23:33Z

### Diagnosis

同UI族:建表/schema设计请求无触发

### Revision

metadata.triggers新增UserPromptSubmit关键词(建表/表结构/schema 设计/写 migration/加索引/慢查询/ORM 选型/选 ORM/数据库设计),cooldown 600

### Evidence

2026-08-16扫描:DB设计请求多项目出现,触发覆盖缺口

## [d-18d0a2f115817-e115817f4] accept

- **Skill**: database-design
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

## [d-18cfffeef9b808a0-58586f4a] accept

- **Skill**: database-design
- **DecidedAt**: 2026-08-28T14:56:18Z
- **By**: claude-code

### Diagnosis

doc-review L2 发现「其 forge 条件块负责盖章」悬空指针（code-review-gate 的 forge 块已随零反向依赖迁移删除），读者循指针找不到目标

### Revision

改为「宿主有审查盖章机制时由其标记已审」，与 code-review-gate:161 的宿主盖章措辞对齐

### Evidence

L2 复审 PASS 96/100（C2 resolved）；双树 grep 零「forge 条件块」残留
