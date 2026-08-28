# dev-workflow — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7729c47497394-3fb15ac0] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-07-31T18:16:32Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a652b9e2e0-1c759957] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

forge task 教程下沉 references/forge-integration.md；pattern-guide.md 归位 skill-authoring-standard/references；去「每文件≤10行」机械硬指标

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620a1ab0008-459b07e3] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca07010d131c88-2b6500b3] accept

- **Skill**: dev-workflow
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

## [d-18ccd74545641f08-2ac579e4] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-08-18T07:57:24Z
- **By**: kimi

### Diagnosis

验收 Run 写成 shell 形式（PWD=... go run）必败：verify-acceptance 的 RunTestCommand 按空白切分直接 exec 不经 shell，输出对也判负，排查链长（2026-08-18 case-split 验收 2/7 因此挂）

### Revision

SKILL.md 验收标准格式节补「Run 命令必须 shell-free」：前缀赋值/管道/重定向/&& 不可写，环境变量用 env VAR=... 形式，组合命令拆多条

### Evidence

taskpipeline/testrun.go:46 strings.Fields+exec.Command 源码实证；修正为 env 形式后 verify-acceptance 7/7 全绿

### Rationale

每个走 dev-workflow 写 Run/Expected 的任务都会撞，属高频坑

## [d-18d0a2f105518-e105518f4] accept

- **Skill**: dev-workflow
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

## [d-18cffa790534b6b8-efd00424] accept

- **Skill**: dev-workflow
- **DecidedAt**: 2026-08-28T13:16:14Z
- **By**: claude-code

### Diagnosis

SKILL.md/references 含 forge 操作性引用（条件块/forge-integration.md/双路径/模板占位符）——违反 skills 零反向依赖契约（CONVENTIONS §13 R18 硬校验），存量豁免通道要求迁出

### Revision

forge 集成内容整体迁出至 forge 侧 internal/skillintegrate notes/（forge skills integration dev-workflow 查看，skill-trigger 推荐块附指针）；正文改为工具中立方法论（降级路径升为主路径/宿主机制中性措辞）

### Evidence

forge skills validate 53/53 通过且 R18Grandfathered 清空；TestR18_Grandfathered_Exact 双向卡死通过

### Rationale

依赖单向化：方法论完整留在中立库，forge 增强完整在 forge 侧；forge 用户体验经集成笔记+触发指针承接
