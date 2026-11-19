# Change：删评分第二批

## 改动

1. `internal/dashboard/pulse.go`：删 `Overall`/`Grade` 字段与赋值（`pulse.go:225-226,331-332,412-413`），文本摘要改「判据/评审/逃生」事实。
2. `internal/clitask/task_golden.go`：不再算总分，只输出 golden 用例的通过事实。
3. `internal/taskpipeline/scoring.go` 与其调用点（`merge_*.go`、`model_alias.go`、`recurrent.go`、`seed_schema.go`）：merge 收敛判据改为「事实清单一致（判据全过 + 评审无未闭合）」，删 `BuildEvaluateInput`/`Evaluate` 调用。
4. `internal/tasktypes/types.go`、`internal/protocol/types.go`：删 `Score` 兼容字段与 `scoringtypes` 依赖。
5. 删除 `internal/scoring/`、`internal/scoringtypes/`。

## 影响

- 破坏性：任何脚本若读 `act health --json` 的 `overall`/`grade` 会失效（2.x 内首次）。
- merge 的「分数收敛」语义消失，改为可复核的事实条件。

## 验证

- `go build ./...`、`go vet ./...`、`grep -r "internal/scoring"` 为空。
- `internal/taskpipeline` 全包测试（merge/converge）绿。
- `forge act health --json` 手工核对无分数键。
