# 澄清：边界与非目标

change: `capability-closure`

## 边界（做什么 / 不做什么）

边界：新增 internal/capability，用运行中的命令树（agentproto.FromCobra）比对活文档（README.md、AGENTS.md、docs/reference/**、internal/change/templates/**），报「文档写了但命令树里没有」的引用；带「已删除/已移除/deprecated/removed in」的行豁免；父命令前缀算已知。verify 摘要 summary.json 新增 capability 段，change verify 输出打印漂移明细；新增 forge docs closure [--json] [--strict]（--strict 有漂移退出非 0）。非目标：漂移不阻断 verify 封印（advisory）；不做 CI/hook 拦截；不改 agentproto.Check 口径；只扫活文档白名单，不追 skills/*/decisions.md 等历史记录；不修文档里已存在的历史漂移（README 迁移表那些行只作为证据列出）；不新增顶层命令组。

## 非目标

- 不把漂移变成 verify 的阻断项：它进摘要、进输出、不改变封印判定（漂移多少都不影响 sealed）。
- 不做 CI / hook 层拦截；不改 `internal/agentproto.Check` 的判据口径（那边管协议块，这边管活文档）。
- 不扫全仓所有 `.md`：只收上面那份活文档白名单，历史裁决记录（`skills/*/decisions.md`）不追。
- 不修文档里已存在的历史漂移：README 迁移表那些行是本次发现的证据，修不修、怎么修由人另定。
- 不新增顶层命令组（`docs closure` 挂在既有 `docs` 组下）。

人确认：2026-11-23T16:16:51Z
