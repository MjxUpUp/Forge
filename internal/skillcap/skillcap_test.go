package skillcap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func draft() Draft {
	return Draft{
		ID:          "read-before-edit",
		Description: "改文件前先读它（含生成物），避免盲改",
		WhenToUse:   "编辑任何非本会话新建的文件前",
		Lesson:      "没读就改会把既有语义改坏；先读、再定位、再改，是最便宜的纪律。",
		Evidence:    "commit abc1234；TestX 覆盖",
	}
}

func TestRender_HasFrontmatterAndSections(t *testing.T) {
	out, err := Render(draft())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, want := range []string{
		"---\nname: read-before-edit\n",
		`description: "改文件前先读它（含生成物），避免盲改"`,
		"## When to use",
		"## Procedure",
		"## Evidence",
		"commit abc1234",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered skill 缺 %q：\n%s", want, out)
		}
	}
}

func TestValidate_RejectsUnsafeOrEmpty(t *testing.T) {
	cases := map[string]Draft{
		"traversal id": {ID: "../evil", Description: "d", WhenToUse: "w", Lesson: "l"},
		"uppercase id": {ID: "ReadBeforeEdit", Description: "d", WhenToUse: "w", Lesson: "l"},
		"no desc":      {ID: "ok-id", Description: "  ", WhenToUse: "w", Lesson: "l"},
		"no when":      {ID: "ok-id", Description: "d", WhenToUse: "", Lesson: "l"},
		"no lesson":    {ID: "ok-id", Description: "d", WhenToUse: "w", Lesson: ""},
		"short id":     {ID: "a", Description: "d", WhenToUse: "w", Lesson: "l"},
		"dot in id":    {ID: "a.b", Description: "d", WhenToUse: "w", Lesson: "l"},
		"space in id":  {ID: "a b", Description: "d", WhenToUse: "w", Lesson: "l"},
		"unicode id":   {ID: "技能", Description: "d", WhenToUse: "w", Lesson: "l"},
	}
	for name, d := range cases {
		if err := d.Validate(); err == nil {
			t.Fatalf("%s: 期望拒绝，实际通过", name)
		}
	}
}

func TestWrite_RefusesOverwriteUnlessForce(t *testing.T) {
	root := t.TempDir()
	path, err := Write(root, "skills", draft(), false)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if want := filepath.Join(root, "skills", "read-before-edit", "SKILL.md"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
	if _, err := Write(root, "skills", draft(), false); err == nil {
		t.Fatal("重写未加 --force 应被拒")
	}
	if _, err := Write(root, "skills", draft(), true); err != nil {
		t.Fatalf("--force 应允许重写：%v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("文件应存在：%v", err)
	}
}

func TestWrite_RejectsSkillsDirTraversal(t *testing.T) {
	if _, err := Write(t.TempDir(), "../outside", draft(), false); err == nil {
		t.Fatal("skillsDir 越界应被拒")
	}
}
