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

// TestInit_HostWiringFlagsAreGone：--agents 是阶段 D 删除宿主层后保留的 hidden
// no-op——旧脚本传了不能失败，只能收到废弃警告，且绝不写 HOME/项目宿主产物。
func TestInit_HostWiringFlagsAreGone(t *testing.T) {
	home, _ := isolatedHomeEnv(t)
	dir := t.TempDir()

	out, _, code := runForge(t, dir, "init", "--agents", "cursor")
	if code != 0 {
		t.Fatalf("--agents 是 hidden no-op，退出码应为 0：%s", out)
	}
	if !strings.Contains(out, "--agents 已废弃（阶段 D 起没有宿主接线），本次忽略") {
		t.Errorf("应打印废弃警告：%s", out)
	}
	for _, name := range []string{".cursor", ".github", ".claude"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("init --agents 不应写项目宿主产物 %s", name)
		}
	}
	if got := snapshotTree(t, home); len(got) != 0 {
		t.Errorf("init --agents 不应写 HOME：%v", got)
	}
}

// TestIntegrations_EnableAndDisable：显式 enable 才写宿主配置与标记；disable 摘标记。
