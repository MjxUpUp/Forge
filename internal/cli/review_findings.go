package cli

import (
	"fmt"
	"strings"
	"unicode"
)

// reviewFinding 是评审问题清单里的一条。
//
// 清单格式（markdown，一行一条）：
//
//   - [ ] F1 未闭合的问题
//   - [x] F2 已修复关闭
//   - [~] F3 (waived: 本轮不修的理由)
//
// ID 取方括号后的第一个词；`[~]` 表示显式豁免（未修但接受）。
type reviewFinding struct {
	ID     string
	Text   string
	Open   bool
	Waived bool
}

// parseFindings 解析评审问题清单。空行与 markdown 标题忽略；其余非清单行、以及
// 缺 id、id 重复的清单项一律报错——宽容解析会让「未闭合项承接」变成可绕过的散文，
// 重复 id 会让「哪一行算数」变成隐式约定（review-rfe-1 P3-i 实证）。
func parseFindings(raw string) ([]reviewFinding, error) {
	var out []reviewFinding
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(line)
		lower := strings.ToLower(t)
		var f reviewFinding
		switch {
		case strings.HasPrefix(lower, "- [ ]"), strings.HasPrefix(lower, "* [ ]"):
			f.Open = true
			t = t[5:]
		case strings.HasPrefix(lower, "- [x]"), strings.HasPrefix(lower, "* [x]"):
			t = t[5:]
		case strings.HasPrefix(lower, "- [~]"), strings.HasPrefix(lower, "* [~]"):
			f.Waived = true
			t = t[5:]
		default:
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			return nil, fmt.Errorf("清单行无法解析（应为 `- [ ] id 描述` / `- [x] id 描述` / `- [~] id (waived: 原因)`）: %q", line)
		}
		fields := strings.Fields(strings.TrimSpace(t))
		if len(fields) == 0 {
			return nil, fmt.Errorf("清单项缺少 id: %q", line)
		}
		f.ID = strings.TrimSuffix(fields[0], ":")
		f.Text = strings.TrimSpace(t)
		if seen[strings.ToLower(f.ID)] {
			return nil, fmt.Errorf("清单里 ID 重复：%s（同一 id 只能出现一次）", f.ID)
		}
		seen[strings.ToLower(f.ID)] = true
		if f.Waived {
			// 豁免必须写明原因：`- [~] id` 不带原因等于让上一轮的未闭合项
			// 无声消失（review-rfe-1 P2-b 实证过）。
			if !waivedHasReason(f.Text) {
				return nil, fmt.Errorf("豁免必须写原因（`- [~] %s (waived: 原因)`）: %q", f.ID, line)
			}
		}
		out = append(out, f)
	}
	return out, nil
}

// waivedHasReason 判断豁免是否带了非空原因：接受半角/全角括号与冒号；
// 原因里只有空白（含全角空格、零宽空格）不算原因——否则「豁免必须写原因」
// 退化成敲一个全角空格（review-rfe-1 P2-e 实证）。
func waivedHasReason(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{"(waived", "（waived"} {
		idx := strings.Index(lower, marker)
		if idx < 0 {
			continue
		}
		rest := lower[idx+len(marker):]
		rest = strings.TrimLeftFunc(rest, func(r rune) bool {
			return unicode.IsSpace(r) || r == 0x200b || r == ':' || r == '：'
		})
		rest = strings.TrimRightFunc(rest, func(r rune) bool {
			return unicode.IsSpace(r) || r == ')' || r == '）'
		})
		// 只认可打印字符：零宽连接符（U+200C）、BOM（U+FEFF）这类不可见字符
		// 看着像「写了原因」，实际什么都没说——上一轮 review-rfe-1 用它们绕过了。
		rest = strings.TrimFunc(rest, func(r rune) bool { return !unicode.IsGraphic(r) })
		if rest != "" {
			return true
		}
	}
	return false
}

// findFinding 按 ID 找清单条目（大小写不敏感）。
func findFinding(fs []reviewFinding, id string) (reviewFinding, bool) {
	for _, f := range fs {
		if strings.EqualFold(f.ID, id) {
			return f, true
		}
	}
	return reviewFinding{}, false
}

// describeFindings 把本轮清单压成一行审计文本。
func describeFindings(fs []reviewFinding) string {
	open, closed, waived := 0, 0, 0
	for _, f := range fs {
		switch {
		case f.Waived:
			waived++
		case f.Open:
			open++
		default:
			closed++
		}
	}
	return fmt.Sprintf("findings: open=%d closed=%d waived=%d", open, closed, waived)
}
