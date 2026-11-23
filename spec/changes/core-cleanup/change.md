# core-cleanup — 改动 / 影响 / 验证

## 改动

1. **共享判据工具** `internal/criteria`（新包）：`SelectorMatchedNothing(run, output string) bool` —— `-run` 存在且输出含 `no tests to run` 视为选择器空匹配。task 侧 `taskpipeline.RunSelectorMatchedNothing` 改为薄包装，change 侧新用。
2. **change verify 判负** `internal/change/verify.go`：`Check` 增 `SelectorEmpty`，`runCheck` 置位，`runPlan` 判 `Exit != 0 || SelectorEmpty`；`internal/cli/change.go` 两处输出加分支。
3. **testcoverage 死代码**：删 `CheckNameTestCoverage`、`coverageEscapeActive`、`CheckTestCoverage`、`checkTestCoverageChanged` 与两个阈值常量（510→427 行），删对应 golden 测试；保留 11 个共享工具（taskChangedFiles 等仍被 acceptance/cheatscan/evidence 用）。
4. **测试账本条件跳过** `internal/testledger`：`Decl.SkipGuarded` / `Entry.GuardedSkip`；`skipInfo` 改成显式 AST walker，If/For/Switch 内的 Skip 记为条件跳过，不进 skipped 违规；`forge test ledger` 单列「条件跳过 N」。
5. **死提示清零**：act nudge 不再教 `forge act retro-done/show`；lease/depref/task_gate/task_abort/task_misc/task_port/task_scope/conventions/workspace/bundle_sig/review/status 里的 `forge <死命令>` 改成活命令或写明「已随 core-only 移除」；新增守卫 `TestNoDeadCommandHints`（go/ast 只扫 fmt 系调用的字符串实参，说明性文案豁免）。
6. **死文件与死包**：`git rm` workspace/doctor.go、registry/audit.go（+测试）、`internal/cliskills` 整包、`internal/{skillseval,skillsdecisions,skilltrigger,skillscanonical,skillsqa,skillmetrics,skillintegrate,skillsdist,skillsfm}`、`skills/embed.go`、`skills-forge/embed.go`；`root.go` 用本地 `errHardExit` 替代 cliskills.ErrHardExit。

## 影响

- 命令面不变（87 命令 / 210 flag）：`forge skills` 在 core-only 时已不在树上，本 change 只是把实现一起删掉。
- 文档：无需改 README/cli.md（drift 0 已验证）；`docs/design/test-ledger.md` 需补条件跳过口径。
- 测试：新增 `internal/criteria`（被 task/change 两侧共用）、`internal/testledger/guarded_test.go`、`internal/change/verify_selector_test.go`、`internal/cli/dead_hint_guard_test.go`；删除 skills 脚手架的测试随包消失。
- 风险：`internal/change` 与 `internal/taskpipeline` 共用判据口径，改一处必须两侧测试都跑；skills 删除后 `skills-decision` 门禁仍按路径工作（无 embed 依赖）。

## 验证

- `go build ./...`、`go test ./... -count=1` 全绿。
- 定向：`internal/criteria`、`internal/testledger` 的单测；`internal/change` 的选择器空匹配两例（红/绿两侧）；`internal/cli` 的死命令守卫。
- 端到端 smoke（仓外临时项目）：init → change → task → 门禁 → verify 封印；反证用 `-run` 不存在的测试必须不封印。
