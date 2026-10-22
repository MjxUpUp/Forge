---
status: review
owner: MjxUpUp
ask: 审改动集剔除「从基线合并带入的文件」的口径与已知局限
tier: mini
---

# 修复方案 | 合并基线分支后改动集剔除基线带入的文件（2026-10）

## TL;DR
任务分支中途合并 main 之后，forge 以任务起点 `HeadCommit` 为基准计算改动集，把 main 带进来的文件也当成本任务的改动，scope-drift（复发升硬项目里是 HARD 阻断）、test-coverage、cheat-scan、mutation 抽样和 review 的 test-diff 都会误报。本批在改动集的单一真相源 `taskChangedFiles` 与 review 的审查窗口里，剔除只经合并提交从另一侧带入的文件；没有合并提交的任务结果逐字不变。

## 问题与目标
- 实证：2026-10-22 在任务 fix/escape-rulings-doc-hygiene 上合并 #128（52 个文件）后，task-verify 报「43 个源文件超出 PlanScope」并 HARD 阻断，只能 `forge task decide` 记录原因后 `scope add` 收编。
- 根因：`internal/taskpipeline/testcoverage.go` 的 `taskChangedFiles` 用 `git diff HeadCommit..HEAD`；`internal/cli/review.go` 的 `reviewWindowFiles` 有审查基线时用 `git diff <基线>`。两者都把合并进来的基线改动算进去。
- 目标：改动集只含本任务分支第一父链上的非合并提交触及的文件，加上合并提交里的冲突解决文件（与所有父提交都不同的文件），再与净差异取交集。

## Non-goals
- 不改 scoring 的 diff 基准（`internal/scoring/git.go` 的 resolveDiffBase），它按行数计变更范围，另议。
- 不处理快进合并：任务尚无提交时快进合并 main，没有合并提交可依据，无法区分；保持原行为并在注释里写明。
- 不改 `forge task scope` 的命令面。

## 需求与验收
- REQ-1 合并基线分支后，改动集剔除只从基线带入的文件，保留本任务自己的提交触及的文件
  accept[REQ-1]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_DropsBaseOnlyFiles$ -count=1 :: --- PASS: TestExcludeMergedInFiles_DropsBaseOnlyFiles (
- REQ-2 合并时有冲突、在合并提交里改过的文件仍算本任务改动
  accept[REQ-2]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsConflictResolution$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsConflictResolution (
- REQ-3 区间内没有合并提交时结果逐字不变
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_NoMergeUnchanged$ -count=1 :: --- PASS: TestExcludeMergedInFiles_NoMergeUnchanged (
- REQ-4 scope-drift 端到端：任务分支合并 main 后，task-verify 的 drift 不含 main 的文件
  accept[REQ-4]: go test ./internal/taskpipeline/ -run ^TestTaskChangedFiles_MergedMainNotDrift$ -count=1 :: --- PASS: TestTaskChangedFiles_MergedMainNotDrift (
- REQ-5 review 的 test-diff 审查窗口在有审查基线时同样剔除合并带入的测试文件
  accept[REQ-5]: go test ./internal/cli/ -run ^TestReviewWindowFiles_ExcludesMergedBaseFiles$ -count=1 :: --- PASS: TestReviewWindowFiles_ExcludesMergedBaseFiles (

## 设计
### 触点
（每行一个路径：PlanScope 按行取第一个路径解析。）
- `internal/taskpipeline/mergedin.go`
- `internal/taskpipeline/mergedin_test.go`
- `internal/taskpipeline/testcoverage.go`
- `internal/cli/review.go`
- `internal/cli/review_mergedin_test.go`
- `docs/plans/scope-drift-merged-base-2026-10.md`

### 做法
- 新增 `taskpipeline.ExcludeMergedInFiles(root, from string, files []string) []string`：
  - `git rev-list --merges --count from..HEAD` 为 0 时原样返回（无合并 = 无变化，也省掉额外 git 调用）；
  - 否则取「本分支自己的文件」= `git log --first-parent --no-merges --name-only --format= from..HEAD` ∪ `git log --first-parent --merges --cc --name-only --format= from..HEAD`（后者是合并提交里与所有父提交都不同的冲突解决文件）；
  - 只剔除 files 里属于已提交区间、但不在「本分支自己的文件」中的项；git 失败时原样返回（保守：不能证明外来就继续算）。
- `taskChangedFiles` 在已提交部分（`HeadCommit..HEAD`）先过 `ExcludeMergedInFiles` 再过既有的跨任务归属过滤；工作树与 untracked 来源不过滤。
- `reviewWindowFiles` 有审查基线时，对 `git diff <基线>` 的结果同样过滤（只作用于已提交差异，工作树与 untracked 不动）。

### 关键取舍
- 不做（继续靠 `scope add` 收编）：否决。收编会把不相干的文件写进 PlanScope，污染「方案外改动」的判定，test-coverage 与 cheat-scan 也会继续误报。
- 用第一父链而不是「对比 origin/main」：不需要猜基线分支名，合并哪个分支都适用；合并提交的 `--cc` 文件补回冲突解决。
- 与净差异取交集而不是直接用第一父链的文件：任务先改后又改回的文件仍按净差异处理，与现行口径一致。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- 回滚即 revert 本分支的提交；只影响改动集的计算，不涉及数据面与命令面。
