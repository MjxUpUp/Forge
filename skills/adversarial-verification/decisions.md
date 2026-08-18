# adversarial-verification — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18cbf5f3fccc93d4-49d4c9c9] accept

- **Skill**: adversarial-verification
- **DecidedAt**: 2026-08-15T11:08:25Z

### Diagnosis

红蓝对抗10次/2项目含2次用户纠正(必须锚外部证据禁止权重自辩驳)+pi侧对抗验收实录(文档比对式验收假绿)

### Revision

新增reviewer双协议skill:协议A辩论锚外部源三判据/协议B逐点实跑+断裂扫描+双路径单一真相源

### Evidence

mine-claude 10次+2纠正;mine-pi验收驳斥实录;与review-pass-requires-real-review/single-source-flattery教训同构

## [d-18cbf713f97d47d4-78938410] accept

- **Skill**: adversarial-verification
- **DecidedAt**: 2026-08-15T11:29:02Z

### Diagnosis

研究族合并连带引用修复:fact-research/web-search-bridge 已并入 research-workflow,本skill对二者的 SKIP/分工/降级链引用悬空

### Revision

引用改指 research-workflow 轻量档(Phase L)/通用搜索桥接节;dev-lookup 的 curl-sourcing 相对路径改 ../research-workflow/

### Evidence

forge skills validate 51/51 + TestSkills_NoDanglingSkillRefs 守卫

## [d-18ccc6cfc3468568-bc455661] accept

- **Skill**: adversarial-verification
- **DecidedAt**: 2026-08-18T02:55:47Z
- **By**: claude-code

### Diagnosis

meta-review 发现协议 A 在触发观测 v2 辩论中暴露 4 个结构性缺口：(1) 翻转裁决的三条论点全部来自子代理转述源，未标记二手未抽查（转述自辩驳，单源奉承变体）；(2) 复检轮只查单点辩护过度，不查修复间矛盾——隐私修复 sessionID 入盐破坏同轮挖矿的跨 session 去重，漏网到 meta-review 才被抓；(3) 没人质询决策相关性（126 条总量下 per-keyword 分析层值不值得建）与统计功效；(4) 优先攻击最弱引用的排序有偏——本次最有效两击打的恰是看似最硬的假代码断言

### Revision

SKILL.md：判据增至 4 条（新增二手转述标记+抽查，翻转裁决先抽查后定）；流程增至 6 步（攻击排序改为先打强措辞且可机械复核的假硬断言；新增决策相关性+统计功效两问；新增第 6 步修复间一致性检查——新修复两两对照下游机制）；自查清单+3 项；Gotchas 表+4 行（转述自辩驳/假硬断言/决策相关性盲区/修复间矛盾），全部带 2026-08-18 会话真实反例锚点

### Prediction

下次红蓝对抗辩论的 meta-review 中：二手源标记覆盖率应为 100%（0 条未标记）；修复间矛盾应为 0 条漏网

### Evidence

基线失败（TDD RED）= 本会话辩论实录：D7×P2 盐矛盾在旧版 skill 的单点复检下漏网、由事后 meta-review 抓出；二手源 3 条零标记、事后抽查 2 过 1 不可复核。修订后规则逐条对应该失败集。forge skills validate+audit 通过；skill-anti-degradation-check.sh 干净

### Rationale

四条修订各自对应一个已发生的最小反例（不是预防性 speculation）；skill-evolution 边界内的行为契约补强，未引入新流程依赖；下轮使用本 skill 的辩论即回归验证场
