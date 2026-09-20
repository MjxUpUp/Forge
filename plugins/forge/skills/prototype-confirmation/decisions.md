# prototype-confirmation — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c771eb6a0d97e0-757a4364] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-07-31T18:03:53Z

### Diagnosis

整体审查发现 frontmatter composes 用标量 'a, b' 写法，与 CONVENTIONS §4 及库内其他 skill 的 flow list 不一致

### Revision

composes 改 flow list [evidence-based-proposal, implementation-discipline]

### Evidence

库内多数 skill 用 [a, b] flow list；CONVENTIONS §4 规定该格式（另一 agent 同步修 CONVENTIONS）

## [d-18c7e620aa5fe038-216e6119] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18c7e7b34ebf7f10-3ca62f41] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-08-02T06:02:14Z

### Diagnosis

composes 与 implementation-discipline 互引成环（机检全库发现的第二对）

### Revision

composes 移除 implementation-discipline（保留调用方 implementation-discipline→本 skill 单向 compose）；确认后进入实施链的衔接正文 :128/:132 已有文本

### Evidence

task-complete 审查子 agent 发现（旧账）；全库 composes 机检

## [d-18cc4bedd168b4c0-c5d66551] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-08-16T13:23:56Z

### Diagnosis

同UI族:原型确认流程请求无触发

### Revision

metadata.triggers新增UserPromptSubmit关键词(出原型/原型给我看/让我先看/先看效果/确认形态/做个原型/原型确认),cooldown 600

### Evidence

原型先行请求高频,触发覆盖缺口

## [d-undloop-b2-20260920] accept

- **Skill**: prototype-confirmation
- **DecidedAt**: 2026-09-20T18:00:00Z
- **By**: feat/understanding-loop（理解侧闭环批次）

### Diagnosis

确认导出是终点站——用户逐项认可的原型确认结果只进对话，不进考卷：被✅认可的形态没有编译成验收标准，实施后的"与确认一致"无机械对账（理解对账物与执法考卷断链，人-AI 沟通调研第③步"样例即验收测试种子"无落地）。

### Revision

模板 DEMOS 条目新增可选 accept 字段（"命令 :: 期望输出子串"）；exportConfirm() 在导出文本末尾追加 ```accept 围栏——仅✅认可且带声明的功能点进围栏（✏/❌/未勾不进，被推翻的形态不该成为考卷）；SKILL.md 步骤 3 硬要求加第 5 项（可编译考卷段）、步骤 5 加考卷编译出口（确认文本落盘 spec 产物 → 提取为 spec-extract 层考卷）。判定行格式不动（grep -cE 核对不受影响）。

### Evidence

导出文本末尾围栏 = 验收产物提取通道的声明形态（evals：task_chain_test 的 --extract 端到端）；围栏行仅收✅认可的 accept 声明，围栏内不掺注释行（提取器逐行当命令）。
