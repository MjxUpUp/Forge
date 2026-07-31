# system-architecture — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221ed7ca0f4-40639dc1] accept

- **Skill**: system-architecture
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与语义错标：forge auto-build 不存在；forge skills audit 被误标为架构评审（audit 只审 skill 文件规范+安全）；forge skills validate --skill=c4-model 指向不存在的 skill；references/ 目录不存在

### Revision

提交前必跑：auto-build 替换为 go build ./...；删除 audit 行与 c4-model validate 行；删除 references/ 占位句

### Evidence

grep internal/cli 确认无 auto-build；ls skills/ 确认无 c4-model；skills_audit.go 确认 audit 语义为 skill 文件审查
