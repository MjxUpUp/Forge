# 澄清：边界与非目标

change: `surface-hardening`

## 边界（做什么 / 不做什么）

边界（用户 m04257「全修复」）：①未知子命令必须 exit 1 并报 unknown command（review/change/docs/compat 等非可执行父命令）；②清掉 compat checks 面 18 个无生产者的历史检查名（12 个 eval-*、project-sync、attribution、sync-version-skew、decision-warrant、decision-evidence、task-guard），连带 roster、evidence 白名单与 status 自评测行。非目标：不改 task zzz 既有报错路径、不保留仍被写入的 task-verify、不迁移历史 checklog 数据、不改门禁判定与判据语义、不动 CHANGELOG/ADR 历史事实。

## 非目标

- 不改 `task zzz` 的既有报错路径（task 父命令已用 legacyArgs 报错，行为不变）；
- 不删 `task-verify`/`task-guard` 之外仍有生产者的检查名：`task-verify` 仍由门禁审计行写入，保留；
- 不迁移历史 checklog 数据（旧行仍能读，只是不再有常量名与展示分支）；
- 不改门禁判定、判据语义与验收强度；
- 不动 CHANGELOG/ADR 里已记录的历史事实。

人确认：2026-11-24T05:06:59Z
