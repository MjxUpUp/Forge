# 入口路由与阶段机：任务是一等入口，change 可后补：目标契约

change: `entry-routing`

> 这份契约是任务的验收前提：五段都要填，删掉每段里的 `forge:fill` 注释标记才算写完。
> 启动任务后改它 = 改判据：`task gate task-implement` 会 BLOCKED；还原 goal.md，
> 或 `forge task abort` 后重新 `task start --change` 钉新哈希。

## Objective

在声明了 `spec/forge.yaml` 的仓库里，让路由与实际工作方式一致：**任务是一等入口**
（`task start` 随时可开），**change 是记录单元**（可开工前立、也可中途补，`change verify`
才是它的封口）。机器负责的是三件事：`forge next` 在脏树时给出与当前状态相符的建议
（立 change / 推进已有 change / 已 ready 就直接开工）；显式 `--change` 时校验该 change
已到 `ready` 阶段并把 goal 钉成哈希；`change phase` 从产物推导还缺什么，供人和 agent
判断「现在补记录还是先干活」。

## Why

2026-11-15 JustForMe 一天 9 个任务、0 个 change：agent 跑了 `forge next`，拿到「先建任务收编」，于是直接开工。第一版修复把「先立 change」做成 `task start` 的硬前置，结果把
两条正路（小修、先干活后补记录）都堵死：agent 用 `--kind task` 绕，或者换个说法重试；
fast change 也因为缺 design/tasks 永远到不了 ready。根因不是「agent 不听话」，而是
**把记录单元放在了执行单元前面**。本版按真实工作方式反过来：任务先走，change 可后补，
机器只保证「链接了就一致」。

## Constraints

- 只对**声明了 `spec/forge.yaml`** 的仓库给建议；未声明的仓库 `next` 与 `task start` 行为不变。
- 不带 `--change` 的 `task start` 必须成功（任务是一等入口）；带 `--change` 才要求 ready。
- `--kind task` 保留为「这句话是小修、没有立 change」的显式标记，仍走三道门禁；它不是绕过门禁的开关。
- 不引入 plan digest 批准、不引入逐节点报告文件（那是 LoopSpec 的耗时来源）。
- 阶段由文件系统推导，不新增需要手改的状态文件；`clarify` 记录由 `forge change clarify --by-human` 生成。
- fast change 不要求 design/tasks；proposal 与 acceptance 必须是真内容而不是脚手架。
- `next` 仍恰给一条命令；`--json` 字段向后兼容（新增字段只增不改）。

## Out of Scope

- 不实现 fragment/profile 编译（那是后续 R1，不在本 change）。
- 不改 `change verify` 的封印规则；不引入新的证据类型。
- 不对未声明 spec 的仓库做任何变更。
- 不改 acceptance 围栏语法（沿用现有 ```bash 判据格式）。

## Check

验收以 acceptance 围栏为准：#1 build；#2 next 决策表（含 ready change 前进）；
#3 任务是一等入口、显式 `--change` 才校验 ready；#4 阶段推导（含 fast/老 change 例外）；
#5 e2e 真实链路；#6 托管块守卫；#7 CLI 文档守卫；#8 gofmt 干净；#9 deadcode 零。
全部通过后 `forge change verify entry-routing` 封印。