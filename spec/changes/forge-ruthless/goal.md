# goal：forge-ruthless

## Objective

把 Forge 从"十项检查、五份产物、六道门禁"砍到"三道要求"：一份可执行 acceptance（启动时钉住并预跑）、一次按 diff 派生的影响面披露、一个独立评审。删除两天内零真实收益的检查与过程分数。

## Why

JustForMe 两天 1109 条 checklog 的实测：artifact-chain 16/16 假、unused-scan 7/7 假、task-verify 28/28 全过、coverage 13 拦 12 绕、doc-gate 5 次末端打回、self-refresh 5 次自证。验证开销占可计费 token 25–41%，其中约三分之一是噪音或时序错误。

## Constraints

- 只删检查与产物份数，不改 acceptance 语法与 verify 封印语义。
- 老 change（五份产物）必须继续可 verify；新 change 用三份。
- 不新增依赖；命令面变更必须重钉 compat（新增/删除命令都要）。

## Out of Scope

- 不做沙箱/环境编排；不做多 Agent 编排。
- 不改 git 工作流；不引入远程服务。

## Check

- acceptance.md 八条围栏全过；
- 删除项在 `forge --help` 与 compat snapshot 中不再出现；
- `go test ./internal/taskpipeline/ ./internal/clitask/ ./internal/change/` 全绿；
- 新 change 只生成 goal.md / change.md / acceptance.md。
