# 输出可读性门禁二期（output-readability-gates v2）

> 状态：设计（待实施）。来源：业界学界调研（2026-09 会话，学结论已内联引用）+ v1 落地后的剩余缺口分析。
> v1 设计与现状证据索引：[output-readability-gates.md](output-readability-gates.md)。本文档只写增量，不复制 v1 内容。

## 结论（先读这个）

二期 = **P0 三个文本级改动（零 schema 变更，一个 PR 可落）+ P1 三个机制级改动（需代码）**，全部来自调研的可吸收项；训练期去偏、verbosity API 旋钮、Vale 引入、通用文档首屏检查等五项显式不做（理由见「不做清单」）。每项带事前声明的可测判据。

## 问题（v1 之后还剩的五个缺口）

v1 建立了 L1 lint / L2 rubric / 门禁证据链三层，架构方向被调研确认为业界前沿（业界 guardrails 框架无持久证据链，AI review 工具的评测冗长无纪律约束）。剩余缺口：

| # | 缺口 | 调研依据 |
|---|---|---|
| G1 | 结论前置纯靠 L2 评——第一轮打回成本最高的问题没有机器代理 | IFEval 证明「可验证指令」便宜、成熟（2025 已饱和）；v1 的 D6 只判枚举存在、不判位置 |
| G2 | 写作期约束薄：3 条行为规则 + 10 条中文禁令，且无微观文风、无正面条款 | 社区同类 skill 已收敛到 20-31 条模式（unslop/no-ai-slop/avoid-ai-writing）；删完 tell 的 sterile 写作同样一眼假 |
| G3 | L2 评委偏差未治理：与产出同家族、无长度控制、单评委 | 评委偏差五类（位置/长度/自偏好/格式/参考）各有已知缓解；自偏好实证存在 |
| G4 | rubric 进化靠人：「同类打回 ≥3 次」无机器聚合，提炼质量依赖复盘者记忆 | Rubrics as Rewards 引发的动态 rubric 生成是 2025-26 热点；手工 rubric 是静态清单 |
| G5 | 无写时反馈：问题最早在回检/门禁才暴露，是最晚时点 | Vale 的 editor+CI 双反馈模式；IDE 实时提示把违规消灭在落盘前 |

## 设计总览

决策原则（调研沉淀，做与不做都从这里推）：

1. **能机器判的不留给模型**（IFEval 教训：便宜、不漂移、可复现）
2. **自审不算数，评委要去偏**（intrinsic self-correction 已被证伪；v1 的「产出者不能自检」方向正确，去偏是补完）
3. **规则从失败里长，但聚合要机器做**（≥3 次判据保留，计数交给 CLI）
4. **治理成本前置**（写作期注入 > 写时 hook 提示 > 回检打回 > 门禁拒绝，越早越便宜）
5. **一次性穷举禁令 = 一次性过时**（v1 gotcha 延续：清单按进化判据生长）

| 缺口 | 方案 | 层 | 阶段 | 落地组件 |
|---|---|---|---|---|
| G1 | D8 结论位置规则（typed docs，advisory） | L1 | P0 | `internal/doclint` |
| G2 | D1/D2 中文清单二期 + 写作期注入微观文风章节 | L1+写作期 | P0 | `internal/doclint` + `internal/skillgen` |
| G3 | 评委偏差防线条款 + borderline 双评纪律 | L2 | P0 | `rubric-docs.md` + `doc-review/SKILL.md` |
| G3 | 双评记录 schema（CoReview 增量字段） | 状态 | P1 | `internal/tasktypes` + CLI |
| G4 | Finding.Tag 打标 + `forge task findings --stats` 聚合 | 进化 | P1 | `internal/tasktypes` + CLI + session-retrospective |
| G5 | doc-lint advisory hook（写时 WARN 提示） | 写时 | P1 | `internal/hookdispatch` |

### 不做清单（调研后显式拒绝，防反复重提）

| 不做 | 理由 |
|---|---|
| 训练期去偏（ALBM/因果增强/RaR 训练） | forge 是模型无关基建，够不着权重；外部约束是唯一可控杠杆——v1 开篇判断再确认 |
| verbosity API 旋钮（GPT-5 `verbosity` / Claude `effort`） | forge 不控制用户的模型调用参数；协议层「结论先行」注入已是等效杠杆 |
| 引入 Vale | doclint 已是同构物；Vale 生态是英文风格指南，中文 AI 指纹不覆盖；引依赖不解决新问题（懒惰阶梯） |
| 通用文档首屏结论检查进 L1 | 误伤面大（叙述型设计文档/ADR 合法后置背景）——typed docs 先走 D8 位置版，通用保持 L2（「不假装能自动化」原则对自身适用） |
| unslop 31 条全量收编 | 英文 tell（em dash/title case/curly quotes）对中文产物不适用；只收可机器判的中文等价物，其余随进化判据生长 |
| 多评委 ensemble 机器聚合 | 先 skill 层双评纪律攒分歧率数据，有数据再上 schema（YAGNI） |
| `forge docs lint --watch` | hook 挂点已存在（写时反馈走 hookdispatch），独立 watch 进程是第二份机制 |

## 落地组件

### P0-A：D8 结论位置规则（G1）

**规则形态**（scoped to typed docs，与 D5-D7 同层，模板文件豁免）：

- DocType 有 `RequiredHeadings` → 必填章节标题须出现在前 3 个 heading 内（advisory）——D5 判「有」，D8 判「靠前」
- DocType 有 `ConclusionEnum` → 枚举 token 须出现在前 10 个非围栏散文行内（advisory）——D6 判「有」，D8 判「靠前」

**判据**：存量 sweep（全仓 `forge docs lint` 人工分类命中）precision ≥80% 才考虑升 hard；低于则维持 advisory 并收窄规则。
**边界**：D8 是结论前置的代理不是判据本身——「位置对但内容空」仍由 L2 维度 1 把关；通用文档的结论前置不进 L1（见不做清单）。

### P0-B：D1/D2 中文清单二期 + 写作期微观文风（G2）

**D1 空转引导词增补 4 条**：`值得注意的是`（离题补充引导）、`总的来说`（模糊总结开场）、`不难发现`（推断伪装成观察）、`众所周知`（无出处断言）。
**D2 无证据判词增补 3 条**：`基本没问题`、`大体正常`、`看起来正常`（补齐 v1「看起来」系列的短形态）。`应该没问题` 落选：存量 sweep 20 处命中全是 skill 文档 Rationalization 表里带引号的借口引文（数据引用形态），短形态与引文撞车，完整形态 `应该没有问题` 已在表——按「存量 sweep 误伤 0」判据删除。
**豁免增补**：`skillintegrate/notes/` 进 doclint 豁免路径（go:embed 集成记录，文件名即 skill 名——BASE 名永久撞 retrospective 类型，D8 存量 sweep 的唯一命中即此，判定为文件名碰撞误伤）。

落地约束（继承 v1）：

- Reason 文本引用兄弟短语必须反引号（生成物须过自身 lint 的既有约束）
- 落地判据：全仓存量 sweep 误伤 0 + `skillgen` guard test（逐短语断言渲染）全绿
- 只进 doclint 表，不手抄进 skill 文本（单一真相源渲染管线不变）

**写作期注入扩充**（`qualitygen.go` 回复详略规则章节追加两条指引，文本级、非 lint）：

- 微观文风：同义词不轮换（一个概念一个词）；加粗只标真重点；`**标签：**复述标签内容` 型列表改散文；一句话放到任何其他项目文档里都成立，删
- 判定要带立场：推荐给理由、风险说大小——干净但没立场的文档不算结论前置

**判据**：生成物 guard test 全绿；「回复详略规则」章节行数增量 ≤6 行（注入文本有 token 成本，防清单膨胀）。

### P0-C：评委偏差防线（G3，文本级）

`rubric-docs.md` 评分纪律追加第 6 条（评委偏差防线）：

1. 详略维度与篇幅脱钩：更长的文档不因覆盖面广得分；打分前先列可删段，列不出的满档、列得出的按可删比例降档
2. 同家族评审（评审子代理与产出方模型同源）不得单独作为 borderline（70-79）放行依据——须第二评委复核，`--reviewer` 记两家 id，不一致取低分
3. plain-speech 判据进维度 2 满档锚点：句子在别的项目文档里也成立 = 不载信息

维度 2/4 档位描述补微观 tell 例：inline-header list（`**X：**` 后复述 X）、加粗滥用、同义词轮换——归入现有 10 分档「为格式加格式」的扩展例，不新增维度。

`doc-review/SKILL.md` 步骤 2（独立派审）增补：评审子代理上下文声明产出方模型家族（可得时）；borderline 双评流程指向上条。

**判据**：评审报告自身过 doclint + 每条发现带行号比例 100%（既有纪律不变）；新增观察指标——borderline 任务的 CoReview 记录率（P1-A 落地后可统计）。

### P1-A：双评记录 schema（G3，机制级）

`DocReview` 增量字段（omitempty，向后兼容，先例：Severity/DocsFingerprint）：

```go
SecondReviewer string `json:"second_reviewer,omitempty"` // 第二评委 id（同家族防线触发时）
SecondScore    int    `json:"second_score,omitempty"`    // 第二评委分数；gate 不消费，只记录
```

- CLI：`forge task doc-review` 增 `--co-reviewer --co-score` 可选参数
- gate 行为不变（不读 CoReview 字段）——分歧处理在 skill 层（取低分），机器只负责让分歧率可统计
- 判据：DocReviewHistory 可算出 borderline 双评率与分歧分布，为将来 ensemble 决策提供数据；分歧率 >30% 说明评委不稳，升级人工（数据说话，不预设）

### P1-B：Finding.Tag + 聚合 CLI（G4）

- `Finding` 增 `Tag string omitempty`，枚举对齐 doc-review 作弊速查表六类 + 微观文风：`padding | conclusion | template | false-precision | disclaimer | evidence | style`
- doc-review 步骤 4（发现四要素）增第 5 项可选 Tag（评审时打标；旧 findings 空 Tag 不影响）
- CLI：`forge task findings --stats --source doc-review` 输出 Tag × Round 聚合计数
- `session-retrospective` 步骤 6 的「同类打回 ≥3 次」判据从人工记忆改为：`--stats` 输出中同 Tag 同类型计数 ≥3，升级动作不变（进 doclint 表 / 进模板章节）
- 判据：一次真实 retrospective 能仅凭 `--stats` 输出完成（或不完成）升级判定——机器可复核

### P1-C：doc-lint advisory hook（G5）

`hookdispatch` 增 advisory emitter（对齐 `hook_kimi_advisory.go` 先例）：

- 触发：PostToolUse(Write|Edit) 目标为 .md 且路径非 doclint 豁免、无 SkipMarker
- 行为：对该文件跑 `LintFile`，命中 D 系列规则以 WARN 注入提示（带行号 + 修复方向），**不阻断**——写时反馈是提示不是门禁，执法仍在 complete
- 宿主矩阵：行为按宿主分级（对齐 task-guard 先例：多数宿主 WARN，不支持的宿主静默跳过），注入的是 lint 发现（来自 doclint 规则表）不是第二份散文文案——无漂移面
- 判据：单文件 lint 延迟 <200ms（纯内存扫描）；hook 命中到门禁命中的一致性（同一规则表，测试钉住）

## 收敛判据（整体）

- **适应度 1——L2 第一轮通过率**：P0 落地前先从存量 DocReviewHistory 统计 round-1 pass rate 基线；落地后观察 4 周，目标相对基线 +20%（写作期与 D8 前置应该在第一轮就消灭掉一部分低级打回）
- **适应度 2——L1 精度**：D1/D2/D8 新增规则存量 sweep precision ≥80%（人工抽检 ≥30 命中）；误伤超标的短语回滚（advisory 本身可承受误伤，hard 不行）
- **适应度 3——进化回路闭环**：P1-B 落地后第一次 retrospective 升级判定完全由 `--stats` 数据驱动，零人工翻记录
- 轮次上限/逃生舱/评分封顶等 v1 收敛机制全部不变

## 已知边界

- 评委身份与家族是自报的，机器无法强制（v1 边界延续，CoReview 同样）；防线靠 rubric 纪律条款 + 分歧率数据外显
- D8 只覆盖 typed docs；通用文档结论前置无 L1 代理是有意为之（不做清单）
- 写时 hook 是 WARN 提示，模型可以无视——它的价值是把违规可见性从 complete 提前到落盘，不是新执法点
- 中文短语清单的误伤风险由存量 sweep + 反引号豁免 + SkipMarker 三层兜底；新短语必须走「≥3 次打回 → 进表」的进化路径，不一次性穷举
- 本文档自身引用禁令短语处全部反引号包裹（引用是数据不是使用）——它也要能过自己设计出的门禁

## 来源（load-bearing 子集）

- IFEval 可验证指令：arXiv 2311.07911；饱和与可靠性再审视：arXiv 2512.14754
- 内在自纠错证伪：Huang et al. ICLR 2024（openreview IkmD3fKBPQ）；自纠错综述：Kamoi et al. TACL 2024（arXiv 2406.01297）
- 评委偏差：位置偏差 IJCNLP 2025；自偏好 arXiv 2410.21819；长度偏差治理 arXiv 2505.12843（v1 已引）
- 动态 rubric：Rubrics as Rewards（arXiv 2507.17746，ICLR'26）
- 业界形态：Vale（docs-as-code prose lint）、NeMo Guardrails / Guardrails AI（validator+judge 混合）、GPT-5 verbosity / Claude effort 参数、Writer/Grammarly 风格指南执行、unslop/no-ai-slop 社区 skill（调研全文在会话记录，2026-09-15）
