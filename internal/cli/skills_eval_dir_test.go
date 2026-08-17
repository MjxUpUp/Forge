package cli

// skills_eval_dir_test.go — the --dir persistent flag routes the eval command family
// at an explicit data dir (repo-level evals/ / CI) instead of the user-level default.
//
// skills_eval_dir_test.go —— --dir persistent flag 让 eval 命令族落到显式数据目录
// （仓库级 evals/ / CI），而非用户级默认。

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvalGen_DirFlag_WritesToExplicitDir(t *testing.T) {
	canonical := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_SKILLS_CANONICAL", canonical)
	evalLoopWriteSkill(t, canonical, "dir-flag-skill", "Use when: 编写组件 SKIP: 技术选型")

	explicit := t.TempDir()
	skEvalDirFlag, skEvalSkill, skEvalCasesOnly = explicit, "dir-flag-skill", true
	t.Cleanup(func() { skEvalDirFlag, skEvalSkill, skEvalCasesOnly = "", "", false })

	if err := runSkillsEvalGen(nil, nil); err != nil {
		t.Fatalf("eval-gen --cases-only --dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(explicit, "cases", "dir-flag-skill.json")); err != nil {
		t.Fatalf("case 集应落在 --dir 显式目录: %v", err)
	}
	// 用户级默认位置不得被写入——--dir 是替代不是镜像。
	if _, err := os.Stat(filepath.Join(home, ".forge", "evals", "cases")); !os.IsNotExist(err) {
		t.Fatalf("--dir 生效时默认位置不应有写入（got err=%v）", err)
	}
}

func TestSaveEval_WritesUnderResolvedRoot(t *testing.T) {
	root := t.TempDir()
	if err := saveEval(root, "demo", "# checklist"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "checklists", "eval-demo.md")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("清单应落 <root>/checklists/eval-<name>.md: %v", err)
	}
	// 旧硬编码位置（~/.pi/research）不得再被创建。
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".pi", "research")); !os.IsNotExist(err) {
		t.Fatalf("saveEval 不应再写 ~/.pi/research（got err=%v）", err)
	}
}
