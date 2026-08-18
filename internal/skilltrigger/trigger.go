// Package skilltrigger 是通用 skill 触发框架的核心引擎（agent-neutral）。
//
// skill 在 frontmatter metadata.triggers 声明触发条件（关键词或命名 condition），
// 本包扫描 canonical skills、按当前事件 + 上下文判定命中、渲染"加载该 skill"指引。
// 把 code-review-gate 的"专属子命令 + hook + 状态"高成本模式抽象成声明式触发 + 通用引擎，
// 让质量/流程类 skill（test-discipline / implementation-discipline 等）在事件点被 hook
// 主动驱动，不再依赖 agent 自觉。
//
// Package skilltrigger is the agent-neutral core engine of the generic skill-trigger
// framework. A skill declares trigger conditions in frontmatter metadata.triggers
// (keywords or a named condition); this package scans canonical skills, evaluates hits
// against the current event + context, and renders "load this skill" guidance.
package skilltrigger

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/util"
)

// Trigger 是 frontmatter metadata.triggers JSON 数组中的一项。
// Trigger is one entry in the frontmatter metadata.triggers JSON array.
type Trigger struct {
	Event    string   `json:"event"`              // UserPromptSubmit|PreToolUse|PostToolUse|Stop|SessionStart
	Keywords []string `json:"keywords,omitempty"` // 子串不区分大小写；与 When 为 AND
	When     string   `json:"when,omitempty"`     // 命名 condition（∈ Conditions 词汇表）
	Match    string   `json:"match,omitempty"`    // tool_name matcher（PreToolUse/PostToolUse；| 分隔）
	Reason   string   `json:"reason,omitempty"`   // 注入理由（覆盖默认模板）
	Cooldown int      `json:"cooldown,omitempty"` // per-session per-skill 冷却秒数（默认 DefaultCooldown）
}

// SkillTriggers 是一个 skill 的全部 triggers。
// SkillTriggers holds all triggers of one skill.
type SkillTriggers struct {
	Skill    string // skill 名（frontmatter.name，fallback 目录名）
	SkillDir string // canonical/<skill> 绝对路径
	Triggers []Trigger
}

// Context 是一次 hook 调用传给引擎的全部上下文（agent-neutral）。
// Context carries the full context of one hook invocation into the engine.
type Context struct {
	Event        string
	Prompt       string         // UserPromptSubmit 的 prompt
	ToolName     string         // hook_input.tool_name
	ToolInput    map[string]any // 已解析的 tool_input（file_path/command/content）
	ToolOutput   map[string]any // 已解析的 tool_output（exit_code/stdout/stderr/interrupted）
	SessionID    string
	ProjectRoot  string // "" 表示非 forge project（condition 优雅降级）
	CanonicalDir string
	Now          time.Time // 注入测试可控时间
}

// Hit 是一次命中的结果。Hit is one positive match.
//
// v2 证据字段（对抗辩论 P0）：matched_keyword / match_source / prompt_hash / trigger_sig
// 让每条命中自带「因为什么、在哪段输入、被哪条规则」的可审计证据——checklog.Detail 只留
// 人类摘要，结构化载荷经 cli 层落 Entry.Meta（键契约见 checklog.MetaKey*）。
//
// v2 evidence fields (debate P0): matched_keyword / match_source / prompt_hash /
// trigger_sig give every hit auditable evidence of "because of what, in which input, by
// which rule" — checklog.Detail keeps only the human summary; the structured payload lands
// in Entry.Meta via the cli layer (key contract: checklog.MetaKey*).
type Hit struct {
	Skill    string
	SkillDir string
	Reason   string
	Trigger  Trigger
	// MatchedKeyword is the specific keyword that fired ("" for condition-only triggers).
	// matchKeywords previously returned a bare bool — the fired keyword died inside the
	// function, making per-keyword noise/dead-keyword analysis impossible (the "thin audit
	// record" gap this v2 closes).
	//
	// MatchedKeyword 是实际命中的具体关键词（condition-only 触发为 ""）。matchKeywords
	// 原先只返回 bool——命中词死在函数内部，per-keyword 噪声/死关键词分析无从做起
	//（本 v2 闭合的「审计记录太薄」缺口）。
	MatchedKeyword string
	// MatchSource names which source text the keyword hit (MatchSource* constants). v2
	// evaluates each source separately — a keyword can no longer match across the
	// prompt/command/output boundary (semantic change, pinned by tests; boundary matches
	// were an untrackable false-positive generator).
	//
	// MatchSource 标注关键词命中的来源文本（MatchSource* 常量）。v2 分源判定——关键词
	// 不再可能跨 prompt/命令/输出边界命中（语义变更，测试钉死；跨边界命中原是无法
	// 追踪的误报生成器）。
	MatchSource string
	// TriggerIndex is the index of the first matching rule in the skill's triggers array.
	//
	// TriggerIndex 是首条命中规则在 skill triggers 数组中的下标。
	TriggerIndex int
	// TriggerSig is sha1[:8] of the declared rule JSON — rule identity that survives array
	// reordering (longitudinal per-rule stats key on this, not the index).
	//
	// TriggerSig 是声明规则 JSON 的 sha1[:8]——在数组重排下存活的规则身份（纵向
	// per-rule 统计以此为主键，而非下标）。
	TriggerSig string
	// PromptHash is sha1[:12] of (project salt + prompt). Project-scoped salt (NOT session:
	// debate G4 — session salt would break the cross-session dedup mining relies on).
	// "" when no prompt (tool events).
	//
	// PromptHash 是（项目盐 + prompt）的 sha1[:12]。项目级盐（不用 session 盐：辩论
	// G4——session 盐会破坏挖矿依赖的跨 session 去重）。无 prompt（tool 事件）为 ""。
	PromptHash string
	// PromptLen is the rune length of the prompt at hit time.
	//
	// PromptLen 是命中时 prompt 的 rune 长度。
	PromptLen int
}

// MatchSource* name the source text a keyword hit (priority order for attribution).
//
// MatchSource* 标注关键词命中的来源文本（归因的优先序）。
const (
	MatchSourcePrompt  = "prompt"
	MatchSourceCommand = "command"
	MatchSourceStdout  = "stdout"
	MatchSourceStderr  = "stderr"
	MatchSourceOutput  = "output"
)

// Suppression causes (Eval's second return value).
//
// 抑制原因（Eval 的第二个返回值）。
const (
	// SuppressCooldown: triggers matched but the per-session per-skill cooldown window
	// (normal dedup — expected, counted for cooldown tuning, NOT alarmed).
	//
	// SuppressCooldown：触发条件命中但处于 per-session per-skill cooldown 窗口内
	//（正常去重——预期行为，计数供 cooldown 调参，不告警）。
	SuppressCooldown = "cooldown"
	// SuppressStopCap: Stop event hit the per-session MaxStopRounds ceiling (loop-guard —
	// recorded as ONE warn advisory per occurrence by the cli layer, not per skill).
	//
	// SuppressStopCap：Stop 事件触到 per-session MaxStopRounds 上限（防死循环兜底——
	// cli 层每次触顶记一条 warn advisory，不逐 skill 记）。
	SuppressStopCap = "stop-max-rounds"
)

// Suppressed is a would-have-fired hit blocked by noise control. Returned (not recorded)
// by Eval — the cli layer owns the side effects: cooldown counts backfill into the next
// fire's Meta (MetaKeySuppressedSinceLast); stop-cap becomes a warn advisory. Honest gap:
// a suppression burst at session end is never backfilled (no next fire) — documented,
// accepted (per-event suppression records would flood the log; the debate's R8 compromise).
//
// Suppressed 是被噪音控制拦下的「本会命中」。Eval 只返回不落盘——副作用归 cli 层：
// cooldown 计数回填进下次触发的 Meta（MetaKeySuppressedSinceLast）；stop-cap 记一条
// warn advisory。诚实缺口：session 末段的抑制突发永远不会被回填（没有下次触发）——
// 已文档化并接受（逐条抑制记录会淹日志；辩论 R8 的折中）。
type Suppressed struct {
	Skill   string
	Trigger Trigger
	Cause   string // SuppressCooldown | SuppressStopCap
}

// DeniedSkills 有专用 driver 的 skill——框架强制忽略其 triggers，避免双重注入。
// code-review-gate 由 review-stop hook 驱动；skill-routing 由 skill-router-claude.sh 驱动。
var DeniedSkills = map[string]bool{
	"code-review-gate": true,
	"skill-routing":    true,
}

// DefaultCooldown 默认 per-session per-skill 冷却秒数。
const DefaultCooldown = 60

// ParseTriggers 解析 JSON 字符串 → []Trigger；空串/非法 JSON 返 nil（不阻塞框架）。
func ParseTriggers(raw string) []Trigger {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ts []Trigger
	if err := json.Unmarshal([]byte(raw), &ts); err != nil {
		return nil
	}
	return ts
}

// LoadAll 扫描 canonicalDir 下所有 SKILL.md，解析 triggers。无 triggers / 解析失败 / 被
// deny 的 skill 一律跳过，绝不阻塞框架。
func LoadAll(canonicalDir string) []SkillTriggers {
	if canonicalDir == "" {
		return nil
	}
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		return nil
	}
	var out []SkillTriggers
	for _, e := range entries {
		if !util.DirEntryIsDir(canonicalDir, e) {
			continue
		}
		name := e.Name()
		if DeniedSkills[name] {
			continue
		}
		skillMD := filepath.Join(canonicalDir, name, "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			continue
		}
		fm := skillsfm.Parse(data)
		raw := fm.Metadata["triggers"]
		triggers := ParseTriggers(raw)
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && trimmed != "[]" && len(triggers) == 0 {
			// 非空、非合法空数组（"[]"是 skill 明确声明"无 trigger"）但解析失败 = 非法 JSON；
			// R12 audit 报详情，此处 stderr 警告让运行时也可见，否则 skill 作者不知 triggers 静默失效。
			fmt.Fprintf(os.Stderr, "[skill-trigger] warning: %s/SKILL.md metadata.triggers 非法 JSON，已跳过（forge skills audit R12 报详情）\n", name)
		}
		if len(triggers) == 0 {
			continue
		}
		skillName := fm.Name
		if skillName == "" {
			skillName = name
		}
		out = append(out, SkillTriggers{
			Skill:    skillName,
			SkillDir: filepath.Join(canonicalDir, name),
			Triggers: triggers,
		})
	}
	return out
}

// Eval 是纯函数：给定 Context + 全部 triggers + 噪音控制器，返回命中的 skills（已去重 +
// cooldown + Stop max-rounds 过滤）与被抑制的近似命中。noise=nil 表示不做噪音控制（测试用）。
//
// 噪音控制：Eval 只读判定（ShouldFire / StopRoundAllowed）；落盘副作用（Mark /
// IncrStopRound / 抑制计数）由 CLI 层在确认注入后调用，保持 Eval 纯函数可测。
//
// v2（辩论 P1 抑制可观测）：被 cooldown / Stop max-rounds 拦下的「本会命中」以 Suppressed
// 返回——命中计数从此不再系统性偏低，cooldown 默认值第一次有调参数据。全局/per-skill
// 显式禁用（FORGE_SKILL_TRIGGER=0 / _DISABLE 列表）是有意配置，不算抑制、不返回。
//
// v2 (debate P1 suppression observability): would-have-fired hits blocked by cooldown /
// Stop max-rounds come back as Suppressed — hit counts stop being systematically
// undercounted, and the cooldown default gets tuning data for the first time. Explicit
// global/per-skill disables (FORGE_SKILL_TRIGGER=0 / _DISABLE list) are intentional config,
// not suppression — not returned.
func Eval(ctx Context, all []SkillTriggers, noise NoiseController) (hits []Hit, suppressed []Suppressed) {
	stopCapped := false
	// Stop max-rounds 兜底：每 session 最多注入 MaxStopRounds 次，防 Stop→注入→响应→Stop 死循环。
	// v2：被兜底拦截时不再提前返回——照常扫描，把「本会命中」作为 SuppressStopCap 返回，
	// CLI 层据此记一条 warn advisory（原先此处零留痕，Stop 抑制是纯黑洞）。
	if ctx.Event == "Stop" && noise != nil {
		if !noise.StopRoundAllowed(ctx.SessionID, ctx.Now) {
			stopCapped = true
		}
	}
	seen := map[string]bool{}
	for _, st := range all {
		if DeniedSkills[st.Skill] || seen[st.Skill] {
			continue
		}
		// 收集该 skill 在当前事件下命中的全部 triggers；cooldown 取命中条目的最大值，
		// 消除"数组顺序决定 cooldown"的隐藏耦合（同 skill 多 trigger 时取最保守/最长冷却）。
		// reason/event/规则身份取首条命中。
		var matched Trigger
		matchedIdx := 0
		var kw keywordMatch
		maxCD := 0
		for i, t := range st.Triggers {
			km, ok := triggerMatches(t, ctx)
			if !ok {
				continue
			}
			if matched.Event == "" {
				matched = t
				matchedIdx = i
				kw = km
			}
			cd := t.Cooldown
			if cd <= 0 {
				cd = DefaultCooldown
			}
			if cd > maxCD {
				maxCD = cd
			}
		}
		if matched.Event == "" {
			continue
		}
		// stop-cap 拦截整事件：全部命中降级为 suppressed（cause 归因 stop-max-rounds 主导，
		// 不再叠加 cooldown 判定——ShouldFire 的 marker 检查在 stop-cap 语境下无意义）。
		if stopCapped {
			suppressed = append(suppressed, Suppressed{Skill: st.Skill, Trigger: matched, Cause: SuppressStopCap})
			seen[st.Skill] = true
			continue
		}
		// 有意禁用（env）≠ 抑制：静默跳过，不进 suppressed（辩论 P1 边界）。
		if isSkillDisabled(st.Skill) {
			continue
		}
		if noise != nil && !noise.ShouldFire(ctx.SessionID, st.Skill, time.Duration(maxCD)*time.Second, ctx.Now) {
			suppressed = append(suppressed, Suppressed{Skill: st.Skill, Trigger: matched, Cause: SuppressCooldown})
			seen[st.Skill] = true
			continue
		}
		// matched 是循环内拷贝，设其 Cooldown 反映实际应用的 maxCD（首条命中 trigger 的 Cooldown
		// 可能是 0，应用时 normalize 为 DefaultCooldown；maxCD 可能来自后续命中的更大 trigger）。
		// 不污染原 st.Triggers，且让 Hit.Trigger.Cooldown 这一隐性不变量保持一致（N2）。
		matched.Cooldown = maxCD
		reason := matched.Reason
		if reason == "" {
			reason = defaultReason(st.Skill, matched)
		}
		hits = append(hits, Hit{
			Skill:          st.Skill,
			SkillDir:       st.SkillDir,
			Reason:         reason,
			Trigger:        matched,
			MatchedKeyword: kw.Keyword,
			MatchSource:    kw.Source,
			TriggerIndex:   matchedIdx,
			TriggerSig:     triggerSig(matched),
			PromptHash:     promptHash(ctx),
			PromptLen:      utf8.RuneCountInString(ctx.Prompt),
		})
		seen[st.Skill] = true
	}
	return hits, suppressed
}

// triggerMatches 判定单条 trigger 是否命中当前 context（event + match + when + keywords），
// v2 同时返回关键词匹配证据（keyword-only 触发 km 为零值）。
func triggerMatches(t Trigger, ctx Context) (keywordMatch, bool) {
	if t.Event != ctx.Event {
		return keywordMatch{}, false
	}
	if !matchToolName(t.Match, ctx.ToolName) {
		return keywordMatch{}, false
	}
	condOK := true
	if t.When != "" {
		fn, ok := Conditions[t.When]
		if !ok || !fn(ctx) {
			condOK = false
		}
	}
	var km keywordMatch
	kwOK := true
	if len(t.Keywords) > 0 {
		km, kwOK = matchKeywords(t.Keywords, ctx)
	}
	return km, condOK && kwOK
}

// matchToolName 检查 tool_name 是否匹配 trigger.match（ForgeHookSpec matcher 语义：
// 空 match = 全匹配；否则 | 分隔任一，大小写不敏感）。
func matchToolName(match, toolName string) bool {
	if match == "" {
		return true
	}
	for _, m := range strings.Split(match, "|") {
		if m = strings.TrimSpace(m); m != "" && strings.EqualFold(m, toolName) {
			return true
		}
	}
	return false
}

// keywordMatch 携带关键词命中证据：哪个词、在哪段来源文本。
// keywordMatch carries keyword-hit evidence: which word, in which source text.
type keywordMatch struct {
	Keyword string
	Source  string // MatchSource* 常量
}

// matchKeywords 分源子串匹配（大小写不敏感），返回首个 (来源, 关键词) 命中。
//
// v2 语义变更（对抗辩论 R1，测试钉死）：v1 把 prompt + command + stdout/stderr/output
// 拼接成单一 haystack 再 Contains——关键词可以跨源边界命中（prompt 结尾 "compile" +
// 命令开头 "error:" 拼出 "compile error"），既无法归因来源、又是不可追踪的误报生成器。
// v2 逐源独立判定：prompt（原文，覆盖 UserPromptSubmit）→ command（经 sanitizeCommand
// 剥离 heredoc body，防脚本文本里出现 "npm publish" 误触发布守卫，详见 command_noise.go）
// → stdout → stderr → output（覆盖 PostToolUse Bash 的编译输出等场景），任一 (源, 词)
// 命中即真。优先序 = v1 拼接顺序；判定真值除跨源边界外与 v1 一致——跨源边界命中的消失
// 是**有意的语义变更**（非增量），battery 预期呈现相应命中变化。
//
// matchKeywords does per-source case-insensitive substring matching; returns the first
// (source, keyword) hit. v2 semantic change (debate R1, pinned by tests): v1 concatenated
// prompt + command + outputs into one haystack — a keyword could match across the
// prompt/command/output boundary (prompt ending "compile" + command starting "error:"
// splicing into "compile error"), neither attributable to a source nor trackable as the
// false-positive generator it was. v2 evaluates each source independently: prompt (raw,
// UserPromptSubmit) → command (sanitizeCommand-stripped, so script text merely mentioning
// "npm publish" cannot false-positive release guards — see command_noise.go) → stdout →
// stderr → output (PostToolUse build output etc.); any (source, keyword) hit wins.
// Priority order = v1's concatenation order; truth values match v1 except boundary-spanning
// matches, whose disappearance is the INTENDED semantic change (not additive) — the battery
// is expected to show the corresponding hit shift.
func matchKeywords(keywords []string, ctx Context) (keywordMatch, bool) {
	cmd, _ := ctx.ToolInput["command"].(string)
	out := func(k string) string { s, _ := ctx.ToolOutput[k].(string); return s }
	sources := []struct {
		name string
		text string
	}{
		{MatchSourcePrompt, ctx.Prompt},
		{MatchSourceCommand, sanitizeCommand(cmd)},
		{MatchSourceStdout, out("stdout")},
		{MatchSourceStderr, out("stderr")},
		{MatchSourceOutput, out("output")},
	}
	for _, src := range sources {
		h := strings.ToLower(src.text)
		if strings.TrimSpace(h) == "" {
			continue
		}
		for _, kw := range keywords {
			if kw = strings.TrimSpace(kw); kw != "" && strings.Contains(h, strings.ToLower(kw)) {
				return keywordMatch{Keyword: kw, Source: src.name}, true
			}
		}
	}
	return keywordMatch{}, false
}

// Excerpt 返回命中关键词在指定来源文本中首次出现位置 ±radius rune 的窗口（脱敏由调用方
// 负责——引擎保持无 redact 依赖）。找不到/来源为空返回 ""。仅服务 opt-in 摘录采集
// （FORGE_TRIGGER_EXCERPT=1，cli 层判定）与 triage。
//
// Excerpt returns a ±radius-rune window around the keyword's first occurrence in the named
// source text (redaction is the caller's job — the engine keeps no redact dependency).
// Returns "" when not found / source empty. Serves only opt-in excerpt capture
// (FORGE_TRIGGER_EXCERPT=1, decided in the cli layer) and triage.
func Excerpt(ctx Context, source, keyword string, radius int) string {
	if keyword == "" || radius <= 0 {
		return ""
	}
	cmd, _ := ctx.ToolInput["command"].(string)
	out := func(k string) string { s, _ := ctx.ToolOutput[k].(string); return s }
	var text string
	switch source {
	case MatchSourcePrompt:
		text = ctx.Prompt
	case MatchSourceCommand:
		text = sanitizeCommand(cmd)
	case MatchSourceStdout:
		text = out("stdout")
	case MatchSourceStderr:
		text = out("stderr")
	case MatchSourceOutput:
		text = out("output")
	default:
		return ""
	}
	runes := []rune(text)
	needle := strings.ToLower(keyword)
	idx := strings.Index(strings.ToLower(text), needle)
	if idx < 0 {
		return ""
	}
	// byte→rune 换算：窗口按 rune 计（避免切坏 UTF-8 中文）。
	runeIdx := utf8.RuneCountInString(text[:idx])
	lo, hi := runeIdx-radius, runeIdx+utf8.RuneCountInString(needle)+radius
	if lo < 0 {
		lo = 0
	}
	if hi > len(runes) {
		hi = len(runes)
	}
	return string(runes[lo:hi])
}

// promptHash 计算 sha1(projectSalt + prompt)[:12]。盐 = ProjectRoot（项目级）：项目内
// 可聚类、跨项目不可关联；刻意不含 sessionID——辩论 G4：session 盐会破坏挖矿（P2）依赖
// 的跨 session prompt 去重。空 prompt 返回 ""。
//
// promptHash computes sha1(projectSalt + prompt)[:12]. Salt = ProjectRoot (project scope):
// clusterable within a project, uncorrelatable across projects; deliberately WITHOUT
// sessionID — debate G4: a session salt would break the cross-session prompt dedup that
// mining (P2) relies on. Empty prompt returns "".
func promptHash(ctx Context) string {
	if strings.TrimSpace(ctx.Prompt) == "" {
		return ""
	}
	sum := sha1.Sum([]byte(ctx.ProjectRoot + "\x00" + ctx.Prompt))
	return hex.EncodeToString(sum[:])[:12]
}

// triggerSig 计算声明规则的 sha1[:8]——不依赖数组顺序的规则身份。sig 对「声明内容」
// （含声明 Cooldown）计算，不受运行时 maxCD 归一化影响（matched.Cooldown 覆写发生在
// sig 计算之后——Eval 中 sig 在覆写前的拷贝上算）。
//
// triggerSig computes sha1[:8] of the declared rule — order-independent rule identity.
// Signed over the DECLARED content (incl. declared Cooldown), unaffected by runtime maxCD
// normalization (matched.Cooldown overwrite happens after — Eval signs the pre-overwrite
// copy).
func triggerSig(t Trigger) string {
	b, err := json.Marshal(t)
	if err != nil {
		return ""
	}
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])[:8]
}

func defaultReason(skill string, t Trigger) string {
	cond := t.When
	if cond == "" {
		cond = "keywords"
	}
	return fmt.Sprintf("%s 触发条件 %s 命中，请加载该 skill", skill, cond)
}
