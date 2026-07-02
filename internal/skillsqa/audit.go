package skillsqa

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Finding 是安全审查发现（对齐 audit.py Finding dataclass）。
type Finding struct {
	RuleID      string  `json:"rule_id"`
	Message     string  `json:"message"`
	Severity    string  `json:"severity"`
	Confidence  float64 `json:"confidence"`
	File        string  `json:"file"`
	StartLine   int     `json:"start_line"`
	Category    string  `json:"category"`
	Matched     string  `json:"matched"`
	Remediation string  `json:"remediation"`
}

// Rule 是声明式安全规则（对齐 audit.py 规则元组）。ExecOnly=true 仅对可执行脚本生效。
type Rule struct {
	ID       string
	Pattern  string
	Severity string
	Conf     float64
	Msg      string
	Fix      string
	Cat      string
	ExecOnly bool
	re       *regexp.Regexp
}

// skipDirs — 扫描时跳过的目录段（对齐 audit.py 的 node_modules/.git/__pycache__/.venv）。
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true, ".venv": true,
}

// auditRules 是 19 条规则。正则已从 Python 语法转为 Go RE2：
//   - PI-5 零宽字符 → \x{200b}（Go 不支持 \u）
//   - 每条加 (?i) 前缀对齐 Python re.IGNORECASE
var auditRules = mustCompile([]Rule{
	// PROMPT_INJECTION（5）
	{ID: "PI-1", Cat: "prompt_injection", Severity: "HIGH", Conf: 0.9, Msg: "指令覆盖：要求忽略先前指令", Fix: "删除该指令；skill 不应改写 agent 基础对齐",
		Pattern: `(?i)ignore\s+(?:all\s+)?previous\s+instructions?`},
	{ID: "PI-2", Cat: "prompt_injection", Severity: "CRITICAL", Conf: 0.95, Msg: "绕过安全约束", Fix: "删除",
		Pattern: `(?i)ignore\s+(?:all\s+)?(?:safety|security)\s+(?:rules?|constraints?|guidelines?)`},
	{ID: "PI-3", Cat: "prompt_injection", Severity: "CRITICAL", Conf: 0.95, Msg: "越狱模式触发", Fix: "删除",
		Pattern: `(?i)you\s+are\s+now\s+(?:in\s+)?(?:jailbreak|unrestricted|unfiltered)\s+mode`},
	{ID: "PI-4", Cat: "prompt_injection", Severity: "HIGH", Conf: 0.7, Msg: "HTML 注释隐藏指令", Fix: "删除隐藏指令",
		Pattern: `(?i)<!--[^>]{0,200}?(?:ignore|system\s+prompt|exfiltrat|send\s+to\s+http)[^>]{0,200}?-->`},
	{ID: "PI-5", Cat: "prompt_injection", Severity: "MEDIUM", Conf: 0.6, Msg: "零宽字符可能藏指令", Fix: "检查并清除不可见字符",
		Pattern: `(?i)[\x{200b}\x{200c}\x{200d}\x{2060}\x{feff}]{3,}`},
	// DATA_EXFIL（4）
	{ID: "DE-1", Cat: "data_exfiltration", Severity: "CRITICAL", Conf: 0.9, Msg: "指示把对话上下文外发", Fix: "删除外发指令；如需遥测须显式告知用户并取得同意",
		Pattern: `(?i)(?:send|transmit|upload|post|forward)\s+(?:the\s+)?(?:conversation|context|chat\s+history|session)\s+to\s+(?:https?://|an?\s+external)`},
	{ID: "DE-2", Cat: "data_exfiltration", Severity: "CRITICAL", Conf: 0.9, Msg: "静默/隐瞒式外发", Fix: "删除；任何数据外发必须用户可见",
		Pattern: `(?i)(?:silently|quietly|secretly|covertly|without\s+(?:telling|informing)\s+the\s+user)\s+.{0,40}?(?:send|transmit|upload|log|exfiltrat)`},
	{ID: "DE-3", Cat: "data_exfiltration", Severity: "HIGH", Conf: 0.8, Msg: "指示附带密钥/凭据外发", Fix: "删除；凭据绝不应进入 prompt/输出",
		Pattern: `(?i)(?:include|append|attach)\s+(?:the\s+)?(?:api\s+key|secret|token|\.env|password|credentials?)\b`},
	{ID: "DE-4", Cat: "data_exfiltration", Severity: "HIGH", Conf: 0.85, Msg: "出现 exfiltrate 字样", Fix: "核实意图，正常 skill 不应出现该词",
		Pattern: `(?i)exfiltrat\w*`},
	// SYSTEM_PROMPT_LEAKAGE（2）
	{ID: "SL-1", Cat: "system_prompt_leakage", Severity: "HIGH", Conf: 0.85, Msg: "要求泄漏系统提示", Fix: "删除",
		Pattern: `(?i)(?:repeat|reveal|output|show|print|leak)\s+(?:your\s+)?(?:system\s+prompt|initial\s+instructions?|hidden\s+instructions?|core\s+instructions?)`},
	{ID: "SL-2", Cat: "system_prompt_leakage", Severity: "MEDIUM", Conf: 0.7, Msg: "探测系统提示", Fix: "删除",
		Pattern: `(?i)(?:what\s+are\s+your|give\s+me\s+your)\s+(?:system\s+prompt|instructions?|rules?)`},
	// DANGEROUS_CODE（6，仅可执行脚本）
	{ID: "DC-1", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.8, Msg: "eval() 任意代码执行", Fix: "避免 eval；用安全解析", ExecOnly: true,
		Pattern: `(?i)\beval\s*\(`},
	{ID: "DC-2", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.7, Msg: "child_process.exec() 任意代码执行", Fix: "避免 child_process.exec；用安全解析", ExecOnly: true,
		// 原 Pattern `\bexec\s*\(` 会误报 RegExp.exec()（正则匹配，完全无害）——
		// arkts-runtime-fix 的 jscrash-parse 因此被误判 CRITICAL、install 被误拦。
		// 收紧到 child_process.exec/execSync（Node 命令执行）。Python audit.py 原版
		// 也是 `\bexec\s*\(`，同样误报——此处修正得比上游更准（安全门控准确性 >
		// 与错误原版逐条一致）。漏报 require/destructured 形式（少见，低风险）。
		Pattern: `(?i)\bchild_process\.exec(?:sync)?\s*\(`},
	{ID: "DC-3", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.75, Msg: "os.system shell 注入面", Fix: "用 subprocess 且 shell=False + 参数列表", ExecOnly: true,
		Pattern: `(?i)\bos\.system\s*\(`},
	{ID: "DC-4", Cat: "dangerous_code", Severity: "MEDIUM", Conf: 0.7, Msg: "subprocess shell=True 注入风险", Fix: "shell=False + 列表参数", ExecOnly: true,
		Pattern: `(?i)\bsubprocess\.(?:call|run|Popen)\s*\([^)]*shell\s*=\s*True`},
	{ID: "DC-5", Cat: "dangerous_code", Severity: "MEDIUM", Conf: 0.6, Msg: "动态 import 可能加载任意模块", Fix: "改用静态 import", ExecOnly: true,
		Pattern: `(?i)\b__import__\s*\(`},
	{ID: "DC-6", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.75, Msg: "脚本中 curl/wget POST 外发动态数据", Fix: "核实目标地址合法性；勿外发用户数据", ExecOnly: true,
		Pattern: `(?i)\b(?:curl|wget)\b[^|&;]{0,120}?(?:-X\s*POST|--data|--data-raw|-d\s)['\"]?(?:\$\{|\$[A-Z_])`},
})

func mustCompile(in []Rule) []Rule {
	out := make([]Rule, len(in))
	for i, r := range in {
		r.re = regexp.MustCompile(r.Pattern)
		out[i] = r
	}
	return out
}

// auditorsForExt 按文件后缀返回适用规则（对齐 audit.py AUDITORS_BY_TYPE）：
//   - .md/.markdown → injection + exfil + leak
//   - 可执行脚本   → injection + exfil + dangerous_code
func auditorsForExt(ext string) []Rule {
	ext = strings.ToLower(ext)
	var out []Rule
	for _, r := range auditRules {
		switch {
		case r.ExecOnly:
			if ExecExts[ext] {
				out = append(out, r)
			}
		case r.Cat == "system_prompt_leakage":
			if markdownExt(ext) {
				out = append(out, r)
			}
		case r.Cat == "prompt_injection" || r.Cat == "data_exfiltration":
			if markdownExt(ext) || ExecExts[ext] {
				out = append(out, r)
			}
		}
	}
	return out
}

// ScanSkill 对 skill 目录跑全部适用审查器，返回去重后的 findings（对齐 audit.py scan_skill）。
func ScanSkill(skillDir string) ([]Finding, error) {
	var raw []Finding
	walkErr := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// 根目录（d==nil）不可访问 → 传播，让调用方区分"skill 不存在"与"空 skill"。
			// 与 AuditSkill 的 SKILL.md 读取错误传播对称：否则下游（skills_audit）吞 err
			// 后 ScanSkill(nil,nil) 会让不存在/无权限的 skill 被报告为"干净"，安全门失守。
			if d == nil {
				return err
			}
			return nil // 子项 best-effort，对齐 Python try/except
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rules := auditorsForExt(strings.ToLower(filepath.Ext(path)))
		if len(rules) == 0 {
			return nil
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil
		}
		rel, _ := filepath.Rel(skillDir, path)
		rel = filepath.ToSlash(rel)
		s := string(content)
		lines := strings.Split(s, "\n")
		for _, r := range rules {
			for _, m := range r.re.FindAllStringIndex(s, -1) {
				lineNo := strings.Count(s[:m[0]], "\n") + 1
				snippet := ""
				if lineNo >= 1 && lineNo <= len(lines) {
					snippet = strings.TrimSpace(lines[lineNo-1])
				}
				if len(snippet) > 120 {
					snippet = snippet[:120]
				}
				raw = append(raw, Finding{
					RuleID: r.ID, Message: r.Msg, Severity: r.Severity, Confidence: r.Conf,
					File: rel, StartLine: lineNo, Category: r.Cat,
					Matched: snippet, Remediation: r.Fix,
				})
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	// 去重：(rule_id, file, line)
	seen := map[string]bool{}
	uniq := make([]Finding, 0, len(raw))
	for _, f := range raw {
		k := f.RuleID + "|" + f.File + "|" + strconv.Itoa(f.StartLine)
		if seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, f)
	}
	return uniq, nil
}

// ScoreFindings 加权评分 → (0-100, severity band, recommendation)。对齐 audit.py score_findings。
func ScoreFindings(findings []Finding) (score int, severity string, recommendation string) {
	raw := 0.0
	for _, f := range findings {
		raw += float64(SeverityWeight[f.Severity]) * f.Confidence
	}
	// Python int(raw) 对正数向零截断；min(100, …) 对齐 audit.py
	score = min(100, int(raw))
	return score, SeverityBand(score), Recommendation(score)
}

// SeverityBand 把 0-100 分映射到严重度（对齐 audit.py sev）。
func SeverityBand(score int) string {
	switch {
	case score >= 50:
		return "CRITICAL"
	case score >= 30:
		return "HIGH"
	case score >= 15:
		return "MEDIUM"
	case score >= 5:
		return "LOW"
	default:
		return "INFO"
	}
}

// Recommendation 把分数映射到安装建议（对齐 audit.py recommend）。
func Recommendation(score int) string {
	switch {
	case score >= 50:
		return "DO_NOT_INSTALL"
	case score >= 20:
		return "CAUTION"
	default:
		return "SAFE"
	}
}
