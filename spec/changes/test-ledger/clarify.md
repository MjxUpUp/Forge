# 澄清：边界与非目标

change: `test-ledger`

## 边界（做什么 / 不做什么）

边界：新增 forge test 命令组（ledger/record/probe）+ impact 增补测试账本段；真相源 = go test -json 记录，落 .forge/test-exec.jsonl；Skip 审批清单 = spec/test-skips.yaml（现为空，59 条 Skip 全部标未审批，等真人逐条批）；probe 只对 git diff 命中的 Go 源文件做增量变异。非目标：不把账本接进门禁（留 D 批）、不做 CI 上传/合并、不改变异算子与判定口径、不加 test/probe 之外顶层命令。

## 非目标

- 不把账本接进 `task complete` / `change verify` 门禁（门禁接线属 D 批的能力面闭合表）。
- 不做 CI 上传/下载与跨机器合并（只定义本地记录文件格式；合并语义留待有人真的需要）。
- 不改全仓 mutation 抽样引擎的算子表、抽样策略与判定口径，只加一个「按 git diff 取文件」的入口。
- 不新增 `test`、`probe` 之外的顶层命令；不加 hook；不改 `internal/change/judge.go`。

人确认：2026-11-23T12:44:33Z
