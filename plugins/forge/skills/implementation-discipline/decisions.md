# implementation-discipline — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c613d1fc2dae90-11dfc5f4] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-07-27T07:08:14Z
- **By**: claude-code

### Diagnosis

接入通用 skill-trigger 框架(feat/skill-trigger): 声明式 metadata.triggers 让通用 hook 在事件点主动驱动本 skill, 解决 dogfood 量化的质量/流程 skill 显式触发=0、靠 agent 自觉必漏问题(code-review-gate 因有 review-stop hook 独活)

### Revision

metadata.triggers 加 [{"event":"UserPromptSubmit","when":"coding_intent"},{"event":"Stop","when":"source_changed_uncommitted"}]

### Evidence

dogfood-findings-2026-07-09(testing×17 全低分, 质量 skill 0 显式触发) + plan flickering-bubbling-bonbon.md(triggers schema 表)

## [d-18c7729c67986c54-986df64a] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-07-31T18:16:33Z

### Diagnosis

复审发现 composes 标量写法库内 11 处分裂（此前只统一了 2 处），且原决策证据声称多数已是 flow list 与事实相反——一次性根治

### Revision

composes 标量逗号写法改 flow list [a, b]，对齐 CONVENTIONS §4

### Evidence

grep 确认全库 composes 已无标量残留；forge skills validate 50/50

## [d-18c7e5a6572b4e90-42ba6935] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

改名符号 grep 规则改指针(D4)

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620a496fc68-87ddc41d] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cbf6dc6346cf38-dfecd930] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-08-15T11:25:03Z

### Diagnosis

三层冗余:阶段3/4断言清单与test-discipline铁律1/2/3逐字重复,提交时刻双注入(本skill阶段4+test-discipline trigger)正文漂移风险

### Revision

阶段3断言禁令改指针+场景清单指向铁律2,阶段4清单收敛为双指针(test-discipline铁律3=提交时刻唯一执行通道,code-review-gate=调用方检查)

### Evidence

skills-hitrate-review-2026-08-15 P2去重项;同构先例:已对code-review-gate做唯一真相源指针

## [d-195a3e4405ee-8b45f228] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-08-20T06:37:11Z
- **By**: kimi

### Diagnosis

kimi 看板盲区半修（2026-08-19 hostcap e2db347）：策略"kimi skill-trigger 仅 UserPromptSubmit"散在 wiring（agentbridge manifest 过滤器）与 runtime（cli bail）两层，修复只改 runtime 层；被重写的旧注释明写过滤器存在却未核查；验证停在引擎单测（机制表面），从未在看板/manifest（目标表面）验证，钉旧策略的守卫测试全绿掩盖半修——用户连遇两天"5 条事件"，体验割裂

### Revision

阶段1 确认清单 4→5 件：新增"改语义/策略先列领域全链、逐环 grep 旧策略拷贝；重写注释必核查其中组件引用；策略单一来源各层派生（防御纵深仅限永真不变量）"；阶段3 门控新增"验证表面=目标表面"（修复声明的表面必须亲眼端到端验证，机制层绿灯≠目标达成）+红线；Rationalizations/Red Flags/Gotchas 各补一条本次实例

### Evidence

fix/kimi-skilltrigger-manifest-wiring（b4a0a27, 98/A Strong）：实跑 dashboard API 复现 5 事件；已装 v1.38.0 插件 manifest 仅 1 条 skill-trigger 绑定；守卫测试改全 spec 对齐+per-event 存在性断言

## [d-18ce9f4e41bf6024-76d42016] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-08-24T03:14:20Z
- **By**: zcode

### Diagnosis

自指性证据失效：audit 在追加 decisions.md 前测得 9 到 0，随后追加的 Diagnosis 逐字引用被移除命令形态，提交树实际 9 到 3——证据写下的那一刻就过期，被独立只读审查拦下而非流程自觉

### Revision

Gotchas 增补：证据的测量时刻必须与声称时刻同态——先改资产再写证据的流程，写完证据必须复测一次才允许声称；独立上下文审查是这类自指失真的最后防线。配套结构修复：skillsqa ScanSkill 对 decisions.md 豁免 MdAlso 三规则（本任务另一提交）

### Evidence

fix/dc10 会话回顾 + 只读审查 I-1 项（ad137cd 修复）；ScanSkill 豁免测试 TestScanSkill_DecisionsMdExemptFromMdOnly 四向钉死

## [d-18cea1966210af14-58d62967] accept

- **Skill**: implementation-discipline
- **DecidedAt**: 2026-08-24T03:56:09Z
- **By**: zcode

### Diagnosis

复审 I-1：decisions.md 豁免初稿按 basename 匹配任意深度且连 CRITICAL DC-8/9 一起豁免——与自述刻意收窄矛盾，且给安装门禁的阻断性供应链规则开了文件名后门

### Revision

边界收紧：仅 skill 根级（rel 等于 decisions.md）豁免、仅 DC-10（MEDIUM advisory——事故 FP 全部源于它）；DC-8/9 对 decisions.md 继续扫（阻断安装的 FN 代价远大于 FP 代价）；references 下同名文件全量扫描

### Evidence

测试升级为六向钉死（根级豁免 + SKILL.md/references 星号/references 同名文件/DC-8 照扫/注入照扫）；audit 复扫 0 finding；全量 43 包绿

## [d-18d0a2f117664-e117664f4] accept

- **Skill**: implementation-discipline
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
