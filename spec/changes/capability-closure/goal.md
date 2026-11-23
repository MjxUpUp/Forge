# 目标契约

## Objective

把「文档教了不存在的动作」从只能靠人眼发现，变成一条可跑的命令与一份随 verify 落盘的证据。

- 新包 `internal/capability`：拿运行中的命令树（`internal/agentproto.FromCobra`）去比活文档（`README.md`、`AGENTS.md`、`docs/reference/**`、`internal/change/templates/**`），报出「文档里写了、命令树里没有」的引用；带「已删除 / 已移除 / deprecated / removed in」的行视为迁移说明，豁免；父命令前缀（如 `forge task`）算已知，不误报。
- `forge change verify` 的 `summary.json` 带 `capability` 段（命令数 / flag 数 / 有注解数 / 漂移数 / 漂移清单），运行输出里逐条打印漂移——封印时顺便留下「文档是否还在教不存在的动作」的事实。
- 新命令 `forge docs closure [--json] [--strict]`：单独查看闭合表；`--strict` 有漂移时退出非 0，为将来把漂移接进门禁留接口，本次不接。

## Out of Scope

- 不把漂移变成 verify 的阻断项：它进摘要、进输出、不改变封印判定（漂移多少都不影响 sealed）。
- 不做 CI / hook 层拦截；不改 `internal/agentproto.Check` 的判据口径（那边管协议块，这边管活文档）。
- 不扫全仓所有 `.md`：只收上面那份活文档白名单，历史裁决记录（`skills/*/decisions.md`）不追。
- 不修文档里已存在的历史漂移：README 迁移表那些行是本次发现的证据，修不修、怎么修由人另定。
- 不新增顶层命令组（`docs closure` 挂在既有 `docs` 组下）。

## Check

- `forge docs closure` 能给出命令面计数与逐条漂移（文件:行号 + 命令）。
- `forge change verify <id>` 跑完，`spec/changes/<id>/evidence/<ts>/summary.json` 里有 `capability` 段。
- 三条单测覆盖：扫描分类、已删除语境豁免、verify 摘要携带闭合表。
