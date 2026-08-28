# on-demand-guards — 持久决策历史

persistent decision history：每条决策记 (诊断, 修订, 脱敏证据, 结果)，让下一轮 agent 理解「为什么这么改」，避免重复探索已失败方向。审计/可复现，非泛化学习。append-only：新决策追加到末尾。

## [d-18c7718fe9b06088-ed1e2873] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-07-31T17:57:20Z

### Diagnosis

整体审查发现分工表引用 delivery-gate，非本仓库 canonical skill，未显式标注来源

### Revision

显式标注'非本仓库 canonical skill，仅部分 agent 以扩展形式提供'

### Evidence

ls skills/ 无 delivery-gate；原表述'部分 agent 以扩展形式提供'含糊，易误当本库 skill

## [d-18c7e45aa5b5f724-b249e2be] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-02T05:00:55Z

### Diagnosis

审计发现：/freeze 目录锁定的可靠性=agent 每回合记得自检，prompt 型护栏在长会话/压缩后必漂移，恰是它防的场景（机制先天不可靠）；护栏覆盖模式很少（仅 3 条），价值密度偏低

### Revision

按契约 D5 改造为 UX 层：/freeze 主路径改为 forge freeze <path> 激活 / --off 解除 / --status 查看（freeze-guard PreToolUse Write|Edit 真 hook 硬阻断，hook 由 forge 侧并行任务实现）；原 prompt 型护栏降级为「无 forge 环境的 fallback」并保留可靠性上限诚实声明（长会话/压缩后必漂移，能装 forge 就不要依赖 fallback）；/careful 补 hazard-guard 未覆盖模式清单：git clean -fd / npm publish / ssh 生产机 / > 覆盖已有文件；激活状态记忆节区分 forge 持有状态与自检状态；Gotchas/Red Flags 同步更新

### Evidence

docs/skills-value-audit-2026-08-02.md 逐项审计（on-demand-guards 详评 + 改进清单项 12）

## [d-18c7e620be749168-309fe30c] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-02T05:33:25Z

### Diagnosis

项10 description 审计+触发回归

### Revision

description 三段式合格未改动;新建 evals/evals.json(5正+4负)

### Evidence

docs/skills-value-audit-2026-08-02.md

## [d-18c7ef895f7900c8-0c2988b4] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-02T08:25:50Z

### Diagnosis

hazard-guard 的 FORGE_ALLOW_HAZARD=1 env 豁免被 agent 自我放行滥用（周复盘失守 b），且行内前缀形式 hook 进程拿不到 env 行为不一致

### Revision

SKILL.md 移除 FORGE_ALLOW_HAZARD 跳过说明，改为 confirm 链（events.jsonl 审计 + 5min TTL）是唯一放行路径，测试/CI 同样走 forge hazard confirm

### Evidence

internal/hooks/embed.go HazardGuardHook 已删 env 豁免分支；e2e TestHook_HazardGuard_EnvBypassRemoved 与脚本级 TestHazardGuardScript_EnvBypassRemoved 钉死新行为

## [d-18ceed293203ea28-cefa88ca] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-25T03:01:03Z
- **By**: kimi-code

### Diagnosis

两周 usage 日志复盘 hazard-guard 25 次真实拦截：mktemp/自建临时目录清理误报约 1/3（agent 不走 confirm 而是悄悄删掉 rm 半截只跑后半截，留下 /tmp 垃圾，guard 赢了命令输了意图）；合并后例行清分支 9 次；只读 python3 heredoc 分析脚本因文本含 rm 危险串被 substring 误拦 2 次；7 次 confirm 里 5 次是已授权场景的冗余二次确认（confirm 疲劳根因是文案缺授权路径）

### Revision

SKILL.md HITL 段改写为授权判定协议：用户本回合已明确指令/确认过该操作时直接 forge hazard confirm --last 放行、无需二次确认，否则先用所在工具的提问确认机制获确认；工具指代泛化（删 Claude Code→AskUserQuestion 逐工具枚举，漏 kimi/copilot/zcode）；删尾部 FORGE_ALLOW_HAZARD 迁移说明（changelog 非行动指引）。新增「自动豁免」段：rm 递归强删目标全在一次性临时区（/tmp、/var/folders、/private/tmp、$TMPDIR 子路径、本命令串可验证的 mktemp 变量）免 HITL；危险串仅在引号/注释/多行字符串内（数据上下文）不拦，exec 包裹除外。git push --delete 远程不可逆保留拦截，git branch -d 本就不拦

### Evidence

internal/hooks/embed.go HazardGuardHook 实现（safe_mktemp_vars/is_tmp_rm_target/strip_quotes 跨行引号态+嵌套感知/nowl 收紧）；脚本级 TestHazardGuardScript_MktempSelfCleanupExempt 等 5 测试与 e2e TestHook_HazardGuard_MktempSelfCleanupAllowed 等 4 测试钉死（含 rm 根目录/HOME/穿越/再赋值攻击对照仍拦）；旧脚本对照实测 python heredoc docstring 含 rm 清理文本由 BLOCK 转 PASS；全仓 go test 绿

## [d-18d0a2f124514-e124514f4] accept

- **Skill**: on-demand-guards
- **DecidedAt**: 2026-08-28T07:53:14Z
- **By**: zcode

### Diagnosis

正文整节的 forge 命令组（命令语法/机制说明）与通用方法论混排，skills-only 分发用户看到不可执行指令

### Revision

forge 操作细节下沉（新建 references/forge-integration.md 或收进「> Forge 项目」条件块），正文保留机制概述与降级路径

### Evidence

feat/skills-boundary-inversion Phase 2：CONVENTIONS §13 forge 引用契约 + R18 advisory 规则落地；forge skills validate 全语料零 R18 告警

### Rationale

依赖倒置：skill 是独立方法论资产，forge 是可选增强层——skills-only 分发用户不应看到不可执行的 forge 指令
