# 命令面收敛：去掉宿主层残留文案与死入口

change: `cmd-surface-convergence`

## 为什么做

- 现状：阶段 B–D 删掉了 hook 接线与宿主层，但命令帮助里仍留着那套说法——根帮助说 `forge init`「初始化管道」，`forge review gate` 说自己由 Stop hook 调用、Stop hook 会自动拦截，`forge task resume` 还挂着三个只为 `forge hook` 服务的模式（`--hook`/`--compact-flag`/`--reinject`）。
- 问题（谁受影响、多严重）：agent 靠 `--help` 决定下一步。读到不存在的命令会写出错误调用；读到「自动拦截」会以为有兜底，而实际上阶段 D 之后是显式命令 + CI。误判一次就可能跳过审查或等一个永远不会来的 hook。
- 不做的代价：帮助文本持续与实现背离，后面每个接手的 agent 都要重新踩一遍。

## 备选方案与取舍

- 方案 A（采用）：删除死入口（三个 resume hook flag 及其分支/实现/测试），把残留的 help 文案改成现状；保留 `TaskState.ResumeStale` 字段做 JSON 兼容。
- 取舍：`task resume` 少三个隐藏 flag；没有调用方，删除无行为损失。
- 方案 B（否）：只改文案、保留 flag（标 deprecated）。否决理由：flag 没有调用方，留着仍是假入口；deadcode 也会一直报它们的实现。
- 方案 C（否）：保留 `--hook` 只输出横幅。否决理由：横幅是给已删除的 SessionStart 通道用的，没有宿主会读它。

## 怎样算解决

- 结论（一句话，可观察）：`forge --help`、`forge review gate --help`、`forge verify --help`、`forge config --help` 与 `forge task resume --help` 里不再出现 hook 时代措辞或 hook 专用 flag，且 build/定向测试/deadcode 全绿。
- 这条结论由 `acceptance.md` 的哪几条围栏证明（写围栏序号）：#1、#2、#3、#4、#5、#6

## 影响面

- 改动范围（目录 / 接口）：`internal/cli/{root,review,verify,config,shim_heal}.go`、`internal/clitask/task_continuity.go`、`internal/taskpipeline/state.go`、相关测试、`scripts/checks/`。
- 需要同步：文档无需改（`docs/reference/cli.md:152` 已经写「没有 Stop hook」）；`TaskState.ResumeStale` 字段保留，JSON 兼容不变。