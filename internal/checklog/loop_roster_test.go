package checklog

import "testing"

// TestLoopExhaustedInRoster 钉住 loop-exhausted 进 roster（compat 面 2 单一真相
// 源；回边语义的升级记录与人工重置裁决两类行都走本名）。
func TestLoopExhaustedInRoster(t *testing.T) {
	if string(CheckLoopExhausted) != "loop-exhausted" {
		t.Fatalf("常量值漂移: %q", CheckLoopExhausted)
	}
	found := false
	for _, n := range AllCheckNames() {
		if n == "loop-exhausted" {
			found = true
		}
	}
	if !found {
		t.Fatal("AllCheckNames 缺 loop-exhausted——escape.go allCheckNames 未同步")
	}
}
