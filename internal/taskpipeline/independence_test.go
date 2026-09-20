package taskpipeline

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// independence_test.go — 价-1b 独立性归因的行为钉（配对 independence.go）。

// TestReviewIndependence_SelfReviewDetected：生产者遥测存在时，盖章会话 ∈
// 生产者集 → ok=false；独立会话 → ok=true；无遥测 → known=false fail-open。
func TestReviewIndependence_SelfReviewDetected(t *testing.T) {
	dir := t.TempDir()
	const ref = "feat/ind"
	if err := SaveTaskState(dir, &TaskState{TaskRef: ref, Branch: "feat/x"}); err != nil {
		t.Fatal(err)
	}
	seed := func(session, tool string) {
		if err := toolusage.Record(dir, &toolusage.ToolCall{
			ToolName: tool, TaskRef: ref, SessionID: session, Timestamp: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 无遥测：unknown 放行。
	if ok, known := ReviewIndependence(dir, ref, "s-author"); ok != true || known != false {
		t.Fatalf("无遥测应 (true,false), got (%v,%v)", ok, known)
	}
	// 生产者 = s-author（Write/Edit）；盖章同会话 → 自审。
	seed("s-author", "Write")
	seed("s-author", "Edit")
	seed("s-reader", "Read")
	if ok, known := ReviewIndependence(dir, ref, "s-author"); ok || !known {
		t.Fatalf("生产者盖章应 (false,true), got (%v,%v)", ok, known)
	}
	if ok, known := ReviewIndependence(dir, ref, "s-reviewer"); !ok || !known {
		t.Fatalf("独立会话应 (true,true), got (%v,%v)", ok, known)
	}
	// Reader-only 会话盖章不算自审（只读不产码）。
	if ok, _ := ReviewIndependence(dir, ref, "s-reader"); !ok {
		t.Fatal("Reader-only 会话盖章应判独立")
	}
}

// TestSelfReviewRow：RecordSelfReviewRow 落行 + HasSelfReviewRow 按 kind 命中。
func TestSelfReviewRow(t *testing.T) {
	dir := t.TempDir()
	const ref = "feat/sr"
	RecordSelfReviewRow(dir, ref, "s1", "code-review")
	if !HasSelfReviewRow(dir, ref, "code-review") {
		t.Fatal("kind=code-review 应命中")
	}
	if HasSelfReviewRow(dir, ref, "doc-review") {
		t.Fatal("kind 隔离：doc-review 不应命中")
	}
	entries, _ := checklog.LoadForTask(dir, ref)
	found := false
	for _, e := range entries {
		if e.Check == CheckNameSelfReview && e.Meta["self_review"] == "true" && e.EffectiveLevel() == checklog.LevelWarn {
			found = true
		}
	}
	if !found {
		t.Error("自审行应为 WARN + self_review meta")
	}
}

// TestBashWritesFiles（二轮复审收尾）：覆盖写/heredoc/就地改写命中；只读
// 重定向与无关命令不命中——误报方向落在 docgate 硬拒路径，两侧都要钉。
func TestBashWritesFiles(t *testing.T) {
	for _, hit := range []string{
		"cat > f.go",                    // 覆盖写（取证主形态）
		"echo x >> log.txt",             // 追加
		"cat <<EOF > f",                 // heredoc
		"sed -i s/a/b/ f.go",            // 就地改写
		"go build 2>&1 | tee build.log", // tee 落盘
		"truncate -s 0 x",               // 截断
		"dd if=a of=b",                  // 词边界 dd
		"forge gate x 2>&1 > out.txt",   // 混合：只读合并 + 真写
	} {
		if !bashWritesFiles(hit) {
			t.Errorf("应命中写文件模式: %q", hit)
		}
	}
	for _, miss := range []string{
		"go test ./... 2>&1 | tail -5", // stderr 合并（只读验证常态）
		"forge task gate task-verify 2>&1",
		"cmd 2> /dev/null",         // stderr 丢弃
		"git add .",                // 词边界 dd 不误伤
		"ls -la && go build ./...", // 无重定向
	} {
		if bashWritesFiles(miss) {
			t.Errorf("不应命中（只读/无关）: %q", miss)
		}
	}
}
