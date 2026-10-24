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

Forge 在 AI 编码过程中插入结构化质量门禁——从任务创建到代码提交，每一步产出物都要留下 deterministic 证据。配合 Claude Code 的 Hook 系统实时拦截，不需要你手动检查。

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
        │   hazard-guard · file-sentinel · cheat-scan       │
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

## ✨ 核心能力

<table>
  <tr>
    <td width="50%" valign="top"><strong>🚦 任务级门禁</strong><br/>每个开发任务走 3 道门禁：实现 → 验证 → 完成，门禁之间有活动检查防止跳阶段。</td>
    <td width="50%" valign="top"><strong>🪝 实时 Hook 拦截</strong><br/>27 个内置 Hook，在 AI 写代码的同时自动检查质量、防止绕过（读改前置 / 文件监控 / 高危拦截）。</td>
  </tr>
  <tr>
    <td valign="top"><strong>🛡️ 安全纵深防御</strong><br/>三层防御架构：工具拦截 → 文件监控 → 自身保护。活跃任务内，Agent 无法经 bash 绕道篡改。</td>
    <td valign="top"><strong>📊 质量评分</strong><br/>每个任务完成后自动评分，量化 AI 编码质量；deterministic 证据链可审计。</td>
  </tr>
</table>

## 🚀 快速开始

先装二进制（唯一权威步骤见下方 [📦 安装](#-安装)），然后在项目目录：

```bash
cd your-project
forge init      # 默认零项目写入，全部资产落在用户级
# 回到 Claude Code 继续工作：AI 会自动读取 Forge 生成的 Skill 并驱动门禁流程
```

主路径是 [Claude Code](https://docs.anthropic.com/en/docs/claude-code)——hooks 经它的 Hook 机制接线；Codex、Cursor、Windsurf、dsh 等 12 种宿主同样支持（接线差异见 [plugins/forge/README.md](plugins/forge/README.md) 与 [中文 README](READMEs/README.zh-CN.md)）。

`forge init` **不在项目目录写任何文件**（不会被 git add 误提交），全部资产落在用户级：

| 路径 | 说明 |
|------|------|
| `~/.forge/projects.json` | 全局项目注册表（forge 项目锚点） |
| `~/.forge/projects/<key>/` | protocol.yml + runtime state（任务状态/hook 参考副本，key=git hash 或路径 hash） |
| `~/.claude/settings.json` | Claude Code hooks（plugin 已装则由 plugin 接管，跳过此文件） |
| `~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md`、windsurf `global_rules.md` | 指针段（备份+幂等替换；激活判据锚定 `[forge-session]` managed 会话横幅，协议细节在 forge-quality skill 与 hook 即时提示，`forge uninstall --restore` 可回滚；版本变更后 autoSync 自动重刷新） |
| `~/.claude/skills/forge-quality/` | 质量协议 Skill |
| `~/.codex/hooks.json`、`~/.cursor/hooks.json` 等 | 其他 agent 的用户级 hook 接线（按检测到的工具） |

> **团队要 git 共享同一份协议？** 用 `forge init --project`（团队模式）——`.forge/protocol.yml`、`CLAUDE.md`、`AGENTS.md` 等指令资产写入项目目录可提交共享；再跑一次普通 `forge init` 即转回零写入。

> **主要用 Claude Code？** 走 [plugin marketplace](plugins/forge/README.md) 一次性接线用户级 hooks（机器上所有项目共享，连 `~/.claude/settings.json` 都不用动）。

## 📋 命令速查

日常最常用的几条。**完整命令表**（每个 flag、逃生舱、门禁语义）在 [docs/reference/cli.md](docs/reference/cli.md)。

| 命令 | 用途 |
|------|------|
| `forge init` | 初始化项目（默认零项目写入） |
| `forge next` | 从 git/任务状态推导**恰好一条**下一步命令——agent 不自选下一步 |
| `forge task start --ref <type/desc> --branch --accept "run :: expected"` | 建任务 + 建分支 + 登记验收标准 |
| `forge task gate <gate-id>` | 推进门禁：task-implement → task-verify → task-complete |
| `forge task verify-acceptance` | 实跑验收标准，落 deterministic 证据 |
| `forge task complete` | 完结任务：自动评分 + Act 结论 + 清 active ref |
| `forge task report` | 交付验收单（考卷逐条结果 + 证据强度 + 未验证面） |
| `forge task resume --match "<关键词>"` | 跨会话/跨工具拉回任务上下文 |
| `forge review pass` / `forge review status` | 代码审查标记与状态 |
| `forge gate push` | git 推送边界门禁（cheat-scan 复检 + 未消解 BLOCKED 任务） |
| `forge hazard confirm --last` | 确认最近一条被拦高危命令（须真人终端） |
| `forge freeze <路径>...` | 冻结写入范围，冻结路径之外的写全部硬阻断 |
| `forge trace <ref>` / `forge health` / `forge dashboard` | 证据链时间线 / 质量趋势 / 全局看板（Pulse） |
| `forge doctor` / `forge status` | 环境一致性审计 / 项目状态 |

其余命令族（`skills`、`eval`、`act`、`workspace`、`project`、`registry`、`harness`、`compat`、`plugin`、`conventions`、`policy`、`enforcement`、`selfcheck`、`sync`、`migrate`、`uninstall`、`update`、`node`、`bridge`、`docs`、`worktree`、`on`/`off`、`config`、`verify`、`trust`）见同一份[完整命令表](docs/reference/cli.md)。

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

活跃任务内，Agent 无法通过 `node -e "fs.writeFileSync()"`、`cat > file`、直接编辑 task JSON 等方式绕过——bash-guard 拦截工具层，file-sentinel 监控文件层，task-guard 保护配置层。**没有活跃任务时 task-guard 只给 WARN 提示，不是硬墙**；有任务才按门禁语义拦截。

27 个内置 Hook 的完整清单（触发时机、职责；可关闭的门禁类都附了逃生开关）见 [docs/reference/hooks.md](docs/reference/hooks.md)。

## 📦 安装

```bash
# npm（推荐）
npm install -g @agent_forge/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/Forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

> **「plugin」一词在 Forge 语境里有两层含义，勿混淆**（设计共识见
> [docs/design/plugin-philosophy.md](docs/design/plugin-philosophy.md)）：
> **Forge 的 plugin** = 宿主接线**打包**（分发层）——`plugins/forge`（Claude
> marketplace pack）与 `plugins/forge-dsh`（dsh 桥）这类"薄 manifest + 共享
> payload"，宿主 agent 加载它们，Forge 二进制自身不加载任何运行时插件；
> **dsh/Cordis 的 plugin** = 运行时可装卸的**扩展组件**（运行时层）。同名不同物，
> `plugins/forge-dsh` 恰好横跨两者——它以 Cordis 插件的形态被 dsh 运行时加载，
> 本体却只是 forge CLI 的接线包。

<details>
<summary><b>📖 通过 Claude Code plugin marketplace（用户级，一次性接线）</b></summary>

若主要用 Claude Code，可走 plugin marketplace 一次性接线用户级 hooks（机器上所有项目共享，连 `~/.claude/settings.json` 都不用动）：

```
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge
```

仍需 `npm install -g @agent_forge/forge` 装二进制（hooks 都 spawn forge）。项目登记按 takeover 偏好（出厂默认 **ask**）：init-suggest SessionStart hook 在未接管项目首次会话**询问一次**（同意→`forge init`；拒绝→`forge off`）；要"装好即用"的无感体验→`forge config set` takeover 为 auto（静默接管所有 git 项目；declined、`.forge-decline` 团队声明与外来 harness 让位仍然生效）。hooks 由 plugin.json 全机器接管，`forge init` 跳过自己的 settings.json 注册。完整三步与各 host 差异见 `plugins/forge/README.md`。

</details>

## 🤝 贡献

欢迎提 Issue 和 PR。开发时注意：

1. **门禁先行** —— 任何源码变更走 `forge task` 三门禁（implement → verify → complete），不经任务的改动不被质量评分追踪。
2. **注释双语** —— Go godoc 采用形式 A（英文段 → 空 `//` → 中文段），中文不删、不单语；行内注释与字符串字面量不动。
3. **审查闭环** —— 提交前派独立只读子 agent 跑 code-review-gate 双轨（AI 作弊 + 工程规范），`forge review pass` 标记后才能过 task-complete 门禁。
4. **提交纪律** —— 提交源码与手维护文档（`docs/reference/`、`docs/design/` 下的参考文档是
   测试硬依赖，缺文件整包测试直接红，必须入库）；排除 `.claude/`、`.forge/` 等生成物与临时工作目录。

详见质量协议（由 `forge init` 生成到用户级 `~/.claude/CLAUDE.md` / `~/.codex/AGENTS.md`；生成器源码在 `internal/skillgen/`）。

## 📚 更多文档

| 文档 | 说明 |
|------|------|
| [完整命令参考](docs/reference/cli.md) | 每个命令、flag、逃生舱与门禁语义 |
| [Hook 完整清单](docs/reference/hooks.md) | 27 个内置 Hook 的触发时机与职责 |
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
