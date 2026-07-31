# database-design — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221fdcfa898-ac7d3e50] accept

- **Skill**: database-design
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与高危假命令：forge skills eval 不存在（只有 eval-gen/cases/record/report/baseline）；forge integration-test 不存在；psql -f up.sql --dry-run 是假 dry-run——psql 无 --dry-run，-f 会真实执行 migration 可能毁数据；references/ 目录不存在

### Revision

eval 改为真实命令 forge skills eval-cases --skill database-design；删除 integration-test 行（perf 回归改指 §2.4 慢查询流程）；psql 假 dry-run 改为 BEGIN/\i up.sql/ROLLBACK 事务包裹演练并加 -f 无 dry-run 警告；删除 references/ 占位句

### Evidence

grep internal/cli 确认无 eval/integration-test；psql 官方文档无 --dry-run 选项
