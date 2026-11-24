package impact

import (
	"path"
	"path/filepath"
	"sort"

	"github.com/MjxUpUp/Forge/internal/testledger"
)

// PackageTestState 是一个测试目录（相对仓库根）的账本摘要。
type PackageTestState struct {
	Dir               string `json:"dir"`
	Live              int    `json:"live"`
	Orphaned          int    `json:"orphaned"`
	Skipped           int    `json:"skipped"`
	SkippedUnapproved int    `json:"skipped_unapproved"`
}

// collectTestState 给"建议先跑的测试目录"补上账本摘要：这些目录里的测试各有多少
// 是有新鲜执行记录的（live）、有多少从来没记录过（orphaned）、有多少被跳过。
//
// 账本读不到（没有记录文件 / 没有审批清单）时不报错：那本身就是"全 orphaned"的
// 如实表达，也正是 impact 应该让人看见的东西。
func collectTestState(root string, dirs []string) []PackageTestState {
	if len(dirs) == 0 {
		return nil
	}
	decls, err := testledger.ScanDecls(root)
	if err != nil {
		return nil
	}
	recs, _, err := testledger.LoadRecords(filepath.Join(root, ".forge", "test-exec.jsonl"))
	if err != nil {
		// 记录文件存在但读不动（权限/损坏）：按"没有记录"降级，仍把每个目录
		// 如实报成 orphaned。整段消失会把"没有证据"读成"没有这个问题"。
		recs = nil
	}
	approvals, err := testledger.LoadSkipApprovals(filepath.Join(root, "spec", "test-skips.yaml"))
	if err != nil {
		approvals = map[string]testledger.SkipApproval{}
	}
	l := testledger.Build(decls, recs, testledger.Options{Approvals: approvals})
	byDir := map[string]*PackageTestState{}
	for _, d := range dirs {
		byDir[d] = &PackageTestState{Dir: d}
	}
	for _, e := range l.Entries {
		st, ok := byDir[path.Dir(e.File)]
		if !ok {
			continue
		}
		switch e.State {
		case testledger.StateLive:
			st.Live++
		case testledger.StateOrphaned:
			st.Orphaned++
		case testledger.StateSkipped:
			st.Skipped++
			if !e.SkipApproved {
				st.SkippedUnapproved++
			}
		}
	}
	out := make([]PackageTestState, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, *byDir[d])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}
