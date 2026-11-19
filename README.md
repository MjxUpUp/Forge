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

Forge 在 AI 编码过程中插入结构化质量门禁——从任务创建到代码提交，每一步产出物都要留下 deterministic 证据，门禁由 CLI 确定性把关。质量检查只在 `forge task gate` / `forge change verify` 的进程内执行，没有实时拦截层，写操作不经过 forge。

**⚡ 60 秒拿到第一个证据**：

```bash
forge init
forge task start --ref feat/demo --branch --accept "git log -1 :: commit"
forge task verify-acceptance    # 实跑验收标准 → 不可伪造的 deterministic 证据
forge trace feat/demo           # 查看任务的完整证据链
```

跑 `forge eval wedge-drill` 可在任何机器上自检这条首证据路径（隔离环境，60 秒），它是发版 workflow 的行为级冒烟门禁与 nightly 的每日行为回归。

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
                             │ 显式调用 forge 命令
                             ▼
        ┌──────────────────────────────────────────────────┐
        │            任务门禁 · change 验证                  │
        │    task-implement → task-complete   │
        │    可执行判据 · 证据封印 · 持久化状态               │
        └────────────────────┬─────────────────────────────┘
                             │
                             ▼
                📊 质量评分 + deterministic 证据链
```

<sub>注：forge 没有宿主层；图中的门禁由 agent 显式跑 `forge task` / `forge change` 命令驱动，证据与评分由 CLI 确定性执行。</sub>

## ✨ 核心能力

<table>
  <tr>
    <td width="50%" valign="top"><strong>🚦 任务级门禁</strong><br/>每个开发任务走 2 道门禁：实现 → 完成（验收在两者之间用 `forge task verify-acceptance` 实跑）。</td>
    <td width="50%" valign="top"><strong>🔧 显式命令驱动</strong><br/>forge 没有实时拦截层：门禁由 agent 显式跑 forge task / forge change 命令触发，状态与证据落盘可审计。</td>
  </tr>
  <tr>
    <td valign="top"><strong>🧭 质量只由 gate/verify 与 CI 把关</strong><br/>没有拦截层：写操作不经过 forge；质量检查发生在 forge task gate / forge change verify 进程内，合并前由 CI 的 forge change check 兜底。</td>
    <td valign="top"><strong>📊 质量评分</strong><br/>每个任务完成后自动评分，量化 AI 编码质量；deterministic 证据链可审计。</td>
  </tr>
</table>

## 🚀 快速开始

先装二进制（唯一权威步骤见下方 [📦 安装](#-安装)），然后在项目目录：

```bash
cd your-project
forge init                 # 在仓库里声明契约：只写仓库内产物
git add -A && git commit -m "chore: forge init"   # 让 next 看到干净工作区
# 有了新需求就直接开工（见场景 A/B）；不必先跑 forge next
# forge next 只回答「接着做手上这件事该怎么走」——它在有 bug/改动时给还原命令，
# 无任务且干净时给下一条任务入口；陈旧的 ready change 只出现在 stale_changes 里，
# 不会把你拖回三天没动的旧活
```

**场景 A：小修——任务是一等入口，直接开工**（change 可以事后补）

```bash
forge task start --ref feat/fix-typo --branch --title "修正文案" --agent <实现者 id>
# 做完依次跑：task gate task-implement → task verify-acceptance
#            → review pass --reviewer <该任务绑定的评审者 id> → task gate task-complete → task complete
```

**场景 B：想留可评审记录——先立 change，再钉契约开工**

```bash
git checkout main   # --branch 只能从 main/master 建分支
forge change new my-feature --title "一句话说清要做什么"
# 填 goal.md 三段（Objective / Out of Scope / Check），问人「边界/非目标」后：
forge change clarify my-feature --by-human --note "只改 A.txt 的文案，不动 B 模块与接口"
# 补齐 goal.md（三段）/ change.md（改动·影响·验证）/ acceptance.md
forge change phase my-feature        # 每补一份看一次，直到 ready
forge task start --ref feat/my-feature --branch --change my-feature --agent <实现者 id> \
  --accept "test -f A.txt" --accept-red "test -f A.txt"   # --accept-red：开工时它应当失败
```

两条 2.1 起的新默认：**实现者身份**（`--agent`）让 `review pass` 能拒绝自批；**先失败证明**
（`--accept-red`）让 `task complete` 能证明判据在实现前确实红过（没有可失败的场景可不登记，
交付单会如实写「未登记」）。

带 `--change` 时那个 change 必须已 `ready`（阶段机只读文件，不写状态）；不带
`--change` 则随时可以开工——change 可以事后补，`forge change verify` 才是它的封口。

`forge init` **只在仓库里写文件**（随 git 提交，不会被误当成用户级状态）：

| 路径 | 说明 |
|------|------|
| `spec/forge.yaml` | 仓库契约（`version: "1"`、`profile`、`hooks: off` 兼容字段） |
| `spec/changes/` | change 产物目录（含 `archive/`） |
| `AGENTS.md` | forge 托管块（可提交共享给协作者与 agent） |
| `CLAUDE.md`、`.cursor/rules/forge.mdc` | 只在文件/目录已存在时追加/补齐指针 |

它**不写** `~/.forge`、`~/.claude`、`~/.codex`，也不登记全局注册表；重复运行只同步仓库内托管块。`--project` 已废弃为 no-op，`--profile mini|standard|full` 写入契约。

forge **没有宿主层**：不分发宿主插件、不写宿主配置。门禁由 agent 显式跑 `forge task`/`forge change` 命令驱动；证据、状态与判据仍然落盘、可审计。需要全局看板或 `forge task list --open --all-projects` 时，用 `forge registry add` 显式登记项目（`forge dashboard` 只读聚合登记过的项目）。

> **仓库自己决定要不要接管。** `forge init` 写的 `spec/forge.yaml` 把成员资格放在仓库本身——clone 到新机器、换个 agent 也能解析到项目身份。
>
> 查找位置：**只认 git 仓库根的 `spec/forge.yaml`**，向上找到 git 根就停；子目录里的同名文件不激活整仓（monorepo 里一个子包不该悄悄改变别人的行为，要激活就放到仓库根），非 git 目录向上查找，走到用户 home 就停（`~` 本身不算项目根）。放好之后 `forge status` 应显示 `spec: spec/forge.yaml (仓库声明式成员资格)`；`forge policy state` 应输出 managed（declined 的仓库例外——`forge off`、`policy yield` 或 clone 到带 `.forge-decline` 的仓库都会这样；成员资格退出优先于契约，`forge on` 可恢复）。

```yaml
# spec/forge.yaml
version: "1"       # 契约版本，按主版本兼容
profile: standard  # standard | mini | full（省略该字段按 standard 解析；forge init 写入的是 mini）
hooks: off         # 兼容字段：保留只为老契约可解析，不启用任何执法
```

- `profile` 决定任务门禁的严格档位（`mini` 最小集、`standard` 缺省、`full` 预留超集位）。
- 契约写坏时按最小档静默降级——`forge status` 会提示「⚠ 契约无法解析」但不阻断任何命令。
- `forge change init` 生成的是最小契约（`version: "1"`、`profile: mini`）。`--profile` 只改契约档位。

> **团队要 git 共享同一份协议？** 提交 `spec/` 与 `AGENTS.md` 即可：契约、change 产物与托管块都随仓库走，协作者 clone 后 `forge status` 就按仓库契约解析。

## 📋 命令速查

日常最常用的几条。**完整命令表**（每个 flag、逃生舱、门禁语义）在 [docs/reference/cli.md](docs/reference/cli.md)。

| 命令 | 用途 |
|------|------|
| `forge init` | 在仓库里声明 forge 契约：写 `spec/forge.yaml`、`spec/changes/`、AGENTS.md 托管块，并按默认规则维护已存在的入口指针；不写 `~/.forge`（`--profile mini\|standard\|full`，`--project` 已废弃为 no-op） |
| `forge next` | 从 git/任务状态推导**恰好一条**下一步命令——agent 不自选下一步；声明了 spec 的仓库脏树会建议推进已有 change、立 change，或给出可直接粘贴的 `task start --change <id>` |
| `forge change init/new/list/phase/clarify/verify/status/gate/check/agents` | 仓库内 change 生命周期：`spec/changes/<id>/` 三份产物（`goal.md` 三段目标 + `change.md` 改动/影响/验证 + `acceptance.md`；tasks.md 可选，老 change 兼容）+ 可执行判据 + 证据封印（绑定 commit；工作区有未提交改动时另记每个文件的 sha256——不依赖 forge 接管，产物随仓库走）。`status` 是循环里唯一该看的下一步来源（同因失败 3 次就停下交给人；`prior_attempts` 列出最近失败的证据，返工前先读）；`gate --task N` 只跑某条任务的判据；`check` 是 CI 与 pre-commit 共用的证据闸门（未封印/失效/失败 → 退出 2，`--staged` 只查暂存区碰到的 change）；`agents` 重同步 AGENTS.md 的 forge 托管块并维护 `CLAUDE.md` / `.cursor/rules/forge.mdc` 指针（有块只改块内、无块末尾追加；`CLAUDE.md` 不存在默认不建，`.cursor/rules/` 目录已存在时补 forge.mdc，要凭空创建用 `--pointers`，`none` 完全不碰）；`change init` 会在仓库根写 `spec/` 与 AGENTS.md，并按默认规则维护已存在的入口指针（已有 `CLAUDE.md` 追加指针、已有 `.cursor/rules/` 目录补 `forge.mdc`；随 git 提交；与 `forge init` 同源，都是仓库内产物）；`new --fast` 与普通 change 同布局（goal + change + acceptance）+ clarify 的人确认，且必须带 `--by-human`（agent 不得自批）；`phase <id>` 按产物报阶段（specify→clarify→plan→tasks→ready；三份产物布局下 plan/tasks 合并到 change.md/acceptance 检查），`clarify --by-human --note` 记录边界与非目标；`task start` 不带 `--change` 也可以开工（任务是一等入口；带 `--change` 才要求该 change 已 ready；一句话小修可加 `--kind task`）；`goal.md` 随 git 走，`task start --change <id>` 启动时钉 `Path+SHA256`，启动后改 goal 即让 implement 门禁 BLOCKED|
| `forge task start --ref <type/desc> --branch --accept "run :: expected"` | 建任务 + 建分支 + 登记验收标准 |
| `forge task gate <gate-id>` | 推进门禁：task-implement → task-complete |
| `forge task verify-acceptance` | 实跑验收标准，落 deterministic 证据 |
| `forge task complete` | 完结任务：输出事实清单（判据/红先绿/评审/逃生）+ Act 结论 + 清 active ref |
| `forge eval suite [--record]` | 仓库级回归基线：逐条实跑归档 change 的 acceptance 判据，看跨任务退化（失败不阻断门禁） |
| `forge eval trend` | 看最近 N 次回归基线的通过率与 HEAD |
| `forge task report` | 交付验收单（考卷逐条结果 + 证据强度 + 未验证面） |
| `forge task resume --match "<关键词>"` | 跨会话/跨工具拉回任务上下文 |
| `forge review pass` / `forge review status` | 代码审查标记与状态 |
| `forge gate push` | git 推送边界门禁（cheat-scan 复检 + 未消解 BLOCKED 任务） |
| `forge trace <ref>` / `forge health` / `forge dashboard` | 证据链时间线 / 质量趋势 / 全局看板（Pulse） |
| `forge status` | 项目状态 |

其余命令族（`skills`、`enforcement`、`eval`、`act`、`workspace`、`project`、`registry`（含 `registry add` 显式登记）、`harness`、`compat`、`conventions`、`policy`、`migrate`、`update`、`node`、`bridge`、`docs`、`worktree`、`on`/`off`、`config`、`verify`、`trust`）见同一份[完整命令表](docs/reference/cli.md)。

**回环怎么跑**：agent 每个回合先 `forge change status <id>`，把它给出的下一步当建议执行——补产物、跑 verify、读失败证据、重跑，acceptance 的判据全部通过才归档。判断标准来自仓库里的文件与证据，不来自对话记忆；in-loop 不需要宿主插件（`forge change status/verify` 仍是 forge 二进制提供的命令，所以「零安装」指不用装宿主插件，不是不用装 forge）。逐节点推进用 `forge change gate <id> --task N`：只跑该任务引用的判据，节点证据不封印整条 change。

**证据怎么变成合并前提**：`forge change check` 给 CI 与 pre-commit 同一套判据——每个未归档 change 都得有一份与当前代码和判据一致的封印证据。仓库里带两份接线示例：

- `.githooks/pre-commit`：`git config core.hooksPath .githooks` 启用。它只查暂存区碰到的 change；一旦暂存区出现 `spec/changes/` 之外的改动（代码、文档），就退化为检查全部 change——封印的证据绑的是代码内容，只看被碰到的 change 会放行一个注定在 CI 变红的提交。hook 执行时看的是工作区：`git add` 后又改回文件、或漏 add 证据，本地可能放行而 CI 变红。机器上没有 `forge` 或版本太旧时它打印原因后放行，仓库还没有 `spec/changes/` 时静默放行（不打扰还没用 change 流程的仓库）；POSIX 上需要可执行位，clone 后若丢了用 `chmod +x .githooks/pre-commit`。
- `.github/workflows/change-verify.yml`：只在 PR 上跑（main 上有新提交后再 merge，合并后的树与被封印的那版不同，证据按设计判失效——那是合并噪音，不是代码变了），`fetch-depth: 0` 且检 PR head sha；有 `spec/changes/` 才执行 `forge change check`，没有就整步跳过。它从源码构建 forge（这一行只在 forge 自己的仓库成立；别的仓库要换成安装固定版本的 forge），所以不存在「没装 forge 就放行」；要让它在 GitHub 上真拦住合并，还得在仓库设置里设为 required status check。

`--json` 在所有路径上输出同一份 `{checked,problems,skipped}`（空列表是 `[]`，跳过时退出码 0）。信任边界：`change check` 只把未归档 change 当作待交付物，已归档的是历史；把未封印的 change 手工 `git mv` 进 `archive/` 等于人宣布它已交付——工具不再检查，这一步只能靠 review。

「同一个原因」指每次 verify/gate 的**第一条失败命令**：换一条命令、或中间有一次通过，就重新计数。连续 3 次同因失败 → `blocked`，此时 `next_steps` 不再给任何可执行命令，恢复命令放在 `handoff` 字段里，由人执行。fast change（`new --fast`）的判据与证据规则和普通 change 完全一样，只是只要求 goal + change + acceptance 三份产物外加 clarify 的人确认，且 `--fast` 必须带 `--by-human`（人已同意）。

## 📦 安装

```bash
# npm（推荐）
npm install -g @agent_forge/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/Forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

## ⬆️ 从 1.84 及更早版本升级

1.85 删除了宿主层：hook 执法、用户级 `CLAUDE.md` / `AGENTS.md` 注入、插件、skills 分发都不再存在，
forge 的 `uninstall` 子命令也随宿主层一起删了。旧版本留在用户目录里的东西不会自动消失，需要手工清理
（删除前先备份）。下面命令按 bash 写，Windows 请用 Git Bash / MSYS 执行。

旧版会写的位置（按你用过的宿主看）：

| 宿主 | 全局注入 | hook 配置 | 插件 / skills |
| --- | --- | --- | --- |
| Claude Code | `~/.claude/CLAUDE.md`（`FORGE:START/END` 段） | `~/.claude/settings.json` 的 hooks 段 | `~/.claude/plugins/{marketplaces,cache}/forge`、`~/.claude/skills/*` 里的链接 |
| Codex | `~/.codex/AGENTS.md`（`FORGE:START/END` 段） | `~/.codex/hooks.json`，以及 `~/.codex/config.toml` 的 `[features] hooks` | `~/.codex/skills/*` 里的链接 |
| Windsurf | `~/.codeium/windsurf/memories/global_rules.md`（`FORGE:START/END` 段） | `~/.codeium/windsurf/hooks.json` 的 forge 条目 | 本版代码未见 skills 落点，如用过请检查 `~/.codeium/windsurf` |
| Cursor | — | `~/.cursor/hooks.json` | `~/.cursor/skills/*` 里的链接 |
| Kimi | `~/.kimi-code/config.toml`（`# FORGE:START` 注释段） | — | `~/.kimi-code/plugins/installed.json` 的 forge 条目 |

> 1.84 的用户级接线名册还有 cline / codebuddy / zcode / opencode 等宿主；它们在各自
> hooks 目录写文件，本表不逐一列出。不确定时先 `grep -rl "forge hook"
> ~/.claude/settings.json ~/.codex/hooks.json ~/.codeium/windsurf/hooks.json ~/.cursor/hooks.json`
> 以及这些宿主的 hooks 目录，再删。

这些段里最要命的一句是「只有看到 `[forge-session]` 横幅或 forge hook 输出才按 forge 行事，
否则忽略并不要运行 forge 命令」——1.85 没有横幅也没有 hook，这个条件恒假，Claude 会主动无视 forge。

```bash
# 0) 备份（按宿主分目录：codex 与 kimi 都有 config.toml，平铺会互相覆盖）
base=~/forge-backup/upgrade-1.84
mkdir -p "$base"/claude "$base"/codex "$base"/windsurf "$base"/cursor "$base"/kimi
backup() {
  dir=$1; shift
  for f in "$@"; do
    if [ -f "$f" ]; then cp "$f" "$base/$dir/"; fi
  done
}
backup claude ~/.claude/CLAUDE.md ~/.claude/settings.json ~/.claude/plugins/installed_plugins.json
backup codex ~/.codex/AGENTS.md ~/.codex/hooks.json ~/.codex/config.toml
backup windsurf ~/.codeium/windsurf/memories/global_rules.md ~/.codeium/windsurf/hooks.json
backup cursor ~/.cursor/hooks.json
backup kimi ~/.kimi-code/config.toml ~/.kimi-code/plugins/installed.json
# 链接清单无法用 cp 备份，先落盘一份再动（`find -maxdepth` 在 macOS 上不存在，
# 所以用 shell glob；`readlink` 把目标也记下来，恢复时有据可查）：
for d in ~/.claude/skills ~/.cursor/skills ~/.codex/skills ~/.copilot/skills ~/.agents/skills; do
  [ -d "$d" ] || continue
  for l in "$d"/*; do
    if [ -L "$l" ]; then echo "$l -> $(readlink "$l")"; fi
  done
done > "$base"/skills-links.txt

# 1) 定位 FORGE:START
grep -l 'FORGE:START' ~/.claude/CLAUDE.md ~/.codex/AGENTS.md \
  ~/.codeium/windsurf/memories/global_rules.md ~/.kimi-code/config.toml 2>/dev/null
#    有就删掉整段（含标记行）；如果文件只剩这一段，直接删文件。
#    同一文件里 forge 段之外的内容（团队规范、你自己的偏好）保留不动。

# 2) Codex hook 配置：只删 forge 条目，别删整个文件
#    编辑 ~/.codex/hooks.json，删掉 command 里带 "forge hook" 的条目；
#    再把 ~/.codex/config.toml 里 [features] hooks = true 改回 false（或删掉该行）。
#    如果 config.toml 里有 # FORGE:START … # FORGE:END 整块（旧版没有 [features] 表时
#    自动追加的），整块删掉，否则会留下孤立标记。

# 3) Claude settings / Windsurf / Cursor hooks：同样只删带 "forge hook" 的条目
#    编辑 ~/.claude/settings.json 的 hooks 段；
#    编辑 ~/.codeium/windsurf/hooks.json，删 forge 条目；
#    编辑 ~/.cursor/hooks.json，删 forge 条目（旧版写过 user-level hook）。

# 4) 旧插件与插件登记
rm -rf ~/.claude/plugins/marketplaces/forge ~/.claude/plugins/cache/forge
#    再编辑 ~/.claude/plugins/installed_plugins.json，删掉 forge@… 条目
#    （或直接跑 claude plugin uninstall forge，如果这个版本的 CLI 还认识它）
#    Kimi：编辑 ~/.kimi-code/plugins/installed.json 删 forge 条目，并删 ~/.kimi-code/plugins/managed/forge。

# 5) skills 分发：只删指向 skills-cache 的链接，再删缓存，否则会留下悬空链接
#    先预览（应全是 .../skills-cache/embedded/...）：
for d in ~/.claude/skills ~/.cursor/skills ~/.codex/skills ~/.copilot/skills ~/.agents/skills; do
  [ -d "$d" ] || continue
  for l in "$d"/*; do
    if [ -L "$l" ]; then case "$(readlink "$l")" in *skills-cache*) echo "$l";; esac; fi
  done
done
#    确认无误后把上面的 echo 换成 rm -f 再跑一遍（用户自己 ln -s 的链接不受影响）。
#    旧版还会写真实目录 forge-quality（里面那句「只在横幅出现时才生效」正是要清掉的
#    文案）；它不在链接清单里，需要单独删。1.84 同时写 claude 与 ~/.agents 两处：
rm -rf ~/.claude/skills/forge-quality ~/.agents/skills/forge-quality
#    Reasonix 用户再加 ~/.reasonix/skills/forge-quality。
rm -rf ~/.forge/skills-cache
rm -f ~/.forge/skills-manifest.json

# 6) 自检：下面都应为空
grep -l 'FORGE:START' ~/.claude/CLAUDE.md ~/.codex/AGENTS.md \
  ~/.codeium/windsurf/memories/global_rules.md ~/.kimi-code/config.toml 2>/dev/null
grep -rl 'forge hook' ~/.claude/settings.json ~/.codex/hooks.json \
  ~/.codeium/windsurf/hooks.json ~/.cursor/hooks.json 2>/dev/null
grep -l 'forge' ~/.claude/plugins/installed_plugins.json ~/.kimi-code/plugins/installed.json 2>/dev/null
ls ~/.forge/skills-cache 2>/dev/null
ls -d ~/.claude/skills/forge-quality ~/.agents/skills/forge-quality 2>/dev/null
for d in ~/.claude/skills ~/.cursor/skills ~/.codex/skills ~/.copilot/skills ~/.agents/skills; do
  [ -d "$d" ] || continue
  for l in "$d"/*; do
    if [ -L "$l" ]; then case "$(readlink "$l")" in *skills-cache*) echo "$l";; esac; fi
  done
done

# 7) 确认上面都干净后，最后再删这些历史目录（它们是最后的回滚点）
rm -rf ~/.forge/skills-backup ~/.forge/.init-suggested
```

> 说明：第 5 步按 `readlink` 的目标里是否含 `skills-cache` 来判定，只匹配 forge 装的链接，
> 用户自己 `ln -s` 的不受影响；链接清单已在第 0 步落盘到 `~/forge-backup/upgrade-1.84/skills-links.txt`。
> 第 7 步放在自检之后，是因为 `skills-backup` 是旧版本留下的回滚副本。

清理后在仓库里跑 `forge status`，应看到 `spec/forge.yaml` 契约。较新的 Claude Code
（2.1.277 起）在没有 `CLAUDE.md` 时会直接读 `AGENTS.md`；不确定自己的版本是否支持时，
跑 `forge change agents --pointers claude` 生成 `CLAUDE.md` 指针最稳妥。

## ⬆️ 从 1.85–1.9x 升级到 2.0

2.0 是破坏性版本：门禁从三道降为两道、change 产物从五份改成三份，并删掉十项检查
（文档闸门、产物链、未用导出扫描、覆盖率回扣、变异抽样、方案先行、二次验证、
自检、需求覆盖、技能决策——以上均已删除，具体命令见下表）。**老仓库不会自动损坏，
但旧流程里的命令、CI 步骤和脚本会失效**，升级时按下面清单改。

### 命令面的破坏性变化

| 旧用法 | 2.0 之后 |
| --- | --- |
| `forge task gate task-verify` | 门禁已删除；验收只跑 `forge task verify-acceptance` |
| `forge task doc-review --passed … --score …` | 命令已删除；文档回检并入评审，结论记在 `forge review pass --score N --reviewer <id>` |
| `forge task mutation` | 命令已删除；抽样变异不再由 forge 做 |
| `forge task artifact …` / `forge task chain-init` | 命令已删除；产物就是 change 里的 `goal.md` / `change.md` / `acceptance.md` |
| `forge selfcheck` | 该子命令已删除；用 `forge docs lint <文件>` + `forge change check` |
| `forge eval artifact-drill` | 子命令已删除；用 `scripts/checks/release-smoke.sh <forge 二进制>`（发布冒烟）或 `forge change check` |
| `forge review pass --note "…"` 刷新基线 / 每轮换新 reviewer | 2.1 起改为**评审者复用**：首轮 `--reviewer <id>` 绑定，之后每轮用同一 id（换新 id 会把旧问题丢掉、永远收敛不了）；确实要换人用 `--rotate-reviewer --reason "…"` 留痕；上一轮未闭合项用 `--findings <清单>` 承接 |
| 五份产物（proposal/design/tasks + acceptance + goal） | 三份：`goal.md`（三段）+ `change.md` + `acceptance.md`；`clarify.md` 记人确认，`tasks.md` 可选 |

`forge integrations` / `forge hook` / `forge freeze` / `forge doctor` 已在 1.85 删除，
宿主层（用户级 CLAUDE.md / AGENTS.md 注入、hook 执行、插件、skills 分发）整体不存在了；
升级前如果还留着那些注入，按上一节「从 1.84 及更早版本升级」清掉。`forge init` 仍然存在，
但它现在只写仓库内产物（契约 / changes 目录 / AGENTS 托管块 / 已存在的指针文件），不碰用户目录。

### 每个老项目要做的

```bash
# 0) 先升级二进制
npm i -g @agent_forge/forge@2.1.3 --include=optional --registry=https://registry.npmjs.org
forge version                      # 应 >= 2.1.3

# 1) 仓库里：搬旧 runtime state（.forge/ 里有历史 task/checklog 时）
cd <your-repo>
forge migrate --dry-run            # 预览要搬的文件
forge migrate

# 2) 刷新仓库的 AGENTS.md 托管块（老块的硬规则还是三道门禁的旧文案）
forge change agents

# 3) 看进行中的 change 和任务
forge change list
forge status

# 4) 需要全局面板时再显式登记（2.0 起 init 不再自登记）
forge registry add
```

进行中的老 change（proposal/design/tasks 布局）**可以继续按老规则跑完**：
`forge change verify` / `forge change gate` 对老布局仍按老判据走，不需要补三份。
想换新布局就新建一个 change 重开（`forge change new <id>` → 填三份 → `clarify --by-human`）。
新布局的 change 缺 `clarify.md` 时 `verify` 与 `gate` 都会拒绝（`forge change clarify <id> --by-human` 补）。

### 清掉旧版本留下的机器级垃圾

```bash
# 旧宿主层的运行时残留（1.85 起已无用；只是历史文件，删掉不影响任何命令）
find ~/.forge/projects -maxdepth 2 -name hooks -type d -exec rm -rf {} +
find ~/.forge/projects -maxdepth 2 -name protocol.yml -delete

# 注册表里的死路径与孤儿数据目录
forge registry prune               # 移除路径已不存在的条目
forge registry audit               # 看 key 漂移 / 孤儿目录 / 非法 ID
forge registry gc --prune          # 孤儿数据目录移入 ~/.forge/backups/gc-<时间戳>/
```

`forge registry gc` 默认演练，`--prune` 才动手；备份目录确认无需恢复后可以手动删。

### 升级后的自检

```bash
forge change check                 # 每个仓库：契约与证据自洽，退出码 0/2
forge status                       # 任务管道：应显示两道门禁
```

再在 CI 脚本里搜一遍上一节表格左列的旧命令名，应当没有残留；有的话按表格换成对应替代命令。

## 🤝 贡献

欢迎提 Issue 和 PR。开发时注意：

1. **门禁先行** —— 任何源码变更走 `forge task` 两道门禁（implement → complete，`review pass` 是 complete 的硬前置）；不经任务的改动不被质量评分追踪。
2. **注释双语** —— Go godoc 采用形式 A（英文段 → 空 `//` → 中文段），中文不删、不单语；行内注释与字符串字面量不动。
3. **审查闭环** —— 提交前派独立只读子 agent 跑 code-review-gate 双轨（AI 作弊 + 工程规范），`forge review pass` 标记后才能过 task-complete 门禁。
4. **提交纪律** —— 提交源码与手维护文档（`docs/reference/` 下的参考文档是测试硬依赖，
   缺文件整包测试直接红，必须入库；`docs/design/` 属设计文档，同样随代码提交）；排除
   `.claude/`、`.forge/` 等生成物与临时工作目录。

详见仓库内的 `AGENTS.md` 托管块（`forge init` 写入，随 git 共享给协作者与 agent）。

## 📚 更多文档

| 文档 | 说明 |
|------|------|
| [完整命令参考](docs/reference/cli.md) | 每个命令、flag、逃生舱与门禁语义 |
| [定位与设计依据](docs/design/positioning.md) | 为什么是确定性门禁而不是模型自检 |
| [中文使用指南](READMEs/README.zh-CN.md) | 面向国内用户的安装 / 日常 / 多宿主精简指南 |
| [项目主页](homepage/index.md) | 一分钟简介 + 核心能力速览 |
| [评估数据约定](evals/README.md) | skill eval 黄金 case 集与盲测迭代纪律 |

## License

Apache-2.0

---

<div align="center">

<sub>⬆ <a href="#top">回到顶部</a></sub>

</div>
