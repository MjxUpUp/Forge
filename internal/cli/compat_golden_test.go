package cli

// compat_golden_test.go — compat golden 棘轮守卫（mechanism-hardening P1-1）：
// 当前实算的六面快照必须与入库的 compat.snapshot.json 逐字节一致。任何面变更
// （新命令/删检查/逃生舱增减/载荷变化/schema 键增删/阻断位点增减）必须显式
// `forge compat snapshot` 重钉——"棘轮基线"哲学与 flag 文档守卫同源。

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/compat"
)

func TestCompatSnapshotMatchesGolden(t *testing.T) {
	root := repoRoot
	goldenPath := filepath.Join(root, "compat.snapshot.json")
	golden, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("golden 快照缺失——先 go run ./cmd/forge compat snapshot 并提交: %v", err)
	}
	snap, err := compat.BuildSnapshot(root, rootCmd)
	if err != nil {
		t.Fatalf("实算失败: %v", err)
	}
	body, err := snap.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(golden) != string(body)+"\n" && string(golden) != string(body) {
		t.Fatalf("六面快照与入库 golden 不一致——面已变更。处置：审阅差异后 go run ./cmd/forge compat snapshot 重钉并提交（破坏性变更须过 forge compat report 与 docs/design/compat-commitments.md 预告流程）")
	}
}
