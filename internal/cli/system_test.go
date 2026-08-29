package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCheckSkillsManifest_Missing：无 manifest 应产 1 warning（提示用户 install）。
func TestCheckSkillsManifest_Missing(t *testing.T) {
	var e, w int
	checkSkillsManifest(t.TempDir(), &e, &w)
	if w != 1 || e != 0 {
		t.Fatalf("missing manifest: want warn=1 err=0, got warn=%d err=%d", w, e)
	}
}

// TestCheckSkillsManifest_Present：合法 manifest 应 clean（无 err/warn）。
func TestCheckSkillsManifest_Present(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, os.MkdirAll(filepath.Join(home, ".forge"), 0755))
	payload := `{"generated_at":"2026-06-23T00:00:00Z","canonical_root":"E:/Forge/skills","stats":{"total":47,"pass":47,"issues":0}}`
	mustMkdir(t, os.WriteFile(filepath.Join(home, ".forge", "skills-manifest.json"), []byte(payload), 0644))

	var e, w int
	checkSkillsManifest(home, &e, &w)
	if e != 0 || w != 0 {
		t.Fatalf("present manifest: want clean, got err=%d warn=%d", e, w)
	}
}

// TestCheckSkillsManifest_Corrupt：损坏 JSON 应产 1 error。
func TestCheckSkillsManifest_Corrupt(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, os.MkdirAll(filepath.Join(home, ".forge"), 0755))
	mustMkdir(t, os.WriteFile(filepath.Join(home, ".forge", "skills-manifest.json"), []byte("not-json"), 0644))

	var e, w int
	checkSkillsManifest(home, &e, &w)
	if e != 1 {
		t.Fatalf("corrupt manifest: want err=1, got err=%d", e)
	}
}

func mustMkdir(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// TestCheckGlobalForge_RealLayout 钉住 refactor-data-home 后的真实布局：
// 检查 ~/.forge/projects/（per-project runtime state）与 ~/.forge/skills-cache/
// （embedded skills）——此前检查的 pipeline-templates/hooks/bin 无任何代码创建，
// 报的是永远修不好的 warning。
func TestCheckGlobalForge_RealLayout(t *testing.T) {
	home := t.TempDir()

	// No ~/.forge at all → 1 error.
	var e, w int
	checkGlobalForge(home, &e, &w)
	if e != 1 || w != 0 {
		t.Fatalf("missing ~/.forge: want err=1 warn=0, got err=%d warn=%d", e, w)
	}

	// ~/.forge exists but real-layout subdirs missing → 2 warnings, no error.
	mustMkdir(t, os.MkdirAll(filepath.Join(home, ".forge"), 0755))
	e, w = 0, 0
	checkGlobalForge(home, &e, &w)
	if e != 0 || w != 2 {
		t.Fatalf("missing projects+skills-cache: want err=0 warn=2, got err=%d warn=%d", e, w)
	}

	// Full real layout → clean.
	mustMkdir(t, os.MkdirAll(filepath.Join(home, ".forge", "projects"), 0755))
	mustMkdir(t, os.MkdirAll(filepath.Join(home, ".forge", "skills-cache"), 0755))
	e, w = 0, 0
	checkGlobalForge(home, &e, &w)
	if e != 0 || w != 0 {
		t.Fatalf("full layout: want clean, got err=%d warn=%d", e, w)
	}
}
