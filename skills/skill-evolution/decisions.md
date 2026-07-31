# skill-evolution — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c77b5a8e2e5f9c-0746e93b] accept

- **Skill**: skill-evolution
- **DecidedAt**: 2026-07-31T20:56:46Z

### Diagnosis

behavior-probe 维度全库只有 1 个消费方（code-review-gate/probes.yaml），维护成本大于回归价值，决策拆除不推广

### Revision

SKILL.md 核心循环从 5 步（含 probe 步）改为 4 步：删除 probes.yaml 写法、C 组件权限分离纪律、behavior pass-rate 验收标准；回归信号收敛到 trigger/not-trigger eval-report

### Evidence

拆除后 go build ./... 通过，go test ./internal/skillseval/ ./internal/cli/ 全绿；全库 grep ProbeInput|judgeBehavior|probes.yaml 零残留
