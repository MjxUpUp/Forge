package specroot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeContract 在 root/spec/forge.yaml 写内容（root 不存在则创建）。
func writeContract(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, MarkerName), []byte(body), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
}

// TestFind_FromNestedDir：契约目录在仓库根，从深层子目录向上找到。
func TestFind_FromNestedDir(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"1\"\n")
	deep := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := Find(deep)
	if !ok {
		t.Fatal("Find 未命中 spec/forge.yaml")
	}
	if filepath.Clean(got) != filepath.Clean(root) {
		t.Fatalf("Find=%q, want %q", got, root)
	}
}

// TestFind_StopsAtGitRoot：外层目录有契约，但内层是独立 git 仓库——不越界激活。
func TestFind_StopsAtGitRoot(t *testing.T) {
	outer := t.TempDir()
	writeContract(t, outer, "version: \"1\"\n")
	inner := filepath.Join(outer, "nested-repo")
	if err := os.MkdirAll(filepath.Join(inner, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(inner, "src")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if got, ok := Find(sub); ok {
		t.Fatalf("Find 越界命中了外层契约：%q", got)
	}
}

// TestFind_GitRootItselfWithContract：契约就在 git 仓库根（同目录含 .git）也应命中。
func TestFind_GitRootItselfWithContract(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeContract(t, root, "version: \"1\"\n")

	if _, ok := Find(root); !ok {
		t.Fatal("仓库根自带契约时 Find 应命中")
	}
}

// TestFind_NoContract：无标记返回 false，不报错。
func TestFind_NoContract(t *testing.T) {
	if _, ok := Find(t.TempDir()); ok {
		t.Fatal("空目录不该命中契约")
	}
}

// TestLoad_Defaults：缺省 profile=mini、version=1。
func TestLoad_Defaults(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "profile: \"\"\n")

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Profile != ProfileStandard {
		t.Errorf("Profile=%q, want %q（缺省与全局档位一致）", c.Profile, ProfileStandard)
	}
	if c.Version != CurrentVersion {
		t.Errorf("Version=%q, want %q", c.Version, CurrentVersion)
	}
}

// TestLoad_ExplicitProfile：显式档位与大小写归一。
func TestLoad_ExplicitProfile(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"1\"\nprofile: FULL\n")

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Profile != ProfileFull {
		t.Errorf("Profile=%q, want %q", c.Profile, ProfileFull)
	}
}

// TestLoad_InvalidProfile：拼错档位必须报错，不静默降级。
func TestLoad_InvalidProfile(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "profile: minni\n")

	if _, err := Load(root); err == nil {
		t.Fatal("无效 profile 应报错")
	}
}

// TestLoad_UnsupportedVersion：未知主版本报错。
func TestLoad_UnsupportedVersion(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"2\"\n")

	if _, err := Load(root); err == nil {
		t.Fatal("未知 version 应报错")
	}
}

// TestFind_StopsAtGitFile：worktree/submodule 的 .git 是「文件」而非目录，
// 同样是仓库边界——实现用 exists 而非 IsDir，这条钉住那个区别。
func TestFind_StopsAtGitFile(t *testing.T) {
	outer := t.TempDir()
	writeContract(t, outer, "version: \"1\"\n")
	inner := filepath.Join(outer, "linked-worktree")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, ".git"),
		[]byte("gitdir: /elsewhere/.git/worktrees/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := Find(inner); ok {
		t.Fatalf("worktree 不应被外层契约激活，got %q", got)
	}
}

// TestLoad_MinorVersionAccepted：version "1.1" 属主版本 1，按主版本兼容接受。
func TestLoad_MinorVersionAccepted(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"1.1\"\n")

	c, err := Load(root)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Version != "1.1" {
		t.Fatalf("Version=%q, want 1.1", c.Version)
	}
}

// TestLoad_Missing：无文件返回 ErrNoContract。
func TestLoad_Missing(t *testing.T) {
	if _, err := Load(t.TempDir()); !errors.Is(err, ErrNoContract) {
		t.Fatalf("err=%v, want ErrNoContract", err)
	}
}

func TestLoad_HooksDefaultOn(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"1\"\nprofile: mini\n")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hooks != HooksOn {
		t.Fatalf("缺省 hooks 应为 on，得到 %q", c.Hooks)
	}
}

func TestLoad_HooksOff(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"1\"\nhooks: OFF\n")
	c, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if c.Hooks != HooksOff {
		t.Fatalf("hooks 应归一为 off，得到 %q", c.Hooks)
	}
}

func TestLoad_RejectsUnknownHooks(t *testing.T) {
	root := t.TempDir()
	writeContract(t, root, "version: \"1\"\nhooks: sometimes\n")
	if _, err := Load(root); err == nil {
		t.Fatal("hooks 写错应当报错，而不是静默按默认值跑")
	}
}

func TestLoad_RejectsUnknownField(t *testing.T) {
	root := t.TempDir()
	// hooks 拼错成 hook：静默忽略会让人以为关掉了执法，实际还在拦。
	writeContract(t, root, "version: \"1\"\nhook: off\n")
	if _, err := Load(root); err == nil {
		t.Fatal("未知键 hook 应当报错，而不是静默按缺省跑")
	}
}
