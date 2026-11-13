# ADR-0002: 接管点从宿主安装态迁到仓库，宿主层整体删除（阶段 D）

**状态**：Accepted
**日期**：2026-11-12
**决策参与者**：用户（MjxUpUp）、DSH agent（数据取证与草案）

> **落地状态（2026-11-13，阶段 D 已合入 `5cfef45b`）**：宿主层已整体删除——`internal/hooks`、
> `internal/hookdispatch`、`internal/agentbridge`、`internal/hazard`、`internal/harnessaudit`、
> `internal/skillgen`、`internal/freeze` 不存在；CLI 的 `hook`/`integrations`/`plugin`/`uninstall`/
> `freeze`/`doctor`/`data-dir` 全部移除；`forge init --agents` 保留为隐藏 no-op（传入打印废弃警告并忽略）。正文保留决策当时（阶段 A–C）
> 的推理与判据原文，凡写「可选适配器」「`forge integrations enable`」「`FORGE_HOOKS=on`」
> 「`forge hook batch`」处均属历史；阶段 D 的落地与验收见文末「阶段 D 的落地进度」。

## 背景

决策当时（2026-11-12）Forge 把「是否接管一个项目」的判据放在安装态：`forge init`/`forge plugin install` 往 `~/.claude/settings.json`、`~/.codex/hooks.json` 等宿主配置里写 hook，hook 再按全局 profile 决定跑哪些检查。仓库侧只有 Task 1–4 落下的 `spec/forge.yaml`（成员资格、profile、hooks 开关）与 `forge change` 生命周期。

这条路线在真实使用中暴露出三类问题，全部有本次会话的取证：

1. **宿主适配是脆弱面，不是价值面。** 本批 6 个任务（Task 1/2/3/4/6 与死代码清扫）**每个**都用过逃生舱，主因是 work-activity：DSH 把官方 `read` 换成自有工具后，forge 的 toolusage 数不到 Read/Edit，节奏门禁恒判红。同类问题还有 winuxcmd shim 让 hookdispatch 11 个测试假红、Windows 8.3 短名让 junction 解析失败。这些都是宿主集成缺口，不是纪律问题——修一次要改一层适配，换一个宿主再来一遍。
2. **安装态判据制造噪音。** `init-suggest` 会在任意非 Forge 仓库的首次会话里注入「建议 forge init」，用户在 m00078 明确反馈「命令/hook/文件检查仍会无脑执行，噪音、污染、浪费 token」。Task 3 把「仓库契约决定 hook 是否运行」做出来之后，噪音问题在仓库侧解决，但接线仍然依赖安装态。
3. **趋势不在插件侧。** 各家 agent 都在向项目级 `AGENTS.md`（Codex/Cursor 原生，Claude Code 走 `CLAUDE.md` 指针）收敛。把接管写进仓库文件，换机器、换 agent、换会话都能复现；写进宿主配置，clone 到另一台机器就丢。

规模对比（`git ls-files '*.go'` 统计，快照 `b161f397`，2026-11-12）：

| 层 | 目录 | 行数 |
|---|---|---|
| 宿主层 | `internal/hooks`（26 文件） | 8,813 |
| 宿主层 | `internal/hookdispatch`（47 文件） | 12,041 |
| 宿主层 | `internal/skillsdist`（13 文件，含 4 个宿主 adapter） | 3,179 |
| 宿主层 | `internal/skillgen`（6 文件，生成 CLAUDE.md） | 2,033 |
| 仓库层 | `internal/specroot`（660） + `internal/change`（5,119） + `internal/projectroot`（761） | 6,540 |

上表是 Go 行数（`git ls-files '*.go'`，不含 `templates/*.md`），快照 `b161f397`。宿主层 26,066 行，约为仓库层的 4 倍。ADR-0001 已把「deterministic where code can verify」定为宪法，而当前最大一块代码恰恰不可 deterministic——它依赖宿主是否照约定调用。

## 决策

1. **接管点写在仓库里**：`spec/forge.yaml`（成员资格 + profile，另留 `hooks` 兼容字段）与 `AGENTS.md`（托管块 `<!-- forge:begin -->`…`<!-- forge:end -->`，`forge` 同步托管块，标记外内容不覆盖）。clone 即生效，不要求装插件、不要求注册表。托管块生成器已提供：`forge change agents` 负责同步（缺文件整份写、无块追加、有块就地替换，标记外不动，二次运行不改文件），`forge change init` 在首次声明契约时调用它；契约已存在时 init 拒绝重跑，用 `forge change agents` 重同步。单源多指针由同一命令维护：`CLAUDE.md` 里放 `@AGENTS.md` 导入，`.cursor/rules/forge.mdc` 的 frontmatter 保证 `alwaysApply: true`（新建或无 frontmatter 时补一份；已有 frontmatter 缺这一键时补这一行、其余键不动，修复发生在托管块外）；默认只维护已存在的 `CLAUDE.md`（`.cursor/rules/` 目录存在时补 `forge.mdc`，是唯一例外），`--pointers` 点名才凭空创建，`none` 完全不动。另有一套更早的 `<!-- FORGE:START -->`…`<!-- FORGE:END -->` 段（`forge init --project` 时代写进 CLAUDE.md/AGENTS.md 的指令资产；`--project` 现为 no-op，旧仓库里仍可能留着），与本托管块互不覆盖。迁移注意：旧版 `change init` 写的整份协议没有标记，升级后首次同步会把托管块追加在它后面（两段协议并存）——手工删掉旧正文即可，同步不会动标记外内容。
2. **分两层：循环节拍放仓库（`forge change status` 驱动），强制点放 git/CI。**
   - 节拍层替代宿主 in-loop 拦截：agent 每次动手前后读 `forge change status`，按 `nextSteps` 执行；节点级 `gate` 立刻给 verdict，反馈延迟是一次 CLI 调用而不是一次提交。这是 LoopSpec 的做法（见下节「回环契约」）——它的 hook 只做一件事：拒绝手改已确认的 plan，门禁本体全在 CLI。
   - 边界层默认接线，绕过需要显式动作：`forge change verify`（可执行判据 + 证据封印）的结论由 `forge change check` 送进 pre-commit 与 CI：每个未归档 change 必须有一份与当前代码和判据一致的封印证据，否则退出 2。仓库下发两份示例（`.githooks/pre-commit`、`.github/workflows/change-verify.yml`）。能绕过的只有显式动作：`git commit --no-verify`、把未封印的 change 手工 `git mv` 进归档（等于宣布已交付）、或仓库设置里没勾 required status check；hook 在没装 forge 的机器上放行，CI 则是从源码构建 forge（不存在「缺 forge 放行」）。**要让它在托管平台上真正成为合并前提，还需在仓库设置里把该工作流设为 required status check**——这一勾不在代码里，fork 与私有仓的默认策略也不同。
   - `AGENTS.md` 承载协议文字，但没有退出码，因此只作为节拍层的说明书，不作为强制机制宣传。
3. **宿主层整体删除（阶段 D 执行）**：没有 hook 接线、插件安装或用户级设置写入，也没有 opt-in 路径——`forge integrations enable`、`FORGE_HOOKS=on`、`forge hook batch` 都不再存在，默认与可选两条口子一起关掉。质量检查只在 `forge task gate` / `forge change verify` 进程内执行；`spec/forge.yaml` 的 `hooks` 字段只作兼容保留（合法值 `on`/`off`，缺省 `off`，`forge status` 会显示值，但没有消费方）。这是阶段 A–C「降级为可选适配器」的终局：可选适配器也没留下——它唯一的独特价值（in-loop 拦截）在取证里没有兑现过，见下节数据。
4. **删除阻断式 hazard-guard，不保留等价物**（阶段 D 已执行；阶段 B/C 期间仍是 opt-in 阻断，见阶段表）：数据见下；由宿主自带的危险命令确认层承接（Claude Code / Codex / DSH 均有）。如需提醒，只允许写进 AGENTS.md 的文字约定，不得阻断、不得触发 safe-halt。
5. **留一个显式的小改动口子（fast 通道），不做成「偷偷无视协议」**：`forge change new <id> --fast` 只要求 proposal + acceptance 两份产物，acceptance 至少一条可执行判据；`verify` 照常封印并在 `summary.json` 标 `fast: true`，`change list` 标注「fast（快速通道：只要求 proposal + acceptance）」（该 change 有证据状态备注时——已封印/已失效/失败/缺 acceptance——输出「fast；…」）。判据由人 review，工具不猜「这次改动够不够小」——口子的价值是**可见**，不是**自动**。周界建议写在 AGENTS.md（例如 ≤3 个文件且 ≤50 行），但不做代码阈值，阈值一判就假。
6. **分四阶段迁移，每阶段有可验证判据**（见「实施备注」），任一阶段未达判据即停，不进入下一阶段。

### hazard-guard 的取证（本地取证，仓库外，不可复核：153 条事件，分布在 23 个有事件日）

| 事件类型 | 次数 |
|---|---|
| block | 109 |
| confirm | 14 |
| release | 11 |
| halt-release（人工解停机） | 10 |
| data（审计附属记录） | 9 |

109 次 block 去重后是 67 条命令（68 个指纹）。按命令分类（按事件计）：`git push --delete` 28 次（force 0 次）、含 `rm -rf` 的 9 次、`git rm` / `rm` 技能文件 8 次、`git reset --hard` 8 次（4 条唯一命令）、`worktree remove --force` 2 次；其余 54 条（40 条唯一）是内联脚本被当成一条命令解析——22 条含 heredoc，25 条含 Python 代码（其中 4 条是 `python -c`），多数并非破坏性动作。9 次 `rm -rf` 中 7 条目标是 `/tmp` 下的临时目录（`dbg`、`$SMOKE`、`demo`、`forge-npm-verify`），另 2 条是 `git rm -r --cached internal/clone` 后紧跟 `rm -rf internal/clone internal/cli/clone.go`（仓库内缓存连带工作区副本清理）——**没有一次是真实数据损失**。

代价是真实的：2026-10-25 连续 10 次 block 触发 safe-halt，此后全部门禁工作被冻结，直到 2026-11-12T10:18 由人在真终端执行 `forge hazard halt release --yes` 才解开（该命令已随 `internal/hazard` 删除，这里记录的是当时动作）——18 天，起因是一次脚本循环。

同一类「用模式匹配判断 shell 字符串危不危险」的问题，我们在 `forge change` 的判据检查上已经打了 6 轮对抗评审（引号、注释、续行、`$'...'`、算术、进程替换……），结论是**只能做启发式，不能做到可靠**。hazard-guard 面临同一个对抗面，而它的失败模式（误判 → 冻结全部工作）比漏判更糟。

## 回环契约（借 LoopSpec 的循环模型）

「拆掉宿主 hook 就没法 in-loop 反馈」是个假两难。LoopSpec 给出的答案是：**循环的节拍由 CLI 的输出驱动，而不是由宿主拦截工具调用驱动**。它的 `agent-protocol.md` 把这件事写死成八节流程，关键约定可以直接搬：

| LoopSpec 的约定 | 作用 | Forge 的对应物（现状 → 要补） |
|---|---|---|
| 只有 CLI 能改工作流状态，agent 不许手改 `plan.yaml`/`.gates/`/`.attempts/` | 状态可信 | 已更强：状态由产物 + 证据 + 内容哈希推导，没有独立状态库；要补「agent 手写证据不算数」的显式判据（已有 `checks>0` + `acceptance_sha256` + diff 绑定） |
| 循环执行 `change status`，按 `nextSteps` 动手；`active` 之外没有「继续」 | 停止条件集中在一处 | `forge next` 已有（任务级）；`forge change status` 已落地（change 级：stage + nextSteps + verdict，`blocked` 时不给命令） |
| 节点 = instruction + template + PASS/FAIL 模板；产物写到 `resolvedOutputPath` | 每步有明确产物与判据 | 已有 tasks.md（每条带「判据：acceptance #N」）与 acceptance.md 围栏；节点级 `forge change gate --task N` 已落地（只跑该节点判据、落节点证据、不封印整条） |
| 有效 FAIL 且有返工次数 → `plan rollback`，按失败 Gate 的 `on_fail` 重置目标 + 该 Gate + 全部下游，归档工作流文件，业务代码不动；重做前先读 `priorAttempts` | 返工有边界、能记住死路 | 下游失效已由内容哈希自动完成（改文件即 stale）；`prior_attempts` 已落地（未封印时列最近 10 次失败）；「返工只重置证据、不动代码」的指令仍待写进 AGENTS.md 协议 |
| `exhausted` 的 Gate（无 `on_fail` 或次数用完）→ **停下交给人**，不自动放行 | 循环知道何时停 | 这正是本仓库 `loop-contract` 提案 B0 的漏洞（`TestLoopExhaustionNoLeak` 复现的「首次检出放行一次」）；本 ADR 要求 `status` 直接返回 `blocked`，不回落到「继续」 |
| 证据绑定 plan 摘要；修订后 code gate 必须重新审查 | 证据不可复用 | 已有 `acceptance_sha256` + `diff_sha256` + `BoundHead`；沿用 |
| 中断不需要特殊处理：重跑 `status`，未完成的命令会再次出现 | 断点恢复 | 已有：状态从文件系统推导，Ctrl-C 不留半状态 |

三条设计推论：

1. **in-loop 反馈不依赖宿主**。节点 gate 是普通 CLI 调用，任何能跑 bash 的 agent 都能用；宿主 hook 只在「想更早发现」时才有价值，而它的成本已经在数据里（见下节）。
2. **「继续」必须由 verdict 授权**。`status` 返回的 `nextSteps` 是唯一合法的下一步；失败时返回返工指令，耗尽时返回 `blocked`（`blocked` 时 `nextSteps` 里不含可执行命令，恢复命令放在 `handoff` 字段，由人执行）。agent 想跳过就只能无视协议，而那会被边界层的闸门抓住：`forge change check` 在本地 pre-commit 与 CI 里跑同一套判据（未封印/失效/失败的 change 直接失败）。托管平台上的 required status check 仍需人工勾选，未勾选时强制力来自 review 与分支保护。
3. **门禁本体留在 CLI，hook 只做窄护栏**。LoopSpec 的 pre-tool-use hook 只拒绝「手改已确认 plan」这一件事，35 个条目的复杂度它没有。这也从侧面支持决策 3 的终局：宿主层连可选适配器都不必留，因为它的独特价值只是「早一点发现」，而不是「能发现」。

## 考虑过的方案

### 方案 A：维持宿主层，继续补适配

**描述**：保留现有 hook 体系，为每个宿主补适配（DSH 的 read 计数、winuxcmd shim、各宿主 settings 格式）。

**优势**：
- in-loop 拦截能力保留（唯一能 exit 2 拦下动作的层）。
- 现有 6 个任务的门禁经验可直接复用。

**劣势**：
- 适配数量随宿主数量线性增长，每层都要重新踩坑（本批 6 个任务全部因此降级到 89 分/Weak 证据）。
- 与「换机器、换 agent 也能跑」的目标相反：宿主配置不进 git。
- 与社区向 `AGENTS.md` 收敛的方向相反。

### 方案 B：仓库接管 + CI 强制，宿主层降级（本决策）

**描述**：接管写进仓库文件，强制点放 git/CI，宿主层退出默认路径（阶段 D 起整体删除）；删除阻断式 hazard-guard。

**优势**：
- 同一仓库在任何宿主、任何机器上行为一致；仓库层只有 6,540 行，可维护。
- 强制点落在 git 边界——不可绕过的是接好之后的 CI required check 与分支保护，而不是「哪个 agent 恰好装了什么」。
- 证据封印（`forge change verify` 绑 diff + sha256）本来就是按提交边界设计的，与 CI 天然对齐。
- 删掉约 2.6 万行宿主代码与一个已证明误判为主的拦截器。

**劣势**：
- 失去 in-loop 拦截：坏改动要到提交/CI 才被挡，反馈延迟从「一次写文件」变成「一次提交」。
- AGENTS.md 支持度不一（Claude Code 读 CLAUDE.md、Cursor 读 `.cursor/rules`），需要单源多指针，指针文件会进仓库。
- 阻断式命令防护没有仓库等价物，属于有意放弃的能力。

### 方案 C：只写 AGENTS.md，不设强制点

**描述**：全部依赖 agent 自觉读 AGENTS.md 并执行 `forge change verify`。

**优势**：最轻，零安装、零 CI 配置。

**劣势**：advisory 没有退出码，agent 可以完全不跑验证就宣称完成——那正是 ADR-0001 要治的「agent-claim 冒充 deterministic 证据」。**不采用**：这会把门禁退化成自我声明。

## 理由

选方案 B。判据是 ADR-0001 的宪法：能在代码里验证的，就不该寄望于人/宿主的自觉。

- 仓库层能 deterministic 回答的问题（产物在不在、判据过不过、证据绑没绑这次 diff），不应该依赖「宿主是否按约定调用 hook」这种不可验证的前提。
- 宿主层唯一不可替代的能力是 in-loop 拦截，但它的代价已经在数据里：6 个任务 89 分封顶、18 天停机、11 次逃生舱。用 2.6 万行不可验证代码换「早 30 秒发现」，不划算；CI 上晚几分钟发现，换来任何 agent 都跑得动。
- 方案 C 被 ADR-0001 直接否掉：没有强制点的协议只是建议，会把 deterministic 证据退回 agent-claim。

接受的权衡：**主动放弃阻断式危险命令拦截**。理由是它在本机 23 个有事件日 / 109 次拦截里没有一次拦住真实损失，却造成 18 天停机；且模式匹配在对抗面上不可靠（同类问题在 `forge change` 判据上已证明只能启发式）。若未来仍需要，应在宿主侧做（宿主有人的确认界面），而不是在仓库工具里假装能判。

## 影响

### 正面

- 接管判据全部落在仓库内，可 review、可 diff、可回滚。
- 宿主层已整体删除，安装噪音（init-suggest、settings 改写、plugin shadow）随之消失。
- 仓库代码量下降约 2.6 万行；测试面同步收缩（宿主适配测试、shim 相关的假红一次性消失）。
- 与「每个阶段都有产物、可审计、脱离一台机器也能跑」的目标一致（用户 m00078 的原始诉求）。

### 负面

- in-loop 反馈从「宿主拦截」变成「协议驱动 + 节点 gate」：延迟是一次 CLI 调用，但 agent 可以无视协议（LoopSpec 同样如此），兜底只能是接进 CI 的 required check。
- 危险命令防护降级为「宿主自带确认 + 评审 + 分支保护」。
- 需要维护 CI 工作流与 pre-commit 示例；需要单源多指针（AGENTS.md → CLAUDE.md、.cursor/rules 的单向指针）。

### 风险

- **AGENTS.md 不被读**：agent 跳过协议直接改代码。缓解：`forge change check` 在 CI 里是兜底（随仓库下发工作流；设为 required status check 后才是硬前提）；本地 pre-commit 在提交时就能给结论，但它看的是工作区（`git add` 后又改回文件属已知缺口），且 `git commit --no-verify` 能绕——所以要两条都留。
- **CI 绕过**：管理员直推或跳过 required check。缓解：分支保护 + CODEOWNERS，属于仓库治理而非工具能解。
- **迁移期双轨（阶段 B/C 历史风险，阶段 D 已消除）**：宿主 hook 与 CI 同时存在会有两套结论冲突。当时的缓解是阶段 B 起默认 `hooks: off`、宿主层只保留显式开启；阶段 D 删除宿主层后这条风险不再存在。
- **删除过猛（阶段 B/C 历史风险）**：删掉 hookdispatch 时误伤 batch 协议（Codex 仍可能调用）。实际执行时先 grep 外部调用点与文档，确认无外部消费方后整体删除，没有保留废弃告警——没有调用方需要告警。
- **fast 通道被滥用**：小改动口子会让一部分人绕过 design/tasks。缓解：`fast: true` 落在 summary.json 与 `change list`，review 与合并时可见；需要时可被 CI 规则单独统计比例。**刻意不做阈值判定**——「多小算小」由人看 diff 决定，工具只保证它写在证据里。

## 实施备注

### 阶段与判据

| 阶段 | 动作 | 完成判据 |
|---|---|---|
| A 仓库层自足 | ①节拍层：`forge change status`（stage + nextSteps + verdict，耗尽返回 `blocked`）、`forge change gate --task N`（节点级判据 + 证据）、`priorAttempts` 列表；②边界层：`forge change check` 接 pre-commit 与 CI（verify 的封印是它的判据）；③协议层：`AGENTS.md` 托管块生成器 + 单源多指针（CLAUDE.md / .cursor/rules 指向同一段）；④fast 通道（`--fast` 只写 proposal+acceptance，summary 标 `fast: true`） | 一个全新 clone（无 forge 安装、无注册表）能靠 CI 判定 change 是否 sealed；`forge change verify` 在干净 clone 上可跑；一次「节点失败 → 返工 → 耗尽 → blocked」的完整演练，最终结果不是「放行」 |
| B 默认安静（audit-only）（历史） | 执法改为 opt-in，默认 audit-only（静默放行 + attribution 审计；不评估、不阻断、不注入）：`specroot` 契约 `hooks` 缺省值翻成 `off`；有契约的仓库按契约执法（`hooks: on` 才执法，仓库自治，`off` 优先于一切 env）；无契约的宿主层由临时逃生舱 `FORGE_HOOKS=on` 显式开启（阶段 C 由 `forge integrations enable` 取代）。**安全底线语义变更**：hazard-guard/mcp-scan/skill-scan 三条底线不再「不受契约 hooks/profile 控制恒执法」，改为随执法开关默认 audit-only——README / cli.md / agents-protocol 的承诺已同步改写，用户 m07455（2026-11-13）明确同意。**观察窗跳过**：用户自用/dogfood，不需要「hook 独立捕获率」数据，B 完成直接进 C（见后续待定 #2） | build/deadcode 绿、测试不新增失败（基线既有 hookdispatch 假红除外，见 commit body）；无契约仓库 hook 默认静默（root 可解析的已接管仓库仍记 attribution，完全未接管目录无审计记录）；显式 `hooks: on` 的仓库（含 Forge 自身）执法行为不变；承诺文档与代码行为一致。**本节为阶段 B 当时判据，已随阶段 D 整体删除宿主层而作废（存档）** |
| C 降级为可选（历史） | `forge integrations enable <host>` 显式开启；移除 `forge init`/plugin 对宿主配置的写入，并移除 `forge init` 对 `~/.forge` 的全部写入（注册表 `projects.json`、`projects/<key>/protocol.yml`、hooks 资产、harness-state、迁移元数据等）——只留仓库内产物；`init-suggest` 删除 | 在临时 `HOME` 里跑一次 `forge init`：`find "$HOME" -type f -exec sha256sum {} +` 前后快照一致（覆盖未跟踪的新文件），且 `~/.claude`、`~/.codex`、`~/.forge` 均无新增。**本节为阶段 C 当时判据，已随阶段 D 删除 `forge integrations` 而作废（存档）** |
| D 删除（已执行） | 删 `internal/hooks`、`internal/hookdispatch`、`internal/agentbridge`、`internal/hazard`、`internal/harnessaudit`、`internal/skillgen`、`internal/freeze`；`internal/skillsdist` 只删宿主 adapter 与 plugin shadow（`ListSkills`/`TargetDirs`/`Install`/`DriftCheck` 等核心分发保留）；删 CLI 的 `hook`/`integrations`/`plugin`/`uninstall`/`freeze`/`doctor`/`data-dir` 命令与宿主侧生成物（`plugins/`、`.claude-plugin/`、`.cursor-plugin/`、`.kimi-plugin/`） | `go build ./...` 与 `go vet ./...` 通过；`make deadcode`（`deadcode -test ./...`）零发现；坑位守卫 `scripts/checks/{no-hook-refs,deadcode-zero}.sh` 通过；`forge compat snapshot` 重钉后 194 条（193 子命令 + 根）逐字节一致；`docs/reference/cli.md` 与 `README.md` 无残留引用。证据见提交 `5cfef45b` 与文末「阶段 D 的落地进度」 |

**阶段 A 的落地进度**（feat/loop-contract-status、feat/agents-managed-block、feat/change-check、feat/phase-a-rest 四条分支）：阶段 A 的四项功能已落地——① 节拍层（`change status` 的 stage/verdict/next steps、`change gate --task N` 的节点判据与证据、同因失败 `blocked`；`blocked` 时 `next_steps` 不含可执行命令，恢复命令在 `handoff`；`prior_attempts` 列出最近 10 次失败的命令、退出码与证据文件，返工前先读）、② 边界层闸门本体（`forge change check` + 随仓库下发的 `.githooks/pre-commit` 与 `.github/workflows/change-verify.yml`；required status check 需在仓库设置里勾选，属运营动作）、③ 协议层（`AGENTS.md` 托管块生成器 + 单源多指针：`CLAUDE.md` 的 `@AGENTS.md` 导入与 `.cursor/rules/forge.mdc`；默认只维护已存在的 `CLAUDE.md`，`.cursor/rules/` 目录存在时补 `forge.mdc`、`--pointers` 点名才凭空创建、`none` 完全不动）、④ fast 通道。

ADR:154 的三条完成判据逐条状态：① 「节点失败 → 返工 → 耗尽 → blocked，最终不放行」已实测（2026-11-12，3 次同因失败后 verdict=blocked、`next_steps` 无可执行命令、恢复命令进 `handoff`、`prior_attempts` 列出三条失败证据）；② 「`forge change verify` 在干净 clone 上可跑」已实测（`git archive` 导出后删掉 dogfood 的 `spec/`，`change init` → `change new` → 填四份产物 → `verify` 封印 → `git init`/commit → `git clone`；新 clone 里 `change status` 仍为已封印、`verify` 重跑通过、`change check` 退出 0）；③ 「全新 clone 靠 CI 判定 sealed」的作业步骤已在上述 clone 里本地等价复现（`go build ./cmd/forge` + `change check` 退出 0），真实 PR/CI 与 required status check 勾选属运营动作，待首次真实 PR 记录（本仓无 `spec/changes/`，下发工作流在此仓库按设计跳过）。

**阶段 B 的落地进度（历史，阶段 D 已删除宿主层）**（feat/phase-b-audit-only）：默认 audit-only 已生效——`specroot` 契约 `hooks` 缺省值翻成 `off`；`FORGE_HOOKS=on` 是宿主层唯一的执法逃生舱；三条安全底线随执法开关，不再恒执法；README / cli.md / agents-protocol 的承诺已同步（用户 m07455 同意）。

**阶段 C 的落地进度（历史，阶段 D 已删除 `forge integrations`）**（refactor/phase-c-optional-host）：`forge init` 改为只写仓库内产物（`spec/forge.yaml`、`spec/changes/`、AGENTS.md 托管块与已存在指针），不写 `~/.forge`、`~/.claude`、`~/.codex`，也不再登记注册表；用户级接线迁到新命令 `forge integrations list/enable/disable <host>`（12 宿主，enable 才写 hooks/指令/skill/注册表并打 `~/.forge/integrations/<host>` 标记；copilot 与 dsh 无可写用户级资产，只走 plugin manifest）；autoSync 按「Claude plugin 已装 > 墓碑 > 接线标记 > legacy 遗留接线」判定是否写用户级资产（disable 摘光标记后写 `.forge/integrations/.disabled` 墓碑，enable 清掉；Phase C 之前已接线的老仓库仍走迁移同步）；更新检查跳过 `init`；`projectroot` 遗留自愈的 `registry.Add` 移除（纯读解析）。`init-suggest` 全链删除（hook 脚本、SessionStart 接线、`FORGE_INIT_SUGGEST` 读侧、`.init-suggested` 标记、`takeover` 三档偏好、`forge config set takeover`、`forge plugin status` 的提示用途与文档叙述）。判据：临时 `HOME` 下 `forge init` 前后 `find "$HOME" -type f` 快照一致，且 `~/.forge`、`~/.claude`、`~/.codex` 无新增（由 `TestInit_WritesOnlyRepoProducts` 与 `TestIntegrations_EnableAndDisable` 覆盖；该测试随 `integrations` 命令在阶段 D 删除）。

### 阶段 D 的落地进度（已合入 `5cfef45b`，修残留 `069dd392`）

- **删除的包**：`internal/hooks`、`internal/hookdispatch`、`internal/agentbridge`、`internal/hazard`、`internal/harnessaudit`、`internal/skillgen`、`internal/freeze`。`internal/skillsdist` 保留核心分发（`ListSkills`/`TargetDirs`/`Install`/`DriftCheck`/`manifest`/`profile`），只删宿主 adapter 与 plugin shadow（`plugin_shadow.go` 及其测试、`DriftReport.PluginShadows` 字段、`forge skills drift-check` 的 plugin-shadow 视图）。
- **删除的命令**：`hook`（含 `hook batch`）、`integrations`、`plugin`、`uninstall`、`freeze`、`doctor`、`data-dir`；`forge init --agents` 保留为隐藏 no-op（传入打印废弃警告并忽略）；`forge update --plugin` 移除（flags 只剩 `--apply`）。
- **验收**：提交 body 记录 acceptance 5/5、`task-verify passed`、`go vet` 绿、deadcode 零、`go build` 绿；新增坑位守卫 `scripts/checks/no-hook-refs.sh` 与 `scripts/checks/deadcode-zero.sh`，`make deadcode` 走 `deadcode -test ./...` 口径。
- **兼容面**：`forge compat snapshot` 重钉后 194 条（193 子命令 + 根），`internal/cli/compat_golden_test.go` 要求与 `compat.snapshot.json` 逐字节一致——`forge update` 条目的 flags 只剩 `apply`，这一改动随阶段 D 一起入库。
- **文档**：`docs/reference/cli.md` 与本文按「无宿主层」改写，`hooks.md` 与插件 README 删除。
- **已知残留**：`internal/cli/verify_scenarios.go` 的 E2E 场景仍引用已删的 `forge data-dir` 与 hooks 副本，`forge verify --regression/--scenario` 当前不可用（`docs/reference/cli.md` 已如实标注）；`internal/cli/init.go` 的 `--agents` 隐藏 no-op 与警告分支是为旧脚本兼容保留的，阶段 D 之后没有宿主接线可接。

### 受影响模块

- 删除清单（阶段 D 已执行；行数为删除前快照 `b161f397` 的统计）：`internal/hooks`（8,813 行）、`internal/hookdispatch`（12,041 行）、`internal/skillgen`（2,033 行，CLAUDE.md 生成部分）、`internal/skillsdist`（3,179 行中的宿主 adapter 与 plugin shadow——包本身保留）。
- 保留并加强：`internal/specroot`、`internal/change`、`internal/projectroot`、`internal/cli`（`change`/`status`/`policy`）。
- 新增：CI 工作流与 pre-commit 钩子示例已随仓库下发（`.github/workflows/change-verify.yml`、`.githooks/pre-commit`）；`AGENTS.md` 托管块渲染已落地。

### 后续待定（不阻塞阶段 A）

1. ~~hazard-guard 在阶段 D 是彻底删除还是保留一个只提示、不阻断的版本~~ **已决**（阶段 D）：`internal/hazard` 整体删除，不保留提示版；执行记录见「阶段 D 的落地进度」。
2. ~~阶段 B 的观察窗是两周还是直接合并进一次发布~~ **已决**（2026-11-13，用户 m07455「继续做阶段B 做完B就做C，不考虑观察 我们是自己在用」）：跳过观察窗，B 完成直接进 C。自用/dogfood 场景独立捕获率数据无决策价值；阶段 D 将删除 hook 代码，测量投入会被浪费。原「hook 拦下但 CI 本会拦下」比例记录随之取消。

## 参考资料

- `docs/design/spec-in-repo-activation-2026-11-proposal.md`（仓库内 spec 层的原始提案，已被本 ADR 取代；保留作为九份行业材料的摘录）
- `docs/adr/0001-constraint-three-way-classification.md`（deterministic vs agent-claim 宪法）
- `docs/design/positioning.md`（「coding agent 循环的验证与状态层」定位）
- LoopSpec `docs/zh/agent-protocol.md` 与 `docs/zh/cli-reference.md`（本 ADR「回环契约」一节的模型来源：CLI 独占状态写入、status→nextSteps 驱动、节点 PASS/FAIL、on_fail/rollback、exhausted 停下）
- `docs/plans/loop-contract-2026-10-proposal.md`（B0 回环耗尽放行漏洞、B1 `loop_verdict` 与 `loop:` 预算段，本 ADR 与它对齐）
- 本地取证（仓库外，不可复核）：原 `~/.forge/projects/<key>/hazards/events.jsonl` 的 153 条事件快照，跨 23 个有事件日；`internal/hazard` 删除后该路径不再产生新数据
- 用户反馈原话（m00078）：「当一个项目并不是 forge 接管时，我们的命令/hook/文件安全检查/代码检查仍会无脑执行……完全可以让 agent 无需明确安装我们的插件」