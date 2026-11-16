# 提案：dogfood-fixes

## 问题

1.87 发布后，dogfood 子代理在 `C:\tmp\dogfood` 真实跑完一个任务（87 分），但暴露：

1. `forge next` 在干净工作区连续 3 次输出逐字相同的「下一步：forge status」，
   而 `status` 只建议 human-only 的 `forge harness init`——按 AGENTS.md 字面执行会死循环。
2. `AGENTS.md` 的 Task 流程没有验收登记（`--accept`/`task accept`）、没有 doc gate
   （`docs lint` + `task doc-review`）、没有 commit/review/complete 顺序；
   `--accept` 按空白切分不经 shell 也未写；`task report` 完成后必须 `--ref` 未写。
3. `task complete` 在契约仓库报 `protocol.yml 加载失败（run 'forge init' first）`，
   与「init 不写 protocol.yml」的文档直接矛盾。

## 怎样算解决

- 干净仓库 `forge next` 首次就给出可执行开工命令（task start 或 change new），不再空转。
- AGENTS 模板与 cli.md 覆盖 dogfood 里踩到的每一步，且每条都有断言测试钉住。
- 契约仓库评分不再因缺少 protocol.yml 告警（静默用默认权重），非契约仓库保持告警。

## 影响面

`internal/taskpipeline/next.go`、`internal/taskpipeline/scoring.go`、
`internal/change/templates/agents-protocol.md`、`docs/reference/cli.md`、对应测试。
