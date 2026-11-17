# proposal：forge-ruthless

## 问题

两天实测显示，门禁体系里有一半的检查没有产生过真实修复：

| 检查 | 实测 | 结论 |
| --- | --- | --- |
| task-verify | 28 次全过、0 失败 | 无信息量 |
| doc-review / doc gate | 19 过 / 5 次末端打回，2–4 轮评审 | 时序错、成本高 |
| mutation gate | 两天 2 个任务，1 次空转拦截 | 投入产出倒挂 |
| artifact-chain | 16 次全 advisory false | 100% 噪音 |
| unused-scan | 7 次全 false | 噪音 |
| test-coverage-gate | 13 拦 / 12 次 env 绕过 | 已被绕过 |
| plan-first | 1 次失败 | goal 契约已覆盖 |
| Score 0–100 | 79–89 由 override 次数决定 | 度量过程税 |

## 提案

1. 删除上表全部检查与 `task-verify` 门禁；doc-review 降为 `docs lint` + change.md 的"文档影响"一行。
2. change 产物三份化：goal.md / change.md / acceptance.md；tasks.md 可选。
3. 新增 `forge impact`：按 diff 派生"文档影响 / 源码无测试"两条披露要求，由 complete 检查、评审核对；不阻断执行。
4. `task start` 预跑 acceptance（允许红，记录基线）。
5. 对外只报"直接合 / 需返工 + 逃生舱披露"，不再打印 0–100 分。

## 影响面

- 命令面：删除 `task doc-review`、`task mutation`；新增 `impact`。
- 产物：New/NewFast 改为三份；goal 解析保留五段兼容，新模板三段。
- 评分：scoring 保留内部计算，report/complete 不再展示 Score。
