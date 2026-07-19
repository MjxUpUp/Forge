package skillsfm

import (
	"strings"
	"testing"
)

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
	// 未知字段（如 typo patten:）必须保留在 Raw 供 R3 白名单校验
	src := "---\nname: x\npatten: reviewer\n---\nbody\n"
	fm := Parse([]byte(src))
	if v, ok := fm.Raw["patten"]; !ok || v != "reviewer" {
		t.Fatalf("unknown field 'patten' not preserved in Raw: %+v", fm.Raw)
	}
}

func TestParse_MetadataOnlyAfterTopLevel(t *testing.T) {
	// 嵌套 metadata 只在已有顶层字段后捕获（对齐 Python `and fm`）
	src := "---\n  orphan: value\nname: x\nmetadata:\n  pattern: gate\n---\nbody\n"
	fm := Parse([]byte(src))
	// orphan 出现在任何顶层字段前，不应被捕获为 metadata
	if _, ok := fm.Metadata["orphan"]; ok {
		t.Fatalf("orphan nested line before any top-level field should not be captured")
	}
	if fm.Pattern() != "gate" {
		t.Fatalf("pattern = %q, want gate", fm.Pattern())
	}
}

func TestParse_DescLengthConsistency(t *testing.T) {
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
	// Windows git autocrlf 会把 \n 变 \r\n；不归一化的话 \r 会粘在字段值尾部，
	// 让 "use when" 末尾多一个 \r 破坏 R5 子串匹配、R4 长度也算错。
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
