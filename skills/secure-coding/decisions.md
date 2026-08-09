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

## [d-18c7e49911141c10-36d88ba1] accept

- **Skill**: secure-coding
- **DecidedAt**: 2026-08-02T05:05:23Z

### Diagnosis

审计判决'改进（更新基线+补 AI 时代盲区）'：全文按 OWASP Top 10 2021 组织而 2025 已正式发布（A03 变 Software Supply Chain Failures、SSRF 并入 A01、新增 A10 Mishandling of Exceptional Conditions），§9 映射表整体过时；作为 AI agent 质量保障项目分发的安全 skill 零提及 prompt injection/MCP 安全/skill 供应链投毒；CVE triage SLA 对小项目不现实；§6 默认已装 snyk/semgrep/gitleaks 无降级路径

### Revision

§9 按官方 OWASP Top 10:2025 重映射（已核实 owasp.org 正式版清单）；新增 §2.7 Agent/LLM 特有威胁（prompt injection/MCP 工具安全/skill 供应链投毒，引 Snyk ToxicSkills 3984 skill 36.8% 缺陷 76 恶意数据）；§2.1/§2.2/§2.3 类目标注同步 2025；§2.6 SLA 加适用条件；§6 工具命令标注'若已安装'并给手工核对路径；参考链接换 2025 + LLM Top 10

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计；owasp.org/Top10/2025 官方清单已 FetchURL 核实

## [d-18c7e622e89c9934-c4d11852] accept

- **Skill**: secure-coding
- **DecidedAt**: 2026-08-02T05:33:34Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18ca0700ff5f3f40-ca4c7773] accept

- **Skill**: secure-coding
- **DecidedAt**: 2026-08-09T03:58:23Z
- **By**: claude-code

### Diagnosis

该 skill 无声明式 trigger，纯靠 agent 自觉加载——dogfood transcript 证明 0 命中，skill 形同被动文档从未注入

### Revision

在 SKILL.md frontmatter metadata 加 triggers 声明（事件 + keywords 或 when condition + cooldown），让 skill-trigger 框架在匹配事件时主动注入加载指引

### Evidence

forge skills validate R1-R17 全 49 通过；trigger 覆盖 5→15（31%）；dry-run 验证 research-workflow/secure-coding 匹配 prompt 正确触发

### Rationale

扩展 trigger 覆盖是 2026-08 审计 P1 优化项；声明式触发是把 skill 从被动文档转主动注入的唯一可靠手段（见 dogfood 发现）
