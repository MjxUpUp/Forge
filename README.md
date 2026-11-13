<a id="top"></a>
<div align="center">

# 🔥 Forge

**AI 开发质量门禁引擎**

Stop trusting AI-generated code. Start gating it.

[![CI](https://github.com/MjxUpUp/Forge/actions/workflows/ci.yml/badge.svg)](https://github.com/MjxUpUp/Forge/actions/workflows/ci.yml)
[![npm](https://img.shields.io/npm/v/@agent_forge/forge?label=npm)](https://www.npmjs.com/package/@agent_forge/forge)
[![downloads](https://img.shields.io/npm/dt/@agent_forge/forge?label=downloads)](https://www.npmjs.com/package/@agent_forge/forge)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![platform](https://img.shields.io/badge/platform-macOS%20%7C%20Linux%20%7C%20Windows-lightgrey)](#-安装)
[![license](https://img.shields.io/github/license/MjxUpUp/Forge)](./LICENSE)

</div>

<div align="center">
  <img src="docs/assets/forge-demo.webp" alt="Forge 30 秒演示（自动播放，静音）" width="860"/>
  <p><sub><b>30 秒演示</b>（自动播放，静音）—— 画面里的终端输出是真实 <code>forge docs lint</code> 跑出来的：两句没带证据的套话，被拦在提交之前。<br/>
  带音轨的 1080p 版本在 <a href="https://mjxupup.github.io/Forge/">网页播放器</a>，或直接下 <a href="docs/assets/forge-demo.mp4">mp4</a>。</sub></p>
</div>

---

> **AI 写的代码，你放心直接提交吗？**

Forge 在 AI 编码过程中插入结构化质量门禁——从任务创建到代码提交，每一步产出物都要留下 deterministic 证据，门禁由 CLI 确定性把关。配合宿主 Hook 系统还能在写代码时实时拦截（需显式开启，缺省 audit-only 静默审计，见下）。

**⚡ 60 秒拿到第一个证据**：

```bash
forge init
forge task start --ref feat/demo --branch --accept "git log -1 :: commit"
forge task verify-acceptance    # 实跑验收标准 → 不可伪造的 deterministic 证据
forge trace feat/demo           # 查看任务的完整证据链
```

跑 `forge eval wedge-drill` 可在任何机器上自检这条首证据路径（隔离环境，60 秒）；`forge eval artifact-drill` 自检产物链分档执法全链。两者是发版 workflow 的行为级冒烟门禁与 nightly 的每日行为回归。

## 🎯 它补的是哪一层

AI 编码是一个循环：写代码 → 运行 → 读反馈 → 修正 → 再写。这个循环由 coding agent（Claude Code、Codex、Cursor）驱动，**Forge 不替代循环本身**，只补上循环最容易缺的两层：**验证层**（每一轮产出物经门禁检验）与**状态层**（跨循环的任务状态与门禁历史——上下文一压缩就丢的那种）。

Forge 是 deterministic 的——范围是 "deterministic where code can verify"：行为与意图的正确性留给 review 和人。判据与证据链归产品而非模型厂商，所以这一层不随模型代际贬值；模型变强时该删的是脚手架，不是传感器。

<sub>完整论述（为什么是确定性门禁而不是让模型自检、退出码三态的设计依据）见 [docs/design/positioning.md](docs/design/positioning.md)。</sub>

## 🔧 它是怎么跑的

```
        ┌──────────────────────────────────────────────────┐
        │                coding agent                       │
        │        (Claude Code / Codex / Cursor ...)         │
        └────────────────────┬─────────────────────────────┘
                             │ 每次 Write / Edit / Bash
                             ▼
        ┌──────────────────────────────────────────────────┐
        │            Forge Hooks · 实时拦截                 │
        │   task-guard · read-before-edit · bash-guard      │
        │   hazard-guard · file-sentinel · doc-lint         │
        └────────────────────┬─────────────────────────────┘
                             │
                             ▼
        ┌──────────────────────────────────────────────────┐
        │           任务门禁 · 持久化状态                    │
        │    task-implement → task-verify → task-complete   │
        └────────────────────┬─────────────────────────────┘
                             │
                             ▼
                📊 质量评分 + deterministic 证据链
```

<sub>注：图中「Forge Hooks · 实时拦截」为 opt-in 加速器——契约 `hooks: on` 或无契约宿主 `FORGE_HOOKS=on` 才执法，缺省 audit-only 静默审计；任务门禁与证据链由 CLI 确定性执行，始终生效。</sub>

## ✨ 核心能力

<table>
  <tr>
    <td width="50%" valign="top"><strong>🚦 任务级门禁</strong><br/>每个开发任务走 3 道门禁：实现 → 验证 → 完成，门禁之间有活动检查防止跳阶段。</td>
    <td width="50%" valign="top"><strong>🪝 实时 Hook 拦截</strong><br/>26 个内置 Hook，显式开启后在 AI 写代码的同时自动检查质量、防止绕过（读改前置 / 文件监控 / 高危拦截）；缺省 audit-only 静默审计。</td>
  </tr>
  <tr>
    <td valign="top"><strong>🛡️ 安全纵深防御</strong><br/>三层防御架构：工具拦截 → 文件监控 → 自身保护。开启执法后，活跃任务内 Agent 无法经 bash 绕道篡改（缺省 audit-only）。</td>
    <td valign="top"><strong>📊 质量评分</strong><br/>每个任务完成后自动评分，量化 AI 编码质量；deterministic 证据链可审计。</td>
  </tr>
</table>

## 🚀 快速开始

先装二进制（唯一权威步骤见下方 [📦 安装](#-安装)），然后在项目目录：

```bash
cd your-project
forge init                              # 在仓库里声明契约：只写仓库内产物
forge integrations enable claude-code   # 可选：显式接线宿主（或直接装 Claude plugin）
```

主路径是 [Claude Code](https://docs.anthropic.com/en/docs/claude-code)——hooks 经它的 Hook 机制接线；Codex、Cursor、Windsurf、dsh 等 12 种宿主同样支持（接线差异见 [plugins/forge/README.md](plugins/forge/README.md) 与 [中文 README](READMEs/README.zh-CN.md)）。

`forge init` **只在仓库里写文件**（随 git 提交，不会被误当成用户级状态）：

| 路径 | 说明 |
|------|------|
| `spec/forge.yaml` | 仓库契约（`version: "1"`、`profile`、`hooks: off` 缺省） |
| `spec/changes/` | change 产物目录（含 `archive/`） |
| `AGENTS.md` | forge 托管块（可提交共享给协作者与 agent） |
| `CLAUDE.md`、`.cursor/rules/forge.mdc` | 只在文件/目录已存在时追加/补齐指针 |

它**不写** `~/.forge`、`~/.claude`、`~/.codex`，也不登记全局注册表；重复运行只同步仓库内托管块。`--project`、`--agents` 已废弃为 no-op，`--profile mini|standard|full` 写入契约。

宿主接线是显式 opt-in：`forge integrations enable <host>`（claude-code / codex / cursor / copilot / windsurf / kimi / cline / opencode / reasonix / codebuddy / dsh / zcode），或 Claude Code 装 plugin。用户级资产只在 enable（或已装 plugin / Phase C 前接过线的老仓库）时写；plugin 已装时跳过 `settings.json`，但 DataDir 的 protocol/hook 模板仍会生成：

| 路径 | 说明 |
|------|------|
| `~/.forge/integrations/<host>` | 已接线标记（`forge integrations disable <host>` 摘掉） |
| `~/.forge/projects.json` | 全局项目注册表（enable 时登记；`forge workspace add` 也可显式登记） |
| `~/.forge/projects/<key>/` | protocol.yml + runtime state（任务状态/hook 参考副本，key=git hash 或路径 hash） |
| `~/.claude/settings.json` | Claude Code hooks（plugin 已装则由 plugin 接管，跳过此文件） |
| `~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md`、windsurf `global_rules.md` | 指针段（备份+幂等替换；`forge uninstall --restore` 可回滚） |
| `~/.claude/skills/forge-quality/` | 质量协议 Skill |
| `~/.codex/hooks.json`、`~/.cursor/hooks.json` 等 | 其他 agent 的用户级 hook 接线 |

> **仓库自己决定要不要接管。** `forge init` 写的 `spec/forge.yaml` 把成员资格放在仓库本身——clone 到新机器、换个 agent 也能解析到项目身份。注意 hook 接线仍由 `forge integrations enable`（或 plugin）提供；契约只决定这些 hook 在这个仓库里跑不跑、按哪档跑。
>
> 查找位置：**只认 git 仓库根的 `spec/forge.yaml`**，向上找到 git 根就停；子目录里的同名文件不激活整仓（monorepo 里一个子包不该悄悄改变别人的行为，要激活就放到仓库根），非 git 目录向上查找，走到用户 home 就停（`~` 本身不算项目根）。放好之后 `forge status` 应显示 `spec: spec/forge.yaml (仓库声明式成员资格)`；`forge policy state` 应输出 managed（declined 的仓库例外——`forge off`、`policy yield` 或 clone 到带 `.forge-decline` 的仓库都会这样；成员资格退出优先于契约，`forge on` 可恢复）。

```yaml
# spec/forge.yaml
version: "1"       # 契约版本，按主版本兼容
profile: standard  # standard | mini | full（缺省 standard）
hooks: off         # on | off（缺省 off）；写 on 才执法
```

- 注意别和 `forge off`（成员资格退出）混：`hooks: off` 只静默 hook 检查，省掉噪音；`forge off` 是项目级退出接管，连成员资格一起让位。两者独立生效。
- `hooks: off`（**缺省**）——这个仓库里的 hook 检查静默放行：**0 次检查、0 注入，三条安全底线也不例外**（audit-only：事件仍进归属/审计，但不评估、不阻断、不注入）。hook 是宿主 spawn 的，进程本身挡不掉：每次事件仍会拉起一次毫秒级 `forge` 然后立刻 allow（batch 接线才合并成一次/工具调用）。只静默 hook；你显式跑的 `forge task` / `forge change` 门禁照旧执行。要执法，显式写 `hooks: on`。
- **安全底线随执法开关**（ADR-0002 阶段 B 语义变更）：`hazard-guard`（破坏性命令）、`mcp-scan`（`.mcp.json` 供应链）、`skill-scan`（恶意 skill 指令）默认 audit-only，只有显式 opt-in（契约 `hooks: on`，或无契约宿主层 `FORGE_HOOKS=on`）后才执法；执法时底线不受契约 `profile` 收窄。取舍：阻断式 hazard-guard 取证 109 次拦截零真实损失、18 天停机，默认安静优先，危险命令防护由宿主自带确认层承接。
  执法时这条的边界仍是机器全局档位：名册在契约之前就先滤一遍，`FORGE_PROFILE=lite` 时 lite 名册里只有 `hazard-guard`，`mcp-scan`/`skill-scan` 本来就没接线、不在此列。
- `profile`——`mini` 是最小执法名册（lite：task-guard / bash-guard / hazard-guard / file-sentinel 等拦截类为主，另含 tool-track / failure-track 等观测项；advisory 类不在内）；`standard` 是缺省名册；`full` 目前与 `standard` 等同（预留超集位）。契约只做运行时收窄：settings 接线时被全局更窄档位滤掉的 hook，不会因为契约写 `full` 又接回来。
- 契约写坏时按审计模式静默放行——`hooks: of`、拼错键名这类错误整份按解析失败处理，opt-in posture 下坏契约不打开执法（也不回落全局档位）。`forge status` 会打出「⚠ 契约无法解析（审计模式，不执法）」并提示修好契约写 `hooks: on`，不阻断任何命令。
- `forge change init` 生成的是最小契约（`version: "1"`、`profile: mini`、`hooks: off`——只用产物、不开执法）。`--profile` 只改档位、不会打开门禁：要执法必须同时把 `hooks` 改成 `on`。

> **团队要 git 共享同一份协议？** 提交 `spec/` 与 `AGENTS.md` 即可：契约、change 产物与托管块都随仓库走，协作者 clone 后 `forge status` 就按仓库契约解析（宿主 hook 仍需各自 `forge integrations enable`，或装 plugin）。

> **主要用 Claude Code？** 走 [plugin marketplace](plugins/forge/README.md) 一次性接线用户级 hooks（机器上所有项目共享，连 `~/.claude/settings.json` 都不用动）。

## 📋 命令速查

日常最常用的几条。**完整命令表**（每个 flag、逃生舱、门禁语义）在 [docs/reference/cli.md](docs/reference/cli.md)。

| 命令 | 用途 |
|------|------|
| `forge init` | 在仓库里声明 forge 契约：写 `spec/forge.yaml`、`spec/changes/`、AGENTS.md 托管块，并按默认规则维护已存在的入口指针；不写 `~/.forge`（`--profile mini\|standard\|full`，`--project`/`--agents` 已废弃为 no-op） |
| `forge integrations list/enable/disable <host>` | 显式把 forge 接线到宿主（12 种）：enable 才写用户级 hooks/指令/skill、登记注册表并打 `~/.forge/integrations/<host>` 标记；disable 只摘标记，宿主配置不删 |
| `forge next` | 从 git/任务状态推导**恰好一条**下一步命令——agent 不自选下一步 |
| `forge change init/new/list/verify/status/gate/check/agents` | 仓库内 change 生命周期：`spec/changes/<id>/` 四份产物（proposal/design/tasks/acceptance）+ 可执行判据 + 证据封印（绑定 commit；工作区有未提交改动时另记每个文件的 sha256——不依赖 forge 接管，产物随仓库走）。`status` 是循环里唯一该看的下一步来源（同因失败 3 次就停下交给人；`prior_attempts` 列出最近失败的证据，返工前先读）；`gate --task N` 只跑某条任务的判据；`check` 是 CI 与 pre-commit 共用的证据闸门（未封印/失效/失败 → 退出 2，`--staged` 只查暂存区碰到的 change）；`agents` 重同步 AGENTS.md 的 forge 托管块并维护 `CLAUDE.md` / `.cursor/rules/forge.mdc` 指针（有块只改块内、无块末尾追加；`CLAUDE.md` 不存在默认不建，`.cursor/rules/` 目录已存在时补 forge.mdc，要凭空创建用 `--pointers`，`none` 完全不碰）；`change init` 会在仓库根写 `spec/` 与 AGENTS.md，并按默认规则维护已存在的入口指针（已有 `CLAUDE.md` 追加指针、已有 `.cursor/rules/` 目录补 `forge.mdc`；随 git 提交；与 `forge init` 同源，都是仓库内产物）；`new --fast` 只要求 proposal + acceptance |
| `forge task start --ref <type/desc> --branch --accept "run :: expected"` | 建任务 + 建分支 + 登记验收标准 |
| `forge task gate <gate-id>` | 推进门禁：task-implement → task-verify → task-complete |
| `forge task verify-acceptance` | 实跑验收标准，落 deterministic 证据 |
| `forge task complete` | 完结任务：自动评分 + Act 结论 + 清 active ref |
| `forge task report` | 交付验收单（考卷逐条结果 + 证据强度 + 未验证面） |
| `forge task resume --match "<关键词>"` | 跨会话/跨工具拉回任务上下文 |
| `forge review pass` / `forge review status` | 代码审查标记与状态 |
| `forge gate push` | git 推送边界门禁（cheat-scan 复检 + 未消解 BLOCKED 任务） |
| `forge hazard confirm --last` | 确认最近一条被拦高危命令（须真人终端） |
| `forge freeze <路径>...` | 冻结写入范围；开启 hook 执法后冻结路径之外的写硬阻断（缺省 audit-only 不阻断） |
| `forge trace <ref>` / `forge health` / `forge dashboard` | 证据链时间线 / 质量趋势 / 全局看板（Pulse） |
| `forge doctor` / `forge status` | 环境一致性审计 / 项目状态 |

其余命令族（`skills`、`eval`、`act`、`workspace`、`project`、`registry`、`harness`、`compat`、`plugin`、`conventions`、`policy`、`enforcement`、`selfcheck`、`sync`、`migrate`、`uninstall`、`update`、`node`、`bridge`、`docs`、`worktree`、`on`/`off`、`config`、`verify`、`trust`）见同一份[完整命令表](docs/reference/cli.md)。

**回环怎么跑**：agent 每个回合先 `forge change status <id>`，只执行它给出的下一步——补产物、跑 verify、读失败证据、重跑，acceptance 的判据全部通过才归档。判断标准来自仓库里的文件与证据，不来自对话记忆；in-loop 不需要宿主 hook（`forge change status/verify` 仍是 forge 二进制提供的命令，所以「零安装」指不用装 hook，不是不用装 forge）。逐节点推进用 `forge change gate <id> --task N`：只跑该任务引用的判据，节点证据不封印整条 change。

**证据怎么变成合并前提**：`forge change check` 给 CI 与 pre-commit 同一套判据——每个未归档 change 都得有一份与当前代码和判据一致的封印证据。仓库里带两份接线示例：

- `.githooks/pre-commit`：`git config core.hooksPath .githooks` 启用。它只查暂存区碰到的 change；一旦暂存区出现 `spec/changes/` 之外的改动（代码、文档），就退化为检查全部 change——封印的证据绑的是代码内容，只看被碰到的 change 会放行一个注定在 CI 变红的提交。hook 执行时看的是工作区：`git add` 后又改回文件、或漏 add 证据，本地可能放行而 CI 变红。机器上没有 `forge` 或版本太旧时它打印原因后放行，仓库还没有 `spec/changes/` 时静默放行（不打扰还没用 change 流程的仓库）；POSIX 上需要可执行位，clone 后若丢了用 `chmod +x .githooks/pre-commit`。
- `.github/workflows/change-verify.yml`：只在 PR 上跑（main 上有新提交后再 merge，合并后的树与被封印的那版不同，证据按设计判失效——那是合并噪音，不是代码变了），`fetch-depth: 0` 且检 PR head sha；有 `spec/changes/` 才执行 `forge change check`，没有就整步跳过。它从源码构建 forge（这一行只在 forge 自己的仓库成立；别的仓库要换成安装固定版本的 forge），所以不存在「没装 forge 就放行」；要让它在 GitHub 上真拦住合并，还得在仓库设置里设为 required status check。

`--json` 在所有路径上输出同一份 `{checked,problems,skipped}`（空列表是 `[]`，跳过时退出码 0）。信任边界：`change check` 只把未归档 change 当作待交付物，已归档的是历史；把未封印的 change 手工 `git mv` 进 `archive/` 等于人宣布它已交付——工具不再检查，这一步只能靠 review。

「同一个原因」指每次 verify/gate 的**第一条失败命令**：换一条命令、或中间有一次通过，就重新计数。连续 3 次同因失败 → `blocked`，此时 `next_steps` 不再给任何可执行命令，恢复命令放在 `handoff` 字段里，由人执行。fast change（`new --fast`）的判据与证据规则和普通 change 完全一样，只是只要求 proposal + acceptance 两份产物。

## 🪝 Hook 系统

Forge 通过 Claude Code 的 Hook 机制实现实时质量检查。三层纵深防御，监控的是文件而非工具：

```
Layer 1: PreToolUse 快速拦截
  ├─ task-guard: Write/Edit → 检查任务状态 + 自保护（forge 配置层）
  └─ bash-guard: Bash → 检测写文件模式

Layer 2: PostToolUse 文件监控
  └─ file-sentinel: Bash → 对比执行前后 git 状态，未授权变更隔离到 DataDir/quarantine/（可恢复）

Layer 3: 会话结束验证
  └─ task-verify: 检查任务完成度 + 主分支保护 + 自身版本
```

开启 hook 执法后（契约 `hooks: on` 或宿主 `FORGE_HOOKS=on`；缺省 audit-only 不拦截），活跃任务内，Agent 无法通过 `node -e "fs.writeFileSync()"`、`cat > file`、直接编辑 task JSON 等方式绕过——bash-guard 拦截工具层，file-sentinel 监控文件层，task-guard 保护配置层。**没有活跃任务时 task-guard 只给 WARN 提示，不是硬墙**；有任务才按门禁语义拦截。

26 个内置 Hook 的完整清单（触发时机、职责；opt-in 执法，执法时的门禁类另附逃生开关）见 [docs/reference/hooks.md](docs/reference/hooks.md)。

## 📦 安装

```bash
# npm（推荐）
npm install -g @agent_forge/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/Forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

> 「plugin」在 Forge 语境指**宿主接线打包**（分发层，`plugins/forge` / `plugins/forge-dsh`），与 dsh/Cordis 的运行时插件同名不同物——辨析见 [plugin-philosophy.md](docs/design/plugin-philosophy.md)。

<details>
<summary><b>📖 通过 Claude Code plugin marketplace（用户级，一次性接线）</b></summary>

若主要用 Claude Code，可走 plugin marketplace 一次性接线用户级 hooks（机器上所有项目共享，连 `~/.claude/settings.json` 都不用动）：

```
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge
```

仍需 `npm install -g @agent_forge/forge` 装二进制（hooks 都 spawn forge）。plugin 已装时 Claude 的 hooks 由 plugin.json 全机器接管；其他宿主跑 `forge integrations enable <host>` 显式接线。原先的 init-suggest（自动探测未接管项目并建议/执行 `forge init` 的 SessionStart hook）已在阶段 C 移除——是否把仓库交给 forge 由人显式 `forge init` 决定，不再有后台接管。完整三步与各 host 差异见 `plugins/forge/README.md`。

</details>

## 🤝 贡献

欢迎提 Issue 和 PR。开发时注意：

1. **门禁先行** —— 任何源码变更走 `forge task` 三门禁（implement → verify → complete），不经任务的改动不被质量评分追踪。
2. **注释双语** —— Go godoc 采用形式 A（英文段 → 空 `//` → 中文段），中文不删、不单语；行内注释与字符串字面量不动。
3. **审查闭环** —— 提交前派独立只读子 agent 跑 code-review-gate 双轨（AI 作弊 + 工程规范），`forge review pass` 标记后才能过 task-complete 门禁。
4. **提交纪律** —— 提交源码与手维护文档（`docs/reference/` 下的参考文档是测试硬依赖，
   缺文件整包测试直接红，必须入库；`docs/design/` 属设计文档，同样随代码提交）；排除
   `.claude/`、`.forge/` 等生成物与临时工作目录。

详见质量协议（由 `forge integrations enable <host>` 生成到用户级 `~/.claude/CLAUDE.md` / `~/.codex/AGENTS.md`；仓库内的 AGENTS.md 托管块由 `forge init` 写入；生成器源码在 `internal/skillgen/`）。

## 📚 更多文档

| 文档 | 说明 |
|------|------|
| [完整命令参考](docs/reference/cli.md) | 每个命令、flag、逃生舱与门禁语义 |
| [Hook 完整清单](docs/reference/hooks.md) | 26 个内置 Hook 的触发时机与职责 |
| [定位与设计依据](docs/design/positioning.md) | 为什么是确定性门禁而不是模型自检 |
| [中文使用指南](READMEs/README.zh-CN.md) | 面向国内用户的安装 / 日常 / 多宿主精简指南 |
| [Plugin 安装详解](plugins/forge/README.md) | 多 host plugin marketplace 三步接线与各 host 差异 |
| [项目主页](homepage/index.md) | 一分钟简介 + 核心能力速览 |
| [评估数据约定](evals/README.md) | skill eval 黄金 case 集与盲测迭代纪律 |

## License

Apache-2.0

---

<div align="center">

<sub>⬆ <a href="#top">回到顶部</a></sub>

</div>
