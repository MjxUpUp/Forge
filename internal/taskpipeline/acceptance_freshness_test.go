package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckAcceptanceFresh_ContentSnapshot is the regression pin for the
// acceptance-snapshot loophole (2026-08-25 gate-loopholes): the protocol order
// is verify-acceptance → commit → task complete, but the freshness snapshot was
// bound to HEAD, so the commit between verify and complete always staled the
// snapshot and forced a penalty re-run ("基于旧代码（快照 a ≠ HEAD b）", 3 times
// in one real session). The snapshot now binds the SOURCE CONTENT fingerprint
// anchored at the task's HeadCommit: a content-preserving commit keeps it fresh,
// while a real post-verify source edit still flips it and must be caught.
//
// TestCheckAcceptanceFresh_ContentSnapshot 是验收快照漏洞的回归钉
// （2026-08-25 gate-loopholes）：协议顺序是 verify-acceptance → commit →
// task complete，但 freshness 快照绑在 HEAD 上，verify 与 complete 之间的
// commit 必然使快照过期、罚重跑一次（「基于旧代码（快照 a ≠ HEAD b）」，单个
// 真实 session 出现 3 次）。快照改为绑锚定在任务 HeadCommit 的源码内容指纹：
// 不改内容的 commit 保持新鲜，验收后的真实源码改动仍翻转指纹、必须被检出。
func TestCheckAcceptanceFresh_ContentSnapshot(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c0")
	c0 := headShort(t, dir)

	// 任务工作：相对任务 HeadCommit 的未提交源码改动，verify-acceptance 在该
	// 工作树上实跑。
	state := &TaskState{TaskRef: `feat/acc`, HeadCommit: c0,
		Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 2 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	VerifyAcceptance(dir, state)
	crit := state.Acceptance[0]
	if !crit.Passed {
		t.Fatalf("criterion should pass: %+v", crit)
	}
	if crit.AcceptedBaseCommit != c0 || crit.AcceptedHeadCommit == "" {
		t.Fatalf("content snapshot not recorded: %+v", crit)
	}

	// 协议顺序：把验收过的内容 commit（HEAD 移动），再 complete——快照必须
	// 保持新鲜（本修复）。
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "c1: verified content")
	if headShort(t, dir) == c0 {
		t.Fatal("HEAD did not move after commit — test setup broken")
	}
	if ok, reasons := CheckAcceptanceFresh(dir, state); !ok {
		t.Fatalf("commit between verify and complete must NOT stale the content snapshot, got reasons=%v", reasons)
	}

	// 防绕过：验收后的源码编辑（此处为未提交）翻转内容指纹 → 必须检出。
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 3 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	ok, reasons := CheckAcceptanceFresh(dir, state)
	if ok {
		t.Fatal("post-verify source edit must stale the snapshot")
	}
	found := false
	for _, r := range reasons {
		if strings.Contains(r, `源码已变更`) {
			found = true
		}
	}
	if !found {
		t.Errorf("reasons should name the post-verify source change, got %v", reasons)
	}

	// 回退到验收过的内容即恢复新鲜——指纹绑内容不绑历史（重跑验收结果相同）。
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() int { return 2 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if ok, reasons := CheckAcceptanceFresh(dir, state); !ok {
		t.Fatalf("reverting to verified content must restore freshness, got %v", reasons)
	}
}

// TestVerifyAcceptance_ContentSnapshotFallback pins the fail-safe direction: when the task has no usable HeadCommit (legacy state) or the recorded commit is unreachable (history rewritten), VerifyAcceptance leaves the content fields empty and CheckAcceptanceFresh falls back to the HEAD-equality check — never fabricating a fingerprint.
//
// TestVerifyAcceptance_ContentSnapshotFallback 钉住 fail-safe 方向：任务无可用
// HeadCommit（老 state）或记录的 commit 不可达（历史改写）时，VerifyAcceptance
// 让内容字段留空、CheckAcceptanceFresh 回落 HEAD 相等检查——绝不伪造指纹。
func TestVerifyAcceptance_ContentSnapshotFallback(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "c0")

	// 老 state：HeadCommit 未设 → 内容字段留空，HEAD 相等路径照旧（verify 后
	// 即 fresh，commit 后过期）。
	state := &TaskState{TaskRef: `feat/legacy`,
		Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	VerifyAcceptance(dir, state)
	if c := state.Acceptance[0]; c.AcceptedBaseCommit != "" || c.AcceptedChangeHash != "" {
		t.Errorf("legacy state must not record a content snapshot: %+v", c)
	}
	if ok, reasons := CheckAcceptanceFresh(dir, state); !ok {
		t.Fatalf("legacy fresh path broken: %v", reasons)
	}
	runGit(t, dir, "commit", "--allow-empty", "-m", "c1")
	if ok, _ := CheckAcceptanceFresh(dir, state); ok {
		t.Fatal("legacy HEAD-equality path must still stale after a commit")
	}

	// 死 base：HeadCommit 已记但 commit 对象没了 → verify 内容字段留空（HEAD
	// 兜底），重跑即锚到新 HEAD，任务永不卡死。
	state2 := &TaskState{TaskRef: `feat/dead`, HeadCommit: `deadbeefdeadbeefdeadbeefdeadbeefdeadbeef`,
		Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	VerifyAcceptance(dir, state2)
	if c := state2.Acceptance[0]; c.AcceptedBaseCommit != "" {
		t.Errorf("unreachable base must fall back to legacy snapshot: %+v", c)
	}
	if ok, reasons := CheckAcceptanceFresh(dir, state2); !ok {
		t.Fatalf("dead-base task must recover via legacy path after re-verify: %v", reasons)
	}
}

// TestMergeAcceptanceResults_ContentFields pins that the §13 merge helper carries the content-snapshot fields onto the authoritative state alongside the other result fields (a merged result missing them would silently downgrade the task to the legacy HEAD check).
//
// TestMergeAcceptanceResults_ContentFields 钉住 §13 合并 helper 把内容快照字段
// 随其他结果字段一并搬到权威 state（漏搬的合并结果会把任务静默降级回 legacy
// HEAD 检查）。
func TestMergeAcceptanceResults_ContentFields(t *testing.T) {
	s := &TaskState{Acceptance: []AcceptanceCriterion{{Run: `go version`, Expected: `go version`}}}
	results := []AcceptanceCriterion{{
		Run: `go version`, Expected: `go version`, Passed: true, Output: `ok`,
		AcceptedHeadCommit: `aaa111`, AcceptedBaseCommit: `bbb222`, AcceptedChangeHash: `hash`,
	}}
	MergeAcceptanceResults(s, results, false)
	got := s.Acceptance[0]
	if got.AcceptedBaseCommit != `bbb222` || got.AcceptedChangeHash != `hash` {
		t.Errorf("content snapshot fields not merged: %+v", got)
	}
}
