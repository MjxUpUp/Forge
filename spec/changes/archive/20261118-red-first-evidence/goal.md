# goal：red-first-evidence

## Objective

让「判据先于实现、失败先于通过、判据不由实现者现场发明」三件事变成 forge 能核对的数据事实，并让同一任务的评审角色跨轮复用同一个子 agent（承接上一轮未闭合项），从而避免「每轮换新评审者、每轮都能扫出新问题」的不收敛。

## Out of Scope

不做 TDD 硬门禁（不要求实现前必须写测试文件，也不猜 git 提交顺序）；不恢复已删的 artifact-chain / doc-review / task-verify / coverage 门禁；不引入节点图；不改两道门禁的数量；机器只证明「红过」，不证明「红的判据有意义」——后者仍归评审。

## Check

见 acceptance.md：#1 构建、#2/#3 单元判据、#4 行为脚本、#5 文档、#6 格式化、#7 死代码。