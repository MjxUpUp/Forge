package attribution

import (
	"testing"
	"time"
)

// TestView_Foreign 钉住「外来」的判定三条件：last-writer 是别人、那个会话仍存活
// （window 内有任意事件）、本会话从未碰过该路径。任一不满足都不算外来。
func TestView_Foreign(t *testing.T) {
	now := time.Now()
	v := &View{
		LastWriter: map[string]Event{
			"own.go":    {Sid: "sess-a", Path: "own.go", Ts: now.Add(-time.Minute)},
			"other.go":  {Sid: "sess-b", Path: "other.go", Ts: now.Add(-2 * time.Minute)},
			"dead.go":   {Sid: "sess-dead", Path: "dead.go", Ts: now.Add(-3 * time.Minute)},
			"nobody.go": {Path: "nobody.go", Ts: now.Add(-time.Minute)}, // 无 sid 的事件不构成归属
			"shared.go": {Sid: "sess-b", Path: "shared.go", Ts: now.Add(-time.Minute)},
		},
		// 双条件的第二半：本会话历史上碰过 shared.go（后被 sess-b 续写），必须留在作用域。
		Touched: map[string]map[string]bool{"sess-a": {"shared.go": true}},
		// 存活是会话属性：sess-b 仍在写（1 分钟前），sess-dead 两小时没有动静。
		LastSeen: map[string]time.Time{
			"sess-a":    now.Add(-time.Minute),
			"sess-b":    now.Add(-time.Minute),
			"sess-dead": now.Add(-2 * time.Hour),
		},
	}

	got := v.Foreign("sess-a", DefaultForeignWindow)
	if len(got) != 1 || got[0] != "other.go" {
		t.Fatalf("应只认「他会话 + 存活 + 本会话从未碰过」的 other.go，得 %v", got)
	}

	// 无主体：不排除任何东西（整树语义）
	if got := v.Foreign("", DefaultForeignWindow); got != nil {
		t.Errorf("sid 为空应返回 nil，得 %v", got)
	}
	// 窗口 ≤0：关闭存活过滤，死会话的路径也算外来（调用方显式要求时不做过期判断）
	if got := v.Foreign("sess-a", 0); len(got) != 2 {
		t.Errorf("window=0 应关闭存活过滤（other.go, dead.go），得 %v", got)
	}
	// nil View 不 panic
	var nilView *View
	if got := nilView.Foreign("sess-a", DefaultForeignWindow); got != nil {
		t.Errorf("nil View 应返回 nil，得 %v", got)
	}
}

// TestView_Foreign_LivenessIsPerSession：邻居改别的文件不代表它「走了」——它在
// window 内还有任意事件时，它更早写过的文件必须继续被排除；否则长期并行的邻居
// 会像事故那样反复触发本会话的门禁。
func TestView_Foreign_LivenessIsPerSession(t *testing.T) {
	now := time.Now()
	v := &View{
		LastWriter: map[string]Event{
			"old.go":   {Sid: "sess-b", Path: "old.go", Ts: now.Add(-45 * time.Minute)},
			"fresh.go": {Sid: "sess-b", Path: "fresh.go", Ts: now.Add(-time.Minute)},
		},
		LastSeen: map[string]time.Time{"sess-b": now.Add(-time.Minute)},
	}
	got := v.Foreign("sess-a", DefaultForeignWindow)
	if len(got) != 2 {
		t.Fatalf("sess-b 仍存活，它 45 分钟前写的 old.go 也应算外来，得 %v", got)
	}
}

// TestEvent_Confidence：写/编辑观测是宿主直接事实（high），bash 推断是启发式（low）。
// 这条断言钉住「推断不可与事实同权」的可读契约——消费方按 Confidence 降权时不能把
// 两类混为一谈。
func TestEvent_Confidence(t *testing.T) {
	if got := (Event{Kind: KindBashInfer}).Confidence(); got != "low" {
		t.Fatalf("bash-infer 的可信度应为 low，得 %q", got)
	}
	for _, k := range []Kind{KindWrite, KindEdit, Kind("")} {
		if got := (Event{Kind: k}).Confidence(); got != "high" {
			t.Fatalf("kind %q 的可信度应为 high，得 %q", k, got)
		}
	}
}
