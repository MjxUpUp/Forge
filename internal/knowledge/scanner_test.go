package knowledge

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestCompileSafeRegex guards the length defense of the scanner's regex
// compiler. Legitimate patterns compile and match; overlong and syntactically
// invalid patterns are rejected. ReDoS screening is intentionally absent — Go's
// RE2 regexp is linear-time and cannot catastrophically backtrack (see the
// compileSafeRegex doc comment), so nested patterns like ((((a)))) compile fine.
func TestCompileSafeRegex(t *testing.T) {
	re, err := compileSafeRegex(`hello`)
	if err != nil || !re.MatchString("hello world") {
		t.Errorf("compileSafeRegex(hello) = %v, %v; want match", re, err)
	}
	if _, err := compileSafeRegex(strings.Repeat("a", maxPatternLength+1)); err == nil {
		t.Error("compileSafeRegex accepted an overlong pattern (compiler DoS vector)")
	}
	// RE2 cannot catastrophically backtrack, so nested-but-valid patterns must
	// compile — rejecting them would false-positive on legitimate regexes.
	if _, err := compileSafeRegex(`((((a))))`); err != nil {
		t.Errorf("compileSafeRegex rejected a valid nested pattern (RE2 is linear, no ReDoS): %v", err)
	}
	if _, err := compileSafeRegex(`[`); err == nil {
		t.Error("compileSafeRegex accepted a syntactically invalid pattern")
	}
}

func TestCheckViolationsEmptyDir(t *testing.T) {
	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{
				ID:       "gotchas-1",
				Category: "gotchas",
				Title:    "Test",
				Patterns: []string{`TODO`},
			},
		},
	}

	violations := idx.CheckViolations("/nonexistent/path/that/does/not/exist")
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for nonexistent dir, got %d", len(violations))
	}
}

func TestCheckViolationsNoGotchas(t *testing.T) {
	dir := t.TempDir()
	// Write a .go file that would match if scanned
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte("TODO: fix this\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{
				ID:       "patterns-1",
				Category: "patterns",
				Title:    "Pattern entry",
				Patterns: []string{`TODO`},
			},
			{
				ID:       "apis-1",
				Category: "apis",
				Title:    "API entry",
				Patterns: []string{`TODO`},
			},
		},
	}

	violations := idx.CheckViolations(dir)
	if len(violations) != 0 {
		t.Fatalf("non-gotchas entries should be skipped, got %d violations", len(violations))
	}
}

func TestCheckViolationsWithPattern(t *testing.T) {
	dir := t.TempDir()

	// Write source files
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n// TODO: fix this\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "util.ts"), []byte("export function foo() { /* TODO: implement */ }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Non-source file should be ignored
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("TODO: write docs\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{
				ID:       "gotchas-todo",
				Category: "gotchas",
				Title:    "Leftover TODO",
				Patterns: []string{`TODO`},
			},
		},
	}

	violations := idx.CheckViolations(dir)
	// Should find TODO in main.go and util.ts, but NOT readme.txt
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}

	files := map[string]bool{}
	for _, v := range violations {
		files[v.File] = true
		if v.EntryID != "gotchas-todo" {
			t.Fatalf("expected entry ID 'gotchas-todo', got %q", v.EntryID)
		}
		if v.Pattern != "TODO" {
			t.Fatalf("expected pattern 'TODO', got %q", v.Pattern)
		}
		if v.Line < 1 {
			t.Fatalf("line number should be >= 1, got %d", v.Line)
		}
		if !strings.Contains(v.LineText, "TODO") {
			t.Fatalf("line text should contain 'TODO', got %q", v.LineText)
		}
	}
	if !files["main.go"] {
		t.Fatal("expected violation in main.go")
	}
	if !files["util.ts"] {
		t.Fatal("expected violation in util.ts")
	}
}

func TestCheckViolationsBadRegex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}

	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{
				ID:       "gotchas-bad",
				Category: "gotchas",
				Title:    "Bad regex",
				Patterns: []string{`[invalid(`}, // invalid regex
			},
		},
	}

	// Should not panic, should return 0 violations
	violations := idx.CheckViolations(dir)
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations for bad regex, got %d", len(violations))
	}
}

func TestScanDirBasic(t *testing.T) {
	dir := t.TempDir()

	// Create subdirectories
	subDir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a skipped directory
	nodeModules := filepath.Join(dir, "node_modules")
	if err := os.MkdirAll(nodeModules, 0755); err != nil {
		t.Fatal(err)
	}

	// Source files with match
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("TODO: a\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.rs"), []byte("TODO: b\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Source file in skipped dir
	if err := os.WriteFile(filepath.Join(nodeModules, "c.js"), []byte("TODO: c\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Non-source extension
	if err := os.WriteFile(filepath.Join(dir, "d.css"), []byte("TODO: d\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Minified file
	if err := os.WriteFile(filepath.Join(dir, "bundle.min.js"), []byte("TODO: min\n"), 0644); err != nil {
		t.Fatal(err)
	}

	matches := scanDir(dir, regexpMust("TODO"))
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d", len(matches))
	}

	files := map[string]bool{}
	for _, m := range matches {
		files[m.File] = true
	}
	if !files["a.go"] {
		t.Error("expected match in a.go")
	}
	if !files[filepath.Join("sub", "b.rs")] {
		t.Error("expected match in sub/b.rs")
	}
}

func TestFormatViolationsEmpty(t *testing.T) {
	result := FormatViolations(nil)
	if result != "" {
		t.Fatalf("expected empty string for nil violations, got %q", result)
	}

	result = FormatViolations([]Violation{})
	if result != "" {
		t.Fatalf("expected empty string for empty slice, got %q", result)
	}
}

func TestFormatViolationsWithItems(t *testing.T) {
	violations := []Violation{
		{
			EntryID:  "gotchas-1",
			Title:    "Leftover TODO",
			Pattern:  "TODO",
			File:     "main.go",
			Line:     42,
			LineText: "// TODO: fix this later",
		},
		{
			EntryID:  "gotchas-2",
			Title:    "Hardcoded secret",
			Pattern:  `password\s*=`,
			File:     "config.go",
			Line:     10,
			LineText: `password = "secret"`,
		},
	}

	result := FormatViolations(violations)

	// Check structure
	if !strings.Contains(result, "[gotchas-1]") {
		t.Fatal("output missing entry ID gotchas-1")
	}
	if !strings.Contains(result, "Leftover TODO") {
		t.Fatal("output missing title 'Leftover TODO'")
	}
	if !strings.Contains(result, "Pattern: TODO") {
		t.Fatal("output missing 'Pattern: TODO'")
	}
	if !strings.Contains(result, "Location: main.go:42") {
		t.Fatal("output missing 'Location: main.go:42'")
	}
	if !strings.Contains(result, "Code: // TODO: fix this later") {
		t.Fatal("output missing code line")
	}
	if !strings.Contains(result, "[gotchas-2]") {
		t.Fatal("output missing entry ID gotchas-2")
	}
	if !strings.Contains(result, "Location: config.go:10") {
		t.Fatal("output missing 'Location: config.go:10'")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"empty", "", nil},
		{"single line no newline", "hello", []string{"hello"}},
		{"single line with newline", "hello\n", []string{"hello"}},
		{"two lines", "hello\nworld", []string{"hello", "world"}},
		{"two lines trailing", "hello\nworld\n", []string{"hello", "world"}},
		{"windows line endings", "hello\r\nworld\r\n", []string{"hello", "world"}},
		{"mixed line endings", "hello\nworld\r\nfoo", []string{"hello", "world", "foo"}},
		{"empty lines", "a\n\nb", []string{"a", "", "b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitLines(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("splitLines(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("splitLines(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"hello", "ell", true},
		{"hello", "xyz", false},
		{"hello", "hello", true},
		{"hello", "", true},
		{"", "", true},
		{"", "a", false},
		{"abc", "abcd", false},
		{"foo.min.js", ".min.", true},
		{"foo.umd.js", ".umd.", true},
	}

	for _, tt := range tests {
		got := containsSubstring(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("containsSubstring(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestTruncateLine(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"needs truncation", "hello world", 8, "hello..."},
		{"empty string", "", 5, ""},
		{"unicode", "你好世界你好世界", 5, "你好..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateLine(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateLine(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

// TestCheckViolations_DedupRepeatedEntries：index 里同 ID entry 重复时，
// CheckViolations 不应放大输出。模拟 exp-accept1 被加 29 次的场景——1 处代码命中
// 1 个 pattern 应只报 1 次（不是 entry 数 × 命中数）。双层去重：entry ID + location。
func TestCheckViolations_DedupRepeatedEntries(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("var _ = err != nil\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// 同 ID entry 放 3 份（模拟 index 重复）
	repeat := Entry{
		ID: "exp-repeat", Category: "gotchas", Title: "repeat",
		Patterns: []string{`err != nil`}, Severity: "error",
	}
	idx := &Index{Version: "2.0", Entries: []Entry{repeat, repeat, repeat}}

	v := idx.CheckViolations(dir)
	// 1 file:line × 1 pattern = 1 violation（不是 3）。重复 entry ID 不放大输出。
	if len(v) != 1 {
		t.Fatalf("CheckViolations = %d violations, want 1（重复 entry 应去重，不放大）: %+v", len(v), v)
	}
}

// regexpMust is a test helper that compiles a regex or fails.
func regexpMust(pat string) *regexp.Regexp {
	// Import regexp inline — the file already imports it via the package.
	// Use the standard library regexp.Compile.
	re, err := regexp.Compile(pat)
	if err != nil {
		panic("regexpMust: " + err.Error())
	}
	return re
}
