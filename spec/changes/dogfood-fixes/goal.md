# 目标契约：dogfood-fixes

## Objective

修复 1.87 真实 dogfood 暴露的两个可用性断点：`forge next` 在干净工作区把 agent 困在
`next → status → next` 的空转里；以及 `AGENTS.md` / `docs/reference/cli.md` 与门禁
实际流程不一致（验收登记、doc gate、提交/审查/完结顺序、`--accept` 不经 shell、
`task report --ref` 必填、`protocol.yml` 警告）。

## Why

1.87 的入口路由让任务成为一等入口，但 dogfood 子代理按协议照做时仍被两件事拖住：
next 不给开工命令，文档不写验收与文档闸。对真实用户来说，「协议写得像能照抄、照抄却走不通」
比没有协议更糟。

## Constraints

- 只改 next 的干净工作区分支、评分回退日志、文档与模板；不改 `--accept` 的解析语义
  （不经 shell 是既定设计，只补文档）。
- 不新增 flag、不改 acceptance 语法、不改门禁强度。
- next 仍保证「恰好一条命令」；`behindRemote > 0` 的同步优先于开工建议。

## Out of Scope

- 不实现 open change 与任务之间的显式绑定（`--change` 仍是可选链接）。
- 不改 `forge status` 的 harness 建议（human-only，保留）。
- 不把 `--accept` 改成经 shell 执行（安全考虑）。
- 不重写 quality/skills 相关文档之外的内容。

## Check

- `go build ./...` 通过；`next` 在 spec 仓库干净工作区给出 `forge task start`，
  落后远端时先给 `git pull --ff-only`。
- 契约仓库 `task complete` 不再打印「protocol.yml 加载失败」；非契约仓库仍告警。
- AGENTS 模板写入验收登记、doc gate、提交→审查→完结顺序、`--accept` 切分方式、
  `task report --ref`；对应断言测试通过。
