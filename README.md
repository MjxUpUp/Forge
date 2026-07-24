# Forge — AI 开发质量门禁引擎

AI 写的代码，你放心直接提交吗？

Forge 在 AI 编码过程中自动插入结构化质量门禁——从任务创建到代码提交，确保每一步产出物都经过验证。配合 Claude Code 的 Hook 系统实现实时拦截，不需要你手动检查。

## 核心功能

- **任务级门禁** — 每个开发任务走 3 道门禁：实现 → 验证 → 完成
- **实时 Hook 拦截** — 多个内置 Hook，在 AI 写代码的同时自动检查质量、防止绕过
- **安全纵深防御** — 三层防御架构：工具拦截 → 文件监控 → 自身保护
- **质量评分** — 每个任务完成后自动评分，量化 AI 编码质量
- **MCP 工具接口** — stdio MCP server，让 agent 结构化调用门禁与任务状态，不必 parse CLI 文本

## 定位：Loop Engineering 的验证 / 状态层

AI 编码是一个循环：写代码 → 运行 → 读反馈 → 修正 → 再写。这个循环由 coding agent（Claude Code、Codex）驱动，**Forge 不替代循环本身**——它补上循环最容易缺的两层：

- **验证层** — 每一轮产出物经门禁检验：编译通过、断言没被弱化、改代码前确实读过代码、文件未被绕道篡改。循环跑得越快，越需要自动化验证兜底，而不是靠人盯着。
- **状态层** — 跨循环的任务状态：3 道门禁（实现 → 验证 → 完成）、活跃任务追踪、门禁历史。"做到哪了 / 是否达标"有持久化、可审计的记录，而不是只活在 agent 的上下文里（上下文一压缩就丢）。

换言之，coding agent 负责**跑循环**，Forge 负责**让每一轮循环产出可信、状态可追**。Forge 不 discovery、不规划需求——那些是循环前端的事；Forge 守的是循环的执行质量。

## 快速开始

需要 [Claude Code](https://docs.anthropic.com/en/docs/claude-code) 已安装。

```bash
# 安装
npm install -g @agent_forge/forge

# 在项目目录初始化
cd your-project
forge init

# 在 Claude Code 中开始工作
# AI 会自动读取 Forge 生成的 Skill 并驱动门禁流程
```

初始化后 Forge 会创建：
- `.forge/` — Hook 脚本、任务状态、协议配置
- `.claude/settings.local.json` — Hook 集成配置
- `.claude/CLAUDE.md` — 质量协议引用
- `.claude/skills/` — 质量协议 Skill

## 工作流程

### 任务级门禁

每个开发任务自动走 3 道门禁：

```bash
forge task start --ref feat/add-login --branch --accept "go test ./... :: PASS"   # 创建任务+分支+登记验收标准（--accept 可重复）
forge task start --ref feat/add-login --scope "internal/auth/*.go"                # 声明计划改动白名单（规划前置→可度量契约，advisory 检测 scope-drift）
# AI 自动完成工作...
forge task gate task-implement    # ✅ 代码实现（advisory：编译/断言提醒，agent 自检）
forge task verify-acceptance      # ✅ 实跑验收标准，记 deterministic 证据（spec-as-gate）
forge task gate task-verify       # ✅ 测试验证
forge task scope show             # 查看声明白名单 + 实时 scope-drift（advisory，不阻塞）
forge task gate task-complete     # ✅ 完成确认
forge task score                  # 查看质量评分
```

门禁之间有时间和活动检查，防止 AI 跳过阶段直接提交。`task-implement` 的编译/断言检查为 advisory 提醒（由 agent 自检，不阻塞）——forge 技术栈无关，适配 loop engineering。`forge task verify-acceptance` 实跑 `task start --accept` 登记的验收标准（`Run :: Expected`），把 dev-workflow Plan 的验收条件从 plan 文本变成不可伪造的 deterministic 证据——对冲 agent 自述"满足验收"却没真跑的盲区。

**门禁退出码契约**：`forge task gate` 非 0 退出（输出 `BLOCKED:` 前缀）= 硬阻断，必须修复后重跑；零退出但见 `ADVISORY:` 前缀 = 软信号（gate 仍过、已记 checklog，应修不阻断）。按退出码而非文案行动——硬错误的散文易被误读成提醒而跳过。

**PlanScope 白名单（规划前置）**：`task start --scope <glob>`（可重复，或中途 `forge task scope add <glob>` 追加）声明"打算改哪些文件"，对应 Copilot Workspace plan / Terraform desired state。`task-verify` 比对实改源码与声明的差集，记一条 `scope-drift` 证据（deterministic，`forge trace` 可见）并 stderr 提醒。全程 **advisory 不阻塞**——变更影响分析召回率仅 ~44%（PASTE），scope 是 prediction 非 contract，偏差是常态信号而非异常；它把"规划前置"变成可度量、可回顾的契约，正堵在 review 反复出问题的根因上。

**Cheat-scan（机械作弊模式扫描）**：`task-verify` 扫任务新增行（`+` 行），机械检测 4 类 AI 作弊模式——`type-suppression`（`@ts-ignore`/`eslint-disable`/`#[allow]`/`type: ignore`）、`error-swallow`（空 `catch{}`/`except:pass`）、`dead-branch`（`if(false)`/`if(1===2)`）、`comment-only-fix`（某文件新增行全注释零逻辑）——记一条 `cheat-scan` 证据（deterministic，`forge trace` 可见）并 stderr 列出命中。全程 **advisory 不阻塞**：这些模式此前全靠 code-review-gate 的 LLM 子 agent 判断，LLM 每轮对同一 diff 重新采样抓不同子集，是"每轮 review 冒新问题"的体感来源；抽到 deterministic 后，机械模式一次判准，LLM-reviewer 退到只做语义判断（设计/架构/mock 是否幻觉）。`comment-only-fix` 是启发式（severity=low，纯文档任务可能误报）。

## Hook 系统

Forge 通过 Claude Code 的 Hook 机制实现实时质量检查：

| Hook | 触发时机 | 功能 |
|------|----------|------|
| **task-guard** | Write/Edit 前 | 无活跃任务时 WARN（仅 `.forge/*` 自保护文件 FAIL），保护 Forge 配置不被篡改 |
| **read-before-edit** | Write/Edit 前（活跃任务内） | 编辑本会话未 Read 过的现存源文件 → 硬阻断（`BLOCKED`）。Edit 需精确匹配旧文本，未读即凭记忆盲改——old_string 撞中即错改入库，先 Read 再 Edit。豁免新建文件/测试文件/非源码；批量重构逃生 `forge task override --work-activity disable`（降 evidence 强度到 Weak）。reads-log 落盘随会话存活，压缩后仍累计 |
| **assertion-check** | Write/Edit 前 | 检测断言弱化（t.Fatal → t.Log、assert! 被删除等），advisory 提醒不阻塞（agent 自检） |
| **bash-guard** | Bash 前 | 检测命令中的写文件模式（writeFile、cat >、sed -i 等），无任务时 WARN（源码随后被 file-sentinel 隔离） |
| **hazard-guard** | Bash 前 | 高危命令（`rm -rf`、`git push --force`、`DROP TABLE/SCHEMA`、`TRUNCATE`、`GRANT ALL`、`kubectl delete`、`docker system prune`、无 WHERE 的 `DELETE/UPDATE` 等）human-in-the-loop 拦截：block + 指引用户确认 → `forge hazard confirm` 登记 5min 限时标记 → 重试放行；测试/CI 设 `FORGE_ALLOW_HAZARD=1` 跳过 |
| **auto-compile** | Write/Edit 后 | advisory 提醒用对应技术栈编译命令自检（go build / cargo check / mvn / tsc 等），不强制编译 |
| **workflow-test-guard** | Write/Edit 后 | 改 `.github/workflows/*.yml` 后自动跑 `internal/ci` 守护测试，把"沙盒异常"即时反馈给 agent（不依赖 CI 兜底），是 release.yml test→goreleaser→npm needs 链的实时守护层 |
| **file-sentinel** | Bash 后 | 监控文件变更，未授权修改隔离到 DataDir/quarantine/（`forge data-dir` 查看路径，可恢复，不删除） |
| **tool-track** | Read 后 | 静默记录 Read 调用到 toollog，供 task-verify 的 read-before-edit 门禁判断（agent 是否先读代码再改） |
| **task-verify** | 会话结束 | advisory：任务门禁/主分支保护到 stderr+checklog（不阻塞会话结束） |
| **review-stop** | 会话结束 | code-review-gate 自动挡：未审源码变更 block 会话结束。task 模式不重复拦（task-complete 门禁 ReviewPassed 硬前置已强制），非 task 模式按 diff stamp 决策 |
| **skill-scan** | 会话开始 | advisory：扫描 ~/.claude/skills 安全性（forge audit 19 规则），补 install 门控缺口（手动 clone/junction/git pull 进入的 skill），全局 hook 不依赖 forge project |
| **mcp-scan** | 会话开始 | advisory：扫描项目级 `.mcp.json` 的 server 配置（管道执行/任意包执行 npx·uvx·dlx·bunx/内联代码/非 https URL/env 明文凭证），补 skill-scan 盲区（攻击者可经 PR 植入恶意 server，clone 即自动连接）；只审 config 层，runtime tool description 注入（Tool Poisoning）不在能力内，全局 hook |
| **init-suggest** | 会话开始 | advisory：检测 git 项目无 `.forge/` 时，首次提示 agent 询问是否启用 forge（用户拒绝→`forge suggest decline` 永久静默；设 `FORGE_AUTO_INIT=1` 处处自动 init，注意 `forge init` 会写入 `.forge/`、`CLAUDE.md`/`AGENTS.md`、`.claude/settings.local.json`、skills——会对所在项目产生文件变更），全局 hook，补"每项目手动 init"缺口，实现一次安装后项目级资产自动就位 |
| **task-resume** | 会话开始 | advisory：自动注入活跃任务的接续上下文（目标/计划/决策/阻塞/门禁进度/git 已改未提交）+ 锚定当前 session——接手方冷启动即知任务在哪一步，无需手动 forge task resume；无活跃任务静默；项目级 hook |
| **compact-resume** | 压缩后（claude-code only） | PostCompact 时设 `ResumeStale=true` 标志（PostCompact 不在 additionalContext 注入点，只设标志等下个 prompt 重注入），context-rot 抗机制根治层·设标志半边 |
| **resume-reinject** | 用户提交时（claude-code only） | 检测 `ResumeStale=true`（刚压缩过）→ 输出完整接续上下文并清标志。补 task-resume 缺口（SessionStart 只注入一次，会话中途压缩不补），context-rot 抗机制根治层·重注入半边 |

### 安全架构

三层纵深防御，监控的是文件而非工具：

```
Layer 1: PreToolUse 快速拦截
  ├─ task-guard: Write/Edit → 检查任务状态 + 保护 .forge/*
  └─ bash-guard: Bash → 检测写文件模式

Layer 2: PostToolUse 文件监控
  └─ file-sentinel: Bash → 对比执行前后 git 状态，未授权变更自动 revert

Layer 3: 会话结束验证
  └─ task-verify: 检查任务完成度 + 主分支保护 + 自身版本
```

Agent 无法通过 `node -e "fs.writeFileSync()"`、`cat > file`、直接编辑 task JSON 等方式绕过——bash-guard 拦截工具层，file-sentinel 监控文件层，task-guard 保护配置层。

## 命令参考

### 项目管理

| 命令 | 说明 |
|------|------|
| `forge init` | 初始化项目（生成 `.forge/` 资产、Hook、质量协议 Skill；旧的 `--mode` 标志已废弃为 no-op） |
| `forge status [--json]` | 查看项目状态（任务管道 + 质量信号） |
| `forge verify` | 项目完整性检查 + 回归测试 |
| `forge update [--plugin]` | 自更新到最新版本；加 `--plugin` 在 binary 更新后打印 plugin marketplace 重装指引（marketplace 镜像同步 hook 时建议重装） |
| `forge suggest decline/status/reset` | 管理 init-suggest hook 的项目 init 提示状态（decline 永久静默当前项目 / status 查看 / reset 清除重新提示） |
| `forge uninstall` | 一键反装：清 npm global `@agent_forge/forge` + 删 init-suggest 标记（默认 `~/.forge/.init-suggested/`，设 `FORGE_DATA_HOME` 时落该根下）；plugin 卸载须在 agent CLI 内交互运行（不可脚本化） |
| `forge migrate [--dry-run] [--force]` | 把旧 `.forge/` runtime state（tasks/gates/checklog/toollog/act/sessions/quarantine/active-task-ref 等）迁到用户级 DataDir（`~/.forge/projects/<key>/`）——升级到 runtime state 外迁版本后的迁移路径；项目配置（hooks/protocol.yml 等）不迁仍留 `.forge/`；幂等，`--dry-run` 预览，`--force` 覆盖 DataDir 已有同名 |
| `forge registry prune` | 精简全局注册表 `~/.forge/projects.json`——移除 `.forge/` 不存在的死路径与重复条目（项目移走/删除/测试残留），原子写回。registry.List 读时惰性精简但只在 `forge dashboard --global` 触发（启 web 阻塞），本命令给不启 web 的主动清理入口 |
| `forge workflow scaffold [--out PATH]` | 生成 WORKFLOW.md scaffold——proof-of-work 闭环在 per-issue workspace 的驱动流程接口，供 spawn 式编排器（Symphony 类）读取自动驱动每个外部 issue（forge_task_start --from_issue → 三门禁 → forge review pass → forge_task_proof → forge_task_complete）。省略 --out 走 stdout |

### 任务管理

| 命令 | 说明 |
|------|------|
| `forge task start --ref <type/desc> --branch` | 创建任务（自动创建分支） |
| `forge task status` | 查看当前任务门禁状态 |
| `forge task list` | 列出所有任务 |
| `forge task gate <gate-id>` | 验证单道任务门禁 |
| `forge task verify-acceptance` | 实跑验收标准（task start --accept 登记），记 deterministic 证据 |
| `forge task scope add <glob>` | 追加计划改动文件到白名单（支持中途迭代） |
| `forge task scope show` | 查看声明的白名单 + 实时 scope-drift（advisory，不阻塞） |
| `forge task complete` | 标记任务完成（自动评分） |
| `forge task abort [--ref <ref>]` | 中止并删除任务（清理 ghost/卡住任务，不评分） |
| `forge task score` | 查看任务质量评分 |
| `forge task resume [--ref <ref>]` | 拉回任务接续上下文（目标/计划/决策/阻塞/参与工具+门禁进度+git 已改），跨会话/跨工具秒级恢复 |
| `forge task context [--ref <ref>]` | 只读查看接续上下文（resume 的不改 state 别名） |
| `forge task decide --content` | 记录已确认决策（持久化进 task，跨会话/跨工具不再推翻） |
| `forge task next <step>` | 追加下一步（可多条） |
| `forge task block --content/--resolve <id>` | 登记阻塞或解决阻塞（open→resolved） |
| `forge task finding --content/--resolve <id>` | 记录跨工具发现（带来源工具）或标 fixed |
| `forge task attach --ref --tool` | 锚定 session+工具到 task（跨工具多向锚定：pi 起、claude-code 接） |

### 代码审查门禁（自动挡）

`forge review` 让 code-review-gate 从"靠手动唤起"变成自动挡——task 流程下 task-complete 门禁强制 ReviewPassed 前置（提交前必审）；非 task 流程下 Stop hook 自动拦截未审的源码变更。误触发已防护：纯文档/配置/生成物变更、无变更、commit 后干净工作区不触发；同一 diff 反复未审最多 block 3 次后 advisory 放行（防 Stop 死循环）。审查由独立只读子 agent 执行（防自审盲区），见 `code-review-gate` skill。

| 命令 | 说明 |
|------|------|
| `forge review pass` | 标记当前变更已通过 code-review-gate（task 模式写任务状态，否则写分支 stamp） |
| `forge review gate` | 判定当前是否需要审查（Stop hook 调用；exit 0=放行，1=需审 block） |
| `forge review status` | 显示当前审查状态 |

### 高危命令 human-in-the-loop（自动挡）

`forge hazard` 让 on-demand-guards 的"高危命令拦截"从 session 级 skill 变成 always-on 自动挡——hazard-guard hook（PreToolUse Bash）检测 `rm -rf` / `git push --force` / `git reset --hard` / `DROP DATABASE|TABLE|SCHEMA` / `TRUNCATE` / `GRANT ALL` / `kubectl delete` / `docker system prune` / `shred` / 无 WHERE 的 `DELETE|UPDATE` 等 → block 并指引 agent 用所在 AI 工具的提问确认工具获用户明确确认 → `forge hazard confirm` 登记 5min 限时标记 → 重试放行。bash-guard 只盯"写文件模式"，对这些破坏性命令无感，hazard-guard 补这个缺口。HITL 而非硬 block：合法高危操作（删 build 产物）确认后能继续；测试/CI 可设 `FORGE_ALLOW_HAZARD=1` 跳过。

| 命令 | 说明 |
|------|------|
| `forge hazard confirm <命令>` | 登记一次高危命令确认（5min 内同命令重试放行） |
| `forge hazard status` | 列出当前有效确认及剩余时间 |

### Act 反馈臂（证据驱动结论）

`forge task complete` 时把本任务的证据驱动结论（评分 + 证据强度 + 验收通过率 + 低分维度）落盘到 `~/.forge/projects/<项目key>/act/conclusions.jsonl`（用户级数据目录），喂给 `session-retrospective`：会话结束回顾不再靠 agent 临结束回忆"这次验证过没"，而是读 deterministic 结论。证据弱（Unverified/Weak——完成声明主要靠 agent 自述）或低分（<70）的结论标 RetrospectiveNudge 并附一行回顾指令——对冲"高分但没真验证"的 LLM-judge 盲区（分数看不出 agent 是否真跑过验证）。

| 命令 | 说明 |
|------|------|
| `forge act show [--ref <ref>]` | 查看最新（或指定）任务结论 |
| `forge act list [--json]` | 列出所有任务结论 |
| `forge act nudge` | 最新结论有回顾 nudge 时输出一行（否则静默）——供 task-verify 会话结束 hook 消费 |

### Skill 治理

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
| `forge skills eval-gen [--save] [--cases-only]` | 生成 eval 清单；`--save`/`--cases-only` 额外落结构化 case 集（回归闭环基准） |
| `forge skills eval-record --skill X --from <file/->` | 回填一次 eval run（agent dispatch 跑完每个 prompt 后整批提交，归一化+判定+算 health） |
| `forge skills eval-report --skill X` | latest run vs baseline 回归报告（regression 三态 + pass-rate delta + 可比性） |
| `forge skills eval-baseline --skill X` | 标记 baseline run（回归基准，显式人工决策） |

### MCP 接口

`forge mcp serve` 在 stdio 上运行 MCP server，让 coding agent（Claude Code / Codex / Copilot）以结构化工具调用 Forge，不必 parse CLI 文本。11 个工具覆盖门禁推进、任务接续、质量追踪、PDCA Act/项目健康与 skill eval 回归：

| 工具 | 说明 |
|------|------|
| `forge_task_status` | 查询任务状态 |
| `forge_task_gate` | 推进任务门禁（implement/verify/complete） |
| `forge_task_resume` | 拉回任务接续上下文（结构化：目标/计划/决策/阻塞/发现/参与工具/门禁进度/git 已改） |
| `forge_task_decide` | 记录已确认决策到 task（跨会话/跨工具不再推翻） |
| `forge_task_attach` | 锚定 session+工具到 task（跨工具多向锚定） |
| `forge_trace_query` | 查询任务质量事件时间线 |
| `forge_act_query` | 查询任务结论（证据强度/score/验收/低分维度）+ 回顾指令（Act 反馈臂读端） |
| `forge_health_query` | 项目级质量趋势上卷（盲区率/复发低分维度，task→project 粒度） |
| `forge_skill_eval_cases` | 生成 skill eval case 集 + dispatch 指令包（agent 据此跑回归） |
| `forge_skill_eval_submit` | 整批回填 eval run（归一化 + 判定 + 算 health） |
| `forge_skill_eval_report` | latest run vs baseline 回归报告（regression 三态 + pass-rate delta + 可比性） |

### 可观测与维护

| 命令 | 说明 |
|------|------|
| `forge health [--json]` | 项目级质量趋势——聚合所有任务结论（分数走势/证据盲区率/复发低分维度，task→project 粒度联动） |
| `forge trace <task-ref>` | 查看任务的完整质量事件时间线（checklog + toollog + token） |
| `forge dashboard [--global] [--port <n>] [--no-open]` | 本地质量看板——起 HTTP 服务把分数走势/证据盲区率/复发低分维度/最近任务渲染成图形（localhost 只读，自动开浏览器，Ctrl+C 退出）。加 `--global` 聚合 `~/.forge/projects.json` 登记的全部项目（`forge init` 自登记），跨项目比对；项目被移走/删除后 `.forge/` 消失即自动淡出，不留幽灵路径 |
| `forge sync [--force]` | 同步 .forge/ 资产到当前二进制版本 |
| `forge clone check` | 检测文件代码克隆 |
| `forge plugin pack [--out <dir>] [--owner-name <n>]` | 生成多 host plugin pack（.claude-plugin/.cursor-plugin marketplace + plugins/\<name\>/ 树：claude manifest 含 hooks + 共享 .mcp.json + 每 host 安装 README），让各 agent 一键 `plugin install forge` 跨工具接线（薄 manifest + 共享内容，单仓即 marketplace） |
| `forge plugin status` | 报告 forge plugin 是否在 user-level 已装（exit 0=已装，非零=未装；供 init-suggest hook / 脚本检测） |
| `forge plugin dedupe [dir] [--keep-empty]` | plugin 已装时清理 project-level 重复 hooks + MCP，并清理 user-level（`~/.claude` 或 `$CLAUDE_CONFIG_DIR`）`settings.local.json` 的重复 forge hooks（plugin.json 已在 user-level 注册全部 hooks）；幂等 no-op；init-suggest SessionStart 自动调用（传 `--keep-empty` 保留项目 `settings.local.json` 为 `{}`，不删用户个人配置文件）；user-level 始终保留文件壳（绝不删用户全局配置）；手动不传则项目级清完删空文件。注：forge 项目内 autoSync 每命令末尾 defer 已静默 dedupe，本命令在非 forge 项目（如 `cd ~ && forge plugin dedupe`）手动跑才作清理主力并给出可读输出 |

## 安装

```bash
# npm（推荐）
npm install -g @agent_forge/forge

# 或从 GitHub Releases 下载二进制
# https://github.com/MjxUpUp/Forge/releases

# 支持平台：macOS (x86_64/ARM64)、Linux (x86_64/ARM64)、Windows (x86_64)
```

### 通过 Claude Code plugin marketplace（用户级，一次性接线）

若主要用 Claude Code，可走 plugin marketplace 一次性接线用户级 hooks + MCP（机器上所有项目共享，无需逐项目配 `.claude/settings.local.json`）：

```
/plugin marketplace add MjxUpUp/Forge
/plugin install forge@forge
```

仍需 `npm install -g @agent_forge/forge` 装二进制（hooks/MCP 都 spawn forge），并在每个项目 `forge init` 生成项目级资产（`.forge/`、`CLAUDE.md`/`AGENTS.md`、skills）。plugin 已装时 `forge init` 会自动去重 project-level 的 hooks + MCP，并清理 user-level `settings.local.json` 的重复 forge hooks（避免与 user-level plugin 双重注册——历史 global `forge init` 写 home / 旧全局安装残留的重复），存量项目由 init-suggest SessionStart hook 自动迁移。完整三步与各 host 差异见 `plugins/forge/README.md`。

## License

MIT
