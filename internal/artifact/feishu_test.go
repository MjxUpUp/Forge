package artifact

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

func TestDefaultFeishuConfigDisabled(t *testing.T) {
	os.Unsetenv("FORGE_FEISHU_SPACE_ID")
	os.Unsetenv("FORGE_FEISHU_WIKI_NODE")
	t.Setenv("FORGE_FEISHU_AUTO_PUBLISH", "false")

	cfg := DefaultFeishuConfig()
	if cfg.Enabled {
		t.Fatal("expected Enabled=false when FORGE_FEISHU_AUTO_PUBLISH=false, got true")
	}
	if cfg.SpaceID != "" {
		t.Fatalf("expected empty SpaceID, got %q", cfg.SpaceID)
	}
	if cfg.ParentNodeToken != "" {
		t.Fatalf("expected empty ParentNodeToken, got %q", cfg.ParentNodeToken)
	}
}

func TestDefaultFeishuConfigEnabled(t *testing.T) {
	t.Setenv("FORGE_FEISHU_SPACE_ID", "space-123")
	t.Setenv("FORGE_FEISHU_WIKI_NODE", "node-456")
	// Default (unset) FORGE_FEISHU_AUTO_PUBLISH means Enabled=true

	cfg := DefaultFeishuConfig()
	if !cfg.Enabled {
		t.Fatal("expected Enabled=true when FORGE_FEISHU_SPACE_ID is set and AUTO_PUBLISH is unset")
	}
	if cfg.SpaceID != "space-123" {
		t.Fatalf("expected SpaceID=%q, got %q", "space-123", cfg.SpaceID)
	}
	if cfg.ParentNodeToken != "node-456" {
		t.Fatalf("expected ParentNodeToken=%q, got %q", "node-456", cfg.ParentNodeToken)
	}
}

func TestPublishMarkdownDisabled(t *testing.T) {
	cfg := FeishuConfig{Enabled: false}
	err := PublishMarkdown(cfg, "gate-0", "test.md", "/tmp")
	if err != nil {
		t.Fatalf("expected nil when disabled, got %v", err)
	}
}

func TestPublishMarkdownNoSpaceID(t *testing.T) {
	cfg := FeishuConfig{Enabled: true, SpaceID: ""}
	err := PublishMarkdown(cfg, "gate-0", "test.md", "/tmp")
	if err == nil {
		t.Fatal("expected error when Enabled=true but SpaceID is empty")
	}
}

func TestPublishAllOutputsSkipsNonMD(t *testing.T) {
	p := forgedatatest.ForDataDir(t.TempDir())
	cfg := FeishuConfig{Enabled: true, SpaceID: "space-123"}

	// Create a .txt file — it should be skipped entirely (no error).
	outputs := []string{"report.txt", "data.json", "summary.md"}

	// We only create the .txt and .json files; .md doesn't exist so it gets
	// skipped too. The important thing is non-.md entries never reach PublishMarkdown.
	gateDir := p.GateDir("gate-0")
	if err := os.MkdirAll(gateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gateDir, "report.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	// This should not panic or error — non-.md files are skipped.
	PublishAllOutputs(cfg, "gate-0", outputs, p)
}

func TestPublishAllOutputsSkipsMissingFile(t *testing.T) {
	p := forgedatatest.ForDataDir(t.TempDir())
	cfg := FeishuConfig{Enabled: true, SpaceID: "space-123"}

	// "missing.md" does not exist on disk — should be skipped without error.
	outputs := []string{"missing.md"}
	PublishAllOutputs(cfg, "gate-0", outputs, p)
}
