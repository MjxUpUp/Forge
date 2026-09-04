# Forge 自评测体系（Forge Evaluation System）设计方案

状态：设计稿 v3（合成 v1 端到端轨与 v2 治理轨；v1/v2 均废弃。建议分支 `feat/forge-eval-p0-p4`，按 P0→P4 分期合入）
依据：调研报告《Harness 评测深度调研》2026-09-03（本地调研工件，~/.forge/research/harness-eval-*/report.md）。

---

## 一、Harness 的广义定义与 Forge 的位置

### 1.1 harness 是光谱，不是单一形态

广义 harness 覆盖一个光谱：**薄 scaffold**（mini-swe-agent，~100 行 bash 循环）→ **agent 平台**（Claude Code / Codex CLI，完整上下文/工具/调度/验证栈）→ **治理/协议层**（在宿主 harness 上叠加流程与门禁）。评测方法论因此分两层但同属一体：凡是改变 agent 端到端行为的层，都用端到端轨测量；凡是可单独归因的组件，都用组件轨测量。

### 1.2 Forge 在光谱上的真实位置：横跨四层

对照调研的 ETCSOVG 七层分类，Forge 实际占据 **4/7 层**，v2"只占 V/G 两层"的说法是错的：

| ETCSOVG 层 | Forge 占据方式 |
|---|---|
| **C**ontext | skills 注入（UserPromptSubmit 触发）、conventions digest 与任务接续上下文的 SessionStart 注入 |
| **S**cheduling | taskpipeline 门禁塑形 agent 循环（完成/验证门的 BLOCKED/ADVISORY）、多任务并发编排 |
| **V**erification | verification-driver、code-review-gate、compile-fix-loop、acceptance 门禁 |
| **G**overnance | project policy（takeover/让位/写锁）、checklog 审计、escape-hatch 治理 |

不占 Execution/Tool/Observability 的主体（这三层仍由宿主承担，Forge 通过 hooks 挂入）。

### 1.3 由位置推出的评测结论

Forge 是广义 harness 中"厚"的一类，其评测必须双轨：

- **Track A · 端到端轨（agent harness 评测）**：profile（off/gates-only/full）× model × 第三方基准——测量 Forge 作为整体 harness 对任务成功率、成本、轨迹质量的影响。这是 v1 的机器，恢复为一等公民。
- **Track B · 组件轨（治理层评测）**：golden 标注集、对抗陷阱、遥测、judge 审计、接续演练——逐组件回答"门禁拦得准不准、稳不稳、贵不贵、安不安全"。这是 v2 的机器，保留。
- **双轨桥接**：wait_tax 双层测量——Track A 观测端到端等待代价，Track B 归因到具体门禁/确认点。两轨数字必须可互相对账（端到端 wait ≈ Σ组件 wait + 宿主自身 wait）。

v1 的错误是把 Track B 当不存在（跑分即一切）；v2 的错误是把 Track A 降级为可选（把 Forge 窄化成插件）。v3 = 双轨并立，组件轨先行（便宜、立即可做），端到端轨按里程碑进入。

---

## 二、总原则

1. **主张驱动**：每个指标挂在 Forge 明确主张上（§三字典），挂不上的不进字典。
2. **遥测先行，基准按里程碑进入**：Track B 数据已在盘上（checklog/toollog/registry），P1 即得；Track A 依赖 runner，P2 MVP、P3 首跑。
3. **golden 标注集是门禁质量的唯一硬尺**：precision/recall 来自人工标注；干净样本库误报率是任何门禁评测的前置（do-nothing 基线思想移植）。
4. **判分器自身受审**：docgate rubric 按 ABC I.c.1 披露重放方差与人工一致率（κ<0.6 时其下游决策降级 ADVISORY）。
5. **防泄漏纪律**：golden 公共部分进 VCS、私有子集 0600 季度轮换；Track A 任务集 manifest 指纹化 + 污染旗标。
6. **profile 阶梯是统一实验因子**：off / gates-only / full 贯穿两轨——Track A 测整体贡献，Track B 逐组件消融，两轨共享同一因子定义。
7. **成本即评测定义**：无预算上限的分数不采信；两轨所有 run 强制 token/$/墙钟三重预算，截断必披露。
8. **沿用 repo 接入协议**：`FORGE_EVAL_*` 逃生舱 + checklog 留痕 + 证据封顶 Weak；`util.AtomicWrite` 用户级落盘；CheckName 进 roster；单一真相源 + guard test。

---

## 三、指标字典（单一真相源 `evals/forge/metrics.yaml`）

### Track A（端到端）

| 指标 | 定义 | 误用注记（硬性字段） |
|---|---|---|
| e2e_pass1 | profile×model 在锚基准 frozen split 的 pass@1 + 双源噪声 95% CI | 与裸宿主差异小于噪声地板（2.2-6.0pp）时不可宣称贡献 |
| e2e_passk | pass^k 曲线（k=1..8） | pass@1 高 pass^k 低 = 不稳定，部署视角看后者 |
| e2e_cost | $/task、token/task、墙钟；截断率必披露 | 无预算上限的精度比较无效 |
| e2e_wait | 端到端 no-action turns / 确认等待轮次 | 是 Forge 的核心代价轴，必须与 e2e_pass 同图 |
| profile_delta | full − off（含 CI） | 唯一回答"Forge 值不值"的数字；CI 跨零时只可说"未检测到差异" |
| hv_mv_ratio | 方差分解：harness(profile) 方差 / model 方差 + 翻转数 + η²_p | 非普适常数，随任务水平程与 profile 对比度变化，必附实验规格 |

### Track B（组件级）

| 指标 | 定义 | 误用注记 |
|---|---|---|
| gate_recall / gate_fpr | golden 缺陷样本被拦比例 / 干净样本误触比例（分门禁计） | recall 高若伴随 fpr 上涨，用户会学会无视门禁 |
| gate_determinism | 同输入 k=5 重放一致率（确定性门禁期望 1.0，<1.0 记 bug） | 100% 只代表可复现，不代表正确 |
| trap_capture_rate | 陷阱样本（测试削弱/伪造证据/虚假完成）被识破比例 | 高 capture 不是安全证明，低 capture 是行动信号 |
| judge_agreement | docgate rubric 重放方差 + 与人工标注 Cohen's κ | κ<0.6 时 75 分阈值视为噪声，不得支撑 BLOCKED |
| resume_fidelity | 脚本化接续演练的正确接续比例 | 演练≠真实复杂度，只做回归对比 |
| override_rate / escape_rate / off_churn | 门禁被推进比例、FORGE_* 使用率、managed→off 迁移数 | override 高≠门禁坏；=0≠健康；必须与 fpr 联合读 |
| injection_block_rate | 注入探针被策略面阻止比例 | 通过率是回归基线，不是安全证明 |
| self_gate_pass_rate | forge 仓库自身被 forge 管理期间门禁通过率 | 自举无对照，只做趋势 |

字典规则：每条必含 `claim / track / definition / source / misuse_note / min_samples`，缺一 fail-closed；n<min_samples 只出 `insufficient`。

---

## 四、领域模型与包结构

```
internal/evalkit/
  metrics.go                 # 字典加载/完整性校验（fail-closed）
  card.go                    # 治理披露卡：Forge 占层声明、hook 清单、gate roster、逃逸清单、已知盲区
  telemetry.go               # checklog/toollog/registry 聚合（Track B / C4-C7）
  golden.go                  # golden 标注集：加载/指纹/运行/precision-recall
  traps.go                   # 对抗陷阱集
  judgeaudit.go              # docgate rubric 重放 + κ
  resume.go                  # 接续演练执行器
  runner.go                  # Track A 隔离执行器：per-task 沙箱、三重预算、轨迹落盘（v1 机器）
  decompose.go               # profile×model 方差分解编排（v1 机器）
  stats.go                   # 双源噪声 CI、Wilson 区间、κ、pass^k、η²_p、重放一致率
internal/cli/eval.go         # forge eval 命令族（card/dashboard/golden/traps/judge-audit/resume-drill/run/decompose；避让 forge harness、harnessdetect）
evals/forge/                 # VCS 资产
  metrics.yaml               # 双轨指标字典
  gates-card.yaml            # 治理披露卡
  golden/  traps/            # 公共标注集与陷阱集
  manifests/                 # Track A 锚基准 frozen manifest（版本 + 污染旗标 + oracle 标记）
~/.forge/evals/forge/        # 用户级数据
  golden-private/            # 0600 私有子集
  snapshots/                 # dashboard 快照
  runs/ decompose/           # Track A 工件
```

关键类型：`RunSpec`（profile×model×benchmark@split×预算×k，四元组指纹化）、`EvalRun`（状态机 planned→running→grading→completed/failed/aborted，无回退边）、`GoldenCase`（输入状态 + 期望门禁行为 + 标注人 + 指纹）、`Scorecard`（header 必含评测对象声明：profile×model×benchmark 四元组——ABC III.6）。

治理披露卡语义：声明 Forge 占 ETCSOVG 哪四层、各以什么机制（skills 注入/门禁/policy），以及明确不碰的三层归宿主——对外可审计"宿主行为被改了哪里"，并与 profile 阶梯互证（gates-only 档 = 仅 S/V/G 生效，full 档 = C/S/V/G 全生效，两档差值即 context 注入层的贡献）。

---

## 五、统计协议（stats.go 计算契约）

1. **Track A**：每格 ≥2 次、全量 ≥5 重复；主指标 pass^k 曲线、pass@1 仅参考；双源噪声 CI（运行间 + 任务抽样二项，最低 Student's t，默认 bootstrap-over-tasks）；确定性配置 0 误差 = 计算错误告警。
2. **profile_delta**：full − off 配对差 + bootstrap CI；CI 跨零 → 结论固定为"未检测到显著差异"，禁止挑选单侧表述。
3. **方差分解**：必报 HV̄/MV̄ 与排名翻转数（η²_p 单观测格不可定义，暂不产出）（与翻转数配对），附任务集与格次数声明。
4. **Track B**：比例一律 Wilson 95% 区间，n<min_samples 出 `insufficient`；judge 一致性 κ<0.6 → 下游自动降级 ADVISORY 并标注。
5. **预算截断**：记 budget-cut（非 fail 非剔除），Scorecard 披露截断率；成本比较一律对齐预算。

---

## 六、分期范围与行为契约

### P0 — 字典与披露卡（1 周，零算力）

范围：metrics.yaml 双轨全字典；gates-card.yaml；`forge eval card`（校验+渲染）；`dashboard --dry-run`（字典 + 数据源连通自检）。

| 场景 | 行为 |
|---|---|
| 字典条目缺七硬性字段任一 | card/dashboard exit 2，`BLOCKED: 指标 <名> 缺 <字段>`，checklog 记 `eval-metrics-incomplete` |
| 删 C1-C7 任一 claim | guard test 红 |
| `forge eval card --render` | 输出五节：占层声明（4/7 层及机制）、hook 清单、gate roster、逃逸清单、已知盲区；缺"已知盲区"节 = 校验失败 |
| 新机器无 checklog 数据 | 指标出 `insufficient`，不报 0（0 与无数据是不同事实） |

### P1 — 遥测仪表盘 + golden v1（2-3 周，Track B 主干）

范围：`forge eval dashboard`（override_rate、escape_rate、wait_turns、off_churn、self_gate_pass_rate，周分桶 + Wilson 区间 + insufficient 标注 + 误用注记随行）；golden v1（test-discipline、compile-fix、verify 三门禁各 20-30 标注样本，`forge eval golden run` 出分门禁 precision/recall 基线）。

| 场景 | 行为 |
|---|---|
| golden 样本缺标注人或期望行为 | fail-closed，`BLOCKED: golden <id> 标注不完整` |
| 干净样本上门禁触发 | 报告标 `gate_fpr>0`，该门禁记 bug 级 finding |
| golden 指纹与 manifest 不符 | run 拒绝启动（防改样本凑数字） |
| dashboard 输出任一比率 | 必附误用注记 + 样本量；低于 min_samples 只出 `insufficient` |

### P2 — 门禁对抗 + Track A runner MVP（4-6 周，双轨并进）

范围：Track B——确定性重放（golden × k=5）、陷阱集 v1（测试削弱/伪造 checklog 证据/虚假完成三类，ImpossibleBench 思想）、`forge eval judge-audit`（rubric 重放方差 + 人工 κ）、resume-drill v1（3 个脚本化接续演练）。Track A——`forge eval run` MVP：锚基准（Terminal-Bench frozen manifest，Docker 优先、fallback-home 标注降级）+ oracle / do-nothing / 成本截断三基线 + off vs full 小对照冒烟（20 题 × 2 profile × 1 模型，验证全流程与 wait_tax 观测）。

| 场景 | 行为 |
|---|---|
| 确定性门禁重放一致率 <1.0 | 报告记 bug 级 finding，不阻塞其他门禁 |
| 陷阱样本未被识破 | 计入 trap_capture_rate 分母，按类型聚合行动清单 |
| judge κ<0.6 | 报告明示阈值失效，checklog 记 `eval-judge-weak` |
| run 启动前 manifest 指纹不符 / oracle 未过全任务 / do-nothing 得分>0 | 拒绝启动，exit 2（oracle 不可解 = 环境坏；do-nothing 通过 = 判分定义缺陷） |
| 预算截断 | 任务记 budget-cut，Scorecard 披露截断率与清单 |
| 无 Docker | 可运行但 Scorecard 标注 sandbox=fallback-home，对照结论只出 ADVISORY |
| `FORGE_EVAL_GOLDEN=1` / `FORGE_EVAL_TRAPS=1` | 跳过对应运行 + checklog escape-hatch 行 + 证据封顶 Weak |

### P3 — 防泄漏 + 方差分解首跑（持续，首轮 2 周）

范围：golden-private（0600，每门禁 5-10 题）+ `forge eval golden rotate`（季度换 1/3，rotate 前 oracle 复验，失效淘汰补新）；`forge eval decompose` 首跑：off / gates-only / full × 2 模型 × 100 题 × 2 重复——首次同时回答"Forge 整体贡献"（full−off）与"context 注入层贡献"（full−gates-only）与"纯门禁代价"（gates-only−off）。

| 场景 | 行为 |
|---|---|
| golden-private 权限非 0600 | run 拒绝使用该子集，exit 2 |
| rotate 执行 | checklog 记 `eval-golden-rotate`（换入/换出 id、失效原因） |
| decompose 完成 | 报告必含 HV̄/MV̄、翻转数、η²_p + 三档成本差（$/task、wait_turns）；结论只做区间表述 |
| 公共锚与私有 golden 分差超置信带 | 报告顶部 `ADVISORY: 疑似基准过拟合` |

### P4 — 季度运营节奏

范围：季度自评测报告（两轨快照 + 环比 + 行动清单）；Track A 大体检季度复跑；对外披露包（card 版本 + decompose 区间结论），页脚固定调研教训："单边引用任何一个数字都是方法性误导"。

---

## 七、配置与集成

- **配置链**：`FORGE_EVAL_*` > `~/.forge/config.json` eval 段（min_samples、桶宽、锚基准、预算月度上限、repeats）> 代码默认；版本化拒读旧 schema。
- **checklog roster**：新增 `eval-metrics-incomplete`、`eval-golden-run`、`eval-golden-rotate`、`eval-judge-weak`、`eval-traps-run`、`eval-run`、`eval-decompose`，登记 `internal/checklog/types.go`。
- **taskpipeline 关系**：两轨均为观测/开发期工具，零新增任务门禁、不改 advisory/hard 分层；release-readiness 可选调用 `golden run`（ADVISORY）。
- **skillseval 边界**：C5 复用 skillseval，不重建；evalkit 只新增 `evals/forge/` 子树。
- **密钥**：被测模型 key 只从 env 读；trace 落盘前 scrub（测试钉住）。

---

## 八、测试设计要点

表驱动、同目录、断言终态；全离线（LLM 相关仅 judge-audit 与 P4，env-gated）。Track B：telemetry 用固定 checklog/toollog 夹具断言比率与 insufficient 边界；stats 黄金值（Wilson/κ/η²_p/pass^k 各一组）；metrics.yaml 坏样本逐一断言 fail-closed；traps 带标记防 skillscan 误伤（guard test 钉住）。Track A：runner 用 scripted fake runner 单测（固定 pass 序列），verify_scenarios 式隔离 HOME 集成测试跑 2 任务假基准断言 Scorecard/checklog 行；渲染 golden 快照；header 缺声明 fail-closed 有专测；每条 FORGE_EVAL_* 断言留痕 + Weak。

---

## 九、成本

| 动作 | 频率 | 成本 |
|---|---|---|
| dashboard / golden run（确定性门禁） | 随时 | ≈0 |
| P2 Track A 冒烟（20 题 × 2 profile） | 一次 | $50-150 |
| P3 decompose 首跑（100 题 × 2 重复 × 3 profile × 2 模型） | 一次 + 季度 | $800-2,000/次 |
| judge-audit | 季度 | 数十次 rubric 调用 |
| golden 标注 | 建设期主要人工投入 | 从 checklog 历史拦截事件反哺样本降低成本 |

预算护栏：RunSpec 三重预算强制；单月评测支出超 config 上限 → 启动前 BLOCKED（`FORGE_EVAL_BUDGET` 逃生 + 留痕）。

---

## 十、红线对照

- **advisory fail-open / HARD 在 executor**：两轨零新增任务门禁；BLOCKED 只出现在"评测自身不合法"（字典缺字段、golden 标注不全、指纹不符、oracle 不可解、do-nothing 击穿判分、权限错误）。
- **逃生舱必有痕**：全部 `FORGE_EVAL_*` 接 checklog escape-hatch 行 + 证据封顶 Weak。
- **单一真相源**：双轨指标字典、gate roster（复用 taskpipeline 既有）、陷阱类型枚举、profile 阶梯定义、CheckName 各一处 + guard test；Scorecard 渲染单点。
- **用户级状态 / AtomicWrite**：runs、snapshots、golden-private、decompose 全在 `~/.forge/evals/forge/`；VCS 资产沿 `evals/` 惯例；大工件按 run-id 存放 + 保留策略（默认最近 10 次 run + 全部 Scorecard）。
- **诚实呈现**：insufficient 与 0 严格区分；误用注记强制随行；profile_delta CI 跨零固定表述"未检测到显著差异"；两轨数字冲突时（端到端 wait ≉ Σ组件 wait）作为 finding 报告而非静默取齐。

---

## 十一、风险与未决问题

1. **golden 标注人工成本**是主要投入：v1 先覆盖 2-3 个高频门禁，样本从 checklog 历史真实拦截事件反哺。
2. **门禁演化使 golden 失效**：rotate 内建 oracle 复验；门禁行为契约变化与 golden 更新须同 PR。
3. **遥测归因边界**：override 率上升可能是门禁变严/任务变难/用户变化，dashboard 只呈现联合视图，不出单因结论。
4. **Track A 锚基准时效性**：Terminal-Bench 版本迭代快（2.x→4.0），manifest 锁版本 + 污染旗标；跨版本分数不可直接比，环比必须同版本。
5. **宿主 headless 语义**：Track A 跑分时宿主确认交互需 headless 策略；wait_turns 把等待变成被测对象而非噪声——P2 实现期与宿主接口对齐。
6. **双轨对账缺口**：端到端 wait 与组件级 wait 之和若系统性不符，说明存在未观测的摩擦点（本身就是 finding）；对账逻辑 P3 落地。
7. **profile 阶梯的工程前提**：`gates-only` 档要求 Forge 支持"仅注册门禁 hooks、不注入 skills/conventions"的降级装配——当前装配粒度是否支持需 P2 实现期确认，不支持则先实现该开关（它同时是消融实验与用户真实需求的交集）。
