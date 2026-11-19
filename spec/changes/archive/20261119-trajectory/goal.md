# Goal：轨迹观测（第 4 项）

## Objective

把「这一步花了多久、跑了几轮」变成落盘事实：checklog 条目带 `duration_ms` / `steps` / `attempt`，`forge task report` 显示轨迹行。目标是让「验收 2 轮、末轮 4.2s、3 条判据」「评审 3 轮」这类问题有直接答案，而不是靠墙钟差猜。

## Out of Scope

- 不改评分（评分已在 drop-score 中下线，证据改事实清单）。
- 不为每个门禁检查新增独立 checklog check 名（会影响 compat 的 check 名单）；门禁耗时先由各检查自己的 DurationMS 表达。
- 不做可视化面板/图表。

## Check

- `forge task report` 对本任务输出「轨迹：验收 N 轮（末轮 Xs·K 条判据）· 评审 M 轮（末轮 Ys）」；没有耗时条目时输出「未记录」。
- 验收实跑落章带 `duration_ms > 0` 与 `steps = 判据条数`。
- 评审落章带 `attempt = 第几轮` 与 `steps = 本次 findings 条数`。
