# Forge — AI 开发质量门禁引擎

> Quality gates for AI-driven development. Stop trusting AI-generated code; start gating it.

## 一分钟简介

Forge 是 AI 编码工作流的"质量层"。在 Claude Code / Codex / Cursor / Copilot 写代码的过程中，它自动插入结构化门禁——从任务创建到代码提交，每一步产出物都经过编译验证、断言守卫、文件监控、review 快照多重检查。不是替代你写代码，是替你的代码把关。

## 三步上手

```bash
# 1. 装 binary（机器级，一次性）
npm install -g @agent_forge/forge

# 2. 装 plugin（agent 级，一次性）—— 在 Claude Code 里执行
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge

# 3. 开任意 git 项目 —— init-suggest hook 首次提示，自动跑 forge init
```

## 核心能力

- **任务级门禁** — 每个开发任务走 3 道门禁（实现 → 验证 → 完成），防跳过
- **19 个实时 Hook** — Write/Edit/Bash/SessionStart/Stop 等事件上拦截代码质量、命令安全性、文件未授权变更
- **cheat-scan 机械检测** — AI 作弊模式（断言弱化、吞错、死分支）一次判准
- **质量评分** — 每个任务完成自动量化 score，沉淀项目级质量基线

## 设计定位

Forge = Loop Engineering 的 **验证 + 状态层**。AI 编码是一个循环：写代码 → 跑 → 读反馈 → 修正 → 再写。**Forge 不替代循环本身**，它补上循环最容易缺的两层：每一轮的产出物是否真可信（验证）、跨轮的状态是否持久可追（状态）。

## 多 agent 支持

| Agent | 安装方式 | 接入 |
|---|---|---|
| **Claude Code** | `/plugin marketplace + install` | 全自动（用户级 hooks + auto-init） |
| **Codex** | plugin marketplace 或 `forge init --agents codex` | 用户级 hooks（`~/.codex/hooks.json` + config.toml 特性开关；可能需在 codex `/hooks` 里 trust 一次）+ guidance |
| **Cursor** | plugin marketplace + `forge init --agents cursor` | 用户级 hooks（`~/.cursor/hooks.json`） |
| **Windsurf** | `forge init --agents windsurf` | 用户级 Cascade hooks（`~/.codeium/windsurf/hooks.json` + `memories/global_rules.md`） |
| **GitHub Copilot (CLI)** | plugin marketplace | plugin 自带 copilot 格式 `hooks.json`，marketplace 装完即接线（PreToolUse/PostToolUse/Stop/SessionStart/UserPromptSubmit）；VS Code 侧未验证；`forge init --agents copilot` 仍是 no-op |
| **Kimi Code** | 仓库根 `.kimi-plugin/plugin.json`（`/plugins install https://github.com/MjxUpUp/Forge`） | 全事件集（含 PostCompact/UserPromptSubmit），exit-2 block 协议；fallback `forge init --agents kimi` |
| **Reasonix** | `plugins/forge/reasonix-plugin.json`（`reasonix plugin install https://github.com/MjxUpUp/Forge/tree/main/plugins/forge`） | native manifest 接线（PreToolUse/PostToolUse/Stop/SessionStart）；fallback `forge init --agents reasonix` |
| **DeepSeek Harness (dsh)** | `dsh plugin --profile web add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"`（npm 通道 `@agent_forge/forge-dsh`；装完重启 `dsh web`） | 类型化拦截点接线（tools/pre-execute、tools/post-execute、agent/pre-step、agent/session-start、agent/turn-stopping），名册镜像 ForgeHookSpec（spec 守卫测试钉死）；会话内 `/forge-status` 查状态 |
| **ZCode (Z.ai)** | `forge init --agents zcode`（plugin 渠道回落读 `.claude-plugin/plugin.json`，未端到端验证） | 用户级 hooks（`~/.zcode/cli/config.json` 合并写入，`hooks.enabled` 强制 true）；协议层刻意 Claude 兼容（蛇形 stdin 别名 + `hookSpecificOutput.additionalContext` + exit-2 阻断）；无 PostCompact/SubagentStop 事件（压缩走 SessionStart `source=compact`）；Stop 连续阻断 3 轮后强制结束；项目级 hooks 不执行（团队共享走 plugin）；协议为文档结论，wire 验证待补 |

v1.22 起 `forge init` 默认**零项目写入**：hooks/协议/skill 全部落在上述用户级位置，项目目录不产生任何文件；团队要 git 共享协议用 `forge init --project`。

其他已接线宿主（OpenCode / Cline / CodeBuddy）走 `forge init --agents <host>` 用户级接线；`plugins/forge/install.sh` / `install.ps1` 只是 forge 二进制的备用安装器（npm 包装），不做 agent 接线。

## 真实证据

- 自举（dogfood）：Forge 用 Forge 自身管住质量，每 PR 过三门禁评分（近期平均 76-100）
- 真实评分 fixture：`scoring/testdata/golden_real/` 固化评分形状防算法隐性漂移
- code-review 确定性：`forge review pass` 绑定代码快照，拒绝改码后空过

## 文档资源

- 主文档：[README.md](../README.md)
- 中文精简：[READMEs/README.zh-CN.md](../READMEs/README.zh-CN.md)
- Plugin 详细：[plugins/forge/README.md](../plugins/forge/README.md)
- 协议：[internal/protocol](../internal/protocol)
- 质量协议 Skill：由 `forge init` 生成到用户级（`~/.claude/skills/forge-quality/` 等；生成器源码在 [internal/skillgen](../internal/skillgen)）

## 许可

[Apache-2.0](../LICENSE)
