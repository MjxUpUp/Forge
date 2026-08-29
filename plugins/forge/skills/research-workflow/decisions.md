# research-workflow — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7e41b47482098-ced1e77a] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-02T04:56:23Z

### Diagnosis

审计发现：模型降级链写死个人栈型号（glm-4.6/doubao/deepseek/glm-4-flash）已陈旧且对他人无意义；SKIP 节三层调研量级路由表为 4 份复制之一，drift 已开始

### Revision

模型降级链改通用表述（按当前可用模型配置降级链，主力模型→任一可用备选，不写死具体型号）；SKIP 节三层量级表删除，改指针指向 fact-research「三层调研量级（路由依据）」节（canonical）

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e62b2368ab04-0d03f50d] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 边界补全 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca0700edf20ae4-bdb77a6f] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-09T03:58:22Z
- **By**: claude-code

### Diagnosis

该 skill 无声明式 trigger，纯靠 agent 自觉加载——dogfood transcript 证明 0 命中，skill 形同被动文档从未注入

### Revision

在 SKILL.md frontmatter metadata 加 triggers 声明（事件 + keywords 或 when condition + cooldown），让 skill-trigger 框架在匹配事件时主动注入加载指引

### Evidence

forge skills validate R1-R17 全 49 通过；trigger 覆盖 5→15（31%）；dry-run 验证 research-workflow/secure-coding 匹配 prompt 正确触发

### Rationale

扩展 trigger 覆盖是 2026-08 审计 P1 优化项；声明式触发是把 skill 从被动文档转主动注入的唯一可靠手段（见 dogfood 发现）

---

# 以下为并入的历史决策归档（fact-research / web-search-bridge，2026-08-15 合并进本 skill；保留原文供审计回溯）
## [d-18c7e4296e9423b4-391dde48] accept

- **Skill**: fact-research
- **DecidedAt**: 2026-08-02T04:57:23Z

### Diagnosis

审计发现：通道状态表（HN ✅/Wikipedia ❌/Jina ❌）是作者本机网络实测写成通用事实，且对多个源断言「别用」过于绝对

### Revision

通道状态表移至 references/curl-sourcing.md（与 dev-lookup 共用），明确标注「特定网络环境实测，非通用结论」并补自检方法；「别用」软化为「该环境下不可用，自检后再定」；主文件改指针；三层调研量级权威表按裁决保留在本 skill「三层调研量级（路由依据）」节

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计

## [d-18c7e62b275fc9b8-1f958a3f] accept

- **Skill**: fact-research
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md

---
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

## [d-18cbf71018b8c320-e20108ba] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-15T11:28:45Z

### Diagnosis

研究族三skill割裂:fact-research(轻量核查)/web-search-bridge(搜索桥接)/research-workflow(深度档)互跳频繁且三层量级真相源在fact-research被双向依赖

### Revision

合并49->47:fact-research并入为Phase L轻量档(三层量级表迁为本节真相源),web-search-bridge并入为通用搜索桥接节(references/provider-curl-examples+scripts/web-search-quota.sh随迁);curl-sourcing.md迁入;历史decisions归档至本skill decisions.md

### Evidence

skills-hitrate-review-2026-08-15 P2研究族合并;同类先例frontend合并(37->14)

## [d-18cff80e4e20041c-81104537] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-08-28T12:31:57Z
- **By**: claude-code

### Diagnosis

run 隔离目录与配额统计硬编码 ~/.forge/ 命名空间（SKILL.md:115、deep-research-engine.md:51、web-search-quota.sh:23 默认值）——skills 零反向依赖违例（CONVENTIONS §13 R18），且全仓无任何 forge 命令读写该路径（纯命名空间侵占）

### Revision

三处统一改为 skill 自有命名空间 ~/.research-workflow/（run 目录与 web-search-stats 同根）；web-search-quota.sh 保留 WEB_SEARCH_STATS_DIR env 覆盖

### Evidence

grep 确认 forge Go 代码零处读写 ~/.forge/research 与 web-search-stats；修改后 forge skills validate 通过

### Rationale

照 doc-generator 双路径先例（~/.doc-generator/）；路径无 forge 消费方，改动零迁移成本
