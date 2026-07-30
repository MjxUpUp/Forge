package taskpipeline

import (
	"strings"
	"testing"
)

// symDefEq compares two symbolDef slices by value (fields are all comparable).
//
// symDefEq 按值比较两个 symbolDef 切片（字段全可比较）。
func symDefEq(a, b []symbolDef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestExtractGo pins exported func/method/type extraction; non-exported and main/init (lowercase)
// are not extracted (so they never become findings — the wiring-miss signal is for exports only).
//
// TestExtractGo 钉导出 func/method/type 提取；未导出及 main/init（小写）不提取（故永不成为
// finding——接线缺失信号只针对导出符号）。
func TestExtractGo(t *testing.T) {
	cases := []struct {
		name string
		line addedLine
		want []symbolDef
	}{
		{`导出函数`, al("a.go", 1, "func Foo() {}"), []symbolDef{{"Foo", "func", "a.go", 1}}},
		{`导出方法`, al("b.go", 2, "func (e *T) Bar() {}"), []symbolDef{{"Bar", "method", "b.go", 2}}},
		{`导出方法无空格`, al("b.go", 2, "func (e *T) Baz() {}"), []symbolDef{{"Baz", "method", "b.go", 2}}},
		{`导出类型 struct`, al("c.go", 3, "type Qux struct{}"), []symbolDef{{"Qux", "type", "c.go", 3}}},
		{`导出类型 alias`, al("d.go", 4, "type Alias = int"), []symbolDef{{"Alias", "type", "d.go", 4}}},
		{`未导出函数不提取`, al("e.go", 5, "func foo() {}"), nil},
		{`未导出类型不提取`, al("f.go", 6, "type baz int"), nil},
		{`main 不提取(小写)`, al("g.go", 7, "func main() {}"), nil},
		{`init 不提取(小写)`, al("h.go", 8, "func init() {}"), nil},
		{`调用语句不提取`, al("i.go", 9, "x := Foo()"), nil},
	}
	for _, c := range cases {
		got := extractGo(c.line)
		if !symDefEq(got, c.want) {
			t.Errorf(`%s: got %+v, want %+v`, c.name, got, c.want)
		}
	}
}

// TestExtractTS pins TS/JS export extraction across forms; non-exported declarations are skipped.
//
// TestExtractTS 钉 TS/JS 各 export 形态提取；非 export 声明跳过。
func TestExtractTS(t *testing.T) {
	cases := []struct {
		name string
		line addedLine
		want []symbolDef
	}{
		{`export function`, al("a.ts", 1, "export function Foo() {}"), []symbolDef{{"Foo", "export", "a.ts", 1}}},
		{`export const`, al("b.ts", 2, "export const Bar = 1"), []symbolDef{{"Bar", "export", "b.ts", 2}}},
		{`export class`, al("c.ts", 3, "export class Baz {}"), []symbolDef{{"Baz", "export", "c.ts", 3}}},
		{`export type`, al("d.ts", 4, "export type Q = number"), []symbolDef{{"Q", "export", "d.ts", 4}}},
		{`export interface`, al("e.ts", 5, "export interface I {}"), []symbolDef{{"I", "export", "e.ts", 5}}},
		{`export default function`, al("f.ts", 6, "export default function D() {}"), []symbolDef{{"D", "export", "f.ts", 6}}},
		{`export async function`, al("g.ts", 7, "export async function A() {}"), []symbolDef{{"A", "export", "g.ts", 7}}},
		{`无 export 不提取`, al("h.ts", 8, "const x = 1"), nil},
		{`缩进 export`, al("i.ts", 9, "    export function Ind() {}"), []symbolDef{{"Ind", "export", "i.ts", 9}}},
	}
	for _, c := range cases {
		got := extractTS(c.line)
		if !symDefEq(got, c.want) {
			t.Errorf(`%s: got %+v, want %+v`, c.name, got, c.want)
		}
	}
}

// TestExtractRust pins pub fn/struct extraction; private items are skipped (Rust dead_code lint
// already covers them; this scanner targets pub items the lint intentionally ignores).
//
// TestExtractRust 钉 pub fn/struct 提取；私有项跳过（Rust dead_code lint 已覆盖；本扫描器针对
// lint 刻意忽略的 pub 项）。
func TestExtractRust(t *testing.T) {
	cases := []struct {
		name string
		line addedLine
		want []symbolDef
	}{
		{`pub fn`, al("a.rs", 1, "pub fn do_thing() {}"), []symbolDef{{"do_thing", "fn", "a.rs", 1}}},
		{`pub async fn`, al("b.rs", 2, "pub async fn afoo() {}"), []symbolDef{{"afoo", "fn", "b.rs", 2}}},
		{`pub struct`, al("c.rs", 3, "pub struct Foo"), []symbolDef{{"Foo", "struct", "c.rs", 3}}},
		{`私有 fn 不提取`, al("d.rs", 4, "fn priv() {}"), nil},
		{`私有 struct 不提取`, al("e.rs", 5, "struct Hidden"), nil},
	}
	for _, c := range cases {
		got := extractRust(c.line)
		if !symDefEq(got, c.want) {
			t.Errorf(`%s: got %+v, want %+v`, c.name, got, c.want)
		}
	}
}

// TestScanUnusedSymbols_RealGitDiff end-to-end: a committed source file with one wired export
// (called by main) and one orphan export (zero references) → only the orphan is reported.
//
// TestScanUnusedSymbols_RealGitDiff 端到端：committed 源码含一个已接线导出（被 main 调用）和
// 一个孤儿导出（零引用）→ 只报孤儿。
func TestScanUnusedSymbols_RealGitDiff(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\n" +
			"func main() { Wired() }\n" +
			"func Wired() int { return Orphan() }\n" +
			"func Orphan() int { return 1 }\n" +
			"func Lonely() int { return 2 }\n",
	}, "add prod")
	state := newVerifyState(t, dir, "unused-committed")
	findings := ScanUnusedSymbols(dir, state)
	syms := make(map[string]bool)
	for _, f := range findings {
		syms[f.Symbol] = true
	}
	if !syms["Lonely"] {
		t.Errorf(`Lonely 零引用应被检出, findings=%+v`, findings)
	}
	if syms["Orphan"] {
		t.Errorf(`Orphan 被 Wired 调用, 不应检出`)
	}
	if syms["Wired"] {
		t.Errorf(`Wired 被 main 调用, 不应检出`)
	}
}

// TestScanUnusedSymbols_TestReferenceDoesNotCount is the core wiring-failure scenario: an export
// referenced ONLY in its test file (unit test passes) but in zero production lines is exactly
// "tests green, feature dead" → must still be reported. Production references count, test do not.
//
// TestScanUnusedSymbols_TestReferenceDoesNotCount 是核心接线失败场景：一个导出符号只在测试文件
// 被引用（单测过）但在零生产行出现，正是"测试绿、功能死"→ 必须仍被检出。生产引用算数，
// 测试不算。
func TestScanUnusedSymbols_TestReferenceDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go":     "package main\n\nfunc Foo() int { return 1 }\n",
		"prod_test.go": "package main\n\nimport \"testing\"\n\nfunc TestFoo(t *testing.T) { Foo() }\n",
	}, "add prod+test")
	state := newVerifyState(t, dir, "test-ref")
	findings := ScanUnusedSymbols(dir, state)
	found := false
	for _, f := range findings {
		if f.Symbol == "Foo" {
			found = true
		}
	}
	if !found {
		t.Errorf(`Foo 只在测试里被引用(生产零引用), 应检出为未接线, findings=%+v`, findings)
	}
}

// TestScanUnusedSymbols_UntrackedFiles: an unreferenced export in an untracked file (agent just
// created, not git add'd) is also detected — collectAddedLines covers the whole-file read path.
//
// TestScanUnusedSymbols_UntrackedFiles 未跟踪文件（agent 刚建未 git add）里的未引用导出也能
// 检出——collectAddedLines 走整文件读路径。
func TestScanUnusedSymbols_UntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeUntracked(t, dir, map[string]string{
		"new.go": "package main\n\nfunc Fresh() int { return 1 }\n",
	})
	state := newVerifyState(t, dir, "unused-untracked")
	findings := ScanUnusedSymbols(dir, state)
	found := false
	for _, f := range findings {
		if f.Symbol == "Fresh" {
			found = true
		}
	}
	if !found {
		t.Errorf(`未跟踪文件的 Fresh 零引用应检出, findings=%+v`, findings)
	}
}

// TestScanUnusedSymbols_CleanDiff: every export has a production caller → zero findings.
//
// TestScanUnusedSymbols_CleanDiff 每个导出都有生产调用方 → 零 findings。
func TestScanUnusedSymbols_CleanDiff(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"clean.go": "package main\n\nfunc main() { Use() }\nfunc Use() int { return 1 }\n",
	}, "add clean")
	state := newVerifyState(t, dir, "clean")
	if got := ScanUnusedSymbols(dir, state); len(got) != 0 {
		t.Fatalf(`所有导出都有调用方, 应零 findings, got %+v`, got)
	}
}

// TestScanUnusedSymbols_CommentDoesNotCount pins that a doc comment mentioning a symbol's own name
// does NOT count as a reference — exactly Forge's own BUG-1 shape (a symbol referenced only in its
// doc comment, never called in production code). Without stripping the comment, the `// Foo ...` line
// matches `\bFoo\b` and the symbol is falsely counted as referenced → missed. This is the core recall
// guarantee of the scanner (comment ≠ wiring).
//
// TestScanUnusedSymbols_CommentDoesNotCount 钉住：doc comment 提及符号自身名字不算引用——恰好是
// Forge 自己 BUG-1 的形状（符号只在 doc comment 被提及，生产从未调用）。不剥注释的话 `// Foo ...`
// 行匹配 `\bFoo\b`，符号被假判为已引用 → 漏检。这是扫描器的核心召回保证（注释 ≠ 接线）。
func TestScanUnusedSymbols_CommentDoesNotCount(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"prod.go": "package main\n\n" +
			"// Foo is a helper, see Foo for details.\n" +
			"func Foo() int { return 1 }\n",
	}, "add prod with doc comment")
	state := newVerifyState(t, dir, "comment-ref")
	findings := ScanUnusedSymbols(dir, state)
	found := false
	for _, f := range findings {
		if f.Symbol == "Foo" {
			found = true
		}
	}
	if !found {
		t.Errorf(`Foo 只在 doc comment 被提及(生产零真实调用), 注释不应算引用, 应检出, findings=%+v`, findings)
	}
}

// TestDedupDefs pins that dedupDefs collapses identical (file,line,symbol) definitions collected from
// overlapping diff specs (collectAddedLayers HeadCommit..HEAD + HEAD). Without dedup, a def line
// present in both specs yields duplicate findings / duplicate Detail entries.
//
// TestDedupDefs 钉住 dedupDefs 折叠相同 (file,line,symbol) 定义——这些来自重叠的 diff spec
// （collectAddedLayers 的 HeadCommit..HEAD + HEAD）。不去重的话，两 spec 都有的 def 行会产生
// 重复 finding / 重复 Detail 条目。
func TestDedupDefs(t *testing.T) {
	in := []symbolDef{
		{symbol: "Foo", kind: "func", file: "a.go", line: 1},
		{symbol: "Foo", kind: "func", file: "a.go", line: 1}, // 重复（同 spec 叠加）
		{symbol: "Bar", kind: "type", file: "b.go", line: 2},
		{symbol: "Baz", kind: "func", file: "c.go", line: 3},
		{symbol: "Baz", kind: "func", file: "c.go", line: 3}, // 重复
	}
	got := dedupDefs(in)
	if len(got) != 3 {
		t.Fatalf(`dedup 应留 3 条去重后定义, got %d: %+v`, len(got), got)
	}
}

// TestUnusedScanDetail pins the detail summary format (clean vs hit).
//
// TestUnusedScanDetail 钉 detail 摘要格式（干净 vs 命中）。
func TestUnusedScanDetail(t *testing.T) {
	if got := unusedScanDetail(nil); !strings.Contains(got, "no ") {
		t.Fatalf(`空 findings 的 detail 应说明干净: %q`, got)
	}
	got := unusedScanDetail([]UnusedFinding{
		{Symbol: "Foo", Kind: "func"},
		{Symbol: "Bar", Kind: "type"},
	})
	if !strings.Contains(got, "Foo(func)") || !strings.Contains(got, "Bar(type)") {
		t.Fatalf(`detail 应含 符号(类型): %q`, got)
	}
}
