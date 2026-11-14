# goal 契约：五段式目标、fast 需人批

change: `task-goal-contract`

## 为什么做

- 现状：任务的目标只有 `--title` 一行和可选的 `--goal` 自由文本；两者都不进仓库、不参与评审，会话压缩后即丢。
- 问题（谁受影响、多严重）：agent 与人都只能从 diff 反推意图；范围蔓延（阶段 D：568 文件 / −77,841 行）无法在实现阶段被机器或评审拦下。
- 不做的代价：acceptance 证明「命令跑过」，但证明不了「跑的是这件事」；评审越晚越贵。

## 备选方案与取舍

- 方案 A（采用）：goal 作为 change 的第五份产物 `spec/changes/<id>/goal.md`，五段固定标题；`task start --change` 读入并钉 `Path+SHA256`，implement 门禁重算哈希，变了即阻断；`--by-human` 才能开 fast change。
- 取舍：goal 与 acceptance 分家——goal 回答「做什么/为什么/不做什么」，acceptance 回答「怎么证明」；两份都可评审，但只有 acceptance 产生证据。
- 方案 B（否）：把五段塞进 `--goal` 自由文本。
- 否决理由：没有稳定结构就没有校验，也没有哈希可比；填没填全靠自觉。
- 方案 C（否）：goal 放 `~/.forge` 运行态。
- 否决理由：跨机器不可见、不可评审，与阶段 C「产物进仓库」的方向相反。

## 怎样算解决

- 结论（一句话，可观察）：`forge change new` 必产 `goal.md`；`task start --change` 在 goal 未填完时拒绝启动；启动后改 `goal.md` 会让 `task gate task-implement` BLOCKED；`--fast` 缺 `--by-human` 被拒。
- 这条结论由 `acceptance.md` 的哪几条围栏证明（写围栏序号）：#1、#2、#3、#4、#5、#6、#7

## 影响面

- 改动范围（目录 / 接口）：`internal/change`（goal.go / templates / New / DocProblems / status）、`internal/clitask`（task start 新 flag）、`internal/taskpipeline`（checkGoalContract）、`internal/tasktypes`（GoalContract）、`internal/checklog`（goal-contract check 名）、`internal/cli`（change new 的 --by-human 与 help）。
- 需要同步：文档（README / cli.md / AGENTS.md 协议段）、compat 快照（新增 flag 与 check 名）。