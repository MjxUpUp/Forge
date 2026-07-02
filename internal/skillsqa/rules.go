// Package skillsqa 实现 SkillsHub 的质量校验：规范契约（registry.py 的 R1-R9）
// 与安全审查（audit.py 的 19 条规则 + 加权评分）。1:1 对齐 Python 语义，确保与
// registry.py --json / audit.py 的判定逐条一致（黄金对比基准）。
package skillsqa

import (
	"regexp"
	"sort"
	"strings"
)

// ValidPatterns — metadata.pattern 合法原子值（registry.py VALID_PATTERNS）。
// 支持组合（如 "pipeline + gate"）：split('+') 后每段都必须在此集合。
var ValidPatterns = map[string]bool{
	"tool-wrapper": true, "generator": true, "reviewer": true,
	"inversion": true, "pipeline": true, "gate": true,
	"routing": true, "fallback": true,
}

// HighSignalKW — body 含任一视为有高信号内容（registry.py HIGH_SIGNAL_KW）。
// 注意：Python 用 `kw in body_low` 子串匹配，故 "when.*try.*because" 是字面串
// （几乎不命中），Go 保持 strings.Contains 一致，不改成正则。
var HighSignalKW = []string{
	"decision tree", "决策树", "post-generation", "自查", "review",
	"gotcha", "易错", "checklist", "检查清单", "when.*try.*because",
	"red flag", "rationaliz", "红旗", "借口",
}

// AllowedFm — frontmatter 顶层字段白名单（registry.py ALLOWED_FM，R3 防字段 typo）。
var AllowedFm = map[string]bool{
	"name": true, "description": true, "license": true, "allowed-tools": true,
	"metadata": true, "compatibility": true, "version": true, "requires": true,
}

// ExecExts — 可执行脚本后缀（audit.py EXEC_EXTS）；dangerous_code 规则仅对这些生效。
var ExecExts = map[string]bool{
	".py": true, ".sh": true, ".ps1": true, ".js": true, ".ts": true,
	".mjs": true, ".cjs": true, ".bat": true, ".cmd": true,
}

// SeverityWeight — 风险评分加权（audit.py SEVERITY_WEIGHT）。
var SeverityWeight = map[string]int{
	"INFO": 0, "LOW": 3, "MEDIUM": 8, "HIGH": 15, "CRITICAL": 25,
}

// kebabRe — R1 name 合法格式（registry.py r'[a-z][a-z0-9-]*' fullmatch）。
var kebabRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// allowedFmSorted 返回排序后的允许字段列表（R3 issue 文案用，对齐 Python sorted(ALLOWED_FM)）。
func allowedFmSorted() []string {
	out := make([]string, 0, len(AllowedFm))
	for k := range AllowedFm {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// markdownExt 判断是否 markdown 后缀（audit.py AUDITORS_BY_TYPE 的 .md/.markdown）。
// 用 strings.ToLower 做完整 Unicode 大小写折叠——旧的手写 ASCII-only lower() 会漏掉
// 非 ASCII 大写字母，统一走标准库与 descLow/bodyLow 的判定口径一致。
func markdownExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == ".md" || ext == ".markdown"
}
