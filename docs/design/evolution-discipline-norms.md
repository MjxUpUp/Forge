# 演化纪律规范：优化判定 / deferred 承诺 / 迁移编舞（2026-09）

- 依据：2026-09-09 会话回顾——dashboard 告警实证（11 告警 = 0 僵尸 + 11 回顾提醒，其中 8 条逃生舱降档噪音 + 3 条同维度真弱模式）触发的方法论讨论；业界实践对照（SRE 告警分级、SonarQube finding 分诊、SPC 特殊原因判定、Deming tampering）。
- 范围：把讨论中裁断出的**三个真缺口**落地为规范（2 个 skill 修订 + 1 个审查项），其余讨论观点按载体决策树裁断——已覆盖的不重复落地，防止规范注水。
- 配套决策留痕：`forge skills decide` 四元组（implementation-discipline / evidence-based-proposal / code-review-gate）。

---

## 一、裁断表：讨论观点 → 载体（按 session-retrospective 载体决策树）

| 讨论观点 | 裁断 | 载体 / 去向 |
|---|---|---|
| 行为保持靠特征测试、先失败测试 | **已覆盖**，不重复落地 | tdd-cycle |
| 验证表面=目标表面、证据与声称同态 | **已覆盖** | implementation-discipline 阶段 3 + Gotchas（dc10） |
| 最小 diff 补错层=第二个 bug、全链 grep 策略拷贝 | **已覆盖** | implementation-discipline 阶段 1 |
| YAGNI / 懒惰阶梯 / 单实现不抽象 / 死代码删除 | **已覆盖**（含"抽象交租、删除预算"——delete/yagni tag 已编码） | implementation-discipline 阶段 1 + code-review-gate/references/over-engineering-checklist.md |
| 范式级先原型拍板（≈单向门决策分类） | **已覆盖** | prototype-confirmation 前置门 |
| 兼容面机械钉住（mirrors+守护测试、compat.snapshot） | **已覆盖** | conventions profile |
| **事前成功度量（适应度函数门）** | **缺口①，本次落地** | evidence-based-proposal 第三必答题 + implementation-discipline 阶段 0 门控一行 |
| **deferred 承诺无载体即腐烂** | **缺口②（元规则），本次落地** | implementation-discipline 阶段 1「"以后"的三要素」 |
| **行为与结构不混车 / expand-contract 迁移编舞** | **缺口③，本次落地（清单级）+ 升格实验** | code-review-gate 步骤 1 变更结构纪律；升格判据见第四节 |
| DDD/SDD/TDD 在 AI 时代的定位（背景哲学） | 非操作门控，落 docs | 本笔记第三节 |

## 二、三项规范的理由

**① 事前成功度量（适应度函数门）**。「优化」是对某个可测目标的声明——没有事前声明的度量，改进与退化在感受上不可区分，且事后选的指标总会"恰好"支持结论。落地形态：方案必答第三问（移动哪个数字、哪个表面、怎么测），改后同数据集/同表面 before/after 实测。与 dc10 教训同源：证据的测量时刻必须与声称时刻同态。

**② deferred 承诺三要素（元规则）**。「以后再说」若不带机制就是写在对话里的 TODO——标注 ≠ 解决。合法的推迟必须同时带：触发条件（当 X 发生）、阈值/复验日期（≥N 次 / 到期 D）、复验节律的消费方（周期回顾扫 open 项）。缺任何一样的"以后"视同已丢弃；到期复验二选一：升格执行或带理由显式放弃。这条是让其他所有 deferred 承诺（包括本笔记第四节的升格实验）不腐烂的元规则。

**③ 变更结构纪律（不混车 + expand-contract）**。行为变更与结构变更混在同一 diff 时无法分别验证：重构的证明是"钉住行为的测试全绿 + 零行为差异"，行为变更的证明是"先失败后通过"——混合 diff 两种证明都不成立。大迁移走 expand→migrate→contract，删除是独立可回滚步骤。仓内实践已有先例（registry 迁移、paths.go 僵尸 accessor 清理），本项把实践升为审查可查项。

## 三、DDD/SDD/TDD 在 AI 时代的定位（背景）

AI coding 改变的前提：生产者变成非确定性、无部落知识、每会话失忆、会在名字/API 上幻觉的协作者；写码边际成本趋零，验证与审读成本未降。三个经典模型各自升一维：

- **TDD：开发实践 → 对非确定性生产者的验收契约**。失败测试是唯一无歧义、可直接交给生成器的规约；"看着它失败"更关键（模型会写出空泛通过的测试）；与反伪造证据链结合（agent 自述不可信，只信实跑证据——forge checklog 的定位）。
- **SDD：先写规约 → spec 为第一等评审对象，代码为派生物**。评审便宜的东西（契约），生成昂贵的东西（实现）；新技术债形态是 spec 漂移与 spec-code 一致性（conventions digest / compat.snapshot 是其实现）。
- **DDD：团队协作工具 → 上下文工程**。限界上下文=上下文窗口预算的分区；统一语言=模型必须使用的可 grep 词汇表；领域模型切错被生成放大百倍，成为杠杆最高的不可逆决策。

覆盖度结论：三者盖住"设计+验证"轴（约六成）；"时间演进"轴需要适应度函数（判定优化）、迁移编舞（渐进）、经济学纪律（防过度工程）补齐——即本笔记落地的三项 + 既有懒惰阶梯。

## 四、升格判据（deferred 三要素实例：code-review-gate 审查项 → 独立 skill）

code-review-gate 的「变更结构纪律」现为**清单级**（一段纪律 + 一条 Red Flag）。若下述判据命中，按 skill-authoring-standard 升格为 skills-forge 独立 skill（迁移编舞：expand-contract 全流程 / 绞杀者模式 / 多消费方编排）：

- **触发条件①**：90 天窗口内，审查中该纪律项实际命中 ≥2 次（发现真实混车 diff）；
- **触发条件②**：发现 1 次本应被该纪律拦截却漏拦的混车迁移（漏检即信号）；
- **触发条件③**：≥3 个 skill/文档需要引用 expand-contract 细则（复制即抽象信号）。
- **复验日期**：2026-12-09（落地后 90 天）。
- **复验节律**：周度回顾（session-retrospective）扫 open deferred 项时逐条核对触发条件；`grep -rn "迁移编舞\|expand-contract" skills/ docs/` 可机械核对命中计数。
- **到期处置**：命中任一 → 升格（走 skill-authoring-standard）；未命中 → 带理由显式 dismiss 或显式续期（改本节日期），不允许无期限续挂。

## 五、证据与出处

- 告警实证：`~/.forge/projects/6d5e51a0f9d4/act/conclusions.jsonl`（81 条结论，14 天窗 34 条，nudge 11 = 逃生舱降档 8 + 真弱 3；3 条真弱同维度 scope/efficiency/expression——run-rule 式模式信号）。
- 业界对照：Google SRE Workbook（page vs ticket、多窗口烧毁率）；AACN/AHRQ 警报管理（分级 + ack 工作流 + 数据化校准委员会）；SonarQube finding 分诊四态（open / false-positive / won't-fix / accepted，带理由留痕）；SPC Western Electric/Nelson run rules（模式规则 vs 单点阈值）；Deming 漏斗实验（对共同原因逐点反应 = tampering，反而增大方差）。
