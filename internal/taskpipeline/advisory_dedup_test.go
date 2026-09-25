package taskpipeline

import (
	"testing"
)

// TestFilterUnreported pins the dedup helper's contract.
//
// TestFilterUnreported 钉住 finding 去重助手的契约：已报告指纹被过滤、新指纹追加进
// state.ReportedFindings（changed=true）、同批重复指纹只报一次、nil state 退化为不去重。
func TestFilterUnreported(t *testing.T) {
	state := &TaskState{TaskRef: "t1"}

	fresh, changed := filterUnreported(state, []string{"r|a.go:1", "r|a.go:2", "r|a.go:1"})
	if !changed {
		t.Fatal("首批应有新增")
	}
	if len(fresh) != 2 {
		t.Fatalf("同批重复指纹只报一次，fresh=%v", fresh)
	}
	if len(state.ReportedFindings) != 2 {
		t.Fatalf("新指纹应入集合，got %v", state.ReportedFindings)
	}

	// 第二轮：旧指纹全过滤、新指纹照报（finding 消失又出现新 finding 的场景）。
	fresh, changed = filterUnreported(state, []string{"r|a.go:1", "r|b.go:9"})
	if !changed || len(fresh) != 1 || fresh[0] != "r|b.go:9" {
		t.Fatalf("只应报新指纹 r|b.go:9，fresh=%v changed=%v", fresh, changed)
	}
	if len(state.ReportedFindings) != 3 {
		t.Fatalf("集合应累计 3 个指纹，got %v", state.ReportedFindings)
	}

	// 全部已报告：无新指纹、无变化。
	fresh, changed = filterUnreported(state, []string{"r|a.go:1", "r|a.go:2", "r|b.go:9"})
	if changed || len(fresh) != 0 {
		t.Fatalf("全已报告应无新增，fresh=%v changed=%v", fresh, changed)
	}

	// nil state：无从持久化，退化为全报。
	fresh, _ = filterUnreported(nil, []string{"x"})
	if len(fresh) != 1 {
		t.Fatalf("nil state 应退化为不去重，fresh=%v", fresh)
	}
}

// TestFindingKeys pins the fingerprint formats: cheat=rule|file:line; unused=rule|file:line|symbol (the symbol is the finding's identity, so a rename on the same line is not mis-suppressed).
//
// TestFindingKeys 钉住指纹格式：cheat=规则|文件：行；unused=规则|文件：行|符号（符号是
// finding 身份本体，防同行重命名被误抑制）。
func TestFindingKeys(t *testing.T) {
	c := CheatFinding{Pattern: CheatCommentOnly, File: "f.go", Line: 12}
	if got := cheatFindingKey(c); got != "comment-only-fix|f.go:12" {
		t.Fatalf("cheat 指纹 %q", got)
	}
	u := UnusedFinding{Pattern: UnusedExport, File: "g.go", Line: 3, Symbol: "Translate"}
	if got := unusedFindingKey(u); got != "unreferenced-export|g.go:3|Translate" {
		t.Fatalf("unused 指纹 %q", got)
	}
}
