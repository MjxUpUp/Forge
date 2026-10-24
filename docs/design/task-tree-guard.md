# task-tree-guard：任务树的错树守护（P1）与 human 档链定义指纹（B）

状态：已实施（v1.84.3）。前置：`multi-task-concurrency.md` §4 的 L1 身份层与 L4 worktree 隔离；`review-gate-session-scope-2026-10.md` 的会话作用域门禁（v1.84.2）。

## 0. 背景：`--worktree` 目前只是一条建议

`forge task start --worktree` 会在 repo 树外建独立 worktree、切分支、写绑定（`internal/clitask/task_worktree.go:79`、`task.go:363-371`），但**没有任何机制要求后续编辑真的发生在那棵树里**。在装了 v1.84.2 的真机上复现（临时 repo，`FORGE_SESSION_ID=demo-1`）：

| 步骤 | 观察 |
|---|---|
| `forge task start --worktree --ref feat/wt-lane …` | 建出 `<repo>-wt/feat-wt-lane`，`git worktree list` 两行 |
| 回到主检出执行 `forge task status` | 仍解析出 `Task: feat/wt-lane`（当前分支其实是 `main`），无任何提示 |
| 在主检出改 `a.go`、跑 `forge task gate task-implement` | **passed** |

根因两条，都在解析层：

1. `internal/taskpipeline/state.go:148-155`：优先级 1 只读 `active-task-ref-<sid>`，读到 ref 且任务未完成就返回，**不与 cwd 的 wtid 比对**；按目录键控的说法（优先级 1.5 的 `worktree.Load`）反而排在它后面。
2. `active-task-ref-<sid>` 落在**共享 DataDir**（`state.go:273-280`），而 `task start --worktree` 是在 **worktree 根**上写它（`task.go:770-773`）——于是主检出也读得到同一枚指针。

后果链：`task-guard` 判定「有活跃任务」放行源码编辑 → commit 落在主检出当前分支 → worktree 恒空 → `forge worktree finish` 合并空分支或 no-op。全仓库只有 `internal/clitask/task_worktree.go:203` 一处消费 `binding.Path`（finish 的合并/清理），没有任何一致性检查。

## 1. 目标与非目标

### 1.1 目标

| 目标 | 验收条件 |
|---|---|
| G1 错树写源码被拦 | 任务有活 binding 锚在另一棵树、当前 cwd 不是锚 → 源码写入被 task-guard 拒绝（exit 1，指名锚路径与三条正当出路） |
| G2 三个门禁同判据 | task-implement / task-verify / task-complete 在错树状态下 BLOCK，`forge task status` 明确展示状态 |
| G3 正当事先被提示 | hook dispatch 在错树时每轮注入一行 `FORGE_TASK_WORKTREE` 提示（不改 cwd，不代跑） |
| G4 放行面不扩散 | 死锚（目录已删）、多锚任一命中、测试文件、无任务、`binding` 未启用 → 一律不阻断，且写 advisory 可追溯 |

### 1.2 非目标

- **不自动 `chdir`/不代跑命令**：工具边界不是 shell 边界，替用户切目录会制造更隐蔽的错位。
- **不改 `--worktree` 的创建语义**：仍建在 `<repo 父>/<repo>-wt/<sanitized branch>`。
- **不比对分支名**：分支只是绑定的属性，判定依据是路径（分支名可比性差、且 rebase/重命名会误判）。
- **不做跨机**：绑定含绝对路径，天然本机性（与 `multi-task-concurrency.md` §4 一致）。

## 2. 判据：任务树状态

单一函数 `taskpipeline.TaskTreeStatus(root, sessionID)` → `TreeStatus{State, AnchorPath}`，四态：

| State | 条件 | 执法 |
|---|---|---|
| `ok` | 无活跃任务；或活跃任务无 binding；或当前 wtid 命中锚 | 放行 |
| `ok-anchored` | 任务有多个活锚，当前命其中任一 | 放行（多锚并发是允许形态） |
| `off-tree` | 任务有 ≥1 个**活**锚，且当前 wtid 不在其中 | **阻断**源码写入与三个门禁 |
| `dead-anchor` | 任务有锚但锚目录不存在（`os.Stat` 失败） | 放行 + advisory「锚已失效，`forge worktree janitor` 可清理」 |

`wtid` 复用 `worktree.ID(root)`（`internal/worktree/wtid.go:24`，EvalSymlinks 后 hash12），与 binding 文件名同源——因此判定是「当前树 != 锚树」，不含任何分支或路径前缀推断。

**为什么看 binding 而不是 active-task-ref**：ref 落在共享 DataDir、按 sid 键控，天然可被任意树读到（这正是漏洞）；binding 按树键控（`workspaces/<wtid>.json`），是唯一记录「任务被锚在哪棵树」的持久事实。ref 仍是**任务归属**的解析入口，binding 只回答**位置**。

## 3. 数据来源

新增 `internal/worktree/binding.go: Anchors(root, taskRef) []Binding`：遍历 `bindingDir(root)/*.json`，取 `TaskRef == taskRef` 且 `Path != ""` 的项，按 `CreatedAt` 排序。读失败的文件跳过（坏 JSON 不应让门禁崩）。现有 `Load`/`BindTask`/`Clear*` 不变。

新增 `forge worktree use`（`internal/clitask/task_worktree.go`）：把当前树的绑定指向该任务（`ClearByID` 掉其他树的锚 + `BindTask(currentRoot)`），这是「我就是要在这棵树干活」的正当出口；无 `--ref` 时用当前解析出的任务。

## 4. 四层执法

| 层 | 位置 | 行为 |
|---|---|---|
| 写入 | `internal/hooks/embed_task.go`（task-guard 脚本） | 新增 `FORGE_TASK_OFFTREE=<anchor>` 环境变量判定（由 hookdispatch 注入）：命中即 `BLOCKED …（exit 1）`；`.forge/**`、测试文件、无任务分支保持原逻辑 |
| 门禁 | `internal/clitask/task_gate.go:runTaskGate` | `off-tree` → `ExecuteTaskGate` 前直接 BLOCK，reason 含锚路径与 `forge worktree use` 提示 |
| 展示 | `forge task status` | 状态表增一行 `Tree: off-tree（锚：<path>）` |
| 提示 | `internal/hookdispatch/hook.go`（tool hook 前） | 注入 `FORGE_TASK_WORKTREE=<anchor>` + 每轮一次 advisory |

guard 脚本不自己 spawn `forge`（避免每次编辑多一次进程）；判定由 hookdispatch 在已有解析点算好落到环境变量。手工 `bash` 里没有该变量时脚本行为与今天一致。

## 5. 逃生舱与审计

- `FORGE_TASK_TREE_GUARD=0`：关闭本守护，checklog 记 `escape-hatch: task-tree-guard disabled`，任务评分 `capped`（复用既有逃生舱语义，不新增机制）。
- 每次 `off-tree` 命中写 checklog：`{tree_guard: off-tree, anchor: <path>, wtid: <id>, resolve_path: active-file}`。
- advisory 不写 checklog（每轮注入，避免噪声）；只写 `forge task status` 展示与门禁 reason。

## 6. B：human 档审批绑定链定义

现状：`artifact.ArtifactApprovals` 只钉产物文件内容 hash（`VerifyArtifact` 漂移失效），**没钉链定义（`schema.yaml`）**——链定义一改，旧审批仍然成立（LoopSpec 在同样情形抛 `plan_changed`，`workflow_plans.py:312-322`）。

做法：`ArtifactApproval` 增 `ChainDigest string`；`RecordApproval` 时算 `sha256(schema.yaml 规范化内容)`；`VerifyArtifact` 增加第二个判据「链定义变 → 该阶段审批 stale」；`forge task gate` 命中 stale 时给 advisory（不直接 BLOCK，避免改 schema 就把所有在途任务打死——与 `multi-task-concurrency.md` §8 的退化矩阵一致）。`forge artifact status` 展示 `chain: ok|stale`。

## 7. 判据（可测）

| # | 判据 | 测试 |
|---|---|---|
| 1 | 活锚在别树 → `off-tree`（并指出锚路径） | `TestTaskTreeStatus_States` |
| 2 | 当前树是锚 → `ok-anchored` | `TestTaskTreeStatus_States` |
| 3 | 锚目录已删 → `dead-anchor` 且不阻断 | `TestTaskTreeStatus_States` |
| 4 | 多锚任一命中即放行 | `TestTaskTreeStatus_States`（多锚段：两棵树都有活锚时任一命中放行） |
| 5 | 错树时门禁 BLOCK（并给出三条出路） | `TestTaskGate_OffTreeBlocks` |
| 6 | 错树时 guard 拒绝源码写入、放行测试文件 | `TestTaskGuardHookOffTreeDenies` |
| 7 | `forge worktree use` 迁移锚后放行 | `TestWorktreeUse_Rebinds` |
| 8 | 链定义变化使审批 stale；行尾差异不算变化 | `TestChainDigestStale`、`TestDigest_StableAndSensitive` |
| 9 | 关闭开关是显式 env（`=0` 才关） | `TestTreeGuardDisabled_Env` |
| 10 | 锚数据源只返回本任务锚、坏绑定文件跳过 | `TestAnchors_ListsOnlyMatchingTask`、`TestAnchors_SkipsCorruptBinding` |

验收命令（`forge task start --accept`）：

```bash
go test ./internal/worktree ./internal/taskpipeline ./internal/clitask ./internal/cli \
  -run 'TestAnchors|TestTaskTreeStatus|TestTaskGate|TestWorktreeUse|TestChainDigest' -count=1
```

实测：`ok internal/worktree 1.7s`、`ok internal/taskpipeline 1.0s`、`ok internal/clitask 18.4s`，`internal/cli` 在该 `-run` 下无匹配用例（该包不承载本特性行为，行为由 clitask 的端到端用例覆盖）。

## 8. 风险与回滚

- **误判风险**：用户在锚树之外合法地读/跑测试 → 不拦（只拦源码写入与门禁）。若某类工作确需在主树写源码，出口是 `forge worktree use`（显式迁移）或 `FORGE_TASK_TREE_GUARD=0`（留审计）。
- **性能**：`Anchors` 每个 `workspaces/*.json` 一次读，典型 <5 个文件；hookdispatch 每轮一次，实测计入报告。
- **回滚**：单提交可 revert；关掉开关即回到 v1.84.2 行为（guard 无 env 变量时逻辑不变）。

## 9. 改动面

源码：`internal/worktree/binding.go`（`Anchors`）、`internal/taskpipeline/state.go`（`TreeStatus`/`TaskTreeStatus`/`TreeGuardDisabled`）、`internal/taskpipeline/artifactchain_gate.go`（`ChainDigestStale`）、`internal/clitask/task_gate.go`（错树 BLOCK + chain advisory）、`internal/clitask/task_worktree.go`（`worktree use`）、`internal/clitask/task_misc.go`（status 展示）、`internal/clitask/task_chain.go`（审批写 ChainDigest + verify 展示）、`internal/hookdispatch/hook.go`（`FORGE_TASK_OFFTREE`/`FORGE_TASK_WORKTREE` 注入 + 死锚告警）、`internal/hooks/embed_task.go`（guard 分支）、`internal/artifactchain/artifactchain.go`（`Digest`）、`internal/tasktypes/model.go`（`ChainDigest` 字段）。

测试：`internal/worktree/binding_anchors_test.go`、`internal/taskpipeline/state_tree_test.go`、`internal/taskpipeline/chain_digest_test.go`、`internal/clitask/task_tree_guard_test.go`、`internal/hooks/taskguard_offtree_test.go`、`internal/artifactchain/digest_test.go`。

文档：`docs/reference/cli.md`（`forge worktree use`）、本文件。`CHANGELOG` 由 release-please 代管（不手写）。

## 10. 已知限制（P1 之后另开）

- `forge worktree finish` 仍只按 `binding.Path` 合并；错树期间的保护已前置到写入与门禁，`finish` 本身不做二次比对。
- 死锚的清理仍要显式 `forge worktree janitor`（守护只告警不删除）。
- 本守护不理解「同一任务被两个人分派到两棵树」的协作语义：多锚即放行（宁可漏拦也不误拦）。
