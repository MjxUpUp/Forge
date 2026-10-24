# review 门禁会话作用域修复（P0）

- 状态：**已实施**（步骤 1–6 全部落地并通过验证）
- 验证证据：`forge task verify-acceptance` 实跑
  `go test -v ./internal/review ./internal/attribution ./internal/taskpipeline -count=1` = ok
  （checklog 记 deterministic acceptance）；另手工跑 `go test ./internal/review ./internal/attribution ./internal/cli -count=1`
  = ok（27.1s / 3.4s / 431.9s）。
- 变异抽样（`forge task mutation --sample 6`，覆盖本次改动的三个包）：首轮 1/4 杀灭；补 `TestEvent_Confidence`、`TestEvaluateFor_MaxRoundsAdvisory`、`TestEvaluateFor_MaxRoundsWithWholeTreeLegacyStamp` 后 **3/4（75%）**。唯一存活 `stamp.go:448`（`rev-parse` 校验的 `||`）是等价变异：`gitOut` 在 `cmd.Output()` 出错时直接返回 `"", err`，构造上「失败⇒空 stdout」，两条件不可区分。撞轮析取那条不是捷径——把 `stamp.DiffHash == wholeHash` 手工改成 `!=` 后新夹具确实转红，才记为「已覆盖」；夹具另带前提守卫（排除 b.go 后 scope≠whole），防归属过滤静默失效时杀灭力无声消失。`./internal/cli` 的变异体被跳过：工具输出称其基线红（未归因到本次改动，且不跑测试无法复现），原始输出见 `~/.forge/projects/8db5a0d1b70f/checklog.jsonl` 的 mutation-sampling 行。
- 提交：`fix(review): Stop 门禁按会话作用域判定，不再因他会话的未审改动阻断`
- 分支：feat/review-gate-session-scope（基线 main@a52273c6 / v1.84.1 后）
- 关联设计：docs/design/multi-task-concurrency.md §6（L3 变更归属）、§8（退化矩阵）
- 事故：2026-10-24 15:46，session-ae124d35（纯 DSH 调研，2 个 turn）在收尾时被 Stop hook 拦下，
  其会话 753/1232 事件（61%）、102 次工具调用、5 个子 agent、23 分钟被用于处理另一个会话
  session-58483295 的未提交变更，并直接改写了其中 5 个文件 9 处。
  取证位置（可复核）：`~/.dsh/sessions/--E-Forge--/<sid>/session.v4.jsonl.zstd`（zstd + JSONL）、
  `~/.forge/projects/<project>/markers/forge-block-dedup-session-<sid>-review-stop`、
  `~/.forge/projects/<project>/attribution/<wtid>.jsonl`。

## 一、根因（四条，均可定位到行）

1. **判定输入是整树，不是"我的变更"**
   `internal/cli/review.go:513` `review.Evaluate(root)` → `internal/review/stamp.go:415`
   `computeDiffHash` → `SourceChangesSince(root, "")`，即"这个工作区里哪些源码被改了"。
   哈希 `5ae8039bf444f22a` 与 B 在 12:03 被拦时完全相同——同一次工作区状态，两个无关会话。

2. **判定签名没有主体**
   `Evaluate(root)` 不接收会话 id，整条链路无法表达"这些变更属于谁"，尽管归属服务早已
   提供 `View{Changed, BySession, Orphans}`（`internal/attribution/attribution.go:238` `Reconcile`）。

3. **原有的并发兜底只覆盖"对方有任务"**
   `internal/cli/review.go:507` `HasActiveTaskFromOtherSession` 在有别的会话持任务时放行。
   事故时刻该条件为假：B 直到 15:50:46 才 `forge task start`，15:46 拦 A 时 B 尚无任务。
   → 兜底方向正确，作用域太窄（任务 ≠ 会话）。

4. **阻断走 user 信道，且只给一条解除路径**
   `gateGuidance`（`internal/cli/review.go:211`）作为 Stop 的 additionalContext 以 `user/message`
   进入会话（本次为 `forge-dsh-1792827960383-10`）。agent 无法把它与真实用户指令区分，
   而文案只提供"审查 → `forge review pass`"一条路径，于是没有归属的会话只能去做别人的审查。

## 二、P0 方案：把"谁在停"接进判定

核心一句话：**Stop 判定只对"能归因到当前会话"的源码变更阻断；能证明属于其他活跃会话的变更离开指纹。**

| 步骤 | 状态 | 文件 | 改动 |
|---|---|---|---|
| 1 | ✅ 已落地 | `internal/attribution/attribution.go` | `View` 增 `LastWriter`（path → 胜出事件）、`Touched`（每会话历史触碰并集）、`LastSeen`（每会话最近事件）；`Foreign(sid, window)` 返回「last-writer 是他人 + 该会话仍存活 + 本会话从未碰过」的路径；`PorcelainLines` 加 `-uall`（否则新目录折叠成 `?? pkg/`，包内文件全变无主） |
| 2 | ✅ 已落地 | `internal/review/stamp.go` | 新增 `EvaluateFor(root, sid)` 与 `scopeAndWholeFingerprint`（一次 git 通过同时产出整树/作用域两枚指纹）；`gitOut` 统一 `-c core.quotepath=off`（否则中文路径既排除不中、内容指纹也取不到 → fail-open） |
| 3 | ✅ 已落地 | 同上 | 决策语义扩展：源码变更非空但**全部**被排除 → `DecisionPassAdvisory`，reason 带"另有 N 个文件属于其他活跃会话，不阻断"；混合场景 reason 带"本会话 M 个 / 他会话 N 个"；`block_count` 只在真正需要审（included 非空）时累加 |
| 4 | ✅ 已落地 | 同上 | `MarkPassedFor(root, sid, note)`：`forge review pass` 的盖章范围与 `EvaluateFor` 的判定范围共用 `foreignForSession`；同时已审判定**接受整树戳或作用域戳**——`review pass` 作为独立 CLI 进程在 dsh 上拿不到会话 id（身份只注入 hook 子进程），盖的是整树戳，若不接受就会落入「盖章→再被拦→3/3 兜底」。整树审查是作用域审查的超集，两者都按内容寻址 |
| 5 | ✅ 已落地 | `internal/cli/review.go` | gate 改调 `review.EvaluateFor(root, taskpipeline.CurrentSessionID())`；`review pass` 非 task 分支改调 `MarkPassedFor`；`gateGuidance` 增加一行范围说明 |
| 6 | ✅ 已落地 | `internal/review/stamp.go` | 降级信号：`FORGE_ATTRIBUTION=0` 时 reason 追加"本次按整树判定"；非 git / 台账空/损坏时 `Foreign` 返回 nil ⇒ 整树（不额外标注，因为那是既有行为） |

窗口默认 30 分钟（`FORGE_REVIEW_FOREIGN_WINDOW_MIN` 可覆盖，**只接受正数**）。窗口量的是
**会话存活**（该 sid 最近任意一条台账事件），不是单个路径的新鲜度：邻居还在改别的文件就算
活着，它更早写过的文件继续被排除——否则长期并行的邻居会像事故那样反复触发本会话的门禁。
`0` 不表示"关闭过滤"而是回落默认——不限时会走向漏审方向，见 `internal/review/stamp.go`
的 `foreignWindow`。

### 为什么不选另外三条路

| 备选 | 否决理由 |
|---|---|
| 维持现状（整树 + 只在文案里劝"先确认是否本会话改动"） | 事故就是文案失效：A 已明确判断"不是我改的"仍被要求审查。文案不是契约。 |
| 加宽 `HasActiveTaskFromOtherSession`（对方有任务即放行） | 把"任务"当主体代理词，B 在 15:50 前无任务时仍误伤；且对方有任务≠对方改了这些文件。 |
| 只加 `forge review ack --foreign`（人工声明无关） | 把判定责任推给被误伤的会话，且 ack 需要 agent 自己判断归属；P0 之后"与我无关"不再阻断，ack 失去动机。 |

## 三、判据（先写夹具，再改实现）

1. **核心回归**：两个 sid，A 写 `a.go`、B 写 `b.go`，`EvaluateFor(root,"A")` 必为
   `DecisionNeedReview` 且指纹只含 `a.go`；对称地 B 只看 `b.go`。
   → `TestEvaluateFor_OwnChangeNeedsReviewAndExcludesForeign`
2. **事故复现**：只有 B 的变更，`EvaluateFor(root,"A")` → `DecisionPassAdvisory`，
   且 `block_count` 不增加（不能因别人的变更消耗 A 的 3 次预算）。
   → `TestEvaluateFor_ForeignOnlyDoesNotBlock`
3. **fail-safe**：台账解释不了的 orphan 路径必须计入指纹并阻断（不可静默丢弃）。
   → `TestEvaluateFor_OrphanStillBlocks`
4. **存活语义**：B 在窗口内**没有任何**台账事件（会话已静默）→ 其旧写入落回无主 → A 阻断；
   反之 B 仍在改别的文件时，它更早写过的 b.go 继续被排除（存活是会话属性）。
   → `TestEvaluateFor_SilentSessionBlocks`、`TestEvaluateFor_AliveSessionOldWriteStillForeign`、
   `TestForeignWindow_EnvOverride`
5. **双条件**：本会话先改、别人后改同一文件（last-writer 是别人）→ 文件里仍有本会话未审的
   改动，**不得**排除（`View.Touched` 守卫；否则漏审）。
   → `TestEvaluateFor_OwnEarlierTouchKeepsPathInScope`
6. **向后兼容 / 盖章互通**：`Evaluate(root)`（sid=""）行为与改动前逐字节一致；且**整树戳**
   能满足作用域门禁（`review pass` 在无身份的 CLI 进程里盖整树戳）。
   → `TestEvaluateFor_NoSessionKeepsWholeTree`、`TestEvaluateFor_WholeTreeStampSatisfiesScopedGate`
   对称性：`TestEvaluateFor_SymmetricOwnership`（A 只看 a.go、B 只看 b.go）。
   中文名：`TestEvaluateFor_NonASCIIPathExcluded`（排除集必须命中，否则 quotepath 回归）。
7. **降级可见**：`FORGE_ATTRIBUTION=0` 时 reason 必须写明按整树判定。
   → `TestEvaluateFor_AttributionDisabledSaysWholeTree`
8. **接线**：事故真实路径（CLI gate）在共享检出里对他人变更输出 ADVISORY 而非 FAIL。
   → `internal/cli/review_gate_scope_test.go` `TestReviewGate_ForeignChangeIsAdvisory`
9. **验收命令**：`go test ./internal/review ./internal/attribution ./internal/taskpipeline ./internal/cli -count=1`

## 四、明确不做

- 不改 `hooks/review-stop.sh` 与 exit code 契约（阻断仍是 exit 2）。
- 不加 `forge review ack --foreign` 子命令：P0 之后"与我无关"不再阻断，ack 失去动机；
  若 orphan 误伤真出现，按线上数据再决定。
- 不动 task 模式路径（`ReviewPassed` 仍是 task-complete 的硬前置）。

## 五、后续阶段（另开任务，不在本分支）

- **P1 错树守护**：`task start --worktree` 之后会话仍可能在主检出工作（`active-task-ref`
  按 session 存于共享 DataDir，主检出同样解析得到活跃任务，task-guard 因此不拦）。
  方案：解析结果带 off-tree 锚点 → task-guard 对"错树 + 源码编辑"fail-closed → hook 每 turn
  注入 `FORGE_TASK_WORKTREE`。
- **P2**：身份诚实化（无宿主身份时写 `unbound` 而非采纳 last-session 指针）；L4 隔离默认策略。

## 六、风险与回滚

- **误判风险**：把本会话的变更误判为他人 → 漏审。缓解：窗口内 + last-writer 判定 + Touched 守卫
  + orphan 恒计入。
- **性能**：Stop 时多读一次台账（本机 9477 行 / 1.42MB，解析 ≈9ms）。实测（本机 warm）：
  `forge review gate` 全进程 115–133 ms，改动前未单独测基线，故**增量未实测**（git spawn
  本身约 100ms/次，是主要成本）；计划目标 ≤50 ms 增量尚未验证。
- **stamp 兼容**：升级前写下的整树 hash stamp 与作用域 hash 不同 → 升级后每个有未提交源码
  变更的工作区会多审一次（安全方向，不误放行）。这是有意的取舍，不是回归。
- **已知边界（M3，未修，P1 处理）**：dsh 插件把子 agent 的写入记成**子会话** sid，父会话的
  `Foreign` 只看 sid 不等即判外来 → 父会话可能把「自己派出去写下的代码」当成他人的活而
  advisory 放行（漏审方向）。持久修法需要事件带 parent sid（`plugins/forge-dsh/lib/tools.js`
  的 `buildToolPayload` 取根会话 id，`Foreign` 把后代视为自己）。P0 期间以有界风险接受：
  子 agent 由本会话派生、代码通常在同一轮被父会话看见，且 `task-complete` 的
  `TaskFingerprint` 仍会在 task 模式覆盖这段。
- **quotepath（已修）**：`gitOut` 统一 `-c core.quotepath=off`，中文路径与台账同编码，
  排除集与内容指纹都恢复正确（夹具 `TestEvaluateFor_NonASCIIPathExcluded`）。
- **回滚**：`FORGE_ATTRIBUTION=0` 一键回退到整树行为，零中间态（且会在 reason 里承认这件事）。
