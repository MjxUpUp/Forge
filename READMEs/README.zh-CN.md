<a id="top"></a>
<div align="center">

# 🔥 Forge（中文使用指南）

Stop trusting AI-generated code. Start gating it.

</div>

<div align="center">
  <img src="../docs/assets/forge-demo.webp" alt="Forge 30 秒演示（自动播放，静音）" width="860"/>
</div>

> 完整英文文档见根 [README.md](../README.md)，逐条 flag 语义见[命令参考](../docs/reference/cli.md)。本中文版覆盖安装 / 日常使用 / 命令参考，命令以代码块为准。

## Forge 是什么

AI 编码的"质量门禁引擎"。在 Claude Code / Codex / Cursor / Copilot 写代码的过程中插入结构化门禁——从任务创建到代码提交，每一步产出物都经过验证。质量检查在 `forge task gate` / `forge change verify` 进程内执行，没有实时拦截层，写操作不经过 forge。

两条主线：**task**（一次改动走两道门禁，实跑验收）与 **change**（一个需求留下 goal / change / acceptance 产物与封印证据）。

## 安装（推荐路径）

```bash
# 装 binary（机器级，一次性，提供 forge CLI）
npm install -g @agent_forge/forge
```

forge **没有宿主层**：不分发宿主插件、不写宿主配置。接不接管某个仓库由人决定：在仓库里跑 `forge init` 声明契约；门禁由 agent 显式跑 `forge task` / `forge change` 命令驱动。`forge off` 是每项目退出，`forge on` 恢复。

> `forge init` **只写仓库内产物**：`spec/forge.yaml`（`hooks: off` 兼容字段）、`spec/changes/`、AGENTS.md 托管块，并按默认规则维护已存在的 `CLAUDE.md` / `.cursor/rules/forge.mdc` 指针。它**不写** `~/.forge`、`~/.claude`、`~/.codex`。团队要 git 共享同一份协议，提交 `spec/` 与 `AGENTS.md` 即可。

## 60 秒拿到第一个证据

```bash
cd your-project && forge init
forge change new demo            # 生成 goal.md / change.md / acceptance.md
# 填完三份产物、删掉 forge:fill 标记，acceptance 里写 ```bash 围栏的判据
forge change clarify demo --by-human --note "边界与非目标"
forge change phase demo          # 看还缺什么
forge change verify demo         # 实跑判据 → 封印证据到 evidence/<UTC>/
```

判据是真实命令：`-run` 选择器匹配不到任何测试时按失败计（go test 退出 0 也不算通过），所以证据不会空过。改了被验证的文件，证据自动失效，必须重跑。

## 日常使用（任务门禁）

```bash
forge task start --ref feat/xxx --branch --title "描述" --accept "go test ./... :: ok"
# AI 工作（门禁由下面的命令显式驱动）
forge task gate task-implement     # 门禁1：实现（确有代码改动）
forge task verify-acceptance       # 实跑验收判据，记录确定性证据
git commit -am "..."               # 提交后再评审
forge review pass --reviewer r1 --criteria-reviewed 1   # 评审绑定代码快照
forge task gate task-complete      # 门禁2：完成确认（review pass 硬前置）
forge task complete                # 🏁 任务完结（清 active ref）
forge task report --ref feat/xxx   # 交付单
```

`forge task start` 会预跑一遍验收，把「现在是哪条判据红的」摆在开工前；`forge next` 只在继续/接手已有任务时用，新需求直接开工。

## 卸载

```bash
npm uninstall -g @agent_forge/forge   # 删二进制；forge 不写宿主文件，也早已不分发 skill
```

## 多宿主支持

Forge 不分发宿主插件、不写宿主配置：`forge` 是一个仓库内的验证与状态 CLI，任何 agent/IDE 都通过 shell 调用同一组命令（`forge task` / `forge change`）。

## 常见问题

- **项目未被识别** → 跑 `forge init` 声明契约（`spec/forge.yaml` 是激活判据）。
- **`forge` 命令 not found** → npm 全局安装目录不在 PATH。`npm root -g` 看路径，加入 shell rc。
- **门禁只有两道** → `task-implement` 与 `task-complete`；`forge review pass --reviewer <id>` 是 complete 的硬前置，同一 reviewer 不能给自己复审。
- **4.0 之后命令变少了** → 4.0 的 core-only 批次删掉了评测、看板、多仓同步与项目生命周期等一批命令；更早的 1.85 已移除宿主层，见[从 1.84 升级](../docs/migration/upgrade-from-1.84.md)与根 README 的「4.0 / core-only」小节。

## 与英文版的差异

英文根 README 涵盖：分层定位（Loop Engineering 验证 / 状态）、两道任务门禁、`verify-acceptance` 验收判据、change 的三份必需产物（另有 clarify 与人确认、可选 tasks）与证据失效规则、`test ledger` 与 `docs closure` 两个自检命令；本中文精简版只覆盖国内用户最关心的安装 + 日常 + 多宿主。
