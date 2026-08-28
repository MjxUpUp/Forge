# code-review-gate — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮
agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。
append-only：新决策追加到末尾。

> 用 `forge skills decide --skill code-review-gate ...` 追加新决策，勿手编。
> 下面是一条示例决策（基于真实历史 8e00456），展示四元组写法。

## [d-1778921400123456789-a1b2c3d4] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-07-18T09:30:00Z
- **By**: claude-code
- **Commit**: 8e00456

### Diagnosis

`forge review pass` 是声明式——只标记「审查通过」不实际执行审查。空 pass 的任务
（agent 没真派 code-reviewer 就 pass）会漏检；更隐蔽的是 pass 之后、task-complete
之前 agent 还能改代码，pass 的「通过」承诺与实际代码脱节。诊断线索：审查发现的
修复引入的副作用第一轮看不到（projectName 末两段拼接→Project 含测试名误触发
session 断言），说明「审查→修复」链路里修复阶段无人复审，pass 形同虚设。

### Revision

feat/review-snapshot（8e00456）：review pass 绑定代码快照——ReviewedHeadCommit +
ReviewedChangeHash 写进 TaskState。task-complete 门禁加硬前置 ReviewPassed，且比对
当前 HEAD == ReviewedHeadCommit：审查后改码即拒（drift 检测）。

### Evidence

回归探针：构造「pass 后改码」场景 → task-complete gate BLOCKED（HEAD 漂移），exit
非 0；「未改码」场景 → 放行。两条路径断言相反结果，覆盖 drift 检测分支（accept
的依据不是断言而是实跑的 BLOCKED/放行对比）。

### Rationale

pass 是 agent 自律最薄弱的环节（声明成本极低），绑定快照把「声明」锚到不可伪造
的 git 状态上，与 file-sentinel 同源思路（git diff = deterministic 证据）。drift
检测让「审查后偷改」从隐性行为变成硬阻断，比加更多审查 checklist 更治本。

## [d-18c771eb72121754-8d797f6a] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-07-31T18:03:53Z

### Diagnosis

整体审查发现 :126/:132/:233 三处裸方括号 [references/xxx.md] 缺链接目标，Markdown 渲染不成链接

### Revision

三处补齐 (references/xxx.md) 链接目标

### Evidence

改后 grep 'references/.*\.md\]' 无未带 ( 的残留

## [d-18c77baa46f23fa4-886a12ef] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-07-31T21:02:28Z

### Diagnosis

behavior-probe 维度全库仅本 skill 的 probes.yaml 一个消费方（约 500 行服务单点），决策拆除不推广

### Revision

删除 probes.yaml 资产；skillseval 的 probes.go/judgeBehavior/behaviorPassRate 通路、cli eval 命令的 probe 相关输出与脱敏机制同步拆除

### Evidence

audit 确认 53 个 skill 中唯一消费方；probe 字段不参与 caseID/DescHash，拆除对存量 case 集 hash 零影响

## [d-18c7e5a6381ad110-43d8bf25] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-02T05:24:39Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

建「改名/删符号后的调用方检查」canonical 节(D4)；forge 段落下沉 references/forge-integration.md；补叠加专项审查输出协议(block 以下也须显式回应)；86%/72% 统计补 Veracode 2025 来源；双语注释规范迁入(maintainability-and-readability 合并)

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e620ad442c3c-2679d4f1] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18cd113414153e18-c97623dd] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-19T01:39:02Z

### Diagnosis

cheat-scan 从 4 类扩到 6 类（comment-as-debt 早已是第 5 类，文档滞后；phantom-import 本次新增）——skill 文档的预扫清单与实现漂移，子 agent 会重复判断已被机械判过的模式

### Revision

SKILL.md 与 references/forge-integration.md 的 cheat-scan 预扫节更新为 6 类枚举，注明 phantom-import 只覆盖相对路径、外部包仍归语义审查；plugins/forge 镜像同步

### Evidence

internal/taskpipeline/cheatscan.go ScanCheatPatterns 实跑 6 个检测器；TestDetectPhantomImport + TestExecuteTaskGate_CheatScan_PhantomImport 通过

## [d-18cd18075a8b76d0-63946ae4] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-19T03:44:06Z

### Diagnosis

cheat-scan 扩到 7 类（新增 path-assumption：OS 分隔符当内容匹配器——2026-08-19 Windows CI 事故的指纹），文档预扫清单需同步

### Revision

SKILL.md + forge-integration.md 预扫节更新为 7 类枚举

### Evidence

internal/taskpipeline/cheatscan.go detectPathAssumption + TestDetectPathAssumption 通过

## [d-18ce6886e095c020-d776841e] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-23T10:30:30Z
- **By**: dsh-glm
- **Commit**: 6c54c68

### Diagnosis

协议缺口：快照闭环（d-1778921400123456789 引入的 review-fix-recheck）只强制循环形状（改码后必须重新盖章），不检验复审实质——修复者可不派复审直接 forge review pass，重盖输出与诚实轮零差别，task-complete 照样放行。真实案例（2026-08 本仓库）：主修复 commit 经第一轮 review 报 6 项发现，修复 commit 后仅盖章未复审即过门禁；用户质询后补的第二轮复审实际抓到 1 项 PARTIAL + 1 处漏网谎报

### Revision

三层：① review pass 在「代码快照自上一轮已变」的重盖上打 ADVISORY（快照增量触发非裸轮次计数，同状态重盖静默）；② SKILL.md 新增「审查-修复-复审闭环」节（复审义务/范围三件套/三项禁止：自证盖章、测试当复审、同 agent 续看）；③ claudemd common-errors 表 code-review-gate 行补「复审修复」步骤

### Evidence

TestRunReviewPassAt_ReworkRoundRequiresRecheck（git 夹具三轮：首章静默/同状态重盖静默/快照变更后重盖必含复审 ADVISORY）；TestClaudeMDReviewFixLoopIncludesRecheck；本仓库 2026-08-23 会话三轮审查实录

### Rationale

复审实质不可机械判定（HARD 会引入可伪造凭据字段），ADVISORY 让义务在欠下的确切时刻可见；快照增量触发消除了无变更重盖的误报

## [d-18cf05d41d876778-9c6ef3ab] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-25T10:33:05Z

### Diagnosis

输出可读性设计需要 L2 rubric 的单一真相源落在 code-review-gate/references/（与 phase-*.md 同址，编写期 design-artifact-standards 路由、审查期本 skill 消费）

### Revision

新增 references/rubric-docs.md（四维 0-25 档位判据 + 评分纪律五条 + 类型特化 Critical 表）；6 个 phase-*.md 确定性规则表各追加一行指向 forge docs lint

### Evidence

docs/design/output-readability-gates.md；task-verify 的 unused-scan/doc advisory 实测链路

## [d-18cf07cf792bf970-865de5e8] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-25T11:09:24Z
- **By**: kimi

### Diagnosis

usage 日志实证：task-complete 因「审查通过后检测到源码变更」HARD 拦截后，agent 不重派只读子 agent 复审、直接自己再跑 forge review pass 即静默刷新基线放行（防君子不防小人）；修复后未复审直接盖章的行为也发生过。旧的盖章后 ADVISORY 只提示不拦截，输出与诚实轮零差别

### Revision

SKILL.md 审查-修复-复审闭环节：复审义务的机制描述从「盖章后 ADVISORY 提示」改为「裸盖章被拒」——距上次基线有源码内容变更时 forge review pass 须 --note 记复审结论或 --acknowledge-changes 自我承担（WARN 级 self-refresh 审计）；非源码变更（amend message 等）指纹不变无需确认

### Evidence

internal/cli/review.go self-refresh 守卫 + TestRunReviewPassAt_ReworkRoundRequiresRecheck 改写：裸盖章被拒且不落章、--note 放行、--acknowledge-changes 放行且记 WARN self-refresh、amend-only 静默

### Rationale

守卫让自助刷新从隐性行为变成显式可审计动作：--note 对应诚实复审流（结论留痕），--acknowledge-changes 对应自我承担（WARN 审计），平衡可用性（不硬堵 amend 类非源码变更）

## [d-18cf554374aa2630-297a6d15] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-26T10:48:45Z

### Diagnosis

AI CR 不稳定的本地形态不是误报（一周 25+ 次审查 0 误报）而是发现集不可复现：7 起后轮报前轮未见新发现、2 起双轨分歧、40 次 review-pass 仅 1 次带 note（rubber-stamp 不可辨）

### Revision

复审轮新发现三选一并归因（抽样未覆盖/修复引入/范围外）写进报告与 --note；双轨大分歧判读为欠采样信号加派不同视角 pass（明确非多数投票过滤，守住不分级原则）；--note 必须记覆盖范围与验证动作；发现用 forge task finding 录入承接 Round/ChangeHash 可度量性

### Evidence

kimi 转录一周 17 个审查 episode 取证 + forge 执行记录 6 项度量盲区分析；业界 Bugbot 多 pass 共识与 Snap Verifier 经验按本地数据裁剪（防漏检优先于防噪声）

## [d-18d0a1f3c02b4e11-b1c2d3e4] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-28T07:30:23Z
- **By**: zcode

### Diagnosis

references/ 内 7 个文件与代码审查无关：rubric-docs.md（文档 L2 评分）与 phase-*.md ×6（设计产物标准清单，编写期主场在 design-artifact-standards，寄居此处导致对方需跨 skill `../` 深链 + requires 提示 + 绝对路径兜底）——SRP 违例（变化频率/受众不同的资产共居）

### Revision

rubric-docs.md 迁出至新建 doc-review skill；phase-*.md ×6 迁至 design-artifact-standards/references/（标准随编写期主场走）；本 skill 保留纯代码资产（双轨审查/作弊指纹/审查清单/过度工程/SQL 安全/子代理契约）；forge-integration 的环节加载表改指新家并注明迁移

### Evidence

design-artifact-standards/SKILL.md 原维护注记自证断链风险（2026-08 Kimi 宿主实测裸 ../ 断链）；仓库 grep：phase-* 消费方为 design-artifact-standards（无条件）与 code-review-gate（forge 条件）——依赖箭头反了

### Rationale

拆分判据用 SRP 的变化频率表述：作弊指纹表随 AI 模式演进、phase 清单随设计标准演进、rubric 随文档质量工作演进——三种频率三批受众不共居


## [d-18d0b3a105897-a105897b5] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-28T07:57:58Z
- **By**: zcode

### Diagnosis

review-checklist 维度 3 名为 SOLID 实为 SRP/DRY/YAGNI（DRY/YAGNI 非 SOLID 成员），OCP/ISP 仅维度 4 一行带过，LSP 完全缺席；契约完整性与类型纪律无正向条文；大 diff 无拆分纪律

### Revision

维度 3 重写为真·SOLID 五原则（SRP 变化频率判据/OCP 侵入式耦合/LSP 行为契约三禁 + 测试全绿≠LSP/ISP/DIP）+ 契约完整性节（前置条件/边界断言/跨层一致）+ 类型纪律节（非法状态不可表示/parse-don't-validate/type-suppression 正解）；维度 4 去重指回维度 3；步骤 1 加 ~400 行 diff 拆分纪律；over-engineering-checklist 挂 Brooks 本质/偶然复杂度锚点；维度 2 点名 fail fast

### Evidence

feat/skills-boundary-inversion Phase 3（工程原则增强）：会话研究结论——SOLID/契约/属性测试在 AI 时代升值（生成速度>审查速度），仓库原有清单存在 LSP 空缺与规格档空白

### Rationale

SOLID 本质是管理知识流动（耦合），AI 时代生成速度超过审查速度使耦合债加速复利；LSP 与契约完整性是 AI 代码高发盲区（断言只验证子类自身不验证可替换性）

## [d-18cffa7921853978-b23ed69d] accept

- **Skill**: code-review-gate
- **DecidedAt**: 2026-08-28T13:16:15Z
- **By**: claude-code

### Diagnosis

SKILL.md/references 含 forge 操作性引用（条件块/forge-integration.md/双路径/模板占位符）——违反 skills 零反向依赖契约（CONVENTIONS §13 R18 硬校验），存量豁免通道要求迁出

### Revision

forge 集成内容整体迁出至 forge 侧 internal/skillintegrate notes/（forge skills integration code-review-gate 查看，skill-trigger 推荐块附指针）；正文改为工具中立方法论（降级路径升为主路径/宿主机制中性措辞）

### Evidence

forge skills validate 53/53 通过且 R18Grandfathered 清空；TestR18_Grandfathered_Exact 双向卡死通过

### Rationale

依赖单向化：方法论完整留在中立库，forge 增强完整在 forge 侧；forge 用户体验经集成笔记+触发指针承接
