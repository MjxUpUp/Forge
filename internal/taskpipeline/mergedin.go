package taskpipeline

import (
	"os/exec"
	"strings"
)

// mergedin.go — 改动集剔除「从基线分支合并带入」的文件。
//
// 任务分支中途合并 main（或其它基线分支）后，`git diff from..HEAD` 会把合并进来的
// 基线改动也算成本任务的改动：scope-drift 误报方案外改动（复发升硬项目里是 HARD
// 阻断）、test-coverage 与 cheat-scan 扫到别人的文件、review 的 test-diff 披露别人
// 的测试改动（2026-10-22 实证：合并 #128 的 52 个文件后报「43 个源文件超出
// PlanScope」）。这里沿第一父链区分「本分支自己的提交」与「合并带入的基线一侧」。
//
// 只有另一侧来自基线分支的合并才剔除：另一侧是本任务自己的侧分支（--no-ff 合回、
// 分叉后 git pull 产生的合并）时整段算本任务改动——否则删测试之类的改动能借一次
// 合并躲过 cheat-scan / test-coverage。

// mergedInBaseRefs are the refs whose history counts as "base": a merge whose second parent is reachable from one of them brought base changes in. Same candidates as taskChangedFiles' legacy fallback.
//
// mergedInBaseRefs 是视为「基线」的 ref：合并提交的第二父可从其中之一到达，即为
// 合并带入了基线改动。与 taskChangedFiles 的 legacy 回落候选一致。
var mergedInBaseRefs = []string{"main", "origin/main", "master", "origin/master"}

// gitLines runs git in root and returns its non-empty trimmed output lines.
//
// gitLines 在 root 运行 git，返回去空白后的非空输出行。
func gitLines(root string, args ...string) ([]string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}

// fromOnFirstParentChain reports whether from is reached by walking HEAD's first-parent chain — the precondition for "first parent = this branch". When the task branch was merged into another branch and then fast-forwarded to it, the chain belongs to the other branch and the split would be inverted.
//
// fromOnFirstParentChain 报告沿 HEAD 第一父链能否走到 from——「第一父 = 本分支」
// 成立的前提。任务分支先被合并进别的分支、再快进回来时，第一父链属于那个分支，
// 区分会整个反过来。
func fromOnFirstParentChain(root, from string) bool {
	fromSHA, err := gitLines(root, "rev-parse", "--verify", from+"^{commit}")
	if err != nil || len(fromSHA) != 1 {
		return false
	}
	chain, err := gitLines(root, "rev-list", "--first-parent", "--reverse", from+"..HEAD")
	if err != nil || len(chain) == 0 {
		return false
	}
	parent, err := gitLines(root, "rev-parse", chain[0]+"^1")
	return err == nil && len(parent) == 1 && parent[0] == fromSHA[0]
}

// reachableFromBase reports whether rev is reachable from a base ref. For a merge's second parent it means the merge brought base changes in; for HEAD it means the task branch is already contained in base (merged into it and fast-forwarded back), where the first-parent chain belongs to base and the split would be inverted.
//
// reachableFromBase 报告 rev 能否从某个基线 ref 到达。对合并提交的第二父，表示这次
// 合并带入的是基线改动；对 HEAD，表示任务分支已被基线包含（合进基线后又快进回来），
// 此时第一父链属于基线，区分会整个反过来。
func reachableFromBase(root, rev string) bool {
	for _, ref := range mergedInBaseRefs {
		if _, err := gitLines(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err != nil {
			continue
		}
		if exec.Command("git", "-C", root, "merge-base", "--is-ancestor", rev, ref).Run() == nil {
			return true
		}
	}
	return false
}

// ExcludeMergedInFiles drops from committed the files that changed in from..HEAD only through base-side merges: a first-parent merge whose second parent comes from a base ref (main, origin/main, master, origin/master) contributes only the files edited in the merge commit itself (conflict resolution or edits made while merging); every other first-parent commit — including merges of non-base branches, such as the task's own side branch — contributes all its files. Callers pass only files of the committed range. With no merge in the range, when from is not on HEAD's first-parent chain, when HEAD is already reachable from a base ref, or on any git failure, committed is returned unchanged.
//
// ExcludeMergedInFiles 从 committed 中剔除在 from..HEAD 内只经「基线一侧合并」带入
// 的文件：第一父链上的合并提交若第二父来自基线 ref（main、origin/main、master、
// origin/master），只计合并提交里本任务亲手改过的文件（冲突解决或合并时顺手改；
// 整体采用对方版本的文件视为基线带入）；其余第一父链提交——包括合并本任务侧分支
// 等非基线分支的合并——全部计入。调用方只传已提交区间的文件。区间内没有合并提交、
// from 不在 HEAD 的第一父链上、HEAD 已能从基线
// ref 到达、或任何 git 调用失败时原样返回（保守：不能证明是
// 外来的，就继续计入）。已知局限：任务尚无提交时快进合并基线，没有合并提交可依据，
// 保持原行为。
func ExcludeMergedInFiles(root, from string, committed []string) []string {
	if from == "" || len(committed) == 0 {
		return committed
	}
	merges, err := gitLines(root, "rev-list", "--first-parent", "--merges", from+"..HEAD")
	if err != nil || len(merges) == 0 {
		return committed
	}
	if !fromOnFirstParentChain(root, from) || reachableFromBase(root, "HEAD") {
		return committed
	}
	own := map[string]bool{}
	addAll := func(lines []string) {
		for _, l := range lines {
			own[l] = true
		}
	}
	// --no-renames：重命名后又大改时，净 diff 认不出重命名、列出旧路径的删除；
	// log 也要列出旧路径，否则测试删除会被误剔。
	files, err := gitLines(root, "log", "--first-parent", "--no-merges", "--no-renames", "--name-only", "--format=", from+"..HEAD")
	if err != nil {
		return committed
	}
	addAll(files)
	for _, m := range merges {
		if reachableFromBase(root, m+"^2") {
			files, err = gitLines(root, "log", "-1", "--cc", "--no-renames", "--name-only", "--format=", m)
		} else {
			files, err = gitLines(root, "diff", "--no-renames", "--name-only", m+"^1", m)
		}
		if err != nil {
			return committed
		}
		addAll(files)
	}
	kept := make([]string, 0, len(committed))
	for _, f := range committed {
		if own[f] {
			kept = append(kept, f)
		}
	}
	return kept
}
