package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillNew_WritesSkillFile(t *testing.T) {
	dir := t.TempDir()
	_, stderr, code := runForge(t, dir, "skill", "new", "read-before-edit",
		"--description", "改文件前先读它",
		"--when", "编辑非本会话新建的文件前",
		"--lesson", "没读就改会把既有语义改坏",
		"--evidence", "commit abc1234")
	if code != 0 {
		t.Fatalf("skill new 应成功，code=%d stderr=%s", code, stderr)
	}

	path := filepath.Join(dir, "skills", "read-before-edit", "SKILL.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 SKILL.md：%v", err)
	}
	body := string(raw)
	if !strings.Contains(body, "name: read-before-edit") || !strings.Contains(body, "## Procedure") {
		t.Fatalf("SKILL.md 内容不完整：\n%s", body)
	}

	// 再写一次必须被拒（除非 --force）：skill 是长期资产，静默覆盖会抹掉别人的修订。
	if _, _, code := runForge(t, dir, "skill", "new", "read-before-edit",
		"--description", "d", "--when", "w", "--lesson", "l"); code == 0 {
		t.Fatal("未加 --force 的重写应被拒")
	}
	if _, stderr, code := runForge(t, dir, "skill", "new", "read-before-edit",
		"--description", "d2", "--when", "w2", "--lesson", "l2", "--force"); code != 0 {
		t.Fatalf("--force 应允许覆盖：code=%d stderr=%s", code, stderr)
	}
}
