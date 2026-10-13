package taskpipeline

// approvaldigest.go —— 审批摘要（design-stage-hardening W5.1）：`artifact --approve`
// 批之前先给人一屏摘要——TL;DR、风险与高危步骤、Open Questions、相对上次批准的
// 变化。人审的是决策与风险，不是逐行审计划（CHI 2025 Plan-Then-Execute：人对 AI
// 计划的信任校准度只有 0.50，过细的计划反而增加认知负担）。
//
// approvaldigest.go — the one-screen approval digest shown before a human signs
// an artifact: TL;DR, risks/risky steps, open questions, and what changed since
// the last approval.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/MjxUpUp/Forge/internal/util"
)

// approvedSnapshotDir holds the content each stage had when it was last approved.
//
// approvedSnapshotDir 存放各 stage 最近一次被批准时的内容。
const approvedSnapshotDir = "approved"

// digestSectionLines caps the lines shown per digest section (one screen total).
//
// digestSectionLines 是摘要每节显示的行数上限（总量控制在一屏）。
const digestSectionLines = 6

// ApprovedSnapshotPath is where the last-approved content of a stage is kept.
//
// ApprovedSnapshotPath 是 stage 最近一次被批准内容的存放位置。
func ApprovedSnapshotPath(root, taskRef, stage string) string {
	return filepath.Join(SpecsDir(root, taskRef), approvedSnapshotDir, stage+".md")
}

// SaveApprovedSnapshot copies the artifact at srcAbs into the approved-snapshot slot.
//
// SaveApprovedSnapshot 把 srcAbs 处的产物复制进已批准快照位。
func SaveApprovedSnapshot(root, taskRef, stage, srcAbs string) error {
	if err := ValidateStageName(stage); err != nil {
		return err
	}
	body, err := os.ReadFile(srcAbs)
	if err != nil {
		return err
	}
	dst := ApprovedSnapshotPath(root, taskRef, stage)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return util.AtomicWrite(dst, body, 0o644)
}

// stripFrontmatter drops a leading YAML frontmatter block.
func stripFrontmatter(text string) string {
	if !strings.HasPrefix(text, "---\n") {
		return text
	}
	if end := strings.Index(text[4:], "\n---"); end >= 0 {
		rest := text[4+end+4:]
		return strings.TrimLeft(rest, "\n")
	}
	return text
}

var (
	digestTLDRRe  = regexp.MustCompile(`(?i)^#{1,6}\s*(tl;dr|tldr|摘要|结论)`)
	digestRiskRe  = regexp.MustCompile(`(?i)^#{1,6}.*(风险|高危|risk)`)
	digestOpenQRe = regexp.MustCompile(`(?i)^#{1,6}.*(open questions|待决问题|未决问题)`)
)

// sectionBody returns up to max non-empty body lines of the first section whose
// heading matches re, plus whether the section exists.
//
// sectionBody 返回首个标题匹配 re 的章节中至多 max 行非空正文，以及该节是否存在。
func sectionBody(text string, re *regexp.Regexp, max int) ([]string, bool) {
	var out []string
	in, found, level := false, false, 0
	extra := 0
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, " \t")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			l := len(trimmed) - len(strings.TrimLeft(trimmed, "#"))
			if in && l <= level {
				break
			}
			if !in && re.MatchString(trimmed) {
				in, found, level = true, true, l
			}
			continue
		}
		if !in || trimmed == "" {
			continue
		}
		if len(out) < max {
			out = append(out, trimmed)
		} else {
			extra++
		}
	}
	if extra > 0 {
		out = append(out, fmt.Sprintf("…（另 %d 行）", extra))
	}
	return out, found
}

// changeSummary compares two artifact texts line-wise and names the sections
// with added or removed lines.
//
// changeSummary 逐行比较两版产物，点名有增删行的章节。
func changeSummary(prev, cur string) string {
	count := func(text string) map[string]int {
		m := map[string]int{}
		for _, l := range strings.Split(text, "\n") {
			if l = strings.TrimSpace(l); l != "" {
				m[l]++
			}
		}
		return m
	}
	prevSet, curSet := count(prev), count(cur)
	touched := map[string]bool{}
	var order []string
	note := func(h string) {
		if !touched[h] {
			touched[h] = true
			order = append(order, h)
		}
	}
	added, removed := 0, 0
	walk := func(text string, other map[string]int, onHit func()) {
		heading := "（开头）"
		seen := map[string]int{}
		for _, l := range strings.Split(text, "\n") {
			t := strings.TrimSpace(l)
			if t == "" {
				continue
			}
			if strings.HasPrefix(t, "#") {
				heading = strings.TrimSpace(strings.TrimLeft(t, "#"))
			}
			seen[t]++
			if seen[t] > other[t] {
				onHit()
				note(heading)
			}
		}
	}
	walk(cur, prevSet, func() { added++ })
	walk(prev, curSet, func() { removed++ })
	if added == 0 && removed == 0 {
		return "与上次批准的内容一致"
	}
	return fmt.Sprintf("+%d / -%d 行；涉及章节：%s", added, removed, strings.Join(order, "、"))
}

// RenderApprovalDigest renders the one-screen digest for stage. prev is the
// last-approved content ("" = first approval).
//
// RenderApprovalDigest 渲染 stage 的一屏审批摘要。prev 是上次批准的内容（"" = 首次审批）。
func RenderApprovalDigest(stage, body, prev string) string {
	cur := stripFrontmatter(body)
	var b strings.Builder
	fmt.Fprintf(&b, "── 审批摘要：%s ──\n", stage)
	section := func(title string, re *regexp.Regexp, missing string) {
		lines, found := sectionBody(cur, re, digestSectionLines)
		fmt.Fprintf(&b, "【%s】", title)
		switch {
		case !found:
			fmt.Fprintf(&b, "%s\n", missing)
		case len(lines) == 0:
			fmt.Fprintf(&b, "（章节为空）\n")
		default:
			b.WriteString("\n")
			for _, l := range lines {
				fmt.Fprintf(&b, "  %s\n", l)
			}
		}
	}
	section("TL;DR", digestTLDRRe, "缺失——方案没有首屏结论，建议打回补 TL;DR")
	section("风险与高危步骤", digestRiskRe, "未单列——迁移/删除/鉴权/外部调用类步骤应集中列出")
	section("Open Questions", digestOpenQRe, "无此节")
	fmt.Fprintf(&b, "【自上次批准以来的变化】")
	if strings.TrimSpace(prev) == "" {
		b.WriteString("首次审批\n")
	} else {
		b.WriteString(changeSummary(stripFrontmatter(prev), cur) + "\n")
	}
	b.WriteString("──\n")
	return b.String()
}
