package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// spec 契约仓库没有 protocol.yml 是常态：静默用默认权重，不要提示 run forge init。
func TestLoadScoringConfig_ContractRepoWithoutProtocolIsSilent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatalf("mkdir spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec", "forge.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
	var cfg *struct{ weights, thresholds int }
	stderr := captureStderr(t, func() {
		c := loadScoringConfig(root)
		cfg = &struct{ weights, thresholds int }{len(c.Weights), len(c.Thresholds)}
	})
	if strings.Contains(stderr, "protocol.yml 加载失败") {
		t.Fatalf("contract repo should not warn about missing protocol.yml, got: %q", stderr)
	}
	if cfg.weights == 0 || cfg.thresholds == 0 {
		t.Fatalf("expected default weights/thresholds, got weights=%d thresholds=%d", cfg.weights, cfg.thresholds)
	}
}

// 非契约仓库缺 protocol.yml 仍要提示：可能是手误或类型错，静默换权重等于无痕改分。
func TestLoadScoringConfig_NonContractRepoWarns(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	stderr := captureStderr(t, func() { _ = loadScoringConfig(root) })
	if !strings.Contains(stderr, "protocol.yml 加载失败") {
		t.Fatalf("non-contract repo should warn on missing protocol.yml, got: %q", stderr)
	}
}
