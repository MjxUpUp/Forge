# change：surface-hardening

## 改动

- `internal/cli/root.go`：新增 `straySubcommand(args []string) (parent, token string)`，在 `Execute()` 调 `rootCmd.Execute()` 之前预检——当 `rootCmd.Find` 解析到「有子命令但不可执行」的父命令，且剩余参数里有非 `-` 开头的位置参数时，向 stderr 打 `forge: unknown command "<token>" for "<parent>"` 并 exit 1。`task` 等已有 legacyArgs 报错的父命令行为不变。
- `internal/checklog/types.go`：删除 18 个无生产者常量——`CheckEvalMetricsIncomplete`、`CheckEvalGoldenRun`、`CheckEvalGoldenRotate`、`CheckEvalJudgeWeak`、`CheckEvalTrapsRun`、`CheckEvalRun`、`CheckEvalDecompose`、`CheckEvalResumeDrill`、`CheckEvalAuditForged`、`CheckEvalPreflight`、`CheckEvalGoldenTestQuery`、`CheckWedgeDrill`、`CheckProjectSync`、`CheckAttribution`、`CheckSyncVersionSkew`、`CheckDecisionWarrant`、`CheckDecisionEvidence`、`CheckTaskGuard`。
- `internal/checklog/escape.go`：`allCheckNames` roster 同步删这 18 条。
- `internal/checklog/evidence.go`：`verificationChecks` 删 `CheckTaskGuard` 条目。
- `internal/cli/status.go`：删除 `evalHealthLine` 与其调用（它只读 `forge eval` 命令族落下的观察行，命令族已在 core-only 删除）。
- `internal/checklog/types.go`：连带删除 4 个写方已消失的 `MetaKey*` 常量（`MetaKeyAttributionAttributed/Orphans/Rate`、`MetaKeySyncOp`）与 `meta_keys_test.go` 的对应行——attribution/metric.go、cli/project_sync.go 都已随 core-only 删除。
- 测试：新增 `internal/cli/subcommand_guard_test.go` 的 `TestUnknownSubcommandRejected`；新增 `internal/checklog/historical_check_guard_test.go` 的 `TestNoHistoricalCheckNames`（AST 扫 types.go + roster 断言）；删 `internal/checklog/eval_validity_roster_test.go`；`escape_test.go` 删 `TestWedgeDrillInRoster`；`evidence_test.go` 的 project-sync 用例改名与换名；`verify_test.go` 样例换成仍存在的检查名。

## 影响

- 命令面不变（86 条命令、209 个 flag）；compat checks 面从 40 条降到 22 条，需重钉 `compat.snapshot.json`。
- 脚本行为变化：过去把 `forge review zzz` 当成功的地方现在会拿到 exit 1——这正是修复目标，属有意的行为收紧。
- `forge status` 少一行「自评测」；`forge status --json` 不再有 eval 健康字段（原本只回放历史行）。
- 文档面：`docs/reference/cli.md` 由快照生成，无需手改；`forge docs closure --strict` 应保持 drift 0。

## 验证

`go build ./...`；两个新守卫测试先红后绿；`go test ./internal/checklog ./internal/cli ./internal/compat` 绿；`forge docs closure --strict` drift 0；另手工实测四条未知子命令的退出码与 stderr。
## 测试变更依据（回应 test-diff 提示）

- 新增 `internal/cli/subcommand_guard_test.go`、`internal/checklog/historical_check_guard_test.go`：为本次两条新行为建守卫，先红后绿（基线 2/5）。
- 删 `internal/checklog/eval_validity_roster_test.go`、`escape_test.go` 的 `TestWedgeDrillInRoster`：它们钉住的常量 `CheckEvalPreflight`/`CheckEvalGoldenTestQuery`/`CheckWedgeDrill` 本次删除，测试对象已不存在——不是改断言让它过，而是删掉对已删除对象的断言，改由 `TestNoHistoricalCheckNames` 反向钉住「这些名字不得回来」。
- `evidence_test.go`：`project-sync` 用例换为 `tool-failure`（`CheckProjectSync` 已删）；legacy 兜底样例 `CheckTaskGuard` 换成 `CheckName("acceptance")`——原期望是「白名单内 + Source 为空 ⇒ deterministic」，`task-complete` 在白名单但默认 source 是 agent-claim，会改变分桶，故改用默认 deterministic 的 acceptance；期望值 3 不变。
- `store_test.go`、`pushgate_test.go`、`verify_test.go`：只是样例检查名换名（`CheckTaskGuard`/`CheckEvalGoldenRun` → `CheckToolFailure`/`CheckReviewPass`），断言的 Level/分桶/签名语义不变。
- `evidence_guard_test.go` 阈值 50→40、`compat_test.go` roster 阈值 30→20：随 18 个常量删除下调基线，declared 实际 45、roster 实际 22；守卫逻辑（白名单条目必须在源里声明 / roster 双向对齐）未改。
- `escape_roster_test.go`、`meta_keys_test.go`：gofmt 空行与删除已删 MetaKey 的条目。
