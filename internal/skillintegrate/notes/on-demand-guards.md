# on-demand-guards · Forge 集成

仅 forge 项目适用。skills 零反向依赖契约（CONVENTIONS §13）下，本 skill 的 forge 集成知识由 forge 侧维护。

## hazard-guard 自动挡（always-on）

高危命令（`rm -rf` / force-push / DROP/TRUNCATE / 无 WHERE DELETE / kubectl delete 等）由 hazard-guard hook（PreToolUse Bash，所有 agent 自动接线）始终拦截，无需激活本 skill。

**拦截后放行（HITL）**：授权判定成立时执行 `forge hazard confirm --last`（或 `forge hazard confirm --fingerprint <hex>`）登记放行——重试原命令自动通过（5min 限时标记，events.jsonl 审计）。confirm 链是唯一放行路径，测试/CI 同样走它。

**自动豁免**（不拦截，无需 confirm）：`rm -rf` 目标全部落在一次性临时区时——字面 `/tmp/*`、`/var/folders/*`、`/private/tmp/*`、`$TMPDIR` 子路径，或同一命令串内 `X=$(mktemp -d)` 赋值变量的引用（`rm -rf "$X"`；变量有其他赋值则作废保守拦截）。危险串仅在引号/注释/多行字符串内（grep 模式、commit message、python heredoc 字符串等数据上下文）也不拦；`bash -c`/`eval`/管道进 shell 等执行包裹仍拦。

## freeze 真 hook

```bash
forge freeze <path>     # 激活：冻结 <path> 之外的写入
forge freeze --status   # 查看当前冻结状态（状态由 forge 持有，不靠 agent 记忆）
forge freeze --off      # 解除
```

激活后 freeze-guard hook（PreToolUse Write|Edit）硬阻断冻结路径外的写入——真 hook，长会话/上下文压缩后依然生效，优先于 prompt 纪律模拟。
