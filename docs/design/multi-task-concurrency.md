# multi-task-concurrency：同 repo 多任务并发与任务接续总体设计

> 特性分支：`feat/multi-task-concurrency`（单一 goal；执行期拆多个任务，见 §12）。本文记录六层架构的完整设计决策与依据；代码注释只保留局部 rationale，全局权衡在这里。
>
> 调研基线（2026-08）：业界生产系统（Claude Code worktrees、Codex rollout、Cursor 云 VM、Amp thread）收敛出的模型 + STORM（arXiv 2605.20563）与 durable execution（Temporal 系）的理论对应物。出处见 §2.4。

## 0. 背景与问题

AI 时代单 repo 多任务并发是常态：多个窗口/多个 agent 同时推进多个任务分支。Forge 当前架构在此形态下的实际故障（均已代码定位）：

| # | 症状 | 根因（file:line） |
|---|---|---|
| P1 | 窗口 B 的 Stop 事件被窗口 A 的未提交改动触发 skill 提示（`Stop · source_changed_uncommitted`） | `condSourceChanged` 全树 `git status --porcelain` 扫描，零会话归属（`internal/skilltrigger/conditions.go:38-61`） |
| P2 | 接续视图（HANDOFF）把别的任务 WIP 当成本任务现场 | `gitPorcelain` 同样全树扫描（`internal/cli/task_continuity.go:1172-1188`）；`taskChangedFiles` 未提交段同病（`internal/taskpipeline/testcoverage.go:359-391`） |
| P3 | 任务 B 开工抹掉任务 A 的证据链 | `task start` 对项目级 checklog/toollog 的 `Clear()`（`internal/cli/task.go:678-683`） |
| P4 | review 印章跨 worktree 同分支串用 | stamp 按 `<branch>.stamp` 键控于共享 DataDir（`internal/review/stamp.go:508-514`） |
| P5 | 会话→任务绑定误挂 | 兜底"唯一未完成任务"在多任务并发下失效；`last-session.json` 15min 指针双窗口竞态（`internal/taskpipeline/session.go:703-798`） |
| P6 | 无多任务工作流支持 | 无 worktree 生命周期管理、无文档 |

**架构级诊断（一句话）**：Forge 有 Temporal 式的中央台账和 Cursor 式的任务对象，却把"共享可变工作树"当成任务状态的真相源。重设计 = 把这句话倒过来——**台账是真相，工作树是需要归属过滤的不可信输入**。

已有的并发资产（不推倒）：中央 DataDir（`~/.forge/projects/<key>/`，key 按 git common-dir，跨 worktree 共享——刻意设计，见 `docs/design/project-sync.md`）、任务文件锁 `LockTask`、跨机租约（TTL 4h + fencing counter）、`offered/claimed` 认领、session 级 `active-task-ref-<sid>`、committed 段跨任务归因（`internal/taskpipeline/taskattribution.go`）。

## 1. 目标与非目标

### 1.1 目标（含验收条件）

| 目标 | 验收条件 |
|---|---|
| G1 多任务并发互不串扰 | 自动化测试：双会话（同目录或双 worktree）下，A 的编辑不触发 B 的 Stop skill 提示；B 的接续视图不含 A 的文件 |
| G2 退出重进无损续上 | kill -9 注入测试：崩溃后新窗口 resume 输出与崩溃前等价（golden 对比）；接续只依赖台账与产物引用，不依赖旧 session id 存活 |
| G3 阶段产物成为跨阶段契约 | plan/spec/review-attempts 落文件、带内容哈希引用；review FAIL 轮次的发现作为下一轮输入（自动化测试覆盖回灌路径） |
| G4 兼容不回退 | 单任务/串行/无 session-id 宿主行为不劣于现状；存量任务数据无需手工迁移 |

### 1.2 非目标

- **运行时隔离**（端口/DB/node_modules per worktree）：引用生态方案，Forge 不自建（Upsun 分析已证明无标准解）。
- **语义级冲突预测**：开放研究题，进 §16 调研清单，不在本设计内实现。
- **同目录多任务达到 worktree 同等隔离强度**：受支持但明确为降级形态（L3 归属撑正确性，L4 才撑隔离）。
- **STORM 式读写中介**：要求中介每一次读写，与旁路 hook 架构不兼容，明确不做。

## 2. 参照系与不变式

### 2.1 业界收敛模型

| 系统 | 持久单元 | 会话（worker） | 文件系统隔离 | 合并 |
|---|---|---|---|---|
| Claude Code | transcript（按目录键控） | 进程，可死可换 | 官方 worktree（创建/清理/逃逸防护/清扫器） | branch |
| Codex CLI | rollout 文件（每会话独立） | 每会话一进程 | 进程级 cwd | branch |
| Cursor 云 agent | 任务对象（云端） | 无状态 worker | 任务一 VM（clone 仓库） | `agent/` 分支 → PR |
| Devin | session 对象 | 浏览器/worker | session 一 VM（COW） | PR |
| Amp | thread（持久对话对象） | 可换 | 每 thread 独立 checkout（产品层吸收） | branch |

### 2.2 理论对应物

- **Durable execution / event sourcing**（Temporal）：状态 = 事件日志的 fold；崩溃/重进后 replay 重建；worker 死亡是常态事件。
- **乐观并发控制**（Kung & Robinson 1981；STORM 的工程化）：read-set/write-set 校验优于事后合并——但其全中介前提不满足，Forge 只取"归属 + 诚实暴露无主"子集。
- **Lease + fencing token**（Gray & Cheriton）：Forge 已实现，保持。
- **Spec-driven development 流派**（GitHub spec-kit / Kiro / LoopSpec）：阶段产物文件化 + 门禁即节点 + 失败轮次保留回灌。

### 2.3 六条不变式（本设计的公理）

- **I1** 任务是持久实体，会话是可抛弃的 worker。任何"接续"依赖 session id 存活的设计都是错的。
- **I2** 文件系统（工作树）是可抛弃的工作副本，不是任务状态。所有对工作树的读取都要经过归属过滤。
- **I3** 跨 worker 的机器态只经协调者存储（中央 DataDir），不经环境文件旁路。
- **I4** 恢复 = 从持久事件重放，不是从快照比对。禁止破坏性操作（`Clear()` 类）。
- **I5** 每类产物只有一个拥有介质（文件 or 类型化状态），另一侧只持可验证引用（内容哈希）。
- **I6** 可从证据推导的状态不得旁存为权威（旁存即缓存，失配重算）。
- **I7 宿主中立**：所有新增面（CLI、文件布局、env、协议）不绑定任何 agent 生态——Claude Code / ZCode / kimi / deepseek harness 等一律平等。宿主差异只在 hostcap 注册表吸收；设计文档引用 Claude Code 等仅作调研出处，不构成实现耦合。

### 2.4 出处

Claude Code worktrees 官方文档（code.claude.com/docs/en/worktrees）；Cursor Cloud Agents（cursor.com/docs/cloud-agent + sandbox 分析）；Codex 多会话（openai/codex discussion #341）；STORM（arxiv.org/html/2605.20563v1）；Temporal durable execution（temporal.io/blog/what-is-durable-execution）；GitHub spec-kit（github/spec-kit）+ Martin Fowler SDD 分析；LoopSpec（github.com/mingyuans/LoopSpec）；Upsun worktree 局限分析；GitButler session-id 归属。

## 3. 总体架构：六层

```
┌──────────────────────────────────────────────────────────────┐
│ L6 产物契约层   specs/<ref>/{proposal,spec,design,plan,tasks} │
│                + attempts/round-NNN（失败保留回灌）           │
│                TaskState.Artifacts ⇄ 文件 hash 双向引用 (I5)  │
├──────────────────────────────────────────────────────────────┤
│ L1 身份层      Task / Session / Workspace 三元组              │
│                解析链: session指针→worktree绑定→分支映射→显式ref│
├──────────────────────────────────────────────────────────────┤
│ L2 状态层      中央台账 + 事件日志（无破坏性操作）(I4/I6)      │
├──────────────────────────────────────────────────────────────┤
│ L3 归属层      attribution ledger + Stop 对账 + 无主暴露 (I2)  │
├──────────────────────────────────────────────────────────────┤
│ L4 隔离层      worktree-per-task 生命周期 + janitor            │
├──────────────────────────────────────────────────────────────┤
│ L5 退化与安全矩阵   崩溃/无sid/非git/跨机/注入面/噪音预算      │
└──────────────────────────────────────────────────────────────┘
```

依赖方向：L5 是所有层的横切约束；L1 依赖 L2；L3/L4 依赖 L1（wtid）；L6 依赖 L1+L2。实现上这不是六个系统——新代码集中在两个小模块（`internal/worktree`、`internal/attribution`）+ 约 10 处调用点改造（§10）。

**物理承载（harness repo）**：L2/L6 的持久内容（tasks、checklog、archive、specs）落 **harness repo**——git 化的用户级 DataDir（`forge harness init` 把 `~/.forge` 变成私有 git 仓库，布局原样，git 缺失时退化为今天的纯文件行为）；机器本地与短命态（markers、attribution、worktree 绑定、locks、**stamps/hazards 信任锚**）gitignore 排除。建立引导见 §13。

## 4. L1 身份层：Task / Session / Workspace 三元组

### 决策

现状只有 Task（持久）与 Session（ephemeral，已有 session 级指针）两个身份，Workspace 是隐式的。本层把它显式化为第三身份。

**数据模型**（`internal/worktree`，新包——刻意不叫 workspace：`internal/workspace` 已被多仓分组特性占用，见 `multi-repo-workspace.md` §2）：

```go
// ~/.forge/projects/<key>/workspaces/<wtid>.json —— wtid = hash12(worktree 绝对路径)
type Workspace struct {
    ID         string    // wtid
    Path       string    // worktree 绝对路径（EvalSymlinks 后）
    Branch     string
    TaskRef    string    // 绑定的任务
    CreatedBy  string    // session id
    CreatedAt  time.Time
    LastSeenAt time.Time // heartbeat，hook dispatcher 顺带刷新
    Node       string    // node id（跨机诊断用；绑定本身是本机性的）
}
```

主检出（非 worktree）也是一个 Workspace（`main`），与 worktree 同构对待——"同目录多任务"与"worktree 多任务"走同一套解析与归属，只是隔离强度不同。

**解析链 v2**（`ActiveTaskState` 重写，顺序固定、每环纯函数）：

1. session 指针 `active-task-ref-<sid>`（现状保留）；
2. **cwd → wtid → workspaces/<wtid>.json → TaskRef**（新增；绑定是持久的，不做新鲜度门控—— freshness 只影响展示，不影响归属）；
3. 分支映射（`internal/taskcontext/detector.go`，保留）——加守卫：映射到的任务若已被**其他活跃会话**锚定且本 cwd 无绑定命中 → 视为歧义，返回 nil + 候选清单（复用 `resume --hook` 已有的候选输出路径，`task_continuity.go:93`）；
4. **删除**"唯一未完成任务"环境猜测兜底——多任务时代它从安全网变成误挂源头（P5）。

**Heartbeat**：hook dispatcher 现有的会话登记点（`EnsureHookSession`/`TouchLastSession`，`internal/cli/hook.go:623-640`）顺带刷新当前 wtid 的 LastSeenAt，零额外进程。

**退出重进语义**（G2 的核心机制）：

- 同目录/worktree 重开窗口：新 session id 无所谓，解析链第 2 环（cwd 绑定）命中，SessionStart `task-resume` 自动挂对任务。
- 跨机：workspace 绑定含路径，本机性——跨机续上走显式 `--ref` 或 project sync + 分支映射，符合现状预期。

### 为什么

"退出重进"的正确锚点不是 session（必变）而是目录（稳定）+ 台账（持久）。这与 Claude Code transcript 按目录键控、Cursor 任务对象与 worker 解耦是同一个模型。

### 边界

- 分支映射歧义守卫依赖"活跃会话"判定：复用 `HasActiveTaskFromOtherSession` 的 7d TTL 语义（`internal/taskpipeline/state.go:226`）。
- legacy 全局 `active-task-ref`（无 sid 后缀）：一次性迁移为 main workspace 绑定（§11）。

## 5. L2 状态层：事件化与无破坏性操作

### 决策

1. **废除 `Clear()`**（P3 根因）：`task start` 不再清空 checklog/toollog，改为追加一条 `task_started` 边界事件；所有查询按边界过滤视图。
2. **条目任务化**：checklog `Entry` 增加 `TaskRef string`（SessionID 字段与按会话过滤语义已存在，`internal/checklog/store.go:284-303`，同构扩展到任务维度）。门禁写证据时带 task-ref（gate 执行时必知 active task）。
3. **stamp 内容寻址**：新写 `stamps/<diffhash>.stamp`（diff-hash 主键）；`<branch>.stamp` 保留只读兼容。`Evaluate` 优先 diff-hash 匹配（现有跨分支 `knownReviewed` 逻辑升为正道，`stamp.go:212-232`）——同分支双 worktree、改基重放、cherry-pick 场景全部自然正确。
4. **派生态降级为缓存**：`ReviewPassed` 等字段保留（避免全量重算），但 task-complete 校验时以 checklog 证据为准，缓存失配 → 重算并告警（I6）。

### 为什么

`Clear()` 同时制造三个生产事故面：双任务证据互抹、崩溃后审计断链、跨机合并语义破碎（事件可确定性合并，被清掉的不能）。事件化之后"开新任务"从破坏性重置变成视图边界——这是 durable execution 的基本纪律，也使 `datamerge` 跨机同步有了确定性基础。

### 边界

- 旧条目无 TaskRef → 兼容读为"全局条目"（与 SessionID 为空的既有语义一致），不迁移数据。
- jsonl 增长：janitor 按 `task_started` 边界滚动归档超过 N 个已完成任务的段（§7）。

## 6. L3 归属层：变更归属视图

### 决策

**单一归属服务，多个消费者**——不在每个调用点各自打补丁。

**Ledger**（`internal/attribution`，新包）：

```
~/.forge/projects/<key>/attribution/<wtid>.jsonl
{ts, sid, kind: "write"|"edit"|"bash-infer", path, confidence}
```

- **写入点在 Go dispatcher**（加入 `isInProcessHook` 家族，`internal/cli/hook.go:424-426`）：PostToolUse(Write|Edit) 直接记 `{kind, path=FORGE_FILE_PATH}`（env 注入已存在，`hook.go:804-849`）；PostToolUse(Bash) 解析命令文本的写目标（`sed -i`/`mv`/`cp`/`>`/`tee` 等），记 `confidence=low`。不做 bash hook——性能与可靠性都更差。
- **Stop 对账算法**：
  1. `git status --porcelain` → changed set；
  2. 每 path 取 ledger 中最近归属（按 ts，last-writer-wins）；
  3. 已提交段走既有 `taskattribution`（不变）；
  4. 剩余 = **无主变更（orphans）**：记录 + 在所有视图中诚实暴露，绝不静默吞掉。
- **View API**：`AttributionView(root, wtid) → {BySession map[sid]set[path], Orphans []path}`。

**消费者切换**（全部改读 View，工作树不再被裸读）：

| 消费者 | 现状 | 切换后 |
|---|---|---|
| `condSourceChanged`（conditions.go:38） | 全树扫描 | 本 session touched 集 ∩ changed ≠ ∅ 才命中 |
| HANDOFF `gitPorcelain`（task_continuity.go:1179） | 全树扫描 | 本任务归属过滤后的 status + "另有 N 个无主文件已排除"行 |
| review 指纹 `SourceChangesSince`（stamp.go:349-434） | 全树 diff | 归属过滤后的 diff |
| `taskChangedFiles` 未提交段（testcoverage.go:359-391） | 全树 | 归属过滤 |

`markers/forge-source-touched-<sid>` 布尔标记（embed.go:523）被 ledger 取代，保留一个发布周期后移除。

**度量**（内嵌 spike，G1 的可观测性）：归属覆盖率 = attributed/(attributed+orphans) 记入 checklog，`forge status` 展示——Bash 旁路写的真实覆盖率用它量化，决定 `bash-infer` 的去留（§16）。

### 为什么

GitButler 用 lifecycle hook + session id 做同构归属；STORM 证明 write-set 归属是共享工作区正确性的最小机制（其全中介前提不满足，取子集）。业界在"归属不完美"上一致：尽力而为 + 诚实暴露，没有银弹。

### 边界与降级

- **无 session-id 宿主**（kimi/codex/cursor 经 Bash）：ledger 无法写 → View 退化为全变更皆 orphan；`condSourceChanged` 行为回退现状，但输出降级标注"归属不可用"（不 nag coding 纪律，只提示环境限制）。
- **TTL/压缩**：ledger 条目 7d 滚动；janitor 重写 jsonl 时丢弃既不在当前 git status 也无活跃会话引用的段。
- **性能预算**：View 查询走顺序读 + 内存索引，Stop 钩子增量 ≤50ms（10k 条 ledger 的基准测试纳入 §13）。

## 7. L4 隔离层：worktree 生命周期

### 决策

**`forge task start --worktree [--base <ref>] [--wt-dir <parent>]`**，原子序列：

1. `git worktree add -b <branch> <path> <base>`——默认路径 `<repo 父目录>/<repo 名>-wt/<task-ref>`（repo 树外，遵守"项目树零写入"原则，`docs/design/project-sync.md` 的既有约束；可配 `forge config worktree.dir`）；
2. 在新 worktree cwd 下 `forge task start`（建任务）；
3. 写 workspace 绑定 + session 指针；
4. 复制 gitignored 必需文件：repo 根 `forge.worktreeinclude`（.gitignore 语法，对齐 Claude Code `.worktreeinclude`）匹配的 `.env` 等；
5. 输出指引：`cd <path> 重开窗口`（解析链第 2 环即接管）。

失败回滚：步骤 2 失败 → `git worktree remove --force` 清理；绑定已写后失败 → 保留现场并报告（宁留勿删）。

**`forge task finish [--merge-to <branch>] [--keep]`**：门禁全过（ReviewPassed + acceptance，以 checklog 证据为准）且工作树干净（有未提交则列出并拒绝）→ 合并（本地 ff/merge，或打印 PR 命令）→ worktree 清理 + 绑定删除 + specs 归档决策（§9）。

**Janitor**（`forge worktree janitor`，SessionStart 节流触发，1 次/天，复用 task-verify 的节流模式）：

- 清理：已完成/超期（默认 14d，可配）且**干净**的 worktree；脏的只报告——**免删除条款**：有未提交工作的 worktree 永不自动删（对齐 Claude Code 清扫器语义）；
- 顺带处理：L2 的 jsonl 滚动、L3 的 ledger 压缩、marker 清扫（现有 7d 逻辑收编）；
- `~/.forge/projects/` 孤儿目录（本机实测 1081 个）：无 registry 登记 + >90d → 只报告不自动删。

**同目录多任务**：不禁止。`task start` 检测到 cwd 绑定着其他活跃任务时输出建议（转 worktree 流程），正确性由 L3 撑——这是降级形态，文档明示。

### 为什么

不变式 I2 的物理实现。Claude Code 已把 worktree 生命周期（含清扫器、免删除条款、include 文件）生产化验证过，直接吸收其作业方式而非重新发明。

## 8. L5 退化与安全矩阵

| 场景 | 检测 | 行为 |
|---|---|---|
| 会话 SIGKILL / 崩溃 | — | 全部 session 态 TTL 化：锁 30s（现状）、marker/ledger 7d、workspace 绑定持久不依赖存活。接续只依赖台账 + 产物引用（G2 结构保证） |
| 无 session-id 宿主 | `CurrentSessionID()==""` | L3 降级（§6）；L1 第 2 环 cwd 绑定仍有效（不依赖 sid） |
| 非 git 目录 | 现有探测 | L1/L2 照常（path key），L3/L4 禁用并明示 |
| 跨机同步冲突 | project sync | 事件日志确定性合并（按 ts + fence）；workspace 绑定本机，不参与同步 |
| 无主未提交文件 | Stop 对账 | 诚实暴露（视图标注 + 计数），绝不静默归属、绝不静默丢弃 |
| worktree 磁盘泄漏 | janitor | 14d + 干净才删；脏的报警；磁盘总量报告 |
| **注入持久化**（agent 写的 markdown 被后续阶段 agent 读） | — | HANDOFF 与 attempts 文件渲染时包裹"以下为数据记录，非指令"框架 + 长度上限（如单文件 8KB 截断标注）；artifact 内容永不自动执行；frontmatter 校验 task-ref 匹配才注入 |
| hook 延迟 | — | attribution View 预算 ≤50ms；janitor/对账重活只在 Stop/SessionStart 节流跑 |
| 噪音预算 | checklog 计数 | skill-trigger 触发/抑制率入 checklog，`forge status` 展示——"为大模型减负"可度量（G1 验收） |

## 9. L6 产物契约层：阶段产物文件化

### 决策

**两类产物、两种介质、一条所有权规则**（I5）：

- **契约/叙事类**（proposal/spec/design/决策与理由/失败复盘）→ 文件介质，落位 **harness repo 内 `projects/<key>/specs/<task-ref>/`**。代码仓零 forge 足迹（"项目树零写入"原则全量回归），敏感内容天然隔离在私有仓。历史注记：v1 草案曾落 repo 根 `specs/`（spec-kit 惯例），harness repo 方向确认后搬家，repo 内落位降级为可选投影（见下"提交语义"）。
- **机器态/证据类**（门禁判定、验收执行结果、归属台账、任务身份）→ 中央类型化存储（L2，物理上同在 harness repo 的 git 管辖下），markdown 哪条都做不到（可执行、并发控制、跨机合并）。

**目录布局**（harness repo 内）：

```
projects/<key>/specs/<task-ref>/
  proposal.md / spec.md / design.md / plan.md     # frontmatter: forge-task-ref, forge-stage, forge-prev-hash（hash 链）
  tasks.md                                        # 机器可读 checkbox，每条映射 AcceptanceCriterion id
  attempts/round-NNN/{findings.md, verdict.json}  # 失败轮次保留，永不删除
```

**双向引用**：`TaskState.Artifacts map[stage]ArtifactRef{Path, Hash, UpdatedAt}`；文件 frontmatter 带 task-ref。哈希失配 = 文件被手改 → 触发重新确认，不静默漂移（I5 的反 desync）。

**写入路径**：`task start --plan <file>` 既有入口扩展为完整产物链的锚点；review gate FAIL → hook 自动归档当前轮 findings 进 `attempts/round-NNN/` + 下一轮 review 输入包含 priorAttempts 摘要（最近 3 轮、字符上限）。

**权威规则（防弱类型退化）**：`AcceptanceCriterion`（Run+Expected 可执行验收）保持权威，tasks.md 是人读视图 + 引用——**门禁永不退化为"勾完复选框"**（LoopSpec 式工具的通病，Forge 的既有强项）。

**声明式产物图与门禁语义（LoopSpec 深读吸收，2026-08 第二轮）**：

1. **产物链声明化为 DAG**：specs 链（proposal→spec→design→plan→tasks）从隐含顺序升格为声明式图——`schema.yaml` 风格（节点/依赖/gate/reset 目标/instruction 模板）落在 harness repo（`projects/<key>/schemas/`），配两阶段加载校验（结构/语义分开，错误码化，含环检测、reset 必须是 gate 祖先、输出路径安全相对、保留名排除）。流程改版改 YAML 不改代码。forge 三门禁（gates.go 硬编码）不在首期声明化，产物链先行。
2. **门禁五态 + exhausted 升级**：review gate 状态机从 pass/fail 扩为 blocked/ready/done/failed/**exhausted**——`MaxReviewRounds` 耗尽显式进入 exhausted（`escalate` 交人 / `stop`），终结自动循环而非静默继续。对齐 review convergence 的收敛纪律。
3. **判定歧义即错误**：checklog 中同一 gate 出现矛盾证据（同轮最新两条 verdict 相反）→ 报 `evidence_conflict`，绝不静默取最新（LoopSpec `gate_output_conflict` 同语义）。
4. **回滚闭包排除持久锚**：attempts 归档的 reset 闭包（reset 目标 + 门禁自身 + 传递依赖）**必须排除** TaskState、Artifacts 引用与 tasks 台账——意图跨轮存活（LoopSpec 的 state.md 保留语义）。
5. **nextSteps 单命令纪律**：gate/verify 语境下给 agent 的输出收敛为"唯一下一步命令"（LoopSpec status 的驱动纪律）；skill 触发列表等多建议输出不属于 gate 语境，维持现状。

**提交语义与"持久性恰好一次"规则**：

- **默认：持续提交进 harness repo**。specs 及全部过程产物在 Stop/任务边界批量 commit（提交策略见 §13 状态机部分与 §12-T6），finish 不再是持久性分水岭——worktree 随时可弃，产物早已入库。
- **跨仓相关性**：`task finish` 把代码仓 commit hash（finish 时 HEAD）记进 TaskState——两个 repo 的变更靠记录锚定，不靠原子合并。
- **可选投影**：想要 spec 随代码 PR 同审的团队，`forge config specs.projection=branch` → finish 时把 specs 派生镜像到代码分支（derived 非权威，I5 不破）。
- **持久性恰好一次（I5 推论）**：权威介质唯一 = harness repo；投影是派生物。hash 引用校验照旧（TaskState.Artifacts 失配即报漂移）。

### 为什么

Forge 从 HANDOFF.md/AI_CONTEXT.md 到结构化字段的升格（types.go:359）解决了抗压缩/跨工具，但丢了文件介质的四个红利：git 版本化与 diff、PR 审查性、跨工具零成本互操作、注入单位可控。本层把它们以受控形态找回；attempts 保留回灌直接服务 review convergence（失败上下文目前活在 transcript 里，压缩即丢）。

## 10. CLI 面与 hook 管线变更汇总

**新命令/旗标**：`task start --worktree [--base] [--wt-dir]`；`task finish [--merge-to] [--keep]`；`worktree janitor`（内部，SessionStart 节流）；`harness init [--from-existing] [--remote]` / `harness status`（§13）；`task archive`（harness repo 内冷存搬移，语义收窄为目录整理而非持久化补救）。

**行为变更**：`task start` 废除 Clear()（写边界事件）；`task resume` 解析链 v2 + HANDOFF 视图 v2（归属过滤 + 无主计数 + artifacts 状态）；`review gate`/`task gate` 证据查询带 task-ref；stamp 内容寻址。

**内部改造点**（约 10 处调用点）：`internal/cli/hook.go`（dispatcher 记 ledger + workspace heartbeat）、`internal/skilltrigger/conditions.go`、`internal/cli/task_continuity.go`、`internal/review/stamp.go`、`internal/taskpipeline/{testcoverage,state,executor}.go`、`internal/cli/task.go`、新增 `internal/{workspace,attribution}` 两包。

## 11. 数据模型与迁移

### 11.1 schema 变更

TaskState：+`BoundWorkspaces []string`（运行态绑定在 workspaces/ 侧，TaskState 只记 wtid 供反查）、+`Artifacts map[stage]ArtifactRef`；schema version bump，走既有 `forgedata/migrate.go` 机制。

### 11.2 历史数据迁移（存量 ~/.forge/projects）

原则：**无重写 + 一次指针转换 + 读兼容**，沿用 migrate.go 已验证的安全纪律（白名单、幂等、--dry-run、信任边界排序、跨设备 Rename 回退）：

| 存量数据 | 迁移策略 |
|---|---|
| `tasks/*.json`（本机实测 57 个） | **零迁移**：新增字段为 Go 零值即"无"，旧文件原样可读可写 |
| legacy 全局 `active-task-ref`（无 sid 后缀） | **一次性指针转换**：首个新版本 hook 首跑时转写为 main workspace 绑定（幂等：已转换则跳过） |
| 旧 `<branch>.stamp` | **读兼容**：保留只读，新写全部走 diff-hash 内容寻址 |
| 旧 checklog/toollog 条目（无 TaskRef） | **读兼容**：语义为全局条目，与 SessionID 为空的既有语义同构，不重写 |
| markers / attribution ledger | **不迁移**：短命态，TTL 自然过期 |
| 灰度开关 | `FORGE_ATTRIBUTION=0` 一键回退旧行为（默认开；行为无破坏性，开关是逃生门） |

关键依据：设计刻意让**所有 schema 变更都是加法**（新字段、新目录、新文件），不修改任何既有字段的语义——这是"无重写迁移"成立的前提，实现期任何破坏性 schema 变更都应视为设计违背。

### 11.3 跨机器迁移（project sync 扩展）

前提：`.forge-project-id` 键控 + export/import bundle + leases/fencing/datamerge 均已存在（`docs/design/project-sync.md`、`sync-convergence.md`）。本设计新增数据的同步分类：

| 数据类别 | 内容 | 同步策略 |
|---|---|---|
| **harness repo tracked（随 git 同步）** | tasks/、checklog、archive/、specs/ | **传输换代**：`git push/pull` 私有 remote（project-sync 的 export/import bundle 降级为兼容模式）；冲突处理不变（leases/fencing/datamerge）；L2 事件化后 checklog 按 ts 确定性合并 |
| **机器本地（gitignore + 同步排除）** | `workspaces/`、`attribution/`、`markers/`、`sessions/`、**`stamps/`、`hazards/`（信任锚）** | 显式排除清单。路径绑定/台账落他机是假状态（wtid 是路径哈希，落过去也只是死文件，但排除是纪律）；**stamps/hazard 确认是本地信任锚，永不外发**——有远端后 clone 下来的都是攻击者可控输入（migrate.go 2026-08-15 信任边界评审先例），跨机 review 走重验（行为级检测），不走出示印章 |

TaskState 字段级的跨机冲突：沿用 lease fencing + datamerge（现状）；事件化让 checklog 类合并从"last-writer-wins"升级为 ts 归并。`BoundWorkspaces` 含 A 机 wtid 到 B 机 → 解析链第 2 环按 B 机 cwd 计算 wtid 自然不命中，穿落到分支映射/显式 ref——by design，无害。

### 11.4 harness repo 目录结构全景

```
~/.forge/                                  # harness repo 根
├── .gitignore                             # 排除清单 = 下述 [本机] 项
│
│ # ── 用户级文件 ──────────────────────────────────────────────
├── projects.json            [本机] 全局注册表 key↔path（path 机器本地）
├── workspaces.json          [本机] 多仓分组清单（multi-repo-workspace.md §5 既定）
├── node.json / node-seq     [本机] 机器身份（node-identity.md）
├── harness-state            [本机] onboarding 状态机（§13）
├── skills-cache/            [本机] 嵌入 skills 安装缓存（随 forge 版本走）
│
└── projects/<key>/          # 每 repo 一份（key = common-dir hash / .forge-project-id IDKey）
    │
    │ # ── tracked：git 版本化 + 可随 remote 同步 ──────────────
    ├── tasks/<ref>.json             # 任务台账（goal/plan/decisions/gates/sessions/artifacts）
    ├── specs/<task-ref>/            # L6 产物链（proposal/spec/design/plan/tasks.md + attempts/round-NNN/）
    ├── archive/<task-ref>/          # 冷存归档
    ├── checklog.jsonl (+日期轮转)   # 门禁证据（事件日志，ts 确定性合并）
    │
    │ # ── 本机：gitignore + 永不外发 ──────────────────────────
    ├── toollog.jsonl                # Read 观测——量大、跨机价值低（证据/观测分离）
    ├── workspaces/<wtid>.json       # L1 worktree 绑定（路径哈希）
    ├── attribution/<wtid>.jsonl     # L3 归属台账
    ├── markers/                     # per-session 标记（7d TTL）
    ├── stamps/                      # review 印章——信任锚，clone 即攻击者可控输入
    ├── hazards/                     # hazard 确认——信任锚
    ├── sessions/<sid>.json、last-session.json      # 会话簿记
    ├── tasks/<ref>.lock、各 *.lock  # 文件锁（O_EXCL，30s 过期）
    ├── active-task-ref-<sid> 等 session 指针/哨兵/节流文件
    ├── quarantine/ freeze/ act/     # 运行时治理状态
    └── hooks/ + protocol.yml        # 部署副本（随安装版本走；治理规则随 clone 走即攻击面）
```

代码仓侧对照：forge 足迹为零（唯一例外 `.forge-project-id`，project-sync 既有）；`forge.worktreeinclude` 可选；`specs/` 仅投影模式（`specs.projection=branch`）下 finish 时派生镜像；worktree 默认落位 repo 树外 `../<repo>-wt/<task-ref>/`（§7）。

分类三条判据：**tracked = 过程状态与门禁证据**（跨机有价值、量可控、append 为主合并友好）；**本机 = 路径绑定类、短命哨兵类、信任锚类、部署副本类**；其中信任锚（stamps/hazards）与治理配置（hooks/protocol.yml）**即便本机也不入 git 跟踪**——它们决定门禁行为，绝不能来自 clone。toollog 与 checklog 分家的原因：前者是 Read 观测流（量大、跨机价值低），后者是门禁证据（量小、判定输入）——证据与观测分离。

## 12. 落地：单一 goal，多任务执行

**一个 goal 一个分支**：`feat/multi-task-concurrency` 覆盖全部六层，不按层分期合入。执行期拆为多个 forge 任务（dogfood——这个特性的开发本身就该用 forge 的多任务并发跑），任务间是**依赖顺序**而非分期边界：

| 执行序 | 任务 | 内容 | 依赖 |
|---|---|---|---|
| T1 | 状态层事件化 | 废 Clear、checklog 条目 TaskRef、stamp 内容寻址、派生态缓存化 | — |
| T2 | 归属服务 | `internal/attribution` + dispatcher 记账 + Stop 对账 + 覆盖率度量 | T1 |
| T3 | 消费者切换 | condSourceChanged / HANDOFF / review 指纹 / taskChangedFiles 四处切 View + golden 测试 | T2 |
| T4 | 身份层 | `internal/worktree` + 解析链 v2 + 指针一次性迁移 | T1 |
| T5 | worktree 生命周期 | `start --worktree` / `finish` / janitor + 多任务工作流文档 | T4 |
| T6 | harness repo | `harness init/status` + git 化 DataDir + 批量提交策略 + 信任分类（stamps/hazards 永不外发）+ 老用户 `--from-existing` 迁移 | T1 |
| T7 | 引导层 | onboarding 状态机 + 触发点矩阵 + HITL 确认（§13） | T6 |
| T8 | 产物契约层 | specs 链（落 harness repo）+ attempts 保留回灌 + 投影模式 | T4+T6 |
| T9 | 传输换代 | project-sync bundle → git remote（bundle 保兼容）+ 跨机排除清单 | T6 |
| T10 | 度量与收尾 | 噪音预算/覆盖率进 `forge status`、全量并发矩阵测试 | T3+T5+T7+T8+T9 |

T2 内嵌 spike：`bash-infer` 归属覆盖率先度量后启用。T3 完成即终结 P1/P2/P3 痛点（最痛的最早消除）；T5/T8 可与 T3 并行推进（T8 依赖 T6 先落）。

## 13. harness repo 建立引导层（onboarding · HITL）

面向个人开发者与企业团队的产品化要求：harness repo 不能是隐含假设——新用户、老版本升级用户、企业团队都需要被显式引导建立或授权建立。

### 分群与流

| 分群 | 检测 | 引导流 |
|---|---|---|
| 新用户 | 用户级 home 无任何 forge 状态 | 首个 forge 命令（建议挂 `project adopt`，天然时机）→ HITL 三选一：**forge 帮建本地私有仓**（推荐：零配置、无远端、无账号）/ **指向已有仓库** / **跳过**（纯文件模式 = 现状行为，后续随时可 init） |
| 老版本升级用户 | DataDir 有存量（tasks/checklog）+ harness repo 不存在 | `forge harness init --from-existing`：git init + 排除清单 .gitignore + 全量基线 commit（"史前史"一次性入库，不重写任何数据——§11.2 同原则）+ 版本 stamp；`--dry-run` 预览，幂等 |
| 企业团队 | init 时配 remote | remote 指内部 GitLab/Gitea；**凭据走用户自己的 git credential helper，forge 永不持有凭据**；首次 push 前展示数据出境清单（什么会同步、什么永不外发） |

### HITL 四原则

1. **批准必须来自终端里的人，agent 不得代批**（LoopSpec approval gate 同款；forge hazards 确认同语义）。落地：`harness init` 的交互确认仅在 TTY 存在时进行——agent 经 Bash 工具执行时无 TTY（`[ -t 0 ]` 检测），拒绝非交互 init 并输出人工指引；`--yes` 逃生口仅供脚本化 CI，agent 纪律禁止自行使用。
2. **外发动作独立确认**：配 remote、首次 push 是两次独立 HITL，各带数据出境清单。
3. **降级不阻断**：所有触点 advisory + cooldown（offered 状态每日至多提示一次），纯文件模式永远可用；hook 路径（非交互硬约束）只出 advisory 绝不弹交互。
4. **迁移幂等 + dry-run**：沿用 migrate.go 纪律（白名单/幂等/--dry-run/信任边界排序）。

### 触发点矩阵

| 触点 | 行为 |
|---|---|
| 版本升级首跑（版本 stamp 不匹配） | 一次性 advisory + 引导文案 |
| `forge project adopt` | 自然时机：adopt 后同会话建议 harness init（一次 HITL 完成） |
| `task start` 等 hook 路径 | advisory 提示（hook 非交互，绝不弹窗） |
| `harness push`/sync 无 repo | 明确报错 + 指引 |
| `forge status` / doctor | 常态显示 harness 状态行：未建立 / 本地 / 已连远端 |

### 状态机

`harness-state`（node 级）：`uninitialized → offered（含提示计数与 cooldown，防 nag 循环）→ initialized(local) → linked(remote)`。企业策略（protocol.yml `harness: required` + remote 白名单）进 v2。批量提交策略：Stop / 任务边界（start/finish/门禁判定）触发 `git add <tracked 集合> && commit`（复用现有锁模式串行化并发提交），janitor 兜底清扫漏网变更。

## 14. 测试策略

- **并发矩阵**（扩展 `concurrent_session_test.go`）：双会话 ×（同目录/双 worktree）×（A 编辑/B Stop/B resume）——断言 B 无触发、B 视图无 A 文件。
- **归属单测**：本 session 改 / 他 session 改 / 无主 / 已提交 / bash-infer 低置信 全组合。
- **崩溃注入**：gate 执行中 kill -9 → 重进断言状态从 checklog 重构等价（golden 对比）。
- **worktree E2E**：`start --worktree` → 编辑 → `finish` 合并清理全链路；中途失败回滚。
- **性能基准**：10k 条 ledger 下 View 查询 ≤50ms；Stop 钩子总增量 ≤200ms。
- **onboarding**：状态机迁移全路径；非交互（无 TTY）init 被拒并输出指引；老用户 `--from-existing` 迁移幂等（跑两遍结果一致）+ dry-run 不落盘；offered cooldown 防 nag（同日第二次触点静默）。
- **harness repo**：批量提交与并发任务写同一 repo 的串行化；排除清单生效（markers/stamps 不入 git）；git 缺失时的纯文件降级 = 现状行为。
- **注入面**：artifact 含指令样文本 → 渲染框架包裹 + 不执行（负向断言）。

## 15. 风险与缓解

| 风险 | 缓解 |
|---|---|
| Bash 旁路写归属覆盖率不明 | T2 先度量（coverage 入 checklog）再决定 bash-infer 去留；orphan 诚实暴露兜底 |
| "六层"复杂度回潮 | 实现收敛为两小包 + 10 调用点；解析链每环纯函数 + 穷举测试 |
| specs 的 repo 污染与团队阻力 | 命名空间收拢 + archive + dir 可配；specs 随分支走（merge 即清理） |
| 注入持久化 | §8 矩阵行（数据非指令框架 + 上限 + 不自动执行） |
| 存量用户迁移 | 无破坏性（视图语义变化仅新增过滤）；FORGE_ATTRIBUTION 逃生门 |
| harness repo 运维负担（提交策略/远端维护） | janitor 兜底提交；纯文件降级永远可用；默认本地无远端（想用时才引入运维） |
| 误推敏感内容到非私有远端 | 首配 remote 的 HITL 数据出境清单 + 默认无远端 + 信任锚永不外发（§11.3） |
| agent 诱导绕过 HITL（注入文本教 agent 跑 `--yes`） | 非 TTY 拒绝交互 init 的检测在 forge 侧（不信任调用方自报）；--yes 仅 CI 显式场景文档声明 |

## 16. 开放问题（调研结论 + 待验证项）

1. **Bash 旁路写归属覆盖率**——T2 先度量（coverage 入 checklog）再决定 bash-infer 去留；orphan 诚实暴露兜底。（业界一致承认此洞，STORM 原话"bash-based writes bypass mediation"。）

2. **语义级冲突预测**（深度调研完成，2026-08）——学界脉络：文本冲突 → 语义冲突（行为不一致）→ LLM 方法。Semex 用变体感知执行把并行改动编码进单一程序；SPGroup 用回归测试做行为级语义冲突检测；Microsoft Edge 实践报告 LLM 解冲突效果 mixed。最相关的是 **AgentSpawn（arXiv 2602.07072）**：冲突检测层识别交叠变更后，15% 自动合并 / 73% LLM 语义合并 / 其余上报父级。**落点是三级方案，行为级恰好是 forge 门禁架构的强项**：
   - 语法级：PlanScope 文件集交集告警（便宜，可随 T5 落）；
   - **行为级：merge 门禁重跑兄弟任务的 AcceptanceCriterion**——回归测试式语义冲突检测是学界验证过的最可靠信号，forge 的可执行验收就是现成探针；落点为 `task finish` 合并前的 cross-task verify 步骤；
   - LLM 级：交叠变更的语义合并兜底（AgentSpawn 73% 成功率作参考），只在行为级失败后调用。
   行为级与 LLM 级不进首个 goal，作为后续任务立项（依赖 T6 的验收结构成熟）。

3. **运行时隔离**（端口/DB/env）——方向定为**本地容器**（轻量、可审计），但属破坏性介入：**必须用户显式批准**——`forge config runtime.isolation=container` 显式开启，默认关。不进首个 goal。

4. **无身份宿主的会话推导**（调研完成，2026-08）——宿主 stdin payload 的 session_id 仍是第一优先，以下为推导回落阶梯（Unix 会话语义：session leader + 控制终端）：
   - **进程谱系**：沿 PPID 链找长命祖先（agent harness 进程），`hash(祖先 PID + 进程启动时间)` 作 sid——启动时间消 PID 重用；裸 PPID 不可用（孤儿重挂 init 后漂移）。粒度 = agent 会话，最准。
   - **SID+TTY**：`ps -o sid=` + 控制终端设备号——粒度 = 终端窗口（同窗先后两个会话会撞车；无 TTY 即 CI/非交互场景，可检测并降级）。作最后回落。
   - 推导来源标注（`derived:psid` / `derived:tty`）写入 ledger，命中率并入 T2 覆盖率度量；Windows 进程谱系获取困难，明示降级。
   实现排 T2/T4 之间，先在无身份宿主上做命中率 spike 再决定启用层级。

---

**与既有设计的关系**：身份 key 与"一 repo 一 key"契约见 `project-sync.md`（不变，本设计在其上加 workspace 维度）；跨机租约/合并见 `sync-convergence.md`（L2 事件化强化其确定性基础）；node 身份见 `node-identity.md`（workspace.Node 复用）。

## 17. 落地记录（2026-08-27，feat/multi-task-concurrency）

T1–T10 全部落地，单一 goal 单一分支，`go test ./...` 全绿，dogfood 任务 task-implement / task-verify 门禁通过：

| 任务 | 落点 |
|---|---|
| T1 状态层 | checklog CheckTaskStarted 边界事件 + task start 废 Clear（非破坏性 Prune 保留 retention）+ stamp dh-<hash> 内容寻址（分支路径只读兼容）+ 证据分桶排除 |
| T2 归属服务 | internal/attribution（台账/Reconcile/SessionTouched/bash-infer 保守提取/Enabled 逃生舱）+ 分发器记账 + Stop 覆盖率观察条目（10min 节流） |
| T3 消费者切换 | condSourceChanged 会话归属、attributedPorcelain（HANDOFF 现场）、SourceChangesSinceExcluded + TaskFingerprint（记录/重算同源）、taskChangedFiles 外来过滤（无主保留） |
| T4 身份层 | internal/worktree 绑定存储（BindTask/Load/Clear/Touch）+ 解析链 v2（session→绑定→分支守卫→legacy 桥）+ P5 跨会话守卫 + dispatcher 心跳 |
| T5 worktree 生命周期 | task start --worktree（repo 树外、宁留勿删）/ task finish（免删除条款）/ worktree janitor + forge.worktreeinclude |
| T6 harness repo | harness init（TTY HITL、--yes 仅 CI、--from-existing 基线）/ status / commit（任务边界批量）+ 信任分类 gitignore（stamps/hazards 永不入库） |
| T7 引导层 | harness-state 状态机（offered 计数/24h cooldown/上限 3 次）+ init/status 触发点 |
| T8 产物契约层 | WriteArtifact/VerifyArtifact（I5 引用三角）+ SpecArtifacts 字段 + --plan-file 落产物 + ArchiveAttempt 一次写入 + PriorAttemptsSummary 回灌进 HANDOFF |
| T9 传输换代 | harness push（首推出境清单 HITL）/ pull（冲突人工裁决）；bundle 通道保留为兼容模式 |
| T10 度量收尾 | forge status 归属覆盖行 + TestMultiTaskConcurrency_Matrix 五断言总验收 |

**v1 未含（后续任务）**：specs 投影模式（`specs.projection=branch`——持久性恰好一次已由 harness repo 满足，投影是可选 PR 同审需求）；声明式 schema.yaml 的用户自定义加载（T8 落的是内置产物链 + 哈希引用，加载校验框架待真实自定义需求出现）；janitor 的 SessionStart 节流自动触发（当前为手动 `forge worktree janitor`）；无身份宿主的进程谱系/SID 推导（§16-4 spike 待实测命中率）；行为级语义冲突检测（§16-2 三级方案的后两级）。

## 18. Dogfood 实录（2026-08-27，v1.45.0 本机全流程）

按设计设想在真实机器走完 T7→T6→L4→L6 全链，实测记录与发现：

**通过项**：T7 引导（status 双行 + cooldown 提示）；HITL 门拒绝 agent 调用并给人工指引；harness init 存量基线 401 文件、stamps/backups 信任边界 check-ignore 验证通过；worktree 三件套（目录/绑定/specs 哈希引用）；解析链 v2 无身份从 worktree cwd 命中绑定（"退出重进"锚成立）；HANDOFF 现场渲染。

**发现并当场修复**：
1. gitignore 缺口——backups（5.9M）/research/skills-backup/evals 不在排除清单，基线会灌入机器本地 store，有 trust.json 的机器泄漏信任锚。改根级允许清单（只跟踪 projects/，新顶层 store 默认 fail-closed 排除）。
2. --worktree ref 派生——ref 合法性 ≠ 分支合法性，非惯例前缀 ref（dogfood/walkthrough）被误拒。改为非前缀 ref 派生 feat/<ref 去斜杠>。

**诚实降级观察**：worktree 内的归属台账需宿主 hook 从该 cwd 触发才记账（单会话演示不可见跨窗口归属，符合设计预期——归属发生在真实多窗口场景）。
