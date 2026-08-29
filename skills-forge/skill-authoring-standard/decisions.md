# skill-authoring-standard — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e438dc639154-6bb67968] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-02T04:58:30Z

### Diagnosis

审计发现：正文引用 R1-R11 但全文从未列出定义（规范依赖外部黑盒）；291 行超自定的 progressive disclosure 要求；自身违反自定的 token 效率要求（约 2900 字），自我违反程度批内最高

### Revision

新建 references/validation-rules.md 落 R1-R11+R12 文本定义（从 internal/skillsqa/registry.go AuditSkill 导出，标注真相源以代码为准）；四类 skill 测试法拆 references/testing-patterns.md、设计模式表拆 references/design-patterns.md、内容结构/目录结构模板拆 references/structure-template.md、防注水自检详情拆 references/anti-degradation.md；正文压缩冗长示例，291 行瘦身至 177 行（≤200 达标），自我违反随瘦身修正

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计（skill-authoring-standard 详评 + 改进清单项 4/9）

## [d-18c7e5ce4a9ec760-b7f76f57] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-02T05:27:31Z

### Diagnosis

pattern taxonomy 缺经验参考型取值，integration-test-architecture 类 skill 被误标 tool-wrapper

### Revision

ValidPatterns 新增 reference 合法值；validation-rules.md R7/design-patterns.md/SKILL.md 同步；R1-R11 文案更新为 R1-R17；挂载 skill-pattern-guide.md 链接并注明与 design-patterns.md 分工

### Evidence

docs/skills-value-audit-2026-08-02.md；批次C 修正触发

## [d-18c7e6076f568708-4e301541] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-02T05:31:36Z

### Diagnosis

项11 rubric 机制库内化：机器校验 R1-R17 管不了质量维度，需人工/LLM 评审 rubric

### Revision

新建 references/quality-rubric.md（四维评分+0/10/20/25锚点+总分<75合并阻断线），SKILL.md 验证节+参考节挂载

### Evidence

docs/skills-value-audit-2026-08-02.md；业界调研 grafana/skills 四维 rubric（conciseness/actionability/workflow clarity/progressive disclosure）

## [d-18c7e62b3fb9e8e0-4679c2a6] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 边界补全 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18c7e7627f24fa00-9595f2b6] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-02T05:56:27Z

### Diagnosis

提交前审查发现 validation-rules.md 出生即过时: 标题 R1-R11、缺本提交新增的 R13-R17，与 SKILL.md 两处指针文案矛盾

### Revision

补 R13-R17 五条定义(硬: R13/R14; advisory: R15/R16/R17)、标题改 R1-R17、注明 R12-R17 为 forge 本地扩展

### Evidence

task-complete 前 code-review-gate 子 agent 审查 (fix 级发现)

## [d-18cbf6ad4b048e44-2375dd15] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-15T11:21:41Z

### Diagnosis

无通道skill命中率审查:该skill无triggers纯靠自觉路由,真实用户语料存在明确触发词

### Revision

metadata补triggers(keywords/cooldown;skill-authoring-standard用新condition skill_file_touched;doc-generator/system-architecture补词修订)

### Evidence

skills-hitrate-review-2026-08-15:四源425会话挖掘语料+trigger覆盖10%缺口

## [d-18ccd74544cc7ea0-7299f220] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-18T07:57:24Z
- **By**: kimi

### Diagnosis

triggers 的 keywords/when 语义陷阱非显然且 R12 查不出：PreToolUse match Agent|Task 配 keywords = 三匹配源（prompt/command/stdout）全空，永不触发；validation-rules.md 的 when 词表还漏了 skill_file_touched（与 rules.go drift）

### Revision

references/validation-rules.md：R12 行补 skill_file_touched；新增「triggers 匹配语义陷阱」小节（keywords 匹配源、工具门禁类用 when、GitHub slugger 双连字符锚点）

### Evidence

2026-08-18 subagent-orchestration trigger 修复时的引擎源码核实（skilltrigger sourceText）；forge skills validate 复跑 52/0

### Rationale

每个写 triggers 的人都会踩，论证过一次不该再论证第二次

## [d-18cd6590d6016e88-d37050c9] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-20T03:24:59Z

### Diagnosis

SKILL.md 清单自述「22 条安全规则」与实现不符（audit.go auditRules 实为 21 条）

### Revision

22 条安全规则 → 21 条安全规则（纯计数修正，行为规范不变；references 两处同步）

### Evidence

grep -o 'ID: "[^"]*"' internal/skillsqa/audit.go | wc -l = 21（PI5+DE4+SL2+DC10）

## [d-18cf01f526945d68-9d85a5cb] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-25T09:22:09Z
- **By**: kimi-code

### Diagnosis

description 规范只有 ≥80 字符下限无上限约定，全库 52 个 skill 有 3 个超 450 字符（均值 317）

### Revision

「其他内容原则」补 ≤350 字符软上限条款（R4 硬上限 1024/>500 advisory 之下的自律线）及超限时自查指引（枚举下沉正文/不重复正文清单/不堆近义词）

### Evidence

CONVENTIONS §5 同步写入同一软上限；validate 52/52 通过

## [d-18d0a2f117846-e117846f4] accept

- **Skill**: skill-authoring-standard
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

skill 本身描述 forge 命令族操作方法论，无法也不应工具中立化

### Revision

frontmatter 加 metadata.requires_forge: "true" 标记（CONVENTIONS §13 形态③），R18 按标记豁免、skills-only 分发按标记过滤

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令
