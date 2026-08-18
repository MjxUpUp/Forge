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

## [d-18ccd4d49801c788-a2d7ca5b] accept

- **Skill**: merge-release-choreography
- **DecidedAt**: 2026-08-18T07:12:41Z
- **By**: kimi

### Diagnosis

skills-qa R10 advisory：description 含工作流总结词「全流程」（CSO 规则：description 只说 what+when，总结工作流会让模型照 description 跳过 body）

### Revision

SKILL.md description 首句「合并发版收尾全流程编排」删「全流程」三字，其余语义（8 站枚举+Use when+SKIP）原样

### Evidence

forge skills validate --canonical ./skills 复跑 52 通过 0 失败、该 advisory 消失

### Rationale

唯一命中的 CSOWorkflowMarkers 词，最小删除即合规；不改动路由语义

## [d-18ccd749b15436b8-5f53c00f] accept

- **Skill**: merge-release-choreography
- **DecidedAt**: 2026-08-18T07:57:43Z
- **By**: kimi

### Diagnosis

契约变更型重构后单元测试全绿但 e2e 场景仍断言旧契约：user-level-assets 重构后 Nightly verify --regression 4/4 全红（场景钉的是重构前的项目级布局），重构完成标准缺「e2e 期望审计」一环

### Revision

SKILL.md S1 预检加一条：契约变更型重构后 e2e/回归场景的期望本身要同步审计，列入重构收尾完成标准

### Evidence

2026-08-18 Nightly run 32098614969 四场景失败根因分析 + 场景重写后本地 4/4 PASS

### Rationale

代码全绿≠场景在断言新契约，该类事故每次命中就是一整轮 CI 红
