# A–G 七项修复落地设计（2026-09，跨机器审计驱动）

依据：两台机器的同口径审计——机器甲（项目 6d5e51a0f9d4，90 任务）：《Forge 本机表现审计与修复度量基线》飞书 wiki CPDbwKxvxi6uz0k2R86clgnjnlg；机器乙（项目 8db5a0d1b70f，33 任务）：其兄弟页 L1XRwn3UFihYsrkThnjckjGxnbf（复算与基线校准）。两审计一致判定的系统性缺陷为 A（skill-trigger 高噪低转化）、B（forge next 零采纳 + 范式漂移）、C（门禁命令嵌组合命令）、D（references 下钻不足，host 分层）；机器乙新增 E（完成后归因泄漏）、F（hazard 误报 + 确认非真人）、G（覆盖软门禁被忽略）。本文按 461ef57「事前成功度量门」要求：每项先钉 基线→目标→防伪护栏，M（度量基建）先行，实现合入前基线可复算。

## 通用约束

- **兼容纪律**（docs/design/compat-commitments.md）：已发布 advisory 转 BLOCKED 预告期 ≥2 个 minor；新 BLOCKED 拒绝文案含预告版本或「首发即 blocked」+ 指向承诺表；每个 BLOCKED 门禁配 `FORGE_*` env 或 per-task override 逃生舱（逃生留痕 + 评分封顶）；`compat.snapshot.json` 六面 golden 随新 CheckName/命令/逃生舱/BLOCKED 位点显式重生成。**版本映射：1.56 = 数据修正 + advisory 批；1.58 = ratchet BLOCKED 批**（1.57 观察；F.2a 首发即 blocked 例外见 B2 行）。
- **命令预算**：单 minor 命令净增 ≤2——本设计新增命令仅 1 个（`forge eval harness-audit`，见 M）。
- **实现纪律**：判定逻辑抽纯函数作单测锚点（`nextDecision`/`BuildConclusion` 先例）；嵌出门禁输出必须走 `GateBlocked`/`GateAdvisory` 前缀（internal/taskpipeline/gate_message.go:33）；新 advisory check 过 `shouldRecordCheck` 噪声门（internal/hookdispatch/hook.go:1279）。
- **两机指标斜杠约定**：`乙/甲` = 乙机（8db5a0d1b70f，33 任务）/ 甲机（6d5e51a0f9d4，90 任务）；单机内部比例写「乙：x/y」明示，不用斜杠。

## 执行批次总览

| 批次 | 版本 | 项 | 性质 |
|---|---|---|---|
| B0 数据完整性 | 1.56 | E、F.3 | 修度量与归因，无 agent 可见面变更（评分口径变更单列） |
| B1 度量基建 | 1.56 | M | 新命令，钉死全部基线，两机同脚本可复算 |
| B2 行为 advisory | 1.56 | A、B、C(adv)、F.1、F.2a、F.2b(adv)、G.1(adv)、G.2 | 触发/输出面变更，advisory 均带 1.58 预告文案；F.2a 例外——首发即 blocked（承诺表 §二.1②，新形态无存量暴露） |
| B3 内容+校验 | 1.56 | D | skills 内容 + R19 校验规则 |
| B4 ratchet | 1.58 | C(block)、G(阈值)、F.2b | 新 BLOCKED 位点，过 compat golden |

## M｜度量基建：`forge eval harness-audit`

- **现状**：两审计的指标靠一次性 python 脚本（toollog 2s 去重口径），不可复算、两机易漂移；对方回测流程明确要求「基线同款脚本原样复用」。
- **设计**：新命令读项目 DataDir（toollog×checklog×tasks 三方交叉，2 秒 Pre/Post 双记去重），一次输出 JSON/表格，覆盖全部修复项的事前/事后指标：A1 日均触发、A2 UPS 转化、A3 verification-driver 精度（触发时点是否真为失败测试命令——按 toollog 实测，替代对方「前 5 分钟存在测试命令」的粗 proxy）、A4 inline 跟随、B1 next 采纳（next-hint 记录后 10 分钟内同命令执行）、B2/B3、C1-C3（门禁命令形态分类器）、D1 refs-critical 下钻（按 origin_tool 分层）、E 泄漏任务数与无 session 记录占比、F 双记率与放行率、G coverage 拦后转 pass 率。判定逻辑全部为 internal/ 下的纯函数（复用 internal/skillmetrics/funnel.go:69 的 join 口径），命令只是渲染层。
- **落点**：internal/skillmetrics/harnessaudit.go（新）+ internal/cliskills 或 eval 命令族挂载（实现时按命令树归属定）；compat snapshot commands/checks 面 regen。
- **验收**：两机各跑一次产出 JSON 入库 `evals/harness-audit-{6d5e,8db5}-baseline-202609.json`；纯函数单测覆盖分类器（standalone/&&/;/管道/多门禁 五形态夹具）。
- **风险**：口径漂移会重造两机分歧——JSON 输出携带 `dedup_window_ms`/`join_window_s`/命令版本字段，回测只对同版本同口径的两份 JSON 作差；A3/A4/F1 的基线由本命令首跑钉死（见各项度量）。

## A｜skill-trigger 通道重构：决策点推送 + 动作点内联

- **现状（证据）**：总转化 12.7%/5%；通道归因一致——UserPromptSubmit 是唯一有效通道（33%/10%），PreToolUse 0%/1%、PostToolUse 2%/0%；merge-release-choreography 在 UPS 上 67%/40%（低频+场景强绑定+决策点三条件成立的实证）；test-nudge 内联指令跟随两机同为 `80%`。噪声源实证：PostToolUse 输出关键词——本设计撰写会话中，输出文本含 `compile error` 字样即触发 compile-fix-loop（skills/compile-fix-loop/SKILL.md:7 keywords 匹配标准输出，无退出码门槛）；test-discipline 的 PreToolUse 关键词 `git commit/git push`（skills/test-discipline/SKILL.md:7）命中 35 次零加载。
- **设计**：按事件分流，改 `internal/skilltrigger/trigger.go` + `render.go`：
  1. **UPS = 决策点**：保持现有完整推送（路径+集成行）。
  2. **PreToolUse/PostToolUse/Stop = 动作点**：仅当 trigger 声明 `inline`（新可选字段，≤2 行动作指令，风格对齐 test-nudge 的 hook_track.go:260）才渲染，形态为 `[forge] <inline>` 一行、无路径无「请加载」；无 `inline` 的动作点 trigger 降级为 Suppressed（cause=non-decision-point，进现有 suppressed 统计，checklog detail 不带 ` hit (` 标记——沿用 recordSuppressed 惯例 skill_trigger.go:237）。
  3. **PostToolUse 关键词精度**：关键词源含输出文本的 trigger（compile-fix-loop）仅在工具退出码非零时评估（退出码取法同 conditions.go:93 exitCodeOf）；`when: test_command_failed` 已是退出码口径（conditions.go:82），保留。
  4. **首批 inline 配置**：test-discipline（"提交前先跑聚焦测试：`go test ./<改动的包>`"）、compile-fix-loop（"编译失败——先读首条完整错误定位根因，禁随机试改"）、verification-driver（"测试失败——先判断是行为 bug 还是测试本身，端到端复现后再改"）。
  5. `trigger.go:630 defaultReason` 的「请加载该 skill」文案随动作点模式一并退场（仅 UPS 保留加载指引）。
- **落点**：internal/skilltrigger/{trigger.go(Trigger struct +inline/follow 字段),render.go(两形态),noise.go}、internal/skillsqa/rules.go ValidConditions 同步、checklog/skill_trigger_detail.go Meta 增 channel-mode、skillmetrics/funnel.go 增 inline 跟随列；canonical skills 的 triggers frontmatter 改 3 处。
- **度量（事前门）**：A1 日均 9/24.3 → ≤8（防伪：≥3——不得靠砍事件清零换转化）；A2 UPS 转化 33%/10% → ≥25%；A3 verification-driver 触发精度——基线：甲 proxy 口径 38%，M 首跑按「触发时点即失败测试命令」口径重钉两机 → ≥80%；A4 inline 跟随（trigger 声明 `follow` 匹配器——如 test-discipline 配 `go test` 命令匹配，30 分钟窗口）——基线：新通道无存量数据，以 M 首跑（1.56 发布后首周）为基线 → ≥50%（test-nudge 80% 为可达性上限参照）。
- **验收**：trigger_test 夹具——UPS 全文/动作点 inline/无 inline 降级/输出关键词零退出码不触发；两 skills 的 eval-gen case 集指纹不变（DescHash，cases.go:92）——keywords 未动，证明零漂移。
- **风险与回滚**：动作点流量骤降可能压掉真实需要的推送——防伪护栏 A1 下限 + 每周 M 复查；回滚 = frontmatter 去掉 inline 字段即回到现状（代码向后兼容旧 triggers）。

## B｜forge next 推送化：挂到门禁输出末尾

- **现状（证据）**：`forge next` 真实调用两机均为 0，而 verify-acceptance 用了 22/57 次——agent 走有产出物的命令；门禁输出是唯一被确定性阅读的界面（甲：139 次 gate 运行、拦截后 `100%` 重跑）；范式漂移的实证是多门禁连刷 24%/15% 与分号续行 26%/12%。
- **设计**：复用纯函数 `nextDecision`（internal/cli/next.go:70，签名不动），在三个输出点末尾追加一行 `→ next: <命令>（<理由>）`：`forge task gate` 通过/BLOCKED 之后、`forge task status`、`forge task complete` 评分行后。同时落 checklog advisory `next-hint`（Meta.suggested=命令），供 B1 采纳率测量。输出属承诺表**不承诺档**（porcelain 可变文本，compat-commitments §一），无兼容义务。
- **落点**：internal/clitask/task_gate.go:127-133、internal/clitask/task_misc.go:77-100、internal/clitask/task_complete.go:186-217；checklog/types.go 新 CheckName `next-hint`（snapshot checks 面 regen）。
- **度量**：B1 采纳率（next-hint 后 10 分钟内执行同命令，toollog 匹配）→ ≥50%；B2 多门禁连刷 24%/15% → ≤8%；B3 分号续行 26%/12% → ≤5%（B2/B3 主执法在 C，此处只看引导性下降）。
- **验收**：task_gate/status/complete 三处输出的 golden 单测；next-hint checklog 行进 snapshot checks 面。
- **风险**：next 行被 `| tail` 截掉——与 C 的 stderr 兜底同批解决；采纳率受 host 影响分层统计。

## C｜门禁命令形态门禁（gate-cmd-form）

- **现状（证据）**：standalone 两机 0%/1%；C1 分号+grep 掩蔽 37%/13%；C2 管道截断 84%/27%（乙机 `2>&1 | tail -N` 习惯根深蒂固）；C3 合规形态 7%/46%。后果实证（甲）：complete 输出接 `grep -E "completed|Score"` 时 BLOCKED 行被吞，agent 靠「没看到预期输出→重跑」恢复；分号续行让前一门禁 BLOCKED 后链条照走。
- **设计**（按 session-retrospective 载体决策树第 1 档——能程序化的不进 skill）：
  1. **形态分类器**（纯函数）：识别命令中的 forge 门禁子命令（`forge task gate|complete`、`forge task verify-acceptance`、`forge review pass`、`forge docs lint`、`forge task doc-review`，含 ./bin/forge-dev 前缀变体），分类 standalone / `cd && ` 前缀 / `&&` 尾段 / 分号续行 / 管道截断（`|` 接 tail/head/grep/cut/sed）/ 多门禁同刷。
  2. **in-process hook**（PreToolUse Bash，仿 test-nudge 的挂载 hook_track.go:207 + settings.go:93）：1.56 advisory——stderr 一行 `ADVISORY: gate-cmd-form …（自 1.58 起 BLOCKED）` + checklog `gate-cmd-form` warn；1.58 转 BLOCKED（规则：门禁命令之后不得接 `;`、`|`、`||`，不得多门禁同刷；`cd X && gate` 与 `gate && 后续` 保留放行——退出码契约仍成立）。逃生舱：`FORGE_GATE_CMD_FORM=0` env + per-task override `--gate-cmd-form disable`（进 overrides.go 枚举与 escapes 面）。
  3. **stderr 兜底**：`forge task gate` 检测 stdout 非 TTY 时（char-device 判定改造自 task_gate.go:166 `stdinIsHumanTerminal` 的 stdin 版——本项判 stdout，是适配不是直接复用），BLOCKED 摘要行同时写 stderr——管道截断不再能吞掉退出码契约的文本面。
- **落点**：internal/taskpipeline/gatecmdform.go（新，分类器+hook）、internal/hooks/settings.go 注册、internal/clitask/task_gate.go stderr 兜底、overrides.go、compat snapshot（checks/escapes/blockings 三面 regen）。
- **度量**：C1 37%/13% → ≤2%；C2 84%/27% → ≤15%；C3 7%/46% → ≥80%。防伪：拦截后重跑率不得劣化（甲基线 `100%`）；单任务 Bash 中位数上浮 ≤30%（乙机现值由 M 钉基线）。
- **验收**：分类器五形态夹具单测；advisory→BLOCKED 文案含「自 1.58 起」字样（承诺表 §二.1 ①）；e2e 式：`forge task gate … | tail -1` 场景下 stderr 可见 BLOCKED。
- **风险**：存量脚本/CI 内嵌门禁连刷会被 1.58 拦——预告期 + 逃生舱 + M 周报监控 advisory 命中存量形态清单，必要时 1.58 只拦「分号+管道」不拦 `&&`（分级 ratchet）。

## D｜references 下钻：refs-critical 声明 + 步骤 0 必读

- **现状（证据）**：总下钻率分歧大（乙 56% / 甲 7%，host 构成不同——乙 zcode/codex、甲 kimi/claude-code/dsh 占多）；refs-critical 两个 skill 两机均为低（transcript-forensics 1/2 与 0/2、release-readiness 0/1 与 0/5）——核心交付物就在 references 里等于没按 skill 执行。
- **设计**：frontmatter 新增 `metadata.refs_critical: [相对路径]`（声明该 skill 不读某 reference 即无法执行核心流程）；R19 校验（internal/skillsqa/registry.go 新规则）：声明 refs_critical 的 SKILL.md 正文必须含「步骤 0」必读块，逐路径点名、整块 ≤5 行；正文其余部分不参与触发匹配的既有约定不变。首批声明：transcript-forensics（references/transcript-formats.md——格式字典是解析前提）、release-readiness（references 四件已核实，见落点；「乙机无该目录」为分发镜像差异，源头树 skills-forge/ 齐全）。防膨胀三道：R19 机械规则；SKILL.md 行数增量守卫（task-verify 时 SKILL.md 在变更集且声明 refs_critical → 新增 >5 行 warn，挂 skill-decisions 现位点 internal/taskpipeline/executor_skill_decisions.go:39）；eval-gen case 集指纹不变（refs_critical 不进触发匹配，DescHash 不动）。
- **落点**：internal/skillsqa/registry.go（R19）+ rules.go；internal/cliskills/skills_validate.go 输出；skills/transcript-forensics/SKILL.md 与 skills-forge/release-readiness/SKILL.md 步骤 0 块（release-readiness 属 forge 原生树——本仓已核实其 references 四件：recommended-checks/decision-tree/checklist-template/gotchas-and-rationalizations，此前「乙机无该目录」是分发镜像差异，源头树齐全；若插件镜像需同步则注明双树）；internal/skillmetrics/funnel.go D1 列（加载后 20 分钟内 Read 任一 refs_critical 路径，按 origin_tool 分层）。
- **度量**：D1 refs-critical 下钻（乙 1/3、甲 0/7）→ ≥50%；D2 单次 SKILL.md 行数增量 ≤5；D3 eval case 指纹不变。防伪：D1 不得靠一次性全读实现（单次加载后 Read refs ≤2 个/会话）。
- **验收**：R19 夹具（有声明无步骤 0 → fail；块超 5 行 → fail）；两 skill validate 过；M 的 D1 分层输出。
- **风险**：refs_critical 滥用成「把 references 搬进正文」的口子——R19 行数上限 + 行数增量守卫双闸。

## E｜完成即冻结归因

- **现状（证据，乙机）**：`fix/env-hermetic-registry` 09-07 23:46 三门禁过 + 评审过，doc-gate 卡结论至 09-09 20:38；期间 50 条无 session_id 记录（含 13 条实为其他任务分支清理的 hazard block、10 次重复 task verify 的 agent-claim）计入该任务 → ratio 0.22 → Weak 误判 + efficiency 35 + RetrospectiveNudge 误触发；另一 Weak（perf-hotpaths）为逃生舱封顶，与验证质量无关。无 session_id 的 checklog 记录占 59%。读侧归因 `ActiveTaskState` 四条路径（session 文件 / workspace 绑定 / branch 映射 / legacy 桥接，state.go:128/139/153/168）均已带 `CompletedAt == nil` 检查——读侧无洞；泄漏在**两个评分读取点**：① 结论证据链走 `checklog.ForTask`（scoring.go:268 → LoadForTask，store.go:226 仅按 TaskRef 过滤、无时间窗，完成后追加的行全部计入）；② 评分输入走 `LatestByCheckForSessionSince`（scoring.go:66），其对 SessionID 为空的条目无条件保留（store.go:265 注释言明）——空 session 行只要 RecordedAt ≥ StartedAt 就混入任意任务。写入侧把 ref 解析到已完成任务的具体路径未定位——**先加探针再修**。
- **设计**（四件，belt-and-braces）：
  1. **归因探针**：checklog 行 Meta 增 `resolve_path`（active-file/workspace/branch/legacy/env-ref 之一）——1.56 落地，定位写入侧泄漏分支；E.2 落地后探针转为 drop 计数。
  2. **写入侧断言**：`checklog.Record` 落行前若 TaskRef 指向 `CompletedAt` 非空的任务 → TaskRef 置空 + Meta.completed_drop=true——已完成任务永不追加 checklog 行（读侧检查已齐，本项补写侧）。
  3. **清除面**：`ClearActiveTaskRef` 扫 `DataDir/active-task-ref-*` 全部指向该任务的 session 文件 + legacy 全局文件一并清（现按单一 sessionID 清，task_complete.go:262；session_links 多会话任务漏清即候选源）；task_abort 同步。
  4. **评分窗口（两个读取点各修一处）**：`ForTask` 增 `until` 参数——结论链传 CompletedAt 截断 `RecordedAt ≤ CompletedAt`，`forge trace` 传零值保持全史；`LatestByCheckForSessionSince` 对 SessionID 为空的条目改为仅当 `e.TaskRef == 被评任务` 才保留（增 taskRef 参数）。`checklog.Record` 时 SessionID 为空且 TaskRef 非空 → 回填该任务 TaskState.SessionID。efficiency 维度输入从挂钟（started→completed）改为活跃工作跨度（任务时间窗内首末 toollog 事件距，无 toollog 回落挂钟）——评分口径变更，进 CHANGELOG 行为变更节。
- **度量**：E1 完成后归因泄漏任务数 2 → 0；E2 无 session_id 记录占比 59% → ≤10%；E3 Weak 误判（机制产物型）2 → 0（以 resolve_path 探针复算为准）。防伪：checklog 总行数不得异常下降（drop 行带 Meta 计数进 M 输出）。
- **验收**：单测夹具——完成任务后 hook 归因返回空 / session 回填 / 多 session 文件全清 / `ForTask` 时间窗截断（until=CompletedAt 与零值两夹具）/ 空 SessionID 条目按 taskRef 过滤；e2e 式复演 env-hermetic 场景（完成→异 session verify）断言零追加。
- **风险**：写侧 drop 过严可能让合法续接（reopen 前）丢归因——reopen 流程有独立写路径（task_continuity.go:388 重设 active ref），不受影响；M 周报监控 drop 计数。

## F｜hazard-guard 提精度 + 确认真人化

- **现状（证据，两机）**：确认全部 agent 自助发起（乙 13/13、甲 53/53，多条与目标命令同 Bash 调用）；乙 52 条 block 60s 窗去重后 28 起（双记 24 条），28 起中仅 6 起确认放行、12 起为「脚本文本误命中」（本设计撰写会话的分析 heredoc 两次被拦——heredoc 体未被当数据）；`CheckHalt` 按原始 EventBlock 计数（halt.go:48），双记直接放大 safe-halt。
- **设计**（三件）：
  1. **F.1 解释器 heredoc 数据上下文**：heredoc 喂给非 shell 解释器（python/node/ruby/perl）且体不含执行原语（subprocess/os.system/os.popen/child_process/exec(/spawn(/system(/sh -c/bash -c）→ 走既有数据上下文放行 + `forge hazard log data`（embed_guard.go:807 路径）；含执行原语仍拦。strip_quotes 已是跨行引号状态机（:671），本项在其上补 heredoc 体白名单判定。
  2. **F.2a confirm 链式分离**（1.56 首发即 blocked，拒绝文案指向承诺表 §二.1②；存量口径：两机链式 confirm 命中全部是 dogfooding 会话内 agent 自助闭环（甲 53/53 全自助、其中链式多条未逐计；乙 1/13 链式），无用户脚本/CI 依赖面——故视为无存量暴露；无需逃生舱：单独执行 confirm 即放行，拒绝的是形态不是操作）：`forge hazard confirm` 与其他命令以 `&&`/`;`/`|` 同调用 → 拒绝并提示单独执行（甲机实证的「confirm && 危险命令」自助闭环被切断）；豁免前缀 `cd X &&`。
  3. **F.2b confirm 真人化**（1.56 advisory → 1.58 blocked，满足 §二.2 两 minor 预告）：confirm 的 stdin 非人类终端（复用 task_gate.go:166 char-device 判定）时，1.56 打 `ADVISORY: 自 1.58 起 hazard confirm 需在用户终端执行` 仍放行；1.58 拒绝并输出「请在用户终端运行：forge hazard confirm --last」。逃生舱走 per-task override `forge task override --hazard-agent-confirm disable`（留痕 + 评分封顶，§二.3 形态）——不用 env 旁路：task_gate.go:158-165 已为同一 TTY 判定否决过 env/flag 旁路（被注入的 agent 同样设得了 env），本项沿用该裁决。
  4. **F.3 事件去重**：`hazard.AppendEvent` 同指纹同 type 短窗（与 hook.go duplicateBlockRecord 标记共享）只记一条；`CheckHalt` 按去重后事件计数。
- **落点**：internal/hooks/embed_guard.go（HazardGuardHook 串）、internal/hazard/{event.go,stamp.go,halt.go}、internal/cli/hazard.go（confirm 入口）、overrides.go（新 override 键）、compat snapshot（checks/escapes/blockings 面）。
- **度量**：F1 数据上下文误拦——基线 乙：12/28 起事件为脚本文本误命中（审计兄弟页 2.2）→ 夹具口径 0、M 实测 ≤1 起/月；F2 双记 乙：24/52 条 → 0；F3 confirm 链式 乙：1/13 → 0；F4（1.58 后）非终端且无 override 的自助 confirm → 0。防伪：真高危拦截样本（push force/reset hard/递归删除）回归集全数仍拦——precision 修复不得降 recall。
- **验收**：hazard-guard 夹具（解释器 heredoc 无原语放行/有原语拦/confirm 链式拒/非终端 advisory 文案/去重）；高危回归集（两机审计实录命令样本入库 testdata）。
- **风险**：F.2b 对单人 dogfooding 摩擦大（乙 11 起分支清理推送将需人工 confirm）——预告期内 M 统计 confirm 频次，若高频且全为分支清理类，1.58 前评估 `git push --delete` 的项目级 allowlist（hazard 白名单面，另立决策）。Windows Git Bash（mintty）下 char-device 判定会把真人终端误判为非终端（task_gate.go:162-164 已记录的局限）——1.56 advisory 期即按 host 分层统计误拒率，若乙机主力终端命中，1.58 前须先解决判定（如 `MSYSTEM` 环境探测）再 ratchet。

## G｜test-coverage-gate 升级 + 扫描族提精度

- **现状（证据，乙机）**：coverage 门禁 27 次 fail 仅 `18.5%` 后续转 pass，13 个被标任务 100% 照常完成；4 个 testing<70 任务（registry-gc 65 / focus-d2-standards 53 / dead-code-sweep 67 / ci-fix-release 30）各有 2-3 次 fail、0 次 pass，缺测文件被点名未补；甲机唯一 C 级 fix/ci-red-sweep（416 行零测试）同链。现阻断条件 `missingN>=3 && assertN==0`（testcoverage.go:193）过松——fudge 放走 1-2 文件缺测。cheat-scan 误报实证：comment-only-fix 对 `chore/comment-dedup`（任务本意即清注释）报 160 条；unused-scan 拦后 0% 转化（细节无证据不可行动）。
- **设计**：
  1. **G.1 阈值收紧**：1.56 起 verify/complete 的 advisory 文案带「自 1.58 起 missingN≥2 且零断言将 BLOCKED」；1.58 将 `testCoverageHardGateThreshold` 3→2（逃生舱既有：`FORGE_TEST_COVERAGE` + per-task override，承诺表 §二.3 已满足）。
  2. **G.2 cheat-scan 上下文抑制**：comment-only-fix 发现量与任务注释占比一致（改动行 ≥90% 为注释/空白——numstat 管线已有同源实现可复用（scoring scope 排除，evaluator.go:345）；注释/空白占比启发式**需新实现并配夹具**，仓内无现成组件）→ 抑制为 suppressed 记录（保留审计行带 suppressed 标记，不进 fail 计数）。
  3. **G.3 unused-scan 可行动化**：detail 附证据（符号名 + 仓内 grep 引用计数 0 的搜索串），维持 advisory——先提可行动性观察转化率，不动阻断。
- **落点**：internal/taskpipeline/{testcoverage.go,cheatscan.go,unusedscan.go,executor_check_verify_scans.go,executor_check_complete.go}。
- **度量**：G1 coverage 拦后转 pass `18.5%` → ≥60%；G2 testing<70 任务 乙：4/33 → ≤1/25；G3 cheat-scan 注释任务误报（夹具）→ 0；unused-scan 转化率由 M 钉基线后另定目标。
- **验收**：阈值表驱动单测（3/2/1 × 断言 0/正 的矩阵）；comment-dedup 场景夹具抑制；两机审计实录的缺测任务清单作回归样本。
- **风险**：1.58 阻断对重构/纯配置任务误伤——既有断言豁免（assertN>0 放行）+ override 逃生 + 预告期 advisory 命中监控（M 输出按任务类型分层）。

## 风险与回滚

- 1.58 批集中三处 ratchet（C、G、F.2b）；F.2a 是 1.56 首发即 blocked（新形态无存量暴露，§二.1②）。1.56/1.57 预告期内任何指标劣化（M 周报：A1<3、拦截后重跑率下降、任务完成中位耗时恶化 >20%）→ 按承诺表流程推迟 ratchet，advisory 状态可长期驻留。
- E.4 评分口径变更（efficiency 输入）会使历史分数不可直接比——CHANGELOG 行为变更节声明 + M 输出同时给两口径 30 天重叠期。
- 每项独立提交、独立守卫测试，revert 粒度 = 单项（forge skills decide 四元组留痕，evidence 附 M 前后值）。

## 两机协同回测

窗口 30 天或 25 个完成任务（先到为准，与甲方基线口径一致）：两机各跑 `forge eval harness-audit`，对照项——甲向乙靠拢（A1 24.3→个位数）说明 A 有效；乙不恶化（A1 已 9、C2 84% 大幅降）说明 C 未砍过头；D1 按 host 分层判定（zcode 高不应掩盖 kimi/dsh 低）。回测 JSON 与决策四元组入 `evals/`。
