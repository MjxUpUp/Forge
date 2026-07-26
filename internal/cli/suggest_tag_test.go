package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// suggest_tag_test.go — tag consistency guard for suggestTagFor / findGitRoot (N4 / F1).
// F1 incident: the original tag was keyed by cwd; running 'forge suggest decline' from a
// subdirectory wrote the wrong tag, and the hook at the project root read a different tag → decline
// was permanently and silently broken. This test pins 'any subdirectory of the same git project
// yields the same tag' and guards the consistency between the hook (FORGE_CWD_TAG) and the suggest
// command sharing suggestTagFor — either side reverting to cwd-keying will go red immediately.
//
// Chinese strings use raw strings (backticks) to avoid Windows input quote corruption.
//
// suggest_tag_test.go — suggestTagFor / findGitRoot 的 tag 一致性守卫（N4 / F1）。
// F1 事故：原 tag 按 cwd 键控，agent 从子目录跑 'forge suggest decline' 写错 tag，
// hook 在项目根读到的是另一个 tag → decline 永久静默失效。本测试钉死「同 git 项目
// 任意子目录产生同一 tag」，并守 hook（FORGE_CWD_TAG）与 suggest 命令共用
// suggestTagFor 的一致性——任一侧未来改回 cwd-keying 会立即红。
//
// 中文字符串用 raw string（反引号）规避 Windows 输入引号腐蚀。

// mkGitProjCLI creates .git in the dir (findGitRoot decides via os.Stat; dir or file both count).
//
// mkGitProjCLI 在目录建 .git（findGitRoot 用 os.Stat 判定，dir 或 file 都算）。
func mkGitProjCLI(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, `.git`), 0755); err != nil {
		t.Fatalf(`mkdir .git: %v`, err)
	}
}

// TestSuggestTagFor_GitRootKeyed: within the same git project, root and deep subdirectories must
// yield the same tag. This is the core of the decline contract — the hook reads the tag at root,
// suggest may write it from a subdirectory, they must agree.
//
// TestSuggestTagFor_GitRootKeyed：同 git 项目，根与深层子目录应产生同一 tag。
// 这是 decline 契约的核心——hook 在根读 tag，suggest 可能从子目录写 tag，必须一致。
func TestSuggestTagFor_GitRootKeyed(t *testing.T) {
	root := t.TempDir()
	mkGitProjCLI(t, root)
	sub := filepath.Join(root, `src`, `deep`)
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatalf(`mkdir sub: %v`, err)
	}
	tagRoot := suggestTagFor(root)
	tagSub := suggestTagFor(sub)
	if tagRoot == `` {
		t.Fatal(`root tag empty`)
	}
	if tagRoot != tagSub {
		t.Fatalf(`同 git 项目应同 tag（decline 契约）：root=%s sub=%s`, tagRoot, tagSub)
	}
}

// TestSuggestTagFor_DifferentProjects: two independent git projects must yield different tags.
//
// TestSuggestTagFor_DifferentProjects：两个独立 git 项目应产生不同 tag。
func TestSuggestTagFor_DifferentProjects(t *testing.T) {
	p1 := t.TempDir()
	mkGitProjCLI(t, p1)
	p2 := t.TempDir()
	mkGitProjCLI(t, p2)
	if suggestTagFor(p1) == suggestTagFor(p2) {
		t.Fatalf(`不同 git 项目应不同 tag`)
	}
}

// TestSuggestTagFor_NonGitFallback: non-git directories fall back to projectTagFor(dir);
// different directories get different tags (non-git projects can also be tagged independently).
//
// TestSuggestTagFor_NonGitFallback：非 git 目录回退 projectTagFor(dir)，
// 不同目录不同 tag（非 git 项目也能被独立标记）。
func TestSuggestTagFor_NonGitFallback(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	ta := suggestTagFor(a)
	tb := suggestTagFor(b)
	if ta == `` || tb == `` {
		t.Fatal(`非 git tag empty`)
	}
	if ta == tb {
		t.Fatalf(`不同非 git 目录应不同 tag`)
	}
}

// TestFindGitRoot_NoHangAtRoot: infinite-loop regression guard — walking up from a directory
// without .git to the filesystem root must return (on a Windows drive root filepath.Dir returns
// itself, a natural break). This test passing means no hang; if the machine Temp happens to live
// inside some git repo, findGitRoot may be non-empty, that is not a bug — tolerated via Logf.
//
// TestFindGitRoot_NoHangAtRoot：防死循环回归——从无 .git 的目录向上查到文件系统根
// 必须返回（Windows 盘根 filepath.Dir 返回自身是天然 break）。本测试通过即未 hang；
// 若机器 Temp 恰在某个 git repo 内，findGitRoot 可能非空，那不是 bug，用 Logf 宽容。
func TestFindGitRoot_NoHangAtRoot(t *testing.T) {
	d := t.TempDir()
	got := findGitRoot(d)
	if got != `` {
		t.Logf(`findGitRoot(%s)=%s（Temp 祖先可能有 .git，非 fatal；本测试验不 hang）`, d, got)
	}
}

// TestBaseName_DriveRootSafe: when basename degenerates (drive-root bare separator / empty / ".")
// it returns the empty string, so suggestProjectName falls back to the full path instead of showing
// a bare separator (R5 / memory windows-go-bash-pitfalls).
//
// TestBaseName_DriveRootSafe：basename 退化（盘根裸分隔符 / 空 /"."）时返""，
// 让 suggestProjectName 回退全路径而非显示"\"（R5 / memory windows-go-bash-pitfalls）。
func TestBaseName_DriveRootSafe(t *testing.T) {
	sep := string(filepath.Separator)
	for _, p := range []string{sep, ``, `.`} {
		if got := baseName(p); got != `` {
			t.Errorf(`baseName(%q)=%q，期望空（退化值不应当项目名）`, p, got)
		}
	}
	// Normal paths still return the last segment.
	//
	// 正常路径仍返末段。
	if got := baseName(filepath.Join(`a`, `b`, `c`)); got != `c` {
		t.Errorf(`baseName(a/b/c)=%q，期望 c`, got)
	}
}

// TestSuggestStateDir_ForgeDataHome pins refactor-data-home commit E: suggestStateDir must go
// through forgedata.GlobalHome() (FORGE_DATA_HOME), sharing the same path as the init-suggest
// hook's ${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested. When FORGE_DATA_HOME is overridden, the
// marker directory must live under the override root (not ~/.forge) — ensuring 'forge suggest
// decline/status/reset' commands and the init-suggest hook read/write the same marker no matter
// where FORGE_DATA_HOME points.
//
// TestSuggestStateDir_ForgeDataHome 钉死 refactor-data-home commit E：suggestStateDir
// 必须走 forgedata.GlobalHome()（FORGE_DATA_HOME），与 init-suggest hook 的
// ${FORGE_DATA_HOME:-$HOME/.forge}/.init-suggested 同路径。FORGE_DATA_HOME 覆盖时 marker
// 目录必须在覆盖根下（不是 ~/.forge）——确保 'forge suggest decline/status/reset' 命令
// 与 init-suggest hook 读写同一 marker，无论 FORGE_DATA_HOME 指向哪。
func TestSuggestStateDir_ForgeDataHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, tmp)
	got := suggestStateDir()
	want := filepath.Join(tmp, `.init-suggested`)
	if got != want {
		t.Fatalf(`suggestStateDir must follow FORGE_DATA_HOME: got %s, want %s`, got, want)
	}
}
