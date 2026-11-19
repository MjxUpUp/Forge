# 目标：删掉合成评分

## Objective

`task complete` / `task report` / `task list` 不再出现任何合成分数或等级（Score、Overall、Grade、DimScores）：
完成时只报事实清单（判据条数、红先绿条数、评审轮次与分数、逃生舱次数、未测区域、剩余风险），
报告的 JSON 用 `evidence` 对象替代 `score`。合成分没有任何门禁读取，却把「证据比例」变成
可刷的目标（2026-11 实测：评审 88/100、7/7、35 条 findings 全闭的任务拿 73(C)）。

## Out of Scope

- 不改 acceptance / 红先绿 / 评审者复用 / 两道门禁的判定逻辑。
- 不删 `forge task golden`（它仍读历史评分输入，留作回归素材），也暂不删 `internal/scoring` 包本身。
- `act`/`health`/`merge` 的分数聚合在第二批处理（本 change 只动 complete/report/list 三个用户可见面）。

## Check

1. `forge task complete` 的输出不含 `Score:` / `(A)` 之类等级串，只含事实清单一行与「详见 report」。
2. `forge task report --json` 不含 `score`/`grade`/`dim_scores`，含 `evidence` 对象。
3. `forge task list` 不再有等级列；历史任务的 `score` 字段被忽略而非报错。