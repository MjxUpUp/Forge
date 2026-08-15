// init_suggest_autotakeover_test.go — E2E for the plugin auto-takeover path of the
// init-suggest SessionStart hook: with the forge plugin user-level installed (simulated
// installed_plugins.json), opening a fresh git project must silently register it (real
// forge init via PATH), while forge suggest decline keeps the project out (per-project
// opt-out beats plugin-wide default-on).
//
// Unlike internal/hooks/initsuggest_test.go (which stubs the forge command), this runs
// the REAL forge binary — asserting the full loop: hook script → forge init → global
// registry membership (forge status exit 0) with ZERO writes into the project dir.
//
// init_suggest_autotakeover_test.go — init-suggest SessionStart hook 的 plugin 自动接管
// 路径 E2E：forge plugin 已 user-level 安装（伪造 installed_plugins.json）时，在新 git
// 项目打开会话须静默登记该项目（经 PATH 调真 forge init），而 forge suggest decline
// 保持项目退出（每项目退出权高于 plugin 级默认开启）。
//
// 区别于 internal/hooks/initsuggest_test.go（stub 了 forge 命令），这里跑真实 forge
// 二进制——断言完整闭环：hook 脚本 → forge init → 全局注册表成员（forge status
// exit 0），且项目目录零写入。
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// fakePluginInstalled writes a minimal installed_plugins.json (forge@<marketplace>,
// scope=user) into a fresh claude home and returns its path — the exact shape
// hooks.IsClaudePluginInstalledAt scans for.
//
// fakePluginInstalled 往全新 claude home 写最小 installed_plugins.json
// （forge@<marketplace>，scope=user）并返回路径——正是 hooks.IsClaudePluginInstalledAt
// 扫描的形态。
func fakePluginInstalled(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir plugins dir: %v", err)
	}
	registry := `{"plugins":{"forge@forge":[{"scope":"user"}]}}`
	if err := os.WriteFile(filepath.Join(dir, "installed_plugins.json"), []byte(registry), 0644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
	return home
}

// runInitSuggestScript writes the real InitSuggestHook to a temp .sh and runs it with
// bash under the given env (PATH resolves the real forge binary; HOME isolated by TestMain).
// Returns combined output; the hook is designed to exit 0.
//
// runInitSuggestScript 把真实 InitSuggestHook 写到临时 .sh，用 bash 在给定 env 下运行
// （PATH 解析真实 forge 二进制；HOME 由 TestMain 隔离）。返回合并输出；hook 设计为
// exit 0。
func runInitSuggestScript(t *testing.T, proj, tag, claudeHome string, extraEnv ...string) string {
	t.Helper()
	scriptFile := filepath.Join(t.TempDir(), "init-suggest.sh")
	if err := os.WriteFile(scriptFile, []byte(hooks.InitSuggestHook), 0644); err != nil {
		t.Fatalf("write hook script: %v", err)
	}
	cmd := exec.Command("bash", scriptFile)
	cmd.Dir = proj
	cmd.Env = append(os.Environ(),
		`FORGE_CWD=`+proj,
		`FORGE_CWD_TAG=`+tag,
		`CLAUDE_CONFIG_DIR=`+claudeHome,
		`PATH=`+filepath.Dir(forgeBin)+string(os.PathListSeparator)+os.Getenv(`PATH`),
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("init-suggest hook exited non-zero (script bug): err=%v, out=%s", err, out)
	}
	return string(out)
}

// TestInitSuggestPluginAutoTakeover: plugin installed + fresh git project → hook silently
// runs a real forge init; the project becomes a registry member (forge status exit 0),
// the project dir stays unwritten (zero-project-write), and the second session start is
// silent (member branch, no repeat init).
//
// TestInitSuggestPluginAutoTakeover：plugin 已装 + 全新 git 项目 → hook 静默跑真
// forge init；项目成为注册表成员（forge status exit 0），项目目录零写入，第二次
// 会话启动静默（成员分支，不重复 init）。
func TestInitSuggestPluginAutoTakeover(t *testing.T) {
	proj := t.TempDir()
	git(t, proj, "init")
	claudeHome := fakePluginInstalled(t)

	out := runInitSuggestScript(t, proj, "tag_e2e_takeover", claudeHome)
	if !strings.Contains(out, "plugin auto-takeover") {
		t.Fatalf("expected plugin auto-takeover line, got: %s", out)
	}

	// The full loop proof: the project is NOW a registry member (real forge status
	// must exit 0), and the project dir was never written (zero-project-write contract).
	//
	// 完整闭环证明：项目现在已是注册表成员（真 forge status 必须 exit 0），且项目
	// 目录从未被写入（零项目写入契约）。
	if _, err := forgeErr(t, proj, "status"); err != nil {
		t.Errorf("project should be registered after auto-takeover (forge status exit 0), got err: %v", err)
	}
	if _, err := os.Stat(filepath.Join(proj, ".forge")); err == nil {
		t.Errorf("auto-takeover must keep zero project writes, but .forge/ was created in %s", proj)
	}

	// Second session start: member branch → silent takeover line, no repeat.
	//
	// 第二次会话启动：成员分支 → 无接管行，不重复。
	out2 := runInitSuggestScript(t, proj, "tag_e2e_takeover", claudeHome)
	if strings.Contains(out2, "plugin auto-takeover") {
		t.Errorf("second session must not re-init (member branch), got: %s", out2)
	}
}

// TestInitSuggestPluginDeclinedBlocksTakeover: a declined marker (forge suggest decline)
// keeps the project out even with the plugin installed — per-project opt-out beats
// plugin-wide default-on. No registration, no takeover line.
//
// TestInitSuggestPluginDeclinedBlocksTakeover：declined 标记（forge suggest decline）
// 让项目即使 plugin 已装也保持退出——每项目退出权高于 plugin 级默认开启。不登记、
// 无接管行。
func TestInitSuggestPluginDeclinedBlocksTakeover(t *testing.T) {
	proj := t.TempDir()
	git(t, proj, "init")
	claudeHome := fakePluginInstalled(t)

	// Write the declined marker exactly like forge suggest decline does (marker content
	// is the literal string, under the isolated HOME's .forge/.init-suggested/<tag>).
	//
	// 与 forge suggest decline 同款写 declined 标记（标记内容为字面量字符串，落
	// 隔离 HOME 的 .forge/.init-suggested/<tag>）。
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home: %v", err)
	}
	markerDir := filepath.Join(home, ".forge", ".init-suggested")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	tag := "tag_e2e_declined"
	if err := os.WriteFile(filepath.Join(markerDir, tag), []byte("declined"), 0644); err != nil {
		t.Fatalf("write declined marker: %v", err)
	}

	out := runInitSuggestScript(t, proj, tag, claudeHome)
	if strings.Contains(out, "plugin auto-takeover") {
		t.Errorf("declined project must not be auto-taken-over, got: %s", out)
	}
	if _, err := forgeErr(t, proj, "status"); err == nil {
		t.Errorf("declined project must stay unregistered (forge status non-zero), but status exited 0")
	}

	// Control project (same claudeHome, no marker): takeover MUST fire — without this,
	// the two assertions above also pass when plugin detection is broken entirely
	// (falls to prompt mode, declined marker silences, status stays non-zero) — a
	// false-green the control side rules out.
	//
	// 对照项目（同 claudeHome、无标记）：接管必须发生——没有它，上面两条断言在
	// plugin 检测完全失效时也通过（落提示模式，declined 静默，status 非零）——
	// 对照侧排除这种假绿。
	control := t.TempDir()
	git(t, control, "init")
	cout := runInitSuggestScript(t, control, "tag_e2e_declined_control", claudeHome)
	if !strings.Contains(cout, "plugin auto-takeover") {
		t.Errorf("control project (no marker, plugin installed) must be auto-taken-over — otherwise the declined assertions above are a false green. got: %s", cout)
	}
}
