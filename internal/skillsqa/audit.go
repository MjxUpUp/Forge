package skillsqa

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Finding is a security audit finding (aligned with audit.py Finding dataclass).
//
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

// Rule is a declarative security rule (aligned with audit.py rule tuples). ExecOnly=true only takes effect for executable scripts.
//
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
	// The dangerous_code rule additionally applies to .html/.htm (browser-side XSS vectors: DC-1 eval / DC-7 browser execution vectors); other DC rules only apply to executable scripts.
	//
	HtmlAlso bool // dangerous_code 规则额外在 .html/.htm 生效（浏览器端 XSS 向量：DC-1 eval / DC-7 浏览器执行向量）；其余 DC 只接可执行脚本
	// The dangerous_code rule additionally applies to .md/.markdown (DC-8/DC-9 remote-script-into-shell /
	// DC-10 arbitrary-registry-package). SKILL.md is instructions an agent follows verbatim — a line like
	// `curl <url> | bash` in markdown executes exactly as it would in a script; pre-DC-8 the DC family only
	// routed through ExecExts and malicious SKILL.md payloads scanned clean (blind spot #3).
	//
	// dangerous_code 规则额外在 .md/.markdown 生效（DC-8/DC-9 远端脚本进 shell / DC-10 任意注册表包）。
	// SKILL.md 是 agent 逐字跟随的指令——markdown 里的 `curl <url> | bash` 与脚本里同等可执行；
	// DC-8 之前 DC 族只经 ExecExts 路由，恶意 SKILL.md payload 扫描为干净（盲区 #3）。
	MdAlso bool
	re     *regexp.Regexp
}

// skipDirs — directory segments skipped during scanning (aligned with audit.py node_modules/.git/__pycache__/.venv).
//
// skipDirs — 扫描时跳过的目录段（对齐 audit.py 的 node_modules/.git/__pycache__/.venv）。
var skipDirs = map[string]bool{
	"node_modules": true, ".git": true, "__pycache__": true, ".venv": true,
}

// auditRules is the set of 21 rules (18 aligned with audit.py + forge-local DC-8/DC-9/DC-10 for the
// markdown supply-chain blind spot). Regexes have been converted from Python syntax to Go RE2:
//   - PI-5 zero-width characters → \x{200b} (Go does not support \u)
//   - Each rule has a (?i) prefix to align with Python re.IGNORECASE
//
// auditRules 是 21 条规则（18 条对齐 audit.py + forge 本地 DC-8/DC-9/DC-10 补 markdown 供应链
// 盲区）。正则已从 Python 语法转为 Go RE2：
//   - PI-5 零宽字符 → \x{200b}（Go 不支持 \u）
//   - 每条加 (?i) 前缀对齐 Python re.IGNORECASE
var auditRules = mustCompile([]Rule{
	// PROMPT_INJECTION (5)
	//
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
	// DATA_EXFIL (4)
	//
	// DATA_EXFIL（4）
	{ID: "DE-1", Cat: "data_exfiltration", Severity: "CRITICAL", Conf: 0.9, Msg: "指示把对话上下文外发", Fix: "删除外发指令；如需遥测须显式告知用户并取得同意",
		Pattern: `(?i)(?:send|transmit|upload|post|forward)\s+(?:the\s+)?(?:conversation|context|chat\s+history|session)\s+to\s+(?:https?://|an?\s+external)`},
	{ID: "DE-2", Cat: "data_exfiltration", Severity: "CRITICAL", Conf: 0.9, Msg: "静默/隐瞒式外发", Fix: "删除；任何数据外发必须用户可见",
		Pattern: `(?i)(?:silently|quietly|secretly|covertly|without\s+(?:telling|informing)\s+the\s+user)\s+.{0,40}?(?:send|transmit|upload|log|exfiltrat)`},
	{ID: "DE-3", Cat: "data_exfiltration", Severity: "HIGH", Conf: 0.8, Msg: "指示附带密钥/凭据外发", Fix: "删除；凭据绝不应进入 prompt/输出",
		Pattern: `(?i)(?:include|append|attach)\s+(?:the\s+)?(?:api\s+key|secret|token|\.env|password|credentials?)\b`},
	{ID: "DE-4", Cat: "data_exfiltration", Severity: "HIGH", Conf: 0.85, Msg: "出现 exfiltrate 字样", Fix: "核实意图，正常 skill 不应出现该词",
		Pattern: `(?i)exfiltrat\w*`},
	// SYSTEM_PROMPT_LEAKAGE (2)
	//
	// SYSTEM_PROMPT_LEAKAGE（2）
	{ID: "SL-1", Cat: "system_prompt_leakage", Severity: "HIGH", Conf: 0.85, Msg: "要求泄漏系统提示", Fix: "删除",
		Pattern: `(?i)(?:repeat|reveal|output|show|print|leak)\s+(?:your\s+)?(?:system\s+prompt|initial\s+instructions?|hidden\s+instructions?|core\s+instructions?)`},
	{ID: "SL-2", Cat: "system_prompt_leakage", Severity: "MEDIUM", Conf: 0.7, Msg: "探测系统提示", Fix: "删除",
		Pattern: `(?i)(?:what\s+are\s+your|give\s+me\s+your)\s+(?:system\s+prompt|instructions?|rules?)`},
	// DANGEROUS_CODE (7; DC-1 eval / DC-7 browser execution vectors also cover HTML via HtmlAlso, the rest only cover executable scripts)
	//
	// DANGEROUS_CODE（7；DC-1 eval / DC-7 浏览器执行向量经 HtmlAlso 也接 HTML，余仅可执行脚本）
	{ID: "DC-1", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.8, Msg: "eval() 任意代码执行", Fix: "避免 eval；用安全解析", ExecOnly: true, HtmlAlso: true,
		Pattern: `(?i)\beval\s*\(`},
	{ID: "DC-2", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.7, Msg: "child_process.exec() 任意代码执行", Fix: "避免 child_process.exec；用安全解析", ExecOnly: true,
		// The original Pattern `\bexec\s*\(` would false-positive on RegExp.exec() (regex matching, completely harmless) —
		// arkts-runtime-fix jscrash-parse was therefore misjudged as CRITICAL and install was wrongly blocked.
		// Tightened to child_process.exec/execSync (Node command execution). The original Python audit.py
		// also uses `\bexec\s*\(` and has the same false positive — this fix is more accurate than upstream (security-gate accuracy >
		// line-by-line parity with the erroneous original). Misses require/destructured forms (rare, low-risk).
		//
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
	// DC-7 browser-side dynamic code execution vector — previously skillsqa had no rule coverage for these at all: HTML inline
	// <script>new Function(...)/document.write(...)/setTimeout string/location.href=javascript:
	// are all arbitrary JS execution (XSS / data exfiltration). DC-1~DC-6 go through ExecOnly and only cover .js/.ts; inline .html was under-reported.
	// HtmlAlso lets DC-7 cover both .html and executable scripts.
	// Pattern deliberately omits innerHTML — static scanning cannot distinguish the innerHTML data source (hardcoded string vs
	// user input); covering all yields a terrible signal-to-noise ratio, so it is not covered; that DOM XSS vector relies on code review. Known under-reporting gaps:
	// ① innerHTML/outerHTML injection; ② Function() without new (adding it would false-positive on function declarations);
	// ③ (0,eval) indirect eval (evasion); ④ setTimeout/setInterval template-literal arguments (quote character class excludes backticks);
	// ⑤ document.writeln, location.assign/replace navigation (javascript: scheme) edge sinks (disclosed but not expanding Pattern,
	// to avoid over-complexity). HTML fetch/XHR/sendBeacon exfiltration is another independent gap
	// (DC-6 curl/wget does not cover HTML), deferred to a DC-8 follow-up.
	//
	// DC-7 浏览器端动态代码执行向量——此前 skillsqa 对这些完全无规则覆盖：HTML 内嵌
	// <script>new Function(...)/document.write(...)/setTimeout 字符串/location.href=javascript:
	// 都是任意 JS 执行（XSS / 数据窃取）。DC-1~DC-6 走 ExecOnly 只接 .js/.ts，.html 内嵌漏报。
	// HtmlAlso 让 DC-7 同时覆盖 .html 与可执行脚本。
	// Pattern 刻意不含 innerHTML——静态扫描无法区分 innerHTML 数据源（硬编码字符串 vs
	// 用户输入），全接信噪比极差，故不接；该 DOM XSS 向量依赖代码审查覆盖。已知漏报缺口：
	// ① innerHTML/outerHTML 注入；② Function() 无 new（补会误报 function 声明）；
	// ③ (0,eval) 间接 eval（evasion）；④ setTimeout/setInterval 模板字面量参数（引号字符类不含反引号）；
	// ⑤ document.writeln、location.assign/replace 跳转（javascript: scheme）边缘 sink（披露不扩 Pattern，
	// 避免过度复杂）。HTML fetch/XHR/sendBeacon 外发是另一独立缺口
	// （DC-6 curl/wget 不接 HTML），留待 DC-8 follow-up。
	{ID: "DC-7", Cat: "dangerous_code", Severity: "HIGH", Conf: 0.75, Msg: "浏览器端动态代码执行向量（Function 构造器/document.write/字符串定时器）", Fix: "避免动态代码执行；用静态 DOM（textContent）/事件绑定", ExecOnly: true, HtmlAlso: true,
		Pattern: `(?i)(?:new\s+Function\s*\(|document\.write\s*\(|\b(?:setTimeout|setInterval)\s*\(\s*['"]|location\.href\s*=\s*['"]javascript:)`},
	// DC-8/DC-9/DC-10 — markdown-visible supply-chain execution vectors (blind spot #3, 2026-08-15 global
	// review). DC-1~DC-7 route through ExecOnly and never scanned SKILL.md: a skill teaching
	// `curl <url> | bash` / `bash <(curl <url>)` / `npx attacker-pkg` produced ZERO findings → SAFE →
	// installed to 5 hosts. MdAlso routes these three into markdown alongside executable scripts.
	// All three require a real https?:// URL / package token so canonical TEACHING content stays clean:
	// on-demand-guards legitimately writes `curl ... | sh` (no URL — pattern), dev-lookup legitimately
	// pipes real-URL curls into DATA processors (jq/grep/head — the tail requires a shell, jq ≠ sh),
	// and canonical npx usages (tsc/playwright/shadcn) surface as non-blocking MEDIUM advisories
	// (4×MEDIUM×0.6=19 < CAUTION) — supply-chain visibility without breaking canonical distribution.
	// Known under-reporting (deliberate, keeps RE2-compatible and canonical clean):
	// ① multi-stage pipes (`curl url | base64 | sh`) — the tail anchors to the first pipe;
	// ② `|sh` glued without spaces is caught (\s*) but newline-continuation pipes and bash's
	//    pipe-both `|&` (`curl url |& bash`) are not;
	// ③ env-var-only URLs (`curl $URL | sh`) — the URL requirement is the FP boundary, its cost is this miss;
	// ④ PowerShell vectors (`iwr ... | iex`) — out of scope, never claimed;
	// ⑤ `xargs sh` / `; `-chained forms. Case is NOT a miss: (?i) catches `| BASH`, `SUDO bash`.
	// Review round 1 (2026-08-16) hardened three initially-missed forms, now covered:
	// `&`-bearing query-string URLs (`?src=gh&v=2` — URL class allows `&`), flags-after-URL
	// (`curl URL -s | bash` — ≤40 non-pipe chars allowed between URL and pipe), and command
	// substitution (`sh -c "$(curl …)"` / `eval "$(curl …)"` — folded into DC-9).
	//
	// DC-8/DC-9/DC-10 —— markdown 可见的供应链执行向量（盲区 #3，2026-08-15 全局审查）。
	// DC-1~DC-7 走 ExecOnly 从不扫 SKILL.md：教唆 `curl <url> | bash` / `bash <(curl <url>)` /
	// `npx attacker-pkg` 的 skill 零 findings → SAFE → 装进 5 host。MdAlso 让这三条同时路由进
	// markdown 与可执行脚本。三条都要求真实 https?:// URL / 包名 token，canonical 教学内容保持
	// 干净：on-demand-guards 合法写 `curl ... | sh`（无 URL——不命中），dev-lookup 合法把真实 URL
	// 的 curl 管道进数据处理器（jq/grep/head——尾部要求 shell，jq ≠ sh），canonical 的 npx 用法
	// （tsc/playwright/shadcn）浮出为非阻断 MEDIUM advisory（4×MEDIUM×0.6=19 < CAUTION）——
	// 供应链可见性而不破坏 canonical 分发。已知漏报（刻意，保 RE2 兼容与 canonical 干净）：
	// ① 多级管道（`curl url | base64 | sh`）——尾部锚定第一个管道；
	// ② 无空格 `|sh` 能抓（\s*）但换行续接管道与 bash 的 pipe-both `|&`（`curl url |& bash`）不抓；
	// ③ 仅环境变量 URL（`curl $URL | sh`）——URL 前置是 FP 边界，其代价即此漏报；
	// ④ PowerShell 向量（`iwr ... | iex`）——范围外，从未声明覆盖；
	// ⑤ `xargs sh` / `; ` 链式形态。大小写不是漏报：(?i) 抓 `| BASH`、`SUDO bash`。
	// 审查第 1 轮（2026-08-16）加固了三个初始漏掉的形态：带 `&` 的查询串 URL（`?src=gh&v=2`
	// ——URL 类允许 `&`）、URL 后置 flag（`curl URL -s | bash`——URL 与管道间允许 ≤40 个非管道
	// 字符）、命令替换（`sh -c "$(curl …)"` / `eval "$(curl …)"`——并入 DC-9）。
	{ID: "DC-8", Cat: "dangerous_code", Severity: "CRITICAL", Conf: 0.9, Msg: "curl/wget 远端脚本直接管道进 shell（远程代码执行）", Fix: "删除该命令；先下载并校验（checksum/签名）再执行，或用包管理器", ExecOnly: true, MdAlso: true,
		Pattern: "(?i)\\b(?:curl|wget)\\b[^|\\n]{0,160}?https?://[^\\s|;\"']+[^|\\n]{0,40}?\\|\\s*(?:sudo\\s+)?\\b(?:ba|z|a|k|da)?sh\\b"},
	{ID: "DC-9", Cat: "dangerous_code", Severity: "CRITICAL", Conf: 0.85, Msg: "sh <(curl) 进程替换 / sh -c \"$(curl)\" 命令替换执行远端脚本（curl|sh 的无管道变体）", Fix: "删除该命令；先下载并校验再执行", ExecOnly: true, MdAlso: true,
		Pattern: "(?i)\\b(?:(?:ba|z|a|k|da)?sh\\b\\s*(?:<\\(|-c\\s+['\\\"]?\\s*\\$\\()\\s*|eval\\b\\s+['\\\"]?\\s*\\$\\()\\s*(?:curl|wget)\\b[^)\\n]{0,160}?https?://"},
	{ID: "DC-10", Cat: "dangerous_code", Severity: "MEDIUM", Conf: 0.6, Msg: "npx/uvx/dlx/bunx 执行任意注册表包（供应链代码执行面）", Fix: "确认包名可信并 pin 版本；优先用本地依赖运行工具", ExecOnly: true, MdAlso: true,
		Pattern: "(?i)\\b(?:npx|uvx|dlx|bunx)[ \\t]+(?:-{1,2}[a-z0-9-]+[ \\t]+)*@?[a-z0-9][a-z0-9@/._-]*"},
})

func mustCompile(in []Rule) []Rule {
	out := make([]Rule, len(in))
	for i, r := range in {
		// HtmlAlso is a modifier of ExecOnly: when ExecOnly=false the rule falls into the auditorsForExt switch
		// default (dangerous_code matches no explicit case), silently failing — a silent false-negative in the security gate.
		// Compile-time fail-fast prevents future mistakes of HtmlAlso:true, ExecOnly:false.
		//
		// HtmlAlso 是 ExecOnly 的修饰：ExecOnly=false 时规则走 auditorsForExt 的 switch
		// default（dangerous_code 不匹配任何显式 case），沉默失效——安全门静默漏报。
		// 编译期 fail-fast，防未来误写 HtmlAlso:true, ExecOnly:false。
		if r.HtmlAlso && !r.ExecOnly {
			panic("rule " + r.ID + ": HtmlAlso requires ExecOnly=true (otherwise rule never fires)")
		}
		// MdAlso carries the same silent-failure shape (see HtmlAlso above) — fail fast on the
		// misconfiguration instead of letting the rule go inert.
		//
		// MdAlso 同 HtmlAlso 的沉默失效形态（见上）——对误配置 fail-fast，而非让规则失效。
		if r.MdAlso && !r.ExecOnly {
			panic("rule " + r.ID + ": MdAlso requires ExecOnly=true (otherwise rule never fires)")
		}
		r.re = regexp.MustCompile(r.Pattern)
		out[i] = r
	}
	return out
}

// auditorsForExt returns the applicable rules by file extension (aligned with audit.py AUDITORS_BY_TYPE,
// plus forge-local MdAlso routing):
//   - .md/.markdown → injection + exfil + leak + MdAlso DC rules (DC-8/DC-9/DC-10 — SKILL.md lines
//     are verbatim agent instructions, supply-chain execution payloads there execute exactly as in a script)
//   - executable scripts → injection + exfil + dangerous_code
//   - .html/.htm → injection + exfil (HTML is a prompt injection carrier: PI-4 is designed for hidden
//     instruction comments) + dangerous_code rules with HtmlAlso=true (DC-1 eval / DC-7 browser execution
//     vectors); other DC rules (child_process/os.system/subprocess and other backend APIs) are not covered — HTML is not directly
//     executable, and backend API keywords are prone to false positives in descriptive text.
//
// auditorsForExt 按文件后缀返回适用规则（对齐 audit.py AUDITORS_BY_TYPE + forge 本地 MdAlso 路由）：
//   - .md/.markdown → injection + exfil + leak + MdAlso 的 DC 规则（DC-8/DC-9/DC-10——SKILL.md
//     的行是 agent 逐字跟随的指令，供应链执行 payload 与脚本里同等生效）
//   - 可执行脚本   → injection + exfil + dangerous_code
//   - .html/.htm   → injection + exfil（HTML 是 prompt injection 载体：PI-4 专为隐藏
//     指令注释设计）+ dangerous_code 中 HtmlAlso=true 的（DC-1 eval / DC-7 浏览器执行
//     向量）；其余 DC（child_process/os.system/subprocess 等后端 API）不接——HTML 非直接
//     可执行，后端 API 关键词在说明文本易误报。
func auditorsForExt(ext string) []Rule {
	ext = strings.ToLower(ext)
	var out []Rule
	for _, r := range auditRules {
		switch {
		case r.ExecOnly:
			// dangerous_code by default only applies to executable scripts; rules with HtmlAlso=true (DC-1 eval / DC-7
			// browser execution vectors) additionally apply to .html/.htm — inline <script> in HTML is a real execution carrier;
			// rules with MdAlso=true (DC-8/DC-9/DC-10) additionally apply to markdown — SKILL.md lines are
			// instructions an agent executes verbatim (blind spot #3).
			//
			// dangerous_code 默认仅可执行脚本；HtmlAlso=true 的规则（DC-1 eval / DC-7
			// 浏览器执行向量）额外在 .html/.htm 生效——HTML 内嵌 <script> 是真实执行载体；
			// MdAlso=true 的规则（DC-8/DC-9/DC-10）额外在 markdown 生效——SKILL.md 的行是
			// agent 逐字执行的指令（盲区 #3）。
			if ExecExts[ext] || (r.HtmlAlso && HtmlExts[ext]) || (r.MdAlso && markdownExt(ext)) {
				out = append(out, r)
			}
		case r.Cat == "system_prompt_leakage":
			if markdownExt(ext) {
				out = append(out, r)
			}
		case r.Cat == "prompt_injection" || r.Cat == "data_exfiltration":
			if markdownExt(ext) || ExecExts[ext] || HtmlExts[ext] {
				out = append(out, r)
			}
		}
	}
	return out
}

// decisionsMdFile is the persistent decision-history filename exempted from MdAlso
// dangerous_code rules (DC-8/DC-9/DC-10) during ScanSkill.
//
// decisions.md is an append-only audit record whose FUNCTION is to quote past
// dangerous commands — a Diagnosis line references the exact `npx <pkg>` /
// `curl | sh` form that was removed (that is the record's evidentiary value).
// Scanning it with MdAlso rules creates a self-referential trap: any honest
// decision documenting a supply-chain fix re-introduces the pattern it fixed,
// and the "audit clean" evidence measured before appending the decision is
// silently stale after (2026-08-24 fix/dc10 incident: evidence said 9→0, the
// committed tree held 9→3 because three Diagnosis lines quoted the removed
// forms; caught only by independent review). Agents then resort to distorted
// spelling (npx-pkg hyphenation) to dodge the scanner — writing contorts to
// satisfy the tool, the FP-boundary smell.
//
// Exemption is NARROW by design: only the dangerous_code MdAlso trio is dropped
// for this one filename. Injection/exfil/leak rules still scan decisions.md —
// those patterns are never part of a legitimate decision record, so a hit there
// stays a real finding. SKILL.md and references/*.md remain fully scanned (they
// ARE verbatim agent instructions).
//
// decisionsMdFile 是 ScanSkill 中豁免 MdAlso 危险代码规则（DC-8/DC-9/DC-10）的
// 持久决策历史文件名。
//
// decisions.md 是 append-only 审计记录，其功能就是引用过去的危险命令——Diagnosis
// 行会逐字引用被移除的 `npx <包>` / `curl | sh` 形态（这正是记录的证据价值）。对它
// 跑 MdAlso 规则制造了自指陷阱：任何如实记录供应链修复的决策都会重新引入它修掉的
// 模式，且追加决策前测得的"audit 干净"证据在追加后静默过期（2026-08-24
// fix/dc10 事故：证据写 9→0，提交树实际 9→3——三条 Diagnosis 引用了被移除形态；
// 仅靠独立审查拦下）。agent 于是被迫用扭曲拼写（npx-包名 连字符化）躲扫描器——
// 写作迁就工具，正是 FP 边界错了的味道。
//
// 豁免刻意收窄：仅对该文件名丢弃 dangerous_code 的 MdAlso 三条。injection/
// exfil/leak 规则继续扫 decisions.md——那些模式绝不属于合法决策记录，命中仍是真
// finding。SKILL.md 与 references/*.md 维持全量扫描（它们才是逐字 agent 指令）。
const decisionsMdFile = "decisions.md"

// dropMdAlsoRules filters MdAlso-routed rules (DC-8/DC-9/DC-10) out of a rule set.
// Used by ScanSkill for decisions.md (see decisionsMdFile).
//
// dropMdAlsoRules 从规则集中滤掉 MdAlso 路由的规则（DC-8/DC-9/DC-10）。
// ScanSkill 对 decisions.md 使用（见 decisionsMdFile）。
func dropMdAlsoRules(rules []Rule) []Rule {
	out := rules[:0:0]
	for _, r := range rules {
		if r.MdAlso {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ScanSkill runs all applicable auditors on a skill directory and returns deduplicated findings (aligned with audit.py scan_skill).
//
// ScanSkill 对 skill 目录跑全部适用审查器，返回去重后的 findings（对齐 audit.py scan_skill）。
func ScanSkill(skillDir string) ([]Finding, error) {
	var raw []Finding
	walkErr := filepath.WalkDir(skillDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Root directory (d==nil) inaccessible → propagate, so callers can distinguish `skill does not exist` from `empty skill`.
			// Symmetric with AuditSkill SKILL.md read error propagation: otherwise downstream (skills_audit) swallows err
			// and ScanSkill(nil,nil) would report a non-existent/permission-denied skill as `clean`, defeating the security gate.
			//
			// 根目录（d==nil）不可访问 → 传播，让调用方区分"skill 不存在"与"空 skill"。
			// 与 AuditSkill 的 SKILL.md 读取错误传播对称：否则下游（skills_audit）吞 err
			// 后 ScanSkill(nil,nil) 会让不存在/无权限的 skill 被报告为"干净"，安全门失守。
			if d == nil {
				return err
			}
			// best-effort for child entries, aligned with Python try/except
			//
			return nil // 子项 best-effort，对齐 Python try/except
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rules := auditorsForExt(strings.ToLower(filepath.Ext(path)))
		// decisions.md exemption: see decisionsMdFile — the decision history quotes past
		// dangerous commands by function; MdAlso rules on it are a self-referential FP
		// generator (evidence goes stale the moment the decision documenting the fix is
		// appended). Narrow: injection/exfil/leak still scan it.
		//
		// decisions.md 豁免：见 decisionsMdFile——决策历史按功能引用过去的危险命令，
		// MdAlso 规则在它上面是自指 FP 生成器（记录修复的决策一追加、证据即过期）。
		// 收窄：injection/exfil/leak 仍扫它。
		if filepath.Base(path) == decisionsMdFile {
			rules = dropMdAlsoRules(rules)
		}
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
	// Deduplicate by: (rule_id, file, line)
	//
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

// ScoreFindings computes a weighted score → (0-100, severity band, recommendation). Aligned with audit.py score_findings.
//
// ScoreFindings 加权评分 → (0-100, severity band, recommendation)。对齐 audit.py score_findings。
func ScoreFindings(findings []Finding) (score int, severity string, recommendation string) {
	raw := 0.0
	for _, f := range findings {
		raw += float64(SeverityWeight[f.Severity]) * f.Confidence
	}
	// Python int(raw) truncates positive numbers toward zero; min(100, …) aligns with audit.py
	//
	// Python int(raw) 对正数向零截断；min(100, …) 对齐 audit.py
	score = min(100, int(raw))
	return score, SeverityBand(score), Recommendation(score)
}

// SeverityBand maps a 0-100 score to a severity (aligned with audit.py sev).
//
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

// HasCritical reports whether findings contain at least one CRITICAL entry. Single source of truth
// for the install gate and `forge skills audit --gate` (blind spot #4): both used to decide on the
// aggregate score/band alone — a single CRITICAL finding contributes ≤25×0.95=23.75 points, which can
// never reach DO_NOT_INSTALL(≥50) and usually not even the HIGH band(≥30), so one confirmed CRITICAL
// sailed through as CAUTION/MEDIUM. Any CRITICAL blocks regardless of the aggregate.
//
// HasCritical 报告 findings 是否含至少一条 CRITICAL。install 门禁与 `forge skills audit --gate`
// 的单一真相源（盲区 #4）：两者此前只看聚合分/带——单条 CRITICAL 贡献 ≤25×0.95=23.75 分，
// 永远够不到 DO_NOT_INSTALL(≥50)、通常也够不到 HIGH 带(≥30)，一条确认的 CRITICAL 就以
// CAUTION/MEDIUM 溜过。任何 CRITICAL 无视聚合直接阻断。
func HasCritical(findings []Finding) bool {
	for _, f := range findings {
		if f.Severity == "CRITICAL" {
			return true
		}
	}
	return false
}

// Recommendation maps a score to an install recommendation (aligned with audit.py recommend).
//
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
