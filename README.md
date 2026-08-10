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
  <img src="dashboard-render.png" alt="Forge Dashboard 质量看板" width="860"/>
  <p><sub>Forge Dashboard —— 项目级质量趋势可视化（分数走势 / 证据盲区率 / 复发低分维度）</sub></p>
</div>

---

<details>
<summary><b>📖 目录</b></summary>

- [核心功能](#-核心功能)
- [快速开始](#-快速开始)
- [它如何工作](#-它如何工作)
- [定位：Loop Engineering 的验证 / 状态层](#-定位loop-engineering-的验证--状态层)
- [工作流程](#-工作流程)
- [Hook 系统](#-hook-系统)
- [命令参考](#-命令参考)
- [安装](#-安装)
- [贡献](#-贡献)
- [更多文档](#-更多文档)
- [License](#license)

</details>

---

> **AI 写的代码，你放心直接提交吗？**

Forge 在 AI 编码过程中自动插入结构化质量门禁——从任务创建到代码提交，确保每一步产出物都经过验证。配合 Claude Code 的 Hook 系统实现实时拦截，不需要你手动检查。

## ✨ 核心功能

<table>
  <tr>
    <td width="50%" valign="top"><strong>🚦 任务级门禁</strong><br/>每个开发任务走 3 道门禁：实现 → 验证 → 完成，门禁之间有活动检查防止跳阶段。</td>
    <td width="50%" valign="top"><strong>🪝 实时 Hook 拦截</strong><br/>多个内置 Hook，在 AI 写代码的同时自动检查质量、防止绕过（读改前置 / 文件监控 / 高危拦截）。</td>
  </tr>
  <tr>
    <td valign="top"><strong>🛡️ 安全纵深防御</strong><br/>三层防御架构：工具拦截 → 文件监控 → 自身保护。Agent 无法经 bash 绕道篡改。</td>
    <td valign="top"><strong>📊 质量评分</strong><br/>每个任务完成后自动评分，量化 AI 编码质量；deterministic 证据链可审计。</td>
  </tr>
</table>

## 🚀 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 已安装。

```bash
# 安装
npm install -g @agent_forge/forge

# 在项目目录初始化（默认零项目写入）
cd your-project
forge init

# 在 Claude Code 中开始工作
# AI 会自动读取 Forge 生成的 Skill 并驱动门禁流程
```

`forge init` **不在项目目录写任何文件**（不会被 git add 误提交），全部资产落在用户级：

| 路径 | 说明 |
|------|------|
| `~/.forge/projects.json` | 全局项目注册表（forge 项目锚点） |
| `~/.forge/projects/<key>/` | protocol.yml + runtime state（任务状态/hook 参考副本，key=git hash 或路径 hash） |
| `~/.claude/settings.json` | Claude Code hooks（plugin 已装则由 plugin 接管，跳过此文件） |
| `~/.claude/CLAUDE.md`、`~/.codex/AGENTS.md` | 质量协议（备份+追加、条件激活，`forge uninstall --restore` 可回滚） |
| `~/.claude/skills/forge-quality/` | 质量协议 Skill |
| `~/.codex/hooks.json`、`~/.cursor/hooks.json` 等 | 其他 agent 的用户级 hook 接线（按检测到的工具） |

> **团队要 git 共享同一份协议？** 用 `forge init --project`（团队模式）——`.forge/protocol.yml`、`CLAUDE.md`、`AGENTS.md` 等指令资产写入项目目录可提交共享；再跑一次普通 `forge init` 即转回零写入。

> **主要用 Claude Code？** 走 [plugin marketplace](plugins/forge/README.md) 一次性接线用户级 hooks（机器上所有项目共享，连 `~/.claude/settings.json` 都不用动）。

## 🔧 它如何工作

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

每轮 AI 编码循环都被门禁兜底：编译是否通过、断言有没有被弱化、改代码前是否真读过、文件有没有被绕道篡改——循环跑得越快，越需要自动化验证，而不是靠人盯着。

## 🎯 定位：Loop Engineering 的验证 / 状态层

AI 编码是一个循环：写代码 → 运行 → 读反馈 → 修正 → 再写。这个循环由 coding agent（Claude Code、Codex）驱动，**Forge 不替代循环本身**——它补上循环最容易缺的两层：

- **验证层** — 每一轮产出物经门禁检验：编译通过、断言没被弱化、改代码前确实读过代码、文件未被绕道篡改。循环跑得越快，越需要自动化验证兜底，而不是靠人盯着。
- **状态层** — 跨循环的任务状态：3 道门禁（实现 → 验证 → 完成）、活跃任务追踪、门禁历史。"做到哪了 / 是否达标"有持久化、可审计的记录，而不是只活在 agent 的上下文里（上下文一压缩就丢）。

换言之，coding agent 负责**跑循环**，Forge 负责**让每一轮循环产出可信、状态可追**。Forge 不 discovery、不规划需求——那些是循环前端的事；Forge 守的是循环的执行质量。

## 🔧 工作流程

每个开发任务自动走 3 道门禁：

```bash
forge task start --ref feat/add-login --branch --accept "go test ./... :: PASS"   # 创建任务+分支+登记验收标准（--accept 可重复）
forge task start --ref feat/add-login --scope "internal/auth/*.go"                # 声明计划改动白名单（规划前置→可度量契约，advisory 检测 scope-drift）
forge task start --ref feat/frontend --assignee kimi --role frontend --depends-on feat/api   # 创建即分派给 kimi（offered），声明上游依赖 feat/api（DAG 环检测；task-verify/task-complete 在 feat/api 交付前阻断）
# AI 自动完成工作...
forge task gate task-implement    # ✅ 代码实现（advisory：编译/断言提醒，agent 自检）
forge task verify-acceptance      # ✅ 实跑验收标准，记 deterministic 证据（spec-as-gate）
forge task gate task-verify       # ✅ 测试验证
forge task scope show             # 查看声明白名单 + 实时 scope-drift（advisory，不阻塞）
forge task gate task-complete     # ✅ 完成确认
forge task score                  # 查看质量评分
```

<details>
<summary><b>📖 门禁细节：退出码契约 / PlanScope / Cheat-scan</b></summary>

门禁之间有时间和活动检查，防止 AI 跳过阶段直接提交。`task-implement` 的编译/断言检查为 advisory 提醒（由 agent 自检，不阻塞）——forge 技术栈无关，适配 loop engineering。`forge task verify-acceptance` 实跑 `task start --accept` 登记的验收标准（`Run :: Expected`），把 dev-workflow Plan 的验收条件从 plan 文本变成不可伪造的 deterministic 证据——对冲 agent 自述"满足验收"却没真跑的盲区。

**门禁退出码契约**：`forge task gate` 非 0 退出（输出 `BLOCKED:` 前缀）= 硬阻断，必须修复后重跑；零退出但见 `ADVISORY:` 前缀 = 软信号（gate 仍过、已记 checklog，应修不阻断）。按退出码而非文案行动——硬错误的散文易被误读成提醒而跳过。

**PlanScope 白名单（规划前置）**：`task start --scope <glob>`（可重复，或中途 `forge task scope add <glob>` 追加）声明"打算改哪些文件"。`task-verify` 比对实改源码与声明的差集，记一条 `scope-drift` 证据（deterministic，`forge trace` 可见）并 stderr 提醒。全程 **advisory 不阻塞**——变更影响分析召回率仅 ~44%，scope 是 prediction 非 contract，偏差是常态信号而非异常；它把"规划前置"变成可度量、可回顾的契约，正堵在 review 反复出问题的根因上。

**Cheat-scan（机械作弊模式扫描）**：`task-verify` 扫任务新增行（`+` 行），机械检测 4 类 AI 作弊模式——`type-suppression`（`@ts-ignore`/`eslint-disable`/`#[allow]`/`type: ignore`）、`error-swallow`（空 `catch{}`/`except:pass`）、`dead-branch`（`if(false)`/`if(1===2)`）、`comment-only-fix`（某文件新增行全注释零逻辑）——记一条 `cheat-scan` 证据（deterministic，`forge trace` 可见）并 stderr 列出命中。全程 **advisory 不阻塞**：这些模式此前全靠 code-review-gate 的 LLM 子 agent 判断，LLM 每轮对同一 diff 重新采样抓不同子集，是"每轮 review 冒新问题"的体感来源；抽到 deterministic 后，机械模式一次判准，LLM-reviewer 退到只做语义判断（设计/架构/mock 是否幻觉）。`comment-only-fix` 是启发式（severity=low，纯文档任务可能误报）。

</details>

## 🪝 Hook 系统

Forge 通过 Claude Code 的 Hook 机制实现实时质量检查。三层纵深防御，监控的是文件而非工具：

```
Layer 1: PreToolUse 快速拦截
  ├─ task-guard: Write/Edit → 检查任务状态 + 自保护（forge 配置层）
  └─ bash-guard: Bash → 检测写文件模式

Layer 2: PostToolUse 文件监控
  └─ file-sentinel: Bash → 对比执行前后 git 状态，未授权变更自动 revert

Layer 3: 会话结束验证
  └─ task-verify: 检查任务完成度 + 主分支保护 + 自身版本
```

Agent 无法通过 `node -e "fs.writeFileSync()"`、`cat > file`、直接编辑 task JSON 等方式绕过——bash-guard 拦截工具层，file-sentinel 监控文件层，task-guard 保护配置层。

<details>
<summary><b>📖 内置 Hook 完整清单（18 个）</b></summary>

| Hook | 触发时机 | 功能 |
|------|----------|------|
| **task-guard** | Write/Edit 前 | 无活跃任务时 WARN（仅 `.forge/*`/`.claude/settings*` 自保护 FAIL——此类项目级文件只在团队模式/老项目存在），保护 Forge 配置不被篡改 |
| **freeze-guard** | Write/Edit 前 | `forge freeze <路径>...` 激活后硬阻断冻结路径之外的 Write/Edit——「只改这里别动其他」的 session 级硬护栏（on-demand-guards /freeze 的 forge 侧落地）；多路径、相对路径归一化、Windows 大小写不敏感；排在 task-guard 之前优先判定；`forge freeze --off` 解除 |
| **read-before-edit** | Write/Edit 前（活跃任务内） | 编辑本会话未 Read 过的现存源文件 → 硬阻断（`BLOCKED`）。Edit 需精确匹配旧文本，未读即凭记忆盲改——old_string 撞中即错改入库，先 Read 再 Edit。豁免新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（降 evidence 强度到 Weak）。reads-log 落盘随会话存活，压缩后仍累计 |
| **assertion-check** | Write/Edit 前 | 检测断言弱化（t.Fatal → t.Log、assert! 被删除等），advisory 提醒不阻塞（agent 自检） |
| **bash-guard** | Bash 前 | 检测命令中的写文件模式（writeFile、cat >、sed -i 等），无任务时 WARN（源码随后被 file-sentinel 隔离） |
| **hazard-guard** | Bash 前 | 高危命令（`rm -rf`、`git push --force`、`DROP TABLE/SCHEMA`、`TRUNCATE`、`GRANT ALL`、`kubectl delete`、`docker system prune`、无 WHERE 的 `DELETE/UPDATE`、解释器内联删除如 `python -c "os.remove(...)"` 等）human-in-the-loop 拦截：block + 指引用户确认 → `forge hazard confirm` 登记 5min 限时标记 → 重试放行（confirm 链是唯一放行路径，`FORGE_ALLOW_HAZARD` 已移除） |
| **auto-compile** | Write/Edit 后 | advisory 提醒用对应技术栈编译命令自检（go build / cargo check / mvn / tsc 等），不强制编译 |
| **workflow-test-guard** | Write/Edit 后 | 改 `.github/workflows/*.yml` 后自动跑 `internal/ci` 守护测试，把"沙盒异常"即时反馈给 agent（不依赖 CI 兜底），是 release.yml test→goreleaser→npm needs 链的实时守护层 |
| **file-sentinel** | Bash 后 | 监控文件变更，未授权修改隔离到 DataDir/quarantine/（`forge data-dir` 查看路径，可恢复，不删除） |
| **tool-track** | Read 后 | 静默记录 Read 调用到 toollog，供 task-verify 的 read-before-edit 门禁判断（agent 是否先读代码再改） |
| **task-verify** | 会话结束 | advisory：任务门禁/主分支保护到 stderr+checklog（不阻塞会话结束） |
| **review-stop** | 会话结束 | code-review-gate 自动挡：未审源码变更 block 会话结束。task 模式不重复拦（task-complete 门禁 ReviewPassed 硬前置已强制），非 task 模式按 diff stamp 决策；并发会话检测——其他 session 有活跃任务时放行（调研 session 不被拦） |
| **skill-scan** | 会话开始 | advisory：扫描 ~/.claude/skills 安全性（forge audit 19 规则），补 install 门控缺口（手动 clone/junction/git pull 进入的 skill），全局 hook 不依赖 forge project |
| **mcp-scan** | 会话开始 | advisory：扫描项目级 `.mcp.json` 的 server 配置（管道执行/任意包执行 npx·uvx·dlx·bunx/内联代码/非 https URL/env 明文凭证），补 skill-scan 盲区（攻击者可经 PR 植入恶意 server，clone 即自动连接）；只审 config 层，runtime tool description 注入（Tool Poisoning）不在能力内，全局 hook |
| **init-suggest** | 会话开始 | advisory：检测到未启用 forge 的 git 项目时，首次提示 agent 询问是否启用（用户拒绝→`forge suggest decline` 永久静默；设 `FORGE_AUTO_INIT=1` 处处自动 init——v1.22 起 init 零项目写入，不再对项目产生任何文件变更），全局 hook，补"每项目手动 init"缺口，实现一次安装后项目自动登记 |
| **task-resume** | 会话开始 | advisory：自动注入活跃任务的接续上下文（目标/计划/决策/阻塞/门禁进度/git 已改未提交）+ 锚定当前 session——接手方冷启动即知任务在哪一步，无需手动 forge task resume；无活跃任务静默；项目级 hook |
| **compact-resume** | 压缩后（claude-code only） | PostCompact 时设 `ResumeStale=true` 标志（PostCompact 不在 additionalContext 注入点，只设标志等下个 prompt 重注入），context-rot 抗机制根治层·设标志半边 |
| **resume-reinject** | 用户提交时（claude-code only） | 检测 `ResumeStale=true`（刚压缩过）→ 输出完整接续上下文并清标志。补 task-resume 缺口（SessionStart 只注入一次，会话中途压缩不补），context-rot 抗机制根治层·重注入半边 |

</details>

## 📋 命令参考

<details>
<summary><b>🔧 项目管理</b></summary>

| 命令 | 说明 |
|------|------|
| `forge init` | 初始化项目（默认**零项目写入**：登记全局注册表 `~/.forge/projects.json`，hooks/指令/skill 全在用户级，protocol.yml + runtime state 在 `~/.forge/projects/<key>/`；`--project` 团队模式把指令资产写项目目录供 git 共享；旧的 `--mode` 标志已废弃为 no-op） |
| `forge status [--json]` | 查看项目状态（任务管道 + 质量信号） |
| `forge verify` | 项目完整性检查 + 回归测试 |
| `forge update [--plugin]` | 自更新到最新版本；加 `--plugin` 在 binary 更新后打印 plugin marketplace 重装指引（marketplace 镜像同步 hook 时建议重装） |
| `forge suggest decline/status/reset` | 管理 init-suggest hook 的项目 init 提示状态（decline 永久静默当前项目 / status 查看 / reset 清除重新提示） |
| `forge uninstall [--restore]` | 一键反装：剥除全部用户级 hooks（claude/codex/cursor/windsurf/opencode/kimi/reasonix）+ 用户级指令段（CLAUDE.md/AGENTS.md/global_rules.md）+ forge-quality skill + 清 npm global `@agent_forge/forge` + 删 init-suggest 标记（默认 `~/.forge/.init-suggested/`，设 `FORGE_DATA_HOME` 时落该根下）；`--restore` 把用户级文件回滚到 forge 修改前字节（备份在 `~/.forge/backups/`）；plugin 卸载须在 agent CLI 内交互运行（不可脚本化） |
| `forge migrate [--dry-run] [--force]` | 把旧 `.forge/` runtime state（tasks/gates/checklog/toollog/act/sessions/quarantine/active-task-ref 等）迁到用户级 DataDir（`~/.forge/projects/<key>/`）——升级到 runtime state 外迁版本后的迁移路径；未改过的 `.forge/protocol.yml` 由 autoSync 自动迁 DataDir，用户改过的保留为团队共享覆盖层；幂等，`--dry-run` 预览，`--force` 覆盖 DataDir 已有同名 |
| `forge registry prune` | 精简全局注册表 `~/.forge/projects.json`——移除项目目录已不存在的死路径与重复条目（项目移走/删除/测试残留），原子写回。registry.List 读时惰性精简但只在 `forge dashboard --global` 触发（启 web 阻塞），本命令给不启 web 的主动清理入口 |

</details>

<details>
<summary><b>🚦 任务管理</b></summary>

| 命令 | 说明 |
|------|------|
| `forge task start --ref <type/desc> --branch` | 创建任务（自动创建分支） |
| `forge task status` | 查看当前任务门禁状态 |
| `forge task list` | 列出所有任务 |
| `forge task mine [--agent <agent>] [--role <role>] [--all-projects] [--blocked] [--json]` | 列出分派给当前/指定 agent 的任务（`--all-projects` 全仓扫描按项目分组；`--blocked` 仅被依赖阻塞的，标注卡在哪环 [status, gate 进度 passed/total]） |
| `forge task gate <gate-id>` | 验证单道任务门禁 |
| `forge task verify-acceptance` | 实跑验收标准（task start --accept 登记），记 deterministic 证据 |
| `forge task scope add <glob>` | 追加计划改动文件到白名单（支持中途迭代） |
| `forge task scope show` | 查看声明的白名单 + 实时 scope-drift（advisory，不阻塞） |
| `forge task complete` | 标记任务完成（自动评分） |
| `forge task abort [--ref <ref>] [--cascade\|--detach-deps]` | 中止并删除任务（清理 ghost/卡住任务，不评分；存在反向依赖时默认仅提示，`--cascade` 递归中止所有依赖它的任务，`--detach-deps` 从依赖它的任务移除该依赖边） |
| `forge task score` | 查看任务质量评分 |
| `forge task resume [--ref <ref>]` | 拉回任务接续上下文（目标/计划/决策/阻塞/参与工具+门禁进度+git 已改），跨会话/跨工具秒级恢复 |
| `forge task context [--ref <ref>]` | 只读查看接续上下文（resume 的不改 state 别名） |
| `forge task decide --content` | 记录已确认决策（持久化进 task，跨会话/跨工具不再推翻） |
| `forge task next <step>` | 追加下一步（可多条） |
| `forge task block --content/--resolve <id>` | 登记阻塞或解决阻塞（open→resolved） |
| `forge task finding --content/--resolve <id>` | 记录跨工具发现（带来源工具）或标 fixed |
| `forge task attach --ref --tool` | 锚定 session+工具到 task（跨工具多向锚定：pi 起、claude-code 接） |
| `forge task assign --ref <ref> --to <agent> [--role] [--by]` | 把任务分派给指定 agent（offered 起步，编排器侧；未知 agent 警告但接受） |
| `forge task claim --ref <ref> [--as <agent>]` | 工作方认领分派给自己的任务（offered→claimed，自动锚定 session） |
| `forge task deliver --ref <ref>` | 工作方交付任务（claimed→delivered，交回编排器） |
| `forge task mine [--agent <agent>] [--role] [--blocked] [--json]` | 列出分派给当前/指定 agent 的任务（--blocked 只看被上游依赖卡住的，pending_deps 带未交付上游 ref） |
| `forge task question --ref <ref> --content <text>` | 工作方回抛问题（claimed→input-required，暂停等编排器/人答复） |
| `forge task answer --ref <ref> [--content <text>]` | 编排器答复回抛（input-required→claimed，答复记入 Decisions；空答复仅恢复 claimed） |
| `forge task fail --ref <ref> --reason <text>` | 工作方标记任务失败（claimed→failed，记录原因） |
| `forge task cancel --ref <ref> --reason <text>` | 编排器撤回分派（offered/claimed/input-required→canceled，记录原因） |
| `forge task reopen --ref <ref> --reason <text>` | 交付后重开（delivered→claimed，交付后发现 bug） |
| `forge task export --ref <ref> [-o\|--output file] [--include-checklog] [--redact]` | 把任务导出为跨机器 JSON Bundle（task state 存于用户级 DataDir 不随仓库走，跨机器交接需此载体；--include-checklog 附带证据链；--redact 抹除 issue/agent/commit/证据供对外分享） |
| `forge task import --file <bundle> [--force\|--merge]` | 从 Bundle 导入任务到本地（导入 session 标记幽灵仅溯源；默认同 ref 拒绝，--force 覆盖，--merge 按 ID 并集协作记录；含 checklog 则回放进本地 trace） |
| `forge task health [--json]` | 扫描全 project 上浮僵尸/死锁/长期未答复任务（只读告警，不改状态）：offered>7d / claimed>TTL（无 checklog 活动）/ input-required>7d / abandoned_count≥2 标黄，DependsOn 指向 failed/canceled/缺失的死锁链与环主动报；与 mine/看板共享同一检测真相源 |

</details>

<details>
<summary><b>🔍 代码审查 / 高危命令 / Act 反馈（自动挡）</b></summary>

**代码审查门禁**：`forge review` 让 code-review-gate 从"靠手动唤起"变成自动挡——task 流程下 task-complete 门禁强制 ReviewPassed 前置（提交前必审）；非 task 流程下 Stop hook 自动拦截未审的源码变更。误触发已防护：纯文档/配置/生成物变更、无变更、commit 后干净工作区不触发；同一 diff 反复未审最多 block 3 次后 advisory 放行（防 Stop 死循环）。审查由独立只读子 agent 执行（防自审盲区），见 `code-review-gate` skill。

| 命令 | 说明 |
|------|------|
| `forge review pass` | 标记当前变更已通过 code-review-gate（task 模式写任务状态，否则写分支 stamp） |
| `forge review gate` | 判定当前是否需要审查（Stop hook 调用；exit 0=放行，1=需审 block） |
| `forge review status` | 显示当前审查状态 |

**高危命令 human-in-the-loop**：`forge hazard` 让高危命令拦截从 session 级 skill 变成 always-on 自动挡——hazard-guard hook（PreToolUse Bash）检测 `rm -rf` / `git push --force` / `git reset --hard` / `DROP DATABASE|TABLE|SCHEMA` / `TRUNCATE` / `GRANT ALL` / `kubectl delete` / `docker system prune` / `shred` / 无 WHERE 的 `DELETE|UPDATE` 等 → block 并指引 agent 获用户明确确认 → `forge hazard confirm` 登记 5min 限时标记 → 重试放行。HITL 而非硬 block：合法高危操作（删 build 产物）确认后能继续；`FORGE_ALLOW_HAZARD` env 豁免已移除（可被 agent 自我放行滥用），confirm 链是唯一放行路径。

| 命令 | 说明 |
|------|------|
| `forge hazard confirm <命令>` | 登记一次高危命令确认（5min 内同命令重试放行） |
| `forge hazard status` | 列出当前有效确认及剩余时间 |

**写入范围冻结**：`forge freeze` 把 on-demand-guards 的 /freeze 目录锁定从「agent 每回合自检」的 prompt 型护栏落地为真 hook——激活后 freeze-guard hook（PreToolUse Write|Edit，排在 task-guard 之前优先判定）硬阻断所有冻结路径之外的写入，长会话/压缩后不漂移。支持多路径、相对路径（相对当前目录归一化）、Windows 大小写不敏感比较。

| 命令 | 说明 |
|------|------|
| `forge freeze <路径>...` | 激活 freeze（可多路径；再次激活即替换范围） |
| `forge freeze --off` | 解除 freeze（幂等） |
| `forge freeze --status` | 查看当前 freeze 状态 |

**Act 反馈臂（证据驱动结论）**：`forge task complete` 时把本任务的证据驱动结论（评分 + 证据强度 + 验收通过率 + 低分维度）落盘到 `~/.forge/projects/<项目key>/act/conclusions.jsonl`，喂给 `session-retrospective`。证据弱（Unverified/Weak）或低分（<70）的结论标 RetrospectiveNudge——对冲"高分但没真验证"的 LLM-judge 盲区。

| 命令 | 说明 |
|------|------|
| `forge act show [--ref <ref>]` | 查看最新（或指定）任务结论（含 skill 触达画像——该 task 期间触发了哪些 skill） |
| `forge act list [--json]` | 列出所有任务结论 |
| `forge act nudge` | 最新结论有回顾 nudge 时输出一行（否则静默）——供 task-verify 会话结束 hook 消费 |

</details>

<details>
<summary><b>🧠 Skill 治理</b></summary>

分发内置 canonical skill 库到各 coding agent，并守护 skill 质量（规范 + 安全）。

| 命令 | 说明 |
|------|------|
| `forge skills install` | 分发 skill 到全局/项目目标目录（link/copy） |
| `forge skills list` | 列出 canonical skill 库中的 skill |
| `forge skills audit` | 19 条安全规则审查（prompt 注入/数据外发/危险代码） |
| `forge skills drift-check` | 检测分发分叉（dry-run，不写） |
| `forge skills validate` | R1-R9 规范校验 |
| `forge skills adapters` | 部署 skill-routing adapter（pi/claude/cursor/routes.json） |
| `forge skills usage` | 使用度量分析（热门 skill + undertrigger 候选） |
| `forge skills effectiveness` | 技能命中×任务成效关联（命中数/task数/avg分/弱占比，agent-neutral） |
| `forge skills eval-gen [--save] [--cases-only]` | 生成 eval 清单；`--save`/`--cases-only` 额外落结构化 case 集（回归闭环基准） |
| `forge skills eval-record --skill X --from <file/->` | 回填一次 eval run（agent dispatch 跑完每个 prompt 后整批提交，归一化+判定+算 health） |
| `forge skills eval-report --skill X` | latest run vs baseline 回归报告（regression 三态 + pass-rate delta + 可比性） |
| `forge skills eval-baseline --skill X` | 标记 baseline run（回归基准，显式人工决策） |

</details>

<details>
<summary><b>📊 可观测与维护</b></summary>

| 命令 | 说明 |
|------|------|
| `forge health [--json]` | 项目级质量趋势——聚合所有任务结论（分数走势/证据盲区率/复发低分维度，task→project 粒度联动） |
| `forge trace <task-ref>` | 查看任务的完整质量事件时间线（checklog + toollog + token） |
| `forge dashboard [--global] [--port <n>] [--no-open]` | 本地质量看板——起 HTTP 服务把分数走势/证据盲区率/复发低分维度/最近任务渲染成图形（localhost 只读，自动开浏览器，Ctrl+C 退出）。加 `--global` 聚合 `~/.forge/projects.json` 登记的全部项目（`forge init` 自登记），跨项目比对；项目目录被移走/删除后注册表条目自动淡出（读时惰性精简），不留幽灵路径 |
| `forge sync [--force]` | 同步 forge 资产到当前二进制版本（用户级 hooks/指令/skill 重生成 + 存量项目级残留收敛） |
| `forge clone check` | 检测文件代码克隆 |
| `forge plugin pack [--out <dir>]` | 生成多 host plugin pack（.claude-plugin/.cursor-plugin marketplace + plugins/\<name\>/ 树：claude manifest + reasonix native manifest + 每 host 安装 README），让各 agent 一键 `plugin install forge` 跨工具接线（薄 manifest + 共享内容，单仓即 marketplace） |
| `forge plugin status` | 报告 forge plugin 是否在 user-level 已装（exit 0=已装，非零=未装；供 init-suggest hook / 脚本检测） |
| `forge plugin dedupe [dir] [--keep-empty]` | plugin 已装时清理 project-level 重复 hooks + 旧项目 .mcp.json forge server 残留，并清理 user-level `settings.local.json` 的重复 forge hooks；幂等 no-op；init-suggest SessionStart 自动调用（传 `--keep-empty` 保留项目 `settings.local.json` 为 `{}`）；user-level 始终保留文件壳（绝不删用户全局配置）；手动不传则项目级清完删空文件 |

</details>

## 📦 安装

```bash
# npm（推荐）
npm install -g @agent_forge/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/Forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

<details>
<summary><b>📖 通过 Claude Code plugin marketplace（用户级，一次性接线）</b></summary>

若主要用 Claude Code，可走 plugin marketplace 一次性接线用户级 hooks（机器上所有项目共享，连 `~/.claude/settings.json` 都不用动）：

```
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge
```

仍需 `npm install -g @agent_forge/forge` 装二进制（hooks 都 spawn forge），并在每个项目 `forge init` 登记（v1.22 起零项目写入——协议与 runtime state 全在用户级 `~/.forge/projects/<key>/`，只对用户级配置生效）。plugin 已装时 hooks 由 plugin.json 全机器接管，`forge init` 跳过自己的 settings.json 注册；存量老项目残留的旧版项目级写入（`.forge/hooks/`、`.claude/settings.local.json` 的 forge hooks、CLAUDE.md/AGENTS.md 的 forge 段）由 autoSync 与 init-suggest SessionStart hook 自动收敛。完整三步与各 host 差异见 `plugins/forge/README.md`。

</details>

## 🤝 贡献

欢迎提 Issue 和 PR。开发时注意：

1. **门禁先行** —— 任何源码变更走 `forge task` 三门禁（implement → verify → complete），不经任务的改动不被质量评分追踪。
2. **注释双语** —— Go godoc 采用形式 A（英文段 → 空 `//` → 中文段），中文不删、不单语；行内注释与字符串字面量不动。
3. **审查闭环** —— 提交前派独立只读子 agent 跑 code-review-gate 双轨（AI 作弊 + 工程规范），`forge review pass` 标记后才能过 task-complete 门禁。
4. **提交纪律** —— 只提交源码变更；排除 `docs/`、设计文档、`.claude/`、`.forge/` 工作目录。

详见 [质量协议](.claude/CLAUDE.md)。

## 📚 更多文档

| 文档 | 说明 |
|------|------|
| [中文使用指南](READMEs/README.zh-CN.md) | 面向国内用户的安装 / 日常 / 多宿主精简指南 |
| [Plugin 安装详解](plugins/forge/README.md) | 多 host plugin marketplace 三步接线与各 host 差异 |
| [项目主页](homepage/index.md) | 一分钟简介 + 核心能力速览 |
| [质量协议](.claude/CLAUDE.md) | Forge 质量协议全文（任务工作流 / 门禁 / 安全机制） |

## License

Apache-2.0

---

<div align="center">

<sub>⬆ <a href="#top">回到顶部</a></sub>

</div>
