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

## [d-18cffa7913104f18-085d332c] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-08-28T13:16:15Z
- **By**: claude-code

### Diagnosis

SKILL.md/references 含 forge 操作性引用（条件块/forge-integration.md/双路径/模板占位符）——违反 skills 零反向依赖契约（CONVENTIONS §13 R18 硬校验），存量豁免通道要求迁出

### Revision

forge 集成内容整体迁出至 forge 侧 internal/skillintegrate notes/（forge skills integration doc-review 查看，skill-trigger 推荐块附指针）；正文改为工具中立方法论（降级路径升为主路径/宿主机制中性措辞）

### Evidence

forge skills validate 53/53 通过且 R18Grandfathered 清空；TestR18_Grandfathered_Exact 双向卡死通过

### Rationale

依赖单向化：方法论完整留在中立库，forge 增强完整在 forge 侧；forge 用户体验经集成笔记+触发指针承接

## [d-18d25377a5916fa8-59f9808f] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-09-05T04:49:35Z

### Diagnosis

功能聚焦批次一线2：按 skills 价值审计（docs/skills-value-audit-2026-08-02.md）与聚焦决策（docs/plans/feature-focus-2026-09.md）执行拆包/瘦身/引用清理

### Revision

拆包至 plugins/forge-design（设计族 12 个）或教科书瘦身/死机制清理（详见 96e0182 提交）

### Evidence

docs/plans/feature-focus-2026-09.md 决策表 + 审计逐项建议 + 96e0182/b967906 提交

## [d-18d56e4b7f13bdf8-ea75006a] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-09-15T07:34:57Z
- **By**: claude-code

### Diagnosis

L2 评委与产出方模型同源时继承自偏好偏差（arXiv 2410.21819），borderline 分数（70-79）可能被同家族评委单独放行——调研（output-readability-gates-v2.md G3）确认缺少偏差防线条款

### Revision

SKILL.md 步骤 2 增补「评委家族声明」段：评审上下文声明产出方家族，同家族 borderline 须第二评委复核（--reviewer 记两家 id，不一致取低分并各带 delete-list）；rubric-docs.md 评分纪律同步加第 6 条评委偏差防线（详略与篇幅脱钩）

### Evidence

forge skills validate 41/41 通过；rubric 变更为档位锚点文本，L1 lint 0 命中；语义与调研结论一致（评委偏差五类缓解清单）

### Rationale

P0 无 schema 变更的评委去偏纪律，机器强制（CoReview 字段）留 P1 攒分歧率数据后决策

## [d-18d5707b4426ad90-10ca2002] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-09-15T08:15:01Z
- **By**: claude-code

### Diagnosis

独立 L2 评审（round 1，89/100 PASS）六条发现：F1 纪律条款含当前 CLI 不可执行的 --reviewer 双值指令（CoReview 属 P1）；F2 SKILL 两处 L1 范围枚举漏更 D8 结论位置；F3 v1 文档自称现状权威未指向 v2；F4 sweep 命中定性过窄（Rationalization 表→含红线条款）；F5 双评/plain-speech 规则多处手抄；F6 判据两处重复成文

### Revision

F1/F5：SKILL 评委家族声明与 rubric 第 6 条改「两家 id 与分数落档于评审记录（--co-reviewer 为 P1 规划）」+ SKILL 压成指针句；F2：SKILL L32/L39 枚举补结论位置；F3：v1 头部加 v2 增量指针 + v2 加时界；F4：定性改为「借口/禁令示例（Rationalization 表与红线条款）」；F6：P0-A 判据改指针 + 删重复口号

### Evidence

round-1 评审 89/100 PASS 零 Critical；修复后 forge skills validate 41/41、docs lint --base HEAD 6 文件 0 命中、skillgen guard test 绿

### Rationale

Important/Minor 未决项按 doc-review 纪律须显式回应，六条全部采纳修复

## [d-18d576f3942a0340-fb331c82] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-09-15T10:13:35Z
- **By**: claude-code

### Diagnosis

P1-A/B 落地（output-readability-gates-v2.md）：--co-reviewer/--co-score 双评 CLI 已存在（P0 时是规划）；进化判据需要 findings 带 Tag 供 --stats 聚合

### Revision

SKILL 步骤 2 双评指引改用真 flag；步骤 4 四要素→五要素（+Tag 打标：--critical tag:前缀，--stats 聚合）；rubric 第 6 条同步真 flag

### Evidence

forge skills validate 41/41；clitask 端到端测试（CoReviewPersisted/PairingRequired/CriticalTagPrefix/TagValidatedAndStats）全绿

### Rationale

skill 文本与实现同步，消灭「规划中 flag」的悬空引用

## [d-18ddc21499948f70-60bd2a4d] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-10-12T10:40:20Z
- **By**: claude-code

### Diagnosis

设计产物标准指向 forge-design pack 且标「未安装则忽略」，但 design-artifact-standards 已收回核心（提案 D2）；rubric 对技术方案只有通用四维兜底

### Revision

去掉 design-artifact-standards 的 pack 指针；rubric-docs.md 新增技术方案类型特化行（Critical：缺 Non-goals/缺端到端验证/无「什么都不做」备选/数据流边未标注/NFR 无数字/断言无指针/与 accepted ADR 冲突）

### Prediction

技术方案 L2 评审的 Critical 命中集中在新列判据上；评审轮次下降

### Evidence

调研 2026-10-12（R2ABench 2026 / IFScale / ETH AGENTS.md ICLR26 / Spec Kit·Kiro·OpenSpec 模板 / CHI25 Plan-Then-Execute）+ 提案 docs/plans/design-stage-hardening-2026-10-proposal.md；forge skills validate 39/39

### Rationale

设计阶段硬化 W1/W5.3/D2 落地；advisory 起步，按数据窗口复核

## [d-18e18060413fa1a8-f30744eb] accept

- **Skill**: doc-review
- **DecidedAt**: 2026-10-24T15:21:17Z
- **By**: claude-code
- **Warrant**: doc:docs/plans/rereview-resume-docs-2026-10.md

### Diagnosis

打回后复审写死「重新派独立子代理评审」，每轮冷启动，3 轮上限下最多 3 个冷启动评审者

### Revision

步骤 2 补派审前留存被评审版本；步骤 5 分增量复核（续用首轮评审者，只喂上轮 findings + 修订 diff，对抗立场不变，四维分以上轮为底）与全量评审（重写结论、大改多文档、即将第 3 轮）两档，禁 fork 作者会话；同家族 75-79 放行前另派第二评委

### Evidence

独立性来自评审者看不到作者推理，不来自冷启动：https://arxiv.org/abs/2603.16244 与 https://arxiv.org/abs/2603.12123 是代码评审实验，外推到文档的依据是同一机制，文档侧收益未验证

### Rationale

与 code-review-gate 已有的复审分档同口径；文档侧每个任务最多续用 1 次，收益可能很小，批准范围是口径统一
