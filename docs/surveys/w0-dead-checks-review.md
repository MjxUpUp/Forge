# W0.1 死检查报告人工归因复核（2026-09-13）

> 复核对象：`forge eval dead-checks` 首跑（观察窗 90 天，最小观察 30）标出的
> 14 个 dead-candidate。复核问题（W0 宪法）：**台账是否覆盖该检查的真实触发
> 面**——零拦截是「确实没拦过东西」还是「拦截本来就不是它的职责」。

## 复核结论（一句话）

14 个 dead-candidate **全部否决删除**，但否决原因揭示了 W0.1 v1 的判定缺陷：
按归因表（下表为准）分布为 **9 个 advisory 类**（设计上永不阻断，「拦截 0」
对它们无意义，健康度口径应是 warn/送达/命中）、**2 个管线事件标记**
（review-pass、task-started——不是检查，是状态机留痕，不该进「死检查」分类）、
**3 个 gate 类**（拦截 0 = 通过率好 ≠ 死，gate 的死法是「该拦的没拦」，要靠
canary/warn 信号判定）。真正的减法候选 = 0，但「拦截口径对 advisory/gate 类
失配」本身登记为 W0.1 v2 修正项（已落地，见产出动作 #1）。

## 逐检查归因表

| # | 检查 | 观察 | 判定 | 归因（台账语义 vs 真实触发面） | 处置 |
|---|---|---|---|---|---|
| 1 | skill-trigger | 497 | **非死** | 触发通道（advisory 注入）；健康度在 skillmetrics 触发→采纳漏斗，送达面见附录 | 归为 advisory 类（健康度=skillmetrics 漏斗）；v2 分类修正 |
| 2 | task-verify | 325（**skip 144**） | **非死（管线核心）** | verify 门 advisory 默认（protocol 可升级）。144 skip = 未登记验收/无代码改动场景的合法跳过——skip 144/总执行 469 ≈ 31%，是覆盖率问题不是死信号。拦截 0 = 升级路径未启用 | gate 类口径修正；观察 skip 率 |
| 3 | test-capability-scan | 258 | **非死** | R 目录能力扫描，advisory。零拦截 = 仓库当前无该族违规，非失明（同族 cheat-scan 264 次拦 43 证明扫描面在工作） | advisory 类修正 |
| 4 | auto-compile | 248 | **非死** | 编译自检提醒，advisory（W0 评审已知阻断为负价值）。pass 248 = 提醒注入正常 | advisory 类修正 |
| 5 | task-complete | 96 | **非死（gate）** | 完成门 advisory 默认。96/96 通过 = 完成门槛健康，非死 | gate 类口径修正 |
| 6 | plan-first | 89 | **非死** | 规划前置提醒，advisory | advisory 类修正 |
| 7 | review-pass | 87（warn 7） | **排除（管线事件）** | `forge review pass` 的状态机留痕，不是检查——「拦截」对它无定义 | v2：pipeline-marker 类排除出报告 |
| 8 | test-nudge | 84 | **非死** | 事中测试提醒，advisory（#4-E 事实性指引） | advisory 类修正 |
| 9 | branch-unmerged | 79 | **非死** | 分支未合并提示（advisory；完成≠交付的可见性） | advisory 类修正 |
| 10 | docs-consistency-gate | 69 | **非死（gate）** | doc gate advisory 默认 | gate 类口径修正 |
| 11 | conventions-inject | 65 | **非死** | 规范摘要注入，advisory | advisory 类修正 |
| 12 | next-hint | 58 | **非死** | 下一步提示注入，advisory | advisory 类修正 |
| 13 | task-started | 53 | **排除（管线事件）** | 任务启动状态机留痕，非检查 | v2：排除出报告 |
| 14 | doc-lint | 43 | **非死** | 文档 L1 硬规则扫描，advisory（L1 0 硬失败 = 文档质量好） | advisory 类修正 |

## 产出动作

1. **W0.1 v2 修正项——已落地**（338ddb0，2026-09-13；维护者）：`forge eval
   dead-checks` 按 check 类别分四档判定——blocking（现行「拦截」口径，未列名
   新检查保守默认）、advisory（改用 warn 率/送达率/命中，永不判死）、gate
   （拦截 0 = gate-healthy）、pipeline-marker（整体排除出报告）。
2. **两个专项信号值得维护者人工跟进**（非删除）：task-verify 的 skip 率
   （144/469 ≈ 31%，验收登记渗透不足）与 skill-trigger 的送达面（附录：kimi
   迁移窗口 108 条未送达）——均已属既有度量面，不另立任务；下次 eval
   dead-checks 例行跑时复核两信号。
3. **零删除**：本轮无可执行减法；真减法等 canary 注入数据与 v2 口径积累。
   v2 口径已落地，「14 个候选」表述一并退役，此后引用走新口径。

## 方法注记

复核未跑新实验，依据三源交叉：(a) 各 check 的 Record 调用点（代码考古）；
(b) ForgeHookSpec 挂载面与 advisory/硬阻断分层（settings.go / 卡片 hooks 节）；
(c) 台账行为分布（pass/warn/skip 构成）。skip 单列不计观察（表内 observed
未含 skip——task-verify 的 144 skip 单独列示，占比分母 = observed + skip）。

## 附：skill-trigger 送达面聚合（2026-09-14 自本仓台账重算）

本仓台账（窗口 2026-08-17 → 09-14，n=504，原始计数未去重、单项目范围）按
通道 × delivered 聚合：

| 通道 | delivered | n | 窗口 |
|---|---|---|---|
| claude/additionalContext | ✓ | 245 | 08-17~09-14（活跃） |
| dsh/enter-messages | ✓ | 93 | 08-21（单日使用窗） |
| kimi/stdout-UserPromptSubmit | ✓ | 27 | 08-17~08-27 |
| dsh/agent.inject | ✓ | 23 | 08-20~08-21 |
| kimi/no-channel | ✗ | 88 | 08-20~08-25 |
| kimi/advisory-queue | ✗ | 20 | 08-26（单日） |
| (none)｜stop-round-cap 抑制 | ✗ | 8 | 08-21~08-28 |

送达 388/504 ≈ 77%。未送达 116 条：kimi/no-channel 88 条（08-20~08-25，
之后无复发）与 kimi/advisory-queue 20 条（全部 08-26；delivered 为入队时点
标记，是否补投本台账不可见）集中于 kimi 通道接线/迁移窗口；(none) 8 条为
stop-round-cap 主动抑制注入，是设计行为非通道故障。08-28 后窗口内无未送达
记录，活跃通道仅 claude/additionalContext（持续 delivered=true）。kimi 通道
排查跟进见产出动作 #2。
