# 入口路由与阶段机：功能改动先立 change：目标契约

change: `entry-routing`

> 这份契约是任务的验收前提：五段都要填，删掉每段里的 `forge:fill` 注释标记才算写完。
> 启动任务后改它 = 改判据：`task gate task-implement` 会 BLOCKED；还原 goal.md，
> 或 `forge task abort` 后重新 `task start --change` 钉新哈希。

## Objective

在声明了 spec 契约的仓库里，把「功能改动先立 change」从文档建议变成机器强制：`forge next` 不再把未归属改动直接导向 `task start`，而是先给立 change 的下一步；`task start` 在没带 `--change` 时必须显式声明 `--kind task` 才能开工；change 增加可推导的阶段（specify → clarify → plan → tasks → ready），未到 ready 不能启动任务。

## Why

2026-11-15 JustForMe 一天 9 个任务、0 个 change：agent 跑了 `forge next`，拿到「先建任务收编」，于是直接开工。`forge next` 只读 git + TaskState（`internal/taskpipeline/next.go:58-64`），不 import specroot，change 在入口根本不存在；`task start --change` 是可选的，goal 契约只到 implement 门禁的 advisory。结果是「有 spec 流程的仓库」实际从未走过 spec 流程。

## Constraints

- 只对**声明了 `spec/forge.yaml`** 的仓库生效；未声明的仓库 `next` 与 `task start` 行为不变。
- `--kind task` 必须可用，作为「一句话小修」的显式出口；不能把纯任务流程堵死。
- 不引入 plan digest 批准、不引入逐节点报告文件（那是 LoopSpec 的耗时来源）。
- 阶段由文件系统推导，不新增需要手改的状态文件；`clarify` 记录可由 `forge change clarify --by-human` 生成。
- 保持 `next` 恰给一条命令；`--json` 字段向后兼容（新增字段只增不改）。

## Out of Scope

- 不实现 fragment/profile 编译（那是后续 R1，不在本 change）。
- 不改 `change verify` 的封印规则；不引入新的证据类型。
- 不对未声明 spec 的仓库做任何变更。

## Check

验收以 acceptance 围栏为准：#1 build；#2 next 决策表；#3 task start 守卫；#4 阶段与澄清记录；#5 e2e 真实路由；#6 deadcode 零；#7 文档/模板守卫。全部通过后 `forge change verify entry-routing` 封印。