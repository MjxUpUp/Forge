package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// TestWriteArtifact_ReferenceTriangle pins I5 (multi-task-concurrency §9): the
// file owns the content (frontmatter carries task-ref/stage), the state side
// holds a DataDir- relative path + content hash, and VerifyArtifact detects
// hand-edits as drift.
//
// TestWriteArtifact_ReferenceTriangle 钉住 I5（multi-task-concurrency §9）：文件拥有
// 内容（frontmatter 带 task-ref/stage），状态侧持 DataDir 相对路径 + 内容哈希，
// VerifyArtifact 把手改检测为漂移。
func TestWriteArtifact_ReferenceTriangle(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	ref, err := WriteArtifact(root, "feat/x", "plan", "# 计划\nstep 1\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref.Path, "specs/") || !strings.HasSuffix(ref.Path, "plan.md") {
		t.Fatalf("引用路径应为 DataDir 相对的 specs/…: %q", ref.Path)
	}
	if len(ref.Hash) != 16 {
		t.Fatalf("哈希应为 16 hex, got %q", ref.Hash)
	}
	if !VerifyArtifact(root, ref) {
		t.Fatal("刚写入的引用应验证通过")
	}
	// frontmatter 身份三角可从文件自证。
	data, _ := os.ReadFile(filepath.Join(forgedata.DataDirFor(root), filepath.FromSlash(ref.Path)))
	if !strings.Contains(string(data), "forge-task-ref: feat/x") || !strings.Contains(string(data), "forge-stage: plan") {
		t.Fatalf("frontmatter 缺身份: %s", data)
	}
	// 手改 → 漂移。
	abs := filepath.Join(forgedata.DataDirFor(root), filepath.FromSlash(ref.Path))
	os.WriteFile(abs, []byte("---\nforge-task-ref: feat/x\n---\n篡改"), 0o644)
	if VerifyArtifact(root, ref) {
		t.Fatal("手改后应报漂移（哈希失配）")
	}
}

// TestArchiveAttempt_WriteOnceAndFeedback pins the attempts contract (§9 +
// LoopSpec deep-read absorption): failed-round findings are preserved write-once
// (re-archiving the same round REFUSES), never deleted, and PriorAttemptsSummary
// feeds them back bounded.
//
// TestArchiveAttempt_WriteOnceAndFeedback 钉住 attempts 契约（§9 + LoopSpec 深读吸
// 收）：失败轮 findings 一次写入保留（同轮重归档【拒绝】）、永不删除，
// PriorAttemptsSummary 有界回灌。
func TestArchiveAttempt_WriteOnceAndFeedback(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()

	if err := ArchiveAttempt(root, "feat/y", 1, []string{"[F1] 空指针（review）", "[F2] 边界缺失"}); err != nil {
		t.Fatal(err)
	}
	if err := ArchiveAttempt(root, "feat/y", 1, []string{"重写"}); err == nil {
		t.Fatal("同轮重归档应拒绝（一次写入契约）")
	}
	if err := ArchiveAttempt(root, "feat/y", 2, []string{"[F3] 复发"}); err != nil {
		t.Fatal(err)
	}

	summary := PriorAttemptsSummary(root, "feat/y", 3, 4000)
	if !strings.Contains(summary, "F1") || !strings.Contains(summary, "F3") || !strings.Contains(summary, "勿重复踩坑") {
		t.Fatalf("回灌摘要应含各轮 findings: %q", summary)
	}
	// 有界：lastN=1 只取最近轮。
	only1 := PriorAttemptsSummary(root, "feat/y", 1, 4000)
	if strings.Contains(only1, "F1") || !strings.Contains(only1, "F3") {
		t.Fatalf("lastN=1 应只回灌最近轮: %q", only1)
	}
	// 字符上限生效。
	capped := PriorAttemptsSummary(root, "feat/y", 3, 60)
	if len(capped) > 200 { // 截断标记允许少量超出
		t.Fatalf("字符上限应生效: len=%d", len(capped))
	}
	// 无 attempts 的任务回灌为空（不污染 HANDOFF）。
	if s := PriorAttemptsSummary(root, "feat/none", 3, 100); s != "" {
		t.Fatalf("无尝试应为空串, got %q", s)
	}
}
