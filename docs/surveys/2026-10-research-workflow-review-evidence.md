# research-workflow 复盘 · 证据附录

日期：2026-10-15 ｜ 主文：[2026-10-research-workflow-retrospective.md](2026-10-research-workflow-retrospective.md)（结论与行动）｜ 本文是主文的明细：运行数据、问题证据、业界学界做法、差距矩阵、优化项展开、更正清单与引用
范围：`skills/research-workflow`（与 `~/.claude/skills`、插件副本一致）；本机全部调研产物——`~/.forge/research/` 10 个 run（2026-08-18 ~ 09-29）、`~/.research-workflow/` 13 个 run（2026-10-08 ~ 10-14）、`docs/surveys/` 3 篇落库调研；10 月 11 次有转录的调研会话。
方法：两路逐 run 审计（流程 / 引用 / 互证 / 来源 / 时效 / 成文 6 项打分）+ 31 条承重断言回原文核验 + 会话转录取证 + 两路外部调研（学界 64 条引用、业界含 5 个社区 skill 拆解）。外部数字凡本文引用的，已回 arXiv / GitHub API 原页复核。
31 条承重断言的逐条判定见文末「31 条承重断言核验明细」；逐 run 打分与转录取证的工作底稿没有入库（含会话原文摘录）。

---

## 一页结论

1. **没有编造来源，但"转述加强"是主要失真方式。** 31 条承重断言回原文：完全支持 21（68%）、部分支持 7（23%）、不支持 3（10%）、死链 0、捏造 0。失真全部发生在 dive → report → 设计文档的转述链上：删掉原文的 hedge（"perhaps" 变"反升 43 倍"）、丢场景限定（"仅 ChatDev"）、把两篇论文拼成一个新结论。这与学界最新定位一致——deep research 的终稿错误 84.7% 产生在编排/写作层，而不是检索层（arXiv 2608.24306）。
2. **"高信度"大量是循环互证。** 信度按"几个 worker 撞到"计，而不是"几个独立来源"计：同一 URL 被三个 worker 引用就升档；agent-memory 17 条高信度中 12 条单源，tacit 21 条中 10 条单源；共作者论文、本机旧报告也被算作独立源。
3. **质量控制几乎全靠"写的人自己检查"。** 质疑（Phase 2）、互证（工序 4）、出口条件都是同一个 agent 用散文规则自查，没有确定性脚本、没有隔离上下文的核验者。结果是流程被静默降级：完整撒网铺面 1/23，resolve 在 9 月是 0/10，10 月有 verify 的 10 个 run 里 6 个没有全局映射表，rsi-selfevolve 整个跳过互证却支撑了两项功能决策。
4. **偏题的主要形态是"答了相邻问题"和"外部已证明 → 本地可做"。** 用户问"是不是真需求"，报告论证的是"问题存在"；两条标"✅ 已证明、现在就可以做"的建议，用本机 loopspec 记录一查就被推翻。skill 没有研究契约（要服务什么决策、什么算答完），也没有本地适用性核验这一步。
5. **工程面：触发、交付、溯源都脆。** Phase L 轻量档 0 次使用；飞书发布 2/23；agent 把建目录改写成 `VAR=$(date …)` 赋值形态时会被 forge hazard-guard 拦截；报告自报搜索量比实测虚高 23–60%；Forge 设计文档引用的 2 个 run 目录已不存在；下游设计文档基本丢掉了信度标注。

**优先级最高的三件事**：① 承重断言交给隔离上下文的核验子代理逐条回原文（+ 数字/限定词的确定性脚本校验）；② 信度改按"独立来源族"计数，本机旧报告只当线索不当证据；③ 开工前写研究契约 `brief.md`（深度档让用户确认），每个结论必须挂回一个子问题，建议类结论必须过本地适用性核验。另有 7 处已落库的失真断言需要更正（第五节）。

---

## 一、本机运行表现

### 1.1 运行画像

| 指标 | 数值 | 来源 |
|---|---|---|
| 深度调研 run 数 | 23（8–9 月 10 个，10 月 13 个；10-15 又新建 1 个进行中） | run 目录 |
| 触发方式（10 月 11 次有转录） | 用户手动 `/research-workflow` 6 次，其中 2 次是 agent 先即兴回答、用户不满后强制补做 | 转录 |
| 未触发但应触发 | 非 forge 项目无 hook，4 次明确说"深度调研/调研"未进流程；约 7 个适合 Phase L 的问题被即兴回答 | 转录 |
| Phase L 轻量档使用 | **0** | 转录 + 遥测 |
| 典型深度档开销 | 6–9 个子代理，WebSearch ~100 + WebFetch 60–140，9–28 分钟（中位 ~11） | 转录 |
| 搜索量 vs 规定 | 撒网 ≥110 / 锁定 ≥60；jev-sdlc-ecom 实测 39、aiuc1 47；自报数比实测虚高 23–60% | 转录 |
| 429 / BLOCKED | 0 次真实限流（正则命中全是网页正文）；真实 worker 失败只在非 Claude Code 宿主 | 转录 |
| 飞书发布 | 2 / 23 | 转录 + run |
| 完整撒网铺面（5 facet + 反框架） | 1 / 23（tacit-context） | run |

### 1.2 质量打分（1–5，节选）

| run | 流程 | 引用 | 互证 | 来源 | 时效 | 成文 | 备注 |
|---|---|---|---|---|---|---|---|
| cordis-koishi (09-16) | 4 | 5 | 4 | 4 | 3 | 4 | 唯一有全局映射表的老 run |
| harness-eval (09-03) | 5 | 3 | 4 | 4 | 3 | 3 | SWE-bench Verified 停用日期写错半年 |
| agent-gate (09-18) | 1 | 1 | 1 | 2 | 2 | 2 | 只有 2 个文件，URL "附于交付消息"未落盘 |
| tacit-context (10-13) | 5 | 5 | 3 | 4 | 3 | 5 | 本机最完整的一次 |
| agent-memory (10-14) | 4 | 5 | 3 | 4 | 3 | 5 | 高信度 12/17 单源 |
| rsi-selfevolve (10-10) | 2 | 2 | 1 | 3 | 5 | 3 | 无 verify.md，却进了 acceptance-gate 提案 |
| decision-gates (10-13) | 3 | 1 | 3 | 4 | 3 | 2 | 报告 0 URL；后续两轮本机实证混进同目录 |
| ai-frontier (10-10) | 2 | 3 | 2 | 4 | 1 | 2 | 6 个 scan 同一秒写出，未出报告 |

全量 23 行见底稿。报告抽样 10 条事实断言的无引用率：8–9 月平均约 58%；10 月两极分化（agent-memory / tacit / multi-harness 0/10，rsi 8/10，decision-gates 与 jev-sdlc-ecom 报告正文 0 个 URL）。

### 1.3 做得好的

- **没有捏造来源，没有死链。** 31 条核验 0 捏造、0 死链；2026 年的新事件（Jev HN 发布帖、Check Point、TypeSafe 文档、多篇 2607–2609 arXiv）都能追到一手页面。对比业界：商用 LLM / deep research agent 有 3–13% 的引用 URL 从未存在过（arXiv 2604.03173）。
- **跨 run 自我纠错出现了。** jev-sdlc-ecom 主动更正前一天"TypeSafe 公布 ECE 0.144"，追到源头是第三方模型卡里互相矛盾的两张表。
- **10 月起信度标注普及**（8–9 月几乎没有），tacit-context、agent-memory、ai-review-cache 三个 run 的结构与引用纪律接近规范要求。
- **文件落盘 + 只发路径**的通信协议在 Claude Code 宿主下大体可用，worker 失败能被显式写进 map.md（agent-dist-standard）。

### 1.4 问题清单（按严重度）

**P0 可信度**

| # | 问题 | 证据 |
|---|---|---|
| 1 | **转述加强**：hedge 被删、限定被丢、两源拼成新结论 | agent-gate TL;DR 把 METR 原文 "perhaps because…" 写成因果"作弊率反升 43 倍"，正文有保留、摘要删了；ai-native-tech-design report:141 把 IFScale 的"指令条数"与 ETH 论文拼成"上下文文件越长遵循越差"，ETH v3 原文说长度不影响结论；forge-trust report:52 把 The New Stack 的标题改写后加引号当成官方博客原话，官方原文还与之相反 |
| 2 | **循环互证**：worker 数 ≠ 独立来源数 | memory 的 verify 写"两次独立抓取同一 README"判高；week 把同一条 HN 数据算两路；multi-harness 把本机 9 月旧报告算独立源；rsi 两篇关键论文共享 3 位作者 |
| 3 | **漏反证**：只引支持面 | agent-gate 引 ImpossibleBench 论证"提示层禁令几乎无效"，同一论文写着合适 prompt 能把 GPT-5 作弊率从 92% 降到 1%，未提 |
| 4 | **本机旧报告污染新 run** | jev-sdlc-ecom 的 dive_01 已证伪 ECE 0.144，同 run 的 dive_05 仍以"前序本地调研"转引该数字；旧文件至今未改 |
| 5 | **写作层临时加料** | 主 agent 写报告时补了一条素材里没有的链接（后自删）；宣布"8 个 worker 与初稿一致"后又按 push 消息改报告——"只读文件不读消息"未被执行 |
| 6 | **时间错位** | SWE-bench Verified 停用写成 2026-08（实为 02）；mem0 停 OpenMemory 写成 07（实为 03）；"超窗"标签全批 0 次使用；ai-frontier 拿 2025 资料讲"最新进展" |

**P1 偏题与流程**

| # | 问题 | 证据 |
|---|---|---|
| 7 | **答了相邻问题** | 用户追问"其实我希望你看下…这是个真需求吗？"——报告论证的是问题存在而非需求成立 |
| 8 | **外部证据直接变本地建议** | 报告标"✅已证明、现在就可以做"的"返工 2–3 轮硬上限""低风险自动放行"，用本机 loopspec 一查被推翻 |
| 9 | **流程静默降级** | 5/10 老 run 无 map.md；outline 1/10；rsi 无 verify；4 个 run 的 verify 与 insight 同一秒写出；13 个新 run 中 5 个有缺口但没有任何文件说明停在哪、为什么 |
| 10 | **撒网名不副实** | jev-sdlc 声明撒网但无 scan；ai-frontier 全 run 32 个 URL；反框架只在撒网模式存在，锁定模式无任何反方视角 |
| 11 | **insight 反例字段退化** | jev-sdlc 9 条 insight 仅 1 条有反例，比 9 月更差 |
| 12 | **内部标签泄漏** | 9/10 老报告出现"撒网模式/锁定模式/worker/dive_NN"；新报告仍有"锁定模式""主控""D1 + D6" |

**P2 工程与交付**

| # | 问题 | 证据 |
|---|---|---|
| 13 | Phase L 形同虚设 | 0 次使用，中等量级问题全部变成无纪律即兴搜索 |
| 14 | 触发依赖 hook | 非 forge 项目只能等用户敲 slash；forge 项目里 skill-trigger 又会被子代理完成通知的文本误触发（本次复盘会话里出现了 4 次） |
| 15 | 建目录命令被 hazard-guard 拦 | 被拦的是 agent 改写的 `RUN=~/.research-workflow/…-$(date …); mkdir -p $RUN`（hazards/events.jsonl 10-10 15:16、10-13 17:41），SKILL.md:115 的字面写法本身不拦；未确认拦截按项目计，卡住了同项目的 `forge task complete` |
| 16 | run 根目录迁移无校验 | 08-28 已改到 `~/.research-workflow/`，9 月 run 仍写旧路径：8/31–9/3 是 ZCode 插件副本未更新（已确认）；9/16 后推测是 Forge 仓 10 个设计文档把 `~/.forge/research/…` 当惯例引用 |
| 17 | 证据链断 | Forge 设计文档引用的 `forge-direction-20260905-0956`、`forge-mechanisms-20260905-1752` 两处都不存在（如 `docs/design/compat-commitments.md:3`）；forge-trust 73 条引用 30 条无 URL |
| 18 | 下游丢信度 | `design-stage-hardening` 提案 0 处信度标记；决策模型 survey 一页结论未逐条标信度 |
| 19 | 飞书发布被写成"必做" | 实际执行 2/23；failure-cases 案例 6 把"没自动发布"当失败，和"外发需确认"冲突 |
| 20 | 评测只测触发 | `evals/evals.json` 与 golden cases 全是 trigger/not-trigger，没有一条测产出质量 |

### 1.5 根因

把 20 条问题归并，根因只有四个：

1. **质量门全是散文规则 + 自查。** 规定写得很全（verbatim quote、四档信度、全局编号、6 字段 insight），但没有一条有确定性检查或独立执行者。Forge 自己的约定是"advisory 可以靠提示，HARD 门必须落到可执行检查"，research-workflow 是本仓唯一一个关键门全靠自觉的流水线。
2. **信度的计数单位错了。** "≥2 worker 撞到"衡量的是工作量，不是证据独立性。在同质搜索引擎 + SEO 转载生态下，worker 越多，同一源被撞到的次数越多，循环互证反而被放大。
3. **流程优化的是"量"而不是"答完问题"。** 出口条件是 ≥6 维度、≥60/110 次搜索（实际经常达不到又不声明），而不是"每个子问题都有独立证据和反证"。没有研究契约，自然容易答相邻问题。
4. **转述链没有不变量。** dive → verify → insight → report → 设计文档，每一跳都可能删 hedge、丢限定，但没有任何字段要求限定条件随断言一起传递。

---

## 二、业界与学界怎么做

### 2.1 实证：deep research 的错在哪里

- **引用"存在"≠引用"支持"。** DeepResearch Bench 上引用准确率 Perplexity DR 90.2%、OpenAI DR 78.0%，Gemini DR 引用数最多（111 条有效）但准确率 81.4%（arXiv 2506.11763）；ReportBench 中 OpenAI DR 有引用陈述匹配率 78.9%，o3+search 仅 31.4%（arXiv 2508.15804）；生成式搜索只有 51.5% 的句子被引用完全支持（Liu et al. 2023, arXiv 2304.09848）；Tow Center 测 8 个 AI 搜索找新闻出处，错误率超 60%（CJR 2025-03）。
- **URL 层面会捏造。** 10 个模型/agent、5.3 万条 URL：3–13% 从未存在过，deep research agent 引得更多也编得更多；给 agent 一个 URL 存活检查工具，非可达 URL 降到 1% 以下（arXiv 2604.03173）。
- **错误主要出在编排/写作层。** 对三个开源 DR 系统逐 agent 定位：84.7% 的终稿错误来自 orchestrator，其中约 31% 是幻觉、其余是引用错误；只做单文档摘要的 agent 几乎不出错（arXiv 2608.24306）。我们的 31 条核验方向一致（样本小、非随机）：检索层没出事，转述层出事。
- **过度自信。** BrowseComp 上 Deep Research 准确率 51.5%，校准误差 91%（arXiv 2504.12516）。
- **瓶颈不在"找不到"。** ResearchRubrics（arXiv 2511.07685）、DeepResearch Bench II 都指向漏掉隐含需求、证据整合与核验不足；Plus AI 实测最常见错误是忽略约束和信息过时——也就是偏题。
- **偏题有机制性来源。** 长程 agent 普遍有目标漂移（Arike et al., arXiv 2505.02709）；多轮对话里过早假设、走错不回头，平均掉 39%（Laban et al., arXiv 2505.06120）；检索和推理两侧都有确认偏误（Xie et al., ICLR 2024, arXiv 2305.13300）。

### 2.2 一线产品与开源框架的关键设计

| 来源 | 机制 | 防什么 |
|---|---|---|
| Anthropic 多 agent 研究系统（2025-06-13） | 编排者给子代理"目标 / 输出格式 / 工具与来源 / 边界"四要素；按复杂度分档定子代理数与调用预算；研究结束后由独立 CitationAgent 挂引用；~20 条真实 query 起步做 LLM-judge + 人工抽检。公开承认的失败：简单问题拉起 50 个子代理、偏爱 SEO 农场、分工重叠 | 跑偏、过度研究、写作者自挂引用 |
| Gemini Deep Research | 研究计划先给用户 "revise or approve" 再开搜 | 偏题、漏约束 |
| LangChain open_deep_research | 先澄清 → 压成 research brief 当全程锚点；多 agent 只用于研究，写作一次成稿 | 漂移、写作层拼接 |
| Jina DeepSearch | 生成与评估分离，评估器按题选检查项（明确性/时效/数量/完整性），不过就补搜 | 自评宽松、过早收工 |
| Consensus / Elicit / Scite | 展示逐字摘录；分类器不确定时不强行下结论 | 摘要当证据 |

### 2.3 Claude Code 社区调研 skill（星数 2026-10-15 GitHub API 实测）

| skill | 星 | 最值得借鉴 |
|---|---|---|
| Imbad0202/academic-research-skills | 52.0k | 3 个固定魔鬼代言人检查点；"难以核实"一律判 FAIL；修订最多 2 轮 |
| Weizhena/Deep-Research-skills | 2.4k | 研究对象与字段各让用户确认一次；JSON 字段校验必跑；`[uncertain]` 一路传到报告 |
| daymade/claude-code-skills · deep-research | 1.5k | 来源 registry 冻结，写作只能引已批准清单；承重论断回原文；"evidence family" 去重；"零发现"是合法结果 |
| 199-biotechnologies/claude-deep-research-skill | 1.2k | source / evidence / claim 三本账；`verify_claim_support.py` 确定性校验论断中的数字与实体是否出现在所引 quote 里——门挂在脚本上 |

anthropics/skills 官方仓没有调研类 skill。共性：**门控靠脚本、写作只读已核材料、把"查不到/不确定"当合法输出。**

### 2.4 学界核验方法与传统方法论

- **原子 claim 独立核验**：FActScore → SAFE → VeriScore（只核可验证 claim），把长文拆成原子断言逐条检索核验（arXiv 2305.14251 / 2403.18802 / 2406.19276）。Chain-of-Verification 证明核验必须**隔离上下文**——看着自己草稿检查的效果明显差（arXiv 2309.11495）。
- **多 agent debate 不是银弹**：同等算力下多数投票通常不输 debate，同质模型辩论收益不稳；要用就用异构模型或盲答投票。LLM 当评委有位置、冗长、自偏好偏差（arXiv 2306.05685）。
- **视角驱动 + 先写大纲**：STORM（arXiv 2402.14207）；**证据落盘、写作只读笔记**：FS-Researcher（arXiv 2602.01566）；**会弃答才精确**：PaperQA2 精确率 85.2% 主要来自允许回答"信息不足"（arXiv 2409.13740）。
- **可直接迁移的传统方法**：PRISMA / PRISMA-S（预注册检索式、检索日期、纳入排除标准、可复现日志）；GRADE（证据分级 + 写明降级理由）；ACH 竞争假设矩阵（以证伪为主）；ICD 203 分析标准（来源描述、不确定性表述、**区分事实与判断**、考虑替代解释）；pre-mortem；横向阅读 / SIFT（追溯到一手出处）。

---

## 三、差距矩阵

| 机制 | 业界/学界做法 | 我们现状 | 差距 |
|---|---|---|---|
| 研究契约 | brief + 用户确认（Gemini、LangChain、Weizhena） | 只有 Phase 3 outline "自行确认"，且 1/10 执行 | **缺** |
| 漂移检查 | 每步映射回子问题；子代理 prompt 重注入原问题 | 无 | **缺** |
| 反证检索 | 每个核心结论派反向搜索；ACH | 只有撒网模式的 1 个反框架 worker，执行 1/23 | 弱 |
| 独立性 | evidence family 去重、追溯一手源 | 按 worker 计数 | **错** |
| 承重断言核验 | 隔离上下文逐条回原文；CitationAgent | 同一 agent 自查 Phase 2 | **缺** |
| 确定性校验 | claim-quote 数字/实体脚本；URL 存活 | 无脚本（只有搜索额度脚本） | **缺** |
| 限定条件传递 | ICD 203 区分事实/判断；`[uncertain]` 传到底 | 无字段；hedge 在转述中丢失 | **缺** |
| 写作层约束 | 写作只读已核材料 / registry 冻结 | "只读文件"约定未执行；写作时临时加料 | 弱 |
| 停止规则 | 按复杂度分档 + 停止条件 | ≥60/110 次搜索下限，常不达标且不声明 | 方向错 |
| 弃答 | "零发现""查不到"是合法输出 | Phase L 有"诚实说查不到"，深度档无 | 弱 |
| 检索可复现 | PRISMA 检索日志 | 无 search_log；自报次数虚高 | **缺** |
| 产出质量评测 | rubric / 引用支持率 | 只测触发 | **缺** |

---

## 四、优化清单

原则：沿用 Forge 的分层——**能脚本化的变确定性检查，不能脚本化的交给隔离上下文的独立执行者，散文规则只留给判断题**。按"防最贵的失败、改动最小"排序。

### P0 — 可信度（建议下一个迭代做）

1. **承重断言核验（claim ledger + 隔离核验者）**
   - 写报告前，主 agent 从草稿抽出承重断言（进 TL;DR / 结论 / 建议的、所有数字 / 日期 / 引语），写 `claims.md`：`断言 | 限定条件 | 引用 | 原文 quote | 来源 URL`。
   - spawn 新上下文核验子代理，**只给 claim + URL，不给草稿和推理**，逐条抓原文判四档：支持 / 部分支持 / 不支持 / 无法访问。
   - 不支持的删或改写，部分支持的按原文收窄措辞；结果写回 `verify.md`。预算：20–40 条承重断言，约增加 30–60 次抓取。
   - 依据：Chain-of-Verification、CitationAgent、arXiv 2608.24306。
2. **限定条件随断言传递**
   - dive / evidence 契约加「限定」字段（场景、样本、模型、时间、原文 hedge 词）。
   - 规则：原文有 perhaps / may / in setting X / 仅限 Y 的，报告与下游文档必须保留；摘要不得比正文更强。
3. **信度按"独立来源族"计**
   - 同 URL、同一原始出处的转载、共作者论文、同厂商多渠道发布 = 一个来源族。
   - 高信度 = ≥2 个独立来源族，或 1 个一手权威源 + 无反证；单源永远不是高。
   - **本机旧 run 报告只能当线索，不能当证据**——引用前必须回到它的一手源。
4. **建议类结论必须过本地适用性核验**
   - 报告里任何"建议我们做 X"，要么附本地证据（checklog / 代码 / 历史记录），要么标"外部推断，未经本地验证"。
   - 与 evidence-based-proposal 的分工写进 SKIP / composes。
5. **确定性校验脚本** `scripts/research-lint.sh`（交付前必跑，不过不交付）
   - 必备文件随模式检查；每段事实陈述有引用；每条引用有 URL + 日期；URL 存活（curl，参考 urlhealth）。
   - `claims.md` 中断言的数字与实体出现在 quote 里（参考 199-bio `verify_claim_support.py`）。
   - 内部标签泄漏扫描（撒网模式 / 锁定模式 / 工序 / dive_NN / worker）。
6. **更正已落库的失真断言**（见第五节，单独一个小任务即可）。

### P1 — 防偏题与防静默降级

7. **研究契约 `brief.md`**
   - 字段：原问题原文 / 要服务的决策 / 子问题清单 / 范围内外 / 时间窗 / "什么算答完"。
   - 深度档开搜前把 brief 给用户确认（一句话即可，Gemini 式）。
   - 每个 worker prompt 重注入原问题与所属子问题；insight / report 每节必须挂回一个子问题，挂不上的进 `parking.md`，不进报告。
   - 直接针对"答了相邻问题"（问题 7）。
8. **全模式反证**
   - 把反框架 worker 从撒网专属改为全模式必选。
   - 每个核心结论配一次反向措辞检索（批评 / 失败 / 局限 / 反例），insight 的"反例"字段为空时必须写"已检索未发现 + 检索式"。
9. **run 清单 `status.json`**
   - 内容：模式、实际走到哪一步、每次降级（跳过 verify / 维度不足 / worker 失败）及原因、实测搜索与抓取次数（来自 `search_log.md`，不用自报）、下游消费方。
   - 规则：不允许静默降级——降级必须写进 status 并在交付时告诉用户。
10. **用停止规则替代搜索量下限**
    - 删掉 ≥60 / ≥110 的硬下限（实际达不到，又鼓励凑数和虚报）。
    - 改为 Anthropic 式分档（简单 / 对比 / 开放）+ 出口条件：每个子问题 ≥2 个独立来源族，或明确"查不到"，且反证已查。
    - 把"零发现 / 信息不足"写成合法结论。
11. **写作层收紧**
    - 报告只能引 `verify.md` 中已核断言的全局编号，新增链接必须先进 claims 核验。
    - 删除"主 agent 结束回合等 push"的描述歧义，明确"读文件后才写"。

### P2 — 工程与交付

12. **修 hazard-guard 冲突**：SKILL.md 明示两步写法（先 `date '+%Y%m%d-%H%M'` 取值，再 `mkdir -p` 字面路径，不存变量）；forge 侧把 `VAR=$(date …)` 赋值视为误报。
13. **飞书发布改为可选**：只在用户要时执行；failure-cases 案例 6 改写为"用户要求发布却没发"。
14. **下游溯源**
    - 被设计文档 / ADR 引用的调研，报告复制进仓库 `docs/surveys/` 并提交，不再引用 `~/` 下会消失的路径。
    - 引用处保留信度标注；仓库里 10 处 `~/.forge/research/…` 引用统一迁移或标注失效。
15. **Phase L 落地**：轻量档 0 使用，说明"同一 skill 内分档"在触发层不成立。两个选项：
    - 恢复独立的轻量 skill 名；
    - 在 dev-lookup 的 SKIP 里把"需要 ≥2 源的事实 / 数字 / 对比"显式转到 Phase L。
16. **产出质量评测**
    - 新增 5–8 条 golden 调研任务，埋好陷阱：hedge 原文、共作者论文、同源转载、过期数字、需要本地核验的建议。
    - 指标用第 1 条的承重断言支持率 + 第 5 条的 lint 通过率。
    - 现有触发 eval 保留。
17. **forge skill-trigger 误触发**（Go 侧，不在本 skill）：子代理完成通知 / 系统消息不应参与 UserPromptSubmit 关键词匹配。本次复盘会话里 4 次通知触发了 adversarial-verification、backend-development、design-artifact-standards、doc-generator、research-workflow 等不相关 skill。

### 成本估计

第 3、4、7、9、10、12、13 条以改 SKILL.md / 契约为主，token 增量 <20%。第 1、8 条约增加 30–60% 抓取。第 5、16 条是一次性脚本与评测工作。参考：Anthropic 报告多 agent 研究约为普通对话 15 倍 token，当前单次 9–28 分钟的耗时还有余量。

---

## 五、需要更正的已落库断言

| 位置 | 现文 | 原文实际 | 建议 |
|---|---|---|---|
| `docs/plans/escape-hatch-hardening-2026-09.md:122` | "METR 43× 警示"（来自 agent-gate：门禁逻辑可见时作弊率反升 43 倍） | METR 原文为 "perhaps because…"，并给出任务更难、scaffold 不同两种替代解释；ImpossibleBench 同时显示 prompt 可把 GPT-5 作弊率从 92% 降到 1% | 改为"可能相关、非因果"，补反证；设计决策本身可保留，但依据降级 |
| `docs/plans/design-stage-hardening-2026-10-proposal.md:32`（E6） | "上下文文件越长成本越高 [E5][E6]" | ETH 论文 v3 说长度不影响结论；"越多越差"来自 IFScale 的指令条数 | 拆开两个结论，E6 只支撑它真正说的部分 |
| `docs/plans/acceptance-gate-and-wedge-2026-10-proposal.md:24`（未跟踪） | "Phantom Guardrails（arXiv 2607.13083）证明的正是这一点" | rsi run 把两个实验设置混成一组对比；R4 另有"对 Claude 效果弱很多"的限定被丢 | 提交前按原文重写这段论证，写明实验设置与模型限定 |
| `skills/agent-delegation/decisions.md:98` | "审查阶段占 59.4% token" | 仅 ChatDev 场景；另有 SendMessage 首次 resume 可能整段 miss 的限定（issue #44724） | 补两处限定 |
| `~/.research-workflow/agent-dist-standard-20261008-1552/jev-skill-routing-feasibility.md:61` | "TypeSafe 公布 ECE 0.144" | 官方从未公布；0.144 / 0.246 均为第三方转述且互相矛盾 | 文件内加更正行，防止再被后续 run 转引 |
| `~/.forge/research/forge-trust-contract-20260929-1650/report.md:52` | 把水印"survives copy-paste but not … translation"当官方博客原话 | 官方博客无此句，且说 Claude 产出的翻译仍带水印 | 去掉引号与官方归属，改正事实 |
| `~/.forge/research/harness-eval-20260903-0942` | OpenAI 停用 SWE-bench Verified 在 2026-08 | 2026-02（Decrypt 2026-02-24） | 改日期 |

另外：`docs/design/compat-commitments.md:3`、`docs/design/mechanism-hardening.md:3` 等引用的 `forge-direction-20260905-0956`、`forge-mechanisms-20260905-1752` 两个 run 目录已不存在，需要标注失效或找回原报告。

---

## 附：31 条承重断言核验明细

核验方法：挑选 8 个对决策影响最大的 run，从报告中抽出承重断言，交给独立子代理打开所引原文逐条判定（2026-10-15）。只列判定与失真类型；原文要点见各 run 目录。

| # | run | 断言（简） | 来源 | 判定 | 失真类型 |
|---|---|---|---|---|---|
| F1 | forge-trust | 约一半测试全绿的 SWE-bench Verified PR 不会被合并，合并率低约 24pp | metr.org/notes/2026-03-10-… | 支持 | — |
| F2 | forge-trust | 官方水印「survives copy-paste but not … translation」 | anthropic.com/news/claude-text-watermark | 不支持 | 转述当原话，归属错，与原文相反 |
| F3 | forge-trust | deep research 事实准确率 39–77% | arxiv 2605.06635 | 支持 | — |
| H1 | harness-eval | HV/MV=7.80×、9 个中 6 个排名翻转 | arxiv 2605.23950 | 支持 | — |
| H2 | harness-eval | OpenAI 2026-08 停用 SWE-bench Verified | openai.com（403）/ Decrypt | 部分支持 | 日期错约 6 个月 |
| H3 | harness-eval | Anthropic 内部研究 28%→59%、PR/日 +67% | anthropic.com/research/… | 支持 | +67% 归属略宽 |
| M1 | memory | W3C 记忆互操作 CG 无产出 | w3.org/community/… | 支持 | — |
| M2 | memory | auto memory 前 200 行或 25KB | code.claude.com/docs/en/memory | 支持 | — |
| M3 | memory | mem0 2026-07 停 OpenMemory | github mem0 #2735 | 部分支持 | 日期错、引文改写 |
| A1 | agent-gate | 门禁逻辑可见时作弊率反升 43 倍 | metr.org/blog/2025-06-05-… | 不支持 | 删 hedge，相关写成因果 |
| A2 | agent-gate | 「intended methods」提示 80%→95% | 同上 | 支持 | — |
| A3 | agent-gate | ImpossibleBench GPT-5 76%/93%、Opus 46% | arxiv 2510.20270 | 部分支持 | 数字错，漏反证（92%→1%） |
| J1 | jev-sdlc | AY Automate 路由显著落后 Terra | ayautomate.com/blog/… | 支持 | — |
| J2 | jev-sdlc | Check Point 27 次攻破 25 次 | blog.checkpoint.com/… | 部分支持 | 取最坏值，丢限定 |
| J3 | jev-sdlc | skill_suggestion 错误加载率 16.8%→7.3% | docs.typesafe.ai/… | 支持 | 丢「合成请求」限定 |
| J4 | jev-sdlc | Clef 开放权重、协议兼容 | huggingface.co/Cloudflare/clef-flash | 支持 | 漏一手源 |
| J5 | jev-sdlc | 28 篇综述：类型化读出无独立准确率优势 | arxiv 2609.32160 | 支持 | — |
| J6 | jev-sdlc | HN 发布帖 1,989 分 | news.ycombinator.com | 支持 | — |
| J7 | jev-sdlc | laya 建仓日期与星数 | github.com/NandhaKishorM/laya | 支持 | — |
| R1 | rsi | Phantom Guardrails 15/60 vs 0/60、引证后 0.00 | arxiv 2607.13083 | 部分支持 | 两个实验设置混为一谈 |
| R2 | rsi | 312 次运行 10 次违规 | arxiv 2609.33812 | 支持 | — |
| R3 | rsi | paired SD 是重跑的 11–58 倍 | arxiv 2608.22331 | 支持 | — |
| R4 | rsi | abort 通道 54%→9% | arxiv 2510.20270 | 部分支持 | 丢「对 Claude 效果弱」限定 |
| C1 | ai-review-cache | 单轮 > 多轮 > 无上下文重审 | arxiv 2603.16244 | 支持 | — |
| C2 | ai-review-cache | fresh 与同会话自审差异不显著 | arxiv 2603.12123 | 支持 | — |
| C3 | ai-review-cache | 审查阶段占 59.4% token | arxiv 2601.14470 | 支持 | 下游丢「仅 ChatDev」 |
| C4 | ai-review-cache | SendMessage 续用可读缓存 | code.claude.com/docs | 支持 | — |
| T1 | ai-native-tech-design | R2ABench 约 33% 无依据 | arxiv 2604.06683 | 部分支持 | 取自未定义的列 |
| T2 | ai-native-tech-design | Spec Kit 约 10 倍开销 | blog.scottlogic.com/… | 支持 | — |
| T3 | ai-native-tech-design | 上下文文件越长遵循越差 | arxiv 2602.11988 | 不支持 | 两篇论文拼成新结论 |
| T4 | ai-native-tech-design | IFScale：500 条指令时最好模型 68% | arxiv 2507.11538 | 支持 | — |

合计：支持 21、部分支持 7、不支持 3、死链 0、捏造 0。样本是按决策影响挑的，不是随机抽样，比例不代表全部断言。

## 附：主要引用

- Anthropic, How we built our multi-agent research system (2025-06-13) https://www.anthropic.com/engineering/multi-agent-research-system
- Google, Gemini Deep Research (2024-12-11) https://blog.google/products/gemini/google-gemini-deep-research/
- LangChain, Open Deep Research https://www.langchain.com/blog/open-deep-research
- Jina, A Practical Guide to Implementing DeepSearch/DeepResearch https://jina.ai/news/a-practical-guide-to-implementing-deepsearch-deepresearch/
- DeepResearch Bench https://arxiv.org/abs/2506.11763 ｜ ReportBench https://arxiv.org/abs/2508.15804 ｜ ResearchRubrics https://arxiv.org/abs/2511.07685 ｜ BrowseComp https://arxiv.org/abs/2504.12516
- Who is the Agent to Blame?（编排层错误定位）https://arxiv.org/abs/2608.24306 ｜ Reference Hallucinations in Deep Research Agents（urlhealth）https://arxiv.org/abs/2604.03173 ｜ DeepTRACE https://arxiv.org/abs/2509.04499
- Liu et al., Evaluating Verifiability in Generative Search Engines https://arxiv.org/abs/2304.09848 ｜ Tow Center/CJR https://www.cjr.org/tow_center/we-compared-eight-ai-search-engines-theyre-all-bad-at-citing-news.php
- FActScore https://arxiv.org/abs/2305.14251 ｜ SAFE https://arxiv.org/abs/2403.18802 ｜ VeriScore https://arxiv.org/abs/2406.19276 ｜ Chain-of-Verification https://arxiv.org/abs/2309.11495 ｜ Xie et al. https://arxiv.org/abs/2305.13300 ｜ LLM-as-judge 偏差 https://arxiv.org/abs/2306.05685
- STORM https://arxiv.org/abs/2402.14207 ｜ PaperQA2 https://arxiv.org/abs/2409.13740 ｜ FS-Researcher https://arxiv.org/abs/2602.01566
- Goal drift https://arxiv.org/abs/2505.02709 ｜ Lost in multi-turn https://arxiv.org/abs/2505.06120
- PRISMA 2020 https://pubmed.ncbi.nlm.nih.gov/33782057/ ｜ GRADE https://pubmed.ncbi.nlm.nih.gov/18456631/ ｜ ICD 203 https://www.dni.gov/files/documents/ICD/ICD-203.pdf ｜ Heuer, ACH https://www.cia.gov/resources/csi/static/Pyschology-of-Intelligence-Analysis.pdf
- 社区 skill：https://github.com/Imbad0202/academic-research-skills ｜ https://github.com/Weizhena/Deep-Research-skills ｜ https://github.com/daymade/claude-code-skills/tree/main/deep-research ｜ https://github.com/199-biotechnologies/claude-deep-research-skill
