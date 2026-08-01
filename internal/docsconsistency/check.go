// Package docsconsistency detects whether forge commands referenced by backticks in
// docs actually exist in the cobra command tree.
//
// Two consumers:
//   - cli/docs_consistency_test.go guards A/B (run by CI on every go test, catches
//     docs that have already drifted).
//   - taskpipeline executor.go task-complete advisory (a local pre-commit reminder so
//     drift is caught early).
//
// The source of truth is rootCmd's cobra command tree (in the cli package). This
// package cannot import cli (a main dependency; would cause an import cycle), so it
// uses a RegisterCommandTree callback: the cli package injects
// func(){ return rootCmd } at init time, and this package fetches the tree via the
// callback. When nothing is registered, ValidateForgePath passes through (returns ""),
// so the advisory/guard does not false-report — ensuring that callers without a
// registered callback (e.g. unit tests) do not see phantom drift.
//
// Package docsconsistency 检测"文档反引号引用的 forge 命令"是否真实存在于 cobra 命令树。
//
// 两个消费方：
//   - cli/docs_consistency_test.go 守卫 A/B（CI 每次 go test 跑，发现已 drift 的文档）
//   - taskpipeline executor.go task-complete advisory（本地提交前提醒，drift 早发现）
//
// 真相源是 rootCmd 的 cobra 命令树（在 cli 包）。本包不能 import cli（main 依赖，防循环），
// 故用 RegisterCommandTree 回调：cli 包 init 时注入 func(){ return rootCmd }，本包通过
// 回调拿命令树。未注册时 ValidateForgePath 放行（返回 ""），advisory/守卫不误报——
// 保证本包被未注册回调的调用方（如单元测试）使用时不报假 drift。
package docsconsistency

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	// forgeBacktickRef matches forge command references inside backticks, such as
	// `forge experience accept`. The backtick delimiter excludes prose like
	// `forge 是...` from matching, sharply reducing false positives. The character class
	// also excludes \n: without it the class could span lines and splice two independent
	// code spans across a line break into one phantom reference.
	//
	// forgeBacktickRef 匹配反引号内的 forge 命令引用，如 `forge experience accept`。
	// 反引号限定把散文里的 "forge 是…" 排除在外，大幅减 false positive。字符类同时
	// 排除 \n：否则字符类可跨行，把被换行隔开的两个独立 code span 拼成幻影引用。
	forgeBacktickRef = regexp.MustCompile("`forge ([^`\n]+)`")

	// commandNameRe describes a legal cobra command name (the first word of Use).
	// Non-command tokens — placeholders like <id>, flags like --force, brackets like
	// [--mode], alternatives like small|medium, or Chinese descriptions — never match;
	// this is used during level-by-level validation to decide that the command path
	// ends here and the rest are arguments.
	//
	// commandNameRe 描述合法 cobra 命令名（Use 的首词）。非命令 token——占位符 <id>、
	// flag --force、方括号 [--mode]、分隔符 small|medium、中文说明——一律不匹配，
	// 用于在逐级验证时判定"命令路径到此结束，剩下都是参数"。
	commandNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

	mu        sync.RWMutex
	cmdTreeFn func() *cobra.Command
)

// RegisterCommandTree registers the callback that returns the rootCmd command tree.
// It is called from the cli package init to inject func(){ return rootCmd }. This
// breaks the cli ↔ taskpipeline cycle: this package does not import cli; taskpipeline
// imports this package to call DriftedInProject, and cli imports this package to
// register the callback.
//
// RegisterCommandTree 注册"获取 rootCmd 命令树"的回调。cli 包 init 调用，注入
// func(){ return rootCmd }。打破 cli ↔ taskpipeline 循环：本包不 import cli，
// taskpipeline import 本包调 DriftedInProject，cli import 本包注册回调。
func RegisterCommandTree(fn func() *cobra.Command) {
	mu.Lock()
	defer mu.Unlock()
	cmdTreeFn = fn
}

func commandTree() *cobra.Command {
	mu.RLock()
	defer mu.RUnlock()
	if cmdTreeFn == nil {
		return nil
	}
	return cmdTreeFn()
}

// findSub looks up a direct subcommand of parent by Name. cobra Commands() does not
// expand aliases, so docs should use the canonical command name (matching the first
// word of Use).
//
// findSub 在 parent 的直接子命令里按 Name 找。cobra Commands() 不展开别名，
// 故文档应使用 canonical 命令名（与 Use 首词一致）。
func findSub(parent *cobra.Command, name string) *cobra.Command {
	if parent == nil {
		return nil
	}
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}
	return nil
}

// ValidateForgePath validates level by level that the command path exists in the
// cobra tree. ref is the content **after** `forge ` inside the backticks (the
// `forge ` prefix is already stripped by forgeBacktickRef), so it walks down from
// rootCmd directly. It stops at the first non-command token (< / -- / [ / Chinese,
// etc.) — everything after is arguments or description. It returns the name of the
// first broken-link subcommand; an empty string means the path is complete (including
// the degenerate case where ref is empty). When the command tree is not registered
// (callback nil) it returns "" (pass through, no false report).
//
// ValidateForgePath 逐级验证命令路径在 cobra 树中存在。ref 是反引号内 "forge " **之后**
// 的内容（"forge " 前缀已由 forgeBacktickRef 剥离），故直接从 rootCmd 起逐级匹配。
// 遇到非命令 token（< / -- / [ / 中文 等）即停——后面都是参数或说明。
// 返回首个断链的子命令名；空串表示路径完整（含 ref 为空的退化情形）。
// 命令树未注册（回调 nil）时返回 ""（放行，不误报）。
func ValidateForgePath(ref string) string {
	cur := commandTree()
	if cur == nil {
		return ""
	}
	for _, p := range strings.Fields(ref) {
		if !commandNameRe.MatchString(p) {
			break
		}
		if sub := findSub(cur, p); sub != nil {
			cur = sub
		} else {
			return p
		}
	}
	return ""
}

// DriftedCommands scans doc text and returns all forge command paths (the part
// after `forge `, e.g. `experience propose`) that are referenced in backticks but
// absent from the command tree. Used by guard A and the task-complete advisory.
// Returns nil when the command tree is not registered (pass through).
//
// DriftedCommands 扫文档文本，返回所有反引号引用但命令树中不存在的 forge 命令路径
// （"forge " 之后的部分，如 "experience propose"）。守卫 A 和 task-complete advisory 用。
// 命令树未注册时返回 nil（放行）。
func DriftedCommands(doc string) []string {
	// Deduplicate: the same drift command reported N times in the doc is reported
	// once, to avoid the advisory stderr repeating `experience propose, experience propose`.
	//
	// 去重：同一 drift 命令在文档出现 N 次只报一次，避免 advisory stderr 重复刷
	// "experience propose, experience propose"。
	seen := make(map[string]bool)
	var drifted []string
	for _, m := range forgeBacktickRef.FindAllStringSubmatch(doc, -1) {
		if ValidateForgePath(m[1]) != "" && !seen[m[1]] {
			seen[m[1]] = true
			drifted = append(drifted, m[1])
		}
	}
	return drifted
}

// DriftedInProject scans the user project root README.md and returns forge command
// references that have drifted. Used by the task-complete gate advisory — it surfaces
// README references to non-existent forge commands before committing (earlier than
// the CI guard: reminded locally at complete time, no need to wait for push).
// Returns nil when there is no README or the command tree is not registered (silent,
// does not block the gate).
//
// DriftedInProject 扫用户项目根 README.md，返回 drift 的 forge 命令引用。
// task-complete 门禁 advisory 用——提交前发现 README 引用了不存在的 forge 命令
// （比 CI 守卫更早：本地 complete 时就提醒，不用等 push）。
// 无 README 或命令树未注册时返回 nil（静默，不阻塞 gate）。
func DriftedInProject(root string) []string {
	body, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		return nil
	}
	return DriftedCommands(string(body))
}

// AllFlags returns the sorted "command --flag" identifiers of every non-hidden
// flag on every non-hidden command in the tree (cobra's auto help flag exempt).
// Used by the ratchet guard: the test pins a grandfathered baseline, so any NEW
// flag must be documented in the README or consciously added to the baseline.
//
// AllFlags 返回树中所有非隐藏命令上的非隐藏 flag 的排序标识（"command --flag"
// 形式；cobra 自动 help flag 豁免）。供棘轮守卫使用：测试钉住一份豁免基线，
// 任何新增 flag 必须进 README 或被有意识地加进基线。
func AllFlags(root *cobra.Command) []string {
	if root == nil {
		return nil
	}
	seen := make(map[string]bool)
	var out []string
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c == nil {
			return
		}
		if !c.Hidden {
			add := func(f *pflag.Flag) {
				if f.Hidden || f.Name == "help" {
					return
				}
				id := c.Name() + " --" + f.Name
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			c.LocalFlags().VisitAll(add)
			c.PersistentFlags().VisitAll(add)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	slices.Sort(out)
	return out
}

// skillBacktickRef matches a kebab-case token wrapped in backticks — the skill-name
// analogue of forgeBacktickRef (which targets forge-command paths). The backtick
// delimiter excludes prose, and the lowercase-first kebab shape excludes CamelCase
// naming examples (MyCard). Built via rune(0x60) + raw string to dodge the Windows
// input-layer quote corruption that breaks double-quoted Go string literals.
//
// skillBacktickRef 匹配反引号包裹的 kebab-case token——forgeBacktickRef（针对 forge
// 命令路径）的 skill 名对应物。反引号限定排除散文，小写首字母 kebab 形态排除 CamelCase
// 命名示例（MyCard）。用 rune(0x60) + raw string 构造，绕过 Windows 输入层双引号腐蚀
// （会破坏双引号 Go 字符串字面量）。
var skillBacktickRef = regexp.MustCompile(string(rune(0x60)) + `([a-z][a-z0-9-]*)` + string(rune(0x60)))

// DanglingSkillRefs scans text for backtick-wrapped kebab tokens and returns the ones
// that look like broken skill references — referenced as if a skill but absent from the
// canonical skill set. Root-cause guard for the 2026-07 frontend dangling-link regression
// (frontend-development referenced frontend-stack-selection / ai-generated-ui-review /
// frontend-aesthetics-execution as skills that did not exist).
//
// Two-pass discrimination avoids the false-positive storm a naive "every backtick kebab
// must be a known skill" check would cause — canonical skill docs contain ~150 backtick
// kebab tokens that are NOT skills (tools grep/curl, keywords any/while, review-pattern
// names type-suppression, naming examples user-avatar-card, etc.):
//
//  1. Single-segment tokens (no hyphen) are exempt. Every canonical Forge skill name is
//     multi-segment kebab (enforced by the TestCanonicalSkillNamesAreMultiSegment
//     meta-guard); single-segment tokens in SKILL.md are overwhelmingly tools/keywords/
//     concept words, uncheckable without a huge deny-list. If a single-segment skill
//     name is ever added, the meta-guard fails and flags this exemption for review.
//  2. Multi-segment tokens: pass if in knownSkills (real skill) or allowlist
//     (human-confirmed non-skill — review-pattern names, sub-agent contracts, hook
//     names, tools, naming examples, external skills, pattern values, etc.); else
//     reported as dangling.
//
// The allowlist is the codification of the "which backtick tokens are NOT skill refs"
// audit knowledge (the false-positive categories from the 2026-07 frontend sweep). The
// caller owns it so this package stays free of a canonical-skills dependency.
//
// DanglingSkillRefs 扫文本反引号 kebab token，返回"疑似 skill 断链"——被当 skill 引用
// 但不在 canonical skill 集的 token。2026-07 frontend 断链回归的根治（frontend-development
// 引用了不存在的 frontend-stack-selection / ai-generated-ui-review / frontend-aesthetics-execution）。
//
// 两遍判别避免朴素"每个反引号 kebab 必须是已知 skill"检查的误报风暴——canonical skill
// 文档含 ~150 个非 skill 反引号 kebab token（工具 grep/curl、关键字 any/while、review 模式名
// type-suppression、命名示例 user-avatar-card 等）：
//
//  1. 单段 token（无连字符）豁免。Forge canonical skill 名全为多段 kebab（由
//     TestCanonicalSkillNamesAreMultiSegment meta 守卫保证）；SKILL.md 里单段 token 压倒性
//     是工具/关键字/概念词，无超大 deny-list 无法校验。若新增单段 skill 名，meta 守卫 fail
//     并提示复审此豁免。
//  2. 多段 token：在 knownSkills（真 skill）或 allowlist（人工确认非 skill——review 模式名/
//     子agent契约/hook名/工具/命名示例/外部skill/pattern值等）则通过；否则报断链。
//
// allowlist 是"哪些反引号 token 不是 skill 引用"的审计知识代码化（2026-07 frontend 梳理的
// false-positive 类别）。调用方拥有，本包不依赖 canonical-skills。
func DanglingSkillRefs(text string, knownSkills, allowlist map[string]bool) []string {
	seen := make(map[string]bool)
	var out []string
	for _, m := range skillBacktickRef.FindAllStringSubmatch(text, -1) {
		tok := m[1]
		// 单段豁免：canonical skill 全多段 kebab，单段 token 歧义不可校验。
		if !strings.Contains(tok, "-") {
			continue
		}
		if knownSkills[tok] || allowlist[tok] || seen[tok] {
			continue
		}
		seen[tok] = true
		out = append(out, tok)
	}
	return out
}
