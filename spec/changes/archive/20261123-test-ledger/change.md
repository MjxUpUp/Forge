# 改动

## 改什么

1. 新包 `internal/testledger`：
   - `ScanDecls(root)`：遍历仓库 `*_test.go`，用 `go/ast` 收集 `Test*` 函数、所在目录对应的 import path、是否含 `t.Skip*`、Skip 的参数是否为非空字符串字面量。
   - `ParseExecJSON(io.Reader)`：解析 `go test -json` 事件，保留 `Action` 为 pass/fail/skip 且带 `Test` 的记录（run/output 丢弃）。
   - `LoadRecords/AppendRecords`：`.forge/test-exec.jsonl` 的读写；损坏行跳过并计数，不吞掉整个文件。
   - `Build(decls, records, Options{MaxAge})`：四态分类与 `Violations()`（缺理由 / 未审批 / phantom）。
   - `LoadSkipApprovals(spec/test-skips.yaml)`：`{test: <import path>.<TestName>, reason, approved_by, at}` 清单。
2. 新包 `internal/mutprobe`（薄封装，复用 `taskpipeline` 的算子表与执行器）：`ChangedGoSources(root, since)`（`git diff --name-only`，只留非 `_test.go` 的 `.go`）与 `RunOn(root, files, sample)`。
3. CLI：
   - `forge test` 命令组（`ledger` / `record` / `probe`）。
   - `forge impact` 增补测试账本段落与 `--json` 字段 `test_state`。
4. 文档：`docs/reference/cli.md` 补三条命令；`docs/design/` 下补一页「测试账本的真相源与四态」。

## 影响

- `internal/impact` 依赖新包（单向往外），`internal/cli` 注册三处命令；`compat.snapshot.json` 因新增命令面必须重钉。
- 新增顶层命令组 `forge test`：与既有命令不冲突（`go test` 是 Go 工具链，不是 forge 子命令）。
- 测试：新包各配单测；`forge impact` 的文本渲染补一条断言；`TestCompatSnapshotMatchesGolden` 需要重钉快照。

## 验证

- `go test ./internal/testledger ./internal/impact ./internal/cli -count=1`
- `forge test ledger`（本仓实跑，观察四态计数与未审批点名）
- `forge test probe --since HEAD~1 --sample 1`（本仓实跑，确认只碰 diff 文件）
