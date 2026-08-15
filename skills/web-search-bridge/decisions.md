# web-search-bridge — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e4845bbda9a8-755fd683] accept

- **Skill**: web-search-bridge
- **DecidedAt**: 2026-08-02T05:03:54Z

### Diagnosis

审计发现：脚本与文档不一致（usage.json 只有 tavily/serper/exa 三个 key，Brave 无统计；额度预检表同漏 Brave）；233 行偏长；Tavily GET /usage 说法未能公开核实；三层量级分层图为 4 份复制之一

### Revision

脚本补 Brave 统计 key + check_brave（无预检能力，同 Exa 被动应对）+ usage 文案；SKILL.md 预检表补 Brave 行；Tavily /usage 与中文查询坑标注为作者联调实测（2026-07 前、具体日期未留痕、以 check 实测为准）；provider curl 示例拆 references/provider-curl-examples.md；三层分层图与重复的配置检查节删除改指针（canonical：fact-research「三层调研量级（路由依据）」节）；主文件 233→158 行

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e62b30059cc8-406267e0] accept

- **Skill**: web-search-bridge
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-p2-merge-20260815] accept

> ⚠️ 本 skill 已于 2026-08-15（任务 feat/skill-hitrate-optimization，P2 研究族合并）整体并入 **research-workflow**——本文件是合并墓碑，后续维护与决策记录在 `skills/research-workflow/decisions.md`。

- 诊断: web-search-bridge（通用搜索 API 桥接）只被调研族消费，独立成 skill 导致触发与引用碎片化，降级链末位指向两跳
- 修订: 整体并入 research-workflow「通用搜索桥接」节（Tavily→Serper→Brave→Exa 路由+jq body 构造+配额预检脚本）；SKILL.md 删除；scripts/references 迁至 research-workflow/ 下
- 证据: dev-lookup 降级链已改指 research-workflow「通用搜索桥接」；本决策历史归档至 research-workflow/decisions.md
- 结果: accept（合并落地）
