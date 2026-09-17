# 提案+规格：发布链三项验收修复

> 状态：已实施（随 db391d3 合入 main；实跑证据见文末验收）

## 提案（为什么做）

1.61.1 发版实测 npm-verify 失败：传播窗内平台子包 packument（registry 元数据
文档）滞后，npm 对 optionalDependencies 静默跳过致主包孤装——推翻「重试窗口
过短」的原始误诊（run 34986576299 日志实证：installed on attempt 8/8 +
added 1 package，退避窗工作正常、孤装才是死因；窗定义见
.github/workflows/release.yml npm-verify job——8 次尝试 + 7×45s 退避间隔）；同日双发版时仓内 pins 依赖
手工 npm-align，实锤 pins 落在旧版；--critical 空内容报错文案不分场景——
裸空串提示「去掉 tag 前缀」纯属误导（真裸空串与「style: 打标后为空」是两种修法）。

## 规格

1. npm-verify 安装改为主包+当前平台子包显式同装：显式依赖缺版本报 ETARGET
   （npm 缺版本错误码）进退避重试，不再被 optional 语义吞掉（release.yml
   npm-verify 安装步）。
2. npm-verify 尾部新增 pins 自动对齐步：make npm-align + 开
   chore/npm-align-<ver> PR 由人合并（job 级 contents:write 供推分支 +
   pull-requests:write 供开 PR；对齐只动版本清单）。
3. --critical 空/空白内容报错文案按有无 tag 分流（internal/clitask/task_misc.go）：
   打标后空 →「tag 后补上发现内容」；裸空串 →「写清要修什么（发现必须有内容）」。

## 验收条件

- internal/ci/release_workflow_test.go 全绿：needs 链 / npm-verify 装回断言 /
  permissions 键位（TestReleaseWorkflow_NoStepLevelPermissions 封 2026-09-15
  step 级非法键实录盲区；对齐步本体为 workflow 结构，无独立 CI 守卫）
- internal/clitask/task_docreview_coreview_test.go 四个 TestDocReview_* 全绿
  （TestDocReview_CriticalEmptyContentAfterTag 断言文案分流）
- 实跑：1.61.3 发版后自动对齐步产出 2a1bba8（forge-release-bot 署名，
  chore/npm-align-1.61.3 分支）并经 PR #69 人审合并——全路径首跑实证；
  1.62.0 run 命中「已对齐」no-op 分支，因 07e862d 人工对齐（13:04:57Z）
  早于 run 启动（13:05:19Z）22 秒——该提交系人工，不得作为本步证据
