# 1.88.1：交付单披露已钉 change；MSYS 路径告警：目标契约

change: `fix-1881`

> 这份契约是任务的验收前提：五段都要填，删掉每段里的 `forge:fill` 注释标记才算写完。
> 启动任务后改它 = 改判据：`task gate task-implement` 会 BLOCKED；还原 goal.md，
> 或 `forge task abort` 后重新 `task start --change` 钉新哈希。

## Objective

`forge task report` 在任务钉了 change 时披露 `pinned_change` 与「以 change 产物为准」；Windows 下 `FORGE_DATA_HOME` 传入 MSYS 风格路径时提示一次「按当前盘符解析」，不改正路径。

## Why

1.88.0 的 dogfood 发现两处诚实性缺口：交付单对钉了 change 的任务仍写「无 spec 产物」，验收方看不到 change id；Windows 用户把 `FORGE_DATA_HOME` 设成 `/c/...` 时，Go 会按当前盘符解析到别处，落盘位置与预期不符却无提示。

## Constraints

- 只新增 JSON 字段；除「钉了 change 时产物链披露取代 spec 审批态那一行」外，既有渲染行为不变；
- `pinned_change` 只在 `GoalContract.Change != ""` 时出现，其它情况沿用原文案；
- MSYS 提示只对 `runtime.GOOS == "windows"` 生效，只提示一次，**不修改** `GlobalHome` 返回值；
- 不改 `FORGE_DATA_HOME` 的既有优先级与语义。

## Out of Scope

- 不做路径自动转换（`/c/...` → `C:\...`）；
- 不改 change 产物链或 phase 的判定；
- 不改 `task report` 的其它行。

## Check

判据见 acceptance：#1 build / #2 报告披露测试 / #3 路径告警测试 / #4 gofmt / #5 docs lint。