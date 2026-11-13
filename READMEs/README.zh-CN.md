<a id="top"></a>
<div align="center">

# 🔥 Forge（中文使用指南）

Stop trusting AI-generated code. Start gating it.

</div>

<div align="center">
  <img src="../docs/assets/forge-demo.webp" alt="Forge 30 秒演示（自动播放，静音）" width="860"/>
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


装完 plugin 后，Claude Code 的 hooks 由 plugin 全机器接管；其他宿主用 `forge integrations enable <host>` 显式接线。**接不接管某个仓库仍由人决定**：在仓库里跑 `forge init` 声明契约，没有 init 的仓库不会被后台自动接管（原 init-suggest 自动探测/接管已移除）。`forge off` 仍是每项目退出，`forge on` 恢复。

>  `forge init` **只写仓库内产物**：`spec/forge.yaml`（`hooks: off` 缺省）、`spec/changes/`、AGENTS.md 托管块，并按默认规则维护已存在的 `CLAUDE.md` / `.cursor/rules/forge.mdc` 指针。它**不写** `~/.forge`、`~/.claude`、`~/.codex`。用户级接线走 `forge integrations enable <host>`；团队要 git 共享同一份协议，提交 `spec/` 与 `AGENTS.md` 即可。

## 宿主接线（显式 opt-in）

```bash
forge integrations enable claude-code        # 或 codex / cursor / copilot / kimi / windsurf / cline / opencode / reasonix / codebuddy / dsh / zcode
forge integrations enable --all              # 一次接全部宿主
forge integrations list                      # 看哪些已接线
forge integrations disable <host>            # 摘标记（不删宿主配置）
```

`enable` 才写用户级 hooks/指令/skill、登记注册表并打 `~/.forge/integrations/<host>` 标记；没有任何标记且未装 Claude plugin 时，forge 不会在后台写用户级资产（Phase C 之前已接过线的老项目除外：它们继续由 autoSync 迁移维护）。

## 不装 plugin 的最低用法

```bash
cd your-project
forge init                                    # 声明仓库契约（只写仓库内文件）
forge integrations enable claude-code         # 需要 hook 实时拦截时接线
```

## 60 秒拿到第一个证据

```bash
cd your-project && forge init
forge task start --ref feat/demo --branch --accept "git log -1 :: commit"
forge task verify-acceptance    # 实跑验收标准 → 不可伪造的证据
forge trace feat/demo           # 查看证据链
```

`forge eval wedge-drill` 可在任何机器自检这条路径（隔离环境）。

## 日常使用（任务门禁）

```bash
forge task start --ref feat/xxx --branch --title "描述"   # 建任务 + 分支
# AI 工作（26 个 hook 自动守：task-guard / read-before-edit / bash-guard / file-sentinel ...）
forge task gate task-implement    # 门禁1：实现（编译 / 断言 advisory 自检）
forge task gate task-verify       # 门禁2：验证（测试伴随变更）
forge task gate task-complete     # 门禁3：完成确认
forge task complete               # 🏁 任务完结（自动评分 + 清 active ref；git commit 必须在此之前）
forge task score                  # 质量评分
```


`forge update` 检查并更新（npm 安装打印对应包管理器的更新命令而非代下载；GitHub 安装下载自替换）。`forge off [--all] | on` 按项目退出/恢复接管；`forge config get/set compat` 管兼容基线；`forge policy state|yield` 快查状态/外来 harness 让位；`forge integrations list/enable/disable` 显式接线宿主。详见[完整命令参考](../docs/reference/cli.md)。

## 卸载

```bash
forge uninstall            # 剥除全部用户级 hooks/指令段/forge-quality skill + 清 npm global binary
forge uninstall --restore  # 加 --restore 把用户级文件回滚到 forge 修改前的原始内容（备份在 ~/.forge/backups/）
# 在 Claude Code 里 /plugin uninstall forge@forge   # 卸 plugin（须在 agent CLI 内交互运行）
```

## 多宿主支持

Forge 为 12 个宿主落地接线：Claude Code / Codex / Cursor / Copilot / Kimi Code / Reasonix 走 plugin manifest 分发（`.claude-plugin/`、`.cursor-plugin/`、`.kimi-plugin/`、`plugins/forge/reasonix-plugin.json`）；Windsurf / OpenCode / Cline / CodeBuddy / ZCode 走 `forge integrations enable <host>` 用户级接线（CodeBuddy 由 `forge integrations enable codebuddy` 生成 Claude 兼容的 plugin pack——其 settings.json 无 hooks 字段；ZCode 写入 `~/.zcode/cli/config.json`，协议层与 Claude Code 兼容，其 plugin 渠道也可回落读 `.claude-plugin/plugin.json`）；DeepSeek Harness (dsh) 走 `dsh plugin --profile <profile> add "github:MjxUpUp/Forge#main&path:/plugins/forge-dsh"`（npm registry 通道为 `@agent_forge/forge-dsh`；桌面 App 跑的是 `desktop` profile，装到别的 profile 不会生效；插件自带 forge 本体，装完无需另装 CLI，需要时重启对应宿主）。`plugins/forge/install.sh`（Windows 为 install.ps1）只是 forge 二进制的备用安装器（npm 的 curl-pipe 包装），不做 agent 接线。各宿主差异详见 [plugins/forge/README.md](../plugins/forge/README.md)。

## 常见问题

- **装完 plugin 后项目一直在 task-guard WARN 报"allowed but not tracked"** → 项目未启用 forge（未登记注册表）。跑 `forge init` 声明契约，或 `forge off` 退出该项目。
- **`forge` 命令 not found** → npm 全局安装目录不在 PATH。`npm bin -g` 看路径，加入 shell rc。
- **二审 reviewer 反复冒新问题** → `forge task gate task-verify` 含 cheat-scan deterministic 扫描（type-suppression / error-swallow / dead-branch / comment-only-fix / comment-as-debt / phantom-import / path-assumption），机械模式一次判准，LLM-reviewer 退到只做语义判断。

## 与英文版的差异

英文根 README 涵盖：分层定位（Loop Engineering 验证 / 状态）、任务级门禁（实现 → 验证 → 完成）、`verify-acceptance` 验收标准、score 评分等；`--scope` 规划前置白名单等逐条 flag 语义见[完整命令参考](../docs/reference/cli.md)。本中文精简版只覆盖国内用户最关心的安装 + 日常 + 多宿主，请参考英文原文获取完整功能索引。
