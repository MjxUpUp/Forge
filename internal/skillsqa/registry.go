package skillsqa

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// Quality holds R4-R9 check results (aligned with registry.py quality dict).
//
// Quality 是 R4-R9 各项检查结果（对齐 registry.py quality dict）。
type Quality struct {
	DescLen       int  `json:"desc_len"`
	HasUseWhen    bool `json:"has_use_when"`
	HasSkip       bool `json:"has_skip"`
	ValidPattern  bool `json:"valid_pattern"`
	Over500Lines  bool `json:"over_500_lines"`
	HasHighSignal bool `json:"has_high_signal"`
}

// SkillReport is the spec audit result for a single skill (aligned with the
// registry.py audit_skill return value; excludes dispatch target status —
// drift detection belongs to skillsdist).
//
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

// AuditSkill runs R1-R18 spec checks on a single skill directory; R1-R11 are
// 1:1 aligned with registry.py audit_skill, R12-R18 are forge-local extensions
// (rule text definitions: RuleDescriptions).
//
// AuditSkill 对单个 skill 目录跑 R1-R18 规范校验。R1-R11 逐条对齐
// registry.py audit_skill，R12-R18 为 forge 本地扩展（规则文本定义见 RuleDescriptions）。
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
	// Line count: aligned with Python registry.py via newline-count + 1 (assumes
	// the file ends with a newline; files without a trailing newline count 1
	// extra line — shared trait with Python, kept as-is for golden parity).
	//
	// 行数：与 Python registry.py 一致用 Count("\n")+1（假设文件以 \n 结尾；
	// 无尾换行的文件会多算 1 行——这是与 Python 共享的特性，黄金对比保持故不改）。
	lines := strings.Count(text, "\n") + 1
	descLow := strings.ToLower(desc)
	bodyLow := strings.ToLower(fm.Body)

	var issues []string
	var advisories []string

	// R1: name must be kebab-case
	//
	// R1 name 须 kebab-case
	if !kebabRe.MatchString(name) {
		issues = append(issues, "name 不符合 kebab-case")
	}
	// R2: name equals directory name
	//
	// R2 name = 目录名
	if name != dirName {
		issues = append(issues, fmt.Sprintf("name(%s) 与目录名(%s)不一致", name, dirName))
	}
	// R3: frontmatter field whitelist (guards against typos)
	//
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
	// R4: description length (Python len() counts characters → Go uses RuneCount
	// to align, otherwise 3 bytes per CJK char would skew R4)
	//
	// R4 description 长度（Python len() 是字符数 → Go 用 RuneCount 对齐，否则中文 3 字节/字符致 R4 失准）
	descLen := utf8.RuneCountInString(desc)
	if descLen < 80 {
		issues = append(issues, fmt.Sprintf(`description 过短(%d字符 <80)`, descLen))
	}
	// R4 upper bound: Anthropic skill spec caps description at ≤1024 chars
	// (hard issue); >500 is overlong (advisory)
	//
	// R4 上限：Anthropic skill 规范 description ≤1024 字符（硬 issue）；>500 偏长（advisory）
	if descLen > 1024 {
		issues = append(issues, fmt.Sprintf(`description 过长(%d字符 >1024，超 Anthropic skill 规范上限)`, descLen))
	} else if descLen > 500 {
		advisories = append(advisories, fmt.Sprintf(`description 偏长(%d字符 >500，建议精简到 what+when，不总结工作流)`, descLen))
	}
	// R5: must contain Use when
	//
	// R5 须含 Use when
	hasUseWhen := strings.Contains(descLow, "use when")
	if !hasUseWhen {
		issues = append(issues, "description 缺 Use when")
	}
	// R6: must contain SKIP
	//
	// R6 须含 SKIP
	hasSkip := strings.Contains(descLow, "skip")
	if !hasSkip {
		issues = append(issues, "description 缺 SKIP")
	}
	// R7: metadata.pattern (single value or + combination; each segment must be valid)
	//
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
	// R8: SKILL.md line count
	//
	// R8 SKILL.md 行数
	over := lines > 500
	if over {
		issues = append(issues, fmt.Sprintf("SKILL.md 过长(%d行 >500，拆 references)", lines))
	}
	// R9: high-signal content
	//
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
	// R10 CSO: description must not summarize body workflow (advisory, regression guard)
	//
	// R10 CSO：description 不应总结 body 工作流（advisory，防回归）
	for _, marker := range CSOWorkflowMarkers {
		if strings.Contains(desc, marker) {
			advisories = append(advisories, fmt.Sprintf(`description 含工作流总结词(%s)；CSO 规则：description 只说 what+when，不总结工作流（否则模型照 description 跳过 body）`, marker))
			break
		}
	}
	// R11 references structure: ≤1 level (no subdirs, hard) + refs over 100
	// lines need ToC (advisory)
	//
	// R11 references 结构：≤1 level（无子目录，硬）+ >100 行 ref 需 ToC（advisory）
	checkReferences(skillDir, &issues, &advisories)
	// R12 triggers 声明校验（advisory）——通用 skill-trigger 框架的实验字段，skill 不
	// 写也合法；写了则校验 JSON 合法性 / event∈集 / keywords 或 when 至少一 / when∈词汇
	// / match 仅对 tool 事件有效。内联 JSON 解析，避免 skillsqa→skilltrigger 循环依赖。
	//
	// R12 triggers 声明校验（advisory）——通用 skill-trigger 框架的实验字段，skill 不
	// 写也合法；写了则校验 JSON 合法性 / event∈集 / keywords 或 when 至少一 / when∈词汇
	// / match 仅对 tool 事件有效。内联 JSON 解析，避免 skillsqa→skilltrigger 循环依赖。
	checkTriggers(fm.Metadata["triggers"], &advisories)
	// R13 正文行数（硬，不含 frontmatter）——与 R8 的关系：R8 计全文行数（对齐
	// Python），R13 只计正文。body >500 ⇒ 全文 >500，故 R13 触发时 R8 必然也触发；
	// R13 的价值是把「正文」口径显式化（frontmatter 膨胀不会再吃掉正文预算的语义）。
	//
	// R13 body line count (hard, frontmatter excluded) — relationship to R8: R8 counts
	// the whole file (Python parity), R13 counts the body only. body >500 implies
	// total >500, so whenever R13 fires R8 fires too; R13 makes the body-only
	// semantics explicit.
	checkBodyLines(fm.Body, &issues)
	// R14 frontmatter 必填字段（硬）：name/description 缺一不可。description 的
	// ≤1024 字符上限由 R4 覆盖，此处不重复报。注意 name 为空时上方已回退 dirName
	// （R1/R2 不误报），R14 用 fm.Name/fm.Description 原始值判定缺失。
	//
	// R14 required frontmatter fields (hard): name and description are mandatory.
	// The ≤1024-char description cap is covered by R4 and not duplicated here. Note
	// the empty name falls back to dirName above (so R1/R2 stay accurate); R14 judges
	// presence from the raw fm.Name/fm.Description values.
	checkRequiredFrontmatter(fm, &issues)
	// R15 ALL-CAPS 命令式词密度（advisory）：ALWAYS/NEVER/MUST 合计 >5 次提醒改
	// 「指令+原因」写法——解释为什么比堆命令更有效（模型对裸命令式词会脱敏）。
	//
	// R15 ALL-CAPS imperative density (advisory): more than 5 combined
	// ALWAYS/NEVER/MUST occurrences suggests switching to "instruction + reason"
	// style — explaining why beats stacking bare imperatives.
	checkImperativeDensity(fm.Body, &advisories)
	// R16 references/ 下 >300 行文件需 ToC（advisory）。markdown 文件由 R11 以
	// >100 行的更低门槛先行覆盖，R16 跳过 markdown 避免同一文件重复 advisory；
	// R16 实际增量是覆盖非 markdown 参考文件（如大段 .txt 资料）。
	//
	// R16 references/ files over 300 lines need a ToC (advisory). Markdown files are
	// already covered by R11 at the stricter >100-line threshold, so R16 skips
	// markdown to avoid duplicate advisories; R16's real increment is non-markdown
	// reference files.
	checkOversizedRefs(skillDir, &advisories)
	// R17 evals/evals.json schema（advisory）：文件存在才校验（skill 不建 evals
	// 合法）；schema = 对象含 trigger_cases 数组，每项 {query: string,
	// should_trigger: boolean}。
	//
	// R17 evals/evals.json schema (advisory): validated only when the file exists
	// (a skill may have no evals); schema = object with a trigger_cases array of
	// {query: string, should_trigger: boolean}.
	checkEvalsSchema(skillDir, &advisories)
	// R18 forge 引用契约（advisory，依赖倒置守卫，CONVENTIONS §13）：SKILL.md
	// 正文的 forge CLI 命令引用只允许出现在「> Forge 项目」起始的条件引用块内
	// （细节归 references/forge-integration.md）。条件块之外命中 = 方法论正文耦合
	// forge，skills-only 分发（不跑 init、不装 hook）的用户会看到不可执行的指令。
	//
	// R18 forge reference contract (advisory, dependency-inversion guard,
	// CONVENTIONS §13): forge CLI references in the SKILL.md body are only allowed
	// inside conditional blockquotes starting with "> Forge 项目" (details belong in
	// references/forge-integration.md). A hit outside the conditional block means the
	// methodology body is coupled to forge, which skills-only distribution users
	// cannot execute.
	checkForgeRefs(fm, &advisories)

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

// checkReferences audits the references/ directory structure (R11):
//   - ≤1 level: files live directly under references/, no subdirectories (hard issue)
//   - markdown references over 100 lines need a ToC for navigation (advisory;
//     recognizes ## 目录 / ## Contents / ## Table of Contents)
//
// Skipped when no references directory exists (legal); advisory when the
// directory exists but is unreadable (permissions, etc.).
// TODO: scope is references/ only — peer directories like templates/scripts/adapters
// are not covered yet (Anthropic spec only names references/; expand when the spec clarifies).
//
// checkReferences 校验 references/ 目录结构（R11）：
//   - ≤1 level：references/ 下直接放文件，不应有子目录（硬 issue）
//   - >100 行的 markdown reference 需 ToC 助导航（advisory；认 ## 目录 / ## Contents / ## Table of Contents）
//
// 无 references 目录时跳过（合法）；目录存在但不可读（权限等）报 advisory。
// TODO: 作用域仅 references/——templates/scripts/adapters 等同级子目录暂不覆盖
// （Anthropic 规范文字只点名 references/，等规范明确后再扩）。
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

// checkTriggers audits metadata.triggers declarations (R12, advisory):
//   - empty: legal (a skill may opt out of the framework)
//   - non-empty: must be valid JSON
//   - per entry: event∈ValidTriggerEvents, keywords or when at least one,
//     when∈ValidConditions, match only meaningful for PreToolUse/PostToolUse
//
// Inline JSON parsing (not via skilltrigger) keeps skillsqa free of an engine
// dependency (avoids a skillsqa→skilltrigger import cycle).
//
// checkTriggers 校验 metadata.triggers 声明（R12，advisory）：
//   - 空：合法（skill 可不接入框架）
//   - 非空：须合法 JSON
//   - 每条：event∈ValidTriggerEvents、keywords 或 when 至少一、when∈ValidConditions、
//     match 仅对 PreToolUse/PostToolUse 有意义
//
// 内联 JSON 解析（不走 skilltrigger）以保持 skillsqa 对引擎包零依赖（避免循环依赖）。
func checkTriggers(raw string, advisories *[]string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	var triggers []struct {
		Event    string   `json:"event"`
		Keywords []string `json:"keywords"`
		When     string   `json:"when"`
		Match    string   `json:"match"`
	}
	if err := json.Unmarshal([]byte(raw), &triggers); err != nil {
		*advisories = append(*advisories, fmt.Sprintf(`metadata.triggers 非合法 JSON: %v`, err))
		return
	}
	for i, t := range triggers {
		idx := i + 1
		isToolEvent := t.Event == "PreToolUse" || t.Event == "PostToolUse"
		switch {
		case t.Event == "":
			*advisories = append(*advisories, fmt.Sprintf(`triggers[%d] 缺 event`, idx))
		case !ValidTriggerEvents[t.Event]:
			*advisories = append(*advisories, fmt.Sprintf(`triggers[%d] event 非法(%s)；合法: %v`, idx, t.Event, validTriggerEventsSorted()))
		}
		if len(t.Keywords) == 0 && t.When == "" {
			*advisories = append(*advisories, fmt.Sprintf(`triggers[%d] keywords 与 when 至少需一`, idx))
		}
		if t.When != "" && !ValidConditions[t.When] {
			*advisories = append(*advisories, fmt.Sprintf(`triggers[%d] when 非法(%s)；合法: %v`, idx, t.When, validConditionsSorted()))
		}
		if t.Match != "" && !isToolEvent {
			*advisories = append(*advisories, fmt.Sprintf(`triggers[%d] match 仅对 PreToolUse/PostToolUse 有效（event=%s）`, idx, t.Event))
		}
		if isToolEvent && t.Match == "" && len(t.Keywords) == 0 {
			*advisories = append(*advisories, fmt.Sprintf(`triggers[%d] PreToolUse/PostToolUse 建议带 match（限定 tool_name），否则对所有 tool 命中`, idx))
		}
	}
}

// checkBodyLines enforces R13: the SKILL.md body (everything after the
// frontmatter block) must be ≤500 lines (hard issue). Line counting mirrors
// R8's newline-count + 1 convention; an empty body counts as 0 lines.
//
// checkBodyLines 执行 R13：SKILL.md 正文（frontmatter 块之后的全部内容）
// ≤500 行（硬 issue）。计行口径与 R8 一致（换行数 + 1）；空正文计 0 行。
func checkBodyLines(body string, issues *[]string) {
	bodyLines := 0
	if body != "" {
		bodyLines = strings.Count(body, "\n") + 1
	}
	if bodyLines > 500 {
		*issues = append(*issues, fmt.Sprintf("SKILL.md 正文过长(%d行 >500，不含 frontmatter；拆 references)", bodyLines))
	}
}

// checkRequiredFrontmatter enforces R14: name and description are mandatory
// frontmatter fields (hard issues). The description ≤1024-char upper bound is
// owned by R4 and intentionally not duplicated here.
//
// checkRequiredFrontmatter 执行 R14：frontmatter 必填 name 与 description
// （硬 issue）。description ≤1024 字符上限由 R4 负责，此处不重复报。
func checkRequiredFrontmatter(fm *skillsfm.Frontmatter, issues *[]string) {
	if fm.Name == "" {
		*issues = append(*issues, "frontmatter 缺 name（必填字段）")
	}
	if fm.Description == "" {
		*issues = append(*issues, "frontmatter 缺 description（必填字段）")
	}
}

// checkImperativeDensity enforces R15: more than 5 combined whole-word
// ALWAYS/NEVER/MUST occurrences in the body goes advisory, suggesting the
// "instruction + reason" style instead of stacked bare imperatives.
//
// checkImperativeDensity 执行 R15：正文整词 ALWAYS/NEVER/MUST 合计 >5 次走
// advisory，建议改「指令+原因」写法而非堆叠裸命令式词。
func checkImperativeDensity(body string, advisories *[]string) {
	n := len(imperativeRe.FindAllStringIndex(body, -1))
	if n > 5 {
		*advisories = append(*advisories, fmt.Sprintf(`正文命令式全大写词密度过高(ALWAYS/NEVER/MUST 共 %d 次 >5；建议改「指令+原因」写法，解释为什么比堆命令更有效)`, n))
	}
}

// checkOversizedRefs enforces R16: non-markdown files under references/ over
// 300 lines without a ToC go advisory. Markdown files are skipped — R11 already
// covers them at the stricter >100-line threshold, and reporting both would
// duplicate advisories for the same file. Missing/unreadable references dir is
// silent (R11 owns those advisories).
//
// checkOversizedRefs 执行 R16：references/ 下 >300 行的非 markdown 文件无 ToC
// 走 advisory。markdown 文件跳过——R11 已以 >100 行更低门槛覆盖，重复报会同
// 文件双 advisory。references 目录不存在/不可读时静默（归 R11 的 advisory）。
func checkOversizedRefs(skillDir string, advisories *[]string) {
	refsDir := filepath.Join(skillDir, "references")
	entries, err := os.ReadDir(refsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || markdownExt(filepath.Ext(e.Name())) {
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
		if lines > 300 && !hasToC {
			*advisories = append(*advisories, fmt.Sprintf(`references/%s 过长(%d行 >300) 缺 ToC（超长参考文件建议 ToC 助导航；markdown 文件由 R11 以 >100 行门槛覆盖）`, e.Name(), lines))
		}
	}
}

// checkEvalsSchema enforces R17: when evals/evals.json exists it must match the
// schema — a JSON object with a trigger_cases array, each item
// {query: string, should_trigger: boolean}. All violations are advisory (evals
// are an opt-in regression asset, schema drift should not block Pass).
//
// checkEvalsSchema 执行 R17：evals/evals.json 存在时须符 schema——JSON 对象含
// trigger_cases 数组，每项 {query: string, should_trigger: boolean}。全部违例
// 走 advisory（evals 是可选回归资产，schema 漂移不应阻断 Pass）。
func checkEvalsSchema(skillDir string, advisories *[]string) {
	data, err := os.ReadFile(filepath.Join(skillDir, "evals", "evals.json"))
	if err != nil {
		if !os.IsNotExist(err) {
			*advisories = append(*advisories, fmt.Sprintf(`evals/evals.json 不可读: %v`, err))
		}
		return
	}
	var doc struct {
		TriggerCases []struct {
			Query         string `json:"query"`
			ShouldTrigger *bool  `json:"should_trigger"`
		} `json:"trigger_cases"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		*advisories = append(*advisories, fmt.Sprintf(`evals/evals.json 不符 schema（须为对象含 trigger_cases 数组，每项 {query: string, should_trigger: boolean}）: %v`, err))
		return
	}
	if doc.TriggerCases == nil {
		*advisories = append(*advisories, `evals/evals.json 缺 trigger_cases 数组`)
		return
	}
	for i, c := range doc.TriggerCases {
		if c.Query == "" {
			*advisories = append(*advisories, fmt.Sprintf(`evals/evals.json trigger_cases[%d] 缺 query（非空 string）`, i))
		}
		if c.ShouldTrigger == nil {
			*advisories = append(*advisories, fmt.Sprintf(`evals/evals.json trigger_cases[%d] 缺 should_trigger（boolean）`, i))
		}
	}
}

// checkForgeRefs enforces R18: forge CLI references (`forge <subcommand>`) in
// the SKILL.md body are only allowed inside conditional blockquotes that start
// with "> Forge 项目" (the block may span consecutive quote lines; indented
// quotes inside list items count — TrimSpace first). Everything else is a
// dependency-inversion violation and goes advisory. Skills declaring
// `metadata.requires_forge: "true"` (CONVENTIONS §13 form ③ — the skill itself
// documents forge's own machinery) are exempt: their body is forge-native by
// design and skills-only consumers filter them by the flag instead. Detection
// is line-based: a non-quote line closes the conditional block; a quote line
// that does not start a forge block preserves the current state.
//
// checkForgeRefs 执行 R18：SKILL.md 正文中的 forge CLI 引用（`forge <子命令>`）
// 只允许出现在「> Forge 项目」起始的条件引用块内（块可跨连续引用行；列表内缩进
// 引用块也算——先 TrimSpace）。其余位置命中即依赖倒置违例，走 advisory。声明了
// `metadata.requires_forge: "true"` 的 skill（CONVENTIONS §13 形态③——skill 本身
// 描述 forge 自身机制）豁免：其正文生来 forge 原生，skills-only 消费方按标记过滤
// 而非按引用位置。按行状态机判定：非引用行关闭条件块状态；非 forge 起始的引用行
// 维持当前状态。
func checkForgeRefs(fm *skillsfm.Frontmatter, advisories *[]string) {
	if v, ok := fm.Metadata["requires_forge"]; ok && strings.Trim(strings.TrimSpace(v), `"`) == "true" {
		return
	}
	var hits []string
	inCond := false
	for _, line := range strings.Split(fm.Body, "\n") {
		trimmed := strings.TrimSpace(line)
		isQuote := strings.HasPrefix(trimmed, ">")
		if isQuote && strings.HasPrefix(trimmed, "> Forge 项目") {
			inCond = true
		} else if !isQuote {
			inCond = false
		}
		if inCond {
			continue
		}
		for _, m := range forgeCmdRe.FindAllString(trimmed, -1) {
			hits = append(hits, strings.TrimSpace(m))
		}
	}
	if len(hits) > 0 {
		*advisories = append(*advisories, fmt.Sprintf(`正文存在条件块之外的 forge 命令引用(%v)——依赖倒置契约：forge 引用只允许「> Forge 项目」条件块或 references/forge-integration.md（CONVENTIONS §13）`, hits))
	}
}
