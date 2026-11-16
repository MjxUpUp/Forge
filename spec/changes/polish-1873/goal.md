# goal: 1.87.3 收尾

## Objective

把 1.87.2 dogfood 暴露的三处措辞/一致性问题修掉：`forge version` 子命令缺失；`task start --change` 的产物链提示与 implement gate 的 change 口径矛盾；`task report` 在没有测试文件的仓库仍写「改动源码均有配对测试」。

## Why

1.87.2 的四项修复已通过 dogfood 验证，但残留提示会让 agent 误判（start 说「产物链未登记」、report 给不实断言），与「交付物可用」的目标冲突。

## Constraints

- 不改门禁判定逻辑，只改呈现与命令面；
- `forge version` 与 `forge --version` 输出同一份版本字符串；
- report 措辞在有无测试文件两种仓库都成立，不引入新的评分输入；
- 走 fast 通道，人已批准（`--by-human`）。

## Out of Scope

- 不改 acceptance/门禁的执行语义；
- 不处理 dogfood 记录的其他噪音（npm shim 提示、构建产物污染工作区）；
- 不改 `task start` 的产物链探针本身。

## Check

- acceptance 五条围栏全过（build、两处新测试、gofmt、deadcode）。
