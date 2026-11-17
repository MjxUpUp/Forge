# design：forge-ruthless

## TL;DR

删除假阳性检查与过程分数，change 三份化，用 `forge impact` 把"删掉的风险"变成按 diff 生成的披露要求；acceptance 在 task start 预跑。

## 1. 删除清单（代码落点）

- `internal/taskpipeline/executor.go`：移除 `checkDocGate`/artifact-chain/plan-first/unused-scan/test-coverage/mutation 分支与 `task-verify` gate case。
- `internal/taskpipeline/docgate.go`、`mutationgate.go`、`unusedgate.go`、`unusedscan.go`、`artifactchain_gate.go`、`selfcheck.go`（仅保留被引用部分）：删除或缩为被删包的内部实现。
- `internal/cli/`：删除 `task doc-review`、`task mutation` 子命令与 `review pass --note` 的 self-refresh。
- `internal/taskpipeline/scoring.go`：保留内部 ratio 计算供披露，删除对外 Score 输出。
- `internal/taskpipeline/executor_check_verify*.go`：task-verify 的检查合并进 complete（acceptance 已跑 + 证据存在 + 影响面披露）。

## 2. 产物三份化

`internal/change/change.go`：`FileChange = "change.md"`；`New` 写 goal/change/acceptance；`NewFast` 同。`tasks.md` 仅当用户手工创建时参与 `change gate --task`。`DocProblems` 要求：goal 可解析、change.md 非空、acceptance 有围栏；老 change（有 proposal/design）继续通过。

goal 模板三段（Objective / Out of Scope / Check）；解析器保留五段并额外接受 `change.md` 合并段。

## 3. forge impact

`internal/impact`：输入 root，输出 `Report{Changed, Docs, SourcesWithoutTests, SuggestedTests}`。判定：
- Docs：路径命中 `docs/`、`README*`、`*.md`（排除 spec/changes 自身）。
- SourcesWithoutTests：`*.go` 非 `_test.go`，且同目录无 `_test.go`（或该文件无同名测试文件）。
`forge impact [--json]` 打印；`next` 与 `task start` 展示提示，不阻断。

## 4. complete 的披露要求

`checkImpactDisclosure`：若 impact 报告非空，complete 要求 state 中记录 `impact_disclosure`（文本或"豁免：理由"），否则 BLOCKED。这是删掉 doc gate 与 coverage 阻断后唯一的新增阻断点。

## 5. acceptance 预跑

`internal/clitask/task_preflight.go`：task start 后按 acceptance 逐条执行（10s 超时），输出通过数与前 3 条失败摘要，记录 checklog `acceptance-preflight`；不影响 start 成功。

## 6. 风险与边界

- 文档过期：只在该次 diff 命中 docs 时要求披露；未命中不检查（接受）。
- 测试质量：只做"有无测试文件"提示，不判质量（删 mutation 的代价）。
- 评审判定：保留唯一评审，输入 diff + acceptance + impact。
