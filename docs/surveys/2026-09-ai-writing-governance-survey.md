# AI 写作治理方案调研（写作期约束 × 产出期复诊）

> 状态：已完成（2026-09-15，调研会话）。结论已落地为 [output-readability-gates-v2.md](../design/output-readability-gates-v2.md)（本文档是其 proposal 上游产物）。
> 问题：业界学界对「AI 产物啰嗦/注水」有哪些方案，各解决什么痛点，Forge 的写作期约束 + L1/L2 门禁是否有更好的实践可吸收。

## 结论（先读这个）

没有单一现成方案优于 Forge 的「写作期注入 + L1 确定性 lint + L2 独立评审门禁」——该架构与业界前沿同构。可吸收五项（已进 v2 设计）：结论前置下沉 L1、评委偏差防线、rubric 进化半自动化、写作期微观文风注入、写时反馈 hook。显式不做五项（见文末）。

## 方案地图（按生效时机）

| 时机层 | 代表方案 | 解决的痛点 | 局限 |
|---|---|---|---|
| 写作期·提示注入 | `unslop` / `no-ai-slop` / `avoid-ai-writing` 类 skill（20-31 条模式） | AI 文风指纹 | 零强制力，跳过零后果 |
| 写作期·实时助手 | Writer.com Voice、Grammarly Business 风格指南 | 品牌语言/术语一致性 | 只在自家编辑器生态生效 |
| 写作期·模型旋钮 | GPT-5 `verbosity`、Claude `effort`、Chain-of-Draft、TALE | 啰嗦的根因补偿 | 只管长度不管质量；推理 token ≠ 交付文本 |
| 产出期·确定性 lint | Vale（docs-as-code 事实标准）、IFEval（学术标准形） | 机器可判的结构/禁令/篇幅违规 | 管不了语义层「重点是否前置」 |
| 产出期·模型评审 | Rubrics as Rewards（ICLR'26）、rubric 自动生成（Autorubric/EvoRubrics） | 语义层质量 | 评委自身有五类偏差 |
| 产出期·运行时 guardrails | NeMo Guardrails、Guardrails AI、OpenAI Agents SDK guardrails | 服务路径即时校验 | 面向 safety/格式，无持久证据链 |
| 训练期（学界） | 长度偏差去偏（ALBM 等）、因果增强、RaR 训练 | 啰嗦根因本身 | 需改模型权重，模型无关基建够不着 |

## 两条对架构直接相关的学界定论

1. **内在自纠错已被证伪，独立评审是共识**：Huang et al.（ICLR 2024）实证无外部反馈的 self-correction 常把对的改成错的；Kamoi et al.（TACL 2024）综述同结论，失败集中在「发现错误」环节。Forge「产出者不能自检」站在实证正确一侧；`unslop` 类 skill 的 self-audit 步骤是已证伪模式。
2. **评委偏差五类，各有缓解**：位置（IJCNLP 2025）、自偏好（arXiv 2410.21819）、长度、格式、参考——缓解手段有 position swap、长度归一、证据锚定判分、跨家族评委、多评委集成。Forge 的带行号 + delete-list 纪律已是「证据锚定判分」，但同家族评委/无长度控制/单评委三点敞开。

## 吸收清单（→ v2 设计 P0/P1）

1. 结论位置下沉 L1（IFEval 式可验证规则，typed docs advisory）→ D8
2. 评委偏差防线（详略与篇幅脱钩 + 同家族 borderline 双评）→ rubric 第 6 条
3. rubric 进化半自动化（`--stats` 聚合替代人工记忆）→ P1-B
4. 写作期微观文风 + 正面条款注入 → qualitygen
5. 写时反馈 hook（Vale editor+CI 双反馈模式）→ P1-C

## 不做清单（调研后显式拒绝）

训练期去偏（够不着权重）；verbosity API 旋钮（forge 不控制用户模型调用）；引入 Vale（doclint 已同构，英文风格生态不覆盖中文 AI 指纹）；unslop 全量收编（英文 tell 不适用中文产物）；多评委 ensemble 机器聚合（先攒分歧率数据，YAGNI）；通用文档首屏结论检查进 L1（叙述型文档误伤面大）。

## 来源（load-bearing 子集）

- IFEval（arXiv 2311.07911）；饱和再审视（arXiv 2512.14754）
- 自纠错证伪：Huang et al. ICLR 2024（openreview IkmD3fKBPQ）；Kamoi et al. TACL 2024（arXiv 2406.01297）
- 评委偏差：IJCNLP 2025 位置偏差系统研究；自偏好 arXiv 2410.21819；长度偏差治理 arXiv 2505.12843；五类偏差与生产级缓解盘点（futureagi LLM-Judge Bias Mitigation 2026）
- 动态 rubric：Rubrics as Rewards（arXiv 2507.17746）
- 业界：Vale（vale.sh）、NeMo Guardrails / Guardrails AI、GPT-5 verbosity / Claude effort 参数、Writer Voice / Grammarly Business、unslop / no-ai-slop 社区 skill
