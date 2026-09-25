# 提案：修复 doc-lint 装机接线缺失

> 状态：已实施 v1.61.1（a64cb6c，随 d1272c3 合入 main）

## 问题

v1.61.0 验收实测 `forge hook doc-lint` 报 unknown hook——P1-C（docs/design/
output-readability-gates-v2.md）只加了 RunHook 分发分支，漏登 isInProcessHook
名册，写时反馈在装机版全死（CHANGELOG 1.61.1 / a64cb6c）。

## 方案

isInProcessHook 补 doc-lint + 名册完备性守卫测试。

## 验收（实测）

- 装机二进制 `forge hook doc-lint` 正常分发、不再报 unknown hook
  （v1.61.1 验收实测；2026-09-17 本机复跑 exit 0，对照未知 hook 名
  exit 1 + stderr「unknown hook: …」）
- 守卫测试 TestHookWiring_EveryNameResolvable（internal/hookdispatch/
  hook_wiring_test.go）绿——此类漏登记 CI 即红（2026-09-17 本机复跑 PASS）
