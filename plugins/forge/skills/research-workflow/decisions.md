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

## [d-18dea1c73935d830-b2f78292] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-10-15T06:59:38Z
- **By**: claude-code
- **Warrant**: doc:docs/surveys/2026-10-research-workflow-retrospective.md

### Diagnosis

2026-10 复盘：23 次深度调研、31 条承重断言回原文核验，3 条不支持、7 条部分支持；失真全在转述链（删 hedge、丢场景限定、两源拼结论）；高信度按 worker 数计导致循环互证；质量门全靠散文自查致静默降级（完整撒网 1/23、Phase L 0 次使用、自报搜索量虚高 23-60%）；答相邻问题、外部结论直接变本地建议

### Revision

新增 references/credibility-protocol.md（研究契约 brief、来源族信度、限定列、全模式反证、claims 隔离核验、本地适用性、status.json/search_log、停止规则）与 references/phase-l.md；新增 scripts/research-lint.sh 交付前确定性检查与 evals/output-cases.json 六类陷阱 golden 集；engine 改 6 段 worker 契约与来源族四档；去掉 ≥60/≥110 搜索下限；飞书发布改为用户要求时；建目录改两步字面路径；failure-cases 补案例 7-10

### Prediction

下一批深度调研 run 的承重断言回原文支持率从 68% 升到 ≥85%，lint 0 ERROR 交付率 100%，Phase L 读取次数 >0

### Evidence

internal/skillsqa/research_workflow_test.go：lint 对 13 类陷阱逐条检出、接线与 golden 集结构钉住；forge skills validate 43/43

### Rationale

复盘根因是关键门只有散文规则加同一 agent 自查；能脚本化的改 lint，不能脚本化的交给隔离上下文核验子代理

## [d-18dfe1ab6b0fde30-38817d57] accept

- **Skill**: research-workflow
- **DecidedAt**: 2026-10-19T08:41:43Z
- **By**: claude-code

### Diagnosis

调研产物散落三处：~/.forge/research 残留旧 run、~/.research-workflow 为当前 run_dir、Forge docs/surveys 收进与 Forge 无关的通用行业调研（2 份与 run_dir report.md 逐字节重复）。根因：SKILL.md 未声明成稿唯一归宿且称 run_dir 会被清理，下游落库条件只写要写进仓库文档时，agent 按 cwd 落库

### Revision

建 run_dir 节声明 ~/.research-workflow 为唯一归宿、长期保留、一个 run 一篇 report.md；Phase 4 下游落库改为仓库落库，两条可判定条件（仓库文件已 grep 到对该调研的引用 + brief 服务项目字段等于仓库名），文件头写 ~/ 形式源路径；brief.md 加服务项目字段；status.json 示例 downstream 改 []；failure-cases 案例 6 去掉本地清理前提

### Evidence

本机盘点：~/.forge/research 10 run（8/18-9/29），~/.research-workflow 16 run；Forge docs/surveys 未跟踪 3 份通用调研，其中 2026-10-ai-native-sdlc-weekly-1012-1019.md 与 2026-10-decision-models-ai-native-sdlc-survey.md 分别与 ai-native-sdlc-weekly-20261019-1508、jev-sdlc-20261012-1119 的 report.md 逐字节相同（cmp 确认）；用户 2026-10-19 选定 ~/.research-workflow 为统一位置并迁移历史产物

### Rationale

run_dir 已是 skill 自有命名空间且全量过程文件在此，以它为唯一归宿不需另起目录；仓库副本只为解决 ~/ 路径跨人跨机失效，因此只在仓库文档确有引用时复制，条件写成 grep 与字段比对，避免 agent 按 cwd 自行判断；姊妹篇方案因 research-lint 只扫 report.md 会漏检而放弃

## 历史来源：fact-research / web-search-bridge 合并墓碑

2026-10-22 两个退役目录删除前迁入。两者的其余决策已于 2026-08-15 并入本文件（见上方「以下为并入的历史决策归档」一节），此处只补原目录独有的合并墓碑条目，按 `skills/CONVENTIONS.md`「退役 skill 存档」把决策标题降到四级（放在「来自 <旧名>」小节下）——两条共用同一 ID，不作为本 skill 的决策解析；正文逐字保留。

### 来自 fact-research

#### [d-p2-merge-20260815] accept

> ⚠️ 本 skill 已于 2026-08-15（任务 feat/skill-hitrate-optimization，P2 研究族合并）整体并入 **research-workflow**——本文件是合并墓碑，后续维护与决策记录在 `skills/research-workflow/decisions.md`。

- 诊断: fact-research 与 research-workflow 职责重叠（轻量事实核查 vs 深度调研），三层调研量级路由表需要单一真相源，两 skill 并存导致 dev-lookup/证据链引用摇摆
- 修订: 整体并入 research-workflow「量级判定与轻量档（Phase L）」节（三层量级表+4步协议）；SKILL.md 删除；本决策历史归档至 research-workflow/decisions.md
- 证据: cross-skill 引用（evidence-based-proposal/design-artifact-standards/adversarial-verification/dev-lookup）已全部改指 research-workflow Phase L；引用完整性 grep 零残留
- 结果: accept（合并落地）

### 来自 web-search-bridge

#### [d-p2-merge-20260815] accept

> ⚠️ 本 skill 已于 2026-08-15（任务 feat/skill-hitrate-optimization，P2 研究族合并）整体并入 **research-workflow**——本文件是合并墓碑，后续维护与决策记录在 `skills/research-workflow/decisions.md`。

- 诊断: web-search-bridge（通用搜索 API 桥接）只被调研族消费，独立成 skill 导致触发与引用碎片化，降级链末位指向两跳
- 修订: 整体并入 research-workflow「通用搜索桥接」节（Tavily→Serper→Brave→Exa 路由+jq body 构造+配额预检脚本）；SKILL.md 删除；scripts/references 迁至 research-workflow/ 下
- 证据: dev-lookup 降级链已改指 research-workflow「通用搜索桥接」；本决策历史归档至 research-workflow/decisions.md
- 结果: accept（合并落地）
