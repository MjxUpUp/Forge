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
	// useWhenRe 捕获 Use when: 到（SKIP: 或结尾）的段落。
	useWhenRe = regexp.MustCompile(`(?is)Use when[:：]\s*(.*?)(?:SKIP[:：]|$)`)
	// skipPartRe 捕获 SKIP: 到结尾的段落。
	skipPartRe = regexp.MustCompile(`(?is)SKIP[:：]\s*(.*?)$`)
	// useSplitRe Use when 段的分隔符：顿号/中英文逗号/分号/ or 。
	useSplitRe = regexp.MustCompile(`[、，,；;]|\s+or\s+`)
	// skipSplitRe SKIP 段的分隔符：顿号/中英文逗号/分号/或。
	skipSplitRe = regexp.MustCompile(`[、，,；;]|或`)
)

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

// renderTriggerPrompt 渲染 trigger 片段为测试 prompt（第一人称口语化）。
func renderTriggerPrompt(raw string) string {
	p := strings.ReplaceAll(raw, "用户说", "")
	p = strings.ReplaceAll(p, "用户需要", "我需要")
	p = strings.ReplaceAll(p, "用户要", "我要")
	return strings.Trim(p, "，。 \"")
}

// renderSkipPrompt 渲染 skip 片段为误问测试 prompt（加「应该用其他 skill」前缀）。
func renderSkipPrompt(raw string) string {
	p := strings.ReplaceAll(raw, "用 ", "（这种情况应该用其他 skill，但用户可能误问：）")
	return strings.Trim(p, "，。")
}

// EvalSkill 对单个 skill 生成 eval markdown 清单（对齐 skill-eval.py eval_skill）。
func EvalSkill(canonical, name string) (string, error) {
	data, err := os.ReadFile(filepath.Join(canonical, name, "SKILL.md"))
	if err != nil {
		return "", err
	}
	fm := skillsfm.Parse(data)
	desc := fm.Description
	shouldTrigger, shouldNot := GenerateEvalPrompts(name, desc)

	// 描述展示：仅当超 200 rune 才截断并加省略号——原实现无脑追加 "..."，对短描述
	// 也产生 "xxx..."，误读为被截断。GenerateEvalPrompts 仍用完整 desc（触发/排除提取）。
	descDisplay := desc
	if utf8.RuneCountInString(desc) > 200 {
		descDisplay = firstNRunes(desc, 200) + "..."
	}

	var b strings.Builder
	b.WriteString("# Skill Eval: " + name + "\n\n")
	b.WriteString("> 借鉴 Thariq skill-creator + SkillForge Analyzer 的外部 eval 方法论\n")
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
