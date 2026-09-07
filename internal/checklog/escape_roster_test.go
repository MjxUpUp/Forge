package checklog

import "testing"

// TestArtifactChainChecksInRoster 钉住产物链两 CheckName 进 roster（compat 面 2
// 单一真相源）：漏登记 = checklog 条目落盘但 compat/看板看不到——面缩水。
func TestArtifactChainChecksInRoster(t *testing.T) {
	want := map[CheckName]string{
		CheckArtifactChain: "artifact-chain",
		CheckArtifactDrift: "artifact-drift",
	}
	for name, lit := range want {
		if string(name) != lit {
			t.Errorf("常量值漂移: %q != %q", name, lit)
		}
	}
	roster := AllCheckNames()
	seen := make(map[string]bool, len(roster))
	for _, n := range roster {
		seen[n] = true
	}
	for _, lit := range want {
		if !seen[lit] {
			t.Errorf("AllCheckNames 缺 %q——escape.go allCheckNames 未同步", lit)
		}
	}
}
