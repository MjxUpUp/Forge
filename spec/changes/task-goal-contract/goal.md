# goal 契约：五段式目标、fast 需人批

change: `task-goal-contract`

## Objective

让每个走 forge 流程的任务都有一份进 git、可评审、且被门禁校验的 `goal.md`：五段（Objective / Why / Constraints / Out of Scope / Check）缺一不可；任务启动时把它钉成内容哈希，实现阶段改动 goal 即 BLOCKED。

## Why

现状里「为什么做」要么是 `task start --title` 的一行标题，要么是会话里的口头说明——会话一压缩就没了，评审只能靠猜意图。阶段 D 的 568 文件/−77,841 行 diff 就是反面教材：目标、边界、范围外都没有可引用的文本，评审无法判断「多出来的改动」是不是任务本身。把 goal 变成第五份产物并钉哈希，才能让「范围蔓延」在机器侧有可判定的信号。

## Constraints

- 兼容没有 `goal.md` 的老 change 与老任务：缺失只 advisory/提示，不阻断。
- 不复制正文进 TaskState，只存 `Path + SHA256`，避免双写漂移。
- 不新增普通 change/task 的必填 flag：`--goal` 自由文本继续可用（无契约，走 advisory）；fast 的 `--by-human` 是唯一例外。
- Windows/macOS/Linux 路径处理一致（仓库相对路径、正斜杠）。

## Out of Scope

- 不做 goal 的版本历史/多版本并存（改 goal = 重开任务，git 自己记历史）。
- 不自动从 proposal/design 推断 goal。
- 不给 `forge change` 加交互式填写向导。

## Check

`acceptance.md` 的 #1–#8：build、goal 解析与老 change 兼容、task start 互斥与仓内/符号链接边界、change new 人批、文档口径、deadcode 零。