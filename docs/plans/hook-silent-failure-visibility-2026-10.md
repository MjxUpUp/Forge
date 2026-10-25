---
status: review
owner: MjxUpUp
ask: 审 hook 基础设施失败的可见性方案：谁负责看见、怎么去重、哪些情形仍然看不见
tier: full
---

# 修复方案 | hook 基础设施失败可诊断、同因同会话只提示一次（2026-10）

## TL;DR
hook 因基础设施原因起不来或跑崩时，宿主把它当「非阻塞错误」放行，没人知道，同样的报错还逐次刷屏。本批把这类失败从门禁裁决里分出来，由能自报的一端各自承担可见性，仍然不阻断：

- forge 进程内能自报的失败（`unknown hook`、panic、hook 返回的非阻断错误）：同一会话里同一原因第一次输出带修复办法的提示并 exit 1（宿主把 stderr 展示给用户），之后同因静默 exit 0。每次发生都落一行 checklog（区分 shown / suppressed）。
- forge 都跑不起来的失败（`env: node: No such file or directory`、`forge: command not found` 即退出码 127、退出码 126）：进程内无法自报，由 `forge doctor` 主动检查——在桌面端的最小 PATH 下解析 hook 命令用到的 `forge`，再解析它的解释器 `node`，报出不可解析项与修复办法。
- npm 包装器 `run.js` 自己能看见的失败（平台子包缺失、spawn 失败、被信号杀死）：同样首次带修复办法 exit 1，之后同因静默。
- hook 版本错位（settings 引用了二进制不认识的 hook）：hook 侧走上面的一次性提示；autoSync 在用户级 settings 里发现二进制不认识的 forge hook 时强制重生成 settings 自愈；`forge doctor` 同步列出。

不做：不改门禁判定逻辑；任何一条路径都不新增阻断，hook 失败的退出码只会是 0 或 1。

## 问题与目标
实证（本机会话转录，约 66 次 hook 非阻塞失败）：

| 现象 | 次数 | 谁能自报 |
|---|---|---|
| `env: node: No such file or directory` | 23（10/24 两个会话） | 无：shebang 在 `run.js` 运行之前失败 |
| `unknown hook: doc-lint` | 9（9/15） | forge 进程（RunHook） |
| `/bin/sh: forge: command not found`（127） | 9 | 无：sh 找不到 forge |
| 退出码 126（路径在 /private/tmp 下） | 9 | 无：文件不可执行 |
| `forge: internal error` | 8 | forge 进程或 run.js |
| 无输出的退出码 1 | 7 | 视来源 |

目标：失败可诊断；同一会话同一原因只提示一次并带修复办法；仍然 fail-open。

## Non-goals
- 不改 hook 命令串（`forge hook batch --event E --matcher M`，生成于 `internal/hooks/profile.go` 的 `ForgeHookWiringForProfile`）。命令串自带降级提示（`command -v forge || ...`）被否决：`IsForgeHookCommand` 以 `forge hook ` 前缀判定 forge 条目，卸载、去重、doctor 扫描、各 translator 与插件包载荷（要求与 settings 逐字节一致）都依赖这个前缀；POSIX 语法在 Windows cmd 下不可用。可见性改由 doctor 承担。
- 不让 `run.js` 解决 node 缺失：`#!/usr/bin/env node` 在 run.js 执行前就失败，run.js 的任何改动都够不到。
- 不改 settings 里 hook 条目的绝对路径化（改变安装契约，另议）。

## 需求与验收
- REQ-1 `forge doctor` 在最小 PATH 下检出 hook 命令跑不起来：forge 不可解析，或 forge 是 node 垫片而 node 不可解析，或解析到临时目录里的 forge
  accept[REQ-1]: go test ./internal/cli/ -run ^TestDoctorHookRuntime$ -count=1 :: --- PASS: TestDoctorHookRuntime (
- REQ-2 基础设施失败与门禁裁决分开：同一会话同一原因只提示一次，提示带修复办法；不同原因、不同会话各提示一次
  accept[REQ-2]: go test ./internal/hooks/ -run ^TestHookInfraFailureDedup$ -count=1 :: --- PASS: TestHookInfraFailureDedup (
- REQ-3 settings 引用了二进制不认识的 hook 时，autoSync 路径自愈（重生成 settings，保留用户自己的条目），hook 侧明确提示
  accept[REQ-3]: go test ./internal/hooks/ -run ^TestUnknownHookSelfHeal$ -count=1 :: --- PASS: TestUnknownHookSelfHeal (
- INV-1 咨询类 hook 仍不阻断，退出码语义不变：基础设施失败路径的退出码只会是 0 或 1，从不是 2
  accept[INV-1]: go test ./internal/hooks/ -run ^TestAdvisoryHookNeverBlocks$ -count=1 :: --- PASS: TestAdvisoryHookNeverBlocks (

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。）
- `internal/hooks/profile.go`
- `npm/run.js`
- `internal/cli/doctor.go`
- `internal/cli/doctor_cli_test.go`
- `internal/hooks/infra.go`
- `internal/hooks/infra_test.go`
- `internal/hooks/infra_advisory_test.go`
- `internal/hookdispatch/hook_infra.go`
- `internal/hookdispatch/hook.go`
- `internal/hookdispatch/hook_test.go`
- `internal/hookdispatch/hook_infra_test.go`
- `internal/doctor/hookruntime.go`
- `internal/doctor/hookruntime_test.go`
- `internal/doctor/doctor.go`
- `internal/checklog/types.go`
- `internal/checklog/escape.go`
- `compat.snapshot.json`
- `internal/cli/root.go`
- `internal/cli/sync.go`
- `internal/cli/sync_test.go`
- `internal/cli/npm_runjs_infra_test.go`
- `docs/plans/hook-silent-failure-visibility-2026-10.md`

触点比任务单声明的四个文件多（新增 checklog 检查名 `hook-infra` 需同步 `types.go`、`escape.go` 的 roster 与 compat 快照 `compat.snapshot.json`）：基础设施提示的核心放进新文件 `internal/hooks/infra.go`（验收测试按任务单落在 `internal/hooks`），分发与退出码映射在 `hookdispatch`/`cli/root.go`，doctor 检查在 `internal/doctor`。`profile.go` 不改：命令串不动（见 Non-goals）。

### 谁承担可见性
| 失败 | 承担端 | 机制 |
|---|---|---|
| node 不在 hook PATH | doctor | 最小 PATH 下解析 forge → 读 shebang → 解析 node |
| forge 不在 hook PATH（127） | doctor | 最小 PATH 下解析 forge 失败 |
| forge 在临时目录、不可执行（126） | doctor | 解析结果位于 `os.TempDir()`、`/tmp`、`/private/tmp` 之下，或缺执行位 |
| 平台子包缺失、spawn 失败、信号杀死 | run.js | 首次 stderr 提示 + exit 1，同会话同因再发静默 |
| unknown hook、panic、hook 非阻断错误 | forge 进程 | `hookdispatch.HandleHookFailure` |
| settings 与二进制 hook 名册错位 | autoSync + doctor | `UserSettingsUnknownHooks` 触发强制重生成；doctor 列出 |

doctor 的「最小 PATH」取桌面端 GUI 进程的典型值 `/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin`（macOS `/etc/paths` 顺序），可用环境变量 `FORGE_DOCTOR_HOOK_PATH` 换成用户自己量到的 hook PATH。doctor 只读，不改任何配置。

### 去重状态
- 落点：`$TMPDIR/forge-hook-infra-<uid>/<会话>/<cause:subject 的哈希>`（目录 0700；根是符号链接时按状态不可用处理；清理只删形如会话桶的目录），用 `O_EXCL` 创建，超过 12 小时的旧标记视为陈旧并重新认领（PID 复用），首个创建者即「第一次」。会话级短期状态放 `os.TempDir()`，由系统清理；创建新会话目录时顺手清掉 7 天前的兄弟目录（有界扫描）。状态目录不可写时按「第一次」处理（宁可多提示，不吞）。
- 会话键：forge 进程用 stdin 里已归一的 session id；拿不到时回落 `ppid-<父进程号>`。run.js 不能消费 stdin（子进程要读），固定用 `ppid-<process.ppid>`。`sh -c` 不 exec 末条命令的宿主上 ppid 逐次变化，去重退化为逐次提示，不比现状差。
- 原因键：`cause:subject`。cause 取 `unknown-hook` / `panic` / `hook-error`（forge 进程）与 `subpackage-missing` / `spawn-failed` / `signal-killed`（run.js）；subject 是 hook 名或截断后的报错首行。

### 逃生口
`FORGE_HOOK_INFRA_DEDUP=0` 关闭去重与静默，恢复逐次 exit 1 并打印原始错误。每次发生（无论 shown 或 suppressed）都落一行 checklog（check=`hook-infra`，Detail 带 cause、subject、shown/suppressed、是否关闭了去重），可审计；没有项目根时跳过 checklog（非 forge 项目里的 unknown hook）。

### 退出码
`Execute` 对 hook 子命令：阻断错误 → 2（不变）；其余非阻断错误与 panic → 首次 1、同因再发 0。`unknown hook` 由 `UnknownHookError` 类型化后走同一路径。run.js：首次 1，同因再发沿用原来的 `{"decision":"approve"}` + 0。

### 自愈（REQ-3）
`hooks.UserSettingsUnknownHooks()` 解析用户级 `settings.json`，列出 `forge hook <name>` 条目里名册（`ForgeHookSpec` 全部名字加 `batch`）之外的名字。`hooks.HealUnknownUserHooks()` 在有未知名且没有安装 forge 插件时调 `GenerateUserSettings()`——它先剥掉全部 forge 来源条目、保留用户条目，再追加本二进制的接线，所以未知条目被收掉、重复执行幂等。`autoSync` 的版本戳 no-op 判定里把「存在未知 forge hook 且自愈可行」（`UserSettingsNeedHeal`）并入强制同步条件。已装 forge 插件时 autoSync 的 defer（`dedupeProjectLevelIfPlugin`）本来就整体剥除用户级 forge hook，未知条目随之消失，所以该条件把「可行」作为门，插件场景保持快速 no-op 路径。方向说明：只处理「settings 引用了本二进制不认识的 hook」；本二进制比 settings 新、settings 缺 hook 的方向由版本戳覆盖，不在此处理。

### 关键取舍
- doctor 为主、进程内提示为辅：node/127/126 三类共占约 60%，进程内无从自报，只有 doctor 能在装机后主动发现。
- 首次 exit 1 而不是 exit 0 + 注入上下文：exit 0 的 stderr 宿主不展示；exit 1 的 stderr 是宿主已有的、展示给用户的非阻塞错误通道，且各宿主都不把 1 当阻断。
- 不在 hook 进程里自愈改 settings：hook 要保持低开销、无副作用，并发 hook 进程改写同一份 settings 有竞态。autoSync 在 `forge hook` 调用里不触发「未知 hook 自愈」强制同步（`isHookInvocation`），自愈留给非 hook 命令；自愈写盘失败只告警、不让命令起不来。

### 已知局限
- 命令串不动，所以 node/127/126 在运行时仍是宿主逐次报的原始错误，forge 无法去重或加提示；只有跑过 `forge doctor` 才能看到修复办法。
- doctor 的最小 PATH 是典型值而非实测值；不同桌面端的真实 PATH 不同，用 `FORGE_DOCTOR_HOOK_PATH` 校准。
- run.js 的会话键是 ppid，在 `sh -c` 不 exec 的宿主上退化为逐次提示。
- 同一会话内第二个不同原因仍会各提示一次，不做会话级总量上限。
- 老二进制（本批之前的版本）仍然逐次 exit 1，修复只对升级后的版本生效。
- run.js 侧的 `FORGE_HOOK_INFRA_DEDUP=0` 逃生口落不了 checklog：这些失败发生时 forge 二进制缺席或没起来，Go 侧才有审计行。
- run.js 的 fail-open 仍沿用既有的 stdout `{"decision":"approve"}`（同因重复时 exit 0 会带它）。仓库其他 allow 路径已刻意不再发这个信封（它在 Claude PreToolUse 上会绕过权限流程），run.js 是遗留面；本批不改 stdout 契约，另议。

## 验证与回滚
- `go build ./...`、`go vet ./...`；先单包 `go test ./internal/hooks/ ./internal/hookdispatch/ ./internal/doctor/`，再 `go test ./... -count=1 -timeout 30m`。
- 回滚即 revert 本分支的提交；不涉及数据格式变更，状态目录在 `$TMPDIR` 下可直接删除。
