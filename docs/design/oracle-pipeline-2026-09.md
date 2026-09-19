# Oracle Pipeline：正确性的传递链（2026-09）

> 状态：阶段一、二已落地；阶段三按本文推进。
> 起点：用户工作模式转向「只接受和验收交付结果」——人在环路上只剩两站：
> 入口（用业务判断定义"什么算对"）与出口（验收交付）。Forge 的职责从
> "验证执行的引擎"升级为"正确性的保值管道"：业务判断从入口注入，到出口
> 交付时要么带着不可伪造的证据，要么带着显式的未验证声明，中途任何环节
> 不得稀释或伪造。

## 问题

1. **零验收平凡通过（P0）**：无 `--accept` 时 verify-acceptance 直接返回退出码 0、
   CheckAcceptanceFresh 对空集放行——deterministic 核心是"选装"的。实证：
   2026-09-07 project-policy-p234 以 ratio 0.08（4 det / 47 agent-claim）完成，
   根因即零登记。
2. **考卷无出处分**：实现者自写考卷与 spec 提取的考卷在证据里不可区分——
   共模故障（测试与实现共享同一误解，全绿）不可见。
3. **交付无单页视图**：验收方要拼 task status / trace / act show / score 四条
   命令；证据束字段（UntestedAreas/RemainingRisks）落盘但无人读面。
4. **假绿无机械对冲**：断言弱化/空断言测试无论多少覆盖率都查不出；
   edge/bad case 靠实现者自觉。

## 设计原则

1. **考卷层级制**：oracle 来源分级盖章（一次盖章不可改写），由强到弱
   `spec-extract` ＞ `start` ＞ `conventions` ＞ `manual`。实现者不能无标记地
   自写考卷。
2. **验证深度跟业务风险走**：spec 声明 criticality 驱动 mutation/fuzz/heldout
   投放（测试计划是业务风险地图，不是覆盖率地图）。
3. **高可信 = 证据 + 未验证面显式披露**：不是全绿。
4. **失败经济学内建**：发现 bug 提升证据强度、隐藏重罚、绿不加分。

## 分层与机制

| 层 | 机制 | 落点 |
|---|---|---|
| L0 业务正确性定义 | `forge task chain-init`（spec 档 human，accept 围栏人工签字）；req-hygiene | internal/clitask/task_chaininit.go、internal/artifactchain |
| L1 考卷前置与升硬 | `AcceptanceCriterion.Source` 四级；complete 登记硬前置；`forge task accept` 补登（manual）；verify-acceptance conventions 兜底（接线在 internal/clitask/task_gate.go） | internal/taskpipeline/acceptance_register.go、internal/clitask/task_gate.go |
| L2 机器出题 | mutation 抽样（AST 变异 + go test 杀灭）；fuzz 执行器（Go 原生 Fuzz 预算化）；edge case 五维机械枚举 | 阶段二 |
| L3 独立出题 | heldout 池（既有 per-task `task start --heldout` 保留集的项目级池化演进：沉淀+抽取） | 阶段三 |
| L4 修复回路 | finding resolve 回归测试前置；test-diff 隔离审查 | 阶段二/三 |
| L5 交付验收面 | `forge task report`（考卷逐条+证据+未验证面+残留风险+逃生舱库存+复现命令） | internal/clitask/task_report.go |
| L6 元验证 | seeded-bug 红队演练（对抗 agent 交付埋雷代码 → 验证链拦截率） | 阶段三 |

## 阶段

- **阶段一（让验收变便宜）**：L1 全部 + L5 + L0 接线入口。✅ 已落地（2026-09-19，
  Score 92A）。
- **阶段二（假绿工业化治理）**：L2 三件套（mutation 抽样 / fuzz 执行器 / edge
  清单）+ L4 回归前置（forge task regression + resolve 硬消费）。✅ 已落地。
- **阶段三（独立性与自校准）**：L3 heldout 池 + L6 红队 + test-diff 隔离。

## 命令面预算裁决（compat-commitments §五）

本设计批次（一个 minor）计划净增命令：阶段一 +3（`forge task accept`、
`forge task report`、`forge task chain-init`）、阶段二 +3（`forge task
mutation`、`forge task fuzz`、`forge task edgecheck`）、阶段三 +2（heldout 池
的命令入口、`forge eval redteam`）——合计 +8，超出"单 minor 净增 ≤2"软预算。

裁决理由：① 全部为 `added`（非破坏）——不触碰任何既有命令面（给既有命令加
flag 判 changed/破坏性，故一律新命令）；② 八个命令同属一个能力（正确性传递
链），拆 minor 拆的是一个可评审整体；③ 用户已对整批方案拍板（本目标即其
落地）。**援引边界**：本裁决不可作为无关命令堆积的先例——与 oracle pipeline
无整体设计关系的命令新增，仍按 ≤2/minor 预算。

## 已知边界

- manual 层标准**如实披露**但不降证据强度：forge 实跑的执行真实性不因层级
  变化，层级定价的是**选题独立性**（披露面在 task report，判断留给验收方）。
- conventions 兜底只在 verify-acceptance 落盘时机登记（agent 主动跑才兜底；
  不跑则 complete 登记门拦截并给出口）。
- spec-extract 按**通道**盖章，不验证时序（审查 P2-4）：advisory 链上实现者
  写完代码后 --set spec + --extract 仍得 spec-extract 标签——"考卷先于答案"
  的时序承诺只在 human 档（审批签内容哈希+完成定稿）下被强约束。缓解：report
  同页披露 spec 审批态与 manual 层占比，验收方可对账。
- 外来导入的验收标准一律归一 manual 层（审查 P1-1 修复）：tier 声明与结果
  字段同属不可信输入，强层级只能由本机登记通道重新挣得。
- **mutation 并发无锁**（阶段二审查 P2-4）：共享 checkout 上两个 mutation
  实例交错变异可能互相写回对方的变异体——多任务并发用 worktree 隔离形态，
  或避免同时跑两个实例；进程被杀的备份残留由下次起跑检测并拒绝执行。
- **mutation 无总预算**（P2-5）：默认 6 变异体 × 单体 3m 串行最长 ~18m
  （FORGE_MUTATION_TIMEOUT 只调单体）；单体只跑所在包测试，跨包集成测试
  杀灭不了同包外的行为依赖 → 可能假存活（advisory 已知边界，中环工具定位）。
- fuzz 失败分型：退出码 1 = 真 crasher（语料在 testdata/）；构建失败/预算
  中断不谎称有语料（阶段二审查 P2-3 修复）。
