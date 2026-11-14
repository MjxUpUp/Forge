# 命令面收敛：去掉宿主层残留文案与死入口

## Objective

把命令面里阶段 B–D 删除宿主层之后残留的过期文案与死入口清干净：`forge --help` 的 init 描述、`forge review gate` 的 Stop-hook 叙述、`forge verify`/`forge config` 的 Short、`forge task resume` 的三个 hook 模式（`--hook`/`--compact-flag`/`--reinject`）及其死代码、`healNpmShimIfNeeded` 的 hook 特例。

## Why

命令帮助是 agent 的主要说明书。教一个已经不存在的 `forge hook`、用「初始化管道」描述只声明仓库契约的 init，会让 agent 写出错误调用；留着没有调用方的 flag 等于在 help 里做假承诺。

## Constraints

- 不改 `task resume` 的接续语义、输出格式与现有 flag 行为；
- 不改 init/verify/config 的实际行为，只改帮助文本；
- 不新增 flag、不动命令分组；
- `TaskState.ResumeStale` 字段保留（JSON 兼容），只停写；
- deadcode 必须保持零。

## Out of Scope

- 命令分组重构（registry/workspace/worktree 拆组）；
- 历史设计文档的大规模重写（只改当前 reference 与帮助）。

## Check

- acceptance #1 build；#2 clitask/taskpipeline 定向测试；#3 no-stale-hook-help 守卫；#4 resume hook flag 消失守卫；#5 cli 文档/兼容守卫；#6 deadcode 零。