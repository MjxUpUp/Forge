---
name: research-workflow
description: "调研全量级流水线：轻量事实核查（≤20 查询、≥2 个独立来源交叉、inline 带引用答案，不出报告）到深度调研报告（研究契约、多 worker、承重断言独立核验、run_dir 报告，可选飞书发布）。Use when: 查市场份额/融资金额/产品对比（A vs B 怎么选）/技术最新进展/某事实核实（需多源交叉但不出报告）、用户说\"调研XXX/深度调研/补充调研/调研并发布\"、需要输出结构化调研报告、给定文件要求\"只看文件/基于这些调研\"、要把调研结果发到飞书、定向源查不到需要桥接通用搜索 API 时。SKIP: 单点技术问题（API 签名/报错含义/库用法/版本兼容→dev-lookup）、纯技术方案设计（用 evidence-based-proposal）、按模板生成 PRD/周报等结构化文档（用 doc-generator）、纯对话不产出调研时。"
metadata:
  pattern: pipeline + gate
  domain: research
  source: merged from deep-research + research-workflow + research-and-publish + fact-research + web-search-bridge
  triggers: [{"event":"UserPromptSubmit","keywords":["做调研","技术调研","调研一下","深度调研","调研并","research the","do research","市场份额","怎么选","对比一下","vs 哪个","最新进展","事实核实","交叉验证","融了多少","全网搜","Google 一下"],"cooldown":300}]
---

# Research Workflow — 调研全流程

把"研究契约 → 搜索素材 → 交叉验证 → 独立核验 → 结构化成文 → 交付"串成一条链路。深度调研方法论（4 路由 / 维度切分 / 来源族信度 / 矛盾消解）是 Phase 1 内核；防偏题与可信度机制在 `references/credibility-protocol.md`，交付前用 `scripts/research-lint.sh` 做确定性检查。

详细规范分置 references：
- **`references/phase-l.md`** — 轻量档完整步骤（执行轻量档时必须读）
- **`references/credibility-protocol.md`** — 研究契约 brief、来源族信度、限定条件、反证、承重断言核验、本地适用性、status.json、停止规则（深度档全程必须读）
- **`references/deep-research-engine.md`** — Phase 1 引擎：路由判定、工序、worker prompt 契约、产出契约
- **`references/sourcing-toolkit.md`** — worker 采集工具路由（agent 能力分级、降级链、JS 渲染源终止判定）
- **`references/curl-sourcing.md`** — curl 定向源通道实测状态、自检方法、命令模板
- **`references/provider-curl-examples.md`** — 通用搜索桥接四 provider 的 curl 模板
- **`scripts/research-lint.sh`** — 交付前确定性检查；**`scripts/web-search-quota.sh`** — 搜索 API 额度预检
- **`references/lark-publish.md`** — 飞书发布命令与异常处理（用户要求发布时）
- **`references/versioned-rewrite.md`** — 补充调研的版本化融入
- **`references/failure-cases.md`** — 真实失败案例

## 对外措辞

工序号、模式名、契约名、文件路径、信度档、全局编号都是**内部标签**，不在用户可见处出现（chat 消息和最终交付物）。对外只汇报实质动作与产出，用自然白话——内部仍按 Phase 记录。`research-lint.sh` 的 E6 会扫报告里的泄漏。
- 别说："这是典型的撒网模式，按工序来"
- 改说："先把全景摸广，再分维度深挖"

## 开工纪律（贯穿全 Phase）

- **取当前时间**：`date '+%Y-%m-%d %H:%M'` 拿系统时间，不假设（预训练知识可能过期）
- **时间窗硬约束**：query 含"2026 Q1 / 近半年 / 最新 / 当前"等时间指向时，窗口当死线——搜索打到该窗内；窗外发现明示"[超窗]"并说明时段
- **搜索前不编断言**：任何事实声明都要等搜索结果落地
- **搜索语言匹配用户语言**：用户中文 → 中文源优先；英文 → 英文源
- **全程 `[^N^]`**：任何外部事实必带 inline citation，定义行带 URL + 日期
- **本机旧调研报告只当线索**：引用前回到它的一手源，旧报告本身不算证据（旧错误会被新 run 转引）

---

## 量级判定（三层调研量级，路由唯一真相源）

| | dev-lookup | **本 skill 轻量档（Phase L）** | 本 skill 深度档（Phase 0-6） |
|---|---|---|---|
| 场景 | API/报错/库用法 | 数据/对比/进展/事实核实 | 方向/竞品/趋势深度调研 |
| 查询次数 | ≤5 | ≤20 | 按复杂度分档（见 credibility-protocol 第 8 节） |
| 多源交叉 | 不做 | **≥2 个独立来源族撞同一事实才采信** | 来源族四档信度 + 承重断言独立核验 |
| 产出 | inline 一句话 | inline 结构化带引用 | run_dir 报告（发布可选） |
| 耗时 | 即时 | 几分钟 | 多轮多 agent |

- 查"这个 API 怎么调" → **dev-lookup**
- 查"React 和 Vue 2025 市场份额谁高"、"X 最新一轮融了多少，多找几个来源核实" → **Phase L：读 `references/phase-l.md` 照做**
- 查"前端框架市场格局深度分析，要出报告" → **Phase 0 起步**

拿不准轻量还是深度：先按 Phase L 做，命中 phase-l.md「升级到深度档的信号」再问用户。

## 通用搜索桥接

定向源不够时的**降级链末位**：桥接付费搜索 API（Tavily/Serper/Brave/Exa，环境变量传 key，任一可用即可）。**agent 已有可用内置 web_search 时优先内置，无需本节。**

- **Provider 路由**（按序第一个可用即用，401/超时自动试下一个）：**Tavily**（`TAVILY_API_KEY`，通用首选）→ **Serper**（`SERPER_API_KEY`）→ **Brave**（`BRAVE_SEARCH_API_KEY`）→ **Exa**（`EXA_API_KEY`，语义搜索）。全部未配 → 诚实说"未配置搜索 API key，只能用定向源"。
- **调用**：curl 模板见 `references/provider-curl-examples.md`。body 用 `jq -n` 构造；Tavily 必带 `search_depth:"advanced"`；中文查询查不到先译英文重试。
- **额度预检**：批量调研前跑 `bash scripts/web-search-quota.sh check all`；单次降级查询不预检。
- **纪律**：≤10 次 API 调用即停；API 返回的多结果是同一检索的产物，关键事实仍需跨来源族交叉；全失败明确说"搜索 API 不可用"，不伪造结果。

---

## Phase 0 — 模式判定 + 研究契约

### 交付模式

| 用户信号 | 模式 | 执行 |
|---|---|---|
| "调研【XXX】" + 完整方向 | **完整调研** | Phase 0 → 1 → 2 → 3 → 4（→ 6 若要求发布） |
| "补充调研XXX" / "针对XXX再调研" | **增量调研** | 识别 gap → Phase 1（只跑 gap 维度）→ 2 → Phase 5 融入 |
| "多维度调研XXX" | **并行调研** | Phase 1 拆 ≥3 维度并行 → 2 → 3 → 4 |
| 提供具体维度列表 | **定向调研** | 跳过维度拆解，直接用用户维度 → 深挖 → 2 → 3 → 4 |
| "做下个迭代规划" / "下个版本做什么" | **迭代规划** | Phase 1 拆 4-8 维度（含现状/竞品/技术债/死代码）→ 2 → 3 → 优先级排序的规划文档 |

### 调研路由（详见 references/deep-research-engine.md 工序 0）

1. **用户是否提供文件？** 有文件 + "只看文件 / 不要外搜" → **文件 only**；有文件 + 无限制 → **文件增强**；无文件 → 判据 2
2. **集合 / 边界**（看 query 结构，不看关键词）：开放集合、主体 niche、sibling 未知 → **撒网**；已知实体的已知属性 → **锁定**；拿不准 → 默认撒网

| 路由 | Phase 1 走法 | 外搜 | 反框架 scan |
|---|---|---|---|
| 撒网 | 5 facet 撒网 + 反框架 | 是 | 是 |
| 锁定 | 起手勘察 | 是 | 是 |
| 文件 only | 工序 F 主题清单直接进深挖 | **0 外搜** | 否 |
| 文件增强 | 工序 F + targeted scan（只补 gap） | 是 | 是 |

### 建 run_dir（两步，不要把命令替换存进变量）

先取时间戳，再用**字面路径**建目录——`RUN=…$(date …); mkdir -p $RUN` 这类写法会被部分宿主的高危命令守卫拦截：

```bash
date '+%Y%m%d-%H%M'
mkdir -p ~/.research-workflow/agent-memory-20261014-1535
```

`topic` 短、英文小写 + 连字符。这个完整路径记作 `{run_dir}`，后续 read/write / spawn prompt 都用字面绝对路径。

### 研究契约（深度档必做）

建目录后立刻写 `{run_dir}/brief.md`（原问题原文 / 要服务的决策 / 子问题 Q1..Qn / 范围内外 / 时间窗 / 什么算答完），并初始化 `{run_dir}/status.json`。字段与规则见 `references/credibility-protocol.md` 第 1、7 节。

**开搜前把 brief 给用户确认**：一条消息列子问题、范围、时间窗，问"按这个查？"。用户请求里已写清全部字段时可跳过，status.json 记 `"brief_confirmed": "implicit"`。

---

## Phase 1 — 深度调研引擎

**完整规范见 `references/deep-research-engine.md`**（worker spawn / 产出契约 / 互证分级时必须读）。**采集工具路由见 `references/sourcing-toolkit.md`**：有内置 web_search/web_fetch/browser 的 agent 优先用内置工具；无内置联网工具的走 bash curl 定向源。worker 开工前跑「采集前自检」把本环境可用通道写进 `{run_dir}/map.md` 顶部。

### 主控流程

1. **工序 F（仅文件模式）**：文件清单 → per-file 抽取 → 跨文件 mapping → 主题清单，落 `file_analysis.md`
2. **工序 1 起步勘察**：3-5 次搜索核准主体 + 关键事实 → `map.md`。撒网追加 ≥5 facet worker；**撒网 / 锁定 / 文件增强都派 1 个反框架 worker**（不给 map.md context）→ `scan_anti.md`
3. **工序 2 课题切分**：按 brief 的子问题拆维度（数量按 credibility-protocol 第 8 节分档），每个维度挂回 ≥1 个子问题，维度间保留概念重叠制造交叉验证压力
4. **工序 3 并行深挖**：每维一个 worker，同一波 spawn。prompt 契约 6 段（见 engine），**必须重注入原问题原文 + 本维度子问题编号**，Mission 含反证检索。worker 把检索追加进 `search_log.md`，产出写 `dive_NN.md`（Key Evidence 带「限定」列）
5. **工序 4 互证分级**：按**独立来源族**分四档（单一来源族永远不是高；同 URL / 转载 / 共作者 / 同厂商 / 本机旧报告都算同族），全局重编号 → `verify.md`
6. **工序 5 定点消解**（有矛盾或 critical 低信度才跑）→ `resolve_NN.md`
7. **工序 6 跨维洞察** → `insight.md`（主张 / 跨维度 / 证据 / 反例 / Confidence / Implications；反例为空时写「已检索未发现：<检索式>」）

**429/限流容错**：worker 返回 429 / rate limit / 5xx 时指数退避重试（30s → 60s → 120s，最多 3 次）→ 仍失败按当前可用模型降级重发 → 仍失败标 `BLOCKED`，写进 status.json `degradations`，交付时告诉用户。

### 通信协议

- worker 直接写 `{run_dir}/` 下指定路径（路径在 spawn prompt 里给死）；push 只承载"完成 + 路径 + 一句话"
- 主 agent 要数据一律 `read` 文件，**不从 push 消息里取数据**；worker 没写文件就算失败，不由主 agent 凭消息代写

### 停止规则（替代搜索量下限）

出口看"答完没有"：每个子问题有 ≥2 个独立来源族支撑的结论，**或**明确「查不到」并列出已尝试的检索式；核心结论都做过反证检索；矛盾已消解或标为领域真实分歧。分档投入见 credibility-protocol 第 8 节。"查不到"是合法结论。

---

## Phase 2 — 承重断言独立核验（必做）

Phase 1 工序 4-5 是素材层的信度分级；本 Phase 是**写作前对承重断言的独立复核**，由新上下文的子代理执行，写的人不自评。完整步骤见 `references/credibility-protocol.md` 第 5、6 节：

1. 先按 Phase 3 outline 写报告草稿，从草稿抽承重断言（进 TL;DR / 结论 / 建议的断言 + 所有数字、日期、引语）→ `claims.md`（ID / 断言 / 限定 / 引用 / 原文 quote / URL / 核验）
2. spawn 新上下文核验子代理，只给 ID + 断言 + 限定 + URL，逐条回原文判 支持 / 部分支持 / 不支持 / 不可达
3. 不支持 → 删或改写；部分支持 → 按原文收窄；不可达 → 降一档并标注
4. 建议类结论过**本地适用性核验**：附本地证据，或标「外部推断，未经本地验证」
5. 另做四问自查（结果直接改到断言里，不新增"质疑"章节）：数据出处与样本？相关还是因果？发布方利益？有没有更新的数据推翻？

---

## Phase 3 — 结构规划（Gate）

写正文前先输出 outline 并自查：

```
目标文档：[本地 / 飞书 / 已有文档]
现有结构：[已有文档列当前章节；新文档写"新建"]
计划结构：一、各子问题结论（答完 / 部分 / 查不到） 二、… 末章：结论与建议
每节挂回的子问题：…
```

补充已有文档时：读现有 outline → 每个新知识点映射到现有章节 → **禁止在结论之后追加章节** → 确不属于任何章节的在结论前插小节。

---

## Phase 4 — 文档输出

写入 `{run_dir}/report.md`：

- 开头逐个子问题给结论，再展开；正文基于 `verify.md` 与 `insight.md`
- **只引用 claims.md 已核验的承重断言**；写作时新增的数字、链接先补进 claims.md 核验
- 限定条件随断言保留，摘要不得比正文更强
- 追加「独立判断与建议」章节；建议类结论带本地证据或「外部推断」标注
- 已有文档：先 fetch 查格式，用相同格式；overwrite 前必须 fetch 完整内容，不凭记忆拼凑

**交付前跑 lint**：`bash <skill 目录>/scripts/research-lint.sh {run_dir}`。有 ERROR 先修再交付；WARN 和 status.json 里的 degradations 在交付消息里说明。把 status.json 的 `stage` 改为 `delivered`。

**下游落库**：调研结论要写进仓库文档（设计 / ADR / 计划）时，把 report.md 复制进仓库的文档目录并提交，下游引用仓库内路径，保留信度标注——本机 run_dir 会被清理，引用 `~/` 路径的证据链迟早断。

---

## Phase 5 — 版本化补充融入（增量调研）

> 前置依赖：lark-cli + 飞书认证 + 目标 doc 的 `{obj_token}`；任一缺失则让用户提供既有报告 markdown 作底稿，或直接首发。

补充调研是最易出错的环节，**完整规范见 [references/versioned-rewrite.md](references/versioned-rewrite.md)**：快照原文档 → 补充内容映射（merge_plan.md）→ 基于原文改写（禁止凭记忆）→ 发布前自检 Gate → overwrite → 归档 + CHANGELOG。**禁止 append 追加新章**，补充内容拆散融入现有章节，结论段重写吸收新发现。

---

## Phase 6 — 飞书发布（用户要求时）

只在用户要求发布、或 brief 里写明发布目标时执行；否则交付本地 `{run_dir}/report.md` 并在交付消息里问一句是否发布。发布是对外动作，目标知识库 `{SPACE_ID}` 由用户提供，不预设。

依赖 `lark-cli`（独立安装）+ 飞书认证；缺失则跳过并告知用户。命令与异常处理见 **`references/lark-publish.md`**：创建主报告节点 → 写入主报告 → 每个 `dive_NN.md` 作为子文档 → 返回目录树链接与本地路径。

---

## Gotchas（高信号）

- **转述加强是头号失真**：删 hedge、丢场景限定、两源拼一个新结论。限定列 + claims 核验 + 摘要不得强于正文三道防线
- **worker 数 ≠ 独立来源数**：三个 worker 引同一个 README 仍是单一来源族
- **答了相邻问题**：报告每节挂回 brief 的子问题，挂不上的进 parking.md
- **外部已证明 ≠ 本地可做**：建议类结论要本地证据，否则标「外部推断」
- **静默降级**：跳过任何步骤都写 status.json `degradations` 并告诉用户
- **补充调研不在结论后加章**：走版本化改写（versioned-rewrite.md）
- **description 不要总结 workflow**：description 是触发器

## 禁止的反模式

- ❌ 结论后追加章节；补充调研是报告本体
- ❌ 不检查格式就写入已有文档；凭记忆 overwrite
- ❌ 质疑结果单独成章而不改原断言
- ❌ 跳过结构规划直接写
- ❌ 报告引用未核验的承重断言；写作时临时加料
- ❌ 把本机旧调研报告当证据
- ❌ 文件 only 模式偷偷外搜
- ❌ 未经用户要求对外发布

## 自查清单（交付前）

- [ ] brief.md 有原问题原文，深度档已获用户确认（或 implicit 有理由）
- [ ] 报告逐个子问题给了结论（含「查不到」）
- [ ] 每条高信度有 ≥2 个独立来源族或一手权威源 + 反证已查
- [ ] claims.md 全部核验，不支持项已删改
- [ ] 建议类结论有本地证据或「外部推断」标注
- [ ] research-lint.sh 0 ERROR；degradations 已告知用户

## SKIP（轻量场景转其他 skill）

单点技术检索转 **dev-lookup**；轻量交叉验证走本 skill **Phase L**（读 `references/phase-l.md`）；提方案前验证本机环境/API 能力、论证建议可行性转 **evidence-based-proposal**；结论需要红队检验时叠加 **adversarial-verification**。
