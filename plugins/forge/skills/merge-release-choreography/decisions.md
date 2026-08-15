# merge-release-choreography — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18cbf5edf27dc368-cfd0c4e3] accept

- **Skill**: merge-release-choreography
- **DecidedAt**: 2026-08-15T11:07:59Z

### Diagnosis

近两月四源会话挖掘最大单一仪式:合并发版清理分支28次/7项目逐字重打,kimi侧18次/4工作区,release-readiness只管go/no-go不管编排

### Revision

新增pipeline编排skill:8站流程(预检/卫生清扫含借鉴痕迹/commit顺序铁律/merge/tag/registry官方源核查/清分支/装机验证)+真实事故Gotchas

### Evidence

mine-claude 28次/7项目;mine-kimi 18次/4工作区;主会话独立挖掘同模式;三次会话三套验证路径无标准化checklist
