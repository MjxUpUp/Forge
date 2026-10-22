---
status: review
owner: MjxUpUp
ask: 审改动集剔除「从基线合并带入的文件」的口径与已知局限
tier: mini
---

# 修复方案 | 合并基线分支后改动集剔除基线带入的文件（2026-10）

## TL;DR
任务分支中途合并 main 之后，forge 以任务起点 `HeadCommit` 为基准计算改动集，把 main 带进来的文件也当成本任务的改动。本批在改动集的单一真相源 `taskChangedFiles` 与 review 的审查窗口里，剔除只经「基线一侧合并」带入的文件；没有合并提交的任务结果逐字不变。

`taskChangedFiles` 的消费方都随之修正：scope-drift（复发升硬项目里是 HARD 阻断）、test-coverage、cheat-scan 的文件集、mutation 抽样、acceptance 的改动指纹、design tier 判定。review 的 test-diff 走 `reviewWindowFiles`，单独接线。

## 问题与目标
- 实证：2026-10-22 在任务 fix/escape-rulings-doc-hygiene 上合并 #128（52 个文件）后，task-verify 报「43 个源文件超出 PlanScope」并 HARD 阻断，只能 `forge task decide` 记录原因后 `scope add` 收编。
- 根因：`internal/taskpipeline/testcoverage.go:280` 的 `taskChangedFiles` 用 `git diff HeadCommit..HEAD`；`internal/cli/review.go:46` 的 `reviewWindowFiles` 有审查基线时用 `git diff <基线>`。两者都把合并进来的基线改动算进去。
- 目标：已提交部分只保留「本任务的文件」，即第一父链上非合并提交触及的文件、非基线合并（本任务侧分支）带入的文件，以及基线合并提交里本任务亲手改过的文件；再与净差异取交集。

## Non-goals
- 不改 scoring 的 diff 基准（`internal/scoring/git.go:26` 的 resolveDiffBase）。它同时给变更范围维度提供行数（git.go:18 `CollectGitData`）、给断言保护维度提供断言密度（assertion.go:44 `CollectAssertionDensity`），合并后这两项仍会把基线改动算进去，另议。
- 不处理快进合并：任务尚无提交时快进合并 main，没有合并提交可依据，无法区分；保持原行为并在注释里写明。
- 不改 `forge task scope` 的命令面。

## 需求与验收
- REQ-1 合并基线分支后，改动集剔除只从基线带入的文件，保留本任务自己的提交触及的文件
  accept[REQ-1]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_DropsBaseOnlyFiles$ -count=1 :: --- PASS: TestExcludeMergedInFiles_DropsBaseOnlyFiles (
- REQ-2 合并时有冲突、在合并提交里改过的文件仍算本任务改动
  accept[REQ-2]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsConflictResolution$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsConflictResolution (
- REQ-3 区间内没有合并提交时结果逐字不变
  accept[REQ-3]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_NoMergeUnchanged$ -count=1 :: --- PASS: TestExcludeMergedInFiles_NoMergeUnchanged (
- REQ-4 taskChangedFiles + ScopeDrift 端到端：任务分支合并 main 后，drift 不含 main 的文件
  accept[REQ-4]: go test ./internal/taskpipeline/ -run ^TestTaskChangedFiles_MergedMainNotDrift$ -count=1 :: --- PASS: TestTaskChangedFiles_MergedMainNotDrift (
- REQ-5 review 的 test-diff 审查窗口在有审查基线时同样剔除合并带入的测试文件
  accept[REQ-5]: go test ./internal/cli/ -run ^TestReviewWindowFiles_ExcludesMergedBaseFiles$ -count=1 :: --- PASS: TestReviewWindowFiles_ExcludesMergedBaseFiles (
- REQ-6 本任务侧分支经 --no-ff 合回时整段保留，含其中的测试删除
  accept[REQ-6]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsOwnSideBranch$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsOwnSideBranch (
- REQ-7 第一父链已不属于本任务时（任务被合进别的分支后快进回来）原样返回
  accept[REQ-7]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_InvertedFirstParentUnchanged$ -count=1 :: --- PASS: TestExcludeMergedInFiles_InvertedFirstParentUnchanged (
- REQ-8 测试文件改名后又大改，净 diff 里旧路径的删除仍算本任务改动
  accept[REQ-8]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename (
- REQ-9 git 调用失败时原样返回
  accept[REQ-9]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_GitFailureUnchanged$ -count=1 :: --- PASS: TestExcludeMergedInFiles_GitFailureUnchanged (
- REQ-10 有审查基线、之后没有合并时，审查窗口与改动前逐字相同
  accept[REQ-10]: go test ./internal/cli/ -run ^TestReviewWindowFiles_NoMergeUnchanged$ -count=1 :: --- PASS: TestReviewWindowFiles_NoMergeUnchanged (

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
- 新增 `taskpipeline.ExcludeMergedInFiles(root, from string, committed []string) []string`，调用方只传已提交区间的文件：
  - `from..HEAD` 第一父链上没有合并提交时原样返回，只多一次 `rev-list` 调用；
  - 两道前置守卫，任一不满足就原样返回：沿 HEAD 的第一父链能走到 from；HEAD 本身不能从基线 ref 到达。前者防「别的分支在 from 之前分叉、任务快进到它」，后者防「任务被合进 main 后快进回来」，两种情况下第一父链都不再是本任务的，区分会整个反过来；
  - 「本任务的文件」= 第一父链上非合并提交的文件（`log --first-parent --no-merges --no-renames --name-only`），加上每个合并提交的贡献：
    - 第二父可从基线 ref（main、origin/main、master、origin/master）到达，是基线合并：只计 `log -1 --cc --no-renames --name-only` 列出的文件，即合并提交里本任务亲手改过的文件（冲突解决或合并时顺手改）；整体采用基线版本的文件视为基线带入；
    - 其他合并（本任务侧分支 --no-ff 合回、分叉后 git pull 产生的合并）：`diff --no-renames --name-only m^1 m` 整段计入；
  - 返回 committed 中属于「本任务的文件」的项；任何 git 调用失败时原样返回（保守：不能证明外来就继续算）。
- `taskChangedFiles` 在已提交部分（`HeadCommit..HEAD`）先过 `ExcludeMergedInFiles`，再过既有的跨任务归属过滤；工作树与 untracked 来源不过滤。
- `reviewWindowFiles` 有审查基线时，先数 `baseline..HEAD` 的合并提交，为 0 直接按原逻辑返回；否则剔除已提交差异里被判为基线带入、且工作树没再改动的文件。untracked 文件不经剔除。

### 关键取舍
- 不做（继续靠 `scope add` 收编）：否决。收编会把不相干的文件写进 PlanScope，污染「方案外改动」的判定，test-coverage 与 cheat-scan 也会继续误报。
- 只剔基线合并，不剔所有合并：所有合并都剔会让本任务侧分支的改动（包括删测试）借一次 `--no-ff` 合并躲过 cheat-scan 与 test-coverage。代价是基线名写死为 4 个候选，与 `taskChangedFiles` 的 legacy 回落、`cheatDiffBases` 用的是同一组。
- 与净差异取交集而不是直接用第一父链的文件：任务先改后又改回的文件仍按净差异处理，与现行口径一致。
- log 侧用 `--no-renames`：测试文件改名后又大改，净 diff 认不出重命名、列出旧路径的删除；log 若按重命名只列新路径，这条删除会被误剔。

### 已知局限
- 快进合并无法区分（见 Non-goals）。
- cheat-scan 只剔文件、不剔行：`cheatDiffBases`（cheatscan.go:710）仍用 `HeadCommit..HEAD` 取新增行，合并带入的行只要落在本任务也改过的文件里，仍会被扫描。完全属于基线的文件已经不在文件集里，不受影响。
- 基线分支不叫 main/master 的仓库（如 develop），合并会被当作非基线合并整段计入，退回原行为，不会误剔。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- 回滚即 revert 本分支的提交；只影响改动集的计算，不涉及数据面与命令面。
