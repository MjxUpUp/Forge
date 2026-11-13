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

AI 编码的"质量门禁引擎"。在 Claude Code / Codex / Cursor / Copilot 写代码的过程中插入结构化门禁——从任务创建到代码提交，每一步产出物都经过验证。质量检查在 `forge task gate` / `forge change verify` 进程内执行，没有实时拦截层，写操作不经过 forge。

## 安装（推荐路径）

```bash
# 1) 装 binary（机器级，一次性，提供 forge CLI）
npm install -g @agent_forge/forge
```

forge **没有宿主层**：不分发宿主插件、不写宿主配置。接不接管某个仓库由人决定：在仓库里跑 `forge init` 声明契约；门禁由 agent 显式跑 `forge task` / `forge change` 命令驱动。需要全局看板或跨项目视图时用 `forge registry add` 显式登记项目。`forge off` 仍是每项目退出，`forge on` 恢复。

>  `forge init` **只写仓库内产物**：`spec/forge.yaml`（`hooks: off` 兼容字段）、`spec/changes/`、AGENTS.md 托管块，并按默认规则维护已存在的 `CLAUDE.md` / `.cursor/rules/forge.mdc` 指针。它**不写** `~/.forge`、`~/.claude`、`~/.codex`。团队要 git 共享同一份协议，提交 `spec/` 与 `AGENTS.md` 即可。

## 显式登记（全局视图需要）

```bash
forge registry add .            # 把当前仓库登记进 ~/.forge/projects.json
forge dashboard                 # 只读聚合登记过的项目
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
# AI 工作（门禁由下面的命令显式驱动）
forge task gate task-implement    # 门禁1：实现（编译 / 断言 advisory 自检）
forge task gate task-verify       # 门禁2：验证（测试伴随变更）
forge task gate task-complete     # 门禁3：完成确认
forge task complete               # 🏁 任务完结（自动评分 + 清 active ref；git commit 必须在此之前）
forge task score                  # 质量评分
```


`forge update` 检查并更新（npm 安装打印对应包管理器的更新命令而非代下载；GitHub 安装下载自替换）。`forge off [--all] | on` 按项目退出/恢复接管；`forge config get/set compat` 管兼容基线；`forge policy state|yield` 快查状态/外来 harness 让位。详见[完整命令参考](../docs/reference/cli.md)。

## 卸载

```bash
npm uninstall -g @agent_forge/forge   # 删二进制；forge 不写宿主文件（阶段 D 起连 skills 也不再分发），已装 skill 自行删目录
```

## 多宿主支持

Forge 不分发宿主插件、不写宿主配置：`forge` 是一个仓库内的验证与状态 CLI，任何 agent/IDE 都通过 shell 调用同一组命令（`forge task` / `forge change`）。

## 常见问题

- **项目未被识别** → 跑 `forge init` 声明契约；全局看板看不到它时再 `forge registry add .`。
- **`forge` 命令 not found** → npm 全局安装目录不在 PATH。`npm root -g` 看路径，加入 shell rc。
- **二审 reviewer 反复冒新问题** → `forge task gate task-verify` 含 cheat-scan deterministic 扫描（type-suppression / error-swallow / dead-branch / comment-only-fix / comment-as-debt / phantom-import / path-assumption），机械模式一次判准，LLM-reviewer 退到只做语义判断。

## 与英文版的差异

英文根 README 涵盖：分层定位（Loop Engineering 验证 / 状态）、任务级门禁（实现 → 验证 → 完成）、`verify-acceptance` 验收标准、score 评分等；`--scope` 规划前置白名单等逐条 flag 语义见[完整命令参考](../docs/reference/cli.md)。本中文精简版只覆盖国内用户最关心的安装 + 日常 + 多宿主，请参考英文原文获取完整功能索引。
