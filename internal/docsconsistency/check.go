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
	// forgeBacktickRef 匹配反引号内的 forge 命令引用，如 `forge experience accept`。
	// 反引号限定把散文里的 "forge 是…" 排除在外，大幅减 false positive。
	forgeBacktickRef = regexp.MustCompile("`forge ([^`]+)`")

	// commandNameRe 描述合法 cobra 命令名（Use 的首词）。非命令 token——占位符 <id>、
	// flag --force、方括号 [--mode]、分隔符 small|medium、中文说明——一律不匹配，
	// 用于在逐级验证时判定"命令路径到此结束，剩下都是参数"。
	commandNameRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`)

	mu        sync.RWMutex
	cmdTreeFn func() *cobra.Command
)

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

// DriftedCommands 扫文档文本，返回所有反引号引用但命令树中不存在的 forge 命令路径
// （"forge " 之后的部分，如 "experience propose"）。守卫 A 和 task-complete advisory 用。
// 命令树未注册时返回 nil（放行）。
func DriftedCommands(doc string) []string {
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
