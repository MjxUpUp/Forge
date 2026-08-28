// Package skillsqa implements SkillsHub quality validation: spec contracts
// (registry.py R1-R11) and security audit (18 rules aligned with audit.py +
// weighted scoring, plus forge-local DC-8/DC-9/DC-10 markdown supply-chain
// extensions — 21 audit rules total).
// 1:1 alignment with Python semantics ensures per-rule judgments match registry.py
// --json / audit.py (golden comparison baseline).
// R12-R18 are forge-local extensions on top of the Python-aligned R1-R11
// (R12 triggers declarations; R13-R17 from the 2026-08 skills value audit,
// improvement item 11 — see docs/skills-value-audit-2026-08-02.md; R18 forge
// reference contract from the 2026-08 dependency-inversion refactor,
// CONVENTIONS §13).
//
// Package skillsqa 实现 SkillsHub 的质量校验：规范契约（registry.py 的 R1-R11）
// 与安全审查（18 条规则对齐 audit.py + 加权评分，另有 forge 本地 DC-8/DC-9/DC-10
// markdown 供应链扩展——audit 共 21 条）。1:1 对齐 Python 语义，确保与
// registry.py --json / audit.py 的判定逐条一致（黄金对比基准）。
// R12-R18 是 forge 在 Python 对齐的 R1-R11 之上的本地扩展
// （R12 triggers 声明；R13-R17 来自 2026-08 skills 价值审计清单项 11，
// 见 docs/skills-value-audit-2026-08-02.md；R18 forge 引用契约来自
// 2026-08 依赖倒置重构，见 CONVENTIONS §13）。
package skillsqa

import (
	"maps"
	"regexp"
	"slices"
	"strings"
)

// ValidPatterns — legal atomic values for metadata.pattern (registry.py VALID_PATTERNS).
// Combinations are supported (e.g. `pipeline + gate`): after split('+'), each segment
// must be in this set.
//
// ValidPatterns — metadata.pattern 合法原子值（registry.py VALID_PATTERNS）。
// 支持组合（如 "pipeline + gate"）：split('+') 后每段都必须在此集合。
var ValidPatterns = map[string]bool{
	"tool-wrapper": true, "generator": true, "reviewer": true,
	"inversion": true, "pipeline": true, "gate": true,
	"routing": true, "fallback": true,
	// reference — 经验/踩坑记录型（无流程无门控的知识参考），2026-08 新增：
	// integration-test-architecture 类 skill 原被误标 tool-wrapper。
	// reference — experience/pitfall-reference skills (no pipeline, no gate).
	"reference": true,
}

// HighSignalKW — any one present in body is treated as high-signal content
// (registry.py HIGH_SIGNAL_KW).
// Note: Python uses `kw in body_low` substring match, so `when.*try.*because` is a
// literal string (rarely matched); Go keeps strings.Contains for consistency and does
// not convert it to a regex.
//
// HighSignalKW — body 含任一视为有高信号内容（registry.py HIGH_SIGNAL_KW）。
// 注意：Python 用 `kw in body_low` 子串匹配，故 "when.*try.*because" 是字面串
// （几乎不命中），Go 保持 strings.Contains 一致，不改成正则。
var HighSignalKW = []string{
	"decision tree", "决策树", "post-generation", "自查", "review",
	"gotcha", "易错", "checklist", "检查清单", "when.*try.*because",
	"red flag", "rationaliz", "红旗", "借口",
}

// CSOWorkflowMarkers — workflow summary words that description should not contain
// (CSO rule: description only states what + when, never summarizes body workflow,
// otherwise the model acts on description and skips the SKILL.md body).
// Heuristic high-confidence Chinese phrases; a hit goes advisory (regression guard,
// does not block Pass).
// Additionally: the model weights head/tail heavily and the middle (body) is easily
// overlooked; stuffing workflow into description further drowns out the body.
//
// CSOWorkflowMarkers — description 不应含的工作流总结词（CSO 规则：description 只说
// what + when，不总结 body 工作流，否则模型照 description 行动而跳过 SKILL.md body）。
// 启发式高置信中文词组，命中走 advisory（防回归，不阻断 Pass）。
// 另：模型偏重开头/结尾、中段（body）易被忽略，description 塞工作流会进一步淹没 body。
var CSOWorkflowMarkers = []string{
	"完整工作流", "完整流程", "全流程", "完整协议", "完整编排", "全链路", "全工序",
}

// AllowedFm — top-level frontmatter field whitelist (registry.py ALLOWED_FM,
// R3 prevents field typos).
//
// AllowedFm — frontmatter 顶层字段白名单（registry.py ALLOWED_FM，R3 防字段 typo）。
var AllowedFm = map[string]bool{
	"name": true, "description": true, "license": true, "allowed-tools": true,
	"metadata": true, "compatibility": true, "version": true, "requires": true,
}

// ValidTriggerEvents — metadata.triggers[].event 合法值（通用 skill-trigger 框架支持的事件）。
// PostCompact 不在内——该事件不支持 additionalContext 注入（plan 边界）。
// 与 internal/hooks ForgeHookSpec 挂载的 5 事件一致。
//
// ValidTriggerEvents — metadata.triggers[].event 合法值（通用 skill-trigger 框架支持的事件）。
// PostCompact 不在内——该事件不支持 additionalContext 注入（plan 边界）。
var ValidTriggerEvents = map[string]bool{
	"UserPromptSubmit": true, "PreToolUse": true, "PostToolUse": true,
	"Stop": true, "SessionStart": true,
}

// ValidConditions — metadata.triggers[].when 合法值（skilltrigger.Conditions 词汇表）。
// 与 internal/skilltrigger.Conditions 的 key 集合必须一致——drift 守卫
// TestValidConditions_MatchEngine 断言两者同步（新增 condition 须同时改两处）。
//
// ValidConditions — metadata.triggers[].when 合法值（skilltrigger.Conditions 词汇表）。
// 与 internal/skilltrigger.Conditions 的 key 集合必须一致——drift 守卫
// TestValidConditions_MatchEngine 断言两者同步（新增 condition 须同时改两处）。
var ValidConditions = map[string]bool{
	"source_changed_uncommitted": true,
	"test_command_failed":        true,
	"coding_intent":              true,
	"task_active_no_review":      true,
	"skill_file_touched":         true,
}

// validTriggerEventsSorted returns sorted trigger-event names (R12 issue 文案用).
//
// validTriggerEventsSorted 返回排序后的 trigger-event 名（R12 issue 文案用）。
func validTriggerEventsSorted() []string { return slices.Sorted(maps.Keys(ValidTriggerEvents)) }

// validConditionsSorted returns sorted condition names (R12 issue 文案用).
//
// validConditionsSorted 返回排序后的 condition 名（R12 issue 文案用）。
func validConditionsSorted() []string { return slices.Sorted(maps.Keys(ValidConditions)) }

// ExecExts — executable script suffixes (audit.py EXEC_EXTS); the dangerous_code
// rule only applies to these.
//
// ExecExts — 可执行脚本后缀（audit.py EXEC_EXTS）；dangerous_code 规则仅对这些生效。
var ExecExts = map[string]bool{
	".py": true, ".sh": true, ".ps1": true, ".js": true, ".ts": true,
	".mjs": true, ".cjs": true, ".bat": true, ".cmd": true,
}

// HtmlExts — HTML suffixes; prompt_injection / data_exfiltration rules apply to
// these, and dangerous_code entries with HtmlAlso=true (DC-1 eval / DC-7 browser
// execution vectors) also apply.
// HTML is a high-risk carrier for injection/code execution: PI-4 hidden directive
// comments, PI-5 zero-width characters, DE exfiltration directives, and inline
// <script>eval(...)/new Function(...)/document.write(...) in HTML are all real
// attack surfaces.
// Other DCs (child_process/os.system and similar backend APIs) do not cover HTML —
// HTML is not a directly executable suffix, and backend API keywords in descriptive
// text are prone to false positives.
// 2026-07: prototype-confirmation introduced the first .html canonical asset, exposing
// a blind spot — PI-4 previously never scanned real .html; DC-1 eval previously used
// ExecOnly and also did not scan .html (HTML inline XSS was under-reported).
//
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

// SeverityWeight — risk-scoring weights (audit.py SEVERITY_WEIGHT).
//
// SeverityWeight — 风险评分加权（audit.py SEVERITY_WEIGHT）。
var SeverityWeight = map[string]int{
	"INFO": 0, "LOW": 3, "MEDIUM": 8, "HIGH": 15, "CRITICAL": 25,
}

// kebabRe — R1 name legal format (registry.py r'[a-z][a-z0-9-]*' fullmatch).
//
// kebabRe — R1 name 合法格式（registry.py r'[a-z][a-z0-9-]*' fullmatch）。
var kebabRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// imperativeRe — R15 ALL-CAPS 命令式词（整词匹配，仅全大写形式；小写 always/must
// 是普通叙述不计入）。
//
// imperativeRe — R15 ALL-CAPS imperative words (whole-word, uppercase only;
// lowercase always/must is ordinary prose and does not count).
var imperativeRe = regexp.MustCompile(`\b(ALWAYS|NEVER|MUST)\b`)

// forgeCmdRe — R18 forge CLI 命令引用（`forge task` / `forge review pass` 形态）。
// 前置字符排除 [\w./-]：xforge（词内）、forge/docs、.forge/（路径）不算命令引用；
// 后随小写子命令名——「forge 项目」「forge 环境」类叙述措辞不命中。
//
// forgeCmdRe — R18 forge CLI command references (`forge task` / `forge review pass`).
// The leading class excludes [\w./-]: xforge (inside a word), forge/docs, .forge/
// (paths) are not command references; a lowercase subcommand must follow, so
// prose like "forge 项目" / "forge 环境" never matches.
var forgeCmdRe = regexp.MustCompile(`(?:^|[^\w./-])forge\s+[a-z][a-z-]*`)

// RuleDescriptions — rule ID → rule text definition (exported single source of
// truth; docs generation greps this table instead of copying rule text, and CLI
// output stays aligned with it). R1-R11 align with SkillsHub registry.py;
// R12-R17 are forge-local extensions (see package doc).
//
// RuleDescriptions — 规则编号 → 规则文本定义（可导出的单一真相源；文档生成 grep
// 本表而非复制规则文本，CLI 输出与之对齐）。R1-R11 对齐 SkillsHub registry.py；
// R12-R17 为 forge 本地扩展（见 package doc）。
var RuleDescriptions = map[string]string{
	"R1":  "name 须 kebab-case（硬）",
	"R2":  "name 与目录名一致（硬）",
	"R3":  "frontmatter 字段白名单，防 typo（硬）",
	"R4":  "description 长度 80-1024 字符（硬）；>500 偏长走 advisory",
	"R5":  "description 须含 Use when（硬）",
	"R6":  "description 须含 SKIP（硬）",
	"R7":  "metadata.pattern 必填，单值或 + 组合每段须合法（硬）",
	"R8":  "SKILL.md 总行数 ≤500（硬，对齐 Python 计行口径）",
	"R9":  "正文须含高信号内容：决策树/自查/Gotchas 等（硬）",
	"R10": "description 不应总结 body 工作流，只说 what+when（CSO，advisory）",
	"R11": "references/ ≤1 level 无子目录（硬）；>100 行 markdown ref 需 ToC（advisory）",
	"R12": "metadata.triggers 声明须合法 JSON 且 event/when ∈ 词汇表（advisory）",
	"R13": "SKILL.md 正文（不含 frontmatter）≤500 行（硬）",
	"R14": "frontmatter 必填 name 与 description（硬；description ≤1024 字符上限由 R4 覆盖）",
	"R15": "正文 ALL-CAPS 命令式词（ALWAYS/NEVER/MUST）合计 >5 次时提醒改「指令+原因」写法（advisory）",
	"R16": "references/ 下 >300 行文件需 ToC（advisory；markdown 文件由 R11 以 >100 行更低门槛先行覆盖，不重复报）",
	"R17": "evals/evals.json 存在时须符 schema：对象含 trigger_cases 数组，每项 {query: string, should_trigger: boolean}（advisory）",
	"R18": "正文 forge 命令引用只允许出现在「> Forge 项目」条件引用块内，其余位置命中即提醒依赖倒置契约（advisory；细节下沉 references/forge-integration.md，CONVENTIONS §13）",
}

// allowedFmSorted returns the sorted allowed-field list (used in R3 issue text;
// aligns with Python sorted(ALLOWED_FM)).
//
// allowedFmSorted 返回排序后的允许字段列表（R3 issue 文案用，对齐 Python sorted(ALLOWED_FM)）。
func allowedFmSorted() []string {
	return slices.Sorted(maps.Keys(AllowedFm))
}

// markdownExt reports whether the suffix is markdown (audit.py AUDITORS_BY_TYPE
// .md/.markdown).
// strings.ToLower performs full Unicode case folding — the old hand-written
// ASCII-only lower() would miss non-ASCII uppercase letters; routing through the
// standard library keeps this judgment consistent with the descLow/bodyLow handling.
//
// markdownExt 判断是否 markdown 后缀（audit.py AUDITORS_BY_TYPE 的 .md/.markdown）。
// 用 strings.ToLower 做完整 Unicode 大小写折叠——旧的手写 ASCII-only lower() 会漏掉
// 非 ASCII 大写字母，统一走标准库与 descLow/bodyLow 的判定口径一致。
func markdownExt(ext string) bool {
	ext = strings.ToLower(ext)
	return ext == ".md" || ext == ".markdown"
}
