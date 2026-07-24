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
- **多个实时 Hook** — Write/Edit/Bash/SessionStart 拦截代码质量、命令安全性、文件未授权变更
- **cheat-scan 机械检测** — AI 作弊模式（断言弱化、吞错、死分支）一次判准
- **质量评分** — 每个任务完成自动量化 score，沉淀项目级质量基线

## 设计定位

Forge = Loop Engineering 的 **验证 + 状态层**。AI 编码是一个循环：写代码 → 跑 → 读反馈 → 修正 → 再写。**Forge 不替代循环本身**，它补上循环最容易缺的两层：每一轮的产出物是否真可信（验证）、跨轮的状态是否持久可追（状态）。

## 多 agent 支持

| Agent | 安装方式 | 接入 |
|---|---|---|
| **Claude Code** | `/plugin marketplace + install` | 全自动（hooks + auto-init） |
| **Codex** | plugin marketplace 或 `forge init --agents codex` | hooks + guidance |
| **Cursor** | plugin marketplace + `forge init --agents cursor` | skills |
| **GitHub Copilot** | plugin marketplace + `forge init --agents copilot` | `.github/instructions` |
| **Windsurf** | `.windsurf/hooks.json`（forge init 自动生成） | Cascade hooks |

其他 agent（OpenCode / Pi / Kiro / Cline / Gemini CLI / Mistral Vibe 等）由 `plugins/forge/install.sh` 兜底安装（仿 [Understand-Anything](https://github.com/Egonex-AI/Understand-Anything) 的 curl-pipe 单脚本）。

## 真实证据

- 自举（dogfood）：Forge 用 Forge 自身管住质量，每 PR 过三门禁评分（近期平均 76-100）
- 真实评分 fixture：`scoring/testdata/golden_real/` 固化评分形状防算法隐性漂移
- code-review 确定性：`forge review pass` 绑定代码快照，拒绝改码后空过

## 文档资源

- 主文档：[README.md](../README.md)
- 中文精简：[READMEs/README.zh-CN.md](../READMEs/README.zh-CN.md)
- Plugin 详细：[plugins/forge/README.md](../plugins/forge/README.md)
- 协议：[internal/protocol](../internal/protocol)
- 质量协议 Skill：[skills/forge-quality](../skills/forge-quality)

## 许可

[MIT](../LICENSE)
