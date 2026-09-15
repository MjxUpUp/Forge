# 提案：修复 doc-lint 装机接线缺失

## 问题

v1.61.0 验收实测 `forge hook doc-lint` 报 unknown hook——P1-C 只加了
RunHook 分发分支，漏登 isInProcessHook 名册，写时反馈在装机版全死。

## 方案

isInProcessHook 补 doc-lint + 名册完备性守卫测试。

## 验收条件

- 装机二进制 `forge hook doc-lint` 正常分发
- 守卫测试存在且绿（此类漏登记 CI 即红）
