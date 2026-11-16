# proposal

## 问题

1.87.1 dogfood（/c/tmp/dogfood-1871，真实两段任务）在流程可用性上留下四处摩擦：

1. `gate task-complete` 与 `task complete` 前置不一致（缺 doc-review 时 gate 绿灯、complete 才拦），照 next 走的人被打回。
2. 交付单不披露 doc-review 的分数/轮次/评审者，验收方无法据此决定抽查深度。
3. 已钉 change 的任务仍提示「产物链 proposal/spec/design/plan 缺失」，诱导重复造 task 级产物。
4. `--accept` 的 expected 是子串匹配、空期望只校验退出码，文档与声明期都没有说明。

## 解决

四项均按最小改动修：gate 复用 `CheckDocGate`；report 增加一行「文档回检」；产物链提醒在 `GoalContract != nil` 时改为「以 change 产物为准」；task start 对空期望提示一次并在 cli.md / agents-protocol 写明子串语义。

## 不做

不引入 plan digest、不改 fragment 规划、不动评分与逃生舱语义。
