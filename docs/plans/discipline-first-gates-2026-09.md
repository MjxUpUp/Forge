# 纪律第一防线：门禁时间再分布（2026-09-17 立项）

> 背景：remin 项目（`~/.forge/projects/9fe95966d4ec`，2026-09-17 单会话 6 任务）的
> checklog 审计证实：全部 115 次门禁类触发中 83% 落在 task-start/verify/complete
> 锚点 ±60s 内；m0 编码 26 分钟门禁零反馈，verify 时 12+ 门禁 15 秒内倾泻，报
> "missing tests for 8 files" 后花 31 分钟补救性补测。结论：门禁系统只有两种时间
> 模式——工作期低语（可忽略）、终点一次性咆哮（为时已晚）。

## 原则（本 spec 的全部改动不得违反）

1. **纪律是结构，不是自觉**：守纪律必须是摩擦最小的路径。每次门禁拦截都要能倒推
   第一防线缺了什么。
2. **门禁永不停止计算，只降低打断成本**：任何"静默化"改动不得删掉 checklog 审计
   落盘——变薄的是打断预算，不是审计轨迹。
3. **调整优先于新增**：优先重定时/重分级已有资产（test-nudge、artifact-chain、
   plan-first、markers、节流文件）；只有度量（P1-A）与镜像入口（P2）允许新增。
4. **防 gaming**：任何"门禁更安静"的改动必须伴随可复核的度量（discovery rate），
   且质量遥测（assertion density、cheat-scan）不得恶化。

## 度量定义（P1-A 落地后即可回测）

| 指标 | 定义 | 数据源 | 基线 | 目标 |
|---|---|---|---|---|
| D1 discovery rate | 失败门禁条目中 `outcome=discovery` 占比（discovery/(discovery+confirmation)） | checklog `outcome` 字段 | 无字段（本 spec 前不可测） | 30 天窗口内单调下降 |
| D2 nudge 分档命中率 | test-nudge 跨档触发时点 task 的未配对文件数 vs 该任务 verify 时 missing 数 | checklog `unpaired_files` vs test-coverage detail | 无（nudge 数写入事件不数文件） | tier3 触发数 ≥ verify missing≥8 任务数（nudge 先于门禁看到同一事实） |
| D3 纪律债兑现率 | `outcome=confirmation` 的条目占比（agent 被告知过但未行动） | checklog `outcome` | 无 | 观察指标：升=信号有效但未被执行，降+discovery 降=双好 |
| 防伪护栏 | assertion density（scoring.CollectAssertionDensity）、cheat-scan 命中率不得恶化；test-nudge 触发总量不得超基线同 activity 水平的 1.5× | scoring / checklog | 2026-09 Forge 自举基线 | 护栏越线 → 对应改动 reject |

## 分期

- **P1（本文件落地范围）**：度量地基 + test-nudge 从"事件计数器"升级为"文件级跨档信号"。
- **P2**：`forge selfcheck pairing|scope|unused` 镜像命令（包 `ClassifyChangedPath`/
  `ScopeDrift` 纯函数，新 CLI 非新逻辑）+ verify 前无 selfcheck 记录时的时机训练提示。
- **P3**：artifact-chain 前移 task start（缺 spec/design 开工前说）；plan-first 归位
  implement gate（审计时机从 verify 前移）。
- **P4**：门禁输出分级（clean streak N → 一行绿；discovery → verbose + 信任重置）；
  BLOCKED 强制触发 skill-evolution 还债条目。
- **P5（视 D1 数据决定）**：RED 运行证据（实现前失败运行记录，挂 task-implement
  gate）；新建源码文件配对测试脚手架。

P2-P5 各自立项时补本格式 spec；本文只钉 P1 的设计与验收。

## P1-A checklog `outcome` 字段（confirmation / discovery）

**语义**（只盖在**失败**的 verify 期门禁条目上；通过/历史条目留空=未分类）：

- `discovery`：门禁是此事实对 agent 的第一披露点——task 内此前无已送达（Delivered）
  的同类第一防线信号。含义：第一防线缺位（无信号或信号死了）。
- `confirmation`：task 内此前存在已送达的同类信号（如 test-nudge），agent 被告知过
  但未行动，门禁确认了纪律债兑现。含义：第一防线在投放，纪律未被执行。

**改动点**：

1. `internal/checklog/types.go`：`type GateOutcome string` + 常量
   `OutcomeDiscovery` / `OutcomeConfirmation`；`Entry` 增 `Outcome GateOutcome
   json:"outcome,omitempty"`（位置随 Source/Level 一组；omitempty 兼容历史行）。
2. `internal/taskpipeline/executor_check_verify.go`：`checkVerifyTestCoverage` 在
   `!ok` 时查 `checklog.LoadForTask` 中是否存在 `CheckTestNudge` 且 `Delivered=true`
   的先于本条目的记录——有则 `confirmation`，无则 `discovery`。
   `checkVerifyScopeDrift` 失败时恒 `discovery`（今日无上游信号——如实记录第一
   防线缺口，为 P3 的 nudge 前移提供依据）。

验收命令见下方「P1 总验收」的 accept 围栏（verify-acceptance 实跑口径）。

## P1-B test-nudge：事件计数器 → 文件级跨档信号

**病灶**（remin 实证）：nudge 数的是 Write/Edit **事件**而非**未配对文件**——19 次触发
全部显示 "3 source writes"，agent 写一个测试文件即全量重置，8 个文件照旧无配对直达
verify。措辞无文件名、无档位、无后果，重复即噪声。

**新契约**：

1. **状态改文件集**：`testNudgeState` 持 `UnpairedFiles []string`（仓库相对路径，
   去重——同一文件反复 Edit 不膨胀）+ `FiredTier int`。源码写入（`ClassifyChangedPath`
   判 source）追加；测试写入按配对约定移除匹配源文件（新导出
   `taskpipeline.TestPairsSource(testRel, srcRel) bool`，覆盖 hasMatchingTest 的同目录
   直接配对约定：`_test.go/_test.rs/.test.ts/.spec.ts/.test.js/.spec.jsx/test_*.py/
   foo_test.py/FooTest.java/foo_spec.rb/foo_test.zig` 等）。
2. **跨档触发**：档位 = f(未配对文件数)，阈值 3/5/8（8 对齐 remin m0 verify 实报
   missing 数）。仅在 `tier > FiredTier` 时触发一次；配对使档位回落时 FiredTier
   随之下调（归零即完全重新武装）——不逐写刷屏，只在状态**跨档**时说话。
3. **措辞升级**（保持事实性、非祈使、自然语言引用 skill 的既有纪律）：
   - tier1（3）："N unpaired source files in this task: a.go, b.go, c.go …
     (task gate task-verify checks this pairing; whitelist: entry
     points/generated/pure types). Load the test-discipline skill …"
   - tier2（5）：同上 + 未配对文件名换新。
   - tier3（8）：追加门禁后果事实："at ≥2 untested source files with zero
     assertions task-verify BLOCKs the task"。
4. **Meta**：`unpaired_files`（数）、`tier`、`files`（逗号清单，≤8）。
   `Detail`：`test-nudge: N unpaired source files (tier T): a.go, b.go …`。
5. **不变式**：活跃任务门控（任务外静默不落状态文件）、永不 block、每档一次、
   Delivered 章按宿主通道如实盖。

验收命令见下方「P1 总验收」的 accept 围栏（verify-acceptance 实跑口径）。

## P1 总验收（verify-acceptance 实跑口径；裸命令 = 退出码 0 判定）

```accept
go test ./internal/checklog/ -run TestEntryOutcomeRoundTrip
go test ./internal/taskpipeline/ -run TestCheckVerifyTestCoverage
go test ./internal/taskpipeline/ -run TestNudgeDeliveredForTask
go test ./internal/hookdispatch/ -run TestRunTestNudgeHook
go test ./internal/taskpipeline/ -run TestTestPairsSource
go test ./...
go vet ./...
```

lint 口径：`golangci-lint run` 对本任务 diff **零新增**（存量 65 issues 为 main
既有债务，不在本任务范围——仓库纪律：lint 只修它标记当前 diff 的部分；比对证据
为 main worktree 同配额 65 issues 的对照组实测）。

## 回测流程

1. 合入后首个 30 天或 25 个完成任务（先到为准）用 checklog 全量算 D1-D3（一次性
   脚本即可，字段已结构化）。
2. D1 无下降 + D3 上升 → 说明信号送达但执行断裂，优先评估 P2（selfcheck 降低守纪
   摩擦）而非加信号。
3. 防伪护栏任一越线 → 按 skill-evolution reject 记决策并 scoped revert 对应改动。
