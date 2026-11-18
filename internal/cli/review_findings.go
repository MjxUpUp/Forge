package cli

import (
	"fmt"
	"strings"
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
// 缺 id 的清单项一律报错——宽容解析会让「未闭合项承接」变成可绕过的散文。
func parseFindings(raw string) ([]reviewFinding, error) {
	var out []reviewFinding
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
		if strings.Contains(strings.ToLower(f.Text), "(waived") || strings.Contains(strings.ToLower(f.Text), "（waived") {
			f.Waived = true
		}
		out = append(out, f)
	}
	return out, nil
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
