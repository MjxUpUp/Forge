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

## [d-18c7e4485cb9af14-cf386f0b] accept

- **Skill**: skill-evolution
- **DecidedAt**: 2026-08-02T04:59:36Z

### Diagnosis

审计发现：与 skill-authoring-standard 零互链——revise 步改 SKILL.md 却不指向创作规范，accept 标准只看 eval pass-rate 不看规范合规；与 session-retrospective 衔接单向缺失；无体检项时留痕机制形同虚设

### Revision

revise 步加「改动须过 skill-authoring-standard 验证（新建/修改后）节清单」指针（按 skill 名+节标题引用，不引行号）；新增「与其他 skill 的衔接」节补 skill-authoring-standard 与 session-retrospective 双向互链（对侧 session-retrospective 分工段由并行批次补齐）；新增「体检：留痕覆盖率」节——每 30 天核对 git log 改动与 decisions.md 留痕是否配对，有改动无留痕须补记

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计（skill-evolution 详评 + 改进清单项 4）

## [d-18c7e609c6094110-17cfd06f] accept

- **Skill**: skill-evolution
- **DecidedAt**: 2026-08-02T05:31:46Z

### Diagnosis

项11 evals 机制库内化：trigger_cases 编写纪律（near-miss 负例、正负配比、改 description 必自查）此前只存在于审计文档，未沉淀进 skill

### Revision

SKILL.md 新增「触发回归（evals）」节：evals.json schema（R17）、5正4-5负配比、near-miss 负例从兄弟 skill 撞车点出、改 description 后必过 trigger_cases

### Evidence

docs/skills-value-audit-2026-08-02.md；业界调研 grafana/skills 四维 rubric；R17 schema 定义见 internal/skillsqa/rules.go

## [d-18c7e62b438b2e20-8df5b0b2] accept

- **Skill**: skill-evolution
- **DecidedAt**: 2026-08-02T05:34:10Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description SKIP 边界补全 + evals.json 建立

### Evidence

docs/skills-value-audit-2026-08-02.md
