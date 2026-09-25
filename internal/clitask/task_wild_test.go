package clitask

// task_wild_test.go —— `forge task wild` 的单元面:说明必填契约与限额常量
// (AC-3(10) 补强,escape-hatch-hardening P2)。落盘/计数/checklog 的端到端
// 契约在 internal/cli/next_wild_test.go(经 RunTaskWild 实跑)。

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestRunTaskWildRequiresNote(t *testing.T) {
	for _, empty := range []string{"", "   ", "\t"} {
		err := RunTaskWild(&cobra.Command{}, []string{empty})
		if err == nil || !strings.Contains(err.Error(), "必须带一句说明") {
			t.Errorf("note %q must be rejected with the required-note error, got %v", empty, err)
		}
	}
}

func TestWildQuotaConstants(t *testing.T) {
	if wildQuotaPerSession != 3 {
		t.Errorf("wildQuotaPerSession = %d, want 3 (AC-3(10) 条件限定面,spec P2 契约)", wildQuotaPerSession)
	}
	if got := truncRunes("一二三四五", 3); got != "一二三…" {
		t.Errorf("truncRunes rune-safe truncation = %q", got)
	}
	if got := truncRunes("短", 3); got != "短" {
		t.Errorf("truncRunes must not extend short notes, got %q", got)
	}
}
