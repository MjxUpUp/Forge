# 升级提示泄漏进 hook 输出 + 每命令重复提示（任务 B）

- 档位：mini
- 结论：两处缺陷都在 `internal/cli`，修法互不依赖，一个任务内修完。
- TL;DR：hook 子树（含 `hook batch`）静默，不再打印升级提示与垫片提示；同一版本 24 小时内最多提示一次。命令与 JSON 输出不变，唯一新增环境变量 `FORGE_UPDATE_NOTICE_THROTTLE`。

## 一、现象与根因

转录排查：`💡 Forge x.y.z 可用（当前 …）` 出现在 214 条工具结果、18 个会话里，部分混进 hook 拦截文案。数字来自上一轮对本机会话转录的人工排查，未留统计命令；泄漏路径由读代码得出，本任务用测试复现（修复前 `isHookSubtree` 尚不存在、测试无法编译；把判断改回 `Name()=="hook"` 时 `hook batch` 用例失败）。

1. **hook 子树漏判。** `shouldSkipUpdateCheck` 用 `cmd.Name() == "hook"`，而 `forge hook batch` 的 `cmd.Name()` 是 `batch`，漏判后 hook 路径照常打印提示到 stderr（stderr 可能作为阻断原因进入 agent 上下文）。`root.go` 的 `healNpmShimIfNeeded(cmd.Name() == "hook")` 同款判断，同款漏洞。`forge hook batch` 定义在 `internal/hookdispatch/hook_batch.go:40`。
2. **无节流。** 缓存命中且有新版本时，每条命令都打印提示，24h 内同一版本反复出现。

## 二、需求

- REQ-1：整个 hook 子树（含 `hook batch`）跳过升级检查与垫片提示。判断沿 `cmd.Parent()` 向上找祖先名为 `hook`，抽成单一函数 `isHookSubtree`，`update_check.go` 与 `root.go` 共用。垫片提示只共用该函数，`root.go` 的接线不单测（`healNpmShimIfNeeded` 仅 Windows 生效，见 `internal/cli/shim_heal.go:31`）。
- REQ-2：同一版本 24 小时内最多提示一次。`updateCache` 增加 `notified_version` / `notified_at`，`saveUpdateCache` 重写缓存时沿用这两个字段，否则刷新缓存即清空节流记录。换新版本不受节流立即提示；缓存读写失败照常提示（fail-open）；时钟回拨（记录时间在未来）不节流；换安装通道不沿用另一通道的记录；并发进程可能各提示一次（读改写无锁，节流为尽力而为）。逃生：`FORGE_UPDATE_NOTICE_THROTTLE=0` 回到每次提示。
- INV-1：`FORGE_SKIP_UPDATE_CHECK` 仍然生效，原有 `TestShouldSkipUpdateCheck` 全过。

Non-goals / 不做：版本检查的网络逻辑与缓存过期策略（`isExpired`、负缓存 TTL）不动。

## 三、备选方案

| 备选 | 否决理由 |
|---|---|
| 什么都不做 | 每条命令一条提示，且提示的 stderr 泄漏进 hook 输出，已在 18 个会话里造成干扰。 |
| 只修 hook 判断，不做节流 | 交互命令里同一版本每次都提示的频次问题仍在（REQ-2）。 |
| 改用 `cmd.CommandPath()` 前缀匹配 | 依赖根命令名拼接，命令改名即失效；沿 `Parent()` 找祖先名更直接。 |

## 四、触点

- `internal/cli/update_check.go`：`isHookSubtree` / `maybePrintUpdateNotice` / `noticeThrottled` / `saveUpdateCache` / `writeUpdateCache`
- `internal/cli/update.go`：`updateCache` 新增字段
- `internal/cli/root.go`：`PersistentPreRunE` 里改用 `isHookSubtree`
- `internal/cli/shim_heal.go`：仅更正 `healNpmShimIfNeeded` 注释（「hook 子命令」改为「hook 子树」）
- `internal/cli/update_test.go`：新增 `TestShouldSkipUpdateCheckHookSubtree`、`TestUpdateNoticeThrottle`，以及边界与端到端接线测试 `TestUpdateNoticeThrottleEdges`、`TestCheckForUpdateNoticeOncePerVersion`

## 五、验收

accept[REQ-1]: go test ./internal/cli/ -run ^TestShouldSkipUpdateCheckHookSubtree$ -count=1 :: --- PASS: TestShouldSkipUpdateCheckHookSubtree (
accept[REQ-2]: go test ./internal/cli/ -run ^TestUpdateNoticeThrottle$ -count=1 :: --- PASS: TestUpdateNoticeThrottle (
accept[INV-1]: go test ./internal/cli/ -run ^TestShouldSkipUpdateCheck$ -count=1 :: --- PASS: TestShouldSkipUpdateCheck (
