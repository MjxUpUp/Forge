package skillseval

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

var (
	// useWhenRe captures the segment from Use when: to (SKIP: or end of string).
	//
	// useWhenRe 捕获 Use when: 到（SKIP: 或结尾）的段落。
	useWhenRe = regexp.MustCompile(`(?is)Use when[:：]\s*(.*?)(?:SKIP[:：]|$)`)
	// skipPartRe captures the segment from SKIP: to end of string.
	//
	// skipPartRe 捕获 SKIP: 到结尾的段落。
	skipPartRe = regexp.MustCompile(`(?is)SKIP[:：]\s*(.*?)$`)
	// useSplitRe delimits the Use when segment: ideographic comma / CJK or ASCII comma / semicolon / the word or。
	//
	// useSplitRe Use when 段的分隔符：顿号/中英文逗号/分号/ or 。
	useSplitRe = regexp.MustCompile(`[、，,；;]|\s+or\s+`)
	// skipSplitRe delimits the SKIP segment: ideographic comma / CJK or ASCII comma / semicolon / the Chinese disjunctive word。
	//
	// skipSplitRe SKIP 段的分隔符：顿号/中英文逗号/分号/或。
	skipSplitRe = regexp.MustCompile(`[、，,；;]|或`)
)

// ExtractTriggers extracts trigger/skip scenarios from the description's Use when / SKIP sections.
// Aligns with skill-eval.py extract_triggers: triggers≤5, skips≤3, fragments with length≤3 are dropped.
//
// ExtractTriggers 从 description 的 Use when / SKIP 段提取触发/排除场景。
// 对齐 skill-eval.py extract_triggers：triggers≤5、skips≤3，长度≤3 的片段丢弃。
func ExtractTriggers(description string) (triggers, skips []string) {
	if m := useWhenRe.FindStringSubmatch(description); m != nil {
		for _, part := range useSplitRe.Split(m[1], -1) {
			p := trimTriggerPart(part)
			if utf8.RuneCountInString(p) > 3 {
				triggers = append(triggers, p)
			}
		}
	}
	if m := skipPartRe.FindStringSubmatch(description); m != nil {
		for _, part := range skipSplitRe.Split(m[1], -1) {
			p := trimTriggerPart(part)
			if utf8.RuneCountInString(p) <= 3 {
				continue
			}
			// Filter prefixes containing the CJK ideograph yong (Python part[:3] includes yong + space).
			//
			// 过滤前缀含"用 "（Python part[:3] 含"用 "）
			if strings.Contains(firstNRunes(p, 3), "用 ") {
				continue
			}
			skips = append(skips, p)
		}
	}
	if len(triggers) > 5 {
		triggers = triggers[:5]
	}
	if len(skips) > 3 {
		skips = skips[:3]
	}
	return triggers, skips
}

// trimTriggerPart mirrors Python part.strip().strip(triple-double-quote).strip(): trim space, strip quotes, trim space.
//
// trimTriggerPart 对齐 Python part.strip().strip('"""').strip()：去空白→去引号→去空白。
func trimTriggerPart(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, "\"")
	s = strings.TrimSpace(s)
	return s
}

func firstNRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

// GenerateEvalPrompts turns trigger/skip scenarios into should-trigger / should-not-trigger test prompts.
// Aligns with skill-eval.py generate_eval_prompts (用户说→empty, 用户需要→我需要, 用户要→我要; skip gets miscue prefix).
//
// The rendering logic is factored into renderTriggerPrompt/renderSkipPrompt as a single source of truth — EvalCases
// reuses the same rendering when generating structured cases, ensuring prompts match verbatim between markdown lists and case sets.
//
// GenerateEvalPrompts 把触发/排除场景转成 should-trigger / should-not-trigger 测试 prompt。
// 对齐 skill-eval.py generate_eval_prompts（用户说→空、用户需要→我需要、用户要→我要；skip 加误问前缀）。
//
// 渲染逻辑抽到 renderTriggerPrompt/renderSkipPrompt 作为单一真相源——EvalCases
// 生成结构化 case 时复用同一渲染，保证 markdown 清单与 case 集的 prompt 逐字一致。
func GenerateEvalPrompts(name, description string) (shouldTrigger, shouldNot []string) {
	triggers, skips := ExtractTriggers(description)
	for _, t := range triggers {
		shouldTrigger = append(shouldTrigger, renderTriggerPrompt(t))
	}
	for _, s := range skips {
		shouldNot = append(shouldNot, renderSkipPrompt(s))
	}
	return shouldTrigger, shouldNot
}

// renderTriggerPrompt renders a trigger fragment into a test prompt (first-person colloquial).
//
// renderTriggerPrompt 渲染 trigger 片段为测试 prompt（第一人称口语化）。
func renderTriggerPrompt(raw string) string {
	p := strings.ReplaceAll(raw, "用户说", "")
	p = strings.ReplaceAll(p, "用户需要", "我需要")
	p = strings.ReplaceAll(p, "用户要", "我要")
	return strings.Trim(p, "，。 \"")
}

// renderSkipPrompt renders a skip fragment into a miscue test prompt (prefixed with a should-use-other-skill hint).
//
// renderSkipPrompt 渲染 skip 片段为误问测试 prompt（加「应该用其他 skill」前缀）。
func renderSkipPrompt(raw string) string {
	p := strings.ReplaceAll(raw, "用 ", "（这种情况应该用其他 skill，但用户可能误问：）")
	return strings.Trim(p, "，。")
}

// EvalSkill generates an eval markdown checklist for a single skill (mirrors skill-eval.py eval_skill).
//
// EvalSkill 对单个 skill 生成 eval markdown 清单（对齐 skill-eval.py eval_skill）。
func EvalSkill(canonical, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(canonical, name, "SKILL.md"))
	if err != nil {
		return "", err
	}
	fm := skillsfm.Parse(data)
	desc := fm.Description
	shouldTrigger, shouldNot := GenerateEvalPrompts(name, desc)

	// Description display: only truncate with ellipsis when exceeding 200 runes — the original impl always
	// appended ... unconditionally, producing xxx... even for short descriptions, falsely implying truncation.
	// GenerateEvalPrompts still uses the full desc (for trigger/skip extraction).
	//
	// 描述展示：仅当超 200 rune 才截断并加省略号——原实现无脑追加 "..."，对短描述
	// 也产生 "xxx..."，误读为被截断。GenerateEvalPrompts 仍用完整 desc（触发/排除提取）。
	descDisplay := desc
	if utf8.RuneCountInString(desc) > 200 {
		descDisplay = firstNRunes(desc, 200) + "..."
	}

	var b strings.Builder
	b.WriteString("# Skill Eval: " + name + "\n\n")
	b.WriteString("> 外部 eval 方法论\n")
	b.WriteString("> 用 subagent 半自动跑（pi 无 claude -p 自动模式）\n\n")
	b.WriteString("## Description（被测对象）\n```\n" + descDisplay + "\n```\n\n")

	b.WriteString("## Should-Trigger 测试 prompt（" + strconv.Itoa(len(shouldTrigger)) + " 个）\n")
	b.WriteString("这些 prompt **应该**触发该 skill。用 subagent 跑，检查是否正确加载：\n\n")
	for i, p := range shouldTrigger {
		b.WriteString(strconv.Itoa(i+1) + ". `" + p + "`\n")
	}
	b.WriteString("\n")

	b.WriteString("## Should-NOT-Trigger 测试 prompt（" + strconv.Itoa(len(shouldNot)) + " 个）\n")
	b.WriteString("这些 prompt **不应该**触发该 skill（应触发其他 skill）。检查是否误触发：\n\n")
	for i, p := range shouldNot {
		b.WriteString(strconv.Itoa(i+1) + ". `" + p + "`\n")
	}
	b.WriteString("\n")

	b.WriteString("## 执行方式（subagent 半自动）\n\n")
	b.WriteString("```\n")
	b.WriteString("# 对每个 should-trigger prompt，dispatch 一个 fresh subagent：\n")
	b.WriteString("# subagent 任务：「你是新 session，收到这个 prompt，你会加载哪个 skill？为什么？」\n")
	b.WriteString("# 检查：subagent 是否说会加载目标 skill\n")
	b.WriteString("#\n")
	b.WriteString("# 对每个 should-not-trigger prompt，同样 dispatch：\n")
	b.WriteString("# 检查：subagent 是否正确说不会加载目标 skill（而是其他）\n")
	b.WriteString("```\n\n")

	b.WriteString("## 记录结果\n\n")
	b.WriteString("| prompt | 预期 | 实际触发 | 正确？ | 备注 |\n")
	b.WriteString("|--------|------|---------|--------|------|\n")
	for _, p := range shouldTrigger {
		b.WriteString("| " + firstNRunes(p, 40) + "... | ✅ " + name + " | | | |\n")
	}
	for _, p := range shouldNot {
		b.WriteString("| " + firstNRunes(p, 40) + "... | ❌ 不触发 | | | |\n")
	}
	return b.String(), nil
}

// EvalAll generates eval checklists for all skills under canonical (mirrors skill-eval.py --all).
// Skills whose SKILL.md cannot be read are skipped. Returns name→markdown.
//
// EvalAll 为 canonical 下所有 skill 生成 eval 清单（对齐 skill-eval.py --all）。
// 读不到 SKILL.md 的 skill 跳过。返回 name→markdown。
func EvalAll(canonical string) (map[string]string, error) {
	names, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, n := range names {
		md, err := EvalSkill(canonical, n)
		if err != nil {
			continue
		}
		out[n] = md
	}
	return out, nil
}
