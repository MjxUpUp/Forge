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

	// Commit a test file with one assertion as a task-time change. changedFiles
	// only sees tracked diff content, so the file must be committed (real scoring
	// runs after the agent commits its work).
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
