# Forge 兼容承诺表（compat commitments）

状态：生效中（3.0 core-only 收窄后）。历史裁决与预算记账已移入 [docs/archive/compat-rulings-2026.md](../archive/compat-rulings-2026.md)。

本文是 forge 对外的兼容契约——三向 × 三档承诺矩阵 + 门禁文案契约。机械执法点：`forge compat snapshot` / `forge compat check`（七面盘点，任何面变更显式过 golden）；分发面另有 `TestNpmPlatformVersionsAligned`（wrapper 与平台子包版本对齐）。

## 零、先看：破坏性收窄的历史（读旧文档前必看）

- **2.0**：forge 只做「验证与状态层」——门禁降为两道（`task-implement`、`task-complete`），产物改三份（`goal.md` / `change.md` / `acceptance.md`，`tasks.md` 可选），`task-verify`、`task doc-review`、`task mutation`、`task artifact`、`task chain-init`、`forge selfcheck`、`forge eval artifact-drill` 已删除。
- **3.0（core-only）**：删除宿主接线、评测体系、看板、多仓同步与一批项目生命周期命令组（`act`、`bridge`、`config`、`conventions`、`dashboard`、`enforcement`、`eval`、`gate`、`harness`、`health`、`migrate`、`node`、`policy`、`project`、`registry`、`skill`/`skills`、`trace`、`trust`、`update`、`verify`、`workspace`、`worktree`）。因此**宿主面与多机同步面退出承诺矩阵**，本表只剩数据面、命令面、分发面三向。完整清单与替代物见 `CHANGELOG.md` 与 `spec/changes/archive/20261124-core-only`。
- 老 change 的旧布局仍可读（按老规则 verify/gate），但不再生成。

## 一、三向 × 三档承诺矩阵

| 方向 \ 档位 | 强承诺（破坏即 bug） | 弱承诺（尽力 + 行为变更公告） | 不承诺（显式排除） |
|---|---|---|---|
| **对下：数据面**（checklog.jsonl / toollog / TaskState） | 已落盘数据永远可读：append-only + legacy 字段兜底 + 惰性重推导；**序列化键只增不删不改名**（compat golden 守卫） | 语义推导规则可随版本演进（同一行数据在不同版本读出的 Strength 可变——审计视角的已知属性） | 历史行的显示排版 / 未文档化内部字段 |
| **对下：命令面**（CLI 命令与 flag） | 命令与 flag **只增不删不改名**（compat golden 守卫；删除/改名须走 §二 的预告流程） | flag 语义收紧（advisory→blocked）按 §二 文案契约带预告版本 | 输出文本排版（porcelain 可变，Git 惯例）；退出码契约固定：0=过 / 2=BLOCKED / 1=故障 |
| **分发面**（npm） | wrapper 与平台子包版本逐版本互锁（CI 守卫 `TestNpmPlatformVersionsAligned`） | release-please 驱动 semver | — |

## 二、门禁文案契约（advisory → blocked 的预告纪律）

1. **新增 BLOCKED 门禁的拒绝文案必须包含其一**：①预告版本（「自 vX.Y 起 advisory 转 blocked」——仅当该门禁曾以 advisory 形态发布过）；②「首发即 blocked」声明 + 指向本文档（无存量暴露则无预告义务）。
2. **时间承诺**：已发布的 advisory 门禁转 blocked，**预告期 ≥2 个 minor 版本**，期间文案持续显示预告版本。
3. **逃生舱纪律**：阻断位点必须有 `FORGE_*` env 逃生或 per-task override（现行清单以 `compat.EscapeEnvs` 为准，当前 9 个），逃生留 checklog 痕 + evidence 强度封顶 Weak；豁免行携带 reason/owner 元数据。只想拦截「命令组合形态」的门禁（合规形态即放行）不强制配独立逃生舱——放行被禁形态本身就等于取消门禁。
4. **机械执法**：`forge compat check` 的 blocking-sites 面使新增 BLOCKED 位点在 golden diff 里显式可见——新增须同时更新本文档或 `CHANGELOG.md` 的行为变更节。
5. **阻断正当性分类（单一真相源）**：一处阻断只有落进下表某一类才正当，并须满足该类的必备约束；落不进任何一类的只能 advisory。判别顺序自上而下、命中即停：拦的是命令组合形态 → ③；放行权须在人 → ⑤；拦的是时机或顺序而非交付内容 → ④；证据或评测本身不成立 → ②；交付声明缺可核验的事实或记录 → ①。（早期的「⑥ 宿主提升」类已随宿主层在 2.x 移除，历史文本见 archive。）

| 类别 | 何时正当 | 必备约束 | 现行代表位点 |
|---|---|---|---|
| ① 事实缺失 | 交付声明缺可机器核验的事实或记录：验收未实跑或快照过期、改 SKILL.md 未记决策、需求未被验收覆盖、文档回检或代码审查未记录、checklist 未勾完、上游依赖未交付 | 逃生舱（env 或 per-task override），逃生留 checklog 并封顶 evidence Weak | acceptance-gate、skill-decisions、req-coverage、doc gate、task-complete 前置 |
| ② 证据不合法 | 证据或评测自身不成立：held-out 逃逸、自报验证命令与 toollog 不符、评测 preflight 不合法、bundle 签名不符 | 同 ①；签名不符无逃生（逃生等于接受伪造） | held-out cheat-suspect、self-report、eval preflight |
| ③ 形态 | 拒绝命令组合形态而非操作本身，合规形态即放行 | 按第 3 条形态门禁豁免，可不配独立逃生舱；拒绝文案须写明放行形态 | gate-cmd-form |
| ④ 流程节奏 | 防门禁空转或脱离任务流程：门禁间无工作、未读即改、任务分支外提交 | 须有会话阻断上限（超限回落 advisory）或明确出口命令 | work-activity、task-drift（上限 5） |
| ⑤ 安全 HITL | 高危或越权操作及其未经人工确认的悬账，放行权在人 | 出口须由用户发起（真人终端执行） | task-tree-guard 的错树写入拦截 |

## 三、生效口径与存量处理

- 本契约自首个含本文档的版本生效。3.0 的 core-only 收窄是**显式破坏性变更**：清单、替代物与理由见 `CHANGELOG.md` 3.0 节与归档 change `spec/changes/archive/20261124-core-only`。
- 既有逃生舱（完整清单以 `compat.EscapeEnvs` 为准）**不设 TTL**（env 传入式 = 不传即失效）。
- 历史裁决记录（drop-score、task-tree-guard 批、逃生舱审计、纯新增 flag 判定等）按时间保存在 [docs/archive/compat-rulings-2026.md](../archive/compat-rulings-2026.md)，供审计复盘，不再逐条重述。

## 四、刻意不做（记录裁决，防重新发明）

- **事前审批门禁**：DORA 实证外部审批与交付绩效负相关——用事后审计 + 自动化复查承接。
- **基线文件式棘轮**（冻结存量违规）：forge 规模走「先清零」路线（新门禁默认只对新任务生效）。
- **GODEBUG 式按版本给默认行为的完整机制**：等 BLOCKED 门禁数量增长后再评估（当前 9 个逃生舱，管理成本不成比例）。

## 五、命令面冻结（L5 杠杆，2026-09-07 起）

- 单个 minor 内 commands **净增 ≤2**，且每个新增命令的 PR 须附设计文档链接。`forge compat check` 的 `net-added commands` 行是该预算的软仪表——只展示不强断。
- 超预算须在本节当期追加裁决记录（援引边界与格式同 §三），否则按违例评审。
- 历史记账（1.56 至 1.79 各批次、含超预算违例与追认）见 [docs/archive/compat-rulings-2026.md](../archive/compat-rulings-2026.md)。
