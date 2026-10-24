# task-tree-guard：任务树的错树守护（P1）与 human 档链定义指纹（B）

状态：已实施（代码在 `fix/task-tree-guard` 分支，**尚未发布**——当前最新 tag 为 v1.84.2；本文档描述的是该分支合入后的行为）。前置：`multi-task-concurrency.md` §4 的 L1 身份层与 L4 worktree 隔离；`review-gate-session-scope-2026-10.md` 的会话作用域门禁（v1.84.2）。

## 0. 背景：`--worktree` 目前只是一条建议

`forge task start --worktree` 会在 repo 树外建独立 worktree、切分支、写绑定（`internal/clitask/task_worktree.go:79` 建树；`internal/clitask/task.go:768` 写 `SetActiveTaskRef`、`task.go:775` 写 `BindTask`），但**没有任何机制要求后续编辑真的发生在那棵树里**。在装了 v1.84.2 的真机上复现（临时 repo，`FORGE_SESSION_ID=demo-1`）：

| 步骤 | 观察 |
|---|---|
| `forge task start --worktree --ref feat/wt-lane …` | 建出 `<repo>-wt/feat-wt-lane`，`git worktree list` 两行 |
| 回到主检出执行 `forge task status` | 仍解析出 `Task: feat/wt-lane`（当前分支其实是 `main`），无任何提示 |
| 在主检出改 `a.go`、跑 `forge task gate task-implement` | **passed** |

根因两条，都在解析层（行号按 v1.84.2 基线；新增代码已把该结构后移）：

1. `internal/taskpipeline/state.go`：优先级 1（`ActiveTaskStateWithPath` 内）只读 `active-task-ref-<sid>`，读到 ref 且任务未完成就返回，**不与 cwd 的 wtid 比对**；按目录键控的说法（优先级 1.5 的 `worktree.Load`）反而排在它后面。
2. `active-task-ref-<sid>` 落在**共享 DataDir**（`activeTaskRefPath`），而 `task start --worktree` 是在 **worktree 根**上写它（`internal/clitask/task.go` 的 `SetActiveTaskRef` 调用点）——于是主检出也读得到同一枚指针。

后果链：`task-guard` 判定「有活跃任务」放行源码编辑 → commit 落在主检出当前分支 → worktree 恒空 → `forge worktree finish` 合并空分支或 no-op。`binding.Path` 在本守护之前只有两处消费——`forge worktree finish` 的合并/清理与 `forge worktree janitor` 的孤儿锚清理——且两处都只把它当路径用，没有任何「当前树是否就是锚树」的判定（本批新增的 `TaskTreeStatus`、`forge worktree use`、`forge task status` 才开始比对 `a.ID` 与当前 wtid）。

## 1. 目标与非目标

### 1.1 目标

| 目标 | 验收条件 |
|---|---|
| G1 错树写源码被拦 | 任务有活 binding 锚在另一棵树、当前 cwd 不是锚 → 源码写入被 task-guard 拒绝（exit 1，指名锚路径与三条正当出路） |
| G2 三个门禁同判据 | task-implement / task-verify / task-complete 在错树状态下 BLOCK，`forge task status` 明确展示状态 |
| G3 正当事先被提示 | 错树时 hookdispatch 注入 `FORGE_TASK_WORKTREE=<锚路径>`，拒绝文案据此给出三条出路；死锚在**编辑类调用时**打 advisory（带 `file_path` 的调用或 patch 类工具才判定，Read/Bash 不重复；同一会话+任务+锚只提示一次——一次编辑会经过 freeze-guard/task-guard/assertion-check/read-before-edit 四个 hook；不改 cwd，不代跑） |
| G4 放行面不扩散 | 死锚（目录已删）、多锚任一命中、测试文件、无任务、`binding` 未启用 → 一律不阻断；死锚打 stderr advisory，其余四种静默 |

### 1.2 非目标

- **不自动 `chdir`/不代跑命令**：工具边界不是 shell 边界，替用户切目录会制造更隐蔽的错位。
- **不改 `--worktree` 的创建语义**：仍建在 `<repo 父>/<repo>-wt/<sanitized branch>`。
- **不比对分支名**：分支只是绑定的属性，判定依据是路径（分支名可比性差、且 rebase/重命名会误判）。
- **不做跨机**：绑定含绝对路径，天然本机性（与 `multi-task-concurrency.md` §4 一致）。

## 2. 判据：任务树状态

单一函数 `taskpipeline.TaskTreeStatus(root, taskRef string) TreeStatus{State, AnchorPath, Wtid}`，四态：

| State | 条件 | 执法 |
|---|---|---|
| `ok` | 无活跃任务；或活跃任务没有任何 binding | 放行 |
| `ok-anchored` | 当前 wtid 命中任一个活锚（单锚与多锚同判——多锚并发是允许形态） | 放行 |
| `off-tree` | 任务有 ≥1 个**活**锚，且当前 wtid 不在其中 | **阻断**源码写入与三个门禁 |
| `dead-anchor` | 任务有锚，但**所有**锚目录都不存在（`os.Stat` 失败），`AnchorPath` 指向第一个死锚 | 放行 + advisory「锚已失效，`forge worktree janitor` 可清理」 |

`wtid` 复用 `worktree.ID(root)`（`internal/worktree/wtid.go:24`，EvalSymlinks 后 hash12），与 binding 文件名同源——因此判定是「当前树 != 锚树」，不含任何分支或路径前缀推断。

**为什么看 binding 而不是 active-task-ref**：ref 落在共享 DataDir、按 sid 键控，天然可被任意树读到（这正是漏洞）；binding 按树键控（`workspaces/<wtid>.json`），是唯一记录「任务被锚在哪棵树」的持久事实。ref 仍是**任务归属**的解析入口，binding 只回答**位置**。

## 3. 数据来源

新增 `internal/worktree/binding.go: Anchors(root, taskRef) []Binding`：遍历 `bindingDir(root)/*.json`，取 `TaskRef == taskRef` 且 `Path != ""` 的项，按 `CreatedAt` 排序。读失败的文件跳过（坏 JSON 不应让门禁崩）。现有 `Load`/`BindTask`/`Clear*` 不变。

新增 `forge worktree use`（`internal/clitask/task_worktree.go`）：把当前树的绑定指向该任务（`ClearByID` 掉其他树的锚 + `BindTask(currentRoot)`），这是「我就是要在这棵树干活」的正当出口；无 `--ref` 时用当前解析出的任务。

## 4. 四层执法

| 层 | 位置 | 行为 |
|---|---|---|
| 写入 | `internal/hooks/embed_task.go`（task-guard 脚本） | 读 hookdispatch 注入的 `FORGE_TASK_OFFTREE=<anchor>`：命中即 `FAIL [task-guard] …（exit 1）`；`.forge/**`、测试文件、无任务分支保持原逻辑。hookdispatch 同时落一条 `tree-guard` 行（按会话+任务节流） |
| 门禁 | `internal/clitask/task_gate.go:runTaskGate` | `off-tree` → `ExecuteTaskGate` 前直接 BLOCK，reason 含锚路径与 `forge worktree use` 提示，并落一条 `tree-guard` 行（每次 BLOCK 一条） |
| 展示 | `forge task status` | 状态表增一行 `Tree: off-tree（锚：<path>——…）`；`dead-anchor` 单独一行 |
| 提示 | `internal/hookdispatch/hook.go`（tool hook 前） | 注入 `FORGE_TASK_OFFTREE` + `FORGE_TASK_WORKTREE=<anchor>`（仅编辑类调用：带 `file_path`，或 patch 工具即使路径合成失败也判）；死锚按会话+任务+锚节流一次 |

guard 脚本不自己 spawn `forge`（避免每次编辑多一次进程）；判定由 hookdispatch 在已有解析点算好落到环境变量。手工 `bash` 里没有该变量时脚本行为与今天一致。

## 5. 逃生舱与审计

判定的留痕分两个 check 名，都是 deterministic（判定由 forge 自算，agent 无法伪造）：

- **`tree-guard`**（`checklog.CheckTreeGuard`）：一次错树判定。`Passed=false`、`Level=warn`，`Meta` 带
  `tree_guard=off-tree`、`anchor`、`wtid`、`resolve_path=active-file-offtree`。写入层由 hookdispatch 在**每次
  编辑类调用的解析点**记一条 `layer=task-guard`（`Write|Edit` 等带 `file_path` 的 tool 事件；patch 类工具即使路径合成失败也计入）（早于 guard 脚本的文件类型判定，所以改测试文件也会落一行——
  它记的是「这次会话此刻站在错树」，不是「这次编辑被拒」），按会话+任务节流——guard 每次编辑都跑，不节流就是日志洪水；
  门禁层由 `runTaskGate` 每次 BLOCK 记一条。
- **`escape-hatch`**（`checklog.CheckEscapeHatch`）：`FORGE_TASK_TREE_GUARD=0` 时记一行，
  `escape.gate=task-tree-guard`、`escape.reason=env`、`escape.owner=env`，按会话+任务至多一条。
  评分侧按既有逃生舱规则把任务记 `capped`——判定在 `internal/taskpipeline/scoring.go:266-278`
  （`usedAnyOverride`/`taskEscapeHatchRecorded` 定义在 `internal/taskpipeline/overrides.go:49`/`:58`；两者任一为真且超出 `escapeCapMaxScore` 即封顶并写 `CappedReason`），
  `internal/clitask/task_complete.go` 的逃生舱库存打印只是把同一台账行展示出来；证据强度侧另有
  `internal/checklog/evidence.go` 的 `EscapeDowngradedStrength` 降档——
  所以「关门禁会被扣分」是真的，不是文案。两层都记：关闭后 guard 脚本整个跳过判定，只有 hookdispatch
  那条能让「只编辑、不推门禁」的会话留下痕迹。
- 死锚 advisory **不写 checklog**（按会话+任务+锚节流一次，避免四个 hook 各打一遍）：只出现在 stderr 与 `forge task status` 展示里。

## 6. B：human 档审批绑定链定义

现状：审批（`ArtifactApproval`）只钉产物文件内容 hash，**没钉链定义（`schema.yaml`）**——链定义一改，旧审批仍然成立（LoopSpec 在同样情形抛 `plan_changed`，`workflow_plans.py:312-322`）。

实现（尚未发布：`fix/task-tree-guard` 分支）：

- `artifactchain.Digest(root)`：`sha256(schema.yaml 内容，CRLF→LF 规范化)`，读不到返回空串（不阻断）。
- `ArtifactApproval` 增 `ChainDigest string`（`json:"chain_digest,omitempty"`，存量审批解出空串=不判定）。
- 写：`internal/clitask/task_chain.go` 的 `runArtifactApprove`（`forge task artifact --approve`）落审批时写入当前 digest。
- 判：`taskpipeline.ChainDigestStale(root, state)` 返回 stale 列表；`runArtifactVerify`（`forge task artifact --verify`）与 `runTaskGate` 都把它打成 advisory `⚠️ [chain] <阶段>: 审批时链定义 … ≠ 当前 …（重批：forge task artifact --approve <阶段>）`。
- **不 BLOCK**：改 schema 不该把所有在途任务锁死（与 `multi-task-concurrency.md` §8 退化矩阵一致）。
- 没有独立的 `forge artifact status` 子命令：stale 状态由 `--verify` 与 gate 的 advisory 暴露（审查曾按旧草稿找 `RecordApproval`/`VerifyArtifact`/`forge artifact status` 三个不存在的名字，这里按实际 API 更正）。

## 7. 判据（可测）

| # | 判据 | 测试 |
|---|---|---|
| 1 | 活锚在别树 → `off-tree`（并指出锚路径） | `TestTaskTreeStatus_States` |
| 2 | 当前树是锚 → `ok-anchored` | `TestTaskTreeStatus_States` |
| 3 | 锚目录已删 → `dead-anchor` 且**不阻断**（断言状态与 AnchorPath，不只看状态名） | `TestTaskTreeStatus_States` |
| 4 | 多锚任一命中即放行 | `TestTaskTreeStatus_States`（多锚段：两棵树都有活锚时任一命中放行） |
| 5 | 错树时门禁 BLOCK，且拒绝文案给出 `cd <锚>`、`forge worktree use`、`FORGE_TASK_TREE_GUARD=0` 三条出路 | `TestTaskGate_OffTreeBlocks`（三条文案均断言） |
| 6 | 错树时 guard 拒绝源码写入、放行测试文件 | `TestTaskGuardHookOffTreeDenies` |
| 7 | `forge worktree use` 迁移锚后放行 | `TestWorktreeUse_Rebinds` |
| 8 | 链定义变化使审批 stale；行尾差异不算变化 | `TestChainDigestStale`、`TestDigest_StableAndSensitive` |
| 9 | 关闭开关是显式 env（`=0` 才关） | `TestTreeGuardDisabled_Env` |
| 10 | 锚数据源只返回本任务锚、坏绑定文件跳过 | `TestAnchors_ListsOnlyMatchingTask`、`TestAnchors_SkipsCorruptBinding` |
| 11 | 错树 BLOCK 与关守护都留 deterministic checklog 行（含 `tree-guard`/`escape-hatch` 载荷），且写入层按会话+任务节流 | `TestTaskGate_OffTreeRecordsChecklog`、`TestRecordTreeGuardBlockThrottled`、`TestRecordTreeGuardEscapesAreThrottled` |
| 12 | 错树判定只发生在编辑类调用（Read/Bash 不重复判定与告警）；patch 工具路径合成失败（`FILE_PATH` 为空）时**仍然阻断**——guard 脚本的错树分支先于空路径早退 | `TestTreeGuardApplies`（谓词）、`TestTaskGuardHookOffTreeDenies`（空路径 exit 1） |
| 13 | 死锚告警按会话+任务+锚节流（四个 hook 只打一条），换死锚可再提示 | `TestAnnounceDeadAnchorThrottled` |

验收命令（`forge task start --accept`）：

```bash
go test ./internal/worktree ./internal/taskpipeline ./internal/clitask ./internal/cli \
  -run 'TestAnchors|TestTaskTreeStatus|TestTaskGate|TestWorktreeUse|TestChainDigest' -count=1
```

实测：`ok internal/worktree 1.7s`、`ok internal/taskpipeline 1.0s`、`ok internal/clitask 18.4s`；`internal/cli` 在该 `-run` 下无匹配用例（该包不承载本特性行为）。

这条命令有一处「包在、正则未匹配」：判据 9 的 `TestTreeGuardDisabled_Env`（`./internal/taskpipeline` 已在命令里，但 `-run` 正则不含它）。另有一批判据落在**未列入命令的包**里：`./internal/hooks`（判据 6）、`./internal/hookdispatch`（判据 11 的 `TestRecordTreeGuard*`、判据 12 的 `TestTreeGuardApplies`、判据 13）、`./internal/artifactchain`（判据 8 的 `TestDigest_StableAndSensitive`）、`./internal/tasktypes`（判据 8 的链指纹 JSON 往返）、`./internal/compat`。这些包合入前逐包实跑过；CI 跑的是全量 `go test ./...`，因此也在覆盖范围内。

## 8. 风险与回滚

- **误判风险**：用户在锚树之外合法地读/跑测试 → 不拦（只拦源码写入与门禁）。若某类工作确需在主树写源码，出口是 `forge worktree use`（显式迁移）或 `FORGE_TASK_TREE_GUARD=0`（留审计、评分 capped）。
- **性能**：`Anchors` 每个 `workspaces/*.json` 一次读，典型 <5 个文件；hookdispatch 在编辑类调用上各一次（`TaskTreeStatus` 只 stat 锚目录）。写入层不新增 `forge` 进程——判定在已有的 hook dispatch 里完成，guard 脚本只读环境变量。
- **回滚**：本批在分支上是六个提交（`b90c8d0e` 功能、`b01defaa` 补测、`99f78efb` 留痕与文案校准、`e50701ec` checklog/compat 配对测试、`9eff25d7` 作用域收窄、`3e18234c` patch 工具覆盖），squash 合并后是一次 revert；若在分支上单独回退，须一并回退测试提交，否则留下编译不过的测试。
- **关开关 ≠ 回到 v1.84.2**：`FORGE_TASK_TREE_GUARD=0` 只关「阻断」，不关观测——`forge task status` 仍会打 `Tree: off-tree` 行、hookdispatch 仍写一条 `escape-hatch` 行、评分仍按逃生舱 capped。真正回到旧行为要把代码回退，开关只买「这次不阻断、但要付审计与分数」。

## 9. 改动面

源码：`internal/worktree/binding.go`（`Anchors`）、`internal/taskpipeline/state.go`（`TreeStatus`/`TaskTreeStatus`/`TreeGuardDisabled`）、`internal/taskpipeline/artifactchain_gate.go`（`ChainDigestStale`）、`internal/clitask/task_gate.go`（错树 BLOCK + 两条留痕 + chain advisory）、`internal/clitask/task_worktree.go`（`worktree use`）、`internal/clitask/task_misc.go`（status 展示）、`internal/clitask/task_chain.go`（审批写 ChainDigest + verify 展示）、`internal/hookdispatch/hook.go`（`FORGE_TASK_OFFTREE`/`FORGE_TASK_WORKTREE` 注入 + 死锚告警 + 两层留痕接线）、`internal/hookdispatch/hook_tree_guard_record.go`（`tree-guard`/`escape-hatch` 行构造与节流）、`internal/hooks/embed_task.go`（guard 分支）、`internal/checklog/types.go` + `internal/checklog/escape.go`（新增 `CheckTreeGuard` 并登记 roster）、`internal/compat/compat.go`（`FORGE_TASK_TREE_GUARD` 入 `EscapeEnvs`）、`internal/artifactchain/artifactchain.go`（`Digest`）、`internal/tasktypes/model.go`（`ChainDigest` 字段）。

测试：`internal/worktree/binding_anchors_test.go`、`internal/taskpipeline/state_tree_test.go`、`internal/taskpipeline/chain_digest_test.go`、`internal/clitask/task_tree_guard_test.go`、`internal/hooks/taskguard_offtree_test.go`、`internal/hookdispatch/hook_task_tree_env_test.go`、`internal/hookdispatch/hook_tree_guard_record_test.go`、`internal/artifactchain/digest_test.go`、`internal/tasktypes/chain_digest_test.go`、`internal/checklog/escape_roster_test.go`（`tree-guard` 进 roster 且只作观察证据）、`internal/compat/escape_envs_test.go`（逃生 env 入册）。

其他：`.gitignore`（`.tmp/`、`.tmp_*`——两个只读子 agent 的临时脚本曾因未忽略而被 review gate 当 orphan 计入）。

文档：`docs/reference/cli.md`（`forge worktree use`）、`docs/design/compat-commitments.md`（BLOCKED 位点与逃生舱裁决记录）、本文件。`compat.snapshot.json` 已 re-pin（新增 `forge worktree use`、`tree-guard` 检查名、`FORGE_TASK_TREE_GUARD` 逃生舱，纯 append）。`CHANGELOG` 由 release-please 代管（不手写）。

## 10. 已知限制（P1 之后另开）

- `forge worktree finish` 仍只按 `binding.Path` 合并；错树期间的保护已前置到写入与门禁，`finish` 本身不做二次比对。
- 死锚的清理仍要显式 `forge worktree janitor`（守护只告警不删除）。
- 本守护不理解「同一任务被两个人分派到两棵树」的协作语义：多锚即放行（宁可漏拦也不误拦）。
- 覆盖面只到**文件编辑工具**（`Write`/`Edit`/patch 类）。Bash 里用重定向或脚本写源码（`echo > internal/a.go`）不经过 task-guard，`bash-guard` 也不读 `FORGE_TASK_OFFTREE`——这是与引入守护前一致的既有边界，不是本次收窄造成的；要覆盖 Bash 写入需另起一条判定（风险是误拦正当的构建产物写入）。
- `apply_patch` 的多文件 patch 只按第一个文件头判定（2a 的既有局限）；解析不出路径时已保证**阻断**（错树分支先于空路径早退），但多文件 patch 里首个目标以外的文件不单独判。
