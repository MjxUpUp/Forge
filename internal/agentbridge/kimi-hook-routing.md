# kimi-code hook 路由表 / kimi-code Hook Routing Table

> 🔄 **2026-08-24 更新（advisory 队列取代 P0 提升）**：两周生产 checklog 证实
> P0 路线两头落空——(a) kimi/no-channel 的 allow 路径 advisory（41 条
> skill-trigger + 1 条 test-nudge）100% 产生但从未到达模型；(b) 被提升为 exit-2
> 的 WARN 文案（"allowed but not tracked" / "Advisory:"）以 isError=true 送达且
> Write/Edit 被实际拦截——文案自述「allowed」的 deny 自相矛盾；且 kimi 把
> PreToolUse 上**任何** stdout 都当 deny，诚实文案也无安全通路。故 kimi 的
> PromoteAdvisory 规则**全部退役**（hostcap 注册表已清空，dsh 保留）：kimi 的
> 非阻断提示改为写入 per-project pending 队列
> （~/.forge/projects/<key>/advisories-pending.jsonl），由 UserPromptSubmit
> hook 在每次触发时 drain——去重、每会话每文本只投一次、最多最近 5 条、攒成
> **一条**注入（`internal/cli/hook_kimi_advisory.go` 的 emitAdvisoryRouted）。
> 设计内阻断（read-before-edit/hazard-guard/freeze-guard 的 FAIL）不受影响，
> 仍走 exit-2。下文 P0 段的「advisory 升级为 exit-2」描述的是历史决策，当前态
> 以本注为准。
>
> 🔄 **2026-08-24 update (advisory queue replaces the P0 promotion)**: two weeks of
> production checklog proved the P0 route lost on both ends — (a) 100% of
> kimi/no-channel allow-path advisories (41 skill-trigger + 1 test-nudge) were
> produced but never reached the model; (b) the promoted exit-2 WARNs ("allowed but
> not tracked" / "Advisory:") arrived as isError=true denies that actually blocked
> Write/Edit — a deny whose reason says "allowed" is self-contradictory, and kimi
> reads ANY PreToolUse stdout as a deny, so the honest wording had no safe ride
> either. kimi's PromoteAdvisory rules are therefore RETIRED (the hostcap row is
> cleared; dsh keeps its rule): kimi's non-blocking advisories now write to a
> per-project pending queue (~/.forge/projects/<key>/advisories-pending.jsonl),
> drained by the UserPromptSubmit hooks on every fire — deduped, once per session
> per text, capped at the newest 5, injected as ONE batched message
> (emitAdvisoryRouted in internal/cli/hook_kimi_advisory.go). Designed denies
> (read-before-edit / hazard-guard / freeze-guard FAILs) are unaffected and still
> ride exit-2. The P0 paragraphs below describe a historical decision; this note
> is the current state.

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

> 🔄 **2026-08-20 反转（hostcap 后续）**：P1 的移除理由（记录不可见命中会虚报 usage 漏斗）
> 已被 hostcap 的「诚实记录」取代——引擎在非 UserPromptSubmit 事件上运行、命中记
> `Delivered=false`（`hostcap.ContextChannel`），漏斗只计 `Delivered=true`。故 manifest
> 重新绑回全部 6 条 skill-trigger（19→25 hook），运行时 noop 也已移除；打印仍只发生在
> UserPromptSubmit。上文 P1 段的「物理删除」描述的是历史决策，当前态以本注为准。
>
> 🔄 **2026-08-20 reversal (hostcap follow-up)**: P1's rationale (recording invisible hits
> would misreport the usage funnel) was superseded by hostcap's "honest recording" — the
> engine now RUNS on non-UserPromptSubmit events, stamping hits `Delivered=false`
> (`hostcap.ContextChannel`), and the funnel counts `Delivered=true` only. The manifest
> therefore re-binds all 6 skill-trigger entries (19→25 hooks) and the runtime noop is
> gone; printing still happens only on UserPromptSubmit. The P1 paragraph above describes
> a historical decision — this note is the current state.
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

> ⚠️ **U1（2026-08-15 生产观测收窄）**：PostToolUse **exit-2 的 stderr 不达模型**——生产观测
> （单会话 n=1）：E:\AgentOffice 8/8 会话 file-sentinel（PostToolUse）exit-2 隔离了 3 次文件，但阻断
> 原因在整个 wire transcript **零出现**（无 isError tool.result、无上下文增量）。即
> PostToolUse 整条通道（stdout + exit-2 stderr）对模型均不可见；enforcement 只靠 hook 自身
> 副作用（file-sentinel 搬文件、tool-track 写日志）兜底。仍未做的是**受控实验**（装 exit-1
> PostToolUse hook 看是否阻断后续轮次）——「是否影响后续轮次」保持未决，见「待验证」节。
>
> ⚠️ **U1 (narrowed by 2026-08-15 production observation)**: PostToolUse **exit-2 stderr never
> reaches the model** — observed in production, single session (n=1): the Aug 8 E:\AgentOffice session's file-sentinel
> (PostToolUse) exit-2 quarantined 3 files, yet the block reason had ZERO occurrences in the
> entire wire transcript (no isError tool.result, no context delta). The whole PostToolUse
> channel (stdout + exit-2 stderr) is model-invisible; enforcement survives only via hook side
> effects (file-sentinel moving files, tool-track writing logs). Still missing is the
> **controlled experiment** (install an exit-1 PostToolUse hook, watch whether subsequent turns
> are blocked) — "does it affect later turns" remains unresolved; see "Remaining verification".

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

### PreToolUse（8 hook = 6 命名 + skill-trigger×2）— 阻断通道可用；advisory 曾经 P0 升级为 exit-2，2026-08-24 起改为入队攒发（见顶部注）

| Hook | 类别 Class | kimi 路由 | PR |
|---|---|---|---|
| freeze-guard | 强制 enforce | exit-2 阻断（原生效） | — |
| **task-guard** | advisory | **入队 + UserPromptSubmit 攒发**（2026-08-24；P0 提升已退役——"allowed"文案的 deny 自相矛盾）；恢复每会话一次 NOWARN 去噪 | ~~P0~~ 已退役 |
| **assertion-check** | advisory | **入队 + UserPromptSubmit 攒发**（2026-08-24；P0 提升已退役） | ~~P0~~ 已退役 |
| read-before-edit | 强制 enforce | exit-2 阻断（原生效） | — |
| **bash-guard** | advisory | **入队 + UserPromptSubmit 攒发**（2026-08-24；P0 提升已退役） | ~~P0~~ 已退役 |
| hazard-guard | 强制 enforce | exit-2 阻断（原生效） | — |
| skill-trigger（Write\|Edit + Bash，共 2 条） | advisory | 记录 `Delivered=false` 后**入队**（2026-08-24），不再静默湮灭；UserPromptSubmit 攒发 | ~~P1~~ 已反转 → 队列 |

> P0 升级用的是**内容谓词 map**（读 detail 文本区分「真 advisory」与「成功/干净分支」），
> 不是名字白名单——否则会把 task-guard 刚 auto-create 的那次编辑也阻断。逃生舱
> `FORGE_ADVISORY_PROMOTION=soft`（全宿主）与 `FORGE_KIMI_ADVISORY=soft`（仅 kimi，
> 向后兼容）回退纯 advisory。
>
> 2026-08-23 起 task-guard 的提升宿主（kimi + dsh）每次无任务源码编辑都输出指令式
> block reason：Go 层按 hostcap 规则注入 `FORGE_TASKGUARD_PROMOTED=1`，脚本跳过
> 每会话一次的 NOWARN 去噪——提升语义下标记是旁路（盲重试同一编辑静默放行）。
> 注册表同时向 dsh 开放（准入路径 (b)：通道送达但 advisory 被实证无视，见
> hostcap.go dsh 行）。

### Stop（3 hook = 2 命名 + skill-trigger×1）— exit-2 阻断通道，原生效

| Hook | 类别 | kimi 路由 | PR |
|---|---|---|---|
| task-verify | 强制 | exit-2 阻断（原生效） | — |
| review-stop | 强制 | exit-2 阻断（wire 实证） | — |
| skill-trigger | advisory | **已恢复绑定**（2026-08-20）：记录 `Delivered=false`，不打印 | ~~P1~~ 已反转 |

### UserPromptSubmit（2 hook = 1 命名 + skill-trigger×1）— 唯一的 stdout 注入通道（滞后到下一 prompt）

| Hook | 类别 | kimi 路由 | PR |
|---|---|---|---|
| resume-reinject | 注入 | stdout 注入（原生效）+ **P3 承载冷启动 handoff** + **2026-08-15 承载 plugin staleness advisory**（原 init-suggest/SessionStart 通道三重不可见，见下。注意覆盖范围：resume-reinject 是项目级 hook，非 forge 项目目录在 step 5 之前即退出——staleness advisory 只在 forge 项目内提示，非全局） | **P3** + staleness 迁移 |
| skill-trigger | 注入 | stdout 注入——引擎在全部事件运行并记录（可看板观测），UserPromptSubmit 是唯一**打印/送达**事件 | **P1** 收敛点（2026-08-20 起仅"唯一打印"义） |

### SessionStart（5 hook = 4 命名 + skill-trigger×1）— stdout observation-only，主价值经 P3 迁移到 UserPromptSubmit

| Hook | 类别 | kimi 命运 Fate | PR |
|---|---|---|---|
| skill-scan | advisory 安全 | **失效**（stdout 丢）→ 依赖 CI / 手动 `forge audit` 兜底 | 文档 |
| mcp-scan | advisory 安全 | **失效**（stdout 丢）→ 依赖 `.mcp.json` 审查兜底 | 文档 |
| init-suggest | advisory | **失效**（stdout 丢；plugin staleness advisory 已于 2026-08-15 迁至 resume-reinject/UserPromptSubmit，因本通道三重不可见：kimi 丢 stdout、noise gate 丢 init-suggest PASS、checklog 也无痕。注：代码中本就不存在单独的 kimi-drift 检测——init-suggest 的建议文本随项目快照生成，无独立漂移提醒可迁） | staleness 迁移 |
| **task-resume** | 接续 | **P3 经 resume-reinject 冷启动回填**（首 prompt 注入，sentinel 去重） | **P3** |
| skill-trigger | advisory | **已恢复绑定**（2026-08-20）：记录 `Delivered=false`，不打印 | ~~P1~~ 已反转 |

### PostToolUse（6 hook = 4 命名 + skill-trigger×2）— stdout observation-only；副作用类不受影响

| Hook | 类别 | kimi 命运 Fate | PR |
|---|---|---|---|
| auto-compile | advisory | **失效**（advisory stdout 丢，确认）→ 本就无硬价值，接受 | 文档 |
| workflow-test-guard | 强制(block) | **stderr 不达模型**（2026-08-15 生产观测·单会话，见 U1；forge 仓专属守护）→ 是否阻后续轮次未受控验证，依赖 CI 兜底 | 文档 |
| file-sentinel | 副作用 | **不受影响**（写 quarantine 快照，靠文件副作用；wire 实证快照存在，且 8/8 生产实测 exit-2 隔离 3 次成功——只是隔离原因模型看不见） | — |
| tool-track | 副作用 | **不受影响**（写 toollog + reads-log，靠文件副作用） | — |
| skill-trigger（Write\|Edit + Bash，共 2 条） | advisory | **已恢复绑定**（2026-08-20）：记录 `Delivered=false`，不打印（stdout 不可达模型） | ~~P1~~ 已反转 |

### PostCompact（1 hook）— 观察通道，部分失效

| Hook | 类别 | kimi 命运 | PR |
|---|---|---|---|
| compact-resume | 副作用(设标记) | 若 PostCompact observation-only 则标记不设 → resume-reinject 压缩重注入在 kimi 下部分失效；P3 冷启动注入覆盖主场景，压缩重注入列为已知项 | 文档 |

## 已知失效项与兜底 / Known-inert hooks & fallbacks

四 PR 合入后，kimi 上仍**knowingly 失效**的 hook（接受 + 依赖兜底）：

1. **auto-compile**（PostToolUse advisory）— 无硬价值，接受。
2. **skill-scan / mcp-scan**（SessionStart 安全扫描）— 滞后到 CI / `forge audit` / `.mcp.json`
   审查。深度防御层，非唯一闸门。
3. **init-suggest + kimi-drift 提醒**（SessionStart）— stdout 丢；plugin staleness advisory
   已于 2026-08-15 迁至 resume-reinject（UserPromptSubmit）+ 触发时记 `kimi-plugin-stale`
   warn checklog 条目（原三重不可见：模型/用户/日志全无信号）。
4. **workflow-test-guard**（PostToolUse block）— 见下「待验证」。
5. **compact-resume 压缩重注入**（PostCompact）— P3 冷启动注入覆盖冷启动主场景。

## 待验证 / Remaining verification

**PostToolUse exit-2 是否阻断后续轮次**是唯一未决项。2026-08-15 生产观测已收窄一半：
**stderr 不达模型**（file-sentinel exit-2 隔离 3 次、原因在 wire transcript 零出现），
即「模型可见性」层面确认失效；未决的只剩「是否影响后续轮次」（kimi 内部对 PostToolUse
非零退出是否有任何处置）：

- **若 exit-2 连轮次都不影响（全 observation-only）**：`workflow-test-guard` 在 kimi 下失效，
  依赖 CI 兜底（已是 workflow 变更的权威闸门）。`auto-compile` 本就失效。`file-sentinel` /
  `tool-track` 副作用类不受影响。
- **若仍阻断后续轮次（仅 stderr 不可见）**：`workflow-test-guard` 的 enforcement 继续工作
  （模型看不到原因，但被拦住），只有 `auto-compile` advisory 失效。

**活体验证方法**（需在真实 kimi 会话执行，claude-code 会话无法代办）：在 kimi 会话装一个
`exit 1` 的 PostToolUse hook，观察是否阻断后续模型轮次。结论回来后订正本表与
`internal/agentbridge/kimi.go` 的「未决」措辞。

> 无论哪种结论，**advisory 路由不变**（auto-compile 都失效、副作用类都不受影响），故 P2
> 的实现不依赖于此验证——但措辞准确性依赖之。

## 自举摩擦 / Bootstrap friction

forge 门禁用**已安装的** forge 二进制（如 `npm-global/forge.exe`），不是 `go run`。改完
源码须 `go build -o <that path> ./cmd/forge` 重建，门禁才用上 fresh binary（P0/P1/P3 有
行为变更时尤其重要；P2 仅改注释，可不重建）。
