# change：仓库级回归基线

## 改动

1. 新增 `internal/evalsuite`：`Load` 解析 `spec/changes/archive/*/acceptance.md` 的 bash 围栏（跳过 `# suite: skip`、支持 `--only`/`--max`），`Run` 逐条 exec 并计时（与 acceptance 相同的切分语义，不过 shell），`Save` 写 `.forge/evals/<ts>.json` + `index.jsonl`，`Trend` 读最近 N 次。
2. 新增 `forge eval suite [--only|--max|--json|--record]` 与 `forge eval trend [--last|--json]`（挂在既有 `forge eval` 下）。
3. `docs/reference/cli.md` 的 eval 段补两行；README 的「日常循环」补一条「隔一段时间跑一次回归基线」。

## 影响

- 只加命令与包，不改门禁语义、不改 acceptance 语法；失败以非零退出但**不阻断**任何 forge 流程。
- 落盘位置 `.forge/evals/`（本地工作区，不入 git）。

## 验证

- `internal/evalsuite/evalsuite_test.go`：围栏收集/跳过/过滤/上限、Run 的通过/失败与计时、Save+Trend 往返、命令切分的引号与未闭合报错。
- `internal/cli/evalsuite_test.go`：在临时仓库造两个归档 change 的 acceptance，跑 `eval suite`（断言输出与退出码）与 `--record` 后 `eval trend` 能读出记录。
