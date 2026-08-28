# doc-review 决策历史

## [d-18d0a1f3c02b4e10-a1b2c3d4] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-08-28T07:30:23Z
- **By**: zcode

### Diagnosis

rubric-docs.md（文档 L2 评分表）寄居 code-review-gate，与代码审查的变化频率/受众不同（SRP 违例）；文档审查纪律散落六处无单一真相源（rubric 文件 + internal/skillgen ×2 / internal/taskpipeline ×2 / internal/cli/task.go 的 Go 字符串内联流程 + design-artifact-standards 路由行 + doc-generator 提及）；「审查文档」意图无路由落点（design-artifact-standards 把产物审查指给纯代码向的 code-review-gate）。规划阶段曾评估 v1 方案（只搬 rubric + 挂 forge 流程）被否：skills-only 用户只能拿到空壳

### Revision

新建 doc-review skill，通用方法论为核（审查协议四要素/Critical 分级/独立派审/文档作弊指纹六类/收敛判据，真实编写而非搬文件）；rubric-docs.md git mv 迁入本 skill references/；Go 字符串瘦身为「按 doc-review skill 评审」——依赖方向倒转：forge 二进制依赖 skill 作为流程真相源；跨 skill 引用指向 skill 名而非深链 references/ 路径

### Evidence

feat/skills-boundary-inversion 规划会话：仓库全量 grep 摸底（15 skill 含 forge 引用/6 处文档审查纪律散落）+ 三组探索 agent 证据（file:line 级清单）；forge skills validate --skill doc-review 过 R1-R17

### Rationale

文档审查是独立于代码审查的真实关注点（自己的 rubric/门禁/失败模式/五类消费者），值得一个家；skill 作为真相源、二进制引用 skill，是依赖倒置的正确方向（具体工具依赖抽象资产）
