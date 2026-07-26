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
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

var (
	// forgeBacktickRef matches forge command references inside backticks, such as
	// `forge experience accept`. The backtick delimiter excludes prose like
	// `forge 是...` from matching, sharply reducing false positives.
	//
	// forgeBacktickRef 匹配反引号内的 forge 命令引用，如 `forge experience accept`。
	// 反引号限定把散文里的 "forge 是…" 排除在外，大幅减 false positive。
	forgeBacktickRef = regexp.MustCompile("`forge ([^`]+)`")

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
