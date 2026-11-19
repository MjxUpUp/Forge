# Goal：仓库级回归基线（forge eval suite）

## Objective

把已归档 change 的 acceptance 判据变成可重复运行的跨任务回归集：改一处代码，能一条命令跑所有历史判据，看到整体有没有退化。
今天 Forge 的判据是「一个 change 自己的、跑完就归档」；跨任务退化（新改动让三个月前某条判据失效）只能靠运气发现。

## Out of Scope

- 不做门禁：`eval suite` 失败不阻断 task complete，也不进 CI required checks（它跑的是历史判据，其中有些依赖当时的环境）。
- 不自动生成新判据（生成式 slop 没有意义）；金标准集由评审与真实事故沉淀。
- 不改 acceptance 的语法，也不解析除 ```bash 之外的围栏。

## Check

- `forge eval suite` 能按 change id 过滤、能限制条数、能 `--json`、能 `--record` 落盘。
- 围栏里写 `# suite: skip` 的整条跳过（重判据归 change 自己的 verify）。
- `forge eval trend` 能读出最近 N 次的通过率与 HEAD。