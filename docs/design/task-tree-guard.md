# task-tree-guard：任务树的错树守护

状态：已发布（判定与门禁拦截在 3.0 仍生效；宿主 hook 层与 `forge worktree use` 均已删除，差异见 §0）。

## 0. 3.0 之后的现实（读旧段落前先看）

| 面 | 现状 |
|---|---|
| 判定 | **仍生效**：`internal/taskpipeline/state.go` 的 `TaskTreeStatus(root, ref)` 返回 `TreeStatus{State, AnchorPath, Wtid}`；`TreeGuardDisabled()` 读 `FORGE_TASK_TREE_GUARD=0` |
| 锚数据源 | **仍生效**：`internal/worktree/binding.go` 的 `Anchors(root, ref)`（按 `workspaces/<wtid>.json` 键控）、`BindTask`、`worktree.ID` |
| 门禁拦截 | **仍生效**：`internal/clitask/task_gate.go` 在 `off-tree` 时直接 `taskpipeline.GateBlocked`，拒绝文案给 `cd <锚>` 与逃生 env，并落一条 `tree-guard` checklog 行 |
| 状态展示 | **仍生效**：`forge task status` 打 `Tree: off-tree（锚：…）` 独立行 |
| 写入拦截（hook 层） | **已移除**（1.85 宿主层删除：`internal/hooks`、`internal/hookdispatch` 不再存在）。当前守护是**门禁级**——写错树不会当场被拒，但门禁会拦并留痕 |
| 迁移锚 | **已移除**：`forge worktree use` 随 core-only 删除；出路改为 `cd` 到锚树，或用 `FORGE_TASK_TREE_GUARD=0` 带审计地临时关闭 |
| 链定义指纹（`chain_digest`） | **已移除**：`internal/artifactchain` 随 core-only 删除；旧审批里的 `chain_digest` 键保留为历史字段，不再判定 |

`forge task start --worktree` 仍会建树外 worktree、切分支并写绑定；把任务锚到某棵树这件事没有变，变的只是「发现你站错树」的时机——从每次编辑提前到门禁。

## 1. 问题：任务可以锚在一棵树，工作在另一棵

`forge task start --worktree` 会在 repo 树外建独立 worktree、切分支，并在 start 收尾时写绑定与活跃指针；但**这两步不是 `--worktree` 专属**——普通 start 也会无条件写 `SetActiveTaskRef` 与 `worktree.BindTask`，所以任何由 forge 启动的任务都会写活锚（外部手工造的 TaskState 除外）。而没有任何机制**要求**后续编辑真的发生在那棵树里。

历史复现（v1.84.2 真机，临时 repo）：`task start --worktree` 建出 `<repo>-wt/feat-wt-lane` → 回主检出执行 `task status` 仍解析出该任务且无提示 → 在主检出改源码、跑 `task gate task-implement` → **passed**。

根因两条，都在解析层：

1. 优先级 1 的 `ActiveTaskStateWithPath` 只读 `active-task-ref-<sid>`，读到 ref 且任务未完成就返回，**不与 cwd 的 wtid 比对**；按目录键控的 `worktree.Load` 反而排在后面。
2. `active-task-ref-<sid>` 落在**共享 DataDir**，于是主检出也读得到同一枚指针。

## 2. 判据：任务树状态（四态）

`taskpipeline.TaskTreeStatus(root, taskRef)` 返回：

| State | 条件 | 执法 |
|---|---|---|
| `ok` | 无活跃任务；或活跃任务没有任何 binding | 放行 |
| `ok-anchored` | 当前 wtid 命中任一个活锚（多锚任一命中即放行——多锚并发是允许形态） | 放行 |
| `off-tree` | 任务有 ≥1 个**活**锚，且当前 wtid 不在其中 | **阻断**门禁（task-implement / task-complete），`task status` 显示锚路径 |
| `dead-anchor` | 任务有锚但所有锚目录都不存在 | 放行（守护不删除、也不再打 hook 级 advisory） |

`wtid` 复用 `worktree.ID(root)`（EvalSymlinks 后 hash12），与 binding 文件名同源——判定是「当前树 != 锚树」，不含分支名或路径前缀推断（分支名可比性差，rebase/重命名会误判）。

**为什么看 binding 而不是 active-task-ref**：ref 落在共享 DataDir、按会话键控，天然可被任意树读到（这正是漏洞）；binding 按树键控（`workspaces/<wtid>.json`），是唯一记录「任务被锚在哪棵树」的持久事实。ref 仍是**任务归属**的解析入口，binding 只回答**位置**。

## 3. 门禁侧执法与逃生

- 拦截点：`internal/clitask/task_gate.go` 的 `runTaskGate` 在 `TaskTreeStatus` 为 `off-tree` 时 `GateBlocked`，**在跑判据之前**拒绝——避免在一棵空树上刷出绿色证据。
- 逃生：`FORGE_TASK_TREE_GUARD=0`（`=0` 才关）。关闭后门禁放行，但落一条 `escape-hatch` 行（`escape.gate=task-tree-guard`），任务落一条逃生记录、证据强度封顶 Weak（3.0 起没有分数，capped 不再指分数）。
- 留痕：每次错树 BLOCK 落一条 `tree-guard` checklog 行（`Passed=false`、`Level=warn`，Meta 带 `tree_guard=off-tree`、`anchor`、`wtid`），它是**观察证据**，不进 evidence 白名单。
- 关开关 ≠ 回到旧行为：`task status` 仍会显示 `Tree: off-tree`，checklog 仍记逃生行，证据强度仍按逃生规则封顶。

## 4. 已知限制

- **只到门禁，不到编辑**：hook 层删除后，在错误的树里写文件不会当场被拒；守的是「别在一棵空树上过门禁」。Bash 重定向写源码同样不在覆盖范围。
- `forge task finish` 仍只按 `binding.Path` 合并，`finish` 本身不做二次比对（保护已前置到门禁）。
- 死锚不自动清理：`forge worktree janitor` 已删除（core-only 批次），删目录后由 binding 判定为 `dead-anchor` 并放行。
- 多锚即放行（宁可漏拦也不误拦）；不理解「同一任务被两个人分派到两棵树」。

## 5. 判据（可测，测试名保留原映射）

| # | 判据 | 测试 |
|---|---|---|
| 1 | 活锚在别树 → `off-tree`（并指出锚路径） | `TestTaskTreeStatus_States` |
| 2 | 当前树是锚 → `ok-anchored` | `TestTaskTreeStatus_States` |
| 3 | 锚目录已删 → `dead-anchor` 且不阻断 | `TestTaskTreeStatus_States` |
| 4 | 多锚任一命中即放行 | `TestTaskTreeStatus_States` |
| 5 | 错树时门禁 BLOCK，文案给出 `cd <锚>` 与 `FORGE_TASK_TREE_GUARD=0` | `TestTaskGate_OffTreeBlocks` |
| 6 | 关闭开关是显式 env（`=0` 才关） | `TestTreeGuardDisabled_Env` |
| 7 | 锚数据源只返回本任务锚、坏绑定文件跳过 | `TestAnchors_ListsOnlyMatchingTask`、`TestAnchors_SkipsCorruptBinding` |
| 8 | 错树 BLOCK 与关守护都留 deterministic checklog 行 | `TestTaskGate_OffTreeRecordsChecklog` |

验收命令：

```bash
go test ./internal/worktree ./internal/taskpipeline ./internal/clitask -run 'TestAnchors|TestTaskTreeStatus|TestTaskGate|TestTreeGuard' -count=1
```

## 6. 风险与回滚

- **误判风险**：在锚树之外合法地读、跑测试、`git` 操作不拦；确实要在别的树过门禁，出口是 `cd` 到锚树或用逃生 env。
- **性能**：`Anchors` 每个 `workspaces/*.json` 一次读（典型 <5 个文件），`TaskTreeStatus` 只 stat 锚目录，没有额外进程。
- **回滚**：判定与门禁是两个独立提交（`state.go` 判定、`task_gate.go` 拦截）；回退判定会让拦截恒为 `ok`，但不会破坏既有的状态展示。

## 7. 改动面（当前存活部分）

源码：`internal/worktree/binding.go`（`Anchors`/`BindTask`）、`internal/worktree/wtid.go`（`ID`）、`internal/taskpipeline/state.go`（`TreeStatus`/`TaskTreeStatus`/`TreeGuardDisabled`）、`internal/clitask/task_gate.go`（错树 BLOCK + `tree-guard` 留痕）、`internal/clitask/task_misc.go`（status 展示）、`internal/checklog/types.go` + `escape.go`（`CheckTreeGuard` 入 roster）、`internal/compat/compat.go`（`FORGE_TASK_TREE_GUARD` 入 `EscapeEnvs`）。

已随之移除：`internal/hooks/embed_task.go`（写入层）、`internal/hookdispatch/*`（注入与节流）、`internal/artifactchain`（链定义指纹）、`internal/clitask/task_worktree.go` 的 `worktree use` 子命令。

文档：`docs/reference/cli.md`（无 `worktree` 命令面）、`docs/design/compat-commitments.md`（逃生舱清单）、本文件。
