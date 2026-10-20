package taskpipeline

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TestIsLoopResetEntry 钉住 reset-loop 审计行的识别口径：只认 ResetLoopExhaustion
// 真实落下的行（经 checklog 读回，不手写形态）；同 check 的耗尽拦截行、带
// "reset-loop" 字样的其他 check 行、缺前缀的通过行一律不认。
func TestIsLoopResetEntry(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	state := &TaskState{TaskRef: "loop-reset-entry", Branch: "feat/loop"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	if err := ResetLoopExhaustion(dir, state, "人工裁决"); err != nil {
		t.Fatal(err)
	}
	entries, err := checklog.LoadForTask(dir, state.TaskRef)
	if err != nil {
		t.Fatal(err)
	}
	var produced *checklog.Entry
	for i := range entries {
		if entries[i].Check == checklog.CheckLoopExhausted {
			produced = &entries[i]
		}
	}
	if produced == nil {
		t.Fatal("ResetLoopExhaustion 应落 loop-exhausted 审计行")
	}

	cases := []struct {
		name string
		e    *checklog.Entry
		want bool
	}{
		{"真实 reset 行", produced, true},
		{"耗尽拦截行（Passed=false）", &checklog.Entry{Check: checklog.CheckLoopExhausted, Passed: false,
			Detail: LoopResetDetailPrefix + "x"}, false},
		{"其他 check 带 reset-loop 字样", &checklog.Entry{Check: checklog.CheckEscapeHatch, Passed: true,
			Detail: "escape-hatch: reset-loop"}, false},
		{"同 check 通过但无前缀", &checklog.Entry{Check: checklog.CheckLoopExhausted, Passed: true,
			Detail: "loop exhausted (rounds-exhausted): x"}, false},
		{"nil", nil, false},
	}
	for _, c := range cases {
		if got := IsLoopResetEntry(c.e); got != c.want {
			t.Errorf("%s: IsLoopResetEntry = %v, want %v", c.name, got, c.want)
		}
	}
}
