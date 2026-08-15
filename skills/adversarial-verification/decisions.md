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
