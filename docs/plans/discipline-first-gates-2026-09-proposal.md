# Proposal：纪律第一防线——门禁时间再分布（2026-09-17）

## 问题（证据）

remin 项目单会话（6 任务）checklog 审计：115 次门禁触发中 83% 挤在
task-start/verify/complete ±60s；m0 编码 26 分钟门禁零反馈，verify 时 12+ 门禁
15 秒内倾泻，报 8 文件缺测试后花 31 分钟补救性补测。test-nudge 从事发第 1 分钟
就持有该信息，但以事件计数 + 一次性平级 advisory 的形态被无视 19 次。

## 决策

1. 方向：**纪律第一防线、门禁最终防线**——门禁拦截率应随纪律采纳单调下降，
   且全程可度量、防 gaming。
2. 路径：**调整为主、新增为辅**。已有资产（nudge/artifact-chain/plan-first/
   markers/节流）重定时重分级；仅新增度量字段（P1-A）与镜像入口（P2，后置）。
3. 排序：先加眼睛（discovery rate 度量）→ 再调时机 → 数据说话后才动新执法。

被否方案：全量 TDD 硬门禁（git 证不了编写顺序，逼出后补测试）；加更多信号
（19 条同类 nudge 已被无视，加量=加重警报疲劳）。

## 分期与验收

见 spec：[discipline-first-gates-2026-09.md](discipline-first-gates-2026-09.md)
（P1 范围：checklog outcome 字段 + test-nudge 文件级跨档升级；P2-P5 按回测数据
决定立项）。
