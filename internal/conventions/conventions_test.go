package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeRepoFile creates rel under root (parent dirs auto-made) with content.
//
// writeRepoFile 在 root 下创建 rel（父目录自动创建），内容 content。
func writeRepoFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatalf("mkdir %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

// TestScan_DetectsDeclaredConventions pins the P0 detection contract: AGENTS.md
// leads the instruction list, lint configs land in style_configs with the
// golangci-aware lint command, and the fingerprint covers them.
//
// TestScan_DetectsDeclaredConventions 钉住 P0 检测契约：AGENTS.md 居规范清单
// 首位，lint 配置进 style_configs 且 lint 命令感知 golangci，指纹覆盖它们。
func TestScan_DetectsDeclaredConventions(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "go.mod", "module example.com/x\n\ngo 1.25\n")
	writeRepoFile(t, root, "AGENTS.md", "# agents\nuse table-driven tests\n")
	writeRepoFile(t, root, ".golangci.yml", "linters:\n  enable:\n    - errorlint\n")
	writeRepoFile(t, root, ".editorconfig", "root = true\n")

	p, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if p.Stack != "go" {
		t.Fatalf("stack = %q, want go", p.Stack)
	}
	if p.LintCmd != "golangci-lint run" {
		t.Fatalf("lint = %q, want golangci-lint run (config present)", p.LintCmd)
	}
	if p.TestCmd != "go test ./..." || p.BuildCmd != "go build ./..." {
		t.Fatalf("test/build = %q/%q", p.TestCmd, p.BuildCmd)
	}
	if len(p.Instructions) != 1 || p.Instructions[0].Path != "AGENTS.md" {
		t.Fatalf("instructions = %+v, want [AGENTS.md]", p.Instructions)
	}
	paths := Paths(p.StyleConfigs)
	if !contains(paths, ".golangci.yml") || !contains(paths, ".editorconfig") {
		t.Fatalf("style configs missing golangci/editorconfig: %v", paths)
	}
	if p.Fingerprint == "" {
		t.Fatal("fingerprint empty")
	}
	if Stale(root, p) {
		t.Fatal("fresh scan reported stale")
	}
}

// TestScan_IgnoresForgeSectionInFingerprint pins the team-mode contract: forge
// writes its protocol between FORGE markers into root AGENTS.md/CLAUDE.md
// (`forge init --project`), so marker-section changes (a forge upgrade +
// re-init) must NOT flip staleness — only the project's OWN text outside the
// markers does. Without the strip, every forge upgrade permanently marks the
// profile STALE (adversarial-review finding, 2026-08-28).
//
// TestScan_IgnoresForgeSectionInFingerprint 钉住 team-mode 契约：forge 把协议
// 写在 FORGE 标记之间进根 AGENTS.md/CLAUDE.md（`forge init --project`），
// 标记段内的变化（forge 升级 + re-init）**不得**翻转过期——只有标记外项目
// 自身的文本才会。不剥离则每次 forge 升级都让档案永久 STALE
// （2026-08-28 对抗审查发现）。
func TestScan_IgnoresForgeSectionInFingerprint(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "AGENTS.md",
		"# house rules\n\nerror wrap: fmt.Errorf %w\n\n<!-- FORGE:START -->\nforge protocol text v1\n<!-- FORGE:END -->\n")
	p, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// 标记段升级（forge 协议 v1 → v2）：指纹不变。
	writeRepoFile(t, root, "AGENTS.md",
		"# house rules\n\nerror wrap: fmt.Errorf %w\n\n<!-- FORGE:START -->\nforge protocol text v2 rewritten\n<!-- FORGE:END -->\n")
	if Stale(root, p) {
		t.Fatal("forge-section rewrite must not flip staleness (team-mode echo/STALE trap)")
	}
	// 标记外项目自身文本变化：翻转。
	writeRepoFile(t, root, "AGENTS.md",
		"# house rules v2 — project edited\n\nerror wrap: fmt.Errorf %w\n\n<!-- FORGE:START -->\nforge protocol text v2 rewritten\n<!-- FORGE:END -->\n")
	if !Stale(root, p) {
		t.Fatal("project-owned text change must flip staleness")
	}
}

// TestExemplars_TestTargetGetsTestExemplars pins the conditional test-file
// exclusion: writing foo_test.go points at sibling tests (they are the RIGHT
// pattern for a test target); the exclusion only guards source targets.
//
// TestExemplars_TestTargetGetsTestExemplars 钉住条件化的测试文件排除：写
// foo_test.go 时应指向兄弟测试（对测试目标它们恰是正确模式）；排除只保护
// 源码目标。
func TestExemplars_TestTargetGetsTestExemplars(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "srv")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"alpha.go", "alpha_test.go", "beta_test.go"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("package srv\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got := Exemplars(root, filepath.Join(dir, "gamma_test.go"))
	if !contains(got, "internal/srv/alpha_test.go") || !contains(got, "internal/srv/beta_test.go") {
		t.Fatalf("test target should point at sibling tests, got %v", got)
	}
	if contains(got, "internal/srv/alpha.go") {
		t.Fatalf("source file must not crowd out test exemplars for a test target, got %v", got)
	}
}

// TestScan_FlipsStaleOnSourceChange pins the staleness contract on both
// content edits and newly-added declaration files.
//
// TestScan_FlipsStaleOnSourceChange 钉住过期契约：内容编辑与新增声明文件
// 都必须翻转。
func TestScan_FlipsStaleOnSourceChange(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "AGENTS.md", "v1\n")
	p, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if Stale(root, p) {
		t.Fatal("no-change scan stale")
	}
	writeRepoFile(t, root, "AGENTS.md", "v2 edited\n")
	if !Stale(root, p) {
		t.Fatal("content edit did not flip staleness")
	}
	// 新增声明文件（检测清单内、扫描时不存在）也须翻转。
	writeRepoFile(t, root, ".editorconfig", "root = true\n")
	p2, _ := Scan(root)
	if Stale(root, p2) {
		t.Fatal("rescan still stale")
	}
	writeRepoFile(t, root, ".prettierrc", "{}\n")
	if !Stale(root, p2) {
		t.Fatal("newly added style config did not flip staleness")
	}
}

// TestScan_NodePrefersOwnScripts pins the node rule: package.json scripts are
// the repo's own statement and beat generic fallbacks.
//
// TestScan_NodePrefersOwnScripts 钉住 node 规则：package.json scripts 是仓库
// 自己的声明，胜过通用回落。
func TestScan_NodePrefersOwnScripts(t *testing.T) {
	root := t.TempDir()
	writeRepoFile(t, root, "package.json", `{"scripts":{"lint":"eslint src","test":"vitest","build":"vite build"}}`)
	p, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if p.LintCmd != "npm run lint" || p.TestCmd != "npm test" || p.BuildCmd != "npm run build" {
		t.Fatalf("commands = %q/%q/%q, want npm run lint/npm test/npm run build", p.LintCmd, p.TestCmd, p.BuildCmd)
	}
}

// TestDetectStack covers the marker table plus the dotnet suffix fallback.
//
// TestDetectStack 覆盖标记表与 dotnet 后缀回落。
func TestDetectStack(t *testing.T) {
	cases := []struct {
		files []string
		want  string
	}{
		{[]string{"go.mod"}, "go"},
		{[]string{"Cargo.toml"}, "rust"},
		{[]string{"package.json"}, "node"},
		{[]string{"requirements.txt"}, "python"},
		{[]string{"build.gradle.kts"}, "java"},
		{[]string{"App.sln"}, "dotnet"},
		{[]string{"README.md"}, ""},
	}
	for _, tc := range cases {
		root := t.TempDir()
		for _, f := range tc.files {
			writeRepoFile(t, root, f, "x")
		}
		if got := DetectStack(root); got != tc.want {
			t.Errorf("DetectStack(%v) = %q, want %q", tc.files, got, tc.want)
		}
	}
}

// TestProfileStorageRoundTrip covers save/load, absent, corrupt, and version
// refusal — the four LoadProfile outcomes.
//
// TestProfileStorageRoundTrip 覆盖存/取、缺失、损坏与版本拒读——LoadProfile
// 的四种结局。
func TestProfileStorageRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	if p, err := LoadProfile(dataDir); err != nil || p != nil {
		t.Fatalf("absent profile = (%v, %v), want (nil, nil)", p, err)
	}
	want := &Profile{Version: ProfileVersion, Stack: "go", Fingerprint: "abc"}
	if err := SaveProfile(dataDir, want); err != nil {
		t.Fatalf("SaveProfile: %v", err)
	}
	got, err := LoadProfile(dataDir)
	if err != nil || got == nil || got.Stack != "go" || got.Fingerprint != "abc" {
		t.Fatalf("LoadProfile = (%+v, %v)", got, err)
	}
	if err := os.WriteFile(ProfilePath(dataDir), []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(dataDir); err == nil {
		t.Fatal("corrupt profile must error, not silently pass")
	}
	if err := os.WriteFile(ProfilePath(dataDir), []byte(`{"version":99}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProfile(dataDir); err == nil {
		t.Fatal("future version must be refused, not mis-parsed")
	}
}

// TestGenerateSummary_AndSessionInject pins the digest scaffold content and
// the session injection: framing line, staleness visibility, line budget,
// ellipsis marker on truncation.
//
// TestGenerateSummary_AndSessionInject 钉住摘要骨架内容与会话注入：框架行、
// 过期可见性、行数预算、截断省略标记。
func TestGenerateSummary_AndSessionInject(t *testing.T) {
	p := &Profile{
		Version:      ProfileVersion,
		Stack:        "go",
		LintCmd:      "golangci-lint run",
		TestCmd:      "go test ./...",
		Instructions: []SourceFile{{Path: "AGENTS.md"}},
		StyleConfigs: []SourceFile{{Path: ".golangci.yml"}},
	}
	sum := GenerateSummary(p)
	for _, want := range []string{"golangci-lint run", "AGENTS.md", ".golangci.yml", "提取要点"} {
		if !strings.Contains(sum, want) {
			t.Fatalf("GenerateSummary missing %q:\n%s", want, sum)
		}
	}

	inj := SessionInject(p, sum, false)
	if !strings.Contains(inj, "conventions profile") || !strings.Contains(inj, "AGENTS.md") {
		t.Fatalf("SessionInject missing framing/digest:\n%s", inj)
	}
	if n := nonEmptyLines(inj); n > SummaryLineBudget+3 { // 容差=框架行+stale 行+截断省略行（预算15 时合法上限 18）
		t.Fatalf("SessionInject %d non-empty lines, budget %d", n, SummaryLineBudget)
	}

	staleInj := SessionInject(p, sum, true)
	if !strings.Contains(staleInj, "STALE") {
		t.Fatal("stale injection must say STALE (a stale digest is worse than none — visible, not silent)")
	}

	// 摘要被删：回落生成版，不静默。
	if inj := SessionInject(p, "", false); !strings.Contains(inj, "stack") && !strings.Contains(inj, "AGENTS.md") {
		t.Fatalf("SessionInject with empty summary should fall back to generated digest:\n%s", inj)
	}

	// 超预算截断带省略标记。
	long := strings.Repeat("rule line\n", 40)
	if inj := SessionInject(p, long, false); !strings.Contains(inj, "…（摘要超行数上限") {
		t.Fatalf("truncated injection missing ellipsis marker:\n%.200s", inj)
	}
	if inj := SessionInject(nil, "x", false); inj != "" {
		t.Fatal("nil profile must render empty")
	}
}

// TestSuggestInit pins the no-profile suggestion: only when conventions are
// declared, naming the files.
//
// TestSuggestInit 钉住无档案建议：仅在已声明规范时给出，点名文件。
func TestSuggestInit(t *testing.T) {
	if SuggestInit(nil) != "" {
		t.Fatal("no declared conventions must not suggest")
	}
	s := SuggestInit([]string{"AGENTS.md", "CONVENTIONS.md"})
	if !strings.Contains(s, "AGENTS.md") || !strings.Contains(s, "forge conventions init") {
		t.Fatalf("SuggestInit = %q", s)
	}
}

// TestWriteInject pins the write-time block: instruction pointers, exemplar
// names, staleness line, and the silent path when there is nothing to say.
//
// TestWriteInject 钉住写入时刻块：规范指针、范例名、过期行、无可奉告时静默。
func TestWriteInject(t *testing.T) {
	p := &Profile{Version: ProfileVersion, Instructions: []SourceFile{{Path: "AGENTS.md"}}}
	s := WriteInject("internal/foo/bar.go", p, true, []string{"internal/foo/baz.go"})
	if !strings.Contains(s, "internal/foo/bar.go") || !strings.Contains(s, "AGENTS.md") ||
		!strings.Contains(s, "internal/foo/baz.go") || !strings.Contains(s, "stale") {
		t.Fatalf("WriteInject = %q", s)
	}
	bare := &Profile{Version: ProfileVersion}
	if WriteInject("x.go", bare, false, nil) != "" {
		t.Fatal("no instructions + no exemplars must stay silent")
	}
	if WriteInject("x.go", nil, false, nil) != "" {
		t.Fatal("nil profile must stay silent")
	}
}

// TestExemplars pins exemplar selection: same extension, test/dotfile
// exclusion, recency order, cap, repo-relative output.
//
// TestExemplars 钉住范例选择：同扩展名、排除测试/点文件、按新近排序、
// 上限、仓库相对输出。
func TestExemplars(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "srv")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	files := []struct {
		name string
		age  time.Duration
	}{
		{"alpha.go", 3 * time.Hour},
		{"beta.go", 1 * time.Hour},
		{"gamma.go", 2 * time.Hour},
		{"delta.go", 0},
		{"alpha_test.go", 0}, // 测试文件：教错模式，排除
		{".hidden.go", 0},    // 点文件：排除
		{"notes.txt", 0},     // 扩展名不同：排除
	}
	for _, f := range files {
		full := filepath.Join(dir, f.name)
		if err := os.WriteFile(full, []byte("package srv\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(full, now.Add(-f.age), now.Add(-f.age)); err != nil {
			t.Fatal(err)
		}
	}
	got := Exemplars(root, filepath.Join(dir, "new.go"))
	want := []string{"internal/srv/delta.go", "internal/srv/beta.go", "internal/srv/gamma.go"}
	if len(got) != len(want) {
		t.Fatalf("Exemplars = %v, want %v (cap %d, recency order)", got, want, ExemplarMax)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Exemplars[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// TestTrimToLines covers the budget boundary both sides.
//
// TestTrimToLines 覆盖预算边界的两侧。
func TestTrimToLines(t *testing.T) {
	if got := TrimToLines("a\n\nb\n", 5); got != "a\nb" {
		t.Fatalf("TrimToLines under budget = %q", got)
	}
	if got := TrimToLines("a\nb\nc", 2); got != "a\nb\n…（摘要超行数上限，全文 forge conventions show）" {
		t.Fatalf("TrimToLines over budget = %q", got)
	}
	if got := TrimToLines("  \n \n", 5); got != "" {
		t.Fatalf("TrimToLines blank = %q", got)
	}
}

// contains reports whether the list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// nonEmptyLines counts non-empty lines of s.
func nonEmptyLines(s string) int {
	n := 0
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// TestEnrichSummary pins the layer-4 write-back contract: placeholder
// replacement on first learn, newest-first insertion afterwards, exact-dedupe
// no-op, missing-summary refusal, and the over-budget flag (write still
// happens — never silently drop user content).
//
// TestEnrichSummary 钉住层 4 写回契约：首次 learn 替换占位符、其后最新在前
// 插入、一字不差去重空操作、summary 缺失拒绝、超预算标记（写入照常——绝不
// 静默丢用户内容）。
func TestEnrichSummary(t *testing.T) {
	dataDir := t.TempDir()
	if _, err := EnrichSummary(dataDir, "rule"); err == nil {
		t.Fatal("learn before init must refuse (an orphan digest with no profile)")
	}
	if err := SaveSummary(dataDir, GenerateSummary(&Profile{Version: ProfileVersion, Stack: "go", LintCmd: "go vet ./..."})); err != nil {
		t.Fatal(err)
	}

	// 首次：替换「待提取」占位符，不新增行。
	res, err := EnrichSummary(dataDir, "error handling: fmt.Errorf %w wrap")
	if err != nil || !res.Changed || res.OverBudget {
		t.Fatalf("first learn = %+v, %v", res, err)
	}
	sum := LoadSummary(dataDir)
	if strings.Contains(sum, "（待提取）") {
		t.Fatalf("placeholder must be replaced, got:\n%s", sum)
	}
	if !strings.Contains(sum, "- error handling: fmt.Errorf %w wrap") {
		t.Fatalf("rule line missing:\n%s", sum)
	}

	// 完全重复：空操作。
	res, err = EnrichSummary(dataDir, "error handling: fmt.Errorf %w wrap")
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatal("exact duplicate must be a no-op")
	}

	// 第二条：插入提取要点标题正下方（最新在前）。
	if _, err := EnrichSummary(dataDir, "naming: want/got in table tests"); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(LoadSummary(dataDir), "\n")
	heading := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), extractHeadingPrefix) {
			heading = i
			break
		}
	}
	if heading == -1 || heading+1 >= len(lines) || !strings.Contains(lines[heading+1], "want/got") {
		t.Fatalf("newest rule must sit right under the heading, got:\n%s", LoadSummary(dataDir))
	}

	// 超预算：写入照常 + 标记 OverBudget。
	for i := 0; i < SummaryLineBudget+2; i++ {
		if _, err := EnrichSummary(dataDir, "filler rule "+strings.Repeat("x", i+1)); err != nil {
			t.Fatal(err)
		}
	}
	res, err = EnrichSummary(dataDir, "one more rule")
	if err != nil || !res.Changed {
		t.Fatalf("over-budget learn must still write: %+v, %v", res, err)
	}
	if !res.OverBudget {
		t.Fatal("digest beyond the budget must flag OverBudget (caller surfaces the prune warning)")
	}

	// 标题被用户删掉：追加新节，规则不因结构漂移丢失。
	bare := t.TempDir()
	if err := SaveSummary(bare, "# digest\n\n- stack: go\n"); err != nil {
		t.Fatal(err)
	}
	res, err = EnrichSummary(bare, "resurrected rule")
	if err != nil || !res.Changed {
		t.Fatalf("missing-heading learn = %+v, %v", res, err)
	}
	if s := LoadSummary(bare); !strings.Contains(s, extractHeadingPrefix) || !strings.Contains(s, "resurrected rule") {
		t.Fatalf("missing heading must append a fresh section:\n%s", s)
	}
}
