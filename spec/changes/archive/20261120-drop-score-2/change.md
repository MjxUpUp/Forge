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

## 实施顺序（2026-11-19 实测补充）

一次性删除会牵出比预估更宽的引用面——`go build` 立刻报 `internal/act/conclusion.go` 也 import `scoringtypes`；仓库走 `-mod=vendor`，删除包后必须把所有 import 同时清掉，否则整棵树下不编译。已实测：先删 `internal/scoring*` 再逐个补import 的方式会让树红很久，不可取。正确顺序是：

1. 先把「读分数」的消费方改成事实清单（`internal/act/conclusion.go`、`internal/dashboard/pulse.go`、`internal/evalkit/*`、`internal/protocol`、`internal/tasktypes`、`internal/taskpipeline/{merge_converge,seed_schema,recurrent,toolusage 相关}`），每步 `go build ./...` 保持绿。
2. 再把 `BuildEvaluateInput/ScoreTask` 与 `internal/scoring`、`internal/scoringtypes` 一次性删除，并同步删 `internal/scoring/testdata/golden*`、`evalkit` 里读分数的 golden 断言。
3. 最后跑 `git grep -l "internal/scoring" -- '*.go'` 应为空、`taskpipeline/dashboard/clitask` 三包测试绿。

2026-11-19 尝试直接做第 2 步，`go build` 报 `internal/act/conclusion.go:25` 仍 import scoringtypes；已回滚到 `dbaed32e`（树绿），本 change 保持 ready 待按上述顺序做。
