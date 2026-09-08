# 《六杠杆点落地》——外部回路、规格契约、系统 4 下料、数据飞轮、命令面冻结、编排组合（分期落地设计）

状态：P0 已落地（feat/leverage-p0：L5 冻结仪表 / L1 wedge-drill / L2 数据形状；2026-09-07 独立评审 PASS 88/100）。P1-P2 未排期。
依据：六杠杆点战略诊断（2026-09-07 会话分析：DL 调研 / 《系统之美》/ 控制论 / 管理学 / 软件工程史 → 项目体检）；本文 file:line 均为现状代码实证。
总原则：只投当前瓶颈（验证与规格），不在非瓶颈（生成 / 自举基建）加码；一切新机制守既有宪法——advisory fail-open、HARD 只守事实、逃生舱 env-disable-able 落 checklog 审计行、单一真相源 guard 钉住。

---

## L5 命令面冻结 + 自举预算——P0（policy 层，半天）

杠杆点：Meadows #12 反向操作（停止参数堆积）+ TOC（不在非瓶颈加码）。先例：cargo-semver-checks / API Extractor。

- **现状**：compat.snapshot.json 六面已钉 174 commands / 39 checks / 8 escapes / 41 payload / 3 schemas / 19 blockings（`internal/compat/compat.go:49-57`），report 对基线 breaking exit 2；但 **added 只有提示、无预算**，自举基建（release/npm-guard/pluginpack/compat 机械）无投入上限。
- **设计**：
  - compat-commitments.md 增「命令面冻结」节：单个 minor 内 commands 净增 ≤2 且 PR 须附设计文档链接；removed/changed 维持两 minor 预告（PEP 387 口径，已有）。
  - `forge compat report` 输出增加一行 `net-added commands: N`（纯展示，不强断）；CI 既有 report 步骤顺带可见。
  - 自举预算（流程规则，落 docs/plans/feature-focus-*.md 批次表）：每个 infra 批次须同 milestone 携带一个 L1-L4 产品批次——预算对齐而非禁止。
- **验收**：冻结节入 commitments；`forge compat report --base v1.51.0` 含净增计数行；ci.yml 现有 report 步骤零改动通过。
- **刻意不做**：硬阻断命令新增（用参数级执法对冲杠杆点 12，重蹈覆辙）；基线文件式棘轮（compat-commitments §四已裁决不做）。

## L1 外部楔子（修复外部平衡回路）——P0

杠杆点：系统动力学的头号缺陷修复——接通环境负反馈。先例：Stripe quickstart（TTFE 哲学）。

- **现状**：零外部用户（feature-focus-2026-09 自认）；174 命令无引导路径，新用户无法在 10 分钟内拿到第一个证据。
- **设计（不做新命令族，做切片 + 演练）**：
  - **wedge-drill**：`forge eval wedge-drill`——脚本化断言（骨架参照 resume-drill，`internal/cli/eval.go`）：临时 git 目录跑 init → `task start --accept "false 命令 :: FAIL"` → 实跑失败 → 修复 → `verify-acceptance` PASS → `trace` 可见证据链；全链断言 + 计时输出。发布冒烟与用户首跑自检双用。
  - **README 切片**：主 README 与 READMEs/README.zh-CN.md 顶部加 5 行楔子路径（init → task start --accept → verify-acceptance → score/trace），其余折叠现状不动。
  - **反馈通道**：复用 `forge project export --redact`（allowlist 默认拒绝敏感 store）作为早期用户证据回传载体；**不建遥测**（红线：无 phone-home）。
  - **种子项目 3-10 个**：运营动作非代码；度量 = TTFE（首证据时间）与 off_churn（eval dashboard 已有 C4/C7）。
- **落点**：internal/cli/eval.go（drill）；README 两处顶部切片。
- **验收**：wedge-drill 三平台 CI 绿；无 git 的目录给出指引性报错（非 stack）；drill 输出含每步耗时（TTFE 可读）。
- **刻意不做**：telemetry phone-home、Web 引导、新 onboarding 命令族。

## L2 规格契约化 spec-as-gate v2（杠杆点 3·目标层）——P0 数据形状 / P1 实现

杠杆点：Meadows #3 目标层——验收标准从散文变契约。先例：Pact 契约测试；PEP 387（兼容口径）。

- **现状**：`--accept "run :: expected"` 仅 `strings.Contains` 大小写敏感（`internal/taskpipeline/acceptance.go:188-198`）；逐条结果只回 stdout，checklog 仅 1 条聚合 `acceptance` 行（`internal/clitask/task_gate.go:235-243`）；**heldout.go:106-116 复制了同一判定逻辑——双套件漂移风险已存在**。
- **设计（v1 永不移除；v2 为 optional 附加，compat added 非破坏）**：
  - `AcceptanceCriterion` 加 `Assertions []Assertion`（`internal/tasktypes/types.go:32-66`，omitempty；`Assertion{Type, Arg, Expected string, Negate bool}`）。五种**栈无关机械可判**类型首发：`exit`（退出码断言）/ `contains`（v1 语义显式化）/ `not-contains`（产物不得含 X——反作弊复合断言）/ `file-changed`（glob 必须出现在本任务 diff）/ `file-untouched`（保护 glob 不得出现在 diff——freeze 语义的任务级持久版，与 freeze-guard 会话级互补）。
  - 判定分派抽公共 `judgeAssertion(a, ctx) Verdict`：verify-acceptance 与 `VerifyHeldout` 共用（消 heldout.go 复制）；`EnsureGoTestVerbose` 仅 contains 型注入。
  - 证据升级：每断言一行 `acceptance-assert`（deterministic；进 `internal/checklog/escape.go` roster 与 `internal/checklog/evidence.go:187` deterministic map）；聚合 `acceptance` 行保留（兼容既有强度评级与 golden）。
  - CLI：`--assert "type:arg :: expected"` 可重复；`--accept-file <yml>` 批量声明（声明期 `ValidateAssertions` 拒绝叙述性——复用 invariant 的 CJK 启发式，`internal/clitask/task_artifacts.go:195`）。
  - held-out 默认开（advisory 形态）：task-verify 对「已登记验收但无保留集」的任务提醒一次（复用 `FORGE_HELDOUT=disable` 静默，不新增 env）；heldout 已有 complete 边界复跑（`CheckHeldoutFresh`），默认开只补登记提醒，不改任何阻断强度。
  - **seed/guard 纪律**：`internal/taskpipeline/seed_schema.go:26-29` 同步加键（漏更新 = 棘轮盲区，见其 :5-7 注释）；`MergeAcceptanceResults` 匹配键 (Run,Expected) 扩为含 Assertions 哈希（`acceptance.go:251`——否则断言不同结果误并）；guard test 钉 roster 与 deterministic map 同步。
- **验收**：v1 任务 JSON（无 assertions）行为与现状逐字节一致（回归钉）；not-contains 命中 → exit 非 0 + BLOCKED 行；file-untouched 命中 → 同上；compat report 对基线 diff 全为 added（exit 0）；五类断言 × 通过/失败 = 10 个 golden 候选并入 L3 批次。
- **逃生舱**：继承 `FORGE_ACCEPTANCE_GATE=disable`（escape.go 已注册），**不新增 env**。
- **刻意不做**：regex 断言（ReDoS + 不可判性）；coverage 断言首期不做（栈相关，待 conventions 档案接命令后评估）；LLM 断言（意见不走 hard）；基线棘轮（已裁决）。

## L3 系统 4 下料（golden/traps 扩容 + 校准判官）——P1

杠杆点：Beer VSM——系统 4 情报必须与系统 3 控制平衡；reward hacking gap 的测量基建。先例：SWE-bench 追溯标注。

- **现状**：16 golden 覆 5 门禁（task-guard×5 / file-sentinel×5 / auto-compile×2 / hazard-guard×2 / read-before-edit×2），**验证门禁族 0 golden**；traps 仅 3（`internal/evalkit/traps.go:43-66`）；judge-audit 机器在（κ<0.6 → ADVISORY 降级 + eval-judge-weak 审计行，`internal/evalkit/judgeaudit.go:254-261`）、无喂料。
- **设计**：
  - **golden harvest**：`forge eval golden harvest --since <ref>`——只读扫描已完结任务（tasks/*.json 终态 + checklog acceptance/cheat-scan 行 + review 记录），机械投影出候选 GoldenCase 骨架（files/probe 字段取任务实况），落 `evals/forge/golden/candidates/`（gitignore until curated）；人工裁决转正（origin: curated，provenance 纪律不破，golden.go 指纹机制照常钉）。目标 16→60，验证门禁族优先（verify-acceptance / scope-drift / cheat-scan / gate 时序）。
  - **traps 3→20**：从 cheat-scan 历史命中（checklog 7 类）反混淆生成 trap 骨架；`expect_detected` 保留三态（nil=现状未知→首跑回填）。
  - **判官臂接数据**：`forge review llm`（复用 code-review-gate 独立只读子 agent 形态）产 schema 化 semantic findings（design / mock-hallucination 两类首发）；判分结果进 judge-audit 的 JudgeAuditEntry 流（`evals/forge/judge-samples/` 格式）——κ 执法机制已有，只接数据。
- **落点**：internal/evalkit/golden.go（HarvestCandidates，只读）；internal/cli/eval.go；candidates 目录 gitignore。
- **验收**：harvest 对本仓 `--since v1.38` 产出 ≥40 候选骨架、canonical 目录零写入（测试断言）；curated 后 `forge eval golden run` 指纹校验通过；llm 判官样例 ≥20 且 κ 可算。
- **刻意不做**：全自动转正（无人工裁决不入 golden）；跨仓 harvest（先本仓自举）；判官走 hard（κ 执法只降级不阻断任务推进）。

## L4 数据飞轮 v1（advisory 信号 × 结果关联）——P1/P2

杠杆点：增强回路 #7——组织专属 RLVR 原料的第一铲。先例：DORA 四指标（可测量化研发行为）。

- **现状**：act.Conclusion（7 维评分 + strength + acceptance 比，`internal/act/conclusion.go:35-76`）与 checklog（39 check roster，`internal/checklog/escape.go:81-91`）均已落盘，但**零关联分析**；enforcement 双环只有计数没有 lift。
- **设计**：`forge eval flywheel report`（本地只读聚合，零新遥测）：
  - join 键 task_ref：act.Conclusion × checklog 计数（scope-drift / cheat-scan / assertion-check / test-nudge）× toollog work-activity；
  - 每信号输出：命中率、与低分（<70）及弱证据（strength < Strong）的 lift + Wilson CI（复用 `internal/evalkit/stats.go:22-35`）；样本低于字典下限出 INSUFFICIENT（fail-closed 立场）；
  - 消费：为 enforcement 双环供数（升档 → 审规则而非加码）；P2 仅出设计：jsonl 导出（组织 RLVR 训练行）。
- **落点**：internal/evalkit/flywheel.go 新文件；internal/cli/eval.go 子命令。
- **验收**：对本仓 DataDir 出报告（INSUFFICIENT 亦为通过——诚实态可渲染）；join 键缺失任务安全跳过；零网络调用（测试断言）。
- **刻意不做**：phone-home、跨项目聚合上报、训练集成（P2 只做设计稿）。

## L6 编排组合（证据层适配器）——P2

杠杆点：平台收口时代的组合策略——做编排器下面的证据与签核基底。先例：OTel 供应商中立。

- **现状**：mirror github 已有（台账主真相、issue 可见面，DataDir/mirror-gh.json）；task export bundle schema 键已进 compat 六面。
- **设计**：mirror 适配器接口化（Provider：github 首发 → linear/jira 只读跟进）；对外契约文档《forge 作为证据基底》：编排器消费 task export bundle 的 schema 承诺（挂 compat-commitments 承诺表）。
- **刻意不做**：自建编排前台；实时双向同步（拉模式起步）。

---

## 执行顺序

P0：L5 冻结声明（半天）→ L1 wedge-drill + README 切片（2-3 天）→ L2 数据形状 + seed/guard 同步（2-3 天，实现可拆 P1）
P1：L2 判定分派 + acceptance-assert 证据行 → L3 harvest / traps → L3 判官接数据 → L4 report v1
P2：L4 导出设计稿 → L6 适配器

## 红线对照

- 无 phone-home：L1 反馈走显式 export；L4 本地只读。
- 意见不走 hard：L2 断言全机械；L3 判官 κ<0.6 自动降级 ADVISORY。
- golden 只进人工策展：L3 candidates 与 canonical 物理隔离。
- advisory fail-open 不动摇：L4 / L5 全 advisory，零新阻断位点。
- 单一真相源：断言判定分派唯一实现（heldout 共用）；check roster 与 deterministic map 由 guard test 钉同步。
