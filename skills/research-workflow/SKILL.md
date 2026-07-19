---
name: research-workflow
description: "深度调研与结构化报告发布。Use when: 用户说\"调研XXX/研究下XXX方向/深度调研/补充调研/调研并发布\"、需要输出结构化调研报告、给定文件要求\"只看文件/基于这些调研\"、要把调研结果发到飞书时。SKIP: 纯技术方案设计（用 evidence-based-proposal）、单次信息查询（直接搜索）、纯对话不产出文档时。"
metadata:
  pattern: pipeline + gate
  domain: research
  source: merged from deep-research + research-workflow + research-and-publish
---

# Research Workflow — 调研全流程

把"搜索素材 → 交叉验证 → 结构化成文 → 发布"串成一条不割裂的链路。
深度调研方法论（4 路由 / ≥6 维度 / 互证分级 / 矛盾消解）作为 Phase 1 内核自包含，
交付与发布段紧随其后，不再割裂成多个 skill 互相 unknown。

详细规范分置 references：
- **`references/deep-research-engine.md`** — Phase 1 调研引擎的完整规范（4 路由判定、6 道工序、产出契约、通信协议、信度分级表）
- **`references/sourcing-toolkit.md`** — Phase 1 worker 的采集工具路由（pi 本机实测可用性、按内容类型选采集方式、降级链、JS 渲染源终止判定）
- **`references/lark-publish.md`** — Phase 6 飞书发布的命令与异常处理
- **`references/versioned-rewrite.md`** — Phase 5 版本化补充融入的完整规范（快照→映射→改写→自检→发布→归档，回滚机制）
- **`references/failure-cases.md`** — 真实失败案例（补充融入 / 格式不一致 / 未触发引擎等）

## 对外措辞

工序号、模式名、契约名、文件路径、信度档、全局编号都是**内部标签**，不在用户可见处
出现（chat 消息和最终交付物）。对外只汇报实质动作与产出，用自然白话——内部仍按 Phase 记录。
- 别说："这是典型的撒网模式，按工序来"
- 改说："先把全景摸广，再分维度深挖"

## 开工纪律（贯穿全 Phase）

任何 Phase 开始前先校准：

- **取当前时间**：`bash date` 拿系统时间，不假设（预训练知识可能过期）
- **时间窗硬约束**：query 含"2026 Q1 / 近半年 / 最新 / 当前"等时间指向时，窗口当死线——搜索 query 必须打到该窗内；窗外发现明示"[超窗]"并说明时段
- **搜索前不编断言**：任何事实声明都要等搜索结果落地；搜索前的脑补不算
- **搜索语言匹配用户语言**：用户中文 → 中文源；用户英文 → 英文源
- **全程 `[^N^]`**：任何外部事实必带 inline citation，Phase 1 起就开始编号

---

## Phase 0 — 模式判定

收到调研请求，先判两件事：**交付模式** + **调研路由**。

### 交付模式

| 用户信号 | 模式 | 执行 |
|---|---|---|
| "调研【XXX】" + 完整方向 | **完整调研** | Phase 1 → 2 → 3 → 4 → 6 |
| "补充调研XXX" / "针对XXX再调研" | **增量调研** | 识别 gap → Phase 1（只跑 gap 维度）→ Phase 5 融入 → Phase 6 |
| "多维度调研XXX" | **并行调研** | Phase 1 拆 ≥3 维度并行 → 2 → 3 → 4 → 6 |
| 提供具体维度列表 | **定向调研** | 跳过 Phase 1 维度拆解，直接用用户维度 → 深挖 → 2 → 3 → 4 → 6 |
| "做下个迭代规划" / "迭代规划XXX" / "下个版本做什么" | **迭代规划模式** | Phase 1 拆 4-8 维度并行（含现状/竞品/技术债/死代码）→ 2 → 3 → 产出优先级排序的规划文档（不走 Phase 6 飞书发布，除非用户要） |

### 调研路由（看是否提供文件 + 集合是否已知，详见 references/deep-research-engine.md 工序 0）

1. **用户是否提供文件？**
   - 有文件 + 明示"只看文件 / 基于这些 / 不要外搜" → **文件 only 模式**
   - 有文件 + 无限制 / "参考 / 结合 / 帮我完成" → **文件增强模式**
   - 无文件 → 走判据 2
2. **集合 / 边界判断**（看 query 结构属性，不看关键词）：
   - 未先验确定（开放性 quantifier "所有 / 类似 / 之类"；主体 niche；sibling 集合未知）→ **撒网模式**
   - 已知边界实体的已知属性问询 → **锁定模式**
   - 拿不准 → 默认撒网（framing 错的代价 > 多跑撒网铺面）

| 路由 | Phase 1 走法 | Phase 1 外搜 | Phase 2 |
|---|---|---|---|
| 撒网模式 | 撒网铺面（5 facet + 反框架 worker） | ≥110 searches | 标准链路 |
| 锁定模式 | 起手勘察（skip 撒网铺面） | ≥60 searches | 标准链路 |
| 文件 only | 跳过勘察，工序 F 主题清单直接进深挖 | **0 外搜** | 矛盾保留不消解 |
| 文件增强 | 工序 F + targeted scan（只补 gap） | ≥65 searches | 文件 + 外搜结合 |

**判定后立即建 run 隔离目录**：

```bash
mkdir -p ~/.forge/research/{topic}-$(date '+%Y%m%d-%H%M')
```

`topic` 短、英文小写 + 连字符（`lobehub`、`smic-fa`）。这个完整路径记作 `{run_dir}`，
后续所有 read/write / spawn prompt 都用它。`-p` 让 mkdir 幂等。

---

## Phase 1 — 深度调研引擎

**本 Phase 是调研链路的方法论内核，完整规范见 `references/deep-research-engine.md`。**
此处只列主控要点；执行 worker spawn / 信度分级 / 矛盾消解时必须读 references。

**采集工具路由见 `references/sourcing-toolkit.md`** —— pi 无 web_fetch/web_search/browser 内置工具，联网只能 bash curl；Jina Reader 本机超时、Reddit 封锁、JS 渲染源不可采；免费通用搜索引擎（Google/Bing/DDG）全不可用。worker 开工前先跑该 reference 的「采集前自检」把可用通道写进 `{run_dir}/map.md` 顶部，后续按路由表选工具、JS 源直接找替代不硬抓。定向源不够时可降级用 **web-search-bridge** skill（桥接付费搜索 API，需配 `TAVILY_API_KEY` 等）作为 worker 采集工具之一。

### 主控流程（6 道工序）

1. **工序 F（仅文件模式）— 文件 Intake & 深析**：文件清单 → per-file 抽取（议题/断言/数据点/时间窗/局限）→ 跨文件 mapping（重叠/矛盾/互补/gap）→ 主题清单。落 `{run_dir}/file_analysis.md`
2. **工序 1 — 起步勘察**：3-5 次搜索核准主体 + 关键事实 → `{run_dir}/map.md`。撒网模式追加 ≥5 facet worker + 1 反框架 worker 撒网铺面（每 worker ≥10 搜索，写 `scan_NN.md`）
3. **工序 2 — 课题切分**：拆 **≥6 个编号维度**，维度间保留 ≥30% 概念重叠（制造交叉验证压力）。每维 scope 含三段：Current state / Key evidence / Tensions
4. **工序 3 — 并行深挖**：每维一个 worker，**同一波 spawn 并行**。worker prompt 5 段必含（Mission / Context / 产出契约 / 输出路径 / Push 行为）。写 `{run_dir}/dive_NN.md`

   **429/限流容错**（并行 spawn 常撞模型 API 限流）：worker 返回含 `429` / `rate limit` / `访问量过大` / `5xx` 时，**不丢不弃**：
   - 首选**指数退避重试**：等 30s → 60s → 120s 重发，最多 3 次
   - 仍失败则**模型降级**：主力模型(如 glm-4.6) → 备选(doubao/deepseek/glm-4-flash)重发
   - 同维度 worker 重试 3 次仍失败，标记该维度 `BLOCKED`，在 Phase 2 显式标注信度降级，不静默吞掉
   - 主 agent 汇报时必须列出失败维度，让用户决定补跑或接受 gap
5. **工序 4 — 互证分级**：跨维度比对，按四档分信度（高/中/低/矛盾）。引用全局重编号 → `{run_dir}/verify.md` + 映射表
6. **工序 5（条件触发）— 定点消解**：有矛盾点或 critical 低信度才 spawn worker 补搜消解 → `resolve_NN.md`
7. **工序 6 — 跨维洞察**：抽单 worker 看不到的跨维度 pattern → `{run_dir}/insight.md`（每条含 主张/跨维度/证据/反例/Confidence/Implications 六字段）。**insights 不可省**

### 通信协议（主 agent 与 worker）

- worker 用 write/bash **直接写到 `{run_dir}/` 下指定路径**；路径在 spawn prompt 里给死，worker 不自取名
- worker 的 push 通道**只承载"完成 + 路径 + 一句话进度"，不承载产出内容**
- 主 agent 任何阶段要数据都 `read` 文件，**不从 push 消息内容里抠数据**

### 引擎核心原则（详见 references）

- **深度优先 / 先广后深**：每维度深挖完再走
- **Raw evidence required**：支撑断言的关键 quote 必须 verbatim（数字/定义/争议措辞/政策原文）
- **矛盾即信号**：冲突高亮不压平，温度高的发现最值钱
- **源质量分级**：优先一手来源（政府/学术/官方公告/主流媒体）；避开内容农场/SEO 聚合/匿名博客
- **单 worker 工具调用上限 ≤ 30 次**（含 search/fetch/exec）；`≥10` 是防偷懒下限，命中 ~15-25 即合格

### Phase 1 出口条件（交棒给 Phase 2）

素材集齐：`map.md` + `scan_NN.md`（撒网）+ `dive_NN.md` + `verify.md` + `resolve_NN.md`（若有）+ `insight.md` 全部就位。`ls {run_dir}/` 拿清单。

---

## Phase 2 — 质疑验证（必做）

**注意层级区分**：Phase 1 工序 4-5 的互证分级是**调研素材层**的质量控制（多 worker 撞证据给信度，产 `{run_dir}/verify.md`）。本 Phase 的质疑是**文档写作层**的复核——对每条将写入文档的断言，写作前再过一遍：

每项关键断言必须经过至少一轮质疑，结果直接修改原断言（不新增"质疑"章节）：

1. **数据验证**：出处可靠吗？样本量？方法论？是否厂商自发布数据？
2. **因果关系**：是否混淆相关性和因果性？有替代解释吗？
3. **利益相关**：结论受发布方利益影响吗？（融资新闻的 ARR、厂商 benchmark）
4. **时效性**：数据最新吗？有更新数据推翻旧结论吗？

标注可信度：✅ 已验证 / ⚠️ 部分验证 / ❌ 无法验证。直接反映到将要写的断言里。

---

## Phase 3 — 结构规划（Gate — 必须通过）

**在写任何文档内容之前，必须先输出 outline 并自行确认。** 这是 Gate，跳过 = 后面全乱。

```
## 文档结构规划

目标文档：[飞书 / wiki / 本地文件 / 新建]
现有结构：[已有文档列当前章节；新文档写"新建"]
计划结构：
  一、...
  二、...
  ...

格式选择：[markdown / XML 富格式 / 与现有文档一致]
```

**补充已有文档的额外要求**（增量调研 / Phase 5）：
1. 读取现有文档完整 outline
2. 识别补充内容的每个知识点
3. 逐个映射到现有章节应插入位置
4. **禁止在"结论/总结"之后追加新章节** —— 结论必须是最后一章
5. 某知识点确不属于任何现有章节，在结论之前插新小节

---

## Phase 4 — 文档输出

依据 Phase 3 结构写入 `{run_dir}/report.md`：

**新文档**：按 Phase 3 结构写入。基于 Phase 1 的 `insight.md`（跨维洞察）+ `verify.md`（信度分级素材）组织正文。

**已有文档**：
1. 先 fetch 现有内容查格式（XML 富格式？Markdown？混合？）
2. 新内容用相同格式
3. 优先 block 级操作插入目标位置
4. 大规模重排用 `overwrite` 整篇重写（确保结构完整 + 格式统一）
5. **overwrite 前必须先 fetch 完整内容**，不能凭记忆拼凑

报告追加「独立判断与建议」章节（Genre-aware：研究报告偏战略可执行；学术综述偏研究 gap / 方法论张力）。

---

## Phase 5 — 版本化补充融入（增量调研）

当用户要求补充新调研内容时，这是**最易出错**的环节（98% 的结构问题发生于此）。
旧流程只靠"模型自觉遵守拆散融入规则"反复失败（见 failure-cases 案例 1/3，连修 4 轮）。现升级为**版本化改写机制**——把约束从文档规则升级为流程强制，每步有产物，出错可回滚。

**完整执行规范见 [references/versioned-rewrite.md](references/versioned-rewrite.md)，执行补充融入时必须读。** 此处主控要点：

```
❌ 旧：append 追加 → 一二三四五(结论)六(补充)   ← 割裂，且无法回滚
✅ 新：版本化改写 → fetch原文快照 → 拆散融入 → overwrite → 版本归档
```

6 步流程（不可跳步）：
1. **快照原文档**：`lark-cli docs +fetch` 拿当前线上版 → 存 `versions/v{N}_pre-merge.md`（回滚锚点，必存）
2. **补充内容映射**：逐条知识点映射到现有章节（写 `merge_plan.md`），无法映射的才新建小节（插结论前）
3. **基于原文改写**：以 `current.md` 为底稿（**禁止凭记忆**），拆散融入生成 `report.md`，结论段重写吸收新发现
4. **发布前自检**（Gate）：章节顺序一致 / 结论是最后章且已更新 / 格式一致 / 无原文被删改
5. **overwrite 发布**：`--command overwrite` 整篇覆盖同一 obj_token
6. **归档 + CHANGELOG**：存 `versions/v{N+1}_post-merge.md`，`CHANGELOG.md` 记版本变更

**铁律**：禁止 append 追加新章。补充内容必须拆散融入现有章节。`CHANGELOG.md` 是唯一允许 append 的地方。

---

## Phase 6 — 飞书发布

详细命令与异常处理见 **`references/lark-publish.md`**。主控要点：

1. **创建主报告节点**：`lark-cli wiki +node-create --space-id 7642344528036252853 --title "{标题}"`，提取 `node_token`（后续作父节点）/ `obj_token`
2. **写入主报告**：`cd {run_dir} && lark-cli docs +update --api-version v2 --doc {obj_token} --command overwrite --doc-format markdown --content "@report.md"`
   - `@file` 语法必须先 cd 到文件目录
   - Windows 编码问题设 `$env:PYTHONIOENCODING="utf-8"`
3. **创建维度子文档**（不可跳过）：用 `--parent-node-token {主报告 node_token}` 为每个 `dive_NN.md` 创建子节点，再逐个写入。形成主报告 + N 个维度子文档的目录树。详见 references/lark-publish.md 步骤 3
4. **返回结果**：报告标题 / 主报告 + 子文档目录树链接 / 本地文件路径 / 维度数·子代理数·总搜索次数

**飞书知识库**：space_id `7642344528036252853`，空间 https://my.feishu.cn/wiki/KDUfw7MNtiIduZk3awpcelI0nRb

---

## Gotchas（高信号）

- **补充调研不在结论后加章**：98% 的结构错误发生于此。Phase 5 已升级为**版本化改写**（见 references/versioned-rewrite.md），流程强制拆散融入 + pre-merge 快照回滚，不再靠模型自觉
- **凭记忆 overwrite 已有文档 = 篡改**：版本化流程步骤 1 强制 `docs +fetch` 拿 current.md 作底稿，记忆中的措辞 ≠ 文档实际措辞
- **质疑结果不建“质疑”章节**：质疑为修正错误不是展示过程，质疑结论直接改到对应断言
- **description: > 多行块不要总结 workflow**：description 是触发器，总结流程会让模型跟 description 走、跳过 skill 正文
- **连续修格式 ≠ 修结构**：格式统一了但结构割裂用户仍不满意，先结构后格式

## 禁止的反模式

- ❌ **结论后追加章节**：补充调研属于报告本体，不是附录。结论必须最后一章
- ❌ **不检查格式就写入**：前半 XML 后半 Markdown = 视觉割裂。写入前先 fetch
- ❌ **质疑结果不反映到原文**：质疑为修正错误，不是展示过程。"质疑"章节不应出现在最终文档
- ❌ **凭记忆拼凑已有文档**：overwrite 必须先 fetch 完整内容，不能凭记忆重写
- ❌ **跳过结构规划直接写**：结构错误比内容错误更难修复
- ❌ **连续修格式 ≠ 修结构**：格式统一了但结构割裂，用户仍不满意。先结构后格式
- ❌ **只发主报告不发维度子文档**：用户只能看到总览无法回溯每个维度的原始证据，违反 "Raw evidence required"。每个 `dive_NN.md` 必须作为主报告子文档发布
- ❌ **文件 only 模式偷偷外搜**：用户明示"只看文件"绝不外搜（违反用户意图）

## SKIP（轻量调研转其他 skill）

本 skill 是**重量级深度调研**（≥60 searches、多 worker、出报告 + 飞书发布）。以下场景转更轻的 skill：

| 用户意图 | 转向 | 依据 |
|---|---|---|
| 查 API 签名/报错含义/库用法/版本兼容 | **dev-lookup** | 单点技术检索，≤5 次 |
| 查数据/对比/进展/事实核实（需≥2源交叉但不出报告） | **fact-research** | 轻量网络调研，5-20 次，inline 答案 |
| 提方案前验证本机环境/API 能力 | **evidence-based-proposal** | 方案要有依据 |

**三层调研量级**：dev-lookup（即时单点） < fact-research（分钟级交叉） < research-workflow（多轮深度报告）。拿不准量级时，先看 fact-research 的对比表。

## 搜索预算（给用户预期）

| 路由 | 预算 |
|---|---|
| 撒网模式 | 5 scan × ≥10 + 6 dive × ≥10 = **≥110 searches/run** |
| 锁定模式 | 6 dive × ≥10 = **≥60 searches/run** |
| 文件 only | **0 外搜** |
| 文件增强 | 1-5 targeted scan + 6 dive × ≥10 = **≥65 searches/run** |
