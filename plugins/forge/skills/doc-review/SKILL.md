---
name: doc-review
description: "文档质量审查与 L2 评分门控（PR 描述/commit body/测试报告/复盘报告/落盘文档）。Use when: 审查文档质量、给文档评分、说\"帮我审下这份文档\"\"这文档写得怎么样\"\"文档太长了帮我看看\"时、文档回检（L2）时。SKIP: 代码审查（用 code-review-gate）、设计产物按环节清单搭骨架自查（用 design-artifact-standards）、按模板生成文档（用 doc-generator）、文档与代码真相源的一致性守卫（用 docs-consistency-guard）、机器可判的禁令/结构/篇幅 lint（forge docs lint）。"
metadata:
  pattern: reviewer + gate
  domain: documentation
---

# 文档审查门控（L2）

对「给人即时阅读的文档产物」做质量审查与评分门控——PR 描述、commit body、测试报告、复盘报告、tech-plan、release notes 及任务产出的任何落盘 .md。与 code-review-gate 是兄弟门控：那边拦截 AI 作弊代码进主干，这边拦截 AI 注水文档进仓库。

## 核心原则

- **文档的成本在读不在写**：一份文档只有一次写作和 N 次阅读，审查保护的是 N 次阅读的时间。篇幅本身不是问题，篇幅不载信息才是。
- **AI 文档有可识别的作弊指纹**：注水拉长、规避结论、模板腔、假精确、免责堆砌——与代码侧作弊指纹同源（code-review-gate 的 [references/ai-cheat-patterns.md](../code-review-gate/references/ai-cheat-patterns.md)），为「看起来完成」而注水，见下方速查表。
- **评分是门控不是建议**：机器可判层全过 + 四维总分 ≥75 + 零 Critical 未决才放行；分级表达处理顺序，不表达可忽略。

## When to Use / When NOT to Use

**Use when:**
- 文档写完要落盘/提交/发出前做质量把关
- 用户说「帮我审下这份文档」「这文档写得怎么样」「太长了帮我看看」
- 文档回检（写完 → 独立评审 → 修改 → 再评的循环）
- PR 描述、commit body、测试报告、复盘报告的质量判断

**SKIP（路由到更专业的 skill）:**
- **代码审查** → `code-review-gate`（兄弟门控，管代码不管文档）
- **设计产物该有什么**（章节骨架/维度覆盖自查）→ `design-artifact-standards`（编写期标准；先骨架后评分，两者构成 producer-chain——先按标准产出、再按门控评审）
- **按模板生成文档** → `doc-generator`（producer，本 skill 是 reviewer）
- **文档与代码是否一致**（命令表漂移/计数过期）→ `docs-consistency-guard`（管对不对，本 skill 管写得好不好）
- **机器可判的结构问题**（禁令短语/必填章节/结论枚举/篇幅）→ L1 lint，命中直接打回修，不进本 skill 评分

## 审查流程

### 步骤 1：定类型、过机器层

1. 判断文档类型（PR 描述 / commit body / 测试报告 / 复盘报告 / 其他）——决定用哪行类型特化判据（rubric 第 3 节）。
2. 机器可判的结构问题先修掉，不带进评分：禁令短语（`整体良好` `综上所述` 类）、必填章节缺失、结论非枚举值、篇幅超限。

> Forge 项目：落盘 .md 先跑 `forge docs lint`（L1，D1-D7 规则，exit 0=过/2=硬失败），PR 描述与 commit body 不落盘、按模板结构人工核对。非 forge 项目按上述结构清单人工过一遍（机制见 [references/forge-integration.md](references/forge-integration.md)）。

### 步骤 2：独立派审

派**只读独立子代理**执行评审，对抗立场（默认「有问题，找出来」，不是「确认没问题」）。子代理契约：只读不写、按 rubric 四维逐维打分、发现按步骤 4 格式输出。

### 步骤 3：四维评分 + 类型特化

加载 [references/rubric-docs.md](references/rubric-docs.md)：

- **通用四维**（各 0-25，总分 100，**阈值 75**）：结论前置（读者最关心的在第几行）/ 详略得当（篇幅与信息量成正比）/ 证据支撑（断言可追溯）/ 受众匹配（写给谁用谁的语言）。
- **类型特化**：按文档类型叠加 Critical 项（如 PR 描述无动机段、测试报告结论非枚举值）——Critical 任一命中即阻断，计入未决 findings。

### 步骤 4：发现四要素

每条发现缺一不可（与 code-review-gate 同协议）：

1. **位置**（行号 `L12`）
2. **问题**（引用原文片段，不是泛泛而谈）
3. **背景分析**：为什么是问题——结合这篇文档的类型、受众、它服务的决策
4. **解决方向**：delete-list 格式（删哪句/压成几行/换成什么），可执行

### 步骤 5：门控决策与收敛

- **放行**：机器层全过 + 总分 ≥75 + 零 Critical 未决（分级 Critical/Important/Minor 表达处理顺序；Important/Minor 也须显式回应——修复或论证不需修，不得悬置）
- **打回**：任一不满足 → 列未决项，修改后**重新派独立子代理评审**（作者不能自证修改合格）
- **收敛判据**：3 轮仍 <75 → 升级人工确认，不无限打磨；同类发现连续两轮不收敛 → 异常信号（评审方或修改方有一侧在空转），换视角或换人，不要第三轮硬磨

> Forge 项目：评审通过后用 `forge task doc-review --passed pass --score <N>` 落档证据（未记录/过期/低分会被 doc gate 拦 complete）；逃生口与门禁细节见 [references/forge-integration.md](references/forge-integration.md)。非 forge 项目跳过落档，报告直接交付。

## 文档作弊模式速查（先扫这个表）

与代码侧 AI 作弊指纹对称，人类审查最容易漏、AI 产出最高频的模式：

| 作弊类型 | 指纹 | 为什么是问题 |
|---|---|---|
| **注水拉长** padding | 填充话（`综上所述`、复述前文）、大段复述 diff/源码/既有文档 | 篇幅不载信息，读者的时间被偷走 |
| **规避结论** conclusion-dodging | 读完 1/3 还不知道判定/推荐；只罗列选项不裁决 | 文档的存在理由是支撑决策，规避结论 = 白写 |
| **模板腔** template-speak | 「本文介绍了…」目录话、没人要求的 P0/P1/P2 排序、空表格、为格式加格式 | 格式表演替代信息，看起来专业实际空心 |
| **假精确** false-precision | 无出处的百分比/数字；「应该」「看起来」伪装成断言 | 不可追溯的精确比诚实的模糊更有害——读者会信 |
| **免责堆砌** disclaimer-stacking | 断言后跟成串前提条件/兜底免责 | 把判断责任推给读者，文档失去决策价值 |
| **证据裸奔** evidence-free-claim | 通过/正常/无风险类断言零指针（无命令+结果、无 file:line、无 issue） | 通过性断言无证据 = 未验证的谎报 |

## Rationalizations（堵借口）

| 借口 | 现实 |
|---|---|
| "文档而已，不用这么严" | 文档是给未来读者的一手接口；错误文档比没有文档更贵（读者会信） |
| "内容都对就行" | 对但读不进去 = 没传达。四维里三个维度都在管传达 |
| "写详细一点总没错" | 详略是维度不是越长越好；每段都要有信息增量，否则给 delete-list |
| "结论我心里有数" | 读者不知道。结论前置是四维之首 |
| "AI 写的都这样" | 指纹可识别、可修改，这正是本 skill 存在的理由 |
| "我自己检查过了" | 产出者不能自检——系统性偏好不随「再读一遍」消失，换独立上下文 |
| "评分主观没意义" | 档位判据 + 行号证据 + 类型特化锚点，主观性被压到档位粒度 |

## Red Flags（看到这些想法 = STOP）

- `整体良好`/`基本清楚` 式无行号结论 → 证据裸奔，重写发现
- 「建议精简一下」→ 不给 delete-list 的建议是新一轮注水
- 自己审自己刚写的 → 换独立子代理
- 打了分但没引用任何行号 → 评分不可证伪，作废重评
- 「先把文档发出去，回头再改」→ 进了仓库的注水文档没有回头改的一天

## Gotchas（从实际失败积累）

- **评审报告自身受同样约束**：分级、每条带行号、给 delete-list——评审冗长即评审不合格。
- **机器层不占评分维度**：L1 能判的（禁令/结构/篇幅）命中直接打回修，不折算进四维分数——两层各管各的。
- **L3 不假装能评**：业务价值判断、对外措辞的敏感度，显式标「需人工确认」，评分时跳过。
- **类型特化会进化**：新文档类型先用通用四维兜底，同类发现打回 ≥3 次再扩特化行（防止一次性穷举）。
- **「太长」多数是结构问题不是文风问题**：先查该下沉的是否下沉（明细/日志/复述），再查是否规避结论导致正文承载了本该前置的裁决。

## 与其他 skill 的分工

- **code-review-gate**：兄弟门控，管代码。两者叠加时（改码 + 改文档的任务）各自独立执行。
- **design-artifact-standards**：编写期管「该有什么」（骨架/维度覆盖），本 skill 审查期管「写得怎么样」。producer-chain：先按标准搭骨架，再按本 skill 评分。
- **doc-generator**：按模板生成（producer），本 skill 评审（reviewer）。
- **docs-consistency-guard**：管文档与代码真相源的一致性（命令表/计数漂移），与质量维度正交。
- **session-retrospective**：文档回检模式提炼（同类打回 ≥3 次升 L1 规则）是本 skill rubric 的进化机制。

## 参考

- 四维档位判据 + 类型特化 + 评分纪律：[references/rubric-docs.md](references/rubric-docs.md)
- Forge 集成（L1 lint / doc gate 落档 / 逃生口，仅 forge 项目适用）：[references/forge-integration.md](references/forge-integration.md)
