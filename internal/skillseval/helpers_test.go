package skillseval

import "testing"

// mustWrite 把 (t, err) 收口为 Fatal——与 internal/skillmetrics 测试的同名助手
// 同实现（测试助手无法跨包共享，注释互指防漂移）。
func mustWrite(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
