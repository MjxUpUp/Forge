# Forge（中文使用指南）

> 完整英文文档见根 [README.md](../README.md)。本中文版为国内用户精简说明，覆盖安装 / 日常使用 / 命令参考，命令以代码块为准。

## Forge 是什么

AI 编码的"质量门禁引擎"。在 Claude Code / Codex / Cursor / Copilot 写代码的过程中自动插入结构化门禁——从任务创建到代码提交，每一步产出物都经过验证。配合 Hook 实现实时拦截，你不需要手动检查。

## 安装（推荐路径）

```bash
# 1) 装 binary（机器级，一次性，提供 forge CLI，hooks/MCP 都要调它）
npm install -g @agent_forge/forge

# 2) 装 plugin（agent 级，一次性）——在 Claude Code 里跑，接用户级 hooks+MCP 到所有项目
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge
```

装完 plugin 后，**每个 git 项目开 Claude Code 都会被 init-suggest SessionStart hook 自动检测**：无 `.forge/` → 首次提示 agent 询问是否 `forge init`（写一次标记不重复）。同意 → agent 自动 init；拒绝 → agent 跑 `forge suggest decline` 永久静默该项目。

## 想"处处无感"自动 init

```bash
export FORGE_AUTO_INIT=1   # 之后任意 git 项目直接 forge init
```

代价：会对你打开的**每个** git 项目写入 `.forge/`、`CLAUDE.md`/`AGENTS.md`、`.claude/settings.local.json`、skills——包括你 clone 来的临时仓库。所以默认是询问模式。

## 不装 plugin 的最低用法

也可以不装 plugin，纯手 init：

```bash
cd your-project
forge init
```

但这样每次进新 git 项目都要手动 init，丢了 init-suggest 的自动提示。

## 日常使用（任务门禁）

```bash
forge task start --ref feat/xxx --branch --title "描述"   # 建任务 + 分支
# AI 工作（8 个 hook 自动守：task-guard / assertion-check / bash-guard / file-sentinel ...）
forge task gate task-implement    # 门禁1：实现（编译 / 断言 advisory 自检）
forge task gate task-verify       # 门禁2：验证（测试伴随变更）
# git commit 必须在 task-complete 之前
forge task gate task-complete     # 门禁3：完成
forge task score                  # 质量评分
```

`forge update` 自更新 binary。`forge suggest decline | status | reset` 管理 init-suggest 标记。详见根 README 命令参考表。

## 卸载

```bash
npm uninstall -g @agent_forge/forge   # 卸 binary
# 在 Claude Code 里 /plugin uninstall forge@forge   # 卸 plugin
# 用户级 init-suggested 标记（默认 ~/.forge/.init-suggested/，设 FORGE_DATA_HOME 时落该根下）由 `forge suggest reset` 或 rm -rf 清理
```

（更彻底的 `forge uninstall` 子命令计划中，见 Forge 路线图。）

## 多宿主支持

Forge 已为 Claude Code / Codex / Cursor / Copilot / Windsurf 落地分发（`.claude-plugin/`、`.copilot-plugin/`、`.cursor-plugin/` 多宿主元数据）。其他 agent（OpenCode / Pi / Kiro / Cline / Gemini CLI / Mistral Vibe 等）由 `plugins/forge/install.sh` 一站式安装脚本支持（仿 [Understand-Anything](https://github.com/Egonex-AI/Understand-Anything) 的 per-skill / folder 双 style symlink 机制）。

## 常见问题

- **装完 plugin 后项目一直在 task-guard WARN 报"allowed but not tracked"** → 项目无 `.forge/`。跑 `forge init` 或 `forge suggest decline` 静默。
- **`forge` 命令 not found** → npm 全局安装目录不在 PATH。`npm bin -g` 看路径，加入 shell rc。
- **二审 reviewer 反复冒新问题** → `forge task gate task-verify` 含 cheat-scan deterministic 扫描（type-suppression / error-swallow / dead-branch / comment-only-fix），机械模式一次判准，LLM-reviewer 退到只做语义判断。

## 与英文版的差异

英文根 README 涵盖：分层定位（Loop Engineering 验证 / 状态 / 学习层）、任务级门禁（实现 → 验证 → 完成）、`verify-acceptance` 验收标准、`--scope` 规划前置白名单、score 评分等。本中文精简版只覆盖国内用户最关心的安装 + 日常 + 多宿主，请参考英文原文获取完整功能索引。
