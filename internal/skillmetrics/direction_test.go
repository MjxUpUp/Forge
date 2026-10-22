package skillmetrics

import (
	"os/exec"
	"strings"
	"testing"
)

// TestPackageLeaf pins the architectural contract of the 2026-09 census A4
// split: skillmetrics (usage observability) must never depend on skillseval
// (eval-case machinery). skillseval may import skillmetrics, never the reverse;
// a reverse import would re-fuse the two consumer groups the split separated
// (skillseval currently imports nothing from skillmetrics — its former consumer
// mine.go was deleted with `skills mine`).
//
// TestPackageLeaf 钉住 2026-09 普查 A4 拆分的架构契约：skillmetrics（使用度量）
// 永不依赖 skillseval（eval 案例机器）。只允许 skillseval → skillmetrics 方向
// （当前 skillseval 已无 import——原消费方 mine.go 随 `skills mine` 删除）；反向
// import 会把拆开的两批消费方重新焊死。
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
