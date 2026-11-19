# 澄清：边界与非目标

change: `drop-score`

## 边界（做什么 / 不做什么）

用户 2026-11-19「将这些都完整落地」：六项中的第 1 项（删评分体系）；本 change 先动 complete/report/list 三个用户可见面，act/health/merge 与删包留第二批

## 非目标

- 不改 acceptance / 红先绿 / 评审者复用 / 两道门禁的判定逻辑。
- 不删 `forge task golden`（它仍读历史评分输入，留作回归素材），也暂不删 `internal/scoring` 包本身。
- `act`/`health`/`merge` 的分数聚合在第二批处理（本 change 只动 complete/report/list 三个用户可见面）。

人确认：2026-11-19T12:47:54Z
