# 提案+规格：发布链三项验收修复

## 提案（为什么做）

1.61.1 发版实测 npm-verify 失败：传播窗内平台子包 packument 滞后，npm 对
optionalDependencies 静默跳过致主包孤装（误诊为重试窗口过短，日志实证 8×45s
窗口工作正常）；同日双发版时仓内 pins 依赖手工 npm-align 惯例。

## 规格

1. npm-verify 安装改为主包+当前平台子包显式同装（显式依赖缺版本 ETARGET 进退避重试）。
2. npm-verify 尾部新增 pins 自动对齐步（make npm-align + 提交回 main，contents:write）。
3. --critical 空/空白内容报错文案按有无 tag 分流。

## 验收条件

- internal/ci 守护测试全绿（workflow 变更）
- clitask 四个 doc-review 用例全绿（含文案分流断言）
