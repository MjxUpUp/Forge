# maintainability-and-readability — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c5ec5c22cd6e98-c9b99947] accept

- **Skill**: maintainability-and-readability
- **DecidedAt**: 2026-07-26T19:05:07Z
- **By**: claude-code

### Diagnosis

注释卫生任务要求 Go godoc 双语并存（英文段+中文段），skill 此前缺注释规范条款，agent 无可循标准

### Revision

SKILL.md 新增 §2.7 注释规范：形式 A（英文文档段→空//行→中文段），中文不删不单语；同步铁律（行为变更同步生成器）；范围限定（独立 // 注释双语，行内尾注与字符串字面量不动）

### Evidence

全量双语化 352 个 Go 文件，自检 c1.awk（未双语化 CJK 块计数）=0，c2.awk（重复英文段）空，go build exit 0

### Rationale

注释双语是本任务核心交付，规范须沉淀进 skill 避免下轮 agent 重复探索

## [d-18c77221db2f9190-46f3045b] accept

- **Skill**: maintainability-and-readability
- **DecidedAt**: 2026-07-31T18:07:47Z

### Diagnosis

整体审查发现幻觉命令与幽灵指针：forge auto-build 不存在（internal/cli 无此命令），forge skills validate 被误当复杂度检查，§4.6 与 references/ 不存在，§2 标题节数写错（6 实为 7）

### Revision

auto-build 替换为 go build/vet 真实编译命令；提交前必跑删除 validate 行（保留 lizard）；删除 references/ 占位句；§4.6 改指 §4.4 流程类；标题改为 7 路径规范

### Evidence

grep internal/cli 确认 Use 列表无 auto-build；SKILL.md 全文标题枚举确认 §2.1–2.7 共 7 节、§4 仅 4.1–4.4
