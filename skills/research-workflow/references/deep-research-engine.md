# Deep Research Engine — 调研内核规范

## 目录

- [工序 0 — 模式判定（详版）](#工序-0-模式判定详版)
- [文件命名与通信约定](#文件命名与通信约定)
- [工序 F — 文件 Intake & 深析（仅文件 only / 文件增强模式）](#工序-f-文件-intake-深析仅文件-only-文件增强模式)
- [工序 1 — 起步勘察（按模式分走法）](#工序-1-起步勘察按模式分走法)
- [工序 2 — 课题切分](#工序-2-课题切分)
- [工序 3 — 并行深挖](#工序-3-并行深挖)
- [工序 4 — 互证分级](#工序-4-互证分级)
- [工序 5 — 定点消解（条件触发）](#工序-5-定点消解条件触发)
- [工序 6 — 跨维洞察](#工序-6-跨维洞察)
- [Core Principles（适用全工序）](#core-principles适用全工序)
- [撒网产出契约（工序 1 撒网 worker 写入 `{run_dir}/scan_NN.md` 的格式）](#撒网产出契约工序-1-撒网-worker-写入-run_dirscan_nnmd-的格式)
- [Facet: <facet 名>](#facet-facet-名)
- [深挖产出契约（工序 3 worker 写入 `{run_dir}/dive_NN.md` 的格式）](#深挖产出契约工序-3-worker-写入-run_dirdive_nnmd-的格式)
- [Dimension <NN>: <维度名>](#dimension-nn-维度名)

本文件是 `research-workflow` Phase 1 的完整执行规范。主文件 SKILL.md 给主控要点，
执行 worker spawn / 路由判定 / 信度分级 / 矛盾消解 / 产出契约时**必须读本文件**。

## 工序 0 — 模式判定（详版）

判据顺序（先看文件，再看集合）：

**1. 用户是否提供文件?**
- 有文件 + 明示"只看文件 / 基于这些 / 不要外搜" → **文件 only 模式**
- 有文件 + 无限制 / "参考 / 结合 / 帮我完成 / 在这基础上" → **文件增强模式**
- 无文件 → 走判据 2

**2. 集合 / 边界判断**（查 query 的结构属性，不看关键词）：
- 未先验确定（开放性 quantifier "所有 / 类似 / 之类"；主体 niche；sibling 集合未知） → **撒网模式**
- 已知边界实体的已知属性问询 → **锁定模式**
- 拿不准 → 默认撒网（framing 错的代价 > 多跑撒网铺面）

| 模式 | 工序 1 走法 | 后续差异 |
|---|---|---|
| **撒网模式** | 工序 1 撒网铺面（5 facet + 反框架 worker） | 标准链路 |
| **锁定模式** | 工序 1 起手勘察 + 反框架 worker（skip facet 撒网） | 标准链路 |
| **文件 only 模式** | 跳过工序 1，工序 F 主题清单直接进工序 2 | 工序 3 不外搜；工序 5 跳过；不派反框架 worker |
| **文件增强模式** | 工序 F + 工序 1 targeted scan（只搜文件 gap）+ 反框架 worker | 工序 3 文件 + 外搜结合 |

文件场景拿不准走哪种 → 默认**文件增强**。

## 文件命名与通信约定

**每次 session 独立目录**——主 agent 选定 topic 后（工序 0 末），分两步建本次 run 的隔离目录：先取时间戳，再用字面路径建目录（不要把命令替换存进变量再 mkdir，部分宿主的高危命令守卫会拦）：

```bash
date '+%Y%m%d-%H%M'
mkdir -p ~/.research-workflow/agent-memory-20261014-1535
```

完整路径记作 `{run_dir}`。`topic` 短、英文小写 + 连字符。`-p` 让 mkdir 幂等，同分钟重跑不挂。建完目录立刻写 `brief.md` 与 `status.json`（`references/credibility-protocol.md` 第 1、7 节）。

所有产出落在 `{run_dir}/` 下，文件名不再带 topic 前缀：

| 文件 | 工序 | 模式 | 内容 |
|---|---|---|---|
| `{run_dir}/brief.md` | 0 | 全模式 | 研究契约：原问题原文 / 要服务的决策 / 子问题 / 范围 / 时间窗 / 出口条件 |
| `{run_dir}/status.json` | 0-交付 | 全模式 | 模式、阶段、降级记录、实测检索次数、下游消费方 |
| `{run_dir}/search_log.md` | 1-5 | 全模式 | 每次检索 / 抓取一行（worker、时间、工具、查询或 URL、纳入/排除、理由） |
| `{run_dir}/file_analysis.md` | F | 文件 only / 文件增强 | 文件清单 + per-file 抽取 + 跨文件 mapping + gap 分析 + 主题清单 |
| `{run_dir}/map.md` | 1 | 撒网 / 锁定 / 文件增强 | landscape 地图（关键玩家、来源、信号入口） |
| `{run_dir}/scan_NN.md` | 1（撒网） | 撒网 | 每 facet worker 的撒网产出 |
| `{run_dir}/dive_NN.md` | 3 | 全模式 | 每维度 worker 的深挖产出 |
| `{run_dir}/verify.md` | 4 | 全模式 | 4 档信度分类 + 矛盾点 + 全局引用映射 |
| `{run_dir}/resolve_NN.md` | 5 | 撒网 / 锁定 / 文件增强 | 每条矛盾的定点消解产出（文件 only 跳过） |
| `{run_dir}/insight.md` | 6 | 全模式 | 跨维度综合洞察 |
| `{run_dir}/claims.md` | 写作前 | 全模式 | 承重断言台账 + 独立核验结论 |
| `{run_dir}/parking.md` | 任意 | 全模式 | 挂不回子问题的发现（不进报告正文） |
| `{run_dir}/report.md` | 写作 | 全模式 | 交付报告 |

**通信协议（主 agent 与 worker 共享 host 文件系统）**：
- worker 用 write/bash 工具**直接写到上述路径**；路径在 spawn prompt 里给死，worker 不自取名
- worker 的 push 通道（auto-announce）**只承载"完成 + 路径 + 一句话进度"信号，不承载产出内容**
- 主 agent 任何阶段要数据都 `read` 文件，**不从 push 消息内容里抠数据**——push 只是"worker 干完了"的通知，不是数据来源

## 工序 F — 文件 Intake & 深析（仅文件 only / 文件增强模式）

**触发**：工序 0 判定为文件 only 或文件增强。**先于工序 1 执行**。

**过程**：
1. **文件清单**：列所有用户提供文件（路径 / 类型 / 大小 / 一句话内容摘要）
2. **per-file 抽取** —— 逐文件抽：核心主题/议题；关键断言/论点/结论；数据点/数字/图表（带页码或段落定位）；发表/数据时间窗（用于工序 4 互证撞时间口径冲突）；方法论（如适用）；文件作者明示的局限/注意/偏见
3. **跨文件 mapping**：重叠主题；文件间矛盾/数据冲突；互补信息；**gap** —— 该话题的重要面相所有文件都没覆盖
4. **主题清单**：合并出统一主题清单，工序 2 维度切分基于它

**模式差异**：文件 only → gap 仅作记录不外搜；文件增强 → gap 清单驱动工序 1 targeted scan

**输出**：`{run_dir}/file_analysis.md`

## 工序 1 — 起步勘察（按模式分走法）

### 前置（撒网 / 锁定 / 文件增强 共用，文件 only 跳过）

文件 only → 跳过本工序，工序 F 主题清单直接进工序 2。其他三模式 → 通用 SOP「上下文收集」：用 bash 执行 curl 或搜索命令 3–5 次搜索 → 核准主体 + 关键事实 → `{run_dir}/map.md`。

### 撒网模式 · 撒网铺面

在 map.md 基础上扩展广度——并行 worker 从不同角度看 topic：
1. 拆 **≥5 个 facet**，facet 之间必须跨不同 dimension（不要都从同一角度切）
2. 同一波 spawn 一 facet 一 worker。spawn prompt **必须 inline `{run_dir}/map.md` 关键摘要**作 context；按「撒网产出契约」写 `{run_dir}/scan_NN.md`
3. **加 1 个反框架 worker**：同一波 spawn，**不给 map.md context**，任务是"挑战主流 framing，从不同视角找漏掉的 sibling / 信号 / 争议"。写 `{run_dir}/scan_anti.md`
4. 每个 worker：10-20 次独立搜索（不同关键词、不同源，避开 landscape 已有词汇），检索追加进 `search_log.md`
5. 主 agent 收齐"完成 + 路径"信号后，`read` 各 scan + scan_anti → **扩展** `{run_dir}/map.md`

### 锁定模式 · 起手勘察
前置完成后派 1 个反框架 worker（同撒网第 3 条，不给 map.md context）写 `scan_anti.md`，读完再进工序 2。锁定模式最容易沿用户的 framing 一路确认下去，反框架是唯一的反方视角。

### 文件增强模式 · targeted scan
工序 F 的 gap 清单驱动搜索 query —— 只搜文件没覆盖的面相，3-5 次。外搜 finding 追加进 `{run_dir}/map.md`，显式标"来源:外搜补 gap"。同时派 1 个反框架 worker 写 `scan_anti.md`。

### 出口条件（两种走法共用）

① 一句话写明"我有[资源]、用户在问[意图]、我要交付[什么]"；
② 关键事实清单，每条 `[^N^]`，落到 `{run_dir}/map.md`；
③ **subject identity 由一手来源支撑** —— 搜索结果同源/低质/互相矛盾，标 "subject under-attested"，停工序 1 补搜或回退。

## 工序 2 — 课题切分

按 `brief.md` 的子问题拆**编号维度**，数量按 `references/credibility-protocol.md` 第 8 节的复杂度分档（轻 3-4 / 标准 5-7 / 深 7-9）——这份清单 = 工序 3 的派单清单（每维默认一个 worker）。**每个维度标注它服务的子问题编号**，服务不到任何子问题的维度不派。

维度可从下列角度切（可组合）：
- **时间**：历史起源 / 当前状态 / 1 年展望 / 5 年展望
- **角色视角**：用户 / 企业 / 监管 / 投资人 / 竞争者 / 从业者
- **场景**：乐观 / 悲观 / 现状延续 / 破坏式 / 黑天鹅
- **地域**：中国 / 美国 / 欧盟 / 新兴市场
- **领域切面**：技术 / 商业 / 财务 / 法规 / 生态
- **文件主题**（文件模式专属）：工序 F 主题清单直接作维度

**维度间保留 ≥30% 概念重叠**。重叠不是浪费——它制造交叉验证压力，工序 4 靠"≥2 个独立 worker 撞到同一事实"才给高信度。零重叠 = 工序 4 废。

每个维度 scope 必须含三段：
1. **Current state** —— 此角度下当前发生什么
2. **Key evidence** —— 该角度的数据 / 来源 / 案例
3. **Tensions** —— 此角度的反向观点 / 争议

**出口条件**：编号维度清单（数量符合分档），每项有 scope 三段描述与所服务的子问题编号。

## 工序 3 — 并行深挖

每维一个 worker，**同一波 spawn，并行，不串行**。

### Worker prompt 契约（6 段必含，标准模式）

1. **原问题与子问题**：`brief.md` 里的原问题原文（逐字）+ 本维度服务的子问题编号——长程任务会漂移，每个 worker 都要重新看到原问题
2. **Mission**：维度名 + scope 三段 + "10-20 次独立搜索，不同关键词、不同源、覆盖一手来源（政府/学术/官方/主流媒体），避开内容农场/SEO 聚合/匿名博客" + "**至少 2 次反证检索**（批评 / 失败 / 局限 / 反例 / does not / fails to），写进 Tensions 段并注明检索式" + "数字、日期、引语打开原文核对，不用搜索摘要"
3. **Context**：主线已有事实（工序 1 关键发现，inline 摘要）；本机旧调研报告只能当线索，引用时回到它的一手源
4. **产出契约**：严格按「深挖产出契约」（见末尾）结构 + 内容要求；支撑断言的关键短 quote（数字、定义、争议措辞、政策原文）必须 verbatim 不可改述，原文的限定（场景 / 样本 / 模型 / hedge 词）写进「限定」列。不要整页 dump / 长篇 verbatim / 重复结果
5. **输出路径（强制）**：用 write 工具写到 `{run_dir}/dive_NN.md`，每次检索 / 抓取追加一行到 `{run_dir}/search_log.md`（主 agent 在 spawn prompt 里把 `{run_dir}` 替换成绝对路径）
6. **Push 行为**：任务完成后，push 通道只回"完成，文件:`{run_dir}/dive_NN.md`，一句话摘要 X"

### 文件增强模式 · 额外要求
spawn prompt 在 Context 段额外 inline **本维度相关的文件 excerpt**（从工序 F 抽取，不让 worker 自己读文件）。worker 把文件作为主源，外搜作补/验证，**产出里显式区分文件来源 vs 外搜来源**（Sources Cited 段分两栏：文件 / URL）。

### 文件 only 子分支
worker prompt 同标准模式 5 段，但 Mission 改为"**不外搜**，只用 spawn prompt 里 inline 的文件 excerpt"。worker 任务：跨文件交叉引用、识别 pattern、评估证据强弱、标记 implicit assumptions / 文件作者偏见。Sources Cited 段格式：`[File: <name>, Section: <X>]`，无 URL。

### 主 agent 行为
- 全部 worker spawn 完，主 agent **结束本回合**——等 auto-announce 推回"完成 + 路径"信号
- 收到完成信号后，**不需要解析 push 内容**——文件在指定路径活着，工序 4 再 read
- 失败 worker 显式标记、工序 6 `insight.md` 里说明缺口，并写进 `status.json` 的 `degradations`
- worker 没写出文件就算失败——主 agent 不凭 push 消息内容代写 dive

**出口条件**：每个 worker 都已写出 `{run_dir}/dive_NN.md`（或显式失败）；worker 未回齐不进工序 4。

## 工序 4 — 互证分级

**操作前** `read` 所有 `{run_dir}/dive_NN.md`（撒网模式还要 read `{run_dir}/scan_NN.md`）取原文。

跨维度比对、分信度、列矛盾。按四档分类每条 finding：

信度按**独立来源族**计，不按 worker 数计（定义见 `references/credibility-protocol.md` 第 2 节）：同一 URL、转载追到同一原始出处、共作者论文、同一厂商多渠道、本机旧报告与它引用的来源，都算同一个来源族。

| 档 | 标准 |
|---|---|
| **高信度** | ≥2 个独立来源族一致；或 1 个一手权威源（官方文档 / 论文原文 / 政府原文）且已检索反证未发现 |
| **中信度** | 1 个一手权威源但反证未检索充分；或 ≥2 个独立二手族一致；厂商自报数据最高到这一档 |
| **低信度** | 单一二手源、博客、搜索摘要（未打开原文） |
| **矛盾点** | 独立来源族在同一指标 / 同一时间 / 同一定义上分歧；数字矛盾；解读冲突 |

**单一来源族永远不是高。** verify.md 每条高信度列出它的来源族（族名 + 代表 URL）。

要求：
- 矛盾点全部明示，**不允许压平**——温度高的发现最值钱
- 时间口径冲突算矛盾（口径不同就标"worker A 取 2024Q3，worker B 取 2024 全年"）
- **引用全局重编号**：各 worker 的局部 `[^N^]` 由 orchestrator 重编成全局编号；在 `{run_dir}/verify.md` 里给一张映射表 `worker_id × 局部N → 全局M`。后续工序 5/6 一律用全局编号

判断工序 5 是否触发：有矛盾点或有 critical 低信度 → 跑；否则跳到工序 6。**critical** 指"报告主论点的核心支撑"：它弱 = 整份报告结论站不住。

**文件 only 模式例外**：不允许外搜，工序 5 直接跳过。矛盾点照样记录但不消解——保留作"文件内真分歧"标记，工序 6 insight 里说明。

输出：`{run_dir}/verify.md`，含四档完整分类 + 矛盾点详析 + 全局引用映射表。

## 工序 5 — 定点消解（条件触发）

**触发**：工序 4 标了矛盾点或 critical 低信度才跑；否则跳过。

对每个未解决项 spawn 一个聚焦 worker，prompt 给两端断言 + 来源，要求"找独立证据消化分歧"。每冲突 **≥3 次额外搜索**。worker **写到 `{run_dir}/resolve_NN.md`**（NN = 矛盾编号），push 只回"完成 + 路径"。

每项二选一：
- **Resolved** —— 找到新证据，重分到高 / 中信度
- **Unresolved (genuine disagreement)** —— 注明这是领域真实分歧，保留在档

主 agent `read` 各 `resolve_NN.md`，更新 `{run_dir}/verify.md`，保持全局编号一致。

## 工序 6 — 跨维洞察

`read` `{run_dir}/verify.md` 取互证后的事实，抽**跨维度才看得到**的洞察——单 worker 视角看不到的 pattern。

每条洞察必含 6 字段：
- **主张**（一句话）
- **跨维度**（列维度编号 — 文件模式还可列文件编号）
- **支撑证据**（全局 `[^N^]`）
- **反例 / 边界条件**（为空时写「已检索未发现：<检索式>」，不得留空）
- **Confidence**：高 / 中 / 探索性 —— 给下游写作判断洞察可信度，confidence 越高越前
- **Implications**：对下游决策 / 行动的意义（一句话）—— 给写作输出 "so-what"

**Genre-aware 调整**（看下游写作用途）：
- 偏研究报告 / 行业分析 / 咨询 deliverable → insight 偏战略可执行、市场机会、竞争动态、前瞻 implications
- 偏学术论文 / 综述 / 文献回顾 → insight 偏研究 gap、方法论矛盾、理论张力、相对前作的新贡献
- 不明确 → 中性，两类都覆盖，让下游自取

**文件模式强调**：优先抽"跨文件 synthesis"型 insight —— 单文件看不到的 pattern；文件增强额外标"外搜证据如何强化 / 反驳 / 扩展文件结论"。

写 `{run_dir}/insight.md`。**insights 不可省**：即便用户要求"简短""压缩"，也保留（可缩篇幅，不可整段砍）—— 这是整轮 deep research 最值钱的产出。

---

## Core Principles（适用全工序）

1. **深度优先**（锁定模式）/ **先广后深**（撒网模式）。浅聚合是 deep research 的反义词；每维度必须深挖完再走
2. **Raw evidence required**：支撑断言的关键 quote 必须 verbatim（数字、定义、争议措辞、政策原文）。只回 paraphrase 不行——互证比的就是口径精度
3. **矛盾即信号**：冲突高亮、分析、不要压平或取平均。温度高的发现最值钱
4. **长内容走文件，push 只发信号**：worker 产出写到 `{run_dir}/*.md`，push 只回"完成 + 路径"。下游消费素材时必须 read 文件，不读 push
5. **源质量分级**：优先一手来源（政府 / 学术 / 官方公告 / 主流媒体）；避开内容农场 / SEO 聚合 / 匿名博客 / AI 生成 listicle。**文件模式：用户提供文件视为一手权威源**
6. **停止规则代替搜索预算**：出口看每个子问题是否有 ≥2 个独立来源族的结论（或明确「查不到」），不看搜了多少次；分档投入见 `references/credibility-protocol.md` 第 8 节。实际检索次数以 `search_log.md` 为准，不自报估算
7. **文件 only 尊重用户意图**：用户明示"只看文件"，绝不偷偷外搜
8. **文件增强平衡来源**：文件做主源，外搜只补 gap / 验证 / 加深。不让外搜淹没用户文件
9. **单 worker 工具调用上限**：每个 scan / dive worker **工具调用总数 ≤ 30 次**（含 search + fetch + exec 等所有工具）。达到上限立刻停止追加新查询、基于已有素材写产出并 announce 收尾——**宁可少 1-2 条边角证据，不要拉满**。查不到就写「查不到 + 已尝试检索式」，不为凑数降低来源标准。

---

## 撒网产出契约（工序 1 撒网 worker 写入 `{run_dir}/scan_NN.md` 的格式）

```
## Facet: <facet 名>

### Key Findings
- <一句话 finding> [^N^]

### Major Players & Sources
- <实体>: <角色/相关度>

### Trends & Signals
- <趋势>: <信号> [^N^]

### Controversies & Conflicting Claims
- <冲突描述>: 两端各 [^N^]

### Recommended Deep-Dive Areas
- <方向>: <为何值得深挖>

### Sources Cited
[^1^]: <URL> — <作者/机构, 日期>
[^2^]: ...
```

## 深挖产出契约（工序 3 worker 写入 `{run_dir}/dive_NN.md` 的格式）

```
## Dimension <NN>: <维度名>

### Current State
- <事实> [^N^]

服务子问题：Q<n>

### Key Evidence
| 数据点 | 数值/描述 | 限定（场景/样本/模型/hedge 原词，无则写"无"） | 时间 | 来源 |
|---|---|---|---|---|
| ... | ... | ... | ... | [^N^] |

### Tensions & Counter-arguments
- <反向观点>: <论据> [^N^]
- 反证检索：<检索式>（结果 / 已检索未发现）

### Sources Cited
[^1^]: <URL or 文献标题> — <作者/机构, 日期>
[^2^]: ...
```
