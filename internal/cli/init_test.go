package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/specroot"
)

// init_test.go —— Phase C 契约： forge init 只写仓库内产物，宿主接线是显式 opt-in。

// isolatedHomeEnv 把用户主目录、全局状态根与各宿主配置目录全指进临时目录，
// 让 init/integrations 的任何 HOME 写入都能被前后快照抓到。
func isolatedHomeEnv(t *testing.T) (home, data string) {
	t.Helper()
	home = t.TempDir()
	data = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_DATA_HOME", data)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	return home, data
}

// snapshotTree 返回 root 下全部文件（相对路径，含未跟踪新文件）；root 不存在时返回 nil。
func snapshotTree(t *testing.T, root string) []string {
	t.Helper()
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return rerr
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return files
}

// TestInit_WritesOnlyRepoProducts 是 Phase C 的核心判据：临时 HOME 跑 forge init，
// 主目录与全局状态根前后都必须是空的（含未跟踪新文件），仓库里只有契约产物。
func TestInit_WritesOnlyRepoProducts(t *testing.T) {
	home, data := isolatedHomeEnv(t)
	dir := t.TempDir()

	initProject(t, dir)

	for _, p := range []string{"spec/forge.yaml", "spec/changes/archive/.gitkeep", "AGENTS.md"} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(p))); err != nil {
			t.Fatalf("%s 未创建：%v", p, err)
		}
	}
	c, err := specroot.Load(dir)
	if err != nil {
		t.Fatalf("specroot.Load: %v", err)
	}
	if c.Profile != specroot.ProfileMini {
		t.Errorf("默认档位 = %q, want %q", c.Profile, specroot.ProfileMini)
	}

	// 不再写项目级 .forge/ 与宿主目录。
	for _, name := range []string{".forge", ".claude", ".codex", ".cursor"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("init 不应写项目级 %s", name)
		}
	}
	if got := snapshotTree(t, home); len(got) != 0 {
		t.Errorf("init 写了用户主目录（Phase C 禁止）：%v", got)
	}
	if got := snapshotTree(t, data); len(got) != 0 {
		t.Errorf("init 写了全局状态根（Phase C 禁止）：%v", got)
	}
}

// TestInit_IdempotentAndProfile：已声明仓库再跑 init 只同步指令层，不覆盖 spec。
func TestInit_IdempotentAndProfile(t *testing.T) {
	dir := t.TempDir()
	if err := initRepoContract(dir, specroot.ProfileStandard); err != nil {
		t.Fatalf("first init: %v", err)
	}
	specPath := filepath.Join(dir, "spec", "forge.yaml")
	before, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := initRepoContract(dir, specroot.ProfileMini); err != nil {
		t.Fatalf("second init: %v", err)
	}
	after, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("已声明仓库的 spec/forge.yaml 被覆盖：\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestInit_LiteMapsToMini：旧的接线档 lite 落到契约最小档 mini，并说明改动。
func TestInit_LiteMapsToMini(t *testing.T) {
	dir := t.TempDir()
	if err := initRepoContract(dir, "lite"); err != nil {
		t.Fatalf("init lite: %v", err)
	}
	c, err := specroot.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Profile != specroot.ProfileMini {
		t.Errorf("lite 应映射为 mini，实得 %q", c.Profile)
	}
}

// TestInit_DeprecatedFlagsAreNoOps：--project/--agents/--mode 仍被接受（旧脚本不炸），
// 但不再产生任何项目级 .forge/ 资产，并给出废弃警告。
func TestInit_DeprecatedFlagsAreNoOps(t *testing.T) {
	home, _ := isolatedHomeEnv(t)
	dir := t.TempDir()

	out, _, code := runForge(t, dir, "init", "--project", "--agents", "cursor,copilot", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init 退出 %d：%s", code, out)
	}
	for _, want := range []string{"--project 已废弃", "--agents 已废弃"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出缺废弃警告 %q：%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".forge")); !os.IsNotExist(err) {
		t.Error("--project 不应再写项目级 .forge/")
	}
	if got := snapshotTree(t, home); len(got) != 0 {
		t.Errorf("废弃 flag 不应把 init 变回写 HOME：%v", got)
	}
}

// TestIntegrations_EnableAndDisable：显式 enable 才写宿主配置与标记；disable 摘标记。
func TestIntegrations_EnableAndDisable(t *testing.T) {
	home, data := isolatedHomeEnv(t)
	dir := t.TempDir()
	initProject(t, dir)

	// init 之后、enable 之前：status 不该把用户主目录写回来（autoSync 闸门）。
	if out, _, code := runForge(t, dir, "status"); code != 0 {
		t.Fatalf("status 退出 %d：%s", code, out)
	}
	if got := snapshotTree(t, home); len(got) != 0 {
		t.Fatalf("无宿主接线时 status 写了用户主目录：%v", got)
	}

	stdout, _, code := runForge(t, dir, "integrations", "enable", "claude-code")
	if code != 0 {
		t.Fatalf("integrations enable 退出 %d：%s", code, stdout)
	}
	if !strings.Contains(stdout, "已接线") {
		t.Errorf("enable 输出缺接线确认：%s", stdout)
	}
	settings := filepath.Join(home, ".claude", "settings.json")
	raw, err := os.ReadFile(settings)
	if err != nil {
		t.Fatalf("~/.claude/settings.json 未写：%v", err)
	}
	if !strings.Contains(string(raw), "forge hook batch --event PreToolUse") {
		t.Error("用户级 settings.json 缺 forge hook 接线")
	}
	if _, err := os.Stat(filepath.Join(data, "integrations", "claude-code")); err != nil {
		t.Errorf("接线标记未写：%v", err)
	}

	stdout, _, code = runForge(t, dir, "integrations", "list")
	if code != 0 || !strings.Contains(stdout, "claude-code") || !strings.Contains(stdout, "已接线") {
		t.Errorf("integrations list 未显示已接线：%s", stdout)
	}

	stdout, _, code = runForge(t, dir, "integrations", "disable", "claude-code")
	if code != 0 || !strings.Contains(stdout, "已停用") {
		t.Errorf("integrations disable 失败：%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(data, "integrations", "claude-code")); !os.IsNotExist(err) {
		t.Error("disable 应摘掉接线标记")
	}
}

// TestIntegrations_RejectsUnknownHost：拼错的宿主名必须报错并列出可选值。
func TestIntegrations_RejectsUnknownHost(t *testing.T) {
	_, _ = isolatedHomeEnv(t)
	dir := t.TempDir()
	initProject(t, dir)

	out, _, code := runForge(t, dir, "integrations", "enable", "claud")
	if code == 0 {
		t.Fatalf("未知宿主应失败，实际退出 0：%s", out)
	}
	if !strings.Contains(out, "未知宿主") || !strings.Contains(out, "claude-code") {
		t.Errorf("错误信息应含未知宿主与可选清单：%s", out)
	}
}

// TestIntegrations_RequiresRepoContract：没有 spec/forge.yaml 时 enable 拒绝并指路 init。
func TestIntegrations_RequiresRepoContract(t *testing.T) {
	_, _ = isolatedHomeEnv(t)
	dir := t.TempDir()

	out, _, code := runForge(t, dir, "integrations", "enable", "claude-code")
	if code == 0 {
		t.Fatalf("仓库外 enable 应失败：%s", out)
	}
	if !strings.Contains(out, "forge init") {
		t.Errorf("错误信息应指路 forge init：%s", out)
	}
}
