package specroot

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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
	if !sameFile(t, got, root) {
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

// sameFile 比较两个路径是否指向同一目录：Find 返回真实路径（Windows 上 8.3
// 短名会被展开成长名），字符串比较会误报。
func sameFile(t *testing.T, a, b string) bool {
	t.Helper()
	ia, err := os.Stat(a)
	if err != nil {
		t.Fatalf("stat %s: %v", a, err)
	}
	ib, err := os.Stat(b)
	if err != nil {
		t.Fatalf("stat %s: %v", b, err)
	}
	return os.SameFile(ia, ib)
}

// TestFind_IgnoresContractOutsideGitRoot：契约只在仓库根算数。仓库根没有、子目录
// 有一份时不算——否则同一个仓库会因 cwd 深浅得到两个身份（status 说 sub 是项目，
// policy state 从仓根看说未知）。
func TestFind_IgnoresContractOutsideGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(repo, "sub")
	writeContract(t, sub, "version: \"1\"\n")

	if got, ok := Find(sub); ok {
		t.Fatalf("子目录契约不该激活整个仓库：Find=%q", got)
	}
}

// TestFind_HomeIsNotProject：home 目录本身不算项目根——home 下的一份契约不该把
// 整棵 home（含所有临时目录）认成一个项目。
func TestFind_HomeIsNotProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows 上 os.UserHomeDir 读 USERPROFILE
	writeContract(t, home, "version: \"1\"\n")
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := Find(proj)
	if ok {
		t.Fatalf("home 下的契约不该激活子目录：Find=%q", got)
	}
}

// TestFind_SymlinkedProject：从符号链接进入仓库与直接进入是同一个项目身份。
func TestFind_SymlinkedProject(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 建符号链接需要管理员或开发者模式")
	}
	real := t.TempDir()
	writeContract(t, real, "version: \"1\"\n")
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	deep := filepath.Join(link, "a")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := Find(deep)
	if !ok {
		t.Fatal("经符号链接进入仓库也应命中契约")
	}
	if !sameFile(t, got, real) {
		t.Fatalf("Find 应返回真实路径，got %q want %q", got, real)
	}
	if strings.HasPrefix(got, link) {
		t.Fatalf("Find 返回了链接路径而不是真实路径：%q", got)
	}
}

// TestFind_SymlinkedMissingLeaf：经 symlink 进入一个尚未创建的子目录，也要归一
// 到真实路径（canonicalDir 解析最深已存在祖先）。
func TestFind_SymlinkedMissingLeaf(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 建符号链接需要管理员或开发者模式")
	}
	real := t.TempDir()
	writeContract(t, real, "version: \"1\"\n")
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	missing := filepath.Join(link, "not", "created", "yet")

	got, ok := Find(missing)
	if !ok {
		t.Fatal("经符号链接进入未创建子目录也应命中契约")
	}
	if strings.HasPrefix(got, link) {
		t.Fatalf("Find 返回了链接路径而不是真实路径：%q", got)
	}
	if !sameFile(t, got, real) {
		t.Fatalf("Find=%q, want %q", got, real)
	}
}

// TestFind_GitRepoRootContractFromSubdir：契约在 git 根、cwd 在深层子目录——最
// 常见的主路径，必须命中根而不是子目录。
func TestFind_GitRepoRootContractFromSubdir(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeContract(t, repo, "version: \"1\"\n")
	src := filepath.Join(repo, "internal", "pkg")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := Find(src)
	if !ok {
		t.Fatal("git 根有契约时应命中")
	}
	if !sameFile(t, got, repo) {
		t.Fatalf("Find=%q, want 仓库根 %q", got, repo)
	}
}
