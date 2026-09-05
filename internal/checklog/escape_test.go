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
