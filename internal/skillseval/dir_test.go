package skillseval

// dir_test.go — 解析优先级链 + 一次性旧路径迁移覆盖。
//
// 测试构造的旧树镜像真实的命名空间前布局（见下）：
//
//	~/.pi/research/skill-eval/{cases/<s>.json, runs/<s>.jsonl, baselines.json}
//	~/.pi/research/eval-<name>.md            (eval-gen --save checklists, sibling dir)
//
// HOME 控制旧路径位置，FORGE_DATA_HOME 控制新根位置，FORGE_EVAL_DIR 控制 env
// 覆盖——三者的优先级正是被测行为。

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// isolateHome 把 HOME 指向全新临时目录（旧路径随之解析到其下）。
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	return home
}

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

func TestResolveDir_EnvOverDefault_NoMigration(t *testing.T) {
	home := isolateHome(t)
	writeLegacyTree(t, home)
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
	// env 分支与 explicit 同理：绝不迁移（env 常被 CI 指向仓库目录）。
	if _, err := os.Stat(filepath.Join(home, ".pi", "research", "skill-eval")); err != nil {
		t.Fatalf("env 解析不该触发迁移: %v", err)
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
	// Marker anchors "done" — deleting migrated artifacts must not resurrect them.
	if _, err := os.Stat(filepath.Join(got, markerName)); err != nil {
		t.Fatalf("迁移完成后应写哨兵标记 %s: %v", markerName, err)
	}
	if err := os.Remove(cl); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "eval-zombie.md"), []byte("# back"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveDir(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(cl); !os.IsNotExist(err) {
		t.Fatalf("标记存在时删掉的清单不得复活（got err=%v）", err)
	}
	if _, err := os.Stat(filepath.Join(got, "checklists", "eval-zombie.md")); !os.IsNotExist(err) {
		t.Fatalf("标记存在后旧目录新增的清单不得被迁入（got err=%v）", err)
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
		t.Fatalf("target 已存在时不得迁入旧树数据（got err=%v）", err)
	}
	// 同一趟 pass 的另一半照常：清单仍迁入（半迁语义由标记收口，两步跑完才标记）。
	if _, err := os.Stat(filepath.Join(got, "checklists", "eval-demo.md")); err != nil {
		t.Fatalf("target 已存在时清单仍应迁入: %v", err)
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
	// 无旧数据：根目录 + 标记会建（一次性判定的代价），但绝无数据结构。
	if _, err := os.Stat(filepath.Join(got, markerName)); err != nil {
		t.Fatalf("无旧数据也应写标记（终止后续每请求 Glob）: %v", err)
	}
	if _, err := os.Stat(filepath.Join(got, "cases")); !os.IsNotExist(err) {
		t.Fatalf("无旧数据时不应创建数据结构（got err=%v）", err)
	}
}

func TestMigrateLegacyTree_LegacyIsFileNotMigrated(t *testing.T) {
	home := isolateHome(t)
	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)
	// 旧位置被误建为普通文件：不得把它 rename 成 eval 根（根变成文件后全线崩）。
	legacy := filepath.Join(home, ".pi", "research", "skill-eval")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("oops"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(got)
	if err != nil || !fi.IsDir() {
		t.Fatalf("eval 根必须是目录（fi=%v err=%v）——旧文件不得被 rename 就位", fi, err)
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("旧文件应原样保留（不该被消费）: %v", err)
	}
}

func TestMigrateLegacyTree_CopyFallback_LeavesStagingClean(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 语义在 Windows 不可用，rename 恒成功不走 copy 路径")
	}
	home := isolateHome(t)
	legacyRoot := writeLegacyTree(t, home)
	// 父目录只读 → rename 出不去（同卷 rename 需要源父目录写权限）→ 走 staging copy。
	if err := os.Chmod(filepath.Join(home, ".pi", "research", "skill-eval"), 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(home, ".pi", "research", "skill-eval"), 0755) })
	if err := os.Chmod(legacyRoot, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(legacyRoot, 0755) })

	dataHome := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", dataHome)

	got, err := ResolveDir("")
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(got); err != nil || !fi.IsDir() {
		t.Fatalf("copy 回退后目标应为完整目录（fi=%v err=%v）", fi, err)
	}
	if _, err := os.Stat(filepath.Join(got, "cases", "demo.json")); err != nil {
		t.Fatalf("copy 回退应迁入全部数据: %v", err)
	}
	if _, err := os.Stat(got + ".migrating"); !os.IsNotExist(err) {
		t.Fatalf("staging 残骸应被清理（got err=%v）", err)
	}
	// copy 路径刻意保留旧树（跨卷场景删源有数据丢失风险）。
	if _, err := os.Stat(filepath.Join(home, ".pi", "research", "skill-eval", "baselines.json")); err != nil {
		t.Fatalf("copy 路径旧树应保留: %v", err)
	}
}

func TestCopyTree_CopiesAllFiles(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "cases"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "cases", "a.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "baselines.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{filepath.Join("cases", "a.json"), "baselines.json"} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Fatalf("copyTree 缺 %s: %v", rel, err)
		}
	}
}
