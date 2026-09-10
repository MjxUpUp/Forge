package hookdispatch

import "testing"

// 归因探针（docs/design/harness-fixes-a-g-2026-09.md E.1/E.2）：每条 hook 产出的 checklog 行
// 记录 active task 是经哪条路径解析到的，以及该任务此刻是否已封印——下轮审计从猜测变数据。
func TestAttributionMeta(t *testing.T) {
	if m := attributionMeta("", false); m != nil {
		t.Fatalf("no active task: meta must be nil, got %v", m)
	}
	m := attributionMeta("active-file", false)
	if m["resolve_path"] != "active-file" {
		t.Fatalf("resolve_path = %q", m["resolve_path"])
	}
	if _, ok := m["post_seal"]; ok {
		t.Fatalf("unsealed task must not carry post_seal: %v", m)
	}
	m = attributionMeta("legacy", true)
	if m["resolve_path"] != "legacy" || m["post_seal"] != "true" {
		t.Fatalf("sealed legacy resolution meta = %v", m)
	}
}
