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
