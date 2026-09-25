# ADR-0001: 采纳「约束三分类」词汇表并映射到 Forge 三档执法通道

**状态**：Accepted
**日期**：2026-09-20
**决策参与者**：用户（MjxUpUp）、ZCode agent（调研综合与批次实施）

## 背景

2026-09 的一次调研会话（「人类与 Agent 交互语言与沟通形式调研」，zcode 会话 sess_1ca53ff5）从人-AI 沟通偏差的六个根因推导出一套「最小理解偏差 + 确定性满足约束」的五步管道：①喂样例不喂形容词；②约束三分类；③动工前产出理解对账物；④机器跑不变量、人只审机器审不了的；⑤全程记账。

对照 Forge 现状核实（2026-09-20 挖掘）：

- **第 ④⑤ 步已被 Forge 既有宪法覆盖**——deterministic vs agent-claim 证据二分、「意见不走 hard」（regex 断言刻意排除、L2 rubric 永不进 gate）、`--trust-foreign` 真人终端判定、held-out 保留集（Goodhart 隔离：测试信号与优化目标分离）、checklog/decisions.md/HANDOFF 记账体系。Forge 独立收敛到了调研结论的输出侧。
- **第 ①②③ 步（输入侧）是空档**——任务级「样例对 ground truth」零落地；约束表达只有两极（可执行=shell 命令+exit 0+子串 / 不可命题=human 档审批），中间的「可命题」类（成不了命令、但可用规则+正反例锚定）无执法通道；理解对账物（原型确认/需求澄清产物）不会编译成验收考卷。

需要决策：这套方法论以什么形态进入 Forge——只做工程补洞，还是把词汇表升格为正式概念。

## 决策

1. **正式采纳「约束三分类」词汇表**，并与 Forge 既有执法通道一一映射：

   | 调研分类 | 判据 | Forge 执法通道 |
   |---|---|---|
   | 可执行（executable） | 能写成机械可判的命令/断言 | hard：acceptance v1 命令 + v2 五型断言（`--assert`），deterministic 证据 |
   | 可命题（propositionable） | 成不了命令，但能声明短规则 + 正反例，判官可对照判定 | κ 门控校准判官（L3，`forge eval judge-audit` 地板 0.6，不达标自动降级 ADVISORY，**永不进 hard**） |
   | 不可命题（non-propositionable） | 连命题化都做不到（品味/业务正确性终审） | human 档：机器守哈希事实 + 真人签字（`forge task artifact --approve --by`，必须可归因） |

2. **五步管道分三批次落地**（批次 A：断言判定分派 = 可执行类扩容 + `file-untouched` 爆炸半径不变量；批次 B：理解对账物→考卷编译器 = 样例对进 spec 模板 + 确认导出可编译；批次 C：L3 校准判官 = 可命题类的家）。
3. **「意见不走 hard」吸收调研结论而不松动**：调研的「人只审机器审不了的」与 Forge 既有宪法同向；可命题类走 κ 判官而非 hard 门禁，判官可靠性不足时降级披露而非硬拦。
4. 第 ④⑤ 步不再重复建设——既有机制即是落地，仅在文档里显式标注对应关系。

## 考虑过的方案

### 方案 A：仅做工程补洞，不引入新词汇表

**描述**：直接实现 L2 P1 断言分派与 L3 判官，不把「三分类」写成正式概念。

**优势**：改动面最小；不增加概念负担。

**劣势**：输入侧三步的**为什么**（喂样例的动机、三分类的判据）只活在一次性会话里，后续贡献者只见到机制不见方法论，演化时容易丢掉判据（如把可命题约束错推进 hard）；调研成果不可追溯。

### 方案 B：采纳词汇表 + 三档通道映射（本决策）

**描述**：词汇表进 ADR，映射关系钉死，批次按映射推进。

**优势**：三分类与 Forge 既有 advisory/rubric/human/hard 四档及考卷层级制自然咬合（词汇表给**输入侧分类判据**，四档给**执法强度**，正交不冲突）；后续每个新门禁可先问「这条约束属于哪类、归哪条通道」；方法论来源可追溯（本 ADR 即账）。

**劣势**：多一层概念；分类判据有灰区（可命题 vs 不可命题的边界靠 κ 可算性界定，需要实践校准）。

### 方案 C：五步管道整体升格为新的顶层工作流（如新 skill 全套承载）

**描述**：把五步做成端到端强制流程，每个任务都必须过「样例对账」。

**劣势**：与既有 requirement-clarification/prototype-verification/artifact-chain 大量重叠（重复机制 = 漂移点，违背「策略单一来源」）；强制全流程对轻任务过重（失败经济学：错误单价 × 发现滞后决定流程重量——调研自己的判据反对一刀切）。YAGNI。

## 理由

选方案 B：调研结论与 Forge 架构在输出侧（④⑤）已经殊途同归，证明两套思考兼容；输入侧（①②③）的空档用 Forge 已规划的 L2/L3 杆位承接，词汇表补的是「约束往哪条通道送」的判据层，不新造机制。方案 A 丢判据，方案 C 造重复机制。

接受的权衡：可命题/不可命题边界由「κ 可算性」操作化界定（能产出 ≥2 类别、≥2 条判分样本的规则才算可命题），灰区实践期先归 human 档（宁保守）。

## 影响

### 正面

- 每条新约束有明确投递通道：可执行→hard 断言；可命题→κ 判官；不可命题→human 签字。
- `file-untouched` 断言把「不可逆动作前缩小爆炸半径」（调研第 ④ 步）从原则变成任务级机械不变量。
- 理解侧（spec 样例对/原型确认导出）与执法侧（考卷层级 spec-extract 最高层）闭合：确认过的样例自动成为最高层级考卷。

### 负面

- 「可命题」通道（L3 判官）落地前，中间类约束只能暂居 human 档或退化成 executable 的近似——分类词汇先于执法能力到位（批次 C 前的过渡态）。
- ADR 词汇表与 focus-batches/oracle-pipeline 等既有设计文档存在概念交叉，需在这些文档演化时保持互指。

### 风险

- κ 判官被误用为 hard 的替代（Goodhart：判官可被优化目标污染）——红线「κ<0.6 自动降级 ADVISORY、判官永不进 hard」由代码强制（judgeaudit.go 地板常量），并受 L3 golden 校准。
- 三分类词汇被望文生义地扩大（把不可命题硬塞给判官）——判据操作化（κ 可算性）+ 本 ADR 灰区归 human 的保守规则对冲。

## 实施备注

- 批次 A（断言判定分派 + 本 ADR + 设计文档节）：feat/assertion-dispatch；落点 internal/taskpipeline/assertion_judge.go、internal/tasktypes/types.go、internal/clitask/task.go 等。
- 批次 B（spec 模板样例对/三分类节 + 原型确认可编译导出）：skills/requirement-clarification、skills/prototype-confirmation。
- 批次 C（golden harvest + 校准判官喂料）：internal/evalkit、internal/cli/eval.go、internal/cli/review.go。
- 词汇表定义处以本 ADR 为单一真相源；技能/设计文档引用不复制判据。

## 参考资料

- 调研会话：zcode sess_1ca53ff5-e1eb-497c-9b82-71bdf1e74ca9（人类与 Agent 交互语言与沟通形式调研）
- docs/design/leverage-points-landing.md（L2 spec-as-gate v2 / L3 校准判官）
- docs/design/oracle-pipeline-2026-09.md（考卷层级制/held-out 池——第 ④⑤ 步的既有落地）
- docs/design/compat-commitments.md §三（纯新增 flag 裁决——批次 A 的命令面变更依据）
