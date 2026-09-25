package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// executor_unusedgate_test.go — CheckUnusedGate（complete 前置 wiring 门）契约：
// internal/ 下零引用 Go 导出 → 拦；非 internal / 非 Go → 维持 advisory 放行；
// env 逃生 → 放行但落 CheckEscapeHatch 审计。2026-09-14 RunReqHygiene 事故
// （advisory fail 照样 complete 进 main）是该门的存在理由。

// TestBlockingUnusedFindings 钉住硬拦子集的判定表：Go kinds（func/method/type）
// + internal/ 路径段才拦；TS/Rust kinds、非 internal 路径、internal 文件名（非
// 目录段）均不拦。
func TestBlockingUnusedFindings(t *testing.T) {
	cases := []struct {
		name string
		in   []UnusedFinding
		want []string // 拦下的 Symbol 列表
	}{
		{"Go func 在 internal 前缀", []UnusedFinding{{Kind: "func", File: "internal/foo/a.go", Symbol: "A"}}, []string{"A"}},
		{"Go type 在嵌套 internal 段", []UnusedFinding{{Kind: "type", File: "pkg/internal/b.go", Symbol: "B"}}, []string{"B"}},
		{"Go method 在 internal", []UnusedFinding{{Kind: "method", File: "x/internal/c.go", Symbol: "C"}}, []string{"C"}},
		{"TS export 不拦", []UnusedFinding{{Kind: "export", File: "internal/ui/x.ts", Symbol: "D"}}, nil},
		{"Rust fn 不拦", []UnusedFinding{{Kind: "fn", File: "internal/rs/x.rs", Symbol: "E"}}, nil},
		{"非 internal 路径不拦", []UnusedFinding{{Kind: "func", File: "pkg/a.go", Symbol: "F"}}, nil},
		{"internal.go 文件名不算目录段", []UnusedFinding{{Kind: "func", File: "pkg/internal.go", Symbol: "G"}}, nil},
		{"空输入", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := blockingUnusedFindings(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf(`拦下 %d 个, want %d: %+v`, len(got), len(tc.want), got)
			}
			for i, w := range tc.want {
				if got[i].Symbol != w {
					t.Errorf(`第 %d 个拦下 %s, want %s`, i, got[i].Symbol, w)
				}
			}
		})
	}
}

// findUnusedGateEntry 在 checklog 里找 CheckNameUnusedGate 条目。
func findUnusedGateEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == CheckNameUnusedGate {
			return &entries[i]
		}
	}
	return nil
}

// findEscapeEntry 在 checklog 里找 CheckEscapeHatch 条目（unused-gate 的逃生审计）。
func findEscapeEntry(t *testing.T, dir string) *checklog.Entry {
	t.Helper()
	entries, err := checklog.LoadAll(dir)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	for i := range entries {
		if entries[i].Check == checklog.CheckEscapeHatch && strings.Contains(entries[i].Detail, "unused gate bypassed") {
			return &entries[i]
		}
	}
	return nil
}

// TestCheckUnusedGate_BlocksInternalGoExport core contract: a task-added exported Go
// func under internal/ with zero references → gate BLOCKS with the symbol in reasons,
// and the checklog row records the decision (Passed=false).
//
// TestCheckUnusedGate_BlocksInternalGoExport 核心契约：任务新增、internal/ 下、零引用
// 的导出 Go 函数 → 门拦下（reasons 含符号名），checklog 落 Passed=false 裁定行。
func TestCheckUnusedGate_BlocksInternalGoExport(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"internal/foo/foo.go": "package foo\n\nfunc Lonely() int { return 2 }\n",
		"main.go":             "package main\n\nfunc main() {}\n",
	}, "add unwired internal export")

	state := newVerifyState(t, dir, "unused-gate-block")
	ok, reasons := CheckUnusedGate(dir, state)
	if ok {
		t.Fatal(`internal/ 下零引用导出应被拦`)
	}
	if !strings.Contains(strings.Join(reasons, ";"), "Lonely") || !strings.Contains(strings.Join(reasons, ";"), "internal/foo/foo.go") {
		t.Errorf(`reasons 应含符号与位置: %v`, reasons)
	}
	rec := findUnusedGateEntry(t, dir)
	if rec == nil {
		t.Fatal(`应记 CheckNameUnusedGate 裁定行`)
	}
	if rec.Passed {
		t.Errorf(`裁定行应 Passed=false: %+v`, rec)
	}
	if !strings.Contains(rec.Detail, "Lonely") {
		t.Errorf(`Detail 应含 Lonely: %q`, rec.Detail)
	}
}

// TestCheckUnusedGate_RootLevelExportAdvisoryOnly: root-level unused exports stay
// advisory — the gate passes (external-consumer exemption still valid outside internal/).
//
// TestCheckUnusedGate_RootLevelExportAdvisoryOnly：根路径的零引用导出维持 advisory——
// 门放行（internal/ 之外「外部消费者」豁免仍然成立）。
func TestCheckUnusedGate_RootLevelExportAdvisoryOnly(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\nfunc main() {}\nfunc Lonely() int { return 2 }\n",
	}, "add root-level unused export")

	state := newVerifyState(t, dir, "unused-gate-advisory")
	ok, reasons := CheckUnusedGate(dir, state)
	if !ok {
		t.Fatalf(`非 internal 导出应放行（advisory 语义不变）, reasons: %v`, reasons)
	}
	rec := findUnusedGateEntry(t, dir)
	if rec == nil || !rec.Passed {
		t.Errorf(`应记 Passed=true 裁定行: %+v`, rec)
	}
}

// TestCheckUnusedGate_WiredPasses: wiring the symbol into the real call chain clears
// the gate — the fix path the error message prescribes actually works.
//
// TestCheckUnusedGate_WiredPasses：把符号接进真实调用链后门放行——错误信息指引的
// 修复路径真实有效（不是只会在测试里拦人）。
func TestCheckUnusedGate_WiredPasses(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// 模块名不影响扫描（word-boundary 引用检测不编译代码）；foo.Used() 是真实调用链形态。
	writeCommitSource(t, dir, map[string]string{
		"internal/foo/foo.go": "package foo\n\nfunc Used() int { return 1 }\n",
		"main.go":             "package main\n\nimport \"x/internal/foo\"\n\nfunc main() { _ = foo.Used() }\n",
	}, "wired into real call chain")

	state := newVerifyState(t, dir, "unused-gate-wired")
	ok, reasons := CheckUnusedGate(dir, state)
	if !ok {
		t.Fatalf(`已接线应放行, reasons: %v`, reasons)
	}
}

// TestCheckUnusedGate_EscapeEnvAudited: FORGE_UNUSED_SCAN=disable bypasses the gate
// AND lands a CheckEscapeHatch audit row — escape is never silent.
//
// TestCheckUnusedGate_EscapeEnvAudited：FORGE_UNUSED_SCAN=disable 放行门禁且落
// CheckEscapeHatch 审计行——逃生绝不静默（有代价，review 可核查）。
func TestCheckUnusedGate_EscapeEnvAudited(t *testing.T) {
	t.Setenv("FORGE_UNUSED_SCAN", "disable")
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"internal/foo/foo.go": "package foo\n\nfunc Lonely() int { return 2 }\n",
		"main.go":             "package main\n\nfunc main() {}\n",
	}, "add unwired internal export")

	state := newVerifyState(t, dir, "unused-gate-escape")
	ok, reasons := CheckUnusedGate(dir, state)
	if !ok {
		t.Fatalf(`逃生应放行, reasons: %v`, reasons)
	}
	esc := findEscapeEntry(t, dir)
	if esc == nil {
		t.Fatal(`逃生须落 CheckEscapeHatch 审计行`)
	}
	if esc.Meta["escape.gate"] != "unused-gate" {
		t.Errorf(`escape.gate 应为 unused-gate: %v`, esc.Meta)
	}
	if findUnusedGateEntry(t, dir) != nil {
		t.Errorf(`逃生路径不应再落裁定行（裁定已被逃生取代，审计行是真相）`)
	}
}
