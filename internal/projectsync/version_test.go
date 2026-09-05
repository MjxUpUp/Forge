package projectsync

// version_test.go — bundle 版本偏移感知的三态判定（同版/前向/后向/缺字段）。

import (
	"strings"
	"testing"
)

func TestVersionSkew(t *testing.T) {
	tests := []struct {
		local, bundle string
		wantWarn      bool
	}{
		{"1.50.0", "1.50.0", false},   // 同版
		{"1.50.0", "1.51.0", true},    // 前向偏移：本机旧、是裁剪风险方
		{"1.51.0", "1.50.0", false},   // 后向：legacy 兜底与惰性重推导覆盖
		{"1.50.0", "", false},         // 早期 bundle 无版本——无从比较
		{"", "1.50.0", false},         // 本机版本未知（dev 构建）——fail-open
		{"1.50.0", "1.50.1", true},    // patch 级前向同样裁剪风险
		{"1.50.0", "2.0.0", true},     // major 前向
		{"1.50.0", "1.49.9", false},   // 明确后向
	}
	for _, tt := range tests {
		got := VersionSkew(tt.local, tt.bundle)
		if (got != "") != tt.wantWarn {
			t.Fatalf("VersionSkew(%q,%q)=%q，wantWarn=%v", tt.local, tt.bundle, got, tt.wantWarn)
		}
		if tt.wantWarn && !strings.Contains(got, "静默裁剪") {
			t.Fatalf("警示文案应说明裁剪风险: %q", got)
		}
	}
}
