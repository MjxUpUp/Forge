# 目标契约

## Objective

把「仓库里有哪些测试、它们上一次真的跑过吗、哪些被跳过了」从人的记忆挪到一条可跑的命令里，并给改动过的代码补一道增量变异检查。

- `forge test ledger`：以 `go test -json` 的执行记录为真相源，把仓库里的测试分成四态——**live**（有声明且记录新鲜）、**orphaned**（有声明、无执行记录）、**skipped**（源码里有 `t.Skip*`）、**phantom**（记录里有、源码里已无）。无记录一律判 orphaned。
- `forge test record`：读 `go test -json` 流（stdin 或文件），把每条 pass/fail/skip 结果追加到 `.forge/test-exec.jsonl`。
- Skip 规则：`t.Skip*` 必须带非空理由；带理由但不在 `spec/test-skips.yaml` 审批清单里的，判「未审批」。`--strict` 对「缺理由」「未审批」「phantom」判失败。
- `forge impact` 在既有影响面上补一段：改动的测试目录各自 live / orphaned / skipped 计数（含未审批数）。
- `forge test probe --since <rev>`：只取该 rev 之后 diff 里的 Go 源文件，复用既有变异算子抽样实跑，报告 killed / survived（增量 mutation：不跑全仓）。

## Out of Scope

- 不把账本接进 `task complete` / `change verify` 门禁（门禁接线属 D 批的能力面闭合表）。
- 不做 CI 上传/下载与跨机器合并（只定义本地记录文件格式；合并语义留待有人真的需要）。
- 不改全仓 mutation 抽样引擎的算子表、抽样策略与判定口径，只加一个「按 git diff 取文件」的入口。
- 不新增 `test`、`probe` 之外的顶层命令；不加 hook；不改 `internal/change/judge.go`。

## Check

- `forge test ledger` 在本仓打印四态计数，且对 `t.Skip` 无理由的测试点名。
- `forge test ledger --strict` 在存在未审批 Skip 时非 0 退出。
- `forge impact` 的文本输出包含测试账本段；四态计数与 `test ledger` 同源。
- `forge test probe --since HEAD~1 --sample 1` 只对 diff 命中的文件做变异，不扫描全仓。
