<a id="top"></a>
<div align="center">

# 🔥 Forge（中文使用指南）

Stop trusting AI-generated code. Start gating it.

</div>

<div align="center">
  <img src="../docs/assets/dashboard-pulse.png" alt="Forge Pulse 全局质量面板" width="860"/>
</div>

> 完整英文文档见根 [README.md](../README.md)。本中文版为国内用户精简说明，覆盖安装 / 日常使用 / 命令参考，命令以代码块为准。

## Forge 是什么

AI 编码的"质量门禁引擎"。在 Claude Code / Codex / Cursor / Copilot 写代码的过程中自动插入结构化门禁——从任务创建到代码提交，每一步产出物都经过验证。配合 Hook 实现实时拦截，你不需要手动检查。

## 安装（推荐路径）

```bash
# 1) 装 binary（机器级，一次性，提供 forge CLI，hooks 都要调它）
npm install -g @agent_forge/forge

# 2) 装 plugin（agent 级，一次性）——在 Claude Code 里跑，接用户级 hooks 到所有项目
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge
```

装完 plugin 后，**每个 git 项目开 Claude Code 都会被 init-suggest SessionStart hook 自动检测**：项目未启用 forge → 首次提示 agent 询问是否 `forge init`（写一次标记不重复）。同意 → agent 自动 init；拒绝 → agent 跑 `forge suggest decline` 永久静默该项目。

> v1.22 起 `forge init` **零项目写入**：不创建 `.forge/`、`CLAUDE.md` 等任何项目文件，只登记全局注册表并把 hooks/协议/skill 写到用户级（`~/.forge/projects/<key>/` 等）。要团队 git 共享协议用 `forge init --project`（团队模式）。

## 想"处处无感"自动 init

```bash
export FORGE_AUTO_INIT=1   # 之后任意 git 项目直接 forge init
```

v1.22 起 `forge init` 零项目写入（只对用户级配置生效，不碰项目目录），自动 init 已不再"污染" clone 来的临时仓库——默认询问模式更多是"要不要启用"的确认，而非文件副作用的顾虑。

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
# AI 工作（19 个 hook 自动守：task-guard / read-before-edit / bash-guard / file-sentinel ...）
forge task gate task-implement    # 门禁1：实现（编译 / 断言 advisory 自检）
forge task gate task-verify       # 门禁2：验证（测试伴随变更）
forge task gate task-complete     # 门禁3：完成确认
forge task complete               # 🏁 任务完结（自动评分 + 清 active ref；git commit 必须在此之前）
forge task score                  # 质量评分
```

`forge update` 检查并更新（npm 安装打印对应包管理器的更新命令而非代下载；GitHub 安装下载自替换）。`forge suggest decline | status | reset` 管理 init-suggest 标记。详见根 README 命令参考表。

## 卸载

```bash
forge uninstall            # 剥除全部用户级 hooks/指令段/forge-quality skill + 清 npm global binary + 删 init-suggest 标记
forge uninstall --restore  # 加 --restore 把用户级文件回滚到 forge 修改前的原始内容（备份在 ~/.forge/backups/）
# 在 Claude Code 里 /plugin uninstall forge@forge   # 卸 plugin（须在 agent CLI 内交互运行）
```

## 多宿主支持

Forge 为 12 个宿主落地接线：Claude Code / Codex / Cursor / Copilot / Kimi Code / Reasonix 走 plugin manifest 分发（`.claude-plugin/`、`.cursor-plugin/`、`.kimi-plugin/`、`plugins/forge/reasonix-plugin.json`）；Windsurf / OpenCode / Cline / CodeBuddy / ZCode 走 `forge init --agents <host>` 用户级接线（CodeBuddy 为 init 生成 Claude 兼容的 plugin pack——其 settings.json 无 hooks 字段；ZCode 写入 `~/.zcode/cli/config.json`，协议层与 Claude Code 兼容，其 plugin 渠道也可回落读 `.claude-plugin/plugin.json`）；DeepSeek Harness (dsh) 走 `dsh plugin --profile web add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"`（npm registry 通道为 `@agent_forge/forge-dsh`，装完重启 `dsh web`）。`plugins/forge/install.sh`（Windows 为 install.ps1）只是 forge 二进制的备用安装器（npm 的 curl-pipe 包装），不做 agent 接线。各宿主差异详见 [plugins/forge/README.md](../plugins/forge/README.md)。

## 常见问题

- **装完 plugin 后项目一直在 task-guard WARN 报"allowed but not tracked"** → 项目未启用 forge（未登记注册表）。跑 `forge init`（零项目写入）或 `forge suggest decline` 静默。
- **`forge` 命令 not found** → npm 全局安装目录不在 PATH。`npm bin -g` 看路径，加入 shell rc。
- **二审 reviewer 反复冒新问题** → `forge task gate task-verify` 含 cheat-scan deterministic 扫描（type-suppression / error-swallow / dead-branch / comment-only-fix / comment-as-debt / phantom-import / path-assumption），机械模式一次判准，LLM-reviewer 退到只做语义判断。

## 与英文版的差异

英文根 README 涵盖：分层定位（Loop Engineering 验证 / 状态）、任务级门禁（实现 → 验证 → 完成）、`verify-acceptance` 验收标准、`--scope` 规划前置白名单、score 评分等。本中文精简版只覆盖国内用户最关心的安装 + 日常 + 多宿主，请参考英文原文获取完整功能索引。
