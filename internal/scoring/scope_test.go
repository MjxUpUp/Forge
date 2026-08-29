package scoring

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCountAssertions_Go verifies Go assertion markers are counted.
func TestCountAssertions_Go(t *testing.T) {
	content := `package x
func TestA(t *testing.T) {
	t.Fatal(x)
	t.Errorf(y)
	require.True(t, ok)
	assert.Equal(t, 1, 1)
}
`
	// t.Fatal(1) + t.Error via Errorf(1) + require.(1) + assert.(1) = 4
	if n := countAssertions(content); n < 4 {
		t.Fatalf(`Go assertions: got %d, want >=4`, n)
	}
}

// TestCountAssertions_FakeTest verifies a test file with only setup/log (no
// assertions) counts zero — the core of the fake-test detection signal.
func TestCountAssertions_FakeTest(t *testing.T) {
	content := `package x
func TestA(t *testing.T) {
	t.Log(setup)
	x := compute()
	_ = x
}
`
	if n := countAssertions(content); n != 0 {
		t.Fatalf(`fake test (only setup/log) should have 0 assertions, got %d`, n)
	}
}

// TestCountAssertions_MultiLang verifies Rust/JS/Python markers register.
func TestCountAssertions_MultiLang(t *testing.T) {
	if n := countAssertions(`assert!(x); assert_eq!(a, b);`); n < 2 {
		t.Errorf(`rust: got %d, want >=2`, n)
	}
	if n := countAssertions(`expect(x).toBe(1); expect(y).toEqual(2);`); n < 3 {
		t.Errorf(`js: got %d, want >=3`, n)
	}
	if n := countAssertions(`self.assertEqual(a, 1); pytest.raises(E)`); n < 2 {
		t.Errorf(`python: got %d, want >=2`, n)
	}
}

// TestCountsAsScope locks the scope-exclusion rule: source files count, test
// files and non-source files do not. This is the A-fix — writing tests must not
// be penalized as "large change" (the bug that compressed an A-grade task to C).
func TestCountsAsScope(t *testing.T) {
	cases := map[string]bool{
		`main.go`:         true,
		`foo.ts`:          true,
		`pkg/bar_test.go`: false, // 测试文件排除
		`foo.spec.ts`:     false,
		`a/b.test.js`:     false,
		`README.md`:       false, // 非源码后缀
		`config.yaml`:     false,
		`Makefile`:        false,
	}
	for path, want := range cases {
		if got := countsAsScope(path); got != want {
			t.Errorf(`countsAsScope(%s) = %v, want %v`, filepath.Base(path), got, want)
		}
	}
}

// TestIsTestPath verifies the test-path heuristic and its precision: must not
// flag ordinary source whose name merely contains "test" (test_utils.go).
func TestIsTestPath(t *testing.T) {
	for _, p := range []string{`a_test.go`, `b.spec.ts`, `c.test.js`, `tests/x.go`, `__tests__/y.ts`} {
		if !isTestPath(p) {
			t.Errorf(`isTestPath(%s) = false, want true`, p)
		}
	}
	for _, p := range []string{`main.go`, `test_utils.go`, `latest.go`, `contest.go`} {
		if isTestPath(p) {
			t.Errorf(`isTestPath(%s) = true, want false (name contains test but is not a test file)`, p)
		}
	}
}

// TestParseDiffStatLines_ExcludesTestsAndNonSource verifies parseDiffStatLines
// skips test files and non-source files when summing scope. Tab/newline built
// from rune codes so the test source stays free of literal "\t"/"\n" escapes.
func TestParseDiffStatLines_ExcludesTestsAndNonSource(t *testing.T) {
	tab := string(rune(9))
	nl := string(rune(10))
	mk := func(a, d, p string) string { return a + tab + d + tab + p }

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{`test file excluded`, mk(`3`, `2`, `main_test.go`), 0},
		{`spec file excluded`, mk(`3`, `2`, `foo.spec.ts`), 0},
		{`non-source excluded`, mk(`3`, `2`, `README.md`), 0},
		{`source counted`, mk(`3`, `2`, `main.go`), 5},
		{`mixed only source counts`, mk(`3`, `2`, `main.go`) + nl + mk(`10`, `0`, `main_test.go`) + nl + mk(`5`, `5`, `util.ts`), 15},
	}
	for _, tt := range tests {
		got := parseDiffStatLines(tt.input)
		if got != tt.expected {
			t.Errorf(`%s: parseDiffStatLines = %d, want %d`, tt.name, got, tt.expected)
		}
	}
}

// TestCollectAssertionDensity verifies CollectAssertionDensity counts assertions
// across changed test files in a real (temp) git repo.
func TestCollectAssertionDensity(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")
	runGit(t, dir, "checkout", "-b", "feat/density")

	// Commit a test file with one assertion as a task-time change. (Untracked
	// files are ALSO visible since fix/cleanup-batch 2026-08-29 — see
	// TestCollectAssertionDensity_IncludesUntracked — but this case stays
	// committed: real scoring usually runs after the agent commits its work.)
	content := []byte(`package x
func TestA(t *testing.T) {
	t.Fatal(x)
}
`)
	if err := os.WriteFile(filepath.Join(dir, "foo_test.go"), content, 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, `add`, `foo_test.go`)
	runGit(t, dir, `commit`, `-m`, `add test`)

	count, files := CollectAssertionDensity(dir, "feat/density", "")
	if files != 1 {
		t.Fatalf(`expected 1 changed test file, got %d`, files)
	}
	if count < 1 {
		t.Fatalf(`expected >=1 assertion in foo_test.go, got %d`, count)
	}

	// A non-test source file committed in the same task must not inflate the
	// test-file count.
	if err := os.WriteFile(filepath.Join(dir, "bar.go"), []byte(`package x`), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, `add`, `bar.go`)
	runGit(t, dir, `commit`, `-m`, `add bar`)
	count2, files2 := CollectAssertionDensity(dir, "feat/density", "")
	if files2 != 1 {
		t.Fatalf(`bar.go is source not test; expected test files still 1, got %d`, files2)
	}
	if count2 != count {
		t.Fatalf(`adding non-test bar.go must not change assertion count: got %d, want %d`, count2, count)
	}
}

// TestCollectAssertionDensity_IncludesUntracked pins the untracked-file fix
// (fix/cleanup-batch, 2026-08-29): a brand-new, never-added test file is
// visible to assertion density. `git diff` does not accept --others (exit 129
// — ls-files option), so untracked files come from a separate
// `git ls-files --others --exclude-standard` probe inside changedFiles; before
// the fix, an uncommitted test file was invisible exactly where fake tests are
// most common (the agent just wrote the file and has not committed).
//
// TestCollectAssertionDensity_IncludesUntracked 钉住未跟踪文件修复
// （fix/cleanup-batch，2026-08-29）：全新、从未 add 的测试文件对断言密度可见。
// `git diff` 不接受 --others（exit 129——ls-files 的选项），故未跟踪文件由
// changedFiles 内单独的 `git ls-files --others --exclude-standard` 探测提供；
// 修复前，未提交的测试文件恰在假测试最常见的场景（agent 刚写完还没提交）
// 里不可见。
func TestCollectAssertionDensity_IncludesUntracked(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "init")

	// UNTRACKED test file: written, never added/committed.
	//
	// 未跟踪的测试文件：已写入、从未 add/commit。
	content := []byte(`package x
func TestFresh(t *testing.T) {
	t.Fatal(x)
}
`)
	if err := os.WriteFile(filepath.Join(dir, "fresh_test.go"), content, 0644); err != nil {
		t.Fatal(err)
	}

	count, files := CollectAssertionDensity(dir, "", "")
	if files != 1 {
		t.Fatalf(`expected 1 untracked test file counted, got %d（ls-files --others 探测未接线？）`, files)
	}
	if count < 1 {
		t.Fatalf(`expected >=1 assertions from the untracked test file, got %d`, count)
	}
}

// TestChangedFiles_AllProbesDeadErrors pins the error contract mirror of
// gitDiffStat (fix/cleanup-batch, 2026-08-29): in a non-git directory every
// probe (diff base..HEAD, diff HEAD, ls-files --others) fails, and the failure
// must surface as an error instead of a silent empty list — CollectAssertionDensity
// consumes it to skip the fake-test penalty (a dead probe must not read as
// "zero assertions, punish").
//
// TestChangedFiles_AllProbesDeadErrors 钉住与 gitDiffStat 镜像的错误契约
// （fix/cleanup-batch，2026-08-29）：非 git 目录里所有探测（diff base..HEAD、
// diff HEAD、ls-files --others）都失败，失败必须以 error 浮出而非静默空列表
// ——CollectAssertionDensity 据此跳过假测试惩罚（死探测不得读作「零断言，惩罚」）。
func TestChangedFiles_AllProbesDeadErrors(t *testing.T) {
	dir := t.TempDir() // 非 git 目录：三探测全死
	if _, err := changedFiles(dir, "HEAD~1"); err == nil {
		t.Fatal("all git probes dead (non-git dir) must return an error, got nil")
	}
}

// TestCollectAssertionDensity_DeadProbeReturnsZeros pins the caller-side half
// of the dead-probe contract: on a collection error the function returns
// (0, 0) — never a crash — and scoreTesting's testFiles>0 guard turns that
// into "penalty skipped" instead of "punish".
//
// TestCollectAssertionDensity_DeadProbeReturnsZeros 钉住死探测契约的调用侧
// 一半：采集出错时返回 (0, 0)——绝不崩——scoreTesting 的 testFiles>0 守卫把它
// 变成「跳过惩罚」而非「惩罚」。
func TestCollectAssertionDensity_DeadProbeReturnsZeros(t *testing.T) {
	dir := t.TempDir() // 非 git 目录：changedFiles 全探测失败
	count, files := CollectAssertionDensity(dir, "", "")
	if count != 0 || files != 0 {
		t.Fatalf("dead probe: got (count=%d, files=%d), want (0, 0)", count, files)
	}
}
