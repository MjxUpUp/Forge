package forgedata

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// projectid_test.go — repo-born 项目 ID 身份层（project-sync 设计 §A）。
// 主 worktree 根下 committed 的 `.forge-project-id` 覆盖路径推导 key：同一仓库在两台
// 机器（不同路径）只要都带 ID 文件即推导同一 key。缺失/非法 ID 静默回落路径 hash
// （fail-open——存量项目在显式 adopt 前身份不变）。
// 中文字符串用 raw string 避 Windows 引号腐蚀。

// seedProjectID 在 repoRoot 写合法 ID 文件（fpid_<32hex>）。
func seedProjectID(t *testing.T, repoRoot, id string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, ProjectIDFileName), []byte(id+"\n"), 0644); err != nil {
		t.Fatalf(`seed %s: %v`, ProjectIDFileName, err)
	}
}

// TestReadProjectID_Validation: strict format contract fpid_[0-9a-f]{32}; trailing whitespace tolerated (TrimSpace), everything else rejected.
//
// TestReadProjectID_Validation：严格格式契约 fpid_[0-9a-f]{32}；容忍尾随空白
// （TrimSpace），其余一律拒绝。
func TestReadProjectID_Validation(t *testing.T) {
	root := t.TempDir()
	valid := `fpid_0123456789abcdef0123456789abcdef`

	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{`合法`, valid + "\n", false},
		{`合法带尾随空格`, valid + "  \n", false},
		{`合法无换行`, valid, false},
		{`空文件`, ``, true},
		{`纯空白`, "  \n\t", true},
		{`缺前缀`, "0123456789abcdef0123456789abcdef", true},
		{`错误前缀`, "fpid2_0123456789abcdef0123456789abcdef", true},
		{`hex 不足 32`, "fpid_0123456789abcdef", true},
		{`hex 超 32`, "fpid_" + strings.Repeat(`0`, 33), true},
		{`大写 hex`, "fpid_" + strings.ToUpper("0123456789abcdef0123456789abcdef"), true},
		{`非 hex 字符`, "fpid_0123456789abcdef0123456789abcdeg", true},
		{`多行`, valid + "\n" + valid, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(root, ProjectIDFileName), []byte(c.content), 0644); err != nil {
				t.Fatalf(`seed: %v`, err)
			}
			id, err := ReadProjectID(root)
			if c.wantErr {
				if err == nil {
					t.Errorf(`%s 应被判非法（实得 %q）`, c.name, id)
				}
				return
			}
			if err != nil {
				t.Fatalf(`%s 不应报错: %v`, c.name, err)
			}
			if id != valid {
				t.Errorf(`%s 读得 %q，期望 %q`, c.name, id, valid)
			}
		})
	}

	// 文件缺失 → 明确报错（Key 据此回落路径 hash）
	if _, err := ReadProjectID(t.TempDir()); err == nil {
		t.Error(`ID 文件缺失应报错`)
	}
}

// TestKey_ProjectIDPriority: a valid ID file at the repo root overrides the path-derived key — Key(repo) == IDKey(id), and differs from the pure path hash (domain separation).
//
// TestKey_ProjectIDPriority：repo 根的合法 ID 文件覆盖路径推导 key——
// Key(repo) == IDKey(id)，且不等于纯路径 hash（域分离）。
func TestKey_ProjectIDPriority(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatalf(`mkdir .git: %v`, err)
	}
	pathKey := keyFromCommonDir(filepath.Join(repo, ".git"))

	id := `fpid_0123456789abcdef0123456789abcdef`
	seedProjectID(t, repo, id)

	got, err := Key(repo)
	if err != nil {
		t.Fatalf(`Key: %v`, err)
	}
	if want := IDKey(id); got != want {
		t.Errorf(`有 ID 文件时 Key=%s，期望 IDKey=%s`, got, want)
	}
	if got == pathKey {
		t.Errorf(`IDKey 与路径 hash 撞车（%s）——域分离失效`, got)
	}
	if len(got) != 12 || strings.ContainsAny(got, `g-z`) {
		t.Errorf(`key 应为 12 位小写 hex，实得 %q`, got)
	}
}

// TestKey_NoOrInvalidIDFallsBackToPath: missing or malformed ID file must leave Key()
// EXACTLY the pre-ID path hash (fail-open:存量项目身份不变，坏文件不 brick 热路径)。
//
// TestKey_NoOrInvalidIDFallsBackToPath：缺失或畸形的 ID 文件必须让 Key() 恒等于
// 既有路径 hash（fail-open：存量项目身份不变，坏文件不 brick 热路径）。
func TestKey_NoOrInvalidIDFallsBackToPath(t *testing.T) {
	for name, content := range map[string]string{
		`无 ID 文件`:  ``,
		`畸形 ID 文件`: `not-a-valid-id`,
	} {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			git := filepath.Join(repo, ".git")
			if err := os.MkdirAll(git, 0755); err != nil {
				t.Fatalf(`mkdir .git: %v`, err)
			}
			if content != `` {
				seedProjectID(t, repo, content)
			}
			// Key() 对 resolvedGitDir 先 EvalSymlinks 再 CanonicalCase（t.TempDir 在
			// macOS 是 /var → /private/var symlink）；期望值必须走同一归一。
			normalized := git
			if eval, evalErr := filepath.EvalSymlinks(git); evalErr == nil {
				normalized = eval
			}
			want := keyFromCommonDir(normalized)
			got, err := Key(repo)
			if err != nil {
				t.Fatalf(`Key: %v`, err)
			}
			if got != want {
				t.Errorf(`%s：Key=%s 应回落路径 hash=%s`, name, got, want)
			}
		})
	}
}

// TestKey_ProjectIDWorktreeSharesMain: the ID is read from the MAIN worktree root (Dir of the resolved common .git dir) — an uncommitted ID file in the main worktree still applies to linked worktrees, preserving the "all worktrees share one key" contract under the ID regime.
//
// TestKey_ProjectIDWorktreeSharesMain：ID 从主 worktree 根读取（解析后 common .git
// 目录的父目录）——主 worktree 未 commit 的 ID 文件对 linked worktree 同样生效，
// ID 体系下保持「同 repo 所有 worktree 共一个 key」契约。
func TestKey_ProjectIDWorktreeSharesMain(t *testing.T) {
	mainRepo := t.TempDir()
	mainGit := filepath.Join(mainRepo, ".git")
	if err := os.MkdirAll(mainGit, 0755); err != nil {
		t.Fatalf(`mkdir main .git: %v`, err)
	}
	wtRepo := t.TempDir()
	wtDir := filepath.Join(mainGit, "worktrees", "wt")
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		t.Fatalf(`mkdir wt dir: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(wtRepo, ".git"), []byte("gitdir: "+wtDir+"\n"), 0644); err != nil {
		t.Fatalf(`write wt .git: %v`, err)
	}

	id := `fpid_ffffffffffffffffffffffffffffffff`
	seedProjectID(t, mainRepo, id) // 只在主 worktree 根；worktree 根不写

	mainKey, err := Key(mainRepo)
	if err != nil {
		t.Fatalf(`Key(main): %v`, err)
	}
	wtKey, err := Key(wtRepo)
	if err != nil {
		t.Fatalf(`Key(worktree): %v`, err)
	}
	if mainKey != IDKey(id) {
		t.Errorf(`main Key=%s 应为 IDKey=%s`, mainKey, IDKey(id))
	}
	if wtKey != mainKey {
		t.Errorf(`worktree Key=%s 应与 main=%s 共享（经主根读 ID）`, wtKey, mainKey)
	}
}

// TestIDKey_DeterministicAndDistinct: IDKey is a pure content hash — deterministic across calls (hence across GOOS: no path, no case-folding input), 12 lowercase hex chars, distinct inputs give distinct keys (sanity, not collision-proof).
//
// TestIDKey_DeterministicAndDistinct：IDKey 是纯内容 hash——跨调用确定（因而跨
// GOOS 确定：无路径、无大小写折叠输入），12 位小写 hex，不同输入不同 key
// （健全性检查，非抗碰撞证明）。
func TestIDKey_DeterministicAndDistinct(t *testing.T) {
	a := `fpid_00000000000000000000000000000000`
	b := `fpid_00000000000000000000000000000001`
	ka1, ka2 := IDKey(a), IDKey(a)
	if ka1 != ka2 {
		t.Errorf(`IDKey 应确定：%s vs %s`, ka1, ka2)
	}
	if IDKey(a) == IDKey(b) {
		t.Errorf(`相邻输入不应同 key：%s`, ka1)
	}
	for _, k := range []string{IDKey(a), IDKey(b)} {
		if len(k) != 12 || strings.ContainsAny(k, `g-z`) {
			t.Errorf(`IDKey 应为 12 位小写 hex，实得 %q`, k)
		}
	}
}

// TestKey_SubmoduleParentIDNotInherited: a submodule shares the parent's common-dir path hash today; under the ID regime the submodule derives from the PARENT repo root's ID file (Dir of parent .git) — i.e. submodule keeps sharing the parent identity.
//
// TestKey_SubmoduleParentIDNotInherited：submodule 今天与父 repo 共享 common-dir
// 路径 hash；ID 体系下 submodule 经「父 .git 的父目录」读父 repo 根的 ID 文件——
// 即 submodule 继续与父共享身份。钉死这个刻意语义（与路径体系一致）而非巧合。
func TestKey_SubmoduleParentIDNotInherited(t *testing.T) {
	parentRepo := t.TempDir()
	parentGit := filepath.Join(parentRepo, ".git")
	if err := os.MkdirAll(parentGit, 0755); err != nil {
		t.Fatalf(`mkdir parent .git: %v`, err)
	}
	subRepo := t.TempDir()
	subGitdir := filepath.Join(parentGit, "modules", "sub")
	if err := os.MkdirAll(subGitdir, 0755); err != nil {
		t.Fatalf(`mkdir sub gitdir: %v`, err)
	}
	if err := os.WriteFile(filepath.Join(subRepo, ".git"), []byte("gitdir: "+subGitdir+"\n"), 0644); err != nil {
		t.Fatalf(`write sub .git: %v`, err)
	}

	id := `fpid_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`
	seedProjectID(t, parentRepo, id)

	parentKey, err := Key(parentRepo)
	if err != nil {
		t.Fatalf(`Key(parent): %v`, err)
	}
	subKey, err := Key(subRepo)
	if err != nil {
		t.Fatalf(`Key(sub): %v`, err)
	}
	if subKey != parentKey {
		t.Errorf(`submodule key=%s 应与 parent=%s 共享（父根 ID）`, subKey, parentKey)
	}
}
