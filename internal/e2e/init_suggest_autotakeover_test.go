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

// TestInitSuggestDefaultAsk：P2 默认值翻转钉子——plugin 已装 + 全新 git 项目 +
// 无偏好配置 → hook 询问一次（不静默接管、不登记、项目零写入）。
func TestInitSuggestDefaultAsk(t *testing.T) {
	proj := t.TempDir()
	git(t, proj, "init")
	claudeHome := fakePluginInstalled(t)
	cfgHome := t.TempDir() // 隔离 config.json：无 FORGE_TAKEOVER env 时读这里

	out := runInitSuggestScript(t, proj, "tag_e2e_ask", claudeHome, `FORGE_DATA_HOME=`+cfgHome)
	if !strings.Contains(out, "询问用户是否让 forge 接管") {
		t.Fatalf("expected ask-once advisory under default ask, got: %s", out)
	}
	if _, err := forgeErr(t, proj, "status"); err == nil {
		t.Errorf("default ask must NOT register the project, but forge status exited 0")
	}
	if _, err := os.Stat(filepath.Join(proj, ".forge")); err == nil {
		t.Errorf("ask must keep zero project writes, but .forge/ was created in %s", proj)
	}

	// 第二次会话：suggested 标记 → 静默（问过即闭嘴）。
	out2 := runInitSuggestScript(t, proj, "tag_e2e_ask", claudeHome, `FORGE_DATA_HOME=`+cfgHome)
	if strings.Contains(out2, "询问用户") {
		t.Errorf("second session must be silent after suggested marker, got: %s", out2)
	}
}

// TestInitSuggestTakeoverAutoViaConfig：偏好落盘（forge config set takeover auto）
// 后静默接管——钉 config get --raw 在真二进制下的接线（env 路径由 unit stub 钉）。
func TestInitSuggestTakeoverAutoViaConfig(t *testing.T) {
	proj := t.TempDir()
	git(t, proj, "init")
	claudeHome := fakePluginInstalled(t)
	cfgHome := t.TempDir()

	// 真二进制落盘偏好（隔离 FORGE_DATA_HOME——config.json 与注册表同根）。
	if out, err := forgeEnv(t, cfgHome, proj, "config", "set", "takeover", "auto"); err != nil {
		t.Fatalf("config set takeover auto: %v (%s)", err, out)
	}

	out := runInitSuggestScript(t, proj, "tag_e2e_auto_cfg", claudeHome, `FORGE_DATA_HOME=`+cfgHome)
	if !strings.Contains(out, "takeover=auto: 已在") {
		t.Fatalf("expected silent takeover under persisted auto preference, got: %s", out)
	}
	if _, err := forgeEnv(t, cfgHome, proj, "status"); err != nil {
		t.Errorf("auto preference should register the project, got err: %v", err)
	}
	// 第二次会话：成员分支静默。
	out2 := runInitSuggestScript(t, proj, "tag_e2e_auto_cfg", claudeHome, `FORGE_DATA_HOME=`+cfgHome)
	if strings.Contains(out2, "takeover=auto") {
		t.Errorf("second session must be silent (member branch), got: %s", out2)
	}
}

// TestInitSuggestPluginDeclinedBlocksTakeover：declined 标记（forge suggest decline）
// 让项目即使 plugin 已装也保持退出——每项目退出权高于 plugin 级默认开启。不登记、
// 无接管行。
func TestInitSuggestPluginDeclinedBlocksTakeover(t *testing.T) {
	proj := t.TempDir()
	git(t, proj, "init")
	claudeHome := fakePluginInstalled(t)

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

	// 对照项目（同 claudeHome、无标记）：接管必须发生——没有它，上面两条断言在
	// plugin 检测完全失效时也通过（落提示模式，declined 静默，status 非零）——
	// 对照侧排除这种假绿。
	control := t.TempDir()
	git(t, control, "init")
	cout := runInitSuggestScript(t, control, "tag_e2e_declined_control", claudeHome, `FORGE_TAKEOVER=auto`)
	if !strings.Contains(cout, "takeover=auto: 已在") {
		t.Errorf("control project (no marker, explicit auto) must be auto-taken-over — otherwise the declined assertions above are a false green. got: %s", cout)
	}
}

// TestInitSuggestDeclinedBlocksAutoInitEnv：Project Policy Layer P1 的 G-1 修复钉子
// ——declined 标记必须拦住 FORGE_AUTO_INIT=1（原实现 AUTO_INIT 分支在 declined
// 检查之前，"显式 env 不拦 declined"语义自 P1 废除：退出不可被 env 穿透）。对照
// 项目（无标记 + AUTO_INIT）必须照常自动 init，排除 declined 断言的假绿。
func TestInitSuggestDeclinedBlocksAutoInitEnv(t *testing.T) {
	proj := t.TempDir()
	git(t, proj, "init")
	claudeHome := fakePluginInstalled(t)

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("user home: %v", err)
	}
	markerDir := filepath.Join(home, ".forge", ".init-suggested")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	tag := "tag_e2e_auto_init_declined"
	if err := os.WriteFile(filepath.Join(markerDir, tag), []byte("declined"), 0644); err != nil {
		t.Fatalf("write declined marker: %v", err)
	}

	out := runInitSuggestScript(t, proj, tag, claudeHome, `FORGE_AUTO_INIT=1`)
	if strings.Contains(out, "FORGE_AUTO_INIT=1: 已在") {
		t.Errorf("declined project must not be auto-inited even with FORGE_AUTO_INIT=1, got: %s", out)
	}
	if _, err := forgeErr(t, proj, "status"); err == nil {
		t.Errorf("declined project must stay unregistered under FORGE_AUTO_INIT=1, but forge status exited 0")
	}

	// 对照：无标记 + AUTO_INIT → 自动 init 必须发生（否则上面两条断言在
	// AUTO_INIT 分支整体失效时也通过——对照侧排除假绿）。
	control := t.TempDir()
	git(t, control, "init")
	cout := runInitSuggestScript(t, control, "tag_e2e_auto_init_control", claudeHome, `FORGE_AUTO_INIT=1`)
	if !strings.Contains(cout, "FORGE_AUTO_INIT=1: 已在") {
		t.Errorf("control project (no marker) must be auto-inited under FORGE_AUTO_INIT=1 — otherwise the declined assertions above are a false green. got: %s", cout)
	}
}

// forgeEnv runs a forge command with an isolated FORGE_DATA_HOME (config.json 与
// 注册表同根——偏好落盘与后续读取必须同一 store）。
func forgeEnv(t *testing.T, dataHome, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), `FORGE_DATA_HOME=`+dataHome)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
