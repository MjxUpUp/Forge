package cli

import (
	"testing"

	"github.com/spf13/cobra"
)

// TestCommandGroups 钉住 aa_groups.go 的温和分组契约。
//
// 根因回归：mcpCmd 曾经是「包级 nil var + init 内赋值」，而 aa_groups.go 的 init
// 因文件名 aa_ 最先执行——解引用尚未赋值的 mcpCmd 触发 nil panic，整个 forge 二进制
// init 崩溃（连 forge --help 都跑不到）。修复是让 mcpCmd 成为包级 var 字面量，
// 与其余 19 个命令一致，在所有 init 之前初始化完成。
//
// 本测试二进制启动时 aa_groups.init 同样会跑：若有人把任一 xxxCmd 改回 init 内赋值，
// 测试二进制 init 直接 panic 崩溃（比断言更强的保护）。下面的显式断言让契约可读：
// 每个被 aa_groups 引用的命令变量必须非 nil、GroupID 已设、CommandPath 不变。
func TestCommandGroups(t *testing.T) {
	// 5 个职能组全部注册，标题正确。
	wantGroups := []string{"项目生命周期", "项目管道", "任务质量", "经验与治理", "集成与安全"}
	gotGroups := make([]string, 0, len(wantGroups))
	for _, g := range rootCmd.Groups() {
		gotGroups = append(gotGroups, g.Title)
	}
	for _, want := range wantGroups {
		found := false
		for _, got := range gotGroups {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("group %q 未注册，实际 groups: %v", want, gotGroups)
		}
	}

	// 每组抽查一个命令：变量非 nil + GroupID 正确 + 命令路径不变（温和分组的承诺）。
	// mcpCmd 是曾经的 panic 源，必须钉。
	cases := []struct {
		name     string
		cmd      *cobra.Command
		group    string
		wantPath string
	}{
		{"mcp", mcpCmd, "integrate", "forge mcp"},
		{"init", initCmd, "lifecycle", "forge init"},
		{"status", statusCmd, "pipeline", "forge status"},
		{"task", taskCmd, "quality", "forge task"},
		{"skills", skillsCmd, "governance", "forge skills"},
		{"hazard", hazardCmd, "integrate", "forge hazard"},
	}
	for _, c := range cases {
		if c.cmd == nil {
			t.Fatalf("%sCmd is nil — 包级 var 未初始化，aa_groups.init 会解引用 nil panic", c.name)
		}
		if c.cmd.GroupID != c.group {
			t.Errorf("%sCmd.GroupID = %q, want %q", c.name, c.cmd.GroupID, c.group)
		}
		if got := c.cmd.CommandPath(); got != c.wantPath {
			t.Errorf("%sCmd path = %q, want %q (温和分组不改路径)", c.name, got, c.wantPath)
		}
	}
}

// TestCommandGroups_AllTopLevelGrouped 除 cobra 自动生成的 completion/help 外，
// 所有顶层命令都归入某个职能组——防止新增命令漏设 GroupID（会显示在 help 末尾游离）。
func TestCommandGroups_AllTopLevelGrouped(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		name := c.Name()
		// completion/help 是 cobra 自动生成的辅助命令，刻意留默认不分组。
		if name == "completion" || name == "help" {
			if c.GroupID != "" {
				t.Errorf("自动命令 %s 不应设 GroupID，got %q", name, c.GroupID)
			}
			continue
		}
		if c.GroupID == "" {
			t.Errorf("顶层命令 %s 未归组（GroupID 空），会在 forge --help 游离显示", name)
		}
		// GroupID 必须是已注册的组。
		valid := false
		for _, g := range rootCmd.Groups() {
			if g.ID == c.GroupID {
				valid = true
				break
			}
		}
		if !valid {
			t.Errorf("命令 %s 的 GroupID %q 未在 rootCmd.AddGroup 注册，AddCommand 会 panic", name, c.GroupID)
		}
	}
}
