# secure-coding — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77221f5a50834-b58ffb2e] accept

- **Skill**: secure-coding
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现语义错标：forge skills audit --skill=secure-coding 被误标为安全 checklist（audit 只审 skill 文件规范）；声称 forge review pass 含 security checklist、code-review-gate 带 OWASP 子检查均不属实（review pass 只是人工审查标记，同文件 §7 自己写将来时应集成）；references/ 目录不存在

### Revision

删除提交前必跑中的 audit 行；§4 自查与 §6 中 review pass 表述改为如实：仅标记人工审查完成，OWASP 检查按 §4 逐项人工核对；删除 references/ 占位句

### Evidence

grep internal/cli 确认 review pass 仅为人工审查标记命令；code-review-gate skill 内容无 OWASP 子检查
