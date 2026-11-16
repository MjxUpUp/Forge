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

// 契约仓库「缺文件」静默，但「文件存在却解析失败」是配置错误，必须告警：
// 否则坏 YAML 会被当成默认权重，等于无痕改分。
func TestLoadScoringConfig_ContractRepoWithBrokenProtocolWarns(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, ".forge"), 0o755); err != nil {
		t.Fatalf("mkdir .forge: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatalf("mkdir spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec", "forge.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
	// 项目级覆盖优先级最高：写一份坏 YAML，Load 必然失败而不是「缺失」。
	if err := os.WriteFile(filepath.Join(root, ".forge", "protocol.yml"), []byte("scoring: [unclosed\n"), 0o644); err != nil {
		t.Fatalf("write broken protocol: %v", err)
	}
	stderr := captureStderr(t, func() { _ = loadScoringConfig(root) })
	if !strings.Contains(stderr, "protocol.yml 加载失败") {
		t.Fatalf("broken protocol.yml must warn even on a contract repo, got: %q", stderr)
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

// 生产路径（BuildEvaluateInput）必须走同一份配置加载：契约仓库缺 protocol.yml 不告警。
// 之前 loadScoringConfig 是孤儿函数，测试全绿而 task complete 仍打印告警。
func TestBuildEvaluateInput_ContractRepoNoProtocolWarning(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Join(root, "spec"), 0o755); err != nil {
		t.Fatalf("mkdir spec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "spec", "forge.yaml"), []byte("version: \"1\"\n"), 0o644); err != nil {
		t.Fatalf("write contract: %v", err)
	}
	st := &TaskState{TaskRef: "feat/probe", Branch: "main", HeadCommit: "deadbeef"}
	stderr := captureStderr(t, func() { _, _, _ = BuildEvaluateInput(root, st) })
	if strings.Contains(stderr, "protocol.yml 加载失败") {
		t.Fatalf("contract repo must not warn about missing protocol.yml in production path, got: %q", stderr)
	}
}

func TestBuildEvaluateInput_NonContractRepoWarns(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	st := &TaskState{TaskRef: "feat/probe", Branch: "main", HeadCommit: "deadbeef"}
	stderr := captureStderr(t, func() { _, _, _ = BuildEvaluateInput(root, st) })
	if !strings.Contains(stderr, "protocol.yml 加载失败") {
		t.Fatalf("non-contract repo must warn in production path, got: %q", stderr)
	}
}
