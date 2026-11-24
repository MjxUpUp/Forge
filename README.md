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

Forge 在 AI 编码过程中插入结构化质量门禁——从任务创建到代码提交，每一步产出物都要留下 deterministic 证据，门禁由 CLI 确定性把关。质量检查只在 `forge task gate` / `forge task verify-acceptance` / `forge change verify` / `forge change check` 的进程内执行，没有实时拦截层，写操作不经过 forge。

**⚡ 60 秒拿到第一个证据**：

```bash
forge init
forge task start --ref feat/demo --branch --agent demo-agent --accept "git log -1 :: commit"
forge task verify-acceptance    # 实跑验收标准 → 不可伪造的 deterministic 证据
forge task report --ref feat/demo # 交付单：判据结果 + 证据强度 + 未验证面
```

跑 `forge change check` 可在任何机器上自检仓库契约与证据是否自洽（退出码 0/2），它既是 CI 的闸门，也是仓库自带可选 pre-commit 脚本调用的同一条检查（forge 不安装 hook）。

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
                deterministic 证据链 + 证据封印
```

<sub>注：forge 没有宿主层；图中的门禁由 agent 显式跑 `forge task` / `forge change` 命令驱动，证据由 CLI 确定性执行与封印。</sub>

## ✨ 核心能力

<table>
  <tr>
    <td width="50%" valign="top"><strong>🚦 任务级门禁</strong><br/>每个开发任务走 2 道门禁：实现 → 完成（验收在两者之间用 `forge task verify-acceptance` 实跑）。</td>
    <td width="50%" valign="top"><strong>🔧 显式命令驱动</strong><br/>forge 没有实时拦截层：门禁由 agent 显式跑 forge task / forge change 命令触发，状态与证据落盘可审计。</td>
  </tr>
  <tr>
    <td valign="top"><strong>🧭 质量只由 gate/verify 与 CI 把关</strong><br/>没有拦截层：写操作不经过 forge；质量检查发生在 forge task gate / forge task verify-acceptance / forge change verify 进程内，合并前由 CI 的 forge change check 兜底。</td>
    <td valign="top"><strong>📒 测试账本</strong><br/>`forge test ledger` 按 `go test -json` 记录给出测试四态（live / orphaned / skipped / phantom），Skip 必须写理由并经人审批；条件跳过单独计数，不算被跳过。</td>
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

forge **没有宿主层**：不分发宿主插件、不写宿主配置。门禁由 agent 显式跑 `forge task`/`forge change` 命令驱动；证据、状态与判据仍然落盘、可审计。项目内任务用 `forge task list` 看；跨项目的全局聚合命令（`registry` / `dashboard`）已在 core-only 中删除。

> **仓库自己决定要不要接管。** `forge init` 写的 `spec/forge.yaml` 把成员资格放在仓库本身——clone 到新机器、换个 agent 也能解析到项目身份。
>
> 查找位置：**只认 git 仓库根的 `spec/forge.yaml`**，向上找到 git 根就停；子目录里的同名文件不激活整仓（monorepo 里一个子包不该悄悄改变别人的行为，要激活就放到仓库根），非 git 目录向上查找，走到用户 home 就停（`~` 本身不算项目根）。放好之后 `forge status` 应显示 `spec: spec/forge.yaml (仓库声明式成员资格)`；declined 的仓库例外——`forge off` 或 clone 到带 `.forge-decline` 的仓库会让 `forge status` 以非零退出码报「已退出接管」，`forge on` 可恢复。

```yaml
# spec/forge.yaml
version: "1"       # 契约版本，按主版本兼容
profile: mini      # mini | standard | full（省略该字段按 standard 解析；forge init 写入的是 mini）
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
| `forge task checkpoint` / `rollback <id>` | 工作树存档点：大改前 `checkpoint --label`，改乱了 `rollback cp-N`（git stash 语义，不产生提交） |
| `forge task complete` | 完结任务：输出事实清单（判据/红先绿/评审/逃生）+ Act 结论 + 清 active ref |
| `forge task report` | 交付验收单（考卷逐条结果 + 证据强度 + 未验证面） |
| `forge task resume --match "<关键词>"` | 跨会话/跨工具拉回任务上下文 |
| `forge review pass` / `forge review status` | 代码审查标记与状态 |
| `forge status` | 项目状态 |

其余命令（`change`、`task`、`review`、`docs`、`impact`、`test`、`compat`、`on`/`off`、`next`、`status`、`version`）见同一份[完整命令表](docs/reference/cli.md)。core-only 之后不再有 `eval`/`gate`/`trace`/`health`/`dashboard`/`registry`/`migrate`/`harness`/`workspace`/`skill`/`update` 这些命令组。

**回环怎么跑**：agent 每个回合先 `forge change status <id>`，把它给出的下一步当建议执行——补产物、跑 verify、读失败证据、重跑，acceptance 的判据全部通过才归档。判断标准来自仓库里的文件与证据，不来自对话记忆；in-loop 不需要宿主插件（`forge change status/verify` 仍是 forge 二进制提供的命令，所以「零安装」指不用装宿主插件，不是不用装 forge）。逐节点推进用 `forge change gate <id> --task N`：只跑该任务引用的判据，节点证据不封印整条 change。

**证据怎么变成合并前提**：`forge change check` 给 CI 与 pre-commit 同一套判据——每个未归档 change 都得有一份与当前代码和判据一致的封印证据。仓库里带两份接线示例：

- `.githooks/pre-commit`（仓库自带、可选；forge 不安装 hook）：`git config core.hooksPath .githooks` 启用。它只查暂存区碰到的 change；一旦暂存区出现 `spec/changes/` 之外的改动（代码、文档），就退化为检查全部 change——封印的证据绑的是代码内容，只看被碰到的 change 会放行一个注定在 CI 变红的提交。hook 执行时看的是工作区：`git add` 后又改回文件、或漏 add 证据，本地可能放行而 CI 变红。机器上没有 `forge` 或版本太旧时它打印原因后放行，仓库还没有 `spec/changes/` 时静默放行（不打扰还没用 change 流程的仓库）；POSIX 上需要可执行位，clone 后若丢了用 `chmod +x .githooks/pre-commit`。
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

## ⬆️ 升级到 3.0（core-only）

3.0 是第二次破坏性收窄：只保留核心链路，旧时代命令组整组移除。命令面从 200 条降到 87 条，技能脚手架（`internal/cliskills` 等约 14k 行）与 10 个 internal 包一并删除。

**保留**：`init / on / off / version / next / status / change / task / review / docs / impact / test / compat`。

**已删除**（运行会得到 `unknown command`）：`act`、`bridge`、`config`、`conventions`、`dashboard`、`enforcement`、`eval`、`gate`、`harness`、`health`、`migrate`、`node`、`policy`、`project`、`registry`、`skill`、`skills`、`trace`、`trust`、`update`、`verify`、`workspace`、`worktree`，以及任务下的 `doc-review` / `mutation` / `artifact` / `chain-init` 子命令与 `forge selfcheck`。

```bash
npm i -g @agent_forge/forge@latest   # 应 >= 3.0.1
forge version
forge docs closure                   # 命令面 87 条、drift 0
```

升级后要做三件事：

1. `.forge/` 旧数据保持原样即可——没有 migrate 命令，它不再参与门禁判定；
2. `forge change agents` 重刷 AGENTS.md 托管块（块由运行中的命令树生成）；
3. CI 与脚本里的旧命令按下一节的 2.0 破坏性变化表逐条替换；证据格式没变，旧 change 的证据仍可读。

## ⬆️ 从 1.84 及更早版本升级

1.85 删除了宿主层（hook 执行、用户级 `CLAUDE.md`/`AGENTS.md` 注入、插件、skills 分发），旧版本留在用户目录里的这些残留不会自动消失，需要手工清理（删除前先备份）。

完整的五宿主写入位置表、备份与逐项删除命令、以及「只有看到 `[forge-session]` 横幅才按 forge 行事」这句残留文案为什么会让人误判，已拆到 [docs/migration/upgrade-from-1.84.md](docs/migration/upgrade-from-1.84.md)。

清理后在仓库里跑 `forge status` 应看到 `spec/forge.yaml` 契约；较新的 Claude Code 无 `CLAUDE.md` 时会读 `AGENTS.md`，不确定时用 `forge change agents --pointers claude` 生成指针。

## ⬆️ 从 1.85–1.9x 升级到 2.0

2.0 是破坏性版本：门禁从三道降为两道、change 产物从五份改成三份，并删掉十项检查
（文档闸门、产物链、未用导出扫描、覆盖率回扣、变异抽样、方案先行、二次验证、
自检、需求覆盖、技能决策——以上均已删除，具体命令见下表）。**老仓库不会自动损坏，
但旧流程里的命令、CI 步骤和脚本会失效**，升级时按下面清单改。

### 命令面的破坏性变化

| 旧用法 | 2.0 之后 |
| --- | --- |
| forge task gate task-verify | 门禁已删除；验收只跑 `forge task verify-acceptance` |
| forge task doc-review（--passed/--score 形态已废） | 命令已删除；文档回检并入评审，结论记在 `forge review pass --score N --reviewer <id>` |
| forge task mutation | 命令已删除；抽样变异不再由 forge 做 |
| forge task artifact / forge task chain-init | 命令已删除；产物就是 change 里的 `goal.md` / `change.md` / `acceptance.md` |
| forge selfcheck | 该子命令已删除；用 `forge docs lint <文件>` + `forge change check` |
| forge eval artifact-drill | 子命令已删除；用 `scripts/checks/release-smoke.sh <forge 二进制>`（发布冒烟）或 `forge change check` |
| `forge review pass --note "…"` 刷新基线 / 每轮换新 reviewer | 2.1 起改为**评审者复用**：首轮 `--reviewer <id>` 绑定，之后每轮用同一 id（换新 id 会把旧问题丢掉、永远收敛不了）；确实要换人用 `--rotate-reviewer --reason "…"` 留痕；上一轮未闭合项用 `--findings <清单>` 承接 |
| 五份产物（proposal/design/tasks + acceptance + goal） | 三份：`goal.md`（三段）+ `change.md` + `acceptance.md`；`clarify.md` 记人确认，`tasks.md` 可选 |

forge integrations / forge hook / forge freeze / forge doctor 已删除（1.85），
宿主层（用户级 CLAUDE.md / AGENTS.md 注入、hook 执行、插件、skills 分发）整体不存在了；
升级前如果还留着那些注入，按上一节「从 1.84 及更早版本升级」清掉。`forge init` 仍然存在，
但它现在只写仓库内产物（契约 / changes 目录 / AGENTS 托管块 / 已存在的指针文件），不碰用户目录。

### 每个老项目要做的

```bash
# 0) 先升级二进制
npm i -g @agent_forge/forge@latest --include=optional --registry=https://registry.npmjs.org
forge version                      # 应 >= 3.0.1

# 1) 仓库里：core-only 之后没有 migrate / registry 命令，旧 .forge/ 数据保持原样即可
#    （只是本地历史，不参与门禁判定，也不再有命令读它）
cd <your-repo>

# 2) 刷新仓库的 AGENTS.md 托管块（块由运行中的命令树生成）
forge change agents

# 3) 看进行中的 change 和任务
forge change list
forge status

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

```

旧注册表文件 `~/.forge/projects.json` 只是历史文件，core-only 之后没有命令再读它，可以直接删。

### 升级后的自检

```bash
forge change check                 # 每个仓库：契约与证据自洽，退出码 0/2
forge status                       # 任务管道：应显示两道门禁
```

再在 CI 脚本里搜一遍上一节表格左列的旧命令名，应当没有残留；有的话按表格换成对应替代命令。

## 🤝 贡献

欢迎提 Issue 和 PR。开发时注意：

1. **门禁先行** —— 任何源码变更走 `forge task` 两道门禁（implement → complete，`review pass` 是 complete 的硬前置）；不经任务的改动不会被 task 门禁与完成记录追踪。
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
| [历史评测数据](evals/README.md) | 3.0 前 skill eval 的黄金 case 集存档（评测命令已移除） |

## License

Apache-2.0

---

<div align="center">

<sub>⬆ <a href="#top">回到顶部</a></sub>

</div>
