package conventions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

// TestScan_DetectsDeclaredConventions pins the P0 detection contract: AGENTS.md leads the instruction list, lint configs land in style_configs with the golangci-aware lint command, and the fingerprint covers them.
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

// TestScan_IgnoresForgeSectionInFingerprint pins the team-mode contract.
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

// TestScan_FlipsStaleOnSourceChange pins the staleness contract on both content edits and newly-added declaration files.
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

// TestScan_NodePrefersOwnScripts pins the node rule: package.json scripts are the repo's own statement and beat generic fallbacks.
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

// TestProfileStorageRoundTrip covers save/load, absent, corrupt, and version refusal — the four LoadProfile outcomes.
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

// contains reports whether the list holds s.
func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TestEnrichSummary pins the layer-4 write-back contract.
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
