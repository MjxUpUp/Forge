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
		observed, blocked int
		want              string
	}{
		{10, 0, "insufficient-data"},
		{29, 0, "insufficient-data"},
		{30, 0, "dead-candidate"},
		{100, 0, "dead-candidate"},
		{100, 3, "live"},
		{5, 1, "live"},
	}
	for _, tc := range cases {
		if got := classifyDeadCheck(tc.observed, tc.blocked); got != tc.want {
			t.Errorf("classifyDeadCheck(%d, %d) = %q, want %q", tc.observed, tc.blocked, got, tc.want)
		}
	}
}
