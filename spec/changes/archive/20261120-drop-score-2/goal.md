# Goal：删评分第二批

## Objective

把最后一批「合成分」从代码里删掉：`forge act health`（dashboard pulse）不再输出 `overall`/`grade`，`forge task golden` 不再算总分，merge 收敛改用事实清单比较，最终删除 `internal/scoring` 与 `internal/scoringtypes` 的评分数学。

## Out of Scope

- 不改 complete/report/list（第一批已完成，输出事实清单）。
- 不新增替代指标（要量化就数事实：判据数、红先绿、评审轮次、逃生次数）。

## Check

- `go build ./...` 通过且 `internal/scoring`、`internal/scoringtypes` 目录不存在。
- `forge act health --json` 的输出不含 `overall`/`grade` 键。
- merge 收敛测试仍绿（判据从分数比较换成事实清单比较）。
