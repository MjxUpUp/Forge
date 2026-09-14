# W0.1 死检查报告人工归因复核（2026-09-13）

> 复核对象：`forge eval dead-checks` 首跑（观察窗 90 天，最小观察 30）标出的
> 14 个 dead-candidate。复核问题（W0 宪法）：**台账是否覆盖该检查的真实触发
> 面**——零拦截是「确实没拦过东西」还是「拦截本来就不是它的职责」。

## 复核结论（一句话）

14 个 dead-candidate **全部否决删除**，但否决原因揭示了 W0.1 v1 的判定缺陷：
其中 8 个是 **advisory 类**（设计上永不阻断，「拦截 0」对它们无意义，健康度
口径应是 warn/送达/命中）；3 个是**管线事件标记**（不是检查，是状态机留痕，
根本不该进「死检查」分类）；3 个是 **gate 类**（拦截 0 = 通过率好 ≠ 死，
gate 的死法是「该拦的没拦」，要靠 canary/warn 信号判定）。真正的减法候选 =
0，但「拦截口径对 advisory/gate 类失配」本身登记为 W0.1 v2 修正项。

## 逐检查归因表

| # | 检查 | 观察 | 判定 | 归因（台账语义 vs 真实触发面） | 处置 |
|---|---|---|---|---|---|
| 1 | skill-trigger | 497 | **非死** | 触发通道（advisory 注入）。拦截不是它的职责；健康度在 skillmetrics 触发→采纳漏斗（`forge eval` 系已有），8 条 warn = 送达失败信号需单独看。台账 497 行即真实触发次数（逐次落账），覆盖完整 | 归为 advisory-funnel 类；v2 分类修正 |
| 2 | task-verify | 325（**skip 144**） | **非死（管线核心）** | verify 门 advisory 默认（protocol 可升级）。144 skip = 未登记验收/无代码改动场景的合法跳过——skip 占 44% 恰是覆盖率问题不是死信号。拦截 0 = 升级路径未启用 | gate 类口径修正；观察 skip 率 |
| 3 | test-capability-scan | 258 | **非死** | R 目录能力扫描，advisory。零拦截 = 仓库当前无该族违规，非失明（同族 cheat-scan 264 次拦 43 证明扫描面在工作） | advisory 类修正 |
| 4 | auto-compile | 248 | **非死** | 编译自检提醒，advisory（W0 评审已知阻断为负价值）。pass 248 = 提醒注入正常 | advisory 类修正 |
| 5 | task-complete | 96 | **非死（gate）** | 完成门 advisory 默认。96/96 通过 = 完成门槛健康，非死 | gate 类口径修正 |
| 6 | plan-first | 89 | **非死** | 规划前置提醒，advisory | advisory 类修正 |
| 7 | review-pass | 87（warn 7） | **排除（管线事件）** | `forge review pass` 的状态机留痕，不是检查——「拦截」对它无定义 | v2：pipeline-marker 类排除出报告 |
| 8 | test-nudge | 84 | **非死** | 事中测试提醒，advisory（#4-E 事实性指引） | advisory 类修正 |
| 9 | branch-unmerged | 79 | **非死** | 分支未合并提示（advisory；完成≠交付的可见性） | advisory 类修正 |
| 10 | docs-consistency-gate | 69 | **非死（gate）** | doc gate advisory 默认（本任务 doc-review 91/88 PASS 即其证据面） | gate 类口径修正 |
| 11 | conventions-inject | 65 | **非死** | 规范摘要注入，advisory | advisory 类修正 |
| 12 | next-hint | 58 | **非死** | 下一步提示注入，advisory | advisory 类修正 |
| 13 | task-started | 53 | **排除（管线事件）** | 任务启动状态机留痕，非检查 | v2：排除出报告 |
| 14 | doc-lint | 43 | **非死** | 文档 L1 硬规则扫描，advisory（L1 0 硬失败 = 文档质量好） | advisory 类修正 |

## 产出动作

1. **W0.1 v2 修正项（登记）**：`forge eval dead-checks` 按 check 类别分三档
   判定——blocking（现行「拦截」口径）、advisory（改用 warn 率/送达率/命中，
   「拦截 0」不再是 dead 证据）、pipeline-marker（整体排除出报告，归
   `forge trace` 状态机观测）。触发条件：下个触碰 internal/cli/eval 或
   internal/taskpipeline 评分面的批次。
2. **两个专项信号值得人工跟进**（非删除）：task-verify 的 44% skip 率（验收
   登记渗透不足）与 skill-trigger 的 8 条送达 warn（通道级排查）——均已属
   既有度量面，不另立任务。
3. **零删除**：本轮无可执行减法；真减法等 canary 注入数据与 v2 口径积累。
   「14 个候选」的表述在 v2 口径落地前不再引用。

## 方法注记

复核未跑新实验，依据三源交叉：(a) 各 check 的 Record 调用点（代码考古）；
(b) ForgeHookSpec 挂载面与 advisory/硬阻断分层（settings.go / 卡片 hooks 节）；
(c) 台账行为分布（pass/warn/skip 构成）。skip 单列不计观察（审查 m2 修正已
落地，故表内 observed 未含 skip——task-verify 的 144 skip 单独列示）。

## 附：skill-trigger 送达 warn 8 条归因（2026-09-13 排查）

台账按通道聚合：claude/additionalContext 184（正常）、codex/hookSpecificOutput 15（正常）、
kimi/advisory-queue 5 + kimi/stdout-UserPromptSubmit 1（正常）、(none) advisory 8 + (none) warn 2。
**8 条 warn 全部为 Channel 字段引入前的 legacy 无通道行**——非活通道故障，当前通道健康。
处置：无需修复；kimiStaleRidesHook 的迁移已把可见性修到 UserPromptSubmit（2026-08-15 修复）。
