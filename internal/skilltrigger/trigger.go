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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
type Hit struct {
	Skill    string
	SkillDir string
	Reason   string
	Trigger  Trigger
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
// cooldown + Stop max-rounds 过滤）。noise=nil 表示不做噪音控制（测试用）。
//
// 噪音控制：Eval 只读判定（ShouldFire / StopRoundAllowed）；落盘副作用（Mark /
// IncrStopRound）由 CLI 层在确认注入后调用，保持 Eval 纯函数可测。
func Eval(ctx Context, all []SkillTriggers, noise NoiseController) []Hit {
	// Stop max-rounds 兜底：每 session 最多注入 MaxStopRounds 次，防 Stop→注入→响应→Stop 死循环。
	if ctx.Event == "Stop" && noise != nil {
		if !noise.StopRoundAllowed(ctx.SessionID, ctx.Now) {
			return nil
		}
	}
	var hits []Hit
	seen := map[string]bool{}
	for _, st := range all {
		if DeniedSkills[st.Skill] || seen[st.Skill] {
			continue
		}
		// 收集该 skill 在当前事件下命中的全部 triggers；cooldown 取命中条目的最大值，
		// 消除"数组顺序决定 cooldown"的隐藏耦合（同 skill 多 trigger 时取最保守/最长冷却）。
		// reason/event 取首条命中。
		var matched Trigger
		maxCD := 0
		for _, t := range st.Triggers {
			if !triggerMatches(t, ctx) {
				continue
			}
			if matched.Event == "" {
				matched = t
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
		if noise != nil && !noise.ShouldFire(ctx.SessionID, st.Skill, time.Duration(maxCD)*time.Second, ctx.Now) {
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
			Skill:    st.Skill,
			SkillDir: st.SkillDir,
			Reason:   reason,
			Trigger:  matched,
		})
		seen[st.Skill] = true
	}
	return hits
}

// triggerMatches 判定单条 trigger 是否命中当前 context（event + match + when + keywords）。
func triggerMatches(t Trigger, ctx Context) bool {
	if t.Event != ctx.Event {
		return false
	}
	if !matchToolName(t.Match, ctx.ToolName) {
		return false
	}
	condOK := true
	if t.When != "" {
		fn, ok := Conditions[t.When]
		if !ok || !fn(ctx) {
			condOK = false
		}
	}
	kwOK := true
	if len(t.Keywords) > 0 {
		kwOK = matchKeywords(t.Keywords, ctx)
	}
	return condOK && kwOK
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

// matchKeywords 子串大小写不敏感，haystack 拼接 prompt + command + output 文本，任一关键词命中即可。
// 覆盖 UserPromptSubmit（prompt）、PostToolUse Bash（command + stdout/stderr，如 compile-fix-loop
// 的 "compile error" 命中编译输出）等场景。
func matchKeywords(keywords []string, ctx Context) bool {
	var hay strings.Builder
	hay.WriteString(ctx.Prompt)
	if cmd, ok := ctx.ToolInput["command"].(string); ok {
		hay.WriteString(" ")
		hay.WriteString(cmd)
	}
	for _, k := range []string{"stdout", "stderr", "output"} {
		if s, ok := ctx.ToolOutput[k].(string); ok {
			hay.WriteString(" ")
			hay.WriteString(s)
		}
	}
	h := strings.ToLower(hay.String())
	if strings.TrimSpace(h) == "" {
		return false
	}
	for _, kw := range keywords {
		if kw = strings.TrimSpace(kw); kw != "" && strings.Contains(h, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

func defaultReason(skill string, t Trigger) string {
	cond := t.When
	if cond == "" {
		cond = "keywords"
	}
	return fmt.Sprintf("%s 触发条件 %s 命中，请加载该 skill", skill, cond)
}
