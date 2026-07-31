# ai-ui-generation-workflow — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c6bc8995e44f5c-d526fa8c] accept

- **Skill**: ai-ui-generation-workflow
- **DecidedAt**: 2026-07-29T10:40:01Z
- **By**: claude-code

### Diagnosis

用户全局specialist skill(按Forge规范有metadata.pattern/domain/composes互引成体系)原不在canonical;被frontend-development等引用致断链

### Revision

纳入canonical:从用户全局复制SKILL.md及references到skills/ai-ui-generation-workflow

### Evidence

forge skills validate R1-R11通过;forge skills audit 0 finding;守卫C验证互引自洽

## [d-18c77169ccbc519c-92d1825c] accept

- **Skill**: ai-ui-generation-workflow
- **DecidedAt**: 2026-07-31T17:54:36Z

### Diagnosis

整体审查发现 SKILL.md:76 硬编码本机路径 E:\GitHubForkProject\awesome-design-md（分发后必断链），:188/:196/:207 pixso_import.py 裸引用无出处

### Revision

:76 改可发现式（DESIGN_MD_ROOT 环境变量/工作区查找 + git clone 指引 + fallback 通用模板）；三处 pixso_import.py 注明外部仓库 jiaweiwei1961/pixso-design-skill scripts/pixso_import.py

### Evidence

design-review-snapshot:192-193 已有同款外部仓库标注法可参照；裸路径在非本机环境不存在
