package skillsqa

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// Quality 是 R4-R9 各项检查结果（对齐 registry.py quality dict）。
type Quality struct {
	DescLen       int  `json:"desc_len"`
	HasUseWhen    bool `json:"has_use_when"`
	HasSkip       bool `json:"has_skip"`
	ValidPattern  bool `json:"valid_pattern"`
	Over500Lines  bool `json:"over_500_lines"`
	HasHighSignal bool `json:"has_high_signal"`
}

// SkillReport 是单个 skill 的规范审查结果（对齐 registry.py audit_skill 返回值，
// 不含分发目标状态——drift 检测属 skillsdist 职责）。
type SkillReport struct {
	Name        string   `json:"name"`
	Pattern     string   `json:"pattern"`
	Domain      string   `json:"domain"`
	Lines       int      `json:"lines"`
	Description string   `json:"description"`
	Quality     Quality  `json:"quality"`
	Issues      []string `json:"issues"`
	Advisories  []string `json:"advisories,omitempty"`
	Pass        bool     `json:"pass"`
}

// AuditSkill 对单个 skill 目录跑 R1-R11 规范校验。1:1 对齐 registry.py audit_skill。
func AuditSkill(skillDir string) (*SkillReport, error) {
	skillPath := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, err
	}
	text := string(data)
	fm := skillsfm.Parse(data)

	dirName := filepath.Base(skillDir)
	name := fm.Name
	if name == "" {
		name = dirName
	}
	desc := fm.Description
	pattern := fm.Pattern()
	domain := fm.Domain()
	// 行数：与 Python registry.py 一致用 Count("\n")+1（假设文件以 \n 结尾；
	// 无尾换行的文件会多算 1 行——这是与 Python 共享的特性，黄金对比保持故不改）。
	lines := strings.Count(text, "\n") + 1
	descLow := strings.ToLower(desc)
	bodyLow := strings.ToLower(fm.Body)

	var issues []string
	var advisories []string

	// R1 name kebab-case
	if !kebabRe.MatchString(name) {
		issues = append(issues, "name 不符合 kebab-case")
	}
	// R2 name = 目录名
	if name != dirName {
		issues = append(issues, fmt.Sprintf("name(%s) 与目录名(%s)不一致", name, dirName))
	}
	// R3 frontmatter 字段白名单（防 typo）
	var unexpected []string
	for k := range fm.Raw {
		if !AllowedFm[k] {
			unexpected = append(unexpected, k)
		}
	}
	slices.Sort(unexpected)
	if len(unexpected) > 0 {
		issues = append(issues, fmt.Sprintf("frontmatter 未知字段: %v（允许: %v）", unexpected, allowedFmSorted()))
	}
	// R4 description 长度（Python len() 是字符数 → Go 用 RuneCount 对齐，否则中文 3 字节/字符致 R4 失准）
	descLen := utf8.RuneCountInString(desc)
	if descLen < 80 {
		issues = append(issues, fmt.Sprintf(`description 过短(%d字符 <80)`, descLen))
	}
	// R4 上限：Anthropic skill 规范 description ≤1024 字符（硬 issue）；>500 偏长（advisory）
	if descLen > 1024 {
		issues = append(issues, fmt.Sprintf(`description 过长(%d字符 >1024，超 Anthropic skill 规范上限)`, descLen))
	} else if descLen > 500 {
		advisories = append(advisories, fmt.Sprintf(`description 偏长(%d字符 >500，建议精简到 what+when，不总结工作流)`, descLen))
	}
	// R5 Use when
	hasUseWhen := strings.Contains(descLow, "use when")
	if !hasUseWhen {
		issues = append(issues, "description 缺 Use when")
	}
	// R6 SKIP
	hasSkip := strings.Contains(descLow, "skip")
	if !hasSkip {
		issues = append(issues, "description 缺 SKIP")
	}
	// R7 metadata.pattern（单值或 + 组合，每段须合法）
	validPattern := false
	if pattern == "" {
		issues = append(issues, "缺 metadata.pattern")
	} else if ValidPatterns[pattern] {
		validPattern = true
	} else {
		parts := strings.Split(pattern, "+")
		ok := true
		for _, p := range parts {
			if !ValidPatterns[strings.TrimSpace(p)] {
				ok = false
				break
			}
		}
		validPattern = ok
		if !ok {
			issues = append(issues, fmt.Sprintf("pattern 非法: %s", pattern))
		}
	}
	// R8 SKILL.md 行数
	over := lines > 500
	if over {
		issues = append(issues, fmt.Sprintf("SKILL.md 过长(%d行 >500，拆 references)", lines))
	}
	// R9 高信号内容
	hasSignal := false
	for _, kw := range HighSignalKW {
		if strings.Contains(bodyLow, kw) {
			hasSignal = true
			break
		}
	}
	if !hasSignal {
		issues = append(issues, `缺高信号内容(决策树/自查/Gotchas)`)
	}
	// R10 CSO：description 不应总结 body 工作流（advisory，防回归）
	for _, marker := range CSOWorkflowMarkers {
		if strings.Contains(desc, marker) {
			advisories = append(advisories, fmt.Sprintf(`description 含工作流总结词(%s)；CSO 规则：description 只说 what+when，不总结工作流（否则模型照 description 跳过 body）`, marker))
			break
		}
	}
	// R11 references 结构：≤1 level（无子目录，硬）+ >100 行 ref 需 ToC（advisory）
	checkReferences(skillDir, &issues, &advisories)

	return &SkillReport{
		Name:        name,
		Pattern:     pattern,
		Domain:      domain,
		Lines:       lines,
		Description: desc,
		Quality: Quality{
			DescLen:       descLen,
			HasUseWhen:    hasUseWhen,
			HasSkip:       hasSkip,
			ValidPattern:  validPattern,
			Over500Lines:  over,
			HasHighSignal: hasSignal,
		},
		Issues:     issues,
		Advisories: advisories,
		Pass:       len(issues) == 0,
	}, nil
}

// checkReferences 校验 references/ 目录结构（R11）：
//   - ≤1 level：references/ 下直接放文件，不应有子目录（硬 issue）
//   - >100 行的 markdown reference 需 ToC 助导航（advisory；认 ## 目录 / ## Contents / ## Table of Contents）
//
// 无 references 目录时跳过（合法）；目录存在但不可读（权限等）报 advisory。
// TODO: 作用域仅 references/——templates/scripts/adapters 等同级子目录暂不覆盖
//（Anthropic 规范文字只点名 references/，等规范明确后再扩）。
func checkReferences(skillDir string, issues, advisories *[]string) {
	refsDir := filepath.Join(skillDir, "references")
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		if !os.IsNotExist(err) {
			*advisories = append(*advisories, fmt.Sprintf(`references 目录不可读: %v`, err))
		}
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			*issues = append(*issues, fmt.Sprintf(`references/%s/ 是子目录，规范要求平铺（references ≤1 level，文件直接放 references/ 下）`, e.Name()))
			continue
		}
		if !markdownExt(filepath.Ext(e.Name())) {
			continue
		}
		data, rerr := os.ReadFile(filepath.Join(refsDir, e.Name()))
		if rerr != nil {
			continue
		}
		content := string(data)
		lines := strings.Count(content, "\n") + 1
		hasToC := strings.Contains(content, "## 目录") ||
			strings.Contains(content, "## Contents") ||
			strings.Contains(content, "## Table of Contents")
		if lines > 100 && !hasToC {
			*advisories = append(*advisories, fmt.Sprintf(`references/%s 过长(%d行 >100) 缺 ## 目录 ToC（>100 行 reference 建议 ToC 助导航）`, e.Name(), lines))
		}
	}
}
