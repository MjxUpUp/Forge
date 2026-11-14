package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillsList_DoesNotWriteHome pins the post-Phase-D contract that the
// read-only listing path never materializes the embedded library on disk:
// `forge skills list` reads the compile-time embed FS directly.
//
// TestSkillsList_DoesNotWriteHome 钉住阶段 D 收尾后的契约：只读列举路径不做任何
// 落盘物化——`forge skills list` 直接读编译期 embed FS，用户 home 保持为空。
func TestSkillsList_DoesNotWriteHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	t.Setenv("FORGE_SKILLS_CANONICAL", "")

	dir := freshProject(t)
	out := forge(t, dir, "skills", "list")

	if !strings.Contains(out, "内置 embed 库") {
		t.Errorf("list 应说明来源为内置 embed 库, got %q", out)
	}
	if !strings.Contains(out, "共 ") {
		t.Errorf("list 应输出 skill 计数, got %q", out)
	}

	// home 必须完全干净：没有 skills-cache、没有 skills-manifest.json、没有任何
	// forge 用户级产物（init 只写仓库）。
	var leaked []string
	_ = filepath.WalkDir(home, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(home, p)
			leaked = append(leaked, rel)
		}
		return nil
	})
	if len(leaked) != 0 {
		t.Fatalf("skills list 不应写用户 home，发现 %d 个文件: %v", len(leaked), leaked)
	}
}
