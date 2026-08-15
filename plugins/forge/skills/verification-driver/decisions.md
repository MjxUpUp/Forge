# verification-driver — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c613d20c3c8a40-8abccdc4] accept

- **Skill**: verification-driver
- **DecidedAt**: 2026-07-27T07:08:15Z
- **By**: claude-code

### Diagnosis

接入通用 skill-trigger 框架(feat/skill-trigger): 声明式 metadata.triggers 让通用 hook 在事件点主动驱动本 skill, 解决 dogfood 量化的质量/流程 skill 显式触发=0、靠 agent 自觉必漏问题(code-review-gate 因有 review-stop hook 独活)

### Revision

metadata.triggers 加 [{"event":"Stop","when":"task_active_no_review"}]

### Evidence

dogfood-findings-2026-07-09(testing×17 全低分, 质量 skill 0 显式触发) + plan flickering-bubbling-bonbon.md(triggers schema 表)

## [d-18c7e5a668e8f0c4-7d76e755] accept

- **Skill**: verification-driver
- **DecidedAt**: 2026-08-02T05:24:40Z

### Diagnosis

skills 库价值审计 13 项改进落地

### Revision

单测≠验证完成单源化改指针到 test-discipline 铁律2；RemoteAddr 案例改指针

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项价值审计

## [d-18c7e622a5f80adc-c7475846] accept

- **Skill**: verification-driver
- **DecidedAt**: 2026-08-02T05:33:33Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 审计合格未改动 + 新建 evals.json 10 条

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-9a3f1c02d4e8b670-2f5a8e1c] accept

- **Skill**: verification-driver
- **DecidedAt**: 2026-08-05T10:20:00Z
- **By**: claude-code

### Diagnosis

metadata.triggers 的 `task_active_no_review` 是语义错配：该 condition 判定的是 `!state.ReviewPassed`（code-review-gate 的审查通过标记，见 conditions.go condTaskActiveNoReview），而 code-review-gate 本身被 skilltrigger.DeniedSkills 排除（由 review-stop hook 专属驱动）——故该 condition 实际无合法消费者。本 skill 是端到端验证（curl/docker/CLI 实测），绑这个 code-review 语义的 condition 造成两类伤害：主 agent 按 code-review-gate 派子 agent 审查代码、等待结果时触发 Stop，Stop 命中该 condition 后注入「端到端验证」提醒——既把审查中的 `ReviewPassed=false` 误判成「没审查」（审查进行中悖论），又让正在做 code-review 的 agent 去做语义无关的 curl/docker。

### Revision

移除 metadata.triggers（原 `[{"event":"Stop","when":"task_active_no_review"}]`）。verification-driver 退化为靠路由/引用加载（与 test-discipline 等 advisory skill 一致）。task 模式的审查强制力已由 task-complete 门禁的 ReviewPassed 硬前置兜底，非 task 模式由 review-stop hook 兜底（task 模式该 hook 直接 PASS 放行），Stop 阶段无须再注入 skill。task_active_no_review condition 词汇本身保留（合法词汇，留待未来 code-review 类 skill 合法消费）。

### Evidence

conditions.go:153 condTaskActiveNoReview 判 !state.ReviewPassed；trigger.go:70 DeniedSkills 排除 code-review-gate；internal/cli/review.go:228-248 task 模式 review gate 实施 PASS 放行（脚本注释见 internal/hooks/embed.go:299）；用户 2026-08-05 报告 Stop 误触发（派审查子 agent 等待时被注入端到端验证提醒）。回归测试 skills/embed_test.go TestNoSKILLMDBindsTaskActiveNoReview 固化「SKILL.md 不绑该 condition」。

## [d-18ca0700f5f7ec40-2e905647] accept

- **Skill**: verification-driver
- **DecidedAt**: 2026-08-09T03:58:22Z
- **By**: claude-code

### Diagnosis

该 skill 无声明式 trigger，纯靠 agent 自觉加载——dogfood transcript 证明 0 命中，skill 形同被动文档从未注入

### Revision

在 SKILL.md frontmatter metadata 加 triggers 声明（事件 + keywords 或 when condition + cooldown），让 skill-trigger 框架在匹配事件时主动注入加载指引

### Evidence

forge skills validate R1-R17 全 49 通过；trigger 覆盖 5→15（31%）；dry-run 验证 research-workflow/secure-coding 匹配 prompt 正确触发

### Rationale

扩展 trigger 覆盖是 2026-08 审计 P1 优化项；声明式触发是把 skill 从被动文档转主动注入的唯一可靠手段（见 dogfood 发现）
