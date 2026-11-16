# 提案：1.88.1 交付单披露已钉 change；MSYS 路径告警

## 为什么做

1.88.0 的真实 dogfood（dogfood-1872，工作目录 `C:\tmp\dogfood-1872`）报告里记录了两处交付信息缺口：任务交付单不写它钉了哪个 change；Windows 下 `FORGE_DATA_HOME` 写成 MSYS 形式时落盘位置与用户预期不符且没有任何提示。

1. **交付单看不到 change**：任务用 `task start --change <id>` 钉了 goal 后，`task report` 仍打印「产物链……无 spec 产物」——验收方既不知道这条任务属于哪个 change，也无法据此决定是否抽查 change 产物。
2. **Windows 上 FORGE_DATA_HOME 落盘位置与预期不符**：MSYS/Git Bash 用户习惯写 `/c/tmp/forge-data`，Windows 的 Go 会把它当相对当前盘符的路径（如 `C:\c\tmp\forge-data`）。forge 不报错，用户以为数据在 `/c/...`。

## 怎么做

- `internal/clitask/task_report.go`：`taskReportJSON` 新增 `pinned_change`（omitempty）；`buildTaskReport` 从 `state.GoalContract.Change` 填充；渲染时优先打「产物链：已钉 change（<id>）——以 change 产物为准」。
- `internal/forgedata/key.go`：新增 `looksMSYSPath` 与 `warnMSYSDataHome`，`GlobalHome` 读 `FORGE_DATA_HOME` 时在 Windows 上提示一次（`sync.Once`），返回值不变。
- `docs/reference/cli.md`：report 行补 `pinned_change`；新增「环境变量」段说明 `FORGE_DATA_HOME` 的 Windows 行为。
- 测试：`TestBuildTaskReport_PinnedChangeDisclosed`（有/无契约两态 + 渲染）、`TestLooksMSYSPath`、`TestGlobalHome_WarnsOnMSYSDataHome`（Windows 专属，stderr 捕获）。

## 影响面

- JSON 只增字段（`pinned_change`），旧消费者不受影响；
- 人读输出多一行 change 披露；未钉 change 的任务文案不变；
- Windows 下首次读 `FORGE_DATA_HOME` 可能多一行 stderr 提示，非 Windows 无变化。

## 怎么算解决

acceptance 五条围栏全过，并且用 `forge task report` 在钉了 change 的任务上实测能看到 change id。

已实测：`forge task report --ref feat/fix-1881` 的考卷披露行显示「产物链：已钉 change（fix-1881）——以 change 产物为准」，`--json` 顶层含 `pinned_change`。