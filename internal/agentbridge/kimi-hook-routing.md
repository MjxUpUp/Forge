# kimi-code hook 路由表 / kimi-code Hook Routing Table

> 真相源：对真实会话 `session_060d5b3a`（960 行 wire.jsonl，76 次工具调用）的交叉验证，
> 结合 forge `emitKimiOutput` 的 exit-code 契约。本文档随 `fix/kimi-advisory-p0/p1/p2/p3`
> 四个 PR 落地。
>
> Source of truth: cross-verification against a real session `session_060d5b3a` (960-line
> wire.jsonl, 76 tool calls) plus forge's `emitKimiOutput` exit-code contract. This doc ships
> with the four PRs `fix/kimi-advisory-p0/p1/p2/p3`.

## 本表读法 / How to read this table

下表展示 **25-hook 基线**（`.kimi-plugin/plugin.json` 在 P0–P3 之前的完整 hook 集），
每行的 `PR` 列指出该路由由哪个 PR 交付。**P1 会从 manifest 物理删除 6 个 skill-trigger
条目**（PostToolUse×2、PreToolUse×2、SessionStart×1、Stop×1，25→19 hook），并在运行时
对 kimi+非 UserPromptSubmit 的 skill-trigger 做防御性 noop（覆盖未重装的旧 plugin）。
故「P1 抑制/移除」既含 manifest 删除也含运行时兜底。P0/P1/P3 是本表(P2)的兄弟 PR，
各自独立分支、独立合入；在某条兄弟 PR 未合入时，对应行描述的是目标态而非当前态。
>
> The table below shows the **25-hook baseline** (the full `.kimi-plugin/plugin.json` set
> before P0–P3). The `PR` column names the PR that delivers each row. **P1 PHYSICALLY REMOVES
> 6 skill-trigger entries** from the manifest (PostToolUse×2, PreToolUse×2, SessionStart×1,
> Stop×1 — 25→19 hooks) and adds a runtime noop for kimi+non-UserPromptSubmit skill-trigger
> (covering stale, not-reinstalled plugins). So "P1" means both manifest removal and a
> runtime backstop. P0/P1/P3 are sibling PRs of this doc (P2), each on its own branch; until a
> sibling merges, its rows describe the TARGET state, not the current branch state.

## TL;DR — kimi 0.35.0 上仅 3 条通道能影响模型

forge 旧假设（`internal/agentbridge/kimi.go` 与 `internal/cli/hook.go` 的协议注释）：
**「exit 0 = 放行，stdout 文本进上下文（advisory 通道）」**。这个假设在 kimi 0.35.0 上
**对 PostToolUse / SessionStart 是错的**——它们的 stdout 被 kimi 丢弃，永不进模型上下文。

经 wire.jsonl 核实，kimi 0.35.0 上 hook 能影响模型的通道**只有 3 条**：

| 通道 Channel | 机制 Mechanism | 验证 Verification |
|---|---|---|
| **PreToolUse exit-2** | 阻断工具，stderr 展示给模型 | ✅ 契约可靠（emitKimiOutput→exit 2） |
| **Stop exit-2** | 阻断结束，强制再来一轮，stderr 展示 | ✅ wire 实证：`stop_hook` origin message |
| **UserPromptSubmit stdout** | 注入上下文，但**下一个 prompt 才送达**（滞后） | ✅ wire 实证：`<hook_result hook_event="UserPromptSubmit">` |

PostToolUse / SessionStart / PostCompact 的 stdout 是 **observation-only**（fire-and-forget）：
无论脚本返回什么，主流程不受影响——**stdout 不进上下文**。

> ⚠️ **未决项（需活体验证）**：PostToolUse 的 **exit-2 阻断**是否在 kimi 上生效，wire 无法
> 证实（样本会话里没有 PostToolUse hook 返回过非零）。PostToolUse 既然连 stdout 都丢，
> **疑似 exit code 也失效（全 observation-only）**，但未证实。见下方「待验证」节。
>
> ⚠️ **Unresolved (needs live probe)**: whether PostToolUse **exit-2 BLOCKS** on kimi cannot be
> settled from the wire (no PostToolUse hook returned non-zero in the sample). Since kimi drops
> PostToolUse stdout, it is **suspected fully observation-only**, but unverified. See "Remaining
> verification" below.

## 证据基础 / Evidence basis

对 `session_060d5b3a` 的 `context.append_message` origin 计数（模型可见的上下文增量）：

```
origin        count
user           17    # 用户输入 + system-reminder + 1 条 UserPromptSubmit 注入
stop_hook       1    # review-stop 阻断（Stop exit-2）
post_tool_use   0    # ← 76 次工具调用全部触发 PostToolUse hook，0 条进上下文
session_start   0    # ← SessionStart stdout 全丢
pre_tool_use    0    # 本会话无 PreToolUse 阻断发生（非通道失效，是无触发条件）
```

- 76 次工具调用（38 Bash + 38 Edit 等）每次都跑 PostToolUse hook（file-sentinel 快照文件
  实存于 Temp），但 PostToolUse 的 stdout **0 次**进模型上下文 → 确认 PostToolUse stdout
  observation-only。
- 唯一进模型的 forge 文本是 UserPromptSubmit 的 skill-trigger（`<hook_result>`）。
- Stop exit-2 的阻断以 `stop_hook` origin 进上下文 → 确认 Stop exit-2 通道生效。
- `kimi-code.log` 只记 LLM 请求/响应计时，**不记 hook 执行与 exit code**——故无法从日志侧
  证实 PostToolUse exit-2 是否阻断。

## 25-hook 路由表 / The 25-hook routing table

### PreToolUse（8 hook = 6 命名 + skill-trigger×2）— 阻断通道可用，advisory 经 P0 升级为 exit-2

| Hook | 类别 Class | kimi 路由 | PR |
|---|---|---|---|
| freeze-guard | 强制 enforce | exit-2 阻断（原生效） | — |
| **task-guard** | advisory→强制 | **advisory 升级为 exit-2**（排除 `Auto-created` 成功路径） | **P0** |
| **assertion-check** | advisory→强制 | **advisory 升级为 exit-2**（只认 `Advisory:`，排除干净分支） | **P0** |
| read-before-edit | 强制 enforce | exit-2 阻断（原生效） | — |
| **bash-guard** | advisory→强制 | **advisory 升级为 exit-2** | **P0** |
| hazard-guard | 强制 enforce | exit-2 阻断（原生效） | — |
| skill-trigger（Write\|Edit + Bash，共 2 条） | advisory | **P1 移除 manifest + 运行时 noop** | **P1** |

> P0 升级用的是**内容谓词 map**（读 detail 文本区分「真 advisory」与「成功/干净分支」），
> 不是名字白名单——否则会把 task-guard 刚 auto-create 的那次编辑也阻断。逃生舱
> `FORGE_KIMI_ADVISORY=soft` 回退纯 advisory。

### Stop（3 hook = 2 命名 + skill-trigger×1）— exit-2 阻断通道，原生效

| Hook | 类别 | kimi 路由 | PR |
|---|---|---|---|
| task-verify | 强制 | exit-2 阻断（原生效） | — |
| review-stop | 强制 | exit-2 阻断（wire 实证） | — |
| skill-trigger | advisory | **P1 移除 manifest + 运行时 noop** | **P1** |

### UserPromptSubmit（2 hook = 1 命名 + skill-trigger×1）— 唯一的 stdout 注入通道（滞后到下一 prompt）

| Hook | 类别 | kimi 路由 | PR |
|---|---|---|---|
| resume-reinject | 注入 | stdout 注入（原生效）+ **P3 承载冷启动 handoff** | **P3** |
| skill-trigger | 注入 | stdout 注入（skill 框架在 kimi 下唯一生效事件） | **P1** 收敛点 |

### SessionStart（5 hook = 4 命名 + skill-trigger×1）— stdout observation-only，主价值经 P3 迁移到 UserPromptSubmit

| Hook | 类别 | kimi 命运 Fate | PR |
|---|---|---|---|
| skill-scan | advisory 安全 | **失效**（stdout 丢）→ 依赖 CI / 手动 `forge audit` 兜底 | 文档 |
| mcp-scan | advisory 安全 | **失效**（stdout 丢）→ 依赖 `.mcp.json` 审查兜底 | 文档 |
| init-suggest | advisory | **失效**（含 kimi-drift 漂移提醒；stdout 丢，checklog 仍记） | 文档 |
| **task-resume** | 接续 | **P3 经 resume-reinject 冷启动回填**（首 prompt 注入，sentinel 去重） | **P3** |
| skill-trigger | advisory | **P1 移除 manifest + 运行时 noop** | **P1** |

### PostToolUse（6 hook = 4 命名 + skill-trigger×2）— stdout observation-only；副作用类不受影响

| Hook | 类别 | kimi 命运 Fate | PR |
|---|---|---|---|
| auto-compile | advisory | **失效**（advisory stdout 丢，确认）→ 本就无硬价值，接受 | 文档 |
| workflow-test-guard | 强制(block) | **未决**（exit-2 是否阻断未证实；forge 仓专属守护）→ 依赖 CI 兜底 | 文档 |
| file-sentinel | 副作用 | **不受影响**（写 quarantine 快照，靠文件副作用；wire 实证快照存在） | — |
| tool-track | 副作用 | **不受影响**（写 toollog + reads-log，靠文件副作用） | — |
| skill-trigger（Write\|Edit + Bash，共 2 条） | advisory | **P1 移除 manifest + 运行时 noop** | **P1** |

### PostCompact（1 hook）— 观察通道，部分失效

| Hook | 类别 | kimi 命运 | PR |
|---|---|---|---|
| compact-resume | 副作用(设标记) | 若 PostCompact observation-only 则标记不设 → resume-reinject 压缩重注入在 kimi 下部分失效；P3 冷启动注入覆盖主场景，压缩重注入列为已知项 | 文档 |

## 已知失效项与兜底 / Known-inert hooks & fallbacks

四 PR 合入后，kimi 上仍**knowingly 失效**的 hook（接受 + 依赖兜底）：

1. **auto-compile**（PostToolUse advisory）— 无硬价值，接受。
2. **skill-scan / mcp-scan**（SessionStart 安全扫描）— 滞后到 CI / `forge audit` / `.mcp.json`
   审查。深度防御层，非唯一闸门。
3. **init-suggest + kimi-drift 提醒**（SessionStart）— 漂移仍记 checklog；plugin staleness
   另有 `hook_kimi_stale` 提醒通道。
4. **workflow-test-guard**（PostToolUse block）— 见下「待验证」。
5. **compact-resume 压缩重注入**（PostCompact）— P3 冷启动注入覆盖冷启动主场景。

## 待验证 / Remaining verification

**PostToolUse exit-2 是否阻断**是唯一未决项。两种可能：

- **若 exit-2 也失效（确认全 observation-only）**：`workflow-test-guard` 在 kimi 下失效，
  依赖 CI 兜底（已是 workflow 变更的权威闸门）。`auto-compile` 本就失效。`file-sentinel` /
  `tool-track` 副作用类不受影响。
- **若仅 stdout 失效、exit-2 仍阻断**：`workflow-test-guard` 继续工作，只有 `auto-compile`
  advisory 失效。

**活体验证方法**（需在真实 kimi 会话执行，claude-code 会话无法代办）：在 kimi 会话装一个
`exit 1` 的 PostToolUse hook，观察是否阻断后续模型轮次。结论回来后订正本表与
`internal/agentbridge/kimi.go` 的「未决」措辞。

> 无论哪种结论，**advisory 路由不变**（auto-compile 都失效、副作用类都不受影响），故 P2
> 的实现不依赖于此验证——但措辞准确性依赖之。

## 自举摩擦 / Bootstrap friction

forge 门禁用**已安装的** forge 二进制（如 `npm-global/forge.exe`），不是 `go run`。改完
源码须 `go build -o <that path> ./cmd/forge` 重建，门禁才用上 fresh binary（P0/P1/P3 有
行为变更时尤其重要；P2 仅改注释，可不重建）。
