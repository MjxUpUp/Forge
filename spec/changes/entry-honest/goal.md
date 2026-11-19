# 入口诚实：目标契约

change: `entry-honest`

## Objective

用户提出新需求时，agent 不应被 `forge next` 引导去推进一个已经放弃的 change；判据的可信度也不再靠「命令名像不像测试」的枚举，而靠可执行证据（红先绿 + 实跑 + 评审认领）。

## Out of Scope

- 不新增任何「命令像不像测试」的白名单或启发式——枚举永远补不完。
- 不改评分模型（ReviewScore 仍只记录，不参与打分）。
- 不删除 `FORGE_TEST_COVERAGE` 的历史审计含义（它不再阻断任何门禁）。

## Check

- 仓库里只有陈旧 ready change 时，`forge next` 给新任务入口（`forge change new <id>`），不再推荐 `task start --change <stale>`。
- `task complete` 不再因「考卷里没有测试类命令」被拒。
- AGENTS 协议写明：用户提新需求直接开工，`forge next` 只用于继续/接手现有任务。
