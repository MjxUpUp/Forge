package checklog

// escape_test.go — 逃生舱行构造/提取的往返测试 + 旧行（无 Meta）的兜底语义。

import (
	"strings"
	"testing"
)

func TestEscapeHatchEntryRoundTrip(t *testing.T) {
	row := EscapeHatchEntry("doc-gate", EscapeReasonTimebox, "feat/x",
		"escape-hatch: doc gate bypassed")
	if row.Check != CheckEscapeHatch || row.Level != LevelWarn || !row.Passed {
		t.Fatalf("行基本字段不符: %+v", row)
	}
	if EscapeGateOf(row) != "doc-gate" || EscapeOwnerOf(row) != "feat/x" || EscapeReasonOf(row) != EscapeReasonTimebox {
		t.Fatalf("提取不符: gate=%q owner=%q reason=%q", EscapeGateOf(row), EscapeOwnerOf(row), EscapeReasonOf(row))
	}
	if !strings.Contains(row.Detail, "escape-hatch") {
		t.Fatalf("detail 保持散文契约（下游 Detail 消费方兼容）: %q", row.Detail)
	}
}

// TestEscapeLegacyRowFallback 旧行（v1 无 Meta）的提取兜底：gate/owner 空、
// reason=unspecified——聚合侧不误判、区分"没记"与"未声明"。
func TestEscapeLegacyRowFallback(t *testing.T) {
	legacy := &Entry{Check: CheckEscapeHatch, Passed: true, Detail: "escape-hatch: old form"}
	if EscapeGateOf(legacy) != "" || EscapeOwnerOf(legacy) != "" {
		t.Fatal("旧行 gate/owner 应空")
	}
	if EscapeReasonOf(legacy) != EscapeReasonUnspecified {
		t.Fatalf("旧行 reason 应 unspecified: %q", EscapeReasonOf(legacy))
	}
	if EscapeGateOf(nil) != "" || EscapeReasonOf(nil) != EscapeReasonUnspecified {
		t.Fatal("nil 行兜底")
	}
}

// TestWedgeDrillInRoster 钉住 eval-wedge-drill 的 roster 注册（compat 面 2 承诺面）：
// 常量值与 AllCheckNames 清单双向一致且不重复——新增 CheckName 漏进 roster 时
// compat 的源对照 guard 会红，本测试在同一目录把契约钉住（测试伴随变更纪律）。
func TestWedgeDrillInRoster(t *testing.T) {
	if CheckWedgeDrill != "eval-wedge-drill" {
		t.Fatalf("CheckWedgeDrill = %q, want %q", CheckWedgeDrill, "eval-wedge-drill")
	}
	found := 0
	for _, name := range AllCheckNames() {
		if name == string(CheckWedgeDrill) {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("eval-wedge-drill 在 AllCheckNames 中出现 %d 次, want 1（漏注 or 重复注册）", found)
	}
}
