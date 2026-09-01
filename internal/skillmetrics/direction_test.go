package skillmetrics

import (
	"os/exec"
	"strings"
	"testing"
)

// TestPackageLeaf pins the architectural contract of the 2026-09 census A4
// split: skillmetrics (usage observability) must never depend on skillseval
// (eval-case machinery). The allowed direction is skillseval → skillmetrics
// (mine.go consumes EngagedAfter); a reverse import would close the cycle and
// re-fuse the two consumer groups the split separated.
//
// TestPackageLeaf 钉住 2026-09 普查 A4 拆分的架构契约：skillmetrics（使用度量）
// 永不依赖 skillseval（eval 案例机器）。合法方向是 skillseval → skillmetrics
//（mine.go 消费 EngagedAfter）；反向 import 会闭合依赖环，把拆开的两批消费方
// 重新焊死。
func TestPackageLeaf(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").Output()
	if err != nil {
		t.Skipf("go list unavailable in test environment: %v", err)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasSuffix(dep, "/internal/skillseval") {
			t.Errorf("skillmetrics must not depend on skillseval, but go list -deps contains %s", dep)
		}
	}
}
