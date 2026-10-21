package checklog

import "testing"

// TestAcceptanceRecurrenceRoster 钉住 acceptance-recurrence 的两条承诺
// （loop 契约 B1-1）：它在 CheckName roster 里（compat 面 2 可见），但不在
// evidence 白名单——它是给外层循环的停止信号，不得被读作正向验证证据。
func TestAcceptanceRecurrenceRoster(t *testing.T) {
	found := false
	for _, n := range AllCheckNames() {
		if CheckName(n) == CheckAcceptanceRecurrence {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("%s 应在 AllCheckNames roster 里", CheckAcceptanceRecurrence)
	}
	if verificationChecks[CheckAcceptanceRecurrence] {
		t.Fatalf("%s 不得进 evidence 白名单（advisory 停止信号不是验证证据）", CheckAcceptanceRecurrence)
	}
	if string(CheckAcceptanceRecurrence) != "acceptance-recurrence" {
		t.Fatalf("检查名字面量漂移: %q", CheckAcceptanceRecurrence)
	}
}
