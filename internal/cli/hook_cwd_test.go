package cli

import (
	"os"
	"testing"

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
	wd, _ := os.Getwd()
	if wd != projRoot {
		t.Errorf("chdir 后 cwd = %q, want %q", wd, projRoot)
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

// TestAdoptPayloadCwd_Invalid: empty and nonexistent payload cwd values leave the
// process cwd untouched (fallback to the old behavior).
//
// TestAdoptPayloadCwd_Invalid：空值与不存在的 payload cwd 不动进程 cwd（回落原行为）。
func TestAdoptPayloadCwd_Invalid(t *testing.T) {
	before, _ := os.Getwd()
	if adoptPayloadCwd("") {
		t.Error("空 cwd 不应被采用")
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
