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
- 目标：已提交部分只保留「本任务的文件」，即第一父链上非合并提交触及的文件、非基线合并（本任务侧分支、章鱼合并）带入的文件，以及基线合并提交里本任务亲手改过的文件；再与净差异取交集。

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
- REQ-7 第一父链已不属于本任务时（任务被合进 main 或别的分支后快进回来，包括之后又继续提交）原样返回
  accept[REQ-7]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_InvertedFirstParentUnchanged$ -count=1 :: --- PASS: TestExcludeMergedInFiles_InvertedFirstParentUnchanged (
- REQ-8 测试文件改名后又大改，净 diff 里旧路径的删除仍算本任务改动
  accept[REQ-8]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsDeletedPathOfHeavyRename (
- REQ-9 git 调用失败时原样返回
  accept[REQ-9]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_GitFailureUnchanged$ -count=1 :: --- PASS: TestExcludeMergedInFiles_GitFailureUnchanged (
- REQ-10 有审查基线、之后没有合并时，审查窗口与改动前逐字相同
  accept[REQ-10]: go test ./internal/cli/ -run ^TestReviewWindowFiles_NoMergeUnchanged$ -count=1 :: --- PASS: TestReviewWindowFiles_NoMergeUnchanged (
- REQ-11 同时合并基线与本任务侧分支的章鱼合并不算基线合并，侧分支上的测试删除保留
  accept[REQ-11]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsOctopusSideBranch$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsOctopusSideBranch (
- REQ-12 有 origin/main 时本地 main 不作基线：本地 main 上的测试删除合并进来后保留，从 origin/main 带入的文件照常剔除
  accept[REQ-12]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_LocalBaseUntrustedWithRemote$ -count=1 :: --- PASS: TestExcludeMergedInFiles_LocalBaseUntrustedWithRemote (
- REQ-13 「基线合并」的第二父选一个陈旧的基线提交、在合并里删掉测试时，删除仍算本任务改动
  accept[REQ-13]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsDeletionAgainstStaleBaseParent$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsDeletionAgainstStaleBaseParent (
- REQ-14 本任务删掉的测试被净 diff 配成基线新增拷贝的「重命名」时，被删路径补回、基线拷贝剔除
  accept[REQ-14]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsDeletionPairedAsRename$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsDeletionPairedAsRename (
- REQ-15 审查窗口里同样的重命名配对场景，被删路径在窗口里、基线拷贝不在
  accept[REQ-15]: go test ./internal/cli/ -run ^TestReviewWindowFiles_KeepsDeletionPairedAsRename$ -count=1 :: --- PASS: TestReviewWindowFiles_KeepsDeletionPairedAsRename (
- REQ-16 从基线合并带入、之后又在工作树里改过的文件留在审查窗口里
  accept[REQ-16]: go test ./internal/cli/ -run ^TestReviewWindowFiles_KeepsWorktreeEditedMergedFile$ -count=1 :: --- PASS: TestReviewWindowFiles_KeepsWorktreeEditedMergedFile (
- REQ-17 前序任务在 from 之前加强的测试，被本任务在基线合并冲突里整体取基线版本盖掉时，算本任务改动
  accept[REQ-17]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_KeepsTaskSideEditOverwrittenByBase$ -count=1 :: --- PASS: TestExcludeMergedInFiles_KeepsTaskSideEditOverwrittenByBase (
- REQ-18 criss-cross（两个 merge-base）时在合并里把测试改回基线版本，不论 git 单独挑哪个 merge-base 都算本任务改动
  accept[REQ-18]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_CrissCrossChecksEveryMergeBase$ -count=1 :: --- PASS: TestExcludeMergedInFiles_CrissCrossChecksEveryMergeBase (
- REQ-19 本任务的纯重命名在合并基线后仍只列新路径，与无合并时一致
  accept[REQ-19]: go test ./internal/taskpipeline/ -run ^TestExcludeMergedInFiles_PureRenameMatchesNoMerge$ -count=1 :: --- PASS: TestExcludeMergedInFiles_PureRenameMatchesNoMerge (
- REQ-20 合并基线后只在工作树里删掉测试、被窗口的 diff 配成基线拷贝的重命名时，被删路径仍在审查窗口里
  accept[REQ-20]: go test ./internal/cli/ -run ^TestReviewWindowFiles_KeepsWorktreeDeletionPairedAsRename$ -count=1 :: --- PASS: TestReviewWindowFiles_KeepsWorktreeDeletionPairedAsRename (
- REQ-21 `git diff --name-status` 的重命名与拷贝条目都解析出旧、新路径
  accept[REQ-21]: go test ./internal/taskpipeline/ -run ^TestRenamePair$ -count=1 :: --- PASS: TestRenamePair (

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
  - 基线 ref：有 `origin/main`、`origin/master` 时只认它们；没有这类远端 ref 时回落本地 `main`、`master`。有远端时不信本地分支，因为 agent 可以往本地 main 提交或 `branch -f` 挪动它，而从基线可达的一切都会被剔除；
  - 两道守卫，任一不满足就原样返回：
    - 沿 HEAD 的第一父链能走到 from，防「别的分支在 from 之前分叉、任务快进到它」；
    - 链上每个合并提交本身都不能从基线到达，防「任务被合进 main 后快进回来」，包括快进后又继续提交。这两种情况下第一父链都不再是本任务的，区分会整个反过来；
  - 「本任务的文件」= 第一父链上非合并提交的文件（`log --first-parent --no-merges --no-renames --name-only`），加上每个合并提交的贡献：
    - 其余各父（第 2..N 个，`rev-list --parents -n 1`）全部可从基线到达，是基线合并。计入它相对第一父 p1 改动的文件，只减去存在某个基线父 pi 同时满足三条的：合并结果等于 pi 的版本（不在 `diff pi m` 里）；对 `merge-base --all p1 pi` 的每个 merge-base mb，基线一侧改过该文件（在 `diff mb pi` 里），任务一侧没改过（不在 `diff mb p1` 里）。冲突解决（含整体取基线版本、盖掉任务一侧的改动）、合并时顺手改，以及选陈旧的基线父把文件改回旧版本，都算本任务改动；
    - 其他合并（本任务侧分支 --no-ff 合回、含侧分支的章鱼合并、分叉后 git pull 产生的合并）：`diff --no-renames --name-only m^1 m` 整段计入；
  - 返回 committed 中属于「本任务的文件」的项。调用方的净 diff 开着重命名检测，本任务删掉的测试可能被配成基线新增拷贝的「重命名」，只剩新路径；所以再取 `diff --name-status from..HEAD` 的重命名与拷贝对（沿用调用方同一份 diff.renames 配置，不写死 `-M`），新路径在 committed 里且被剔掉、旧路径属于本任务时把旧路径补回。新路径保留的重命名对不动，纯重命名的结果与无合并时一致；
  - 任何 git 调用失败时原样返回（保守：不能证明外来就继续算）；所有 git 调用带 `--no-replace-objects`，防 `git replace --graft` 把普通提交伪装成基线合并。这一项是纵深防御：按文件比对的规则下，伪装合并能剔掉的不超过一次真实合并，没有单独的用例。
- `taskChangedFiles` 在已提交部分（`HeadCommit..HEAD`）先过 `ExcludeMergedInFiles`，再过既有的跨任务归属过滤；工作树与 untracked 来源不过滤。
- `reviewWindowFiles` 有审查基线时，先数 `baseline..HEAD` 第一父链上的合并提交，为 0 直接按原逻辑返回；否则剔除已提交差异里被判为基线带入、且工作树没再改动的文件，并加入 `ExcludeMergedInFiles` 补回的旧路径；窗口的 diff 比到工作树，所以再按 `diff --name-status <基线>` 的配对，把新路径被剔掉、旧路径不是基线带入的旧路径补回（工作树里刚删、未提交的测试）。untracked 文件不经剔除。

### 关键取舍
- 不做（继续靠 `scope add` 收编）：否决。收编会把不相干的文件写进 PlanScope，污染「方案外改动」的判定，test-coverage 与 cheat-scan 也会继续误报。
- 只剔基线合并，不剔所有合并：所有合并都剔会让本任务侧分支的改动（包括删测试）借一次 `--no-ff` 或章鱼合并躲过 cheat-scan 与 test-coverage。代价是基线名写死为 4 个候选。名字与 `taskChangedFiles` 的 legacy 回落、`cheatDiffBases` 相同，但信任规则不同：这里有远端时不认本地 main/master。
- 倒置守卫查「链上合并提交本身在基线上」，不查「合并的第一父在基线上」：后者会把「任务尚无自有提交时 `--no-ff` 合 main」误判为倒置（此时第一父就是 from，本在基线上）。
- 与净差异取交集而不是直接用第一父链的文件：任务先改后又改回的文件仍按净差异处理，与现行口径一致。
- log 侧用 `--no-renames`：测试文件先改名、后在另一个提交里大改时，净 diff 认不出重命名、列出旧路径的删除；log 若按重命名只列新路径，这条删除会被误剔。
- 基线合并按文件比对版本，不用 `--cc`：`--cc` 只列出与所有父都不同的文件，合并结果等于某个陈旧基线父时会漏掉，测试的删除或削弱就被当成基线带入。
- 判定基线带入时要求「任务一侧分叉后没改过」，并检查全部 merge-base：只看基线一侧时，前序任务在 from 之前的加强被本任务取基线版本盖掉、或 criss-cross 下 git 恰好挑中某个 merge-base，削弱都会被剔除。`--all` 在已有任务一侧条件时只多覆盖一种窄拓扑（两个 merge-base 版本不同，基线回到其一、任务停在另一），这一项没有单独钉住它的用例。

### 已知局限
- 快进合并无法区分（见 Non-goals）。
- cheat-scan 只剔文件、不剔行：`cheatDiffBases`（cheatscan.go:710）仍用 `HeadCommit..HEAD` 取新增行，合并带入的行只要落在本任务也改过的文件里，仍会被扫描。完全属于基线的文件已经不在文件集里，不受影响。
- 基线分支不叫 main/master 的仓库（如 develop、upstream/main），以及用裸 SHA、`FETCH_HEAD`、`git pull <url> main` 合并时，合并会被当作非基线合并整段计入，退回原行为，不会误剔。squash 合并、cherry-pick 过来的基线提交同样保留原行为。
- 本任务的子分支先经 PR 合进 origin/main、任务再合并 origin/main 时，子分支上的改动按基线带入剔除。这些改动已经过那次 PR 的审查，本批不另行追溯。
- 任务先合过一次 origin/main，之后部分提交经「合并提交式」PR 进入 origin/main：那次合并本身已可从基线到达，触发倒置守卫，整段退回原行为（基线文件重新算进改动集，偏保守，不误剔）。squash 合并的 PR 不受影响。
- 基线上任何一个分叉后临时削弱过某个测试、后来又恢复的提交（例如 origin/main 自己的中间提交），本任务用 `git merge <sha>` 合并它、且任务一侧没碰过该测试时，这次削弱会被当作基线带入。PR 合回 main 时 main 的版本胜出，绕过的只是本地门禁。
- 「任务一侧改过」只看文件有没有变，不看那次改动是否已进基线：前序任务改过的文件经 squash PR 进了 main、main 又继续改它，当前任务合并 origin/main 后，这个文件仍算本任务改动。偏保守，不漏报，但会留下少量本批想消掉的 scope-drift 误报。
- 邪恶合并里「保留我方」、把基线对某个测试的加强撤回：from..HEAD 的净 diff 里该测试没有变化，改动集本来就看不到。这是净 diff 口径的问题，早于本批，与合并剔除无关。
- 基线可信度以远端 ref 为界：agent 用 `git update-ref` 或 `git fetch . X:refs/remotes/origin/main` 改写远端跟踪 ref，仍能让任意提交被当作基线。拦截这类命令属于 hazard-guard 的范围，本批不做。
- 有合并时每次调用多若干 git 子进程（固定约 10 个，每个合并另加 4～10 个，criss-cross 时每多一个 merge-base 再加 2 个），`taskChangedFiles` 在一次 verify 内被调用十余次；没有合并时只多 1 次。

## 验证与回滚
- `go vet ./...`；`go test ./... -count=1 -timeout 30m`。
- 回滚即 revert 本分支的提交；只影响改动集的计算，不涉及数据面与命令面。
