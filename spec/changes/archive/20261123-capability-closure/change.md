# 改动

## 改什么

- 新增 `internal/capability`：`Surface` / `paths`（前缀也算已知）/ `ScanText` / `ScanFiles` / `LiveDocs` / `hasDeletedMarker`，把「文档提到的 forge 命令」与命令树对齐。
- 新增 `internal/change/capability_bridge.go`：把 `agentproto.Command` 桥成 `change.SurfaceCommand`，产出 `change.Capability` 并挂到 `Summary.Capability`。
- `internal/change/verify.go`：`VerifyOptions` 新增 `Surface`，verify 结束时填 `sum.Capability`。
- `internal/cli/change.go`：`change verify` 打印能力面与漂移明细。
- `internal/cli/docs_closure.go`：新命令 `forge docs closure`（`--json`、`--strict`）。
- 测试：`internal/capability/capability_test.go`、`internal/change/verify_capability_test.go`。

## 影响

- 文档：`docs/reference/cli.md` 补 `forge docs closure`；`compat.snapshot.json` 重钉（新增命令面）。
- 代码：verify 的输出与摘要多一段；不改变任何门禁判定与退出码（`change verify` 的通过与否仍只看判据）。
- 已知风险：扫描是行级启发式——同一行里出现多个命令只认第一个 `forge ` 后的 token；被 ``` 围起来的示例文字同样会被扫到（这是「文档引用」的本义）。协议块里被误判的短词（`AGENTS.md` 中 `state` 一处）在本次按「非命令 token 不算引用」收敛，未做语义级判断。

## 验证

- 单测三条（见 acceptance #1–#3）。
- `forge docs closure` 实跑给出计数（#4）。
- dogfooding：本仓 `forge docs closure` 报出的漂移与人工 grep 结果一致。
