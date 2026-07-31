package skillsfm

import (
	"strings"
	"testing"
)

// foldedSrc mirrors the real-world shape of tdd-cycle: description: > followed by two indented body lines.
// Python parse_frontmatter strips both lines and joins them with a single space; this test pins that behavior.
//
// foldedDescription 是 tdd-cycle 的真实格式：description: > 后跟两行缩进正文。
// Python parse_frontmatter 把两行 strip 后用空格 join 成单行。本测试锁定该语义。
const foldedSrc = "---\n" +
	"name: tdd-cycle\n" +
	"description: >\n" +
	"  测试驱动开发强制循环。Use when: 实现任何功能前、想跳过TDD时。\n" +
	"  先写失败的测试。SKIP: 测试质量守卫用 test-discipline。\n" +
	"metadata:\n" +
	"  pattern: pipeline + gate\n" +
	"  domain: testing\n" +
	"---\n\n" +
	"# 正文\n\n决策树：先做啥。\n"

func TestParse_FoldedDescription(t *testing.T) {
	fm := Parse([]byte(foldedSrc))
	if fm.Name != "tdd-cycle" {
		t.Fatalf("name = %q, want tdd-cycle", fm.Name)
	}
	// The two folded lines must be joined with a single space (not a newline); otherwise it diverges from Python desc_len.
	//
	// folded 两行必须用单空格 join（不是换行），否则与 Python desc_len 不一致
	wantDesc := "测试驱动开发强制循环。Use when: 实现任何功能前、想跳过TDD时。 先写失败的测试。SKIP: 测试质量守卫用 test-discipline。"
	if fm.Description != wantDesc {
		t.Fatalf("description folded join mismatch:\n got: %q\nwant: %q", fm.Description, wantDesc)
	}
	if fm.Pattern() != "pipeline + gate" {
		t.Fatalf("pattern = %q, want 'pipeline + gate'", fm.Pattern())
	}
	if fm.Domain() != "testing" {
		t.Fatalf("domain = %q, want testing", fm.Domain())
	}
	if fm.Body[:2] != "# " {
		t.Fatalf("body should start with markdown heading, got %q", fm.Body[:min(10, len(fm.Body))])
	}
	// description must not contain newlines (the defining property of folded style).
	//
	// description 不含换行（folded 关键特性）
	if containsNewline(fm.Description) {
		t.Fatalf("folded description must not contain newlines: %q", fm.Description)
	}
}

func TestParse_LiteralDescription(t *testing.T) {
	src := "---\nname: x\ndescription: |\n  line1\n  line2\n---\nbody\n"
	fm := Parse([]byte(src))
	wantDesc := "line1\nline2"
	if fm.Description != wantDesc {
		t.Fatalf("literal description = %q, want %q", fm.Description, wantDesc)
	}
}

func TestParse_QuotedValue(t *testing.T) {
	src := "---\nname: lark-router\ndescription: \"飞书路由。Use when: 飞书。SKIP: 非飞书。\"\n---\nbody\n"
	fm := Parse([]byte(src))
	if fm.Description != "飞书路由。Use when: 飞书。SKIP: 非飞书。" {
		t.Fatalf("quoted value not stripped: %q", fm.Description)
	}
}

func TestParse_NoFrontmatter(t *testing.T) {
	src := "# just a doc\nno frontmatter here\n"
	fm := Parse([]byte(src))
	if fm.Name != "" || len(fm.Raw) != 0 {
		t.Fatalf("expected empty frontmatter, got %+v", fm)
	}
	if fm.Body != src {
		t.Fatalf("body should be full text when no frontmatter block")
	}
}

func TestParse_CommentLines(t *testing.T) {
	src := "---\n# 这是注释\nname: x\n# 另一个注释\ndescription: ok Use when: a. SKIP: b.\n---\nbody\n"
	fm := Parse([]byte(src))
	if fm.Name != "x" {
		t.Fatalf("name = %q, want x (comment should be skipped)", fm.Name)
	}
	if _, ok := fm.Raw["# 这是注释"]; ok {
		t.Fatalf("comment line leaked into Raw")
	}
}

func TestParse_UnknownFieldsPreserved(t *testing.T) {
	// Unknown fields (e.g. the typo patten:) must be preserved in Raw for the R3 whitelist check.
	//
	// 未知字段（如 typo patten:）必须保留在 Raw 供 R3 白名单校验
	src := "---\nname: x\npatten: reviewer\n---\nbody\n"
	fm := Parse([]byte(src))
	if v, ok := fm.Raw["patten"]; !ok || v != "reviewer" {
		t.Fatalf("unknown field 'patten' not preserved in Raw: %+v", fm.Raw)
	}
}

func TestParse_MetadataOnlyAfterTopLevel(t *testing.T) {
	// Nested metadata is captured only after a top-level field exists (mirrors the Python `and fm` guard).
	//
	// 嵌套 metadata 只在已有顶层字段后捕获（对齐 Python `and fm`）
	src := "---\n  orphan: value\nname: x\nmetadata:\n  pattern: gate\n---\nbody\n"
	fm := Parse([]byte(src))
	// orphan appears before any top-level field, so it must not be captured as metadata.
	//
	// orphan 出现在任何顶层字段前，不应被捕获为 metadata
	if _, ok := fm.Metadata["orphan"]; ok {
		t.Fatalf("orphan nested line before any top-level field should not be captured")
	}
	if fm.Pattern() != "gate" {
		t.Fatalf("pattern = %q, want gate", fm.Pattern())
	}
}

func TestParse_DescLengthConsistency(t *testing.T) {
	// Pin the R4 verdict baseline: folded description length equals the joined-string length (matches Python).
	//
	// 锁定 R4 判定基准：folded description 长度 = join 后字符串长度（Python 一致）
	fm := Parse([]byte(foldedSrc))
	wantLen := len("测试驱动开发强制循环。Use when: 实现任何功能前、想跳过TDD时。 先写失败的测试。SKIP: 测试质量守卫用 test-discipline。")
	if len(fm.Description) != wantLen {
		t.Fatalf("description len = %d, want %d (R4 baseline)", len(fm.Description), wantLen)
	}
}

func containsNewline(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return true
		}
	}
	return false
}

func TestParse_StripsBOM(t *testing.T) {
	// A UTF-8 BOM (\xEF\xBB\xBF) prefix makes ^--- never match, so the entire frontmatter block would be lost as body.
	// Python yaml.safe_load strips BOM automatically; our hand-written parser must do it itself. This guards R1-R11 against BOM breakthrough.
	//
	// UTF-8 BOM（\xEF\xBB\xBF）前缀让 ^--- 永不匹配，整个 frontmatter 会被当正文丢失。
	// Python yaml.safe_load 自动 strip BOM；手写解析必须自己做。守护 R1-R11 不被 BOM 击穿。
	src := "\xEF\xBB\xBF---\nname: bom\ndescription: d Use when: a. SKIP: b.\n---\nbody\n"
	fm := Parse([]byte(src))
	if fm.Name != "bom" {
		t.Fatalf("BOM not stripped: name=%q (frontmatter block lost)", fm.Name)
	}
	if fm.Description != "d Use when: a. SKIP: b." {
		t.Fatalf("BOM stripped but desc wrong: %q", fm.Description)
	}
}

func TestParse_NormalizesCRLF(t *testing.T) {
	// Windows git autocrlf turns \n into \r\n; without normalization, \r sticks to the tail of field values,
	// leaving an extra \r at the end of `use when` that breaks R5 substring matching and skews R4 length.
	//
	// Windows git autocrlf 会把 \n 变 \r\n；不归一化的话 \r 会粘在字段值尾部，
	// 让"use when"末尾多一个 \r 破坏 R5 子串匹配、R4 长度也算错。
	src := "---\r\nname: crlf\r\ndescription: d Use when: a. SKIP: b.\r\n---\r\nbody\r\n"
	fm := Parse([]byte(src))
	if fm.Name != "crlf" {
		t.Fatalf("CRLF not normalized: name=%q", fm.Name)
	}
	if strings.HasSuffix(fm.Name, "\r") || strings.HasSuffix(fm.Description, "\r") {
		t.Fatalf("CR leaked into field value: name=%q desc=%q", fm.Name, fm.Description)
	}
}

func TestParse_FrontmatterOnlyEOF(t *testing.T) {
	// Edge case: the frontmatter block ends directly at EOF, with no trailing newline and no body.
	// The trailing \n? in fmBlockRe lets such a file still match (real SKILL.md always has a body; this is a robustness fallback).
	//
	// 极端边界：frontmatter 块结束后直接 EOF，无尾换行也无正文。
	// fmBlockRe 尾部 \n? 让这种文件也能匹配（真实 SKILL.md 都有正文，此为鲁棒性兜底）。
	src := "---\nname: eof\ndescription: d Use when: a. SKIP: b.\n---"
	fm := Parse([]byte(src))
	if fm.Name != "eof" {
		t.Fatalf("frontmatter-only EOF not matched: name=%q", fm.Name)
	}
	if fm.Body != "" {
		t.Fatalf("EOF body should be empty, got %q", fm.Body)
	}
}

// TestParse_SingleQuoteCharValue: a field value that is a single quote character (`name: "`)
// satisfies HasPrefix+HasSuffix simultaneously — stripping must require len>=2, otherwise
// val[1:len-1] panics. The value is preserved as-is.
//
// TestParse_SingleQuoteCharValue：字段值为单个引号字符（`name: "`）时 HasPrefix+HasSuffix
// 同时成立——剥引号必须要求 len>=2，否则 val[1:len-1] panic。值原样保留。
func TestParse_SingleQuoteCharValue(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"---\nname: \"\ndescription: d\n---\nbody\n", `"`},
		{"---\nname: '\ndescription: d\n---\nbody\n", `'`},
	} {
		fm := Parse([]byte(tc.src)) // must not panic
		if fm.Name != tc.want {
			t.Fatalf("单字符引号值应原样保留: name=%q want %q (src=%q)", fm.Name, tc.want, tc.src)
		}
	}
}

// TestParse_BlockScalarChomping: block scalar headers with chomping indicators
// (`>-`, `>+`, `|-`, `|+` — `>-` is common in the Anthropic ecosystem) must be parsed as
// block scalars, not kept as the literal string ">-".
//
// TestParse_BlockScalarChomping：带 chomping 指示符的 block scalar 头
// （`>-`、`>+`、`|-`、`|+`——Anthropic 生态常用 `>-`）必须按 block scalar 解析，
// 不能留成字面字符串 ">-"。
func TestParse_BlockScalarChomping(t *testing.T) {
	folded := "---\nname: x\ndescription: >-\n  line1\n  line2\n---\nbody\n"
	fm := Parse([]byte(folded))
	if fm.Description != "line1 line2" {
		t.Fatalf(">- folded = %q, want %q", fm.Description, "line1 line2")
	}
	literal := "---\nname: x\ndescription: |+\n  line1\n  line2\n---\nbody\n"
	fm = Parse([]byte(literal))
	if fm.Description != "line1\nline2" {
		t.Fatalf("|+ literal = %q, want %q", fm.Description, "line1\nline2")
	}
	// `>+` folded as well
	//
	// `>+` 同样按 folded
	plus := "---\nname: x\ndescription: >+\n  a\n  b\n---\nbody\n"
	fm = Parse([]byte(plus))
	if fm.Description != "a b" {
		t.Fatalf(">+ folded = %q, want %q", fm.Description, "a b")
	}
}

// TestParse_MetadataQuotedValue: nested metadata values must go through the same quote
// stripping as top-level fields — `pattern: "gate"` yields gate (a quoted pattern breaks
// R7's pattern matching).
//
// TestParse_MetadataQuotedValue：嵌套 metadata 值必须与顶层字段走同一套剥引号——
// `pattern: "gate"` 得到 gate（带引号的 pattern 会让 R7 误判）。
func TestParse_MetadataQuotedValue(t *testing.T) {
	src := "---\nname: x\ndescription: d\nmetadata:\n  pattern: \"gate\"\n  domain: 'testing'\n---\nbody\n"
	fm := Parse([]byte(src))
	if fm.Pattern() != "gate" {
		t.Fatalf("quoted metadata pattern = %q, want gate", fm.Pattern())
	}
	if fm.Domain() != "testing" {
		t.Fatalf("quoted metadata domain = %q, want testing", fm.Domain())
	}
}
