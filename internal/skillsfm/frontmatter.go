// Package skillsfm 解析 SKILL.md 顶部的 frontmatter 块。
//
// 解析器手写，1:1 移植 SkillsHub admin/scripts/registry.py 的 parse_frontmatter
// 语义。故意不用 gopkg.in/yaml.v3——yaml.v3 对 `>` folded scalar 的折叠规则会
// 保留换行，导致 description 长度与 Python 实现不一致，进而破坏 R4(desc<80)
// 判定与 Python registry.py --json 的黄金对比。逐行解析 + 自定义 block scalar
// 折叠是唯一能保证两者逐字节一致的实现方式。
package skillsfm

import (
	"bytes"
	"regexp"
	"strings"
)

// Frontmatter is the parse result of the YAML block at the top of SKILL.md.
//
// Frontmatter 是 SKILL.md 顶部 YAML 块的解析结果。
type Frontmatter struct {
	Name          string
	Description   string
	License       string
	AllowedTools  string
	Compatibility string
	Version       string
	Requires      string
	Metadata      map[string]string // 嵌套 metadata.* 字段（pattern/domain/steps/composes 等）
	Raw           map[string]string // 所有顶层字段原始值（含未知字段，供白名单校验 R3）
	Body          string            // frontmatter 之后的正文（R9 高信号词检查用）
}

var (
	// frontmatter 块：^---\s*\n(.*?)\n---\s*\n?，(?s) 让 . 匹配换行，对应 Python re.S。
	// 尾部 \n? 允许 frontmatter-only 文件（--- 后直接 EOF 无尾换行）也能匹配；
	// 真实 SKILL.md 都有正文（\n\nbody），不受影响，黄金对比保持。
	// 键字符集含连字符：Anthropic 规范字段 allowed-tools 及任意 kebab-case 键
	// 必须可解析（此前不含 - 时这些键被静默丢弃，AllowedTools 字段与 R3 白名单
	// 对连字符键形全盲——2026-08-29 审查轮功能实证）。
	fmBlockRe  = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n?`)
	topLevelRe = regexp.MustCompile(`^([A-Za-z0-9_-]+):\s*(.*)$`)
	nestedRe   = regexp.MustCompile(`^\s+([A-Za-z0-9_-]+):\s*(.*)$`)
)

// Parse parses the SKILL.md content.
//
// Parse 解析 SKILL.md 内容。无 frontmatter 块时返回空 Frontmatter，Body 为全文。
func Parse(text []byte) *Frontmatter {
	// 规范化：strip UTF-8 BOM + CRLF→LF。Python yaml.safe_load 自动 strip BOM，
	// 手写解析必须自己做——否则 BOM 使 ^--- 永不匹配（frontmatter 整个丢失）、
	// CRLF（Windows autocrlf）让 \r 混入字段值破坏 R4/R5/R6 判定。
	text = bytes.TrimPrefix(text, []byte{0xEF, 0xBB, 0xBF})
	text = bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
	fm := &Frontmatter{Metadata: map[string]string{}, Raw: map[string]string{}}

	loc := fmBlockRe.FindSubmatchIndex(text)
	if loc == nil {
		fm.Body = string(text)
		return fm
	}
	// Capture group 1 (block content)
	fmRaw := string(text[loc[2]:loc[3]]) // 捕获组 1（块内容）
	// After the whole match (body)
	fm.Body = string(text[loc[1]:]) // 整个匹配之后（正文）

	lines := strings.Split(fmRaw, "\n")
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			i++
			continue
		}
		if mm := topLevelRe.FindStringSubmatch(line); mm != nil {
			key, val := mm[1], strings.TrimSpace(mm[2])
			if isBlockScalarHeader(val) {
				// YAML block scalar：收集后续缩进行（以空格开头或空行），
				// `>` folded 用空格 join、`|` literal 用换行 join（对齐 Python）。
				// chomping 指示符（`>-`、`>+`、`|-`、`|+`——Anthropic 生态常用 `>-`）按前缀
				// 识别；本实现的 buffer join 无尾部换行可剥/可留，指示符只区分 fold 与 literal。
				buf := []string{}
				i++
				for i < len(lines) && (strings.HasPrefix(lines[i], " ") || lines[i] == "") {
					if t := strings.TrimSpace(lines[i]); t != "" {
						buf = append(buf, t)
					}
					i++
				}
				if val[0] == '>' {
					val = strings.Join(buf, " ")
				} else {
					val = strings.Join(buf, "\n")
				}
			} else {
				i++
				val = stripQuotes(val)
			}
			fm.Raw[key] = val
			switch key {
			case "name":
				fm.Name = val
			case "description":
				fm.Description = val
			case "license":
				fm.License = val
			case "allowed-tools":
				fm.AllowedTools = val
			case "compatibility":
				fm.Compatibility = val
			case "version":
				fm.Version = val
			case "requires":
				fm.Requires = val
			}
		} else if strings.HasPrefix(line, " ") && len(fm.Raw) > 0 {
			// 嵌套 metadata.*（仅当已有顶层字段，对齐 Python `elif ... and fm`）
			// 值与顶层字段走同一套剥引号——`pattern: "gate"` 必须得到 gate 而非 `"gate"`
			// （带引号的 pattern 会让 R7 模式匹配误判）。
			if mm2 := nestedRe.FindStringSubmatch(line); mm2 != nil {
				fm.Metadata[mm2[1]] = stripQuotes(strings.TrimSpace(mm2[2]))
			}
			i++
		} else {
			i++
		}
	}
	return fm
}

// isBlockScalarHeader 判断顶层字段值是否开启 YAML block scalar：
// `>` / `|` 及 chomping 指示符形式 `>-`、`>+`、`|-`、`|+`。
func isBlockScalarHeader(val string) bool {
	if len(val) == 0 || (val[0] != '>' && val[0] != '|') {
		return false
	}
	return len(val) == 1 || (len(val) == 2 && (val[1] == '-' || val[1] == '+'))
}

// stripQuotes 剥一层成对的首尾引号。要求 len >= 2 且首尾为同一引号字符——
// 单字符值（如 `"`）会同时满足 HasPrefix+HasSuffix，直接切 val[1:len-1] 会 panic。
func stripQuotes(val string) string {
	if len(val) >= 2 &&
		((val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'')) {
		return val[1 : len(val)-1]
	}
	return val
}

// Pattern returns metadata.pattern (combination patterns like `pipeline + gate` returned as-is).
//
// Pattern 返回 metadata.pattern（组合模式如 "pipeline + gate" 原样返回）。
func (f *Frontmatter) Pattern() string { return f.Metadata["pattern"] }

// Domain returns metadata.domain.
//
// Domain 返回 metadata.domain。
func (f *Frontmatter) Domain() string { return f.Metadata["domain"] }
