# Forge 兼容承诺表（compat commitments）

状态：生效中（自首个含本文档的发版起；证据链：机制史调研 ~/.forge/research/forge-mechanisms-20260905-1752/report.md（失效：该 run 目录 2026-10-15 已不存在，不可复核） §六/§八，K8s deprecation policy / PEP 387 / GODEBUG 形态先例）。

本文是 forge 对外的兼容契约——三向 × 三档承诺矩阵 + 门禁文案契约。机械执法点：`forge compat snapshot/report`（七面盘点——六面 + 外部桥契约面 bridges，见 compat-bridge-face.md；任何面变更显式过 golden）；`TestNpmPlatformVersionsAligned`（分发对齐）；导入侧版本偏移警示（`sync-version-skew`）。

## 一、三向 × 三档承诺矩阵

| 方向 \ 档位 | 强承诺（破坏即 bug） | 弱承诺（尽力 + 行为变更公告） | 不承诺（显式排除） |
|---|---|---|---|
| **对下：数据面**（checklog.jsonl / toollog / TaskState / heldout 侧车） | 已落盘数据永远可读：append-only + legacy 字段兜底 + 惰性重推导；**序列化键只增不删不改名**（compat golden 守卫） | 语义推导规则可随版本演进（同一行数据在不同版本读出的 Strength 可变——审计视角的已知属性，见调研 dive_06 T3） | 历史行的显示排版/未文档化内部字段 |
| **对下：命令面**（CLI 命令与 flag） | 命令与 flag **只增不删不改名**（compat golden 守卫；删除/改名须走下节的预告流程） | flag 语义收紧（advisory→blocked）按文案契约带预告版本 | 输出文本排版（porcelain 可变，Git 惯例）；退出码契约：0=过 / 2=BLOCKED / 1=故障 |
| **对侧：多机同步**（forge-sync bundle） | 导入幂等 + 原样保留（AppendEntries 不重签）；bundle manifest 携带 forge_version | 前向偏移（新 bundle 入旧节点）警告不硬拒（K8s 偏移窗口语义；旧节点 re-export 裁剪较新字段的风险已显性化） | 跨版本字段透传（类型化反序列化会丢弃未知键——建议升级本机 forge 后再转发放） |
| **对上：宿主面**（12 宿主接线） | 未知 hook 事件/未知 stdin 字段不崩不脏（宽容读）；接线幂等（重装不毁用户已有 hooks）；单宿主方言错误不污染他宿主归因 | 新宿主能力机会式跟进（按发版节奏） | 跟随宿主 experimental 特性的稳定性；宿主自身回归（如 v2.0.31 hooks 全失效） |
| **分发面**（npm） | wrapper 与平台子包版本逐版本互锁（CI 守卫 TestNpmPlatformVersionsAligned） | release-please 驱动 semver | — |

## 二、门禁文案契约（advisory → blocked 的预告纪律）

依据 PEP 387（"警告文本必须写明预计生效版本+反馈渠道"是 Python 的硬要求）与 K8s 承诺矩阵形态：

1. **新增 BLOCKED 门禁的拒绝文案必须包含其一**：①预告版本（"自 vX.Y 起 advisory 转 blocked"——若该门禁曾以 advisory 形态发布过）；②"首发即 blocked"声明 + 指向本文档（若门禁与含它的版本同首发，无存量暴露则无预告义务）。
2. **时间承诺**：已发布的 advisory 门禁转 blocked，**预告期 ≥2 个 minor 版本**（PEP 387 口径；期间文案持续显示预告版本）。
3. **逃生舱纪律**：第 5 条 ①② 类 BLOCKED 门禁必须有 `FORGE_*` env 或 per-task override 逃生舱（③–⑥ 类的出口约束见第 5 条表；形态门禁豁免见本条末），逃生留 checklog 痕 + evidence 封顶 Weak；豁免行携带 reason/owner 元数据（v1 简化：reason 为机制枚举值——`per-task override` / `env`——尚无自由文本 `--reason` flag，owner 在 env 逃生时记 "env" 而非委托任务；升级为设计文档 P1-2 的后续项）。**形态门禁豁免（2026-09-10 裁决）**：拒绝「命令组合形态」而非「操作本身」的门禁（合规形态即放行、拒绝文案指明放行形态）**不强制**配独立逃生舱——放行被禁形态本身的逃生舱等于取消门禁；是否配置按存量暴露风险自选（F.2a hazard confirm 链式分离：新形态无存量、不配；C gate-cmd-form：存量脚本可能嵌门禁连刷、保留 env 逃生 `FORGE_GATE_CMD_FORM=0` 作过渡，无 per-task override）。两例均见 docs/design/harness-fixes-a-g-2026-09.md。
4. **机械执法**：`forge compat report` 的 blocking-sites 面使新增 BLOCKED 位点在 golden diff 里显式可见——新增须同时更新本文档或 CHANGELOG 行为变更节，否则 report 以 warn 提示。
5. **阻断正当性分类（单一真相源，2026-10-22 起）**：一处阻断只有落进下表某一类才正当，并须满足该类的必备约束；落不进任何一类的只能 advisory。新增 BLOCKED 位点的提案须写明所属类别。第 1、2 条（拒绝文案与预告期）对 ①–⑤ 类中新增或由 advisory 转硬的位点都生效（⑥ 不适用，裁决见 §三 2026-10-22 逃生舱审计与裁决批），下表「必备约束」只列各类特有项。判别顺序（自上而下，命中即停）：底层规则是 advisory、只因宿主上 advisory 不构成行为约束而升为阻断 → ⑥；拦的是命令组合形态（合规形态即放行）→ ③；放行权须在人（高危或越权操作本身，或其未经人工确认的悬账）→ ⑤；拦的是动作的时机或顺序而非交付内容（门禁未按序推进、门禁间无工作；不含快照过期与上游未交付，它们是交付内容缺事实）→ ④；证据或评测本身不成立（伪造或评测设计不合法，不含时效过期；不论在哪个时点拦）→ ②；交付声明（门禁、verify、complete 时点）缺可核验的事实或记录 → ①；其余会话或流程动作（Stop、Edit、commit 时点）→ ④。（§三 较早条目里的 ①②③ 是裁决序号，与本表类别号无关。）其他文档里的「HARD 只守事实」「BLOCKED 仅用于证据造假 / 评测自身不合法」是对 ①② 类的局部口径，冲突时以本表为准。

   | 类别 | 何时正当 | 必备约束 | 现行代表位点 |
   |---|---|---|---|
   | ① 事实缺失 | 交付声明缺可机器核验的事实或记录：验收未实跑或快照过期、缺配对测试且零断言、改 SKILL.md 未记决策、需求未被验收覆盖、文档回检或代码审查未记录、checklist 未勾完、上游依赖未交付 | 逃生舱（env 或 per-task override），逃生留 checklog 并封顶 evidence Weak | acceptance-gate、test-coverage 兜底、skill-decisions、req-coverage、doc gate、task-complete 前置（审查、checklist）、上游依赖未交付（verify 与 complete 两处） |
   | ② 证据不合法 | 证据或评测自身不成立：held-out 逃逸、自报验证命令与 toollog 不符、评测 preflight 不合法 | 同 ① | held-out cheat-suspect、self-report、eval preflight |
   | ③ 形态 | 拒绝命令组合形态而非操作本身，合规形态即放行 | 按第 3 条形态门禁豁免，可不配独立逃生舱；拒绝文案须写明放行形态 | gate-cmd-form（1.67 起，会话上限 5）、hazard confirm 链式分离 |
   | ④ 流程节奏 | 防门禁空转或脱离任务流程：门禁间无工作、未读即改、任务分支外提交、Stop 时有未提交代码变更、非 task 模式未审查即停、门禁未按序推进 | 须有会话阻断上限（超限回落 advisory）或明确出口命令；有逃生 env 的须留审计行 | work-activity、read-before-edit、task-drift（上限 5）、task-verify Stop（上限 3）、review-stop（审查轮次上限 3）、前置门禁顺序 |
   | ⑤ 安全 HITL | 高危或越权操作及其未经人工确认的悬账，放行权在人 | 出口须由用户发起（hazard confirm 机械校验真人终端）；env 逃生若有，只用于误报止血或无人 CI，并留审计行 | hazard-guard、hazard-pending、freeze-guard、项目级 `.forge/*` 与 `.claude/settings*` 自保护 |
   | ⑥ 宿主提升 | 宿主上 advisory 不构成行为约束，把它提升为阻断：(a) advisory 通道物理失效，或 (b) 通道送达但 advisory 被实证无视（`internal/hostcap/hostcap.go` PromoteAdvisory 的两条准入路径） | 只作用于 hostcap 登记的宿主；文案须标明是宿主提升，底层规则仍按 advisory 计；逃生 `FORGE_ADVISORY_PROMOTION=soft`（仅 kimi 的 `FORGE_KIMI_ADVISORY=soft` 随 kimi 提升退役而不再起作用） | task-guard 在 dsh/zcode 上（路径 b） |

   已知例外（与上表约束不符，待补或待裁决，修复前不得被新提案援引为先例）：
   - freeze-guard 与项目级自保护（⑤）：`forge freeze --off` 等出口没有真人终端校验，只靠协议约束。

   2026-10-22 已处理（见 §三「逃生舱审计与裁决批」）：task-verify Stop 逃生审计、复发升硬与宿主提升入 roster、宿主提升文案、审查前置 / checklist / 上游依赖 / bundle 验签的无逃生裁决。复发升硬的 scope 分支本质是「项目级 advisory 被实证无视后升硬」，与 ⑥(b) 同构，仍归 ①，因为它按项目履历而非宿主触发。

## 三、生效口径与存量处理

- 本契约自首个含本文档的版本生效。本批新增门禁（held-out / self-report / gate push 等）与契约同版本首发——按第 2 节②无预告义务；1.50.0 及更早的已发布版本不含它们，无存量用户暴露。
- 既有逃生舱（FORGE_TEST_COVERAGE / FORGE_WORK_ACTIVITY / FORGE_ACCEPTANCE_GATE / FORGE_DOC_GATE / FORGE_SKILL_DECISIONS / FORGE_SELF_REPORT / FORGE_HELDOUT / FORGE_GATE_PUSH / FORGE_TASK_DRIFT / FORGE_TASK_VERIFY_STOP）：**不设 TTL**（env 传入式 = 不传即失效），但 dashboard 逃生舱库存提供永久化与 unfulfilled 候选信号（复查而非过期——调研反框架律：衰减靠复查不靠硬过期）。基础设施旋钮（非门禁逃生,不列舱位）：FORGE_HOOK_DEDUP_WINDOW（dispatch 幂等窗口,0=禁用）。

### 存量处理裁决记录

- **2026-10-22 逃生舱审计与裁决批（方案 docs/plans/escape-rulings-doc-hygiene-2026-10.md）**：(1) task-verify Stop 有界阻断：`FORGE_TASK_VERIFY_STOP=0` 在本来会阻断时每会话写一行 escape-hatch 审计（`internal/hooks/embed_quality.go`）。该阻断 2026-09-18（escape-hatch-hardening P1）由 advisory 直接转为有界阻断，未按第二节第 1、2 条预告——追认：每会话上限 3 次、超限回落 advisory、有逃生 env，风险有界；拒绝文案改为指向第二节第 5 条 ④。此追认不得被后续转硬援引。(2) 复发升硬：`FORGE_RECURRENT_HARDEN=disable` 撤下的是本来会触发的 BLOCKED，归 ① 类逃生——入 `compat.EscapeEnvs`，本来会升硬时写 escape-hatch 审计行，与其它逃生同价（分数封顶、证据 Weak）；原设计「不加 Strength 惩罚」作废，testing 与 scope 两个分支同样处理。未复发的项目关掉开关不产生审计行。(3) 宿主提升：`FORGE_ADVISORY_PROMOTION` 入 roster；提升后的 task-guard 拒绝文案标明 Host promotion 并给出逃生。⑥ 类不适用第二节第 1、2 条的预告期：底层规则仍是 advisory，提升只为已实证 advisory 被无视的宿主恢复约束力，且逃生 env 常备。(4) 无逃生裁决（① 类，不补 env）：代码审查前置的出口 `forge review pass --acknowledge-changes` 由执行者自我承担并记 self-refresh 审计，等价于留痕逃生；checklist 未勾完的出口 `forge task checklist tick` / `drop` 是执行者的显式操作；上游依赖未交付的出口 `forge task abort --detach-deps` 显式解除依赖；bundle 验签（② 类）维持无逃生——签名不符时的逃生等于接受伪造。(5) 快照逃生面 +2（`FORGE_RECURRENT_HARDEN`、`FORGE_ADVISORY_PROMOTION`，added 非破坏）；零新增阻断位点、零新增命令。
- **2026-10-22 协议文本与代码对齐批**：(1) gate-cmd-form 的「1.58 承诺未兑现」已知债销账——代码自 1.67.0 起以机械版本门 BLOCK（`internal/hookdispatch/hook_gate_cmd_form.go` 的 `gateCmdFormBlockVersion`），逃生只有 `FORGE_GATE_CMD_FORM=0`；生成的项目级协议文本、checklog 注释同批改正为 1.67、删除从未实现的 per-task override 说法。(2) test-coverage G.1「自 1.58 起阈值 3→2」预告**撤回**：预告挂了 25 个 minor 未兑现，长期悬置与第二节第 2 条「期间文案持续显示预告版本」的意图不符；收紧与否另行按第二节重新预告，本批不改行为，`testCoverageHardGateThreshold` 仍为 3，advisory 文案改为陈述现行规则并从该常量取值。(3) F.2b hazard confirm 真人化不设独立逃生舱：放行权本身在人，真人在终端执行 confirm 即出口（第二节第 5 条 ⑤）；harness-fixes-a-g 设计中的 `--hazard-agent-confirm` override 不实现，教 agent 自行 confirm 的协议、hook 与 skill 文案同批改为「请用户在自己的终端运行」。本批零新增阻断位点、零新增命令与逃生 env。
- **2026-10-21 评测有效性前置批（周期总纲 docs/plans/acceptance-gate-and-wedge-2026-10-proposal.md R3–R5，方案 docs/plans/eval-validity-r3-r5-2026-10.md，实现 `internal/evalkit/preflight.go` / `stats.go` / `golden.go` / `judgeaudit.go`）**：① 命令面净增 0。`forge eval run` / `eval decompose` 新增 `--variants`、`eval golden run` 新增 `--release`，compat 引擎按 flags 签名整体比对判 changed/破坏性，援引 2026-09-20 裁决：纯新增、不改既有 flag 语义。② 新增 BLOCKED 位点一处：Track A preflight（带 `test_cmd` 的任务 oracle 臂未过或未声明 `oracle_cmd`、do-nothing 臂通过）拒绝启动、exit 2——属评测设计 §十许可的「评测自身不合法」，按第二节第 1 条②首发即 blocked，拒绝文案已指向本文档；逃生 `FORGE_EVAL_PREFLIGHT=skip`（已入 compat.EscapeEnvs roster，留 escape-hatch 行，scorecard Note 写明证据封顶 Weak）。存量暴露：只作用于带 `test_cmd` 且后端为 docker 的任务；仓库内唯一此类示例 `evals/forge/manifests/tb-frozen-example.yaml` 的判分本身会被 do-nothing 击穿，已同批改为检查 run 产物并补 `oracle_cmd`；只有 `command` 的 manifest 与 scripted/exec 后端标 `preflight=n/a`、行为不变。仓库外用户自写的、带 `test_cmd` 的 docker manifest 升级后可能被拦：当前外部用户为 0（周期总纲 §一第 5 条），且拒绝文案给出逃生 env，不设预告期。③ checks 面新增 `eval-preflight`、`eval-golden-test-query`（added，非破坏），观察类，不入 evidence 白名单。④ BenchTask 新增 `oracle_cmd`/`leakage_cmd`，RunSpec 新增 `variants`，GoldenCase 新增 `layer`，JudgeAuditEntry 新增 `pair_original`/`pair_swapped`；Scorecard / GoldenReport / JudgeAuditReport / DecomposeReport 只追加键——存量 manifest、用例、分数文件逐字节可读；无 `layer` 的用例集指纹不变（守卫 `TestGoldenTestLayerQueryCount` 对照已钉的 MANIFEST.sha256）。**援引边界**同 2026-09-20。
- **2026-10-20 loop 契约 B1 批（提案 docs/plans/loop-contract-2026-10-proposal.md B1-1～B1-5，实现 `internal/taskpipeline/loopverdict.go` / `acceptance_recurrence.go` / `failnorm.go` / `loopconfig.go`）**：① 命令面净增 0。`forge task decide` 新增 `--rejected`（死路记忆），compat 引擎按 flags 签名整体比对判 changed/破坏性，援引 2026-09-20 裁决：纯新增、不改既有 flag 语义——既有 `--content/--by/--affects/--rationale` 的解析与幂等判定不变（幂等键加入 Kind，对不带新 flag 的调用 Kind 恒为空，行为逐字节一致）。② checks 面新增 `acceptance-recurrence`（added，非破坏）；Source 为 deterministic，但不入 evidence 白名单，不参与证据强度。③ 零新增阻断位点、零新增逃生 env——同因复发、预算、判词全部 advisory：verify-acceptance 每条复发各打一行 `ADVISORY:`，末尾加一行 `loop:`，退出码契约不变；`task status --json` 只追加顶层 `loop_verdict`，原有键集合与顺序不变（嵌入结构体，守卫 `TestTaskStatusJSONLoopVerdict`）。日后把预算升为 BLOCKED 须按第二节第 1 条①、第 2 条（预告期 ≥2 minor）并按第 3 条配逃生舱。④ TaskState 新增 `acceptance[].fail_history`（及嵌套 at/exit_code/fingerprint/change_hash/changed）、`decisions[].kind` 与 `history[].silent`（hook 路径记录的门禁结果标记），protocol.yml 新增 `loop:` 段——只增键，存量 state / protocol 逐字节兼容；checklog `acceptance` 聚合行 Meta 增 `passed`/`total` 两键（append-only）；`task resume/context` 与 hook 注入文本增「循环判词:」头行与【已证伪方案】节（输出文本面，不承诺）。**援引边界**同 2026-09-20。
- **2026-10-13 理由锚点批（decision-warrant，实现 `internal/cliskills/skills_decide_warrant.go`）**：① 命令面净增 0。`forge skills decide` 新增 `--warrant`，compat 引擎按 flags 签名整体比对判 changed/破坏性，援引 2026-09-20 裁决：纯新增、不改既有 flag 语义——新校验（单行、≤512 字节）只作用于新 flag，既有 flag 的解析与校验不变。② checks 面新增 `decision-warrant`（added，非破坏）；Source 为 deterministic，但不入 evidence 白名单，不参与证据强度。③ 零新增阻断位点、零新增逃生 env——缺锚点、核验失败、run 回归均只 advisory 告警（stderr WARN；在 forge 项目内另落 checklog 行），决策照常写入、退出码不变；这是对既有 accept 调用唯一可见的变化，落在第一节命令面「不承诺」的输出文本与数据面强承诺的 checklog append-only 之内。日后升格为 BLOCKED 须按第二节第 1 条①（拒绝文案带预告版本）、第 2 条（预告期 ≥2 minor）并按第 3 条配逃生舱。④ SkillDecision 新增 `warrant` 键并在 decisions.md 渲染为 `Warrant` 字段行——只增键，无该字段的存量决策逐字节兼容（decisions.md 不属 compat golden 面，守卫为 `TestFormatDecision_NoWarrantLineWhenEmpty`）。**援引边界**同 2026-09-20。
- **2026-10-12 设计阶段硬化批（design-stage-hardening，提案 docs/plans/design-stage-hardening-2026-10-proposal.md）**：① 命令面净增 0——方案事实核查走既有 `forge task artifact` 的新动作 `--check`（及审批附注 `--checked`），分档走 `task start --tier/--tier-reason`；compat 引擎按 flags 签名整体比对判 changed/破坏性，援引 2026-09-20 裁决：纯新增、不改既有 flag 语义。② 新增 BLOCKED 位点一处（reqtrace.go 需求覆盖）：只在用户显式把产物链 spec/design 升到 rubric 及以上档位时阻断，默认链 advisory——属第二节②「首发即 blocked」，拒绝文案已指向本文档，逃生 `FORGE_REQ_COVERAGE=disable`（已入 compat.EscapeEnvs roster）。③ TaskState 新增 `plan_scope_source` / `design_tier`，AcceptanceCriterion 新增 `traces`，ArtifactApproval 新增 `checked`——只增键，存量 state 逐字节兼容。**援引边界**同 2026-09-20。
- **2026-09-20 纯新增 flag 的 changed 判定（assertion-dispatch 批）**：task start 新增 `--assert`/`--accept-file`、task accept 新增 `--assert`——compat 引擎按 flags 签名整体比对,纯新增也判 changed/破坏性（签名 diff 粒度无法区分「加 flag」与「改名 flag」）。裁决为可接受：① 强承诺「只增不删不改名」未破坏——本变更是**纯新增**,无删除/改名/语义收紧；② 设计上断言必须与 `--accept` 就近绑定（preceding 绑定规则）,独立命令会撕裂考卷声明的时序语义——oracle-pipeline「一律新命令避 changed」先例不适用于附着型参数；③ CHANGELOG 行为变更节已预声明,快照重钉入库。**援引边界**：此裁决只覆盖「纯新增、不改既有 flag 语义」的变更——借机收紧/改名/删除既有 flag 仍须按 §二预告流程。
- **2026-09-18 task-drift BLOCK ratchet（机械版本门先例）**：task-drift（escape-hatch-hardening P1）1.64 以 advisory 首发,预告文案载明「自 1.66 起 advisory 转 BLOCK」,**且 ratchet 以代码内版本门机械生效**（`util.CompareVersions(version, "1.66.0")`——非 gate-cmd-form 式「文案承诺、实现另行跟进」的软预告;后者 1.58 承诺曾记为已知债,已于 1.67.0 以同款机械版本门兑现,见 2026-10-22 条）。逃生舱 `FORGE_TASK_DRIFT=0`（留 checklog escape-hatch 行,evidence 封顶 Weak）;另有**会话阻断上限 5 次**（超限自动降级 advisory——有界阻断,防 deny 死循环;CC Stop hook 8 次强制放行同哲学）。
- **2026-09-06 死代码清扫批**：移除 4 个命令（clone check / suggest 族 / skills analyze、mine）未走 ≥2 minor 预告。可辩护依据：① 零使用证据（本机 checklog 全史无痕迹 + npm 从未发布含冻结标注的版本——冻结声明从未出海，无存量用户接触）；② 决策文档 feature-focus §2.3 冻结决议留痕在先；③ CHANGELOG 行为变更节 + 迁移指引齐备。**援引边界**：此裁决不可作为常规命令删除的先例——若无同等零使用证据，命令删除必须走预告流程。

## 四、刻意不做（记录裁决，防重新发明）

- **事前审批门禁**：DORA 实证外部审批与交付绩效负相关——用事后审计+自动化复查承接。
- **基线文件式棘轮**（冻结存量违规）：forge 规模走 Google"先清零"路线（新门禁默认只对新任务生效）。
- **GODEBUG 式按版本给默认行为的完整机制**：等 BLOCKED 门禁数量增长后再评估（记录时 8 个逃生舱、管理成本不成比例；2026-10-22 roster 已 19 个，已越过「增长后」——下一次新增 BLOCKED 位点的提案须附复验结论）。

## 五、命令面冻结（L5 杠杆，2026-09-07 起；依据 docs/design/leverage-points-landing.md）

- 单个 minor 内 commands **净增 ≤2**，且每个新增命令的 PR 须附设计文档链接。`forge compat report` 的 `net-added commands` 行是该预算的软仪表——只展示不强断：硬阻断命令新增会用"参数级执法"对冲冻结本身要遏制的形态（Meadows 杠杆点 12 的堆积病），预算执法走本节的评审流程。
- 超预算须在本节追加裁决记录（援引边界与 §三存量处理裁决同格式），否则按违例评审。
- 首发预算记账：leverage-p0 批次净增 +1（`forge eval wedge-drill`，L1 楔子演练）。
- 1.56 批次记账：净增 +1（`forge eval harness-audit`，A–G 修复的可复算度量；设计 docs/design/harness-fixes-a-g-2026-09.md M，该设计声明本 minor 仅此一个新命令，预算用 1/2）。
- calibrated-judge 批次记账（2026-09-20）：净增 +2（`forge eval golden harvest`、`forge review llm`，L3 校准判官/leverage-points-landing.md L3）——本 minor 预算用尽 2/2。
- oracle-pipeline 批次裁决（2026-09，超预算）：本批计划净增 +9（阶段一 `forge task accept`/`task report`/`task chain-init`，阶段二 `task mutation`/`task fuzz`/`task edgecheck`/`task regression`，阶段三 heldout 池命令入口/`forge eval redteam`），超"单 minor 净增 ≤2"。理由：① 全部为 added 非破坏（不触碰既有命令面——加 flag 判 changed/破坏性，故一律新命令）；② 九命令同属一个能力（正确性传递链，设计 docs/design/oracle-pipeline-2026-09.md），拆 minor 拆的是一个可评审整体；③ 用户对整批方案已拍板。**援引边界**：不可作为无关命令堆积的先例——与该设计无整体关系的命令新增仍按 ≤2/minor 预算。
- 2026-10-22 补记（按 tag 快照逐版本对比 `commands` 面得出，此前未当期记账）：
  - 1.57 批次：净增 +3（`forge eval dead-checks`、`forge eval friction`、`forge eval rule-ledger`；快照 id eval-dead-checks、eval-friction、eval-rule-ledger），超预算 1。三者同属 W0 瘦身的度量工具（设计 docs/design/slim-w0-spec.md），追认。
  - 1.63 批次：净增 +3（`forge selfcheck`、`forge selfcheck pairing`、`forge selfcheck scope`），超预算 1。三者是同一个镜像自检能力的父命令与两个子命令（设计 docs/plans/discipline-first-gates-2026-09.md），追认。
  - 1.79 批次：净增 +2（`forge eval oracle`、`forge eval oracle run`，设计 docs/design/sandbox-oracle.md），在预算内，但落在周期总纲「命令面净增 0」的周期里；周期总纲已同批登记为例外。
  - **援引边界**：补记只说明历史，不构成「事后追认即可超预算」的先例；超预算仍须当期在本节记裁决。
