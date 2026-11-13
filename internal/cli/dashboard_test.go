package cli

import (
	"strings"
	"testing"
)

// TestDashboardCommand_Wiring 钉住 dashboard 命令接线：分组、路径、flags、RunE。
// 防止新增命令漏设 GroupID（aa_groups_test 的 AllTopLevelGrouped 会兜底，这里显式钉）。
func TestDashboardCommand_Wiring(t *testing.T) {
	if dashboardCmd == nil {
		t.Fatal("dashboardCmd is nil — 包级 var 未初始化")
	}
	if dashboardCmd.GroupID != "quality" {
		t.Errorf("GroupID = %q, want quality", dashboardCmd.GroupID)
	}
	if got := dashboardCmd.CommandPath(); got != "forge dashboard" {
		t.Errorf("CommandPath = %q, want forge dashboard", got)
	}
	for _, f := range []string{"port", "no-open"} {
		if dashboardCmd.Flags().Lookup(f) == nil {
			t.Errorf("flag --%s not registered", f)
		}
	}
	if dashboardCmd.RunE == nil {
		t.Error("RunE is nil — 命令将静默无动作")
	}
}

func TestDashboardNoManagedProjectsMessage(t *testing.T) {
	if !strings.Contains(dashboardNoManagedProjects, "forge integrations enable") {
		t.Fatalf("提示未指向 registry 登记命令：%s", dashboardNoManagedProjects)
	}
	if strings.Contains(dashboardNoManagedProjects, "workspace add") {
		t.Fatalf("提示不得声称 workspace add 会登记全局注册表：%s", dashboardNoManagedProjects)
	}
}
