package experience

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestResolveReview_MandatoryRequiresReason 钉死 dogfood 2.2：mandatory review resolve
// 必须填 reason。dogfood 实测 resolve(237) > accept(142)，resolve 成零成本绕过经验闭环
// 的常规动作，review 空转、知识库不增长。空 reason 对 mandatory 报错；非 mandatory 或
// 带 reason 正常；reason 写入 ResolutionNote 留审计。
func TestResolveReview_MandatoryRequiresReason(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	mand := &ReviewRequest{
		TaskRef:   "low/mand",
		Score:     55,
		Grade:     "F",
		Mandatory: true,
		Status:    ReviewPending,
		CreatedAt: time.Now(),
	}
	if err := SaveReview(root, mand); err != nil {
		t.Fatal(err)
	}
	// 空 reason → mandatory 报错（防零成本绕过）
	if err := ResolveReview(root, "low/mand", ""); err == nil {
		t.Fatal("mandatory resolve with empty reason must error")
	}
	// 带 reason → 成功 + 审计字段写入
	const reason = "已成约人工确认，无新规则可沉淀"
	if err := ResolveReview(root, "low/mand", reason); err != nil {
		t.Fatalf("resolve with reason: %v", err)
	}
	loaded, err := LoadReview(root, "low/mand")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != ReviewResolved {
		t.Errorf("status = %s, want resolved", loaded.Status)
	}
	if loaded.ResolutionNote != reason {
		t.Errorf("ResolutionNote = %q, want %q", loaded.ResolutionNote, reason)
	}

	// 非 mandatory（70-79 borderline）空 reason → 正常，不挡
	opt := &ReviewRequest{TaskRef: "c/optional", Score: 75, Grade: "C", Mandatory: false, Status: ReviewPending, CreatedAt: time.Now()}
	if err := SaveReview(root, opt); err != nil {
		t.Fatal(err)
	}
	if err := ResolveReview(root, "c/optional", ""); err != nil {
		t.Fatalf("non-mandatory resolve without reason should pass: %v", err)
	}
}
