// Package skillsqa 实现 SkillsHub 的质量校验：规范契约（registry.py 的 R1-R11）
// 与安全审查（audit.py 的 19 条规则 + 加权评分）。1:1 对齐 Python 语义，确保与
// registry.py --json / audit.py 的判定逐条一致（黄金对比基准）。
package skillsqa

import (
	"maps"
	"regexp"
	"slices"
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

// CSOWorkflowMarkers — description 不应含的工作流总结词（CSO 规则：description 只说
// what + when，不总结 body 工作流，否则模型照 description 行动而跳过 SKILL.md body）。
// 启发式高置信中文词组，命中走 advisory（防回归，不阻断 Pass）。
// 三方背书：Anthropic best-practices（description 不复述 workflow）+ Steve Kinney AP-1
// + Lost in the Middle（arXiv:2307.03172，模型偏重开头描述而漏 body）。
var CSOWorkflowMarkers = []string{
	"完整工作流", "完整流程", "全流程", "完整协议", "完整编排", "全链路", "全工序",
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

// HtmlExts — HTML 后缀；prompt_injection / data_exfiltration 规则对这些生效，
// dangerous_code 中 HtmlAlso=true 的（DC-1 eval / DC-7 浏览器执行向量）也生效。
// HTML 是 injection/代码执行高危载体：PI-4 隐藏指令注释、PI-5 零宽字符、DE 外发指令、
// HTML 内嵌 <script>eval(...)/new Function(...)/document.write(...) 都是真实攻击面。
// 其余 DC（child_process/os.system 等后端 API）不接 HTML——HTML 非直接可执行后缀，
// 后端 API 关键词在说明文本易误报。
// 2026-07：prototype-confirmation 引入首个 .html canonical 资产暴露盲区——PI-4 此前
// 从不扫真正的 .html；DC-1 eval 此前走 ExecOnly 也不扫 .html（HTML 内嵌 XSS 漏报）。
var HtmlExts = map[string]bool{
	".html": true, ".htm": true,
}

// SeverityWeight — 风险评分加权（audit.py SEVERITY_WEIGHT）。
var SeverityWeight = map[string]int{
	"INFO": 0, "LOW": 3, "MEDIUM": 8, "HIGH": 15, "CRITICAL": 25,
}

// kebabRe — R1 name 合法格式（registry.py r'[a-z][a-z0-9-]*' fullmatch）。
var kebabRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// allowedFmSorted 返回排序后的允许字段列表（R3 issue 文案用，对齐 Python sorted(ALLOWED_FM)）。
func allowedFmSorted() []string {
	return slices.Sorted(maps.Keys(AllowedFm))
}

// markdownExt 判断是否 markdown 后缀（audit.py AUDITORS_BY_TYPE 的 .md/.markdown）。
// 用 strings.ToLower 做完整 Unicode 大小写折叠——旧的手写 ASCII-only lower() 会漏掉
// 非 ASCII 大写字母，统一走标准库与 descLow/bodyLow 的判定口径一致。
func markdownExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == ".md" || ext == ".markdown"
}
