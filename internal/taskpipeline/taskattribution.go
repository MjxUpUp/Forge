package taskpipeline

// taskattribution.go 把 taskChangedFiles 的已提交部分在同分支的多任务间归属，使一个
// 任务的改动集合只含由它自己记账的文件。
//
// 问题（2026-08 usage 实证，fix/gate-loopholes）：已提交部分原为
// `git diff HeadCommit..HEAD`——任务开始后的全部 commit，包括同分支前序任务在
// 自己 complete 时已记过账的 commit（顺序工作流情形：任务 B 先于任务 A 的 commit
// 落地前启动；或两个并行任务互踩文件）。这些外来 commit 虚增 B 的改动集合：
// PlanScope 永远收编不干净（agent 被迫 `scope add` 自己没碰过的文件——门禁反向
// 激励污染声明），test-coverage 对 A 已覆盖的文件重复要测试，一个真实 session
// 里两个并行任务互踩产生连续 6 条 scope-drift。
//
// 规则：已提交文件默认属于当前任务，除非它在 HeadCommit..HEAD 内的每一个触及
// commit 都落在另一个【已完成】任务记录的跨度内（其 HeadCommit → 最后一条通过
// 门禁的 head）。只有已完成任务可作归属锚：进行中任务的跨度开口，两个开口跨度
// 会互相吞掉对方的文件（两边都不记账）——覆盖黑洞。仅以已完成任务为锚时，一个
// 文件恰由一个任务记账：最先在其最后触及 commit 之上 complete 的那个。工作树与
// untracked 文件永不排除——未提交的工作永远属于当下正在做的人。
//
// 已知盲区（接受的取舍，由 TestTaskChangedFiles_InterleavedFirstToComplete 钉住）：
// rev-list 跨度是 commit【区间】，不辨作者。B 在 A 完成前提交、且 A 的最终 head
// 是 B 该 commit 的后代（同分支）时，B 的 commit 落入 A 的跨度，其文件归 A 记账
// ——先完成者胜过真实作者。安全依据：该文件在 A 完成时本就在 A 自己的 diff 里，
// 其 scope/test-coverage 要求在彼处恰已触发一次；A 完成之后 B 的 commit 在 A 跨度
// 外、仍归 B。跨度也随任务 state 保留期（PruneOldTasks）消退，之后回落旧的未过滤
// 行为——降级但不反向出错。

import (
	"os/exec"
	"strings"
)

// completedTaskSpans 返回已完成兄弟任务（排除 currentTaskRef）的 commit 跨度
// （start, end）：start = 任务的 HeadCommit（任务启动时记录），end = 其最后一
// 条已通过门禁记录的 head（≈ 任务完成时的最终 commit）。缺任一锚的任务（老
// state）不贡献跨度——保守方向：无归属、保持旧行为。跨度内 commit 后被改写
// 掉的，在 rev-list 处失败并跳过。
func completedTaskSpans(root, currentTaskRef string) [][2]string {
	states, err := ListTaskStates(root)
	if err != nil {
		return nil
	}
	var spans [][2]string
	for _, s := range states {
		if s.TaskRef == currentTaskRef || s.CompletedAt == nil || s.HeadCommit == "" {
			continue
		}
		// 最后一条已通过门禁的 head = 该任务最后记过账的 commit。History 只追加，
		// 故倒序扫。
		end := ""
		for i := len(s.History) - 1; i >= 0; i-- {
			if s.History[i].Passed && s.History[i].HeadCommit != "" {
				end = s.History[i].HeadCommit
				break
			}
		}
		if end == "" || end == s.HeadCommit {
			continue // 空跨度（start==end rev-list 为空，无归因价值）
		}
		spans = append(spans, [2]string{s.HeadCommit, end})
	}
	return spans
}

// foreignCommitSet 把所有跨度的 `git rev-list start..end` 并成一个 full hash 集。
// 失败的跨度（commit 被改写/回收）单独跳过——一条死跨度不拖垮其余归属。
func foreignCommitSet(root string, spans [][2]string) map[string]bool {
	if len(spans) == 0 {
		return nil
	}
	set := make(map[string]bool)
	for _, sp := range spans {
		out, err := exec.Command("git", "-C", root, "rev-list", sp[0]+".."+sp[1]).Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			if h := strings.TrimSpace(line); h != "" {
				set[h] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// committedFileCommits 把 headCommit..HEAD 内变更的每个文件映射到该区间内触及它
// 的 commit full hash 列表（一次 `git log --name-only`）。`commit:` 格式前缀让
// hash 行无歧义——恰好像 hex 串的文件名绝不会被误当 commit 行（且解析与
// sha1/sha256 长度无关）。
func committedFileCommits(root, headCommit string) map[string][]string {
	out, err := exec.Command("git", "-C", root, "log", "--format=commit:%H", "--name-only", headCommit+"..HEAD").Output()
	if err != nil {
		return nil
	}
	m := make(map[string][]string)
	commit := ""
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if h, ok := strings.CutPrefix(line, "commit:"); ok {
			commit = h
			continue
		}
		if commit != "" {
			m[line] = append(m[line], commit)
		}
	}
	return m
}

// excludeForeignCommitted 过滤任务改动文件的已提交部分：丢弃在 HeadCommit..HEAD
// 内全部触及 commit 都属于已完成兄弟任务跨度的文件（规则与其理由见文件 doc
// 注释）。无归属信息可用的文件（git log 失败、映射为空）保留——保守方向：
// 凡不能证明是外来的，继续要覆盖/scope。
func excludeForeignCommitted(root string, state *TaskState, committed []string) []string {
	if state == nil || state.HeadCommit == "" || len(committed) == 0 {
		return committed
	}
	foreign := foreignCommitSet(root, completedTaskSpans(root, state.TaskRef))
	if len(foreign) == 0 {
		return committed
	}
	byFile := committedFileCommits(root, state.HeadCommit)
	kept := committed[:0]
	for _, f := range committed {
		commits := byFile[f]
		allForeign := len(commits) > 0
		for _, c := range commits {
			if !foreign[c] {
				allForeign = false
				break
			}
		}
		if !allForeign {
			kept = append(kept, f)
		}
	}
	return kept
}
