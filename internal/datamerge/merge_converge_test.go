package datamerge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// merge_converge_test.go — the DataDir-level convergence anchor: two machine data
// dirs with overlapping/conflicting content, merged BOTH WAYS through the real
// Dirs path (TaskUnion + TrustResults + DedupExactLines — the project-import option
// set), must end up byte-identical across the whole tree, and a re-merge must be a
// no-op. This is the property a continuous two-machine sync loop relies on.
//
// merge_converge_test.go —— DataDir 级收敛锚：两台机器的数据目录带重叠/冲突内容，
// 经真实 Dirs 路径双向合并（TaskUnion + TrustResults + DedupExactLines——project
// import 的选项集），全树必须字节一致，且重复合并必须是 no-op。这是持续双机
// 同步循环依赖的性质。

// cpTree deep-copies a directory tree (regular files only — fixtures stay regular).
//
// cpTree 深拷贝目录树（仅普通文件——fixture 保持普通文件）。
func cpTree(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), raw, 0644)
	})
	if err != nil {
		t.Fatalf("cpTree: %v", err)
	}
	return dst
}

// treeBytes renders a directory tree as rel-path → content for byte comparison.
//
// treeBytes 把目录树渲染为 相对路径→内容 供字节比较。
func treeBytes(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		raw, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		out[rel] = raw
		return nil
	})
	if err != nil {
		t.Fatalf("treeBytes: %v", err)
	}
	return out
}

func treesEqual(t *testing.T, a, b map[string][]byte) bool {
	t.Helper()
	if len(a) != len(b) {
		return false
	}
	for k, va := range a {
		vb, ok := b[k]
		if !ok || !bytes.Equal(va, vb) {
			return false
		}
	}
	return true
}

// writeTask persists a TaskState fixture into dir/tasks/.
//
// writeTask 把 TaskState fixture 落进 dir/tasks/。
func writeTask(t *testing.T, dir string, s *taskpipeline.TaskState) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "tasks"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks", s.TaskRef+".json"), raw, 0644); err != nil {
		t.Fatalf("write task: %v", err)
	}
}

// TestDirs_TwoWaySyncConverges is THE convergence property at DataDir level.
//
// TestDirs_TwoWaySyncConverges 是 DataDir 级收敛性质本体。
func TestDirs_TwoWaySyncConverges(t *testing.T) {
	early := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	late := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)

	// machine A: task with decisions d1/d2, gate implement passed (early), one checklog line.
	a := t.TempDir()
	writeTask(t, a, &taskpipeline.TaskState{
		TaskRef: `feat-shared`, Branch: `feat-shared`,
		History:   []taskpipeline.TaskGateResult{{Gate: `task-implement`, Passed: true, CompletedAt: early}},
		Decisions: []taskpipeline.Decision{{ID: `d1`, Content: `x`, DecidedAt: early}, {ID: `d2`, Content: `y`, DecidedAt: early}},
	})
	os.WriteFile(filepath.Join(a, "checklog.jsonl"), []byte(`{"check":"task-guard","passed":true,"detail":"from-a","recorded_at":"2026-08-20T10:00:00Z"}`+"\n"), 0644)

	// machine B: same task further along (implement passed LATER + verify passed),
	// decisions d2 (same ID, same content) + d3; checklog shares A's line plus its own.
	b := t.TempDir()
	writeTask(t, b, &taskpipeline.TaskState{
		TaskRef: `feat-shared`, Branch: `feat-shared`,
		History: []taskpipeline.TaskGateResult{
			{Gate: `task-implement`, Passed: true, CompletedAt: late},
			{Gate: `task-verify`, Passed: true, CompletedAt: late},
		},
		Decisions: []taskpipeline.Decision{{ID: `d3`, Content: `z`, DecidedAt: late}, {ID: `d2`, Content: `y`, DecidedAt: early}},
	})
	os.WriteFile(filepath.Join(b, "checklog.jsonl"), []byte(
		`{"check":"task-guard","passed":true,"detail":"from-a","recorded_at":"2026-08-20T10:00:00Z"}`+"\n"+
			`{"check":"task-guard","passed":true,"detail":"from-b","recorded_at":"2026-08-21T10:00:00Z"}`+"\n"), 0644)

	// NoFromBackup: the from-dirs are disposable cpTree copies (the import-staging
	// precedent) — otherwise the whole-dir backup move nests .rekey-backup dirs into
	// the fixtures and second-resolution backup names collide across the two merges.
	//
	// NoFromBackup：from 目录是一次性 cpTree 副本（import staging 先例）——否则
	// 整目录备份搬移会把 .rekey-backup 嵌进 fixture，且秒级备份名在两次合并间撞名。
	opts := Options{TaskPolicy: TaskUnion, TrustResults: true, DedupExactLines: true, MergeConclusions: true, NoFromBackup: true}

	// B → A (Dirs consumes/moves the from-dir, so merge from a COPY).
	if _, err := Dirs(cpTree(t, b), a, opts); err != nil {
		t.Fatalf("B→A: %v", err)
	}
	// A(orig) → B.
	if _, err := Dirs(cpTree(t, a), b, opts); err != nil {
		t.Fatalf("A→B: %v", err)
	}

	ta, tb := treeBytes(t, a), treeBytes(t, b)
	if !treesEqual(t, ta, tb) {
		for k := range ta {
			if !bytes.Equal(ta[k], tb[k]) {
				t.Fatalf("divergent file %s:\nA=%s\nB=%s", k, ta[k], tb[k])
			}
		}
		t.Fatalf("tree file sets diverge: A=%v B=%v", keys(ta), keys(tb))
	}

	// Idempotency: merging B into A AGAIN changes nothing.
	before := treeBytes(t, a)
	if _, err := Dirs(cpTree(t, b), a, opts); err != nil {
		t.Fatalf("re-merge: %v", err)
	}
	if !treesEqual(t, before, treeBytes(t, a)) {
		t.Fatal("re-merge not idempotent")
	}
}

func keys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
