package cli

import "testing"

// TestParseDeadCheckWindow：观察窗解析——默认 90d、天数/时长/非法三态。
// 非法输入 fail-closed（BLOCKED），不静默回落。
func TestParseDeadCheckWindow(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{in: "", wantErr: false},
		{in: "90d", wantErr: false},
		{in: "2160h", wantErr: false},
		{in: "abc", wantErr: true},
		{in: "-5d", wantErr: true},
		{in: "0d", wantErr: true},
	}
	for _, tc := range cases {
		d, err := parseDeadCheckWindow(tc.in)
		if tc.wantErr && err == nil {
			t.Errorf("parseDeadCheckWindow(%q) 应报错，got %v", tc.in, d)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("parseDeadCheckWindow(%q) 不应报错，got %v", tc.in, err)
		}
	}
	if d, _ := parseDeadCheckWindow(""); d != 90*24*3600*1e9 {
		t.Errorf("空窗应为默认 90d，got %v", d)
	}
}

// TestClassifyDeadCheck：三态判定——观察不足 → insufficient-data（绝不把无数据
// 包装成「已证死」）；观察充足且零拦截 → dead-candidate；有拦截 → live。
func TestClassifyDeadCheck(t *testing.T) {
	cases := []struct {
		kind              string
		observed, blocked int
		want              string
	}{
		{kind: "blocking", observed: 10, blocked: 0, want: "insufficient-data"},
		{kind: "blocking", observed: 29, blocked: 0, want: "insufficient-data"},
		{kind: "blocking", observed: 30, blocked: 0, want: "dead-candidate"},
		{kind: "blocking", observed: 100, blocked: 0, want: "dead-candidate"},
		{kind: "blocking", observed: 100, blocked: 3, want: "live"},
		{kind: "blocking", observed: 5, blocked: 1, want: "live"},
		{kind: "advisory", observed: 100, blocked: 0, want: "advisory-pass-only"},
		{kind: "advisory", observed: 100, blocked: 2, want: "advisory-signal"},
		{kind: "advisory", observed: 10, blocked: 0, want: "insufficient-data"},
		{kind: "gate", observed: 100, blocked: 0, want: "gate-healthy"},
		{kind: "gate", observed: 100, blocked: 3, want: "live"},
		{kind: "pipeline", observed: 100, blocked: 9, want: "pipeline-marker"},
	}
	for _, tc := range cases {
		if got := classifyDeadCheck(tc.kind, tc.observed, tc.blocked); got != tc.want {
			t.Errorf("classifyDeadCheck(%q, %d, %d) = %q, want %q", tc.kind, tc.observed, tc.blocked, got, tc.want)
		}
	}
}

// TestClassifyCheckKind：类别映射的锚点抽查——review 文档
// docs/surveys/w0-dead-checks-review.md 的三类代表 + 未列名默认 blocking。
func TestClassifyCheckKind(t *testing.T) {
	for check, want := range map[string]string{
		"skill-trigger":         "advisory",
		"auto-compile":          "advisory",
		"review-pass":           "pipeline",
		"task-started":          "pipeline",
		"task-verify":           "gate",
		"task-complete":         "gate",
		"hazard-guard":          "blocking",
		"cheat-scan":            "blocking",
		"some-future-new-check": "blocking",
	} {
		if got := classifyCheckKind(check); got != want {
			t.Errorf("classifyCheckKind(%q) = %q, want %q", check, got, want)
		}
	}
}

// TestClassifyCheckKind_MachineQuestioning (oracle-pipeline L2)：三个新检查
// 都是 advisory 类——零拦截不是死证据（独立命令按需执行）。
func TestClassifyCheckKind_MachineQuestioning(t *testing.T) {
	for _, name := range []string{"mutation-sampling", "fuzz-run", "edge-checklist"} {
		if got := classifyCheckKind(name); got != "advisory" {
			t.Errorf(`%s 应分类 advisory，got %q`, name, got)
		}
	}
}

// TestClassifyCheckKind_DeliveryHardening（墙价硬批次）：三个新检查的分类
// 钉——hazard-pending/mutation-gate 是 gate（advisory-default，拦截有意义），
// self-review 是 advisory（披露事件）。
func TestClassifyCheckKind_DeliveryHardening(t *testing.T) {
	for name, want := range map[string]string{
		"hazard-pending": "gate", "mutation-gate": "gate", "self-review": "advisory",
	} {
		if got := classifyCheckKind(name); got != want {
			t.Errorf("%s 应分类 %s，got %q", name, want, got)
		}
	}
}
