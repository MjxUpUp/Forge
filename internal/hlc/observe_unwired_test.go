package hlc

// observe_unwired_test.go — pins the sync-convergence §3 实现校正 (2026-08-21):
// Clock.Observe (the HLC recv rule) is implemented and unit-tested but UNWIRED —
// no production caller. ts_hlc is a display/reserved field; decisive keys today
// are fencing + canonical bytes. Wiring Observe is a deliberate act that must
// update the design doc and add clock persistence in the same change — this guard
// makes "silently start calling Observe" fail loudly instead.
//
// observe_unwired_test.go —— 钉住 sync-convergence §3 实现校正（2026-08-21）：
// Clock.Observe（HLC recv 规则）已实现、有单测，但未接线——无生产调用方。ts_hlc
// 是展示/预留字段；今日决胜键是 fencing + 规范字节。接线 Observe 是一个必须同步
// 更新设计文档并补时钟持久化的自觉动作——本守卫让「悄悄开始调 Observe」响亮失败。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestObserve_RemainsUnwiredUntilCrossProcessConsumer(t *testing.T) {
	root := mustRepoRoot(t)
	var offenders []string
	for _, dir := range []string{filepath.Join(root, "internal"), filepath.Join(root, "cmd")} {
		filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.Dir(path) == filepath.Join(root, "internal", "hlc") {
				return nil // the implementation itself
			}
			raw, rerr := os.ReadFile(path)
			if rerr != nil {
				return nil
			}
			if strings.Contains(string(raw), `.Observe(`) {
				offenders = append(offenders, path)
			}
			return nil
		})
	}
	if len(offenders) > 0 {
		t.Fatalf("Clock.Observe 有了生产调用方但设计文档（sync-convergence §3 实现校正）仍声明未接线：\n%s\n接线须同步：更新 §3 校正、补时钟持久化、删除本守卫", strings.Join(offenders, "\n"))
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, serr := os.Stat(filepath.Join(dir, "go.mod")); serr == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("repo root (go.mod) not found")
	return ""
}
