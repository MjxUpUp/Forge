package skillscanonical

import (
	"os"
	"path/filepath"
	"testing"

	skillsforge "github.com/MjxUpUp/Forge/skills-forge"
)

// TestEnsureEmbeddedCache_ForgeOverlay pins the merged-cache overlay contract (2026-08 zero-reverse-dependency migration): the forge-native skills-forge/ tree is additively extracted into the SAME cache dir with its own marker (.embedded-forge-version), independently of the neutral layer's lifecycle — a reused pre-migration neutral cache still receives the overlay, and a fresh marker suppresses re-extraction on the next run.
//
// TestEnsureEmbeddedCache_ForgeOverlay 钉住合并缓存覆盖层契约（2026-08 零反向依赖
// 迁移）：skills-forge/ 树增量解压进同一缓存目录、自带独立标记
// （.embedded-forge-version）、与中立层生命周期互不干扰——迁移前复用的旧中立缓存
// 也会补上覆盖层；标记新鲜时下次运行不再重解压。
func TestEnsureEmbeddedCache_ForgeOverlay(t *testing.T) {
	cache := t.TempDir()

	// 模拟迁移前的旧中立缓存：CONVENTIONS.md + 匹配的中立版本标记，无覆盖层内容、
	// 无 forge 标记。
	if err := os.WriteFile(filepath.Join(cache, "CONVENTIONS.md"), []byte("# c\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, VersionFile), []byte("v9"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureEmbeddedCache(cache, "v9"); err != nil {
		t.Fatalf("EnsureEmbeddedCache on reused neutral cache: %v", err)
	}

	// 覆盖层必须已落盘：每个内嵌 forge 原生 skill 目录都有 SKILL.md，且 forge 标记
	// 写入的是运行版本。
	entries, err := skillsforge.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	skillDirs := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, serr := os.Stat(filepath.Join(cache, e.Name(), "SKILL.md")); serr != nil {
			t.Errorf("overlay skill %s missing SKILL.md in cache: %v", e.Name(), serr)
		}
		skillDirs++
	}
	if skillDirs == 0 {
		t.Fatal("skillsforge.FS resolved 0 skill dirs — overlay contract untestable")
	}
	marker, rerr := os.ReadFile(filepath.Join(cache, ForgeVersionFile))
	if rerr != nil || string(marker) != "v9" {
		t.Errorf("forge marker want v9, got %q (err %v)", marker, rerr)
	}
	// 复用路径不动中立层：CONVENTIONS.md 仍在。
	if _, serr := os.Stat(filepath.Join(cache, "CONVENTIONS.md")); serr != nil {
		t.Errorf("neutral CONVENTIONS.md must survive the overlay pass: %v", serr)
	}

	// 同版本二次运行：整体 no-op（两个标记都新鲜）——EnsureEmbeddedCache 不报错且
	// 标记仍在即通过。
	if err := EnsureEmbeddedCache(cache, "v9"); err != nil {
		t.Fatalf("EnsureEmbeddedCache idempotent rerun: %v", err)
	}
}
