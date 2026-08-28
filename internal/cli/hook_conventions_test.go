package cli

// hook_conventions_test.go —— conventions-profile 层 2 两个注入 hook
// （conventions-context / conventions-write，hook_conventions.go）的守卫。
// 与 hook_track_test.go 同款三维度：checklog 观察落盘（CheckConventionsInject）；
// 注入只在应发时发、永不阻断；静默契约（无档案→静默、非代码文件→静默、
// marker 门控：SessionStart 每会话一次 / PostCompact 恒注入 / 每目录一次）。
//
// hook_conventions_test.go — guards for the two conventions-profile layer-2
// injection hooks (conventions-context / conventions-write, hook_conventions.go).
// Same three dimensions as hook_track_test.go: the checklog observation lands
// (CheckConventionsInject); the injection fires only when it should and never
// blocks; silence contracts hold (no profile → silent, non-code file → silent,
// marker gating: SessionStart once per session / PostCompact always / once per
// directory).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// convTestProject isolates FORGE_DATA_HOME and returns a temp project root with
// a declared AGENTS.md + go.mod, ready for profiling.
//
// convTestProject 隔离 FORGE_DATA_HOME 并返回带 AGENTS.md + go.mod 的临时
// 项目根，可直接建档。
func convTestProject(t *testing.T) string {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	convWrite(t, root, "AGENTS.md", "# agents\nerror handling: always wrap with fmt.Errorf %w\n")
	convWrite(t, root, "go.mod", "module example.com/x\n\ngo 1.25\n")
	return root
}

// convWrite writes rel under root (parent dirs auto-made).
func convWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// resetConvMarkers removes the session's marker dir under the real os.TempDir()
// (production lifespan choice) before and after each test — cross-run isolation.
//
// resetConvMarkers 在每个测试前后删除真实 os.TempDir() 下该 session 的 marker
// 目录（生产寿命选择）——跨运行隔离。
func resetConvMarkers(t *testing.T, sessionID string) {
	t.Helper()
	dir := filepath.Join(conventionsMarkerDir(), util.SanitizeSessionID(sessionID))
	_ = os.RemoveAll(dir)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
}

// convProfile builds + persists the profile and digest for root.
//
// convProfile 为 root 建档（档案 + 摘要）。
func convProfile(t *testing.T, root string) *conventions.Profile {
	t.Helper()
	p, err := conventions.Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	dataDir := forgedata.DataDirFor(root)
	if err := conventions.SaveProfile(dataDir, p); err != nil {
		t.Fatal(err)
	}
	if err := conventions.SaveSummary(dataDir, conventions.GenerateSummary(p)); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestConventionsContextHook_InjectsDigestAndGatesByMarker pins the digest
// injection: lands on SessionStart with the checklog observation, marker-gated
// to once per session, but ALWAYS fires on PostCompact (compaction just wiped
// the context the digest was part of).
//
// TestConventionsContextHook_InjectsDigestAndGatesByMarker 钉住摘要注入：
// SessionStart 落注入与 checklog 观察，marker 每会话一次；PostCompact 恒注入
// （压缩刚清掉摘要所在的上下文）。
func TestConventionsContextHook_InjectsDigestAndGatesByMarker(t *testing.T) {
	root := convTestProject(t)
	convProfile(t, root)
	resetConvMarkers(t, "sess-cc-1")

	in := HookInput{HookEventName: "SessionStart", SessionID: "sess-cc-1"}
	first := captureStdout(t, func() {
		if err := runConventionsContextHook(in, root, "test", ""); err != nil {
			t.Fatalf("context hook must never error: %v", err)
		}
	})
	if !strings.Contains(first, "conventions profile") || !strings.Contains(first, "AGENTS.md") {
		t.Fatalf("SessionStart injection missing digest:\n%s", first)
	}
	if strings.Contains(first, `"decision"`) {
		t.Fatalf("advisory hook must not emit a decision field:\n%s", first)
	}
	entries := findTrackEntries(t, root, checklog.CheckConventionsInject)
	if len(entries) != 1 {
		t.Fatalf("checklog entries = %d, want 1", len(entries))
	}
	if entries[0].Meta["event"] != "SessionStart" || entries[0].Meta["stale"] != "false" {
		t.Fatalf("entry meta = %v", entries[0].Meta)
	}

	// 同 session 的第二次 SessionStart（resume 重发）：marker 静默。
	second := captureStdout(t, func() { _ = runConventionsContextHook(in, root, "test", "") })
	if second != "" {
		t.Fatalf("marker-gated SessionStart must be silent, got:\n%s", second)
	}

	// PostCompact：无视 marker，恒注入（上下文刚被清空）。
	pc := captureStdout(t, func() {
		_ = runConventionsContextHook(HookInput{HookEventName: "PostCompact", SessionID: "sess-cc-1"}, root, "test", "")
	})
	if !strings.Contains(pc, "conventions profile") {
		t.Fatalf("PostCompact must re-inject the digest, got:\n%s", pc)
	}
}

// TestConventionsContextHook_StaleDigestVisible pins that staleness rides the
// injection itself (a stale digest confidently stating outdated rules is worse
// than none — it must be visible, not silent).
//
// TestConventionsContextHook_StaleDigestVisible 钉住过期状态随注入可见
// （过期摘要自信陈述旧规则比没有更糟——必须可见，不能静默）。
func TestConventionsContextHook_StaleDigestVisible(t *testing.T) {
	root := convTestProject(t)
	convProfile(t, root)
	resetConvMarkers(t, "sess-cc-2")
	convWrite(t, root, "AGENTS.md", "# agents v2 — rules changed\n")

	out := captureStdout(t, func() {
		_ = runConventionsContextHook(HookInput{HookEventName: "SessionStart", SessionID: "sess-cc-2"}, root, "test", "")
	})
	if !strings.Contains(out, "STALE") {
		t.Fatalf("stale digest must be visible in the injection, got:\n%s", out)
	}
	entries := findTrackEntries(t, root, checklog.CheckConventionsInject)
	if len(entries) != 1 || entries[0].Meta["stale"] != "true" {
		t.Fatalf("stale observation wrong: %+v", entries)
	}
}

// TestConventionsContextHook_SuggestsInitOnce pins the adoption path: a repo
// that DECLARES conventions but has no profile gets a once-per-session factual
// suggestion; a repo with nothing declared stays fully silent (no observation,
// no marker).
//
// TestConventionsContextHook_SuggestsInitOnce 钉住采纳路径：已声明规范而无
// 档案的仓库获得每会话一次的事实性建议；什么都没声明的仓库完全静默
// （无观察、无 marker）。
func TestConventionsContextHook_SuggestsInitOnce(t *testing.T) {
	root := convTestProject(t) // 有 AGENTS.md，无档案
	resetConvMarkers(t, "sess-cc-3")

	in := HookInput{HookEventName: "SessionStart", SessionID: "sess-cc-3"}
	first := captureStdout(t, func() { _ = runConventionsContextHook(in, root, "test", "") })
	if !strings.Contains(first, "forge conventions init") || !strings.Contains(first, "AGENTS.md") {
		t.Fatalf("missing init suggestion:\n%s", first)
	}
	entries := findTrackEntries(t, root, checklog.CheckConventionsInject)
	if len(entries) != 1 || entries[0].Meta["suggestion"] != "init" {
		t.Fatalf("suggestion observation missing/wrong: %+v", entries)
	}
	again := captureStdout(t, func() { _ = runConventionsContextHook(in, root, "test", "") })
	if again != "" {
		t.Fatalf("suggestion must fire once per session, got:\n%s", again)
	}

	// 无任何声明的仓库：完全静默，无观察条目。
	bare := trackTestProject(t)
	resetConvMarkers(t, "sess-cc-4")
	out := captureStdout(t, func() {
		_ = runConventionsContextHook(HookInput{HookEventName: "SessionStart", SessionID: "sess-cc-4"}, bare, "test", "")
	})
	if out != "" {
		t.Fatalf("repo with nothing declared must be silent, got:\n%s", out)
	}
	if entries := findTrackEntries(t, bare, checklog.CheckConventionsInject); len(entries) != 0 {
		t.Fatalf("silent path must not record: %d entries", len(entries))
	}
}

// TestConventionsWriteHook_PointersAndExemplars pins the write-time injection:
// instruction pointers + sibling exemplars for the first source write in a
// directory; once per directory per session; silent for non-code files and for
// unadopted projects.
//
// TestConventionsWriteHook_PointersAndExemplars 钉住写入时刻注入：每目录首个
// 源码写入获得规范指针 + 同目录范例；每目录每会话一次；非代码文件与未采纳
// 项目静默。
func TestConventionsWriteHook_PointersAndExemplars(t *testing.T) {
	root := convTestProject(t)
	convProfile(t, root)
	convWrite(t, root, "internal/srv/alpha.go", "package srv\n")
	convWrite(t, root, "internal/srv/alpha_test.go", "package srv\n")
	resetConvMarkers(t, "sess-cw-1")

	writeIn := func(file string) HookInput {
		raw, _ := json.Marshal(map[string]string{"file_path": file})
		return HookInput{
			HookEventName: "PreToolUse",
			SessionID:     "sess-cw-1",
			ToolName:      "Write",
			ToolInput:     raw,
		}
	}
	target := filepath.Join(root, "internal", "srv", "new.go")
	out := captureStdout(t, func() {
		if err := runConventionsWriteHook(writeIn(target), root, "test", ""); err != nil {
			t.Fatalf("write hook must never error: %v", err)
		}
	})
	// Windows 分隔符归一：注入里的 repo 相对路径在 Windows 是反斜杠
	//（CI 三平台红过一轮，2026-08-28）——断言统一按正斜杠表述。
	//
	// Normalize separators: repo-relative paths in the injection carry
	// backslashes on Windows (a CI round caught this, 2026-08-28) —
	// assertions state forward slashes.
	out = filepath.ToSlash(out)
	if !strings.Contains(out, "AGENTS.md") {
		t.Fatalf("injection missing instruction pointer:\n%s", out)
	}
	if !strings.Contains(out, "internal/srv/alpha.go") {
		t.Fatalf("injection missing sibling exemplar:\n%s", out)
	}
	if strings.Contains(out, "alpha_test.go") {
		t.Fatalf("test file must not be offered as an exemplar:\n%s", out)
	}
	entries := findTrackEntries(t, root, checklog.CheckConventionsInject)
	if len(entries) != 1 || entries[0].Meta["dir"] != "internal/srv" {
		t.Fatalf("write observation wrong: %+v", entries)
	}

	// 同目录第二次写：静默。
	again := captureStdout(t, func() {
		_ = runConventionsWriteHook(writeIn(filepath.Join(root, "internal", "srv", "other.go")), root, "test", "")
	})
	if again != "" {
		t.Fatalf("per-directory gating failed, got:\n%s", again)
	}

	// 非代码文件（文档）：静默、无观察。
	docOut := captureStdout(t, func() {
		_ = runConventionsWriteHook(writeIn(filepath.Join(root, "docs", "guide.md")), root, "test", "")
	})
	if docOut != "" {
		t.Fatalf("non-code write must be silent, got:\n%s", docOut)
	}

	// 项目根外的目标（绝对用户路径绝不搭注入的便车）：静默。
	outside := captureStdout(t, func() {
		_ = runConventionsWriteHook(writeIn(filepath.Join(t.TempDir(), "elsewhere.go")), root, "test", "")
	})
	if outside != "" {
		t.Fatalf("out-of-root write must be silent, got:\n%s", outside)
	}

	// 未采纳（无档案）的仓库：静默。
	bare := trackTestProject(t)
	convWrite(t, bare, "internal/x/a.go", "package x\n")
	bareOut := captureStdout(t, func() {
		raw, _ := json.Marshal(map[string]string{"file_path": filepath.Join(bare, "internal", "x", "b.go")})
		_ = runConventionsWriteHook(HookInput{HookEventName: "PreToolUse", SessionID: "sess-cw-2", ToolName: "Edit", ToolInput: raw}, bare, "test", "")
	})
	if bareOut != "" {
		t.Fatalf("unadopted project must be silent, got:\n%s", bareOut)
	}
}

// TestConventionsWriteHook_ApplyPatchSynthesis pins the codex path: apply_patch
// tool_input carries {command: <patch>} with NO file_path — the hook must
// synthesize the target from the FIRST patch header (shared applyPatchFilePath)
// and inject for it. This was the adversarial-review gap closed in
// conventions-followups.
//
// TestConventionsWriteHook_ApplyPatchSynthesis 钉住 codex 路径：apply_patch 的
// tool_input 只带 {command: <patch>}、无 file_path——hook 必须经首个 patch 头
// （共享 applyPatchFilePath）合成目标并注入。这是对抗审查发现、在
// conventions-followups 修复的缺口。
func TestConventionsWriteHook_ApplyPatchSynthesis(t *testing.T) {
	root := convTestProject(t)
	convProfile(t, root)
	convWrite(t, root, "internal/srv/alpha.go", "package srv\n")
	resetConvMarkers(t, "sess-cw-patch")

	patch := "*** Begin Patch\n*** Update File: internal/srv/new.go\n@@\n package srv\n-x\n+y\n*** End Patch\n"
	raw, _ := json.Marshal(map[string]string{"command": patch})
	in := HookInput{
		HookEventName: "PreToolUse",
		SessionID:     "sess-cw-patch",
		ToolName:      "apply_patch",
		ToolInput:     raw,
	}
	out := captureStdout(t, func() {
		if err := runConventionsWriteHook(in, root, "test", ""); err != nil {
			t.Fatalf("write hook must never error: %v", err)
		}
	})
	out = filepath.ToSlash(out) // Windows 分隔符归一（同上）
	if !strings.Contains(out, "internal/srv/new.go") {
		t.Fatalf("apply_patch target must be synthesized and injected, got:\n%s", out)
	}
	if !strings.Contains(out, "internal/srv/alpha.go") {
		t.Fatalf("sibling exemplar missing on the synthesized path:\n%s", out)
	}
}
