package taskpipeline

import (
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// redteam_test.go — L6 红队演练行为钉（配对 redteam.go）：五类雷全拦截 +
// 汇总行落盘（escaped 时如实 warn——本测试钉的是当前全拦契约；链回归引入
// 洞时本测试即红，这正是红队作为回归钉的形态）。

// TestRunRedteamDrills_AllIntercepted: the fixed seed list must be fully
// intercepted by the chain — an escape here IS a chain regression (or a seed
// that no longer matches the documented contract).
//
// TestRunRedteamDrills_AllIntercepted：固定雷场必须被链全拦——此处逃逸即链
// 回归（或种子不再匹配已文档化契约）。
func TestRunRedteamDrills_AllIntercepted(t *testing.T) {
	if testing.Short() {
		t.Skip("红队演练需要 git/go 工具链")
	}
	dir := t.TempDir()
	rep, err := RunRedteamDrills(dir)
	if err != nil {
		t.Fatalf(`RunRedteamDrills: %v`, err)
	}
	if rep.Intercepted != rep.Total {
		t.Fatalf("红队应全拦：拦截 %d/%d——escaped: %v\n%s", rep.Intercepted, rep.Total, rep.Escaped, FormatRedteamReport(rep))
	}
	if len(rep.Rows) != rep.Total {
		t.Errorf(`每颗雷一行结果，got %d`, len(rep.Rows))
	}
	// 汇总行落盘（deterministic 源、全拦 pass）。
	entries, _ := checklog.LoadForTask(dir, "redteam")
	found := false
	for _, e := range entries {
		if e.Check == CheckNameRedteam && e.Passed {
			found = true
		}
	}
	if !found {
		t.Error(`应落 redteam-drill 全拦 pass 行`)
	}
	if text := FormatRedteamReport(rep); !strings.Contains(text, "拦截率 5/5") {
		t.Errorf("报告应含拦截率 5/5:\n%s", text)
	}
}
