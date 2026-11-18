# goal：backlog-closeout

## Objective

清空 2.0.4 之后剩余的遗留工单：红快照只能由开工当场拍下（禁止事后补拍）、评审者不能是实现者本人、幻影路径不再进变更集、入口与 verify/gate 对 clarify 同口径。完成后 open 工单为 0，且每个修复都有可执行反例。

## Out of Scope

不引入新的门禁实例（复用 task start / task complete / review pass 三条已有路径）；不改红先绿的声明式语义（无红声明仍然放行，只披露）；不做子 agent 调度（Forge 只记录身份与连续性）。

## Check

验收见 acceptance.md 的 8 条围栏（build / clitask 与 tasktypes / cli 与 scoring / 文档 / gofmt / deadcode / 守卫 / 红先绿 e2e）。