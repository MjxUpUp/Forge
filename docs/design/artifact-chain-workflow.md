# 《产物链工作流化》——L6 契约层接线 + schema.yaml 声明化 + spec→acceptance 提取 + advisory 转化率（落地设计）

状态：设计定稿，随 `feat/artifact-chain-workflow` 落地。
依据：multi-task-concurrency.md §9/§10（L6 产物契约层原案，schema.yaml DAG 与门禁五态在该节已设计）；leverage-points-landing.md 总原则（advisory fail-open / HARD 只守事实 / 逃生舱 env-disable-able 落 checklog 审计行 / 单一真相源 guard 钉住）；本仓现状实证（`WriteArtifact` 单生产调用方 `internal/clitask/task.go:484` 仅 plan stage、`VerifyArtifact` 零生产调用方、proposal/spec/design 三 stage 零写入方、plan-first advisory 已发 24 次但转化率无报告）。

---

## 0. 问题

用户下达「开发某任务」后，模型可以零需求分析、零设计产物直达三道门禁全绿：advisory 提醒了，但产物没有去处——不落盘、不传递给下一阶段、不被任何审查消费，提醒因此显得空。企业 spec 工作流（需求设计→需求评审→方案设计→…每节点产物存在性检查 + `.next` 驱动）的真实强制力来自「节点背后有人读产物、签产物」，不在「检查存在」；agent 场景直接照抄存在性硬门禁 = 模型 30 秒生成模板垃圾过检（§9:273 已裁决的 LoopSpec 式「勾完复选框」通病）。

本设计走第三条路：**产物链声明化 + 门禁分档（hard 只守事实）+ 产物编译成验收 + 漂移探测接线 + advisory 转化率先测量**。产物在链上的价值不是「存在」，而是三个可强制的事实与一个编译关系：存在（文件+哈希引用）、被确认（human 档审批=哈希匹配的事实）、未漂移（VerifyArtifact 失配即失效）、以及验收标准从产物文本编译而来（绕过产物 = 绕不过验收）。

## 1. artifactchain 包——声明式产物链

**现状**：§9 设计了 schema.yaml DAG（「流程改版改 YAML 不改代码」）未实现；阶段顺序只活在注释里。

**设计**：
- 位置 `<DataDir>/schemas/schema.yaml`（`forgedata.DataDirFor(root)` 侧，与 `specs/` 同侧；项目级，随 harness repo 版本化）。
- 数据模型：`Chain{Version int, Stages []Stage}`；`Stage{Name, Mode, Produces, Requires []string, Instruction string}`。
- Mode 四档：`advisory`（默认）| `rubric` | `human` | `hard`。
- 默认链（无 schema.yaml 时）：`proposal → spec → design → plan`，全 advisory，produces 对应 `proposal.md/spec.md/design.md/plan.md`。零配置时行为变化 = implement gate 多一条一次性 advisory（对齐「叙事阶段靠纪律」现状，不突袭既有用户）。
- 两阶段校验（§9 原案收敛）：**结构**（yaml 形状、未知 mode、重复 stage、produces 路径安全——禁 `..`/绝对路径、保留名 `attempts` 排除）→ **语义**（requires 引用存在的 stage、DAG 无环）。
- **fail-open 宪法**：schema 解析/校验失败 → stderr 警告 + 回落默认链，绝不因配置错误阻断任务。只有「显式 hard 配置 + 事实缺失」才阻断。
- `tasks.md` 节点不做：TaskState.Checklist 已承担（§10 裁决：tasks.md 是人读视图，门禁永不退化为勾完复选框）。

## 2. 门禁分档——task-implement 接链

**现状**：implement gate 只有 plan-first advisory（`internal/taskpipeline/executor.go:414-444`），v0.17 把 task-design 从硬门禁降级后，叙事阶段无任何代码层执法点。

**设计**（implement gate 末尾、Passed 返回前追加）：
- **advisory**：汇总缺失节点 → 一条 `artifact-chain` advisory（LevelAdvisory）+ stderr，每任务一次（`ArtifactAdvisoryFired` 持久化，同 `PlanFirstAdvisoryFired` 模式）。
- **rubric**：产物缺失 → BLOCKED；存在但 doclint L1 lint 失败 → BLOCKED（机械判定）。L2 rubric 评审不进 implement gate（意见不走 hard）。
- **human**：产物缺失 → BLOCKED；缺审批、或审批 hash ≠ 当前文件 hash → BLOCKED。
- **hard**：产物缺失或引用哈希漂移 → BLOCKED（事实失效）。
- 逃生舱：`FORGE_ARTIFACT_CHAIN=disable`（全局 env）+ `forge task override --artifact-chain disable`（per-task，`TaskOverrides.ArtifactChain` 新键）→ 链检查整体跳过 + `CheckEscapeHatch` 审计行 + 证据强度 cap（验证类，沿 `EscapeDowngradedStrength` 现行缩放规则）。
- BLOCKED 文案自带唯一下一步命令（§9 nextSteps 单命令纪律）：如 `forge task artifact --set spec --file spec.md`。
- checklog 新名：`artifact-chain`（分档执法与 advisory 共用名，Detail 区分）、`artifact-drift`（complete 侧漂移，见 §5）。
- generic kind 任务不走门禁，天然不接链。

## 3. 产物 CLI——`forge task artifact`（净增 1 命令，命令面预算 ≤2 内）

动作全走互斥 flags：
- `--set <stage> [--file <path> | --stdin]`：读文件内容经 `taskpipeline.WriteArtifact` 落 `SpecsDir` + 锁内折 `ArtifactRef` 进 `TaskState.SpecArtifacts`。链序不在写入处执法（写产物永远合法），在 gate 处执法。
- `--list [--json]`：渲染 stage / mode / path / 漂移✓✗ / 审批态。
- `--verify`：全 ref 实跑 `VerifyArtifact`，漂移逐条报 + `artifact-drift` checklog 行。
- `--approve <stage> [--by <id>]`：human 档审批——当前文件哈希须与 ref 一致（事实校验），记 `ArtifactApprovals[stage]={By, At, Hash}`。
- `--extract`：从产物提取验收标准（§4）经 `MergeAcceptance` 合并进任务，打印提取条数与来源 stage。
- `task start --artifact <stage>=<path>`（可重复）：开工即注册既有产物（spec-kit 式「先写 spec 再开工」路径；flag 不占命令预算）。

## 4. spec→acceptance 提取（产物编译成门禁）

**现状**：`ParseAcceptanceFromPlan`（`internal/taskpipeline/acceptance.go:68`）只扫 `Run:/Expected:` 行且只对 `--plan-file` 接线——产物与验收门禁之间没有编译关系。

**设计**：
- 新 `ParseAcceptanceFromArtifact(path)`：超集解析器，识别三种行形态：
  1. `Run: <cmd>` / `Expected: <out>` 行对（既有语义原样保留）；
  2. 列表行 `accept: <cmd> :: <expected>`（`验收:` 同义；裸 `accept: <cmd>` = 只看退出码 0）；
  3. ```accept 围栏块内每行 `<cmd> :: <expected>`。
- `--extract` 合并进 `TaskState.Acceptance`（`MergeAcceptance` 按 (Run,Expected) 去重），本仓产物不打外来标记。
- verify-acceptance / complete freshness 零改动——提取的验收与其他验收同管道实跑、同 freshness 快照。**产物由此成为硬门禁的源码**：不写产物 → 提不出验收 → complete 被新鲜度拦；写了假产物 → 提出的验收必须真实跑过。

## 5. 漂移探测接线——终结 VerifyArtifact 孤儿状态

**现状**：`VerifyArtifact`（`internal/taskpipeline/specs.go:69`）零生产调用方；「哈希失配触发重新确认」只是注释。

**设计**：
- complete pre-flight 新增产物链段（`task_complete.go`，acceptance pre-flight 之后）：遍历 `SpecArtifacts` 跑 `VerifyArtifact`：
  - 漂移且 mode∈{hard,human} → BLOCKED（事实失效），文案给唯一下一步（`--set` 重登记后重审批）或逃生舱；
  - 漂移且 mode∈{advisory,rubric} → `artifact-drift` warn 行 + stderr（不拦）；
  - 漂移一律作废该 stage 审批（哈希不再匹配的审批不是审批）。
- `forge task status` 渲染产物段：stage / mode / 漂移 / 审批。

## 6. advisory 转化率测量（先测量再翻转）

**现状**：`PlanFirstAdvisoryFired` 已持久化但无消费面——plan-first 已发 24 次，多少任务随后补了方案，无人知晓；「哪些 stage 该升档」全凭直觉。

**设计**：`forge task list --plan-conversion`：只读扫描任务态，输出四计数：已发 advisory / 已发且已转化（发后 Plan|Goal 非空）/ 已发未转化 / 未发但自有方案，附转化率。零新遥测（landing doc L4 飞轮的本地前置切片）。

## 7. 兼容面与 seed 纪律

- compat 六面：commands +1（`task artifact`）；checks +2（`artifact-chain`/`artifact-drift`）；escapes +1（`FORGE_ARTIFACT_CHAIN`）；TaskState 序列化新键（`artifact_approvals` / `artifact_advisory_fired` / `overrides.artifact_chain`）按 added 处理。
- `seed_schema.go`：三个新键全填（种子纪律：值无意义、键必须满）。
- `checklog/types.go` 常量 + `escape.go allCheckNames` 同步（guard 对照测试钉住）。
- `compat.go EscapeEnvs` 追加；`compat.snapshot.json` 再生成。

## 8. 验收

- `go test ./...` 全绿；`go vet` 干净。
- 默认链零阻断回归钉：无 schema.yaml 时 implement gate 仅多一条一次性 advisory，三道门禁行为与现状一致。
- hard 档：缺失 BLOCKED / 登记后 PASS / 逃生舱跳过并落审计行。
- human 档：无审批 BLOCKED / `--approve` 后 PASS / 漂移后审批作废并 BLOCKED。
- 提取：含 `accept:` 行的 spec.md 经 `--extract` 后 `verify-acceptance` 实跑回填。
- complete pre-flight：human/hard 档漂移 BLOCKED，advisory 档漂移 warn 行不拦。
- `--plan-conversion` 输出四计数；compat report 对基线全 added（exit 0）。
- 端到端：临时仓伪造任务走 full 链（start --artifact → gate implement BLOCKED→补产物→PASS→extract→verify-acceptance→complete 漂移拦截→重登记→complete PASS）。

## 9. 刻意不做

- 通用工作流引擎（重试/定时/五态门禁 exhausted/evidence_conflict/reset 闭包——§9 已列，属编排层；本设计只在证据层接线，`.next` 驱动对应物即既有 `forge task gate` 顺序推进 + nextSteps 单命令文案）。
- LLM rubric 进 implement gate（意见不走 hard；L2 评审仍走 doc-review 人工升级链）。
- 自动生成产物（gate 只守事实，不代笔）。
- tasks.md 节点与 `specs.projection=branch` 投影（Checklist 已承担；投影属 §9 可选项，待需求出现）。
- 跨项目 schema 共享、遥测、phone-home。

---

## 自举（2026-09-08 追记）

Forge 仓启用自身产物链（v1.53.0 审计遗留 #4 收口）：schema 实件落
`<DataDir>/schemas/schema.yaml`（用户级数据目录不入 VCS，内容在此存档）——
`proposal`(advisory) / `spec`(**hard**, requires proposal) / `design`(advisory) /
`plan`(advisory)。自本节合并起，本仓代码任务的 task-implement gate 要求 spec 产物
先于 proposal 登记（`forge task artifact --set`），advisory 三节点随任务自愿。
同批收口：产物链 golden 用例 `artifact-chain-tier-block`（标注集 16→17）、
release.yml cosign 身份硬化（精确锚定 release.yml@refs/tags/vX.Y.Z）+ 演练报告
workflow artifacts 留痕、hookdispatch 包级测试隔离（用户级 store 1159 孤儿目录
污染源根治）。

---

## 回边语义（2026-09-08 定稿，G2 审查回环落地）

无限循环在架构上不可「防止」（循环终止判定等价于停机问题），只能**不可表达**——每条回边必须声明预算、耗尽出口与收敛判据，缺一即校验拒绝。三种循环形态与对应机制：

| 循环形态 | 机制 |
|---|---|
| 评审乒乓（修 A 坏 B） | 轮次预算 + 复发检测（指纹隔轮复活 = 修复无效，直接升级） |
| 规格共演振荡 | 同上（复用同一预算语义） |
| 验收稀释（挪门柱） | 反稀释护栏：记因 + 强度不降 + 非产出方单方（v2，schema `guard: anti-dilution` 位已预留） |

**三条机械判定**（复用既有轮次结构 ReviewRounds / Finding.Round，零新计数器）：

1. **轮龄**：open finding 存活轮龄 = len(ReviewRounds) - Finding.Round + 1；轮龄 ≥ 回边 max_rounds → exhausted（rounds-exhausted）。
2. **复发**：finding 指纹 = 规范化内容 sha256[0:16]；标 fixed/wontfix 时指纹入 ResolvedPrints；同指纹再登记 = 复活 → 立即耗尽（finding-recurrence）且 ResolvedPrints 保留。
3. **exhausted → complete BLOCKED（升级人工）**：耗尽后唯一出口是 `forge task finding --reset-loop --note "<人工裁决>"`（审计行）或 abort；agent 不得自宣收敛、不得重置预算（终止权外置）。

**schema 声明**：

```yaml
edges:
  - from: review
    to: implement
    max_rounds: 3        # 缺省 3（doc gate 先例）
    exhaustion: escalate # escalate | stop（缺省 escalate）
    progress: fingerprint
```

校验规则：from/to 必填、max_rounds ≥ 0（0 → 归一缺省 3）、exhaustion ∈ escalate|stop（缺省 escalate）、progress 仅 fingerprint、回边不重复。真伪缺陷分流：实现缺陷走有界修复轮；规格缺陷退出修复循环走 spec 修正 → 失效传播（G6，v2 预留 invalidate 字段）。豁免：FORGE_ARTIFACT_CHAIN / override --artifact-chain（chain 与 loop 同一声明子系统共用逃生舱）。

已实现落点：`internal/artifactchain`（edges 解析）→ `internal/taskpipeline/loopedge.go`（轮龄/复发/exhausted）→ `forge review pass`（轮次评估）→ `forge task finding`（指纹/复活/--reset-loop）→ `forge task complete`（耗尽 pre-flight）→ `forge task status`（回环状态渲染）。
