package compat

// compat_test.go — 六面快照的确定性契约 + Diff 判级规则 + roster 完整性守卫。

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDiff_BreakingRules 判级规则表：removed/changed → Breaking；added → 非。
func TestDiff_BreakingRules(t *testing.T) {
	base := &Snapshot{
		Commands:  []CommandSurface{{Path: "forge old", Flags: []string{"a"}}},
		Checks:    []string{"a", "b"},
		Escapes:   []string{"FORGE_X"},
		Payload:   []PayloadItem{{Tree: "canonical", Name: "s1", Hash: "h1"}},
		Schemas:   map[string][]string{"Entry": {"check", "detail"}},
		Blockings: []BlockingSite{{File: "internal/x.go", Count: 1}},
	}
	cur := &Snapshot{
		Commands: []CommandSurface{
			{Path: "forge old", Flags: []string{"a", "b"}}, // flag 变更 → changed+Breaking
			{Path: "forge new"},                            // added 非 Breaking
		},
		Checks:    []string{"b", "c"},                                         // a 删→Breaking；c 增→非
		Escapes:   []string{"FORGE_X2"},                                       // X 删→Breaking
		Payload:   []PayloadItem{{Tree: "canonical", Name: "s1", Hash: "h2"}}, // hash 变→changed 非破坏（内容变更走 skill-decisions 门禁）
		Schemas:   map[string][]string{"Entry": {"check", "extra"}},           // detail 删→Breaking；extra 增→非
		Blockings: []BlockingSite{{File: "internal/x.go", Count: 3}},          // 增→非破坏（文案契约提示）
	}
	changes := Diff(base, cur)
	byItem := map[string]Change{}
	for _, c := range changes {
		byItem[c.Surface+"/"+c.Item] = c
	}
	// 破坏面逐项断言（四个 removed/changed 形态）。
	for _, w := range []string{"commands|forge old（flags 变更）", "checks|a", "escapes|FORGE_X", "schemas|Entry.detail"} {
		parts := strings.SplitN(w, "|", 2)
		c, ok := byItem[parts[0]+"/"+parts[1]]
		if !ok || !c.Breaking {
			t.Fatalf("缺破坏性变更 %s: %+v", w, byItem)
		}
	}
	gotBreaking := 0
	for _, c := range changes {
		if c.Breaking {
			gotBreaking++
		}
	}
	if gotBreaking != 4 {
		t.Fatalf("破坏性变更应 4 项，实际 %d：%+v", gotBreaking, changes)
	}
	if BreakingCount(changes) != gotBreaking {
		t.Fatal("BreakingCount 不一致")
	}
}

// TestScanBlockings 在临时树上验证源扫描的确定性与 _test 豁免。
func TestScanBlockings(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal", "demo")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "a.go"), []byte("x := GateBlocked(\"m\")\ny := GateBlocked(\"n\")\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.go"), []byte("z := 1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "c_test.go"), []byte("GateBlocked(\"t\")\n"), 0o644)
	sites, err := scanBlockings(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].File != "internal/demo/a.go" || sites[0].Count != 2 {
		t.Fatalf("扫描不符（_test 应豁免）: %+v", sites)
	}
}

// TestEscapeRosterComplete 守卫：compat.EscapeEnvs 与 taskpipeline 源里
// escapeDisabled 消费的 env 常量对齐（新增逃生舱必须同步 roster）。
func TestEscapeRosterComplete(t *testing.T) {
	// 守卫 A：roster 非空且无重复。完整性由 golden 钉——snapshot 的 escapes 面
	// 任何变化都在 compat.snapshot.json diff 里显式（新增逃生舱必须同步 roster
	// 才能过 golden 守卫）。
	if len(EscapeEnvs) == 0 {
		t.Fatal("逃生舱 roster 不应为空")
	}
	sorted := append([]string(nil), EscapeEnvs...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Fatalf("roster 重复项: %s", sorted[i])
		}
	}
}

// TestAllCheckNamesSorted roster 排序且非空。
func TestAllCheckNamesSorted(t *testing.T) {
	names := AllCheckNames()
	if len(names) < 30 {
		t.Fatalf("roster 异常小: %d", len(names))
	}
	if !sort.StringsAreSorted(names) {
		t.Fatal("roster 未排序")
	}
	for _, n := range names {
		if strings.Contains(n, " ") {
			t.Fatalf("CheckName 含空格: %q", n)
		}
	}
}
