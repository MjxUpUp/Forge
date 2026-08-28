# doc-generator — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e48bf1c158bc-c2a40a86] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-02T05:04:26Z

### Diagnosis

审计发现：按模板生成 PRD/周报已接近主流模型原生能力，skill 真正价值在防编造护栏而非生成能力，但正文未点破；历史存储路径寄居 research 命名空间语义不干净

### Revision

开篇加定位声明（不增强生成能力，价值=防编造护栏+结构一致性+增量记忆）；历史文件路径从 ~/.forge/research/doc-gen-history.jsonl 挪到独立目录 ~/.forge/doc-generator/history.jsonl（模板文件未引用该路径，无需同步改）

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e623067bd438-8887656c] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-02T05:33:35Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca070111d19150-12d4b57a] accept

- **Skill**: doc-generator
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

## [d-18cbf6ad81d1149c-f510109d] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-15T11:21:42Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口

## [d-18cf05aaa0adc9c8-376a96f7] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-25T10:30:07Z

### Diagnosis

触发表宣称 7 类文档但模板库只有 4 个——复盘报告/技术方案/发布说明三处空挂（触发后无模板可用）；PR 描述与测试报告两个高频文档场景无模板

### Revision

新增 5 个模板（template-pr/test-report/retrospective/tech-plan/release-notes，三段式与现有 4 模板同构）；触发表加 PR 描述/测试报告行；模板清单与 references/README 状态表双更新（修过时状态）；SKILL.md 顺手把裸引号的综上所述改为反引号（过自身 doclint D1）；evals 加 6 条触发用例

### Evidence

飞书《AI 产物可读性差调研设计》发现三 + docs/design/output-readability-gates.md；forge docs lint 29 文件全过

## [d-18d0a2f107990-e107990f4] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

方法论正文夹杂 forge 操作句（未标 forge-only、缺降级说明），破坏工具中立性

### Revision

改为「> Forge 项目」条件引用块并补无 forge 降级行为（dev-workflow shell-free 段工具中立化等）

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令

## [d-18cffa78fdc9008c-ca20e009] accept

- **Skill**: doc-generator
- **DecidedAt**: 2026-08-28T13:16:14Z
- **By**: claude-code

### Diagnosis

SKILL.md/references 含 forge 操作性引用（条件块/forge-integration.md/双路径/模板占位符）——违反 skills 零反向依赖契约（CONVENTIONS §13 R18 硬校验），存量豁免通道要求迁出

### Revision

forge 集成内容整体迁出至 forge 侧 internal/skillintegrate notes/（forge skills integration doc-generator 查看，skill-trigger 推荐块附指针）；正文改为工具中立方法论（降级路径升为主路径/宿主机制中性措辞）

### Evidence

forge skills validate 53/53 通过且 R18Grandfathered 清空；TestR18_Grandfathered_Exact 双向卡死通过

### Rationale

依赖单向化：方法论完整留在中立库，forge 增强完整在 forge 侧；forge 用户体验经集成笔记+触发指针承接
