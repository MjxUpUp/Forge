# 升级提示泄漏进 hook 输出 + 每命令重复提示（任务 B）

- 档位：mini
- 结论：两处缺陷都在 `internal/cli`，修法互不依赖，一个任务内修完。

## 一、现象与根因

转录排查：`💡 Forge x.y.z 可用（当前 …）` 出现在 214 条工具结果、18 个会话里，部分混进 hook 拦截文案。

1. **hook 子树漏判。** `shouldSkipUpdateCheck` 用 `cmd.Name() == "hook"`，而 `forge hook batch` 的 `cmd.Name()` 是 `batch`，漏判后 hook 路径照常打印提示到 stderr（stderr 可能作为阻断原因进入 agent 上下文）。`root.go` 的 `healNpmShimIfNeeded(cmd.Name() == "hook")` 同款判断，同款漏洞。（读代码得出，用测试复现。）
2. **无节流。** 缓存命中且有新版本时，每条命令都打印提示，24h 内同一版本反复出现。

## 二、需求

- REQ-1：整个 hook 子树（含 `hook batch`）跳过升级检查与垫片提示。判断沿 `cmd.Parent()` 向上找祖先名为 `hook`，抽成单一函数 `isHookSubtree`，`update_check.go` 与 `root.go` 共用。
- REQ-2：同一版本 24 小时内最多提示一次。`updateCache` 增加 `notified_version` / `notified_at`，`saveUpdateCache` 重写缓存时沿用这两个字段，否则刷新缓存即清空节流记录。逃生：`FORGE_UPDATE_NOTICE_THROTTLE=0` 回到每次提示。
- INV-1：`FORGE_SKIP_UPDATE_CHECK` 仍然生效，原有 `TestShouldSkipUpdateCheck` 全过。

不做：版本检查的网络逻辑与缓存过期策略（`isExpired`、负缓存 TTL）不动。

## 三、触点

- `internal/cli/update_check.go`
- `internal/cli/update.go`
- `internal/cli/root.go`
- `internal/cli/update_test.go`

## 四、验收

accept[REQ-1]: go test ./internal/cli/ -run ^TestShouldSkipUpdateCheckHookSubtree$ -count=1 :: --- PASS: TestShouldSkipUpdateCheckHookSubtree (
accept[REQ-2]: go test ./internal/cli/ -run ^TestUpdateNoticeThrottle$ -count=1 :: --- PASS: TestUpdateNoticeThrottle (
accept[INV-1]: go test ./internal/cli/ -run ^TestShouldSkipUpdateCheck$ -count=1 :: --- PASS: TestShouldSkipUpdateCheck (
