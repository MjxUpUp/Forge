package checklog

import (
	"os"
	"strings"
	"testing"
)

// TestNoHistoricalCheckNames 守住 compat checks 面：已无生产者的历史检查名
// 不得回到常量、roster 或 evidence 白名单——它们只记录已删除命令族的历史行。
func TestNoHistoricalCheckNames(t *testing.T) {
	historical := []string{
		"eval-metrics-incomplete", "eval-golden-run", "eval-golden-rotate",
		"eval-judge-weak", "eval-traps-run", "eval-run", "eval-decompose",
		"eval-resume-drill", "eval-audit-forged", "eval-preflight",
		"eval-golden-test-query", "eval-wedge-drill",
		"project-sync", "attribution", "sync-version-skew",
		"decision-warrant", "decision-evidence", "task-guard",
	}
	src, err := os.ReadFile("types.go")
	if err != nil {
		t.Fatal(err)
	}
	roster := map[string]bool{}
	for _, n := range AllCheckNames() {
		roster[n] = true
	}
	for _, name := range historical {
		if strings.Contains(string(src), `"`+name+`"`) {
			t.Errorf("types.go 仍含历史检查名 %q", name)
		}
		if roster[name] {
			t.Errorf("AllCheckNames roster 仍含历史检查名 %q", name)
		}
		if verificationChecks[CheckName(name)] {
			t.Errorf("evidence 白名单仍含历史检查名 %q", name)
		}
	}
}
