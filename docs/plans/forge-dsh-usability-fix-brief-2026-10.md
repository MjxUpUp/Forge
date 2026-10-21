# Forge DSH 插件易用性修复任务书（2026-10）

**状态**：已实施（2026-10-21，见 §11）
**来源**：2026-10-20 对 `@agent_forge/forge-dsh` 的实测审计——被审计对象是一个真实的 DSH 桌面版会话（插件在跑、门禁在拦），不是静态走查。
**目标**：从插件市场安装之后，用户第一轮就能知道"插件已生效"、被拦时知道怎么恢复、插件失效时有人告诉他。
**非目标**：不改 DSH 宿主；不改门禁规则（拦什么放什么）；不改 fail-open 决策。

---

## 1. 背景（给没参与审计的人）

Forge 是 AI 编码质量门禁引擎（Go CLI，`npm install -g @agent_forge/forge`）。`@agent_forge/forge-dsh` 是把它接进 DeepSeek Harness 的桥：把 DSH 的五个拦截点翻译成 forge 的 hook 事件，再把 forge 的判定翻译回 DSH 的 `deny` / `block` / `reject` / `steer`。

核心设计契约：**fail-open** —— forge 缺失、超时、输出不可解析时，一律放行，绝不锁死 agent 的工具。

审计时本机版本：插件 **1.74.1**（`~/.dsh/profiles/desktop/package.json`），仓库 HEAD **1.79.1**，forge CLI 1.79.1，macOS arm64。

---

## 2. 怎么复现和观测

| 要观测的东西 | 方法 |
|---|---|
| 插件是否在跑 | `~/.forge/projects/<key>/sessions.jsonl` 里有无 `agent_type=dsh` 的会话行 |
| 引导是否送达 | 解压会话 transcript，找 `source.kind=plugin:forge-quality` 的 user 消息 |
| 会话文件 | `~/.dsh/sessions/<mangled-cwd>/<session-id>/session.v4.jsonl.zstd` |
| 解压查看 | `zstd -dc <file> \| python3 -c '…逐行 json.loads…'`（附录 C 有现成脚本） |
| 手动跑 hook | 附录 A 的 payload 管道喂给 `forge hook <name>` |
| hook 耗时 | 附录 B 的 bench 脚本 |

判断"首次引导是否送达"的唯一口径：**新会话首轮 transcript 里有没有 `source.kind=plugin:forge-quality` 的消息**。

---

## 3. P0：首装链路断裂

### P0-1 SessionStart 的 4 条上下文全部丢失（不是延迟）

**现象**：新会话第一轮，插件一句话都不说。用户在第一次被 task-guard 拦下之前，不知道 forge 存在、不知道协议已生效、不知道自己有未完成任务。

**证据链**（全部可复现）：

1. session-start 组确实跑了：`sessions.jsonl` 在会话启动同一毫秒写入 `agent_type=dsh`。
2. 手动复跑该组，产出 4 条非空上下文：
   - `[skill-scan] all skills SAFE (advisory, 21 rules)`
   - `[mcp-scan] no .mcp.json (advisory)`
   - `[forge-session] 本项目由 forge 管理（managed）——forge 质量协议生效…` + 1 个未完成任务
   - `[forge] conventions: …`（约 10 行）
3. transcript 实测（`session-0c91b953-…`）：
   - turn 1 前 4 条 user/message 只有用户输入、runtime-context、skill 清单、时间（`seq 8–11`）；
   - 第一条 `plugin:forge-quality` 出现在 `seq 218`（turn 1 中途，来自一次 Bash 的 PreToolUse）；
   - **turn 2 起始（`seq 451–458`）也没有补投**。
   - 结论：**丢失，不是延迟**。

**根因**：
- `agent/session-start` 是 detached async（`plugins/forge-dsh/lib/index.js:145-169`），context 靠 `agent.inject` 投递（`lib/decisions.js:169-171` → `174-184`）。
- `inject` 不可用时 `injectContexts` 直接 `return`（`lib/decisions.js:176-177`），`debug=false` 时零日志。
- 对照：同一会话里 `agent/pre-step` 的消息投递可靠（turn 2 起始 `seq 455` 有插件消息）——**改走 pre-step 就必达**。

**修复（推荐方案）**：
1. `lib/index.js` 增加 `let pendingStart = []`。
2. session-start 组跑完后不再调 `sessionStartOutcome`，改为 `pendingStart.push(...outcome.contexts)`。
3. `agent/pre-step` 里先 `const decision = await preStepDecision(...)`；若 `pendingStart.length > 0 && decision.kind === "enter"`，返回 `{kind: "enter", messages: [...pendingStart.map(pluginMessage), ...decision.messages]}` 并清空队列；`reject` 时保留队列，等下一次。
4. 首个 pre-step 拿不到 agent 时保留队列，不丢。
5. `injectContexts` 在 inject 缺失时打一行 `console.error`（受 `debug` 控制）。
6. 补 `lib/index.test.js` 用例：session-start → 首个 pre-step，引导消息必须被 prepend。

**验收**：
- 新开会话、只发一条 prompt，首轮 transcript 必须含 `[forge-session]` 引导消息。
- 多轮会话里同一条引导不得重复投递。

---

### P0-2 缺 forge CLI 时静默

**现象**：本机 1.74.1 完全没有 probe；1.79.1 加了 probe 和提示，但提示走 P0-1 那条坏通道，同样会丢。用户能感知到的唯一区别是"门禁突然不拦了"。

**证据**：
- `~/.dsh/profiles/desktop/node_modules/@agent_forge/forge-dsh/package.json` → `1.74.1`。
- 与仓库 `lib/index.js` 的 diff：`probeForge` / `missingForgeNotice` / `ensureForge` 是后来加的（仓库版 `lib/index.js:70-112`）。
- 所有 hook 走 fail-open：`lib/runner.js:75-166`。

**修复**：
- `ensureForge` 的 missing-forge notice 改为推进 P0-1 的 `pendingStart` 队列，不再用 `agent.inject`。
- 文案保留三要素：动作（`npm install -g @agent_forge/forge`）、配置出口（`forgeBin`）、生效时机（启动新会话）。
- **验收**：把 profile patch 里的 `forgeBin` 设成不存在的路径，新会话首轮必须出现该提示。

---

## 4. P1：日常使用摩擦

### P1-1 每次工具调用 +0.86s（Bash）～1.9s（编辑）

**实测**（热态，macOS arm64）：

| 场景 | 串行逐条（现状） | 单进程 batch |
|---|---|---|
| PreToolUse Bash（5 hook） | **856ms** | **458ms** |
| PreToolUse Edit（6 hook） | 1077ms | 未测（同比例） |
| PostToolUse Edit（5 hook） | 857ms | — |
| 冷态首调 Bash 组 | 1641ms | — |
| `forge --version`（npm shim） | 211ms/次 | — |

**根因**：
1. 插件把 spec 里每个 hook 当独立进程串行跑（`lib/decisions.js:40-53`）。
2. `/opt/homebrew/bin/forge` 是 node shim（`#!/usr/bin/env node`），每次 spawn 先付约 200ms。
3. **batch 目前用不了，因为一个真实契约缺口**：单 hook 阻断会输出 exit 2 + stdout JSON；`forge hook batch` 阻断时 **stdout 为空**，reason 只在 stderr。

**batch 的根因定位**：`internal/hookdispatch/hook_batch.go:64-77` —— 内层 hook 的 block JSON 由 `emitClaudeOutput` 写进被 `runHookCaptured` 捕获的 stdout（`internal/hookdispatch/hook_emitters.go:105-141`），batch 在 `runErr != nil` 时直接 `return runErr`，没有把捕获到的 stdout 打出来。于是只剩 stderr 的纯文本。

**修复（按顺序做）**：
1. 核心：`runHookBatch` 在 `return runErr` 之前 `fmt.Print(out)`；补测试——同一 payload，`forge hook batch --event PreToolUse --matcher "Write|Edit"` 的 stdout 必须等于 `forge hook task-guard` 的 stdout。
2. 插件：`runHookGroup` 改为一次 `forge hook batch --event <E> --matcher <M>`，`matchedCommands` 从"算命令列表"降级为"选 matcher"。
3. 可选：npm shim 从 node 改为 shell `exec`，每次 spawn 省约 100ms。

**验收**：热态 PreToolUse Bash 组 < 600ms（附录 B 跑前后各一次）；`/forge-status` 增加按组耗时字段。

**注意**：batch 有历史挂死修复（`runHookCaptured` 的双向并发泵送，v1.57.0–v1.77.0 曾挂死），改输出时不要动这段并发逻辑。

---

### P1-2 合成消息噪音

**现象**：allow-path 的 advisory 会被注入成一条 `role=user` 的假用户消息。

**实测**：
- `[hazard-guard] 危险串仅在引号内或注释行（数据上下文），放行: <整条命令逐字回放>` —— **917 字节**。触发条件只是命令的引号里出现了 `rm -rf`。
- skill 触发横幅 360–597 字节。已在做去重（第二次变成"本 session 已注入过，短提醒"，478 字节），但每次仍进对话。

**根因**：`lib/decisions.js:67-73` 把 PreToolUse allow-path 的 `outcome.contexts` 全部 `agent.inject`，没有"这条值不值得进对话"的判断；`injectContexts`（`lib/decisions.js:174-184`）也没有按来源过滤。

**修复**：
- 核心层：`hazard-guard` 的"放行"说明改为只落 checklog，不再作为 `additionalContext` 输出（它是诊断信息，不是给模型的指令）。
- 插件层：allow-path 注入增加白名单（`[forge-session]`、`[task-guard]`、`[conventions]` 之类需要模型看到；纯 pass-through 说明丢弃）。
- **验收**：一条引号里含 `rm -rf` 的 Bash 命令，不产生任何合成 user 消息。

---

### P1-3 fail-open 没有"降级"信号

**现象**：超时、stdout 解析失败、spawn 失败全部静默 fail-open（`lib/runner.js:99-166`），只在 `/forge-status` 可见；而 `/forge-status` 只有 README 提过。big repo 上 `auto-compile` 超过 30s 会静默丢掉那一次反馈。

**修复**：
- Stop 时若本会话 fail-open 次数 ≥ 3，输出一行 advisory（复用 `turnStoppingOutcome` 的 steer 通道）。
- 首轮引导文案里点名 `/forge-status`。
- **验收**：把 `timeoutMs` 调到 1，跑一次编辑，会话里必须出现降级提示，且不阻断。

---

## 5. P2：安装与文档

| 编号 | 问题 | 位置 | 修法 |
|---|---|---|---|
| P2-1 | README 首条命令 `--profile web`，桌面 App 实际是 `desktop`，照抄会装进没在跑的 profile | `plugins/forge-dsh/README.md:60-66`（卸载段 `:73` 才提 desktop） | 安装段改为 `dsh plugin --profile <你的 profile>` 并显式写"桌面 App 用 desktop"；或去掉 `--profile` 让 CLI 用默认 |
| P2-2 | 版本漂移无提示（本机 1.74.1 vs 仓库 1.79.1，缺的正好是缺 CLI 提示） | profile `package.json` | 首轮引导加一行版本号；`/forge-status` 加"latest"对比（可选） |
| P2-3 | 文案语言混杂：task-guard 英文、hazard-guard 中文、横幅中文 | 核心 hook 文案 | 统一走一个 locale 配置（至少 DSH 侧） |
| P2-4 | 首轮引导是内部术语（`[skill-scan] all skills SAFE`、`[mcp-scan] no .mcp.json`），不是 onboarding | SessionStart 组 | 首轮单独一条人话："Forge 已接管本项目：会在改源码前要求任务、拦截危险命令；`/forge-status` 看状态，`forge off` 关闭。" |
| P2-5 | init-suggest 不认 DSH 插件安装，仍要用户回答一次接管提问 | `internal/hooks/plugin_detect.go:42-89`（只查 Claude home） | 增加 DSH profile 检测，命中则与 Claude 插件同策略；或明确产品决定"DSH 必须问" |

---

## 6. 产品分层与打包策略

**结论**：`forge-dsh` 是桥，不是产品。市场安装只交付了"接线"这一层；门禁引擎、技能库、协议文本三层都缺。DSH 又是把 task-guard 提升为硬阻断的宿主，所以新用户的第一个真实体验是"被最严格的门禁挡住，且此前没收到任何引导"。

### 6.1 四层交付物与当前缺口

| 层 | 谁提供 | 只装插件时 | 缺口证据 |
|---|---|---|---|
| **门禁引擎** forge CLI（13MB/平台二进制） | `npm i -g @agent_forge/forge` | 无 → 每个 hook 都 fail-open | `plugins/forge-dsh/lib/runner.js:75-166`；`package.json` 零 runtime 依赖 |
| **宿主接线**（76KB） | 市场安装 | 有 | — |
| **技能库**（43 个 skill） | `forge skills install --global --target agents` | 无（`~/.agents/skills` 为空） | `internal/agentbridge/pluginpack.go:279` 自述是 plugin 用户缺口 |
| **协议文本** | `forge init` 写 `~/.claude/CLAUDE.md` + `~/.codex/AGENTS.md` | 无（DSH 两个都不读），只剩 SessionStart 注入 | `internal/skillgen/claudemd.go:58-65`；注入本身见 P0-1 |

补充事实：DSH 是 task-guard **硬阻断**宿主（`internal/skillgen/claudemd.go:132`，双事故实证），"无引导 + 硬阻断"叠加，是当前最差的新用户组合。

### 6.2 市场用户的真实漏斗（4 步，只有第 1 步在市场里）

1. 市场点安装 → 结束。
2. 发现没反应 → 回 README → `npm install -g @agent_forge/forge`。
3. 想要技能 → `forge skills install --global --target agents`（DSH README 未提）。
4. 项目登记 → init-suggest 问一次 / `forge init`。

### 6.3 三个方案

**A. 插件内置依赖**
- 做法：`dependencies: {"@agent_forge/forge": "<pin>"}`；插件从自身 require 解析平台二进制（`@agent_forge/forge-<os>-<arch>/bin/forge`）直接 spawn，绕开 node shim（顺带省每次 211ms）。
- 收益：一次安装 hook 就能跑；版本与 `lib/spec.json` 锁死，顺带消灭 spec 漂移。
- 代价：+13MB/profile；**`forge` 命令仍然不在 PATH**，门禁文案里的 `forge task start --ref …` 照旧 not found——被拦后无法按提示恢复，比没门禁更糟。
- 判定：只解决"有门禁"，不解决"能恢复"。

**B. 引导安装（成本最低）**
- 做法：P0-2 修好后，首轮必达提示给两条可复制命令（CLI + skills）；市场条目与 README 把前置依赖放第一行。
- 收益：`forge` 命令天然可用；无供应链与体积代价。
- 代价：仍是两步，漏斗损耗真实存在。

**C. 首跑自动安装**
- 做法：缺 CLI → agent 提问确认 → 执行 `npm i -g`，失败降级到官方 tarball。
- 收益：体验最接近一键。
- 代价：权限、网络、企业 registry。**本机即是反例**：npm 走公司 Nexus，`npm view` 返回 401——自动安装必须带降级，不能作为唯一路径。

### 6.4 建议

1. 本版本走 B：把 P0-2 和 skills 安装并进同一条首轮引导，成本一个 PR。
2. 若要"市场安装 = 完整"，走 A + 命令桥：内置二进制负责 hooks，同时提供 PATH 可见的 `forge` 入口，或在门禁文案里用绝对路径。
3. 无论选哪条，把 `forge skills install --global --target agents` 接进安装流程；DSH 用户没有技能库，等于丢了一半产品。
4. 协议文本在 DSH 没有文件通道，只能靠 SessionStart 注入——P0-1 不修，"完整体验"无从谈起。

三个待拍板的问题见 §10。

---

## 7. 建议 PR 拆分

| PR | 内容 | 改动面 | 风险 |
|---|---|---|---|
| PR1 | P0-1 + P0-2（首轮必达队列 + 缺 CLI 提示） | 仅 `plugins/forge-dsh/lib/*` | 低（新增队列，不动 hook 执行） |
| PR2 | P1-2 噪音（hazard 放行说明降级为 checklog + 注入白名单） | `internal/hooks` + `plugins/forge-dsh/lib/decisions.js` | 中（影响其他宿主的输出量） |
| PR3 | P1-1 batch JSON 对齐 + 插件切 batch | `internal/hookdispatch/hook_batch.go` + 插件 | 中（动 batch 输出，需跑通全部宿主测试） |
| PR4 | P2 文档与文案 | README + 引导文案 | 低 |
| PR5 | 打包策略：B（引导 + skills 并入）→ 视决策再上 A | 插件 + README + 可选 npm 依赖 | 中（依赖 §10 决策） |

PR1 单独就能消除"装了等于没装"，建议先合。

---

## 8. 全局验收清单

1. 新会话 + 单条 prompt：transcript 含 `plugin:forge-quality` 引导，内容含 `[forge-session]`。
2. `forgeBin` 指向不存在路径：首轮出现可读提示，且工具调用不被阻断。
3. 引号内含 `rm -rf` 的 Bash 命令：不产生合成 user 消息。
4. 热态 PreToolUse Bash 组 < 600ms（附前后 bench 数据）。
5. `timeoutMs=1`：出现降级提示，且不阻断。
6. `/forge-status` 正常输出，新增耗时字段。
7. Windows 冒烟：spawn 走 `cmd.exe`、超时走 `taskkill /T`，行为不变。

---

## 9. 不要改坏的东西

- **fail-open 契约**：任何基础设施失败都不得变成 block。改 `runner.js` 时保持"只有 JSON `decision:"block"` 才算阻断"。
- **不要用 exit code 推断阻断**：batch 修好之后也继续读 JSON。
- **stdin EPIPE 护栏**（`lib/runner.js:164`）：forge 不读 stdin 就退出时会崩宿主进程，护栏不能删。
- **win32 路由与进程树击杀**（`lib/runner.js:49-62`、`185-210`）：`cmd.exe` 解析 `forge.cmd`、`taskkill /T /F` 杀树。
- **batch 的双向并发泵送**（`internal/hookdispatch/hook_batch.go:125-160`）：那是修过的挂死 bug。

---

## 10. 需要产品决策的问题

1. 首轮欢迎语的语言：中文、英文，还是跟随 DSH locale（本机 locale 是 zh）。
2. "插件装了但没生效"要不要在 DSH 里常驻可见（目前唯一通道是 inject，没有状态栏）。
3. init-suggest 对 DSH 插件是否算 opt-in：算的话与 Claude 插件对齐（安装即接管），不算就把提问文案写进首轮引导。
4. DSH 插件的定位是"桥"还是"完整产品"——决定 §6.3 走 A 还是 B。
5. 是否接受每个 profile +13MB，换"市场安装即可用"。
6. 若走 C（自动安装）：是否需要用户显式同意，企业 registry 失败时降级到哪条路径（本机 npm 走 Nexus，`npm view` 返回 401）。

---

## 附录 A：hook payload 模板

```json
{"session_id":"audit","cwd":"<项目绝对路径>","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"},"forge_agent":"dsh"}
```

```json
{"session_id":"audit","cwd":"<项目绝对路径>","hook_event_name":"PreToolUse","tool_name":"Edit","tool_input":{"file_path":"<文件绝对路径>","old_string":"a","new_string":"b"},"forge_agent":"dsh"}
```

```json
{"session_id":"audit","cwd":"<项目绝对路径>","hook_event_name":"SessionStart","source":"startup","forge_agent":"dsh"}
```

## 附录 B：bench 脚本

```python
import subprocess, time
payload = b'{"session_id":"bench","cwd":"<项目>","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"git status"},"forge_agent":"dsh"}\n'
hooks = ["bash-guard","hazard-guard","gate-cmd-form","skill-trigger","task-drift"]
def serial():
    for h in hooks: subprocess.run(["forge","hook",h], input=payload, capture_output=True)
def batch():
    subprocess.run(["forge","hook","batch","--event","PreToolUse","--matcher","Bash"], input=payload, capture_output=True)
for label, fn in (("serial", serial), ("batch", batch)):
    fn()  # warm
    ts = []
    for _ in range(4):
        t = time.time(); fn(); ts.append((time.time()-t)*1000)
    print(label, [round(x) for x in ts], "median", round(sorted(ts)[2]))
```

## 附录 C：证据坐标（本次审计）

- 会话：`~/.dsh/sessions/--Users-jx.ma-own-projects-Forge--/session-0c91b953-1212-490a-acff-c463f06b7076/session.v4.jsonl.zstd`
  - `seq 8–11`：turn 1 启动消息（无插件消息）
  - `seq 218/219`：turn 1 中途首批插件注入
  - `seq 451–458`：turn 2 启动（仍无 SessionStart 引导）
  - `seq 455`：pre-step 消息投递成功（证明 pre-step 通道可用）
- forge 状态：`~/.forge/projects/<key>/sessions.jsonl`（14:22:04.714，agent_type=dsh）
- 已装版本：`~/.dsh/profiles/desktop/package.json` → `@agent_forge/forge-dsh: 1.74.1`

---

## 11. 实施结果（2026-10-21）

| 编号 | 结果 | 落地 / 证据 |
|---|---|---|
| P0-1 SessionStart 上下文丢失 | 已修 | 按 agent 排队，随下一次 `enter` 的 pre-step 投递；首个 pre-step 最多等 `startWaitMs`（任务书未提的竞态：detached 的 session-start 可能晚于首个 pre-step）；reject 保留队列。PR #123 |
| P0-2 缺 CLI 静默 | 已修 | 缺 CLI 提示走同一队列；插件内置 forge 本体后该路径只剩兜底 |
| P1-1 每次工具调用 +0.86s | 已修 | 核心：`forge hook batch` 阻断时打出内层 block JSON（`hook_batch_test.go` 断言与单 hook stdout 相等）；插件：每个 matcher 组一次 batch。实测 PreToolUse/Bash 组 1720ms → 716ms（机器 load≈9，绝对值未达 <600ms，比例 2.4×）。旧 forge（阻断时 stdout 为空）被识别为 batch 不可答并回退逐条，绝不静默放行 |
| P1-2 合成消息噪音 | 已修 | 核心：hazard-guard 数据上下文放行静默（stdout 空，审计落 hazard 台账）；插件：丢弃已确认的纯状态回执，未知提示一律放行。**与任务书不同**：不用白名单（会吞掉新增 advisory），用静默回执名单 |
| P1-3 fail-open 无降级信号 | 已修 | 同会话 3 次 fail-open 后，在下一个不为真实阻断 steer 的 Stop 上一次性提示，按会话计数，batch 降级不计入 |
| P2-1 README `--profile web` | 已修 | README、中文 README、主页同步改为 `<profile>`，写明桌面 App 跑 `desktop` |
| P2-2 版本漂移无提示 | 已修 | 欢迎语带插件与 forge 版本；`/forge-status` 显示 `plugin: x  latest: y`（只在执行命令时发一次只读 GET、3s 超时，离线/私有 registry 降级为 unknown，`checkLatest:false` 可关）。对真实 registry 验证过返回结构 |
| P2-3 文案语言混杂 | 已修（DSH 侧） | 插件自己的消息集中到 `lib/messages.js`，中英键集合测试钉死；`locale`：配置 > `FORGE_DSH_LOCALE` > 系统。核心 hook 文案未动（任务书「至少 DSH 侧」） |
| P2-4 首轮引导是内部术语 | 已修 | 仅在 forge 自己的 `[forge-session]` 接管横幅出现时发人话欢迎语；子 agent、resume、compact 不发 |
| P2-5 init-suggest 不认 DSH 插件 | 无需改 | 前提已过期：`init-suggest` 现为「每项目问一次」，不从「装了插件」推断同意（`takeover` 偏好才是 opt-in），Claude 与 DSH 插件策略一致；`plugin_detect.go` 只服务 hooks 去重 |
| §6 打包策略 | 走 B 并超出 | 插件依赖 `@agent_forge/forge`（精确版本、lockstep），装插件即装本体、卸载即移除；首轮告知绝对路径；缺技能库时首轮提议 `forge skills install --global --target agents`（征得同意后执行）；发布后验证脚本 |
| §8 验收 6 `/forge-status` 耗时字段 | 已加 | `dispatch:` 行、按拦截点耗时、每次运行耗时 |
| §8 验收 7 Windows 冒烟 | 已验证（CI） | 本机是 macOS，无法实跑；依据是 PR 125 的 CI：`scripts-test (windows-latest)` 通过（run 39587876627 / 39587883430，提交 f4de00c8）——整套插件测试在 Windows 上跑过，覆盖 batch 参数引用（matcher 的 `Write\|Edit` 经 `cmd.exe`）与 `.cmd` shim 替身；依赖 POSIX 的用例（符号链接、shell 脚本替身）显式跳过 win32。Go 侧的 `build (windows-latest)` 以 PR 最终 CI 为准 |

§10 决策（按用户「全部做完」取默认）：欢迎语语言随 locale（系统为中文则中文）；init-suggest 维持「问一次」；定位走 B 并内置本体依赖（接受每 profile +13MB）；不做自动安装（C）。
