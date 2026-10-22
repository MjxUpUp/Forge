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
// 只有其余各父全部来自基线分支的合并才剔除：任一侧是本任务自己的侧分支（--no-ff
// 合回、章鱼合并、分叉后 git pull 产生的合并）时整段算本任务改动——否则删测试之类
// 的改动能借一次合并躲过 cheat-scan / test-coverage。有远端时基线只认 origin/main、
// origin/master，本地 main/master 可被 agent 随手提交或 branch -f，不作为依据。

// mergedInRemoteBaseRefs are trusted as "base" whenever one of them exists; mergedInLocalBaseRefs are the fallback for repos without such a remote. Local branches are not trusted alongside a remote: an agent can commit to local main or move it with branch -f, and everything reachable from base gets dropped.
//
// mergedInRemoteBaseRefs 只要存在其一就只认它们为「基线」；mergedInLocalBaseRefs
// 仅在没有这类远端 ref 的仓库里回落使用。有远端时不信本地分支：agent 可以往本地
// main 提交或 branch -f 挪动它，而从基线可达的一切都会被剔除。
var (
	mergedInRemoteBaseRefs = []string{"origin/main", "origin/master"}
	mergedInLocalBaseRefs  = []string{"main", "master"}
)

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

// existingBaseRefs returns the base refs present in root: the remote ones if any exist, otherwise the local fallback.
//
// existingBaseRefs 返回 root 里存在的基线 ref：有远端基线就只返回远端的，否则返回
// 本地回落。
func existingBaseRefs(root string) []string {
	for _, cands := range [][]string{mergedInRemoteBaseRefs, mergedInLocalBaseRefs} {
		var refs []string
		for _, ref := range cands {
			if _, err := gitLines(root, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
				refs = append(refs, ref)
			}
		}
		if len(refs) > 0 {
			return refs
		}
	}
	return nil
}

// reachableFromBase reports whether rev is reachable from one of refs.
//
// reachableFromBase 报告 rev 能否从 refs 之一到达。
func reachableFromBase(root string, refs []string, rev string) bool {
	for _, ref := range refs {
		if exec.Command("git", "-C", root, "merge-base", "--is-ancestor", rev, ref).Run() == nil {
			return true
		}
	}
	return false
}

// ExcludeMergedInFiles drops from committed the files that changed in from..HEAD only through base-side merges: a first-parent merge whose other parents (2..N) are all reachable from a base ref contributes only the files edited in the merge commit itself (conflict resolution or edits made while merging); every other first-parent commit — including merges with any non-base parent, such as the task's own side branch — contributes all its files. Base refs are origin/main and origin/master when present, else local main and master. Callers pass only files of the committed range. committed is returned unchanged when the range has no merge, when from is not on HEAD's first-parent chain, when a merge on that chain is itself reachable from base (the chain is base's history: the task was merged into base and fast-forwarded back), when no base ref exists, or on any git failure.
//
// ExcludeMergedInFiles 从 committed 中剔除在 from..HEAD 内只经「基线一侧合并」带入
// 的文件：第一父链上的合并提交若其余各父（第 2..N 个）全部可从基线 ref 到达，只计
// 合并提交里本任务亲手改过的文件（冲突解决或合并时顺手改；整体采用对方版本的文件
// 视为基线带入）；其余第一父链提交——包括任一父不来自基线的合并，如合并本任务侧
// 分支——全部计入。基线 ref 有 origin/main、origin/master 时只认它们，否则回落本地
// main、master。调用方只传已提交区间的文件。以下情况原样返回（保守：不能证明是外来
// 的，就继续计入）：区间内没有合并提交；from 不在 HEAD 的第一父链上；链上某个合并
// 提交本身可从基线到达（链是基线的历史：任务被合进基线后又快进回来）；没有基线
// ref；任何 git 调用失败。已知局限：任务尚无提交时快进合并基线，没有合并提交可依据，
// 保持原行为。
func ExcludeMergedInFiles(root, from string, committed []string) []string {
	if from == "" || len(committed) == 0 {
		return committed
	}
	merges, err := gitLines(root, "rev-list", "--first-parent", "--merges", from+"..HEAD")
	if err != nil || len(merges) == 0 {
		return committed
	}
	if !fromOnFirstParentChain(root, from) {
		return committed
	}
	refs := existingBaseRefs(root)
	if len(refs) == 0 {
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
		if reachableFromBase(root, refs, m) {
			return committed
		}
		parents, err := gitLines(root, "rev-list", "--parents", "-n", "1", m)
		if err != nil || len(parents) != 1 {
			return committed
		}
		others := strings.Fields(parents[0])
		if len(others) < 3 {
			return committed
		}
		baseSide := true
		for _, p := range others[2:] {
			if !reachableFromBase(root, refs, p) {
				baseSide = false
				break
			}
		}
		if baseSide {
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
