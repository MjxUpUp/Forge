# 目标

## Objective

把三条「已有能力但默认可选」的门禁变成默认硬：

1. `go test -run <名字>` 一个测试都没匹配上（输出 `no tests to run`）却退出 0 时，判据判负——
   否则测试名拼错、测试被删、包改名都会让判据假绿封印。
2. 行为类任务（改动集含源码扩展名）开工时未登记 `--accept-red` 时，`forge task complete` 直接 BLOCKED，
   并给出补登记的恢复路径；显式豁免必须带理由留痕。
3. `forge review pass` 必须声明本次核对过多少条验收判据（`--criteria-reviewed`），
   缺省不再静默通过——「审过但没看判据」是当前最常见的评审空洞。

## Out of Scope

- 不实现 `--reaffirm`（判据逾期失效）与 `--criteria-by`（判据作者独立）——留到 B2 批。
- 不改 eval suite 的阻断范围，不改 change 侧判据语法判官（internal/change/judge.go）。
- 不新增顶层命令；不改两道门禁的名称与顺序。
- 不把任何检查做成后台 hook：仍然是命令跑才生效。

## Check

四条判据见 acceptance.md：两条红判据覆盖 B1/B2，一条覆盖 B3，一条全包测试。
先失败证明登记 `go test ./internal/taskpipeline -run TestRunSelectorNoMatchJudgedFail -count=1`。
