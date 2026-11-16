# complete-parity：gate 与 complete 同口径

## Objective

把 `forge task gate task-complete` 与 `forge task complete` 的前置检查对齐（缺 L2 文档回检时 gate 也 BLOCKED），并在交付单里披露文档回检（L2 分数/轮次/评审者），让 1.87.1 dogfood 的「照 next 走到最后一步被打回」不再发生。

## Why

1.87.1 dogfood（子代理真实跑任务）报告：改了 `.md`、门禁全过、只是没记 doc-review 时，`gate task-complete` 打「✅ passed / → next: complete」，`task complete` 才报「doc gate 未通过」——照 next 走的人必然在最后一步被打回，代价是整轮重跑 + 重录。

## Constraints

- 不改 acceptance 语法、不改 review pass / doc-review 的记录格式。
- gate 只复用 `CheckDocGate`（同一真相源），不新造一套判定。
- 空期望与子串匹配是既有语义，本次只补文档披露与声明期提示，不改匹配算法。

## Out of Scope

- 不强制校验评审者独立性（宿主无法证明），本次只做披露。
- 不改评分模型与 evidence 强度规则。

## Check

acceptance #1–#5。
