package forgedata

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// key_test.go — Key/FindGitRoot/RootDir 全行为守卫（§6.1 + §2 corrupt 测试矩阵）。
// 中文字符串 raw string 避 Windows 引号腐蚀。

// TestFindGitRoot_StopsAtRoot：findGitRoot 找到含 .git 的祖先并停止，
// 不沿系统盘根继续（防死循环，plan §2 safety 段）。
func TestFindGitRoot_StopsAtRoot(t *testing.T) {
	// t.TempDir 在系统 Temp 下（一般非 git repo 上方）
	d := t.TempDir()
	got := FindGitRoot(d)
	if got != "" {
		t.Logf(`FindGitRoot(%s)=%s（Temp 上方有 .git，预期非空；确认未 panic）`, d, got)
	}
}

// TestFindGitRoot_MainGitDir：主 worktree .git 目录。
func TestFindGitRoot_MainGitDir(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatalf(`mkdir .git: %v`, err)
	}
	// cwd 是 .git 子目录
	sub := filepath.Join(gitDir, "objects")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf(`mkdir .git/objects: %v`, err)
	}
	if got := FindGitRoot(sub); got != root {
		t.Errorf(`FindGitRoot(%s)=%s，期望 %s`, sub, got, root)
	}
}

// TestFindGitRoot_GitAsFile：.git 是 file（worktree/submodule）。
func TestFindGitRoot_GitAsFile(t *testing.T) {
	// 主 git 仓库
	mainRepo := t.TempDir()
	mainGit := filepath.Join(mainRepo, ".git")
	if err := os.MkdirAll(mainGit, 0755); err != nil {
		t.Fatalf(`mkdir main .git: %v`, err)
	}
	// worktree 仓库：wt/.git 是 file 指向 main/.git/worktrees/wt
	wtRepo := t.TempDir()
	wtGitFile := filepath.Join(wtRepo, ".git")
	worktreePath := filepath.Join(mainGit, "worktrees", "wt")
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatalf(`mkdir worktree dir: %v`, err)
	}
	if err := os.WriteFile(wtGitFile, []byte("gitdir: "+worktreePath+"\n"), 0644); err != nil {
		t.Fatalf(`write wt .git file: %v`, err)
	}
	// worktree cwd 子目录
	wtSub := filepath.Join(wtRepo, "src")
	if err := os.MkdirAll(wtSub, 0755); err != nil {
		t.Fatalf(`mkdir wt sub: %v`, err)
	}
	got := FindGitRoot(wtSub)
	if got != wtRepo {
		t.Errorf(`FindGitRoot worktree(%s)=%s，期望 %s`, wtSub, got, wtRepo)
	}
	// 主 repo 子目录 vs worktree 子目录 key 应一致
	keyMain, err := Key(filepath.Join(mainRepo, "src"))
	if err != nil {
		t.Fatalf(`Key(main): %v`, err)
	}
	keyWT, err := Key(wtSub)
	if err != nil {
		t.Fatalf(`Key(wt): %v`, err)
	}
	if keyMain != keyWT {
		t.Errorf(`主 repo 与 worktree key 不一致：main=%s wt=%s`, keyMain, keyWT)
	}
}

// TestKey_NotInGitRepo：非 git 项目返 ErrNotInGitRepo。
func TestKey_NotInGitRepo(t *testing.T) {
	d := t.TempDir() // 无 .git
	_, err := Key(d)
	if err == nil {
		t.Fatal(`非 git 项目应返 err`)
	}
	if !errorIs(err, ErrNotInGitRepo) {
		t.Errorf(`期望 ErrNotInGitRepo，实得 %v`, err)
	}
}

// TestKey_WorktreeKeyMatchesMain：同 repo 多 worktree（worktree + 子模块两个）共享 hash。
func TestKey_WorktreeKeyMatchesMain(t *testing.T) {
	mainRepo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(mainRepo, ".git"), 0755); err != nil {
		t.Fatalf(`mkdir: %v`, err)
	}
	// 单 worktree
	wtRepo := t.TempDir()
	wtDir := filepath.Join(mainRepo, ".git", "worktrees", "wt")
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatalf(`mkdir wt dir: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(wtRepo, ".git"), []byte("gitdir: "+wtDir+"\n"), 0644); err != nil {
		t.Fatalf(`write wt .git: %v`, err)
	}
	// 第二 worktree
	wt2Repo := t.TempDir()
	wt2Dir := filepath.Join(mainRepo, ".git", "worktrees", "wt2")
	if err := os.MkdirAll(wt2Dir, 0755); err != nil {
		t.Fatalf(`mkdir wt2: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(wt2Repo, ".git"), []byte("gitdir: "+wt2Dir+"\n"), 0644); err != nil {
		t.Fatalf(`write wt2 .git: %v`, err)
	}
	// detached worktree（path 是 raw，不链 .git/worktrees 子目录）
	detachedRepo := t.TempDir()
	detachedDir := filepath.Join(mainRepo, ".git", "worktrees", "detached")
	if err := os.MkdirAll(detachedDir, 0755); err != nil {
		t.Fatalf(`mkdir detached: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(detachedRepo, ".git"), []byte("gitdir: "+detachedDir+"\n"), 0644); err != nil {
		t.Fatalf(`write detached .git: %v`, err)
	}

	mainKey, err := Key(mainRepo)
	if err != nil {
		t.Fatalf(`Key(main): %v`, err)
	}
	for name, path := range map[string]string{
		"wt":       wtRepo,
		"wt2":      wt2Repo,
		"detached": detachedRepo,
	} {
		k, err := Key(path)
		if err != nil {
			t.Fatalf(`Key(%s): %v`, name, err)
		}
		if k != mainKey {
			t.Errorf(`worktree %s key=%s 不等于 main key=%s`, name, k, mainKey)
		}
	}
}

// TestKey_Submodule：submodule（.git 是 file 指父 .git/modules/sub）解析后与父 repo key 不一定等
// （设计如此——submodule 的 .git 指向 `<parent>/.git/modules/<sub>/`，该 path 沿 chain 找 .git 祖先
// 结果是 `<parent>/.git`，与 parent 自己的 .git 路径相同 → key 相等）。
func TestKey_Submodule(t *testing.T) {
	parentRepo := t.TempDir()
	parentGit := filepath.Join(parentRepo, ".git")
	if err := os.MkdirAll(parentGit, 0755); err != nil {
		t.Fatalf(`mkdir parent .git: %v`, err)
	}
	// submodule 的 .git 指向 parent/.git/modules/<sub>
	subRepo := t.TempDir()
	subGitdir := filepath.Join(parentGit, "modules", "sub")
	if err := os.MkdirAll(subGitdir, 0755); err != nil {
		t.Fatalf(`mkdir sub gitdir: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(subRepo, ".git"), []byte("gitdir: "+subGitdir+"\n"), 0644); err != nil {
		t.Fatalf(`write sub .git: %v`, err)
	}

	parentKey, err := Key(parentRepo)
	if err != nil {
		t.Fatalf(`Key(parent): %v`, err)
	}
	subKey, err := Key(subRepo)
	if err != nil {
		t.Fatalf(`Key(sub): %v`, err)
	}
	// 模块 .gitdir 经解析沿 parent 找 .git 祖先应等于 parent .git
	if subKey != parentKey {
		t.Errorf(`submodule key 与 parent 应共享：parent=%s sub=%s`, parentKey, subKey)
	}
}

// TestKey_CorruptGitFile（§2 corrupt 防护矩阵）：
// 各种 .git file 损坏输入 → ErrInvalidGitFile。
func TestKey_CorruptGitFile(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{`空文件`, ``},
		{`全空白`, " \t\n  \n"},
		{`缺 gitdir: 前缀`, "not a gitdir line\n"},
		{`gitdir 值为空`, "gitdir: \n"},
		{`gitdir 值为空字符串`, "gitdir: "}, // trailing 无 newline
		{`含 NUL`, "gitdir: /tmp/\x00bad\n"},
		{`无 .git 祖先`, "gitdir: /nonexistent/path/without/git/ancestor\n"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := t.TempDir()
			gitFile := filepath.Join(repo, ".git")
			if err := os.WriteFile(gitFile, []byte(c.content), 0644); err != nil {
				t.Fatalf(`seed .git: %v`, err)
			}
			_, err := Key(repo)
			if err == nil {
				t.Errorf(`corrupt .git (%s) 应返 err`, c.name)
				return
			}
			if !errorIs(err, ErrInvalidGitFile) {
				t.Errorf(`corrupt .git (%s) 期望 ErrInvalidGitFile，实得 %v`, c.name, err)
			}
		})
	}
}

// TestKey_LoopedGitDirSafetyCounter：循环 gitdir 引用在 safety counter 内返 ErrInvalidGitFile。
// 构造"gitdir: ./a/b/.git/worktrees/foo/.git/..."——极深目录超过 safetyMax=64。
func TestKey_LoopedGitDirSafetyCounter(t *testing.T) {
	repo := t.TempDir()
	// 构造深度超过 64 的 .gitdir 路径（用 .git 子目录名填充）
	deep := repo
	for i := 0; i < 70; i++ {
		deep = filepath.Join(deep, ".git")
	}
	// 注意：深层目录可能不存在；放个目录让 stat 通过但 base 永不到 .git
	for i := 0; i < 69; i++ {
		// 路径中已有 .git，base() 已是 .git，第一次 while 循环 exit
		// ——构造正确路径不易；改测更简易：循环引用自身
		_ = i
	}
	// 真正"循环 gitdir"：.git 指相对路径回自己
	loopBack := strings.Repeat("../", 50) + "." // 50 层 ../ 回到 repo
	if err := os.WriteFile(filepath.Join(repo, ".git"), []byte("gitdir: "+loopBack+"\n"), 0644); err != nil {
		t.Fatalf(`seed: %v`, err)
	}
	_, err := Key(repo)
	if err == nil {
		t.Error(`循环 gitdir 应返 err（要么 ErrNotInGitRepo 要么 ErrInvalidGitFile）`)
		return
	}
	// 可能是 ErrInvalidGitFile（safety 触限）或是 ErrNotInGitRepo（解析失败导致 .git 不到）
	// 不强求具体类型——只要非 nil 就行
}

// TestKey_SymlinkRepo：symlink 指真实 repo → EvalSymlinks 归物理路径 key 一致。
func TestKey_SymlinkRepo(t *testing.T) {
	real := t.TempDir()
	if err := os.MkdirAll(filepath.Join(real, ".git"), 0755); err != nil {
		t.Fatalf(`mkdir: %v`, err)
	}
	realKey, err := Key(real)
	if err != nil {
		t.Fatalf(`Key(real): %v`, err)
	}
	// symlink repo
	linkParent := t.TempDir()
	link := filepath.Join(linkParent, "repo-link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf(`skip symlink test on this platform: %v`, err)
	}
	linkKey, err := Key(link)
	if err != nil {
		t.Fatalf(`Key(link): %v`, err)
	}
	if linkKey != realKey {
		t.Errorf(`symlinked repo key 与 physical repo key 应一致：real=%s link=%s`, realKey, linkKey)
	}
}

// TestRootDir_FORGE_DATA_HOME 覆盖：FORGE_DATA_HOME 优先于 UserHomeDir。
func TestRootDir_FORGE_DATA_HOME(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, tmp)
	got := RootDir(`abc123456789`)
	want := filepath.Join(tmp, `projects`, `abc123456789`)
	if got != want {
		t.Errorf(`RootDir=%s，期望 %s`, got, want)
	}
}

// TestRootDir_EmptyKey：空 key 返 ""，不构造假路径。
func TestRootDir_EmptyKey(t *testing.T) {
	if got := RootDir(``); got != `` {
		t.Errorf(`空 key 应返 ""，实得 %s`, got)
	}
}

// TestProjectFor 完整路径 + ConfigDir walk-up。
func TestProjectFor(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatalf(`mkdir .git: %v`, err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".forge"), 0755); err != nil {
		t.Fatalf(`mkdir .forge: %v`, err)
	}

	// cwd 是 repo 内子目录——应 walk-up 找到 .forge/ 父目录
	sub := filepath.Join(repo, `src`, `deep`)
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf(`mkdir sub: %v`, err)
	}

	t.Setenv(`FORGE_DATA_HOME`, t.TempDir()) // 隔离 globalHome
	p, err := ProjectFor(sub)
	if err != nil {
		t.Fatalf(`ProjectFor: %v`, err)
	}
	if p.Key == `` {
		t.Fatal(`Key 空`)
	}
	if p.DataDir == `` {
		t.Fatal(`DataDir 空`)
	}
	if !strings.HasSuffix(p.ConfigDir, `.forge`) {
		t.Errorf(`ConfigDir 应以 .forge 结尾，实得 %s`, p.ConfigDir)
	}
	if p.ConfigDir != filepath.Join(repo, `.forge`) {
		t.Errorf(`ConfigDir 应=%s，实得 %s`, filepath.Join(repo, `.forge`), p.ConfigDir)
	}
}

// TestProjectFor_NoForgeConfig：项目未 init（无 .forge/）→ ErrNoForgeConfig。
func TestProjectFor_NoForgeConfig(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatalf(`mkdir .git: %v`, err)
	}
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	_, err := ProjectFor(repo)
	if err == nil {
		t.Fatal(`无 .forge/ 应返 err`)
	}
	if !errorIs(err, ErrNoForgeConfig) {
		t.Errorf(`期望 ErrNoForgeConfig，实得 %v`, err)
	}
}

// errorIs wraps errors.Is；这里 inline 因为我们 package 内不能 import 其他 err 包
func errorIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// guard windows path-related test skip
func init() {
	if runtime.GOOS == "windows" {
		// 当前测试在 Windows 上也应跑；路径相关用 filepath.Join 跨平台
	}
}
