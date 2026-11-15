package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// legacyChange 造一个没有 goal.md 的老 change（其余产物齐全）。
func legacyChange(t *testing.T, dir, id string) {
	t.Helper()
	rel := filepath.Join(dir, "spec", "changes", id)
	if err := os.MkdirAll(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"proposal.md":   "# 提案\n\n为什么做。\n",
		"design.md":     "# 设计\n\n怎么改。\n",
		"acceptance.md": "# 验收\n\n```\ntrue\n```\n",
		"tasks.md":      "# 任务\n\n- [ ] 1. 做一件事（判据：acceptance #1）\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(rel, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// 老 change（无 goal.md）也必须能用 CLI 记录 clarify——否则升级用户会被卡在
// clarify 阶段，而阶段机自己又跳过了 goal 检查（两处口径不一致）。
func TestChangeClarify_LegacyChangeWithoutGoal(t *testing.T) {
	if _, _, code := runForge(t, t.TempDir(), "change", "init"); code != 0 {
		t.Skip("change init 不可用")
	}
	dir := t.TempDir()
	if _, _, code := runForge(t, dir, "change", "init"); code != 0 {
		t.Fatalf("change init 失败")
	}
	legacyChange(t, dir, "old")

	out, stderr, code := runForge(t, dir, "change", "clarify", "old", "--by-human", "--note", "只改 A 的渲染路径；不动 B 的存储格式")
	if code != 0 {
		t.Fatalf("老 change 应能记录 clarify: %s %s", out, stderr)
	}
	phaseOut, _, _ := runForge(t, dir, "change", "phase", "old")
	if !strings.Contains(phaseOut, "ready") {
		t.Fatalf("补齐 clarify 后老 change 应到 ready，got:\n%s", phaseOut)
	}
	if !strings.Contains(phaseOut, "老 change") || !strings.Contains(phaseOut, "--change") {
		t.Fatalf("老 change 的下一步应说明不能 --change，got:\n%s", phaseOut)
	}
}

func TestChangePhase_ArchivedChange(t *testing.T) {
	dir := t.TempDir()
	if _, _, code := runForge(t, dir, "change", "init"); code != 0 {
		t.Fatalf("change init 失败")
	}
	arch := filepath.Join(dir, "spec", "changes", "archive", "20260101-old")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	out, stderr, code := runForge(t, dir, "change", "phase", "old")
	if code == 0 {
		t.Fatalf("归档的 change 不该继续推进阶段")
	}
	if !strings.Contains(out+stderr, "已归档") {
		t.Fatalf("错误应说明已归档: %s %s", out, stderr)
	}
}
