package cli

import "testing"

// TestCoreOnlyCommandSurface 钉住 core-only 的删除面：这些命令组在
// 1.88/3.0 之后已经只剩历史意义（宿主接线、评分、多仓、全局看板），
// 留着就是一个教不会、修不动、永远无人调用的入口。
//
// 断言的是「root 下没有这些名字」，不是「help 文本里没有」——后者可以靠
// 措辞绕过，前者是命令树的实际形状。
func TestCoreOnlyCommandSurface(t *testing.T) {
	removed := []string{
		"act", "bridge", "config", "conventions", "dashboard", "enforcement", "eval",
		"gate", "harness", "health", "migrate", "node", "policy", "project",
		"registry", "skill", "skills", "trace", "trust", "update", "verify",
		"workspace", "worktree",
	}
	present := map[string]bool{}
	for _, c := range rootCmd.Commands() {
		present[c.Name()] = true
	}
	for _, name := range removed {
		if present[name] {
			t.Errorf("命令 %q 应已在 core-only 中删除，但仍在命令树上", name)
		}
	}
	for _, name := range []string{"init", "on", "off", "version", "next", "status", "change", "task", "review", "docs", "impact", "test", "compat"} {
		if !present[name] {
			t.Errorf("核心命令 %q 不见了", name)
		}
	}
}
