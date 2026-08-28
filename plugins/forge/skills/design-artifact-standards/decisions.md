# design-artifact-standards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77808f2fa5868-ee7bbb99] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-07-31T19:55:57Z

### Diagnosis

批①将 AllDesignPhases 降私有为 allDesignPhases（零跨包调用），SKILL.md 两处引用未同步

### Revision

SKILL.md :41/:95 引用改为 allDesignPhases

### Evidence

grep 确认符号已改名，编译通过

## [d-18c7e5a68fe85534-d074f0d2] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

实测跨 skill .. 链接可达性(Kimi 宿主裸 .. 断链)并写入解析约定+绝对路径兜底；删路由表核心维度列

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620c454689c-e955e800] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf713f4b632d8-6304a859] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-15T11:29:02Z

### Diagnosis

研究族合并连带引用修复:fact-research/web-search-bridge 已并入 research-workflow,本skill对二者的 SKIP/分工/降级链引用悬空

### Revision

引用改指 research-workflow 轻量档(Phase L)/通用搜索桥接节;dev-lookup 的 curl-sourcing 相对路径改 ../research-workflow/

### Evidence

forge skills validate 51/51 + TestSkills_NoDanglingSkillRefs 守卫

## [d-18cc4be84736b450-a70e2910] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-16T13:23:33Z

### Diagnosis

同UI族:设计文档请求无触发;该skill是编写期路由入口,漏触发=6个phase文档全不走标准

### Revision

metadata.triggers新增UserPromptSubmit关键词(写 PRD/需求文档/API 契约/OpenAPI/proto 定义/测试方案/user story/设计文档),cooldown 600

### Evidence

PRD/API设计请求高频;路由入口漏触发放大下游全族失守

## [d-18cf01f4af43e698-7a2bc5aa] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-25T09:22:07Z
- **By**: kimi-code

### Diagnosis

description 484 字符超 350 软上限：六种产物类型的路由表（含 phase 名括注）整个塞进 description，与正文路由表重复

### Revision

压缩至 331 字符（-32%）：Use when 只留产物类型举例（写 PRD/需求文档、API 契约/OpenAPI、建表/schema、页面/组件设计、测试方案等设计产物时），删 phase 括注与 SKIP 冗述；正文路由表不动

### Evidence

评审实测其为第二长；触发关键词由 metadata.triggers 兜底（写 PRD/需求文档/API 契约/OpenAPI/proto 定义/测试方案/user story/设计文档均在）；validate 52/52 通过

## [d-18cf032af733bca8-f85a9715] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-25T09:44:20Z
- **By**: kimi-code

### Diagnosis

上轮 description 压缩删去的触发词中，migration/接口定义/路由设计/测试用例/测试计划在 metadata.triggers 无兜底（proto/建表/schema/OpenAPI 等已有关键词覆盖，不补）

### Revision

triggers keywords 增补 5 个高价值词：migration、接口定义、路由设计、测试用例、测试计划

### Evidence

审查 minor 修复：对比被删词表与现有 keywords 逐词核对；forge skills validate 通过

## [d-18cf05aaa48690e8-ff578b33] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-25T10:30:07Z

### Diagnosis

路由表只覆盖 6 个设计环节，PR 描述/commit/测试报告/复盘四类文档产物无编写期入口（审强写弱：rubric 只在审查侧存在）

### Revision

路由表加文档产物行指向 code-review-gate/references/rubric-docs.md（轻路径：不动 phase_detect.go，不参与 task-verify 路径推断）；写前模板拿 doc-generator、落盘后过 forge docs lint 再按 rubric 评分

### Evidence

docs/design/output-readability-gates.md L2 章节；6 个 phase-*.md 确定性规则表同步追加 L1 lint 规则行

## [d-18d0a1f3c02b4e12-c1d2e3f4] accept

- **Skill**: design-artifact-standards
- **DecidedAt**: 2026-08-28T07:30:23Z
- **By**: zcode

### Diagnosis

本 skill 是纯路由型 skill 却不托管自己的核心资产：phase-*.md ×6 寄居 code-review-gate，靠 `../code-review-gate/references/` 跨 skill 相对链接维持（Kimi 宿主实测断链）+ requires: code-review-gate 安装提示 + 绝对路径兜底说明——无条件消费者依赖条件消费者，依赖箭头反了

### Revision

phase-*.md ×6 git mv 迁入本 skill references/（单一真相源本地化，单装即可完整使用）；requires 改为 doc-review（路由表末行文档产物的软依赖，skill 名引用不深链）；路径锚点/维护注记同步改写；6 个 phase 文件内指回 review-checklist.md 的链接改为 code-review-gate skill 名引用

### Evidence

SKILL.md 原维护注记「跨 skill 引用首例」自证脆弱性；迁移后 forge skills validate --skill design-artifact-standards 过 R1-R17

### Rationale

编写期是 phase 清单的无条件消费者（本 skill 的全部身份），审查期只在 forge 项目有 DesignPhases 时条件加载——资产随无条件消费者走，条件消费者跨引

## [d-18d0a2f111452-e111452f4] accept

- **Skill**: design-artifact-standards
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
