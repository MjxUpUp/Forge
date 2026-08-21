# sync-convergence：多机器数据收敛语义设计

> 姊妹篇：`docs/design/node-identity.md`（节点身份与信任）。本文定义每类数据在双机/多机并发写入下的**收敛规则**——这是自动同步（Phase 1）和任务租约（Phase 2）的正确性前提。代码注释只保留局部 rationale，全局权衡在这里。

## 1. 问题

- 现状（`internal/projectsync` + `internal/datamerge`）：jsonl 事件流按行内容去重，幂等 ✅；但 `tasks/*.json` 是**结构化文档**——两台机器同时 `task decide` 同一任务时，行去重救不了，raw copy 是 last-writer-wins 整文件覆盖，静默丢一边的 decisions/next-steps。
- 没有形式化的收敛规则，"双向同步"只是搬运工，不保证双方收敛到同一状态（convergence property）。
- 目标：**任何交错写入序列 + 任意次双向 sync 后，所有机器字节一致**（强收敛），且人类语义字段不静默丢失。

## 2. 数据分类与收敛规则总表

按写入语义把 DataDir 数据分三类，每类一条收敛规则（单一真相源在本节，各实现派生）：

| 类 | 数据 | 收敛规则 | 依据 |
|---|---|---|---|
| **A. append-only 事件流** | `checklog*.jsonl`、`toollog*.jsonl`、`sessions*.jsonl`、`act/conclusions.jsonl` | **G-Set**：按 `(node_id, seq)` 去重取并集；缺 node_id 的存量行按内容 hash 去重（向后兼容） | CRDT G-Set，天然收敛，现有行去重已是其退化形 |
| **B. 结构化文档** | `tasks/*.json` | **字段级分流**：decisions/findings/blockers/next_steps/artifacts/session_links 等记录集 → 并集（G-Set 语义，按 ID/内容键）+ **规范排序**（到达顺序不得渗入字节）；同 ID 双侧编辑 → 确定性胜者（主时间戳早者，规范 JSON 字节序破平）；status/完成块 → 单调采纳 + 双完成时**先完成者胜**；门禁 history prefer-Passed + 同结论取更早 CompletedAt | Figma per-property LWW 的精神；实现落点 `internal/taskpipeline/merge_converge.go` |

> **实现校正（feat/task-convergence）**：TaskState 字段无逐字段时间戳，HLC 无处可落——B 类收敛改用「规范排序 + 确定性决胜」达成同等交换律/幂等性质，无 schema 变更。HLC 保留在事件流（A 类 ts_hlc）与租约（§4）。收敛性质由双层测试锚定：merge 层 40 种子随机交错 property test + datamerge 层双 DataDir 双向合并字节一致。
| **C. 机器本地态** | `freeze/state.json`、session 锚、`hooks/`、`imports.jsonl` 等 | **不同步**（沿用 allowlist 默认拒绝） | 已论证见 project-sync.md §3 |

### 为什么 decisions 必须 append-only（不能 LWW）

- decisions/next-steps 是**接续数据的载体**（`internal/taskpipeline/lock.go` 注释明写：last writer wins 静默丢的恰恰是本设计要保的接续数据）——单机时代用 flock 防，多机时代 flock 失效，只能靠数据结构保证。
- LWW 对人类语义字段 = 静默丢一方的决策记录，团队档下还意味着恶意/错误 peer 能覆盖他人结论（**收敛守卫**：append-only 字段拒绝任何形式的覆盖）。
- status/score 是单值快照，天然 LWW 语义（最新即真相），无丢失问题。

### 存量迁移

- 存量 `tasks/*.json` 的 decisions 等字段视为「同一 node 的批量 append」参与合并（node_id 空 = legacy，排序键退化为行内时间戳）。
- 无 schema 破坏：新字段（`ts_hlc`/`node_id` 于行内）对老版本不可见（JSON 忽略未知字段）。

## 3. HLC 时间戳（LWW 决胜键）

### 决策

- LWW 决胜与租约 TTL 一律用 **HLC（混合逻辑时钟）**：`ts_hlc = <物理毫秒>.<逻辑计数>`，物理相同则逻辑递增；收到更大 HLC 时本地 HLC 跟进（`max(local, remote)+tick`）。
- 几十行实现（stdlib-only），收益：时钟回拨/偏斜下仍单调，决胜确定性不依赖各机时钟同步质量。

### 为什么不用裸 `updated_at`

- Redlock 论战（Kleppmann vs antirez）的核心教训：时钟不可信（GC 停顿/NTP 跳变）。裸时间戳 LWW 在偏斜下会选错胜者且不可复现。
- 先例：CockroachDB/Mongo 用 HLC 支撑跨机 LWW。
- 兜底不变：HLC 完全平手（同物理同逻辑）→ node_id 字典序（确定性 > 正确性：两台机器必须收敛到同一结果，哪怕内容是"错"的）。

### 时钟偏斜检测

- 事件带 `ts_hlc` 后，偏斜可观测：对端事件物理分量与本地差 >5min → `forge doctor` + Pulse 告警（不阻断，fail-open 哲学）。

## 4. 任务租约（Phase 2 预埋语义）

### 决策

- task state 增 `lease: {holder_node, ts_hlc, ttl_sec, fencing}`。
- **个人档 advisory**：他机活跃租约 → 门禁提示"该任务由 node X 持有"，不阻断（fail-open）。
- **团队档 enforced**：claim 才可变更 decided/complete；**必须带单调递增 fencing 序号**，变更时校验 fencing 大于该任务历史最大 claim——否则宁可只做 advisory，不做假互斥。

### 为什么 fencing 不可省

- 纯 TTL 租约在 GC 停顿/时钟跳变下会出现「两节点都自以为持有锁」（Kleppmann 的 fencing token 论证；K8s Lease/etcd lease 都用 TTL + 任期号缓解同一问题）。
- fencing 序号让「过期持有者迟到写入」被确定性拒绝——这是把租约从 UX 提示升级为正确性依据的**唯一**轻量做法。
- 单机 `LockTask`（flock）保留：多机租约是跨机互斥，flock 仍是本机并发写任务的最后一道。

## 5. 收敛正确性：property test 为锚

- **收敛 property test**（`internal/datamerge` 旁新增）：两个临时 DataDir 模拟双机，随机交错写入（事件 append + task decide + status 翻转）→ 随机序双向 sync N 轮 → 断言双方**字节一致** + append-only 字段无丢失。
- 这个测试是防回归的定海神针：收敛规则任何一处实现漂移（排序键、决胜规则、去重键）都会让随机交错序列不再收敛。
- 时钟注入：测试用可控时钟源制造回拨/偏斜，断言 HLC 决胜仍收敛。

## 6. 先例对照

| 决策 | 先例 |
|---|---|
| per-field LWW | Figma multiplayer（不用全量 CRDT，per-property LWW + 决胜） |
| append-only 事件流 = G-Set | local-first 基石（Ink & Switch）；Automerge actor seq 模型（(node_id, seq) 去重与其逐字对应） |
| HLC | CockroachDB / Mongo 跨机 LWW |
| TTL 租约 + fencing | K8s Lease（holderIdentity+renewTime+duration）；etcd lease keepalive；Kleppmann fencing tokens |
| 收敛守卫（拒覆盖他人 append） | git 的 append-only 对象库同构 |

## 7. 明确不做（v1）

- 不引入 CRDT 库 / sync engine（Zero/PowerSync/Automerge-repo 全是 DB 中心化 + server/WASM 依赖，与文件即状态、stdlib-only 冲突）。可偷协议思想：Phase 1 后期「交换 heads 只拉缺失 ops」的增量同步替代全量 bundle。
- 不做实时协作（秒级 OT/CRDT 编辑）——门禁状态是分钟级粒度，LWW+G-Set 足够。
- 不做向量时钟：双场景下节点数小，(node_id, seq) 已覆盖去重，HLC 已覆盖决胜。
