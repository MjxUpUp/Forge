package compat

// compat_test.go — 六面快照的确定性契约 + Diff 判级规则 + roster 完整性守卫。

import (
	"os"
	"path/filepath"
	"regexp"
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

// TestEscapeRosterComplete 守卫（对抗审查 should-fix：原注释宣称源对照而实际
// 只查非空——执法点虚设）：源扫描 taskpipeline 里 escapeDisabled 使用的
// *DisableEnv 常量与 EscapeEnvs 双向对齐。
func TestEscapeRosterComplete(t *testing.T) {
	if len(EscapeEnvs) == 0 {
		t.Fatal("逃生舱 roster 不应为空")
	}
	// 源扫描：taskpipeline 各文件里 `xxxDisableEnv = "FORGE_..."` 常量。
	srcDir := filepath.Join("..", "taskpipeline")
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatalf("读 taskpipeline 源目录失败（守卫需在仓内运行）: %v", err)
	}
	// 匹配任意 "FORGE_*" 字符串常量声明（DisableEnv 后缀与 envXxx 两种命名
	// 形态都命中；常量声明用双引号，hook 脚本嵌 Bash 字符串另有反引号形态——
	// 双形态都命中）；已知非逃生舱的 env（阈值调节）显式排除。
	re := regexp.MustCompile("[\"`](FORGE_[A-Z_]+)[\"`]")
	nonEscape := map[string]bool{
		// 非逃生舱类 env（白名单——命中源扫描但不属"gate-bypass 逃生舱"语义）：
		"FORGE_RECURRENT_THRESHOLD": true, // 阈值调节（recurrent-harden 强度）
		"FORGE_RECURRENT_HARDEN":    true, // 强度回退（advisory 化偏好，非 gate-bypass）
		"FORGE_CONVENTIONS_LINT":    true, // 功能开关（conventions lint 启停）
		"FORGE_TOOL_NAME":           true, // hook 协议字段（hook stdin 传值，非用户 env）
		"FORGE_FILE_PATH":           true, // hook 协议字段（同上）
		"FORGE_COMMAND":             true, // hook 协议字段（同上）
		"FORGE_CONTENT":             true, // hook 协议字段（同上）
		"FORGE_OLD_STRING":          true, // hook 协议字段（同上）
		"FORGE_NEW_STRING":          true, // hook 协议字段（同上）
		"FORGE_SESSION_ID":          true, // 会话标识（hook 传值）
		"FORGE_ATTRIBUTION":         true, // 归属通道选择（非 gate-bypass）
		"FORGE_LOG_RETENTION_DAYS":  true, // 日志保留期参数
		"FORGE_TEST_TIMEOUT":        true, // 测试参数
	}
	inSrc := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		body, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			inSrc[m[1]] = true
		}
	}
	if len(inSrc) == 0 {
		t.Fatal("源扫描零命中——守卫正则可能失配（检查常量命名形态）")
	}
	roster := map[string]bool{}
	for _, e := range EscapeEnvs {
		roster[e] = true
	}
	for env := range inSrc {
		if nonEscape[env] {
			continue
		}
		if !roster[env] {
			t.Errorf("taskpipeline 源里的逃生舱 %s 不在 compat.EscapeEnvs roster——新增逃生舱必须同步 roster（快照面 3）", env)
		}
	}
	for env := range roster {
		if !inSrc[env] {
			t.Errorf("roster 里的 %s 在 taskpipeline 源中无对应常量（逃生舱已删除？同步 roster）", env)
		}
	}
	sorted := append([]string(nil), EscapeEnvs...)
	sort.Strings(sorted)
	for i := 1; i < len(sorted); i++ {
		if sorted[i] == sorted[i-1] {
			t.Fatalf("roster 重复项: %s", sorted[i])
		}
	}
}

// TestAllCheckNamesSorted roster 排序非空 + 源对照（types.go 常量与显式清单
// 双向对齐——对抗审查 should-fix：原注释宣称守卫而未实现）。
func TestAllCheckNamesSorted(t *testing.T) {
	names := AllCheckNames()
	if len(names) < 30 {
		t.Fatalf("roster 异常小: %d", len(names))
	}
	// 源对照：checklog/types.go 里 `CheckXxx CheckName = "y"` 声明集（复审
	// 发现原实现读 internal/compat/types.go（不存在）+ 正则双重转义，双
	// fail-open 静默虚设——修正为正确路径与双引号形态）。
	body, err := os.ReadFile(filepath.Join("..", "checklog", "types.go"))
	if err == nil {
		re := regexp.MustCompile("Check\\w+\\s+CheckName\\s*=\\s*\"([\\w-]+)\"")
		declared := map[string]bool{}
		for _, m := range re.FindAllStringSubmatch(string(body), -1) {
			declared[m[1]] = true
		}
		listed := map[string]bool{}
		for _, n := range names {
			listed[n] = true
		}
		for c := range declared {
			if !listed[c] {
				t.Errorf("types.go 声明的 %s 不在 AllCheckNames 清单——同步 escape.go 的 allCheckNames", c)
			}
		}
		for c := range listed {
			if !declared[c] {
				t.Errorf("清单里的 %s 在 types.go 无声明（已删除？同步清单）", c)
			}
		}
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
