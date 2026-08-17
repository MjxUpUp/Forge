package skillseval

// dir_test.go — resolution-priority chain + one-time legacy migration coverage.
//
// The legacy tree built by these tests mirrors the real pre-namespace layout:
//
//	~/.pi/research/skill-eval/{cases/<s>.json, runs/<s>.jsonl, baselines.json}
//	~/.pi/research/eval-<name>.md            (eval-gen --save checklists, sibling dir)
//
// dir_test.go — 解析优先级链 + 一次性旧路径迁移覆盖。
//
// 测试构造的旧树镜像真实的命名空间前布局（见上）。HOME 控制旧路径位置，
// FORGE_DATA_HOME 控制新根位置，FORGE_EVAL_DIR 控制 env 覆盖——三者的优先级
// 正是被测行为。

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateHome points HOME at a fresh temp dir (legacy paths resolve under it).
//
// isolateHome 把 HOME 指向全新临时目录（旧路径随之解析到其下）。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

// writeLegacyTree creates the pre-namespace layout under home and returns the legacy root.
//
// writeLegacyTree 在 home 下造命名空间前布局，返回旧 research 根。
func writeLegacyTree(t *testing.T, home string) string {
	t.Helper()
	root := filepath.Join(home, ".pi", "research")
	for _, rel := range []string{
		filepath.Join("skill-eval", "cases", "demo.json"),
		filepath.Join("skill-eval", "runs", "demo.jsonl"),
		filepath.Join("skill-eval", "baselines.json"),
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "eval-demo.md"), []byte("# checklist"), 0644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestResolveDir_ExplicitWins(t *testing.T) {
	home := isolateHome(t)
	writeLegacyTree(t, home)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)
	t.Setenv(EnvDirName, filepath.Join(dataHome, "env-dir"))

	explicit := filepath.Join(t.TempDir(), "repo-evals")
	got, err := ResolveDir(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if got != explicit {
		t.Fatalf("explicit = %q, want %q（explicit 必须压过 env 与默认）", got, explicit)
	}
	// Explicit resolution must never migrate: legacy stays untouched.
	if _, err := os.Stat(filepath.Join(home, ".pi", "research", "skill-eval")); err != nil {
		t.Fatalf("explicit 解析不该触发迁移，旧树应原样保留: %v", err)
	}
}

func TestResolveDir_EnvOverDefault(t *testing.T) {
	isolateHome(t)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)
	envDir := filepath.Join(t.TempDir(), "env-evals")
	t.Setenv(EnvDirName, envDir)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	if got != envDir {
		t.Fatalf("env 解析 = %q, want %q", got, envDir)
	}
}

func TestResolveDir_DefaultUnderGlobalHome(t *testing.T) {
	isolateHome(t)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dataHome, "evals")
	if got != want {
		t.Fatalf("默认解析 = %q, want %q（须在 GlobalHome 下）", got, want)
	}
}

func TestResolveDir_MigratesLegacyOnce(t *testing.T) {
	home := isolateHome(t)
	legacyRoot := writeLegacyTree(t, home)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	// Tree moved: new location has the data, legacy tree is gone (rename semantics).
	if _, err := os.Stat(filepath.Join(got, "cases", "demo.json")); err != nil {
		t.Fatalf("迁移后新根应有 cases/demo.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "runs", "demo.jsonl")); err != nil {
		t.Fatalf("迁移后新根应有 runs/demo.jsonl: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".pi", "research", "skill-eval")); !os.IsNotExist(err) {
		t.Fatalf("rename 迁移后旧树应不存在（got err=%v）", err)
	}
	// Sibling checklists copied into checklists/ (copy, legacy root itself stays).
	cl := filepath.Join(got, "checklists", "eval-demo.md")
	if _, err := os.Stat(cl); err != nil {
		t.Fatalf("旧清单应迁入 checklists/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyRoot, "eval-demo.md")); err != nil {
		t.Fatalf("清单走 copy 不删除源（旧文件应保留）: %v", err)
	}

	// Idempotent: second resolution is a no-op and keeps data intact.
	if _, err := ResolveDir(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got, "cases", "demo.json")); err != nil {
		t.Fatalf("二次解析不应影响已迁移数据: %v", err)
	}
}

func TestResolveDir_TargetExistsLegacyUntouched(t *testing.T) {
	home := isolateHome(t)
	writeLegacyTree(t, home)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)
	// Pre-existing target must never be clobbered by migration.
	existing := filepath.Join(dataHome, "evals", "marker.txt")
	if err := os.MkdirAll(filepath.Dir(existing), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(existing, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "keep" {
		t.Fatalf("已存在目标不得被覆盖: data=%q err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(got, "cases", "demo.json")); !os.IsNotExist(err) {
		t.Fatalf("target 已存在时不得迁入旧数据（got err=%v）", err)
	}
}

func TestResolveDir_NoLegacyNoOp(t *testing.T) {
	isolateHome(t)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(got, "cases")); !os.IsNotExist(err) {
		t.Fatalf("无旧数据时不应创建任何结构（got err=%v）", err)
	}
}
