package cli

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestAdoptPayloadCwd pins the kimi plugin-hook cwd fix: kimi runs plugin hooks with the
// process cwd set to the plugin root (verified on kimi 0.31.0 — matches the docs "each
// hook runs with its working directory set to the plugin root"), so resolving the forge
// project from the process cwd fails and every project-scoped hook bails with a silent
// allow. The payload's cwd (the session's real project dir) must be adopted first.
//
// TestAdoptPayloadCwd 钉住 kimi 插件 hook 的 cwd 修复：kimi 以插件根为进程 cwd 拉起
// 插件 hook（0.31.0 实测，与文档「hook 以插件根为工作目录运行」一致），按进程 cwd
// 解析 forge 项目会失败、所有项目级 hook 静默放行。必须先采用 payload 的 cwd
// （会话真实项目目录）。
func TestAdoptPayloadCwd(t *testing.T) {
	// A non-project directory to simulate the plugin root.
	//
	// 非项目目录，模拟插件根
	pluginRoot := t.TempDir()
	t.Chdir(pluginRoot)

	if _, err := findProjectRoot(); err == nil {
		t.Fatal("从非项目目录（模拟插件根）解析项目根应失败")
	}

	projRoot, _ := forgedatatest.RealProject(t)
	if !adoptPayloadCwd(projRoot) {
		t.Fatal("有效项目目录应被采用（返回 true）")
	}
	// Compare directories by identity, not string form: os.Getwd may return the
	// physical path (macOS /var → /private/var symlink, Windows 8.3 short names)
	// while projRoot is the unresolved form.
	//
	// 按目录同一性比较而非字符串：os.Getwd 可能返回物理路径（macOS 的
	// /var → /private/var 符号链接、Windows 8.3 短名），而 projRoot 是未解析形式。
	wd, _ := os.Getwd()
	wdInfo, _ := os.Stat(wd)
	projInfo, _ := os.Stat(projRoot)
	if !os.SameFile(wdInfo, projInfo) {
		t.Errorf("chdir 后 cwd = %q, 应与 %q 同一目录", wd, projRoot)
	}
	if _, err := findProjectRoot(); err != nil {
		t.Errorf("采用 payload cwd 后应能解析项目根: %v", err)
	}

	// Same dir again → no-op (returns false, stays put).
	//
	// 同目录再调 → 无操作（返回 false，原地不动）
	if adoptPayloadCwd(projRoot) {
		t.Error("同目录应为无操作（返回 false）")
	}
}

// TestAdoptPayloadCwd_Invalid: empty, relative and nonexistent payload cwd values leave
// the process cwd untouched (fallback to the old behavior). Relative paths are rejected
// outright — they would resolve against the process cwd (the plugin root under kimi).
//
// TestAdoptPayloadCwd_Invalid：空值、相对路径与不存在的 payload cwd 不动进程 cwd
// （回落原行为）。相对路径直接拒绝——它会相对进程 cwd（kimi 下即插件根）解析。
func TestAdoptPayloadCwd_Invalid(t *testing.T) {
	before, _ := os.Getwd()
	if adoptPayloadCwd("") {
		t.Error("空 cwd 不应被采用")
	}
	if adoptPayloadCwd("relative/subdir") {
		t.Error("相对路径不应被采用")
	}
	if adoptPayloadCwd(`Z:\no\such\dir\anywhere`) {
		t.Error("不存在的目录不应被采用")
	}
	after, _ := os.Getwd()
	if after != before {
		t.Errorf("无效 cwd 不应改变进程 cwd: %q → %q", before, after)
	}
}

// TestKimiNormalize_PopulatesCwd: kimi's hook payload carries the session project in its
// cwd field — kimiNormalize must surface it into HookInput.Cwd, or adoptPayloadCwd has
// nothing to adopt.
//
// TestKimiNormalize_PopulatesCwd：kimi 的 hook payload 用 cwd 字段携带会话项目目录
// ——kimiNormalize 必须把它填进 HookInput.Cwd，否则 adoptPayloadCwd 无值可用。
func TestKimiNormalize_PopulatesCwd(t *testing.T) {
	var in HookInput
	kimiNormalize([]byte(kimiPreToolUsePayload), &in)
	if in.Cwd != "C:/proj" {
		t.Errorf("Cwd = %q, want %q", in.Cwd, "C:/proj")
	}
}

// TestRunHook_AdoptsPayloadCwd is the end-to-end wiring for the kimi plugin-hook fix:
// the process starts in a NON-project directory (simulating the plugin root), the hook
// payload carries the real project in its cwd field, and the project-scoped hook
// (tool-track) must take effect there — before the fix it bailed with a silent allow.
//
// TestRunHook_AdoptsPayloadCwd 是 kimi 插件 hook 修复的端到端接线：进程从非项目目录
// （模拟插件根）启动，hook payload 的 cwd 字段携带真实项目，项目级 hook
// （tool-track）必须在那里生效——修复前它会静默放行。
func TestRunHook_AdoptsPayloadCwd(t *testing.T) {
	projRoot, _ := forgedatatest.RealProject(t)

	// Start from a non-project directory (the plugin root under kimi).
	//
	// 从非项目目录（kimi 下的插件根）启动
	pluginRoot := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(pluginRoot)
	defer os.Chdir(originalWd)

	if _, err := findProjectRoot(); err == nil {
		t.Fatal("前置：模拟插件根应解析不到项目")
	}

	payload := `{"hook_event_name":"PostToolUse","session_id":"s-cwd","cwd":` +
		strconv.Quote(filepath.ToSlash(projRoot)) +
		`,"tool_name":"Read","tool_input":{"file_path":"src/main.go"}}`

	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(payload)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runHook(nil, []string{"tool-track"})

	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	toollogPath := filepath.Join(forgedata.DataDirFor(projRoot), "toollog.jsonl")
	data, err := os.ReadFile(toollogPath)
	if err != nil {
		t.Fatalf("tool-track 未在 payload.cwd 项目生效（toollog 未生成）: %v", err)
	}
	if !strings.Contains(string(data), `"tool_name":"Read"`) {
		t.Errorf("toollog 应含 tool_name=Read 条目, got: %s", data)
	}
}
