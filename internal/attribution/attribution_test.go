package attribution

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/worktree"
)

// Real temporary git repos (t.TempDir + git init): reconciliation is a join between
// `git status` reality and the ledger — mocks cannot verify porcelain parsing or
// rename handling. Same style as internal/review's stamp tests.
//
// 真实临时 git 仓库（t.TempDir + git init）：对账是 git status 现实与台账的 join——
// mock 验证不了 porcelain 解析与 rename 处理。与 internal/review 的 stamp 测试同款。
func initGitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir()) // isolate DataDir from real ~/.forge
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte(""), 0o644)
	git("add", "-A")
	git("commit", "-q", "-m", "init")
	return dir
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReconcile_AttributionMatrix is the T3 correctness core: this-session edits,
// other-session edits, and unattributed files must split exactly — B's window never
// inherits A's WIP, and A's WIP surfaces as an orphan in B's view (honest exposure),
// never as B's attribution.
//
// TestReconcile_AttributionMatrix 是 T3 的正确性核心：本会话编辑、他会话编辑、无归属
// 文件必须精确三分——B 的窗口绝不继承 A 的 WIP，A 的 WIP 在 B 的视图里以无主暴露
// （诚实暴露），绝不记成 B 的归属。
func TestReconcile_AttributionMatrix(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	write(t, dir, "b.go", "package b\n")
	write(t, dir, "orphan.go", "package orphan\n")

	base := time.Now().Add(-time.Hour)
	Record(dir,
		Event{Ts: base, Sid: "sess-a", Kind: KindWrite, Path: "a.go"},
		Event{Ts: base.Add(time.Minute), Sid: "sess-b", Kind: KindEdit, Path: "b.go"},
		// Stale history for a path no longer changed: must not leak into the view.
		//
		// 已不在变更集里的历史路径：不得泄漏进视图。
		Event{Ts: base, Sid: "sess-a", Kind: KindWrite, Path: "gone.go"},
	)

	v := Reconcile(dir)
	if len(v.Changed) != 3 {
		t.Fatalf("changed 集应 3 项, got %v", v.Changed)
	}
	if got := v.BySession["sess-a"]; len(got) != 1 || got[0] != "a.go" {
		t.Errorf("sess-a 应只归属 a.go, got %v", got)
	}
	if got := v.BySession["sess-b"]; len(got) != 1 || got[0] != "b.go" {
		t.Errorf("sess-b 应只归属 b.go, got %v", got)
	}
	if len(v.Orphans) != 1 || v.Orphans[0] != "orphan.go" {
		t.Errorf("orphan.go 应为唯一无主项, got %v", v.Orphans)
	}
	if r := v.AttributionRate(); r != 2.0/3.0 {
		t.Errorf("覆盖率应 2/3, got %v", r)
	}

	// Last-writer-wins: sess-b later rewrites a.go → a.go moves to sess-b.
	//
	// 最后写入者胜：sess-b 随后重写 a.go → a.go 归 sess-b。
	Record(dir, Event{Ts: base.Add(2 * time.Minute), Sid: "sess-b", Kind: KindWrite, Path: "a.go"})
	v = Reconcile(dir)
	if got := v.BySession["sess-a"]; len(got) != 0 {
		t.Errorf("被重写后 sess-a 不应再持有 a.go, got %v", got)
	}
	if got := v.BySession["sess-b"]; len(got) != 2 {
		t.Errorf("sess-b 应持有 a.go+b.go, got %v", got)
	}
}

// TestSessionTouched_FastPredicate: the trigger-condition predicate returns the session's
// ledger union regardless of current git state.
//
// TestSessionTouched_FastPredicate：触发条件谓词返回会话的台账并集，与当前 git 状态
// 无关。
func TestSessionTouched_FastPredicate(t *testing.T) {
	dir := initGitRepo(t)
	Record(dir, Event{Ts: time.Now(), Sid: "s1", Kind: KindWrite, Path: "x/y.go"})
	if !SessionTouched(dir, "s1")["x/y.go"] {
		t.Error("s1 应含 x/y.go")
	}
	if len(SessionTouched(dir, "s2")) != 0 {
		t.Error("s2 应为空集")
	}
}

// TestRecordHookEvent_ToolsAndDegradation pins the dispatcher seam: Write/Edit/patch/Bash
// produce the right ledger shapes; empty session and non-PostToolUse events record
// nothing (no-identity hosts degrade to orphan-at-reconcile by design).
//
// TestRecordHookEvent_ToolsAndDegradation 钉住分发器挂点：Write/Edit/patch/Bash 产出
// 正确台账形状；空 session 与非 PostToolUse 事件不记账（无身份宿主按设计在对账时
// 降级为无主）。
func TestRecordHookEvent_ToolsAndDegradation(t *testing.T) {
	dir := initGitRepo(t)
	call := func(event, sid, tool, input string) {
		RecordHookEvent(dir, event, sid, tool, json.RawMessage(input))
	}
	call("PostToolUse", "s1", "Write", `{"file_path":"w.go"}`)
	call("PostToolUse", "s1", "Edit", `{"file_path":"e.go"}`)
	call("PostToolUse", "s1", "apply_patch", `{"command":"*** Begin Patch\n*** Update File: p.go\n@@\n-x\n+y\n*** End Patch"}`)
	call("PostToolUse", "s1", "Bash", `{"command":"sed -i 's/x/y/' s.go && go build > out.log"}`)
	call("PostToolUse", "", "Write", `{"file_path":"nosid.go"}`)
	call("PreToolUse", "s1", "Write", `{"file_path":"pre.go"}`)
	call("PostToolUse", "s1", "Read", `{"file_path":"r.go"}`)

	touched := SessionTouched(dir, "s1")
	for _, want := range []string{"w.go", "e.go", "p.go", "s.go", "out.log"} {
		if !touched[want] {
			t.Errorf("s1 台账缺 %q（got %v）", want, touched)
		}
	}
	if len(touched) != 5 {
		t.Errorf("台账应恰 5 项（Read/PreToolUse/无 sid 不记）, got %v", touched)
	}
}

// TestBashWriteTargets_PrecisionFirst pins the conservative extractor: the recognized
// shapes extract; ambiguous shapes deliberately miss (degrade to orphan, never
// misattribute).
//
// TestBashWriteTargets_PrecisionFirst 钉住保守提取器：认识的形状提取；歧义形状刻意
// 漏掉（降级为无主，绝不错误归属）。
func TestBashWriteTargets_PrecisionFirst(t *testing.T) {
	cases := []struct {
		cmd  string
		want []string
	}{
		{"go test ./... > result.txt", []string{"result.txt"}},
		{"echo hi >> notes.md", []string{"notes.md"}},
		{"sed -i.bak 's/a/b/' main.go", []string{"main.go"}},
		{"sed -i -e 's/a/b/' -e 's/c/d/' f.go", []string{"f.go"}},
		{"cp src.go dst.go", []string{"dst.go"}},
		{"mv old.go new.go", []string{"new.go"}},
		{"tee log.txt", []string{"log.txt"}},
		{"go build ./...", nil},
		{"cat file.go", nil},
		{"echo $VAR >$(mktemp)", nil}, // 命令替换目标——刻意不解析
	}
	for _, c := range cases {
		got := bashWriteTargets(c.cmd)
		if len(got) != len(c.want) {
			t.Errorf("bashWriteTargets(%q) = %v, want %v", c.cmd, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("bashWriteTargets(%q)[%d] = %q, want %q", c.cmd, i, got[i], c.want[i])
			}
		}
	}
}

// TestRecordStopMetric_ThrottledAndShaped: first Stop records one observation-class entry
// with coverage Meta; an immediate second Stop is throttled silent.
//
// TestRecordStopMetric_ThrottledAndShaped：首个 Stop 落一条带覆盖率 Meta 的观察类
// 条目；紧随的第二个 Stop 被节流静默。
func TestRecordStopMetric_ThrottledAndShaped(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "a.go", "package a\n")
	Record(dir, Event{Ts: time.Now(), Sid: "s1", Kind: KindWrite, Path: "a.go"})

	RecordStopMetric(dir, "")
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatal(err)
	}
	var metric *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckAttribution {
			metric = &entries[i]
		}
	}
	if metric == nil {
		t.Fatalf("应落一条 attribution 观察条目, got %d entries", len(entries))
	}
	if metric.Meta[checklog.MetaKeyAttributionAttributed] != "1" ||
		metric.Meta[checklog.MetaKeyAttributionOrphans] != "0" ||
		metric.Meta[checklog.MetaKeyAttributionRate] != "1.0000" {
		t.Errorf("覆盖率 Meta 不符: %+v", metric.Meta)
	}

	RecordStopMetric(dir, "") // 节流窗口内：不得重复落章
	entries, _ = checklog.LoadAll(dir)
	count := 0
	for _, e := range entries {
		if e.Check == checklog.CheckAttribution {
			count++
		}
	}
	if count != 1 {
		t.Errorf("节流失败：attribution 条目应恰 1 条, got %d", count)
	}
}

// TestLedgerPerWorkspace: two workspaces of one project keep separate ledgers (wtid is
// path-keyed — the whole point of the triad).
//
// TestLedgerPerWorkspace：同项目两个 workspace 各持独立台账（wtid 按路径键控——
// 三元组的意义所在）。
func TestLedgerPerWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", home)
	ws1, ws2 := t.TempDir(), t.TempDir()
	if worktree.ID(ws1) == worktree.ID(ws2) {
		t.Fatal("不同路径的 wtid 必须不同")
	}
	Record(
		ws1, Event{Ts: time.Now(), Sid: "s", Kind: KindWrite, Path: "only-ws1.go"})
	if len(SessionTouched(ws2, "s")) != 0 {
		t.Error("ws2 不得看到 ws1 的台账")
	}
	if !SessionTouched(ws1, "s")["only-ws1.go"] {
		t.Error("ws1 应看到自己的台账")
	}
}

// TestLedgerTTL_StaleEntriesExpire pins the 7d TTL (review HIGH): an event older than
// the ledger TTL must not win last-writer over the current task's unseen change — stale
// attribution is worse than a miss (a miss surfaces as a visible orphan).
//
// TestLedgerTTL_StaleEntriesExpire 钉住 7d TTL（review HIGH）：超 TTL 的事件不得作为
// 最后写入者胜过当前任务的漏记变更——陈旧归因比漏归属更糟（漏 = 无主 = 可见）。
func TestLedgerTTL_StaleEntriesExpire(t *testing.T) {
	dir := initGitRepo(t)
	write(t, dir, "p.go", "package p\n")
	Record(dir,
		Event{Ts: time.Now().Add(-8 * 24 * time.Hour), Sid: "zombie", Kind: KindWrite, Path: "p.go"},
		Event{Ts: time.Now().Add(-8 * 24 * time.Hour), Sid: "zombie", Kind: KindWrite, Path: "gone-old.go"},
	)
	v := Reconcile(dir)
	if len(v.BySession) != 0 {
		t.Fatalf("过期事件不应参与归属: %v", v.BySession)
	}
	if len(v.Orphans) != 1 || v.Orphans[0] != "p.go" {
		t.Fatalf("p.go 应降为无主（诚实暴露）: %v", v.Orphans)
	}
	if got := SessionTouched(dir, "zombie"); len(got) != 0 {
		t.Fatalf("过期事件不应进 SessionTouched: %v", got)
	}
}
