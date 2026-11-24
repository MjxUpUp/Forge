# goal：surface-hardening

## Objective

收掉两处「命令表面说一套、实际做一套」：

1. 未知子命令退出码为 0：`forge review zzz`、`forge change zzz`、`forge docs zzz`、`forge compat zzz` 都打印帮助并成功退出，脚本无法据此判断命令拼错。要求非可执行父命令收到多余位置参数时报 `unknown command` 并 exit 1。
2. compat checks 面残留 17 个已无生产者的历史检查名（12 个 `eval-*`、`project-sync`、`attribution`、`sync-version-skew`、`decision-warrant`、`decision-evidence`，另加 hook 时代的 `task-guard`），`forge status` 的自评测行也只在读这些已停产的行。要求把它们从 checklog 常量、roster、evidence 白名单与 status 展示中移除。

完成状态可观察：上述四条命令退出码 1 且 stderr 含 `unknown command`；`go test ./internal/checklog -run TestNoHistoricalCheckNames` 绿；compat 快照 checks 面只剩仍有生产者的名字。

## Out of Scope

- 不改 `task zzz` 的既有报错路径（task 父命令已用 legacyArgs 报错，行为不变）；
- 不删 `task-verify`/`task-guard` 之外仍有生产者的检查名：`task-verify` 仍由门禁审计行写入，保留；
- 不迁移历史 checklog 数据（旧行仍能读，只是不再有常量名与展示分支）；
- 不改门禁判定、判据语义与验收强度；
- 不动 CHANGELOG/ADR 里已记录的历史事实。

## Check

见 `acceptance.md` 五条围栏：构建、未知子命令守卫、历史检查名守卫、三包测试、能力面漂移。
