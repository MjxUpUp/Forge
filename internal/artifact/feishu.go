package artifact

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FeishuConfig holds feishu publishing configuration.
type FeishuConfig struct {
	SpaceID        string
	ParentNodeToken string
	Enabled        bool
}

// DefaultFeishuConfig returns config from environment variables.
func DefaultFeishuConfig() FeishuConfig {
	return FeishuConfig{
		SpaceID:        os.Getenv("FORGE_FEISHU_SPACE_ID"),
		ParentNodeToken: os.Getenv("FORGE_FEISHU_WIKI_NODE"),
		Enabled:        os.Getenv("FORGE_FEISHU_AUTO_PUBLISH") != "false",
	}
}

// PublishMarkdown publishes a .md file to feishu wiki.
func PublishMarkdown(cfg FeishuConfig, gateID, filePath, dir string) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.SpaceID == "" {
		return fmt.Errorf("FORGE_FEISHU_SPACE_ID not set")
	}
	if _, err := exec.LookPath("lark-cli"); err != nil {
		return fmt.Errorf("lark-cli not found: %w", err)
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", filePath, err)
	}

	fileName := filepath.Base(filePath)
	title := fmt.Sprintf("%s - %s", gateID, fileName)

	cmd := exec.Command("lark-cli", "drive", "+export",
		"--token", cfg.SpaceID,
		"--title", title,
		"--file-extension", "markdown",
	)
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(string(data))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("lark-cli publish failed: %w\n%s", err, string(output))
	}

	fmt.Printf("  Published %s to Feishu\n", title)
	return nil
}

// PublishAllOutputs publishes all .md output artifacts for a gate.
// gateID is the gate identifier (e.g., "gate-0-research"), NOT the display name.
func PublishAllOutputs(cfg FeishuConfig, gateID string, outputs []string, dir string) {
	if !cfg.Enabled {
		return
	}

	for _, out := range outputs {
		if !strings.HasSuffix(out, ".md") {
			continue
		}
		path := filepath.Join(dir, ".forge", "gates", gateID, out)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		if err := PublishMarkdown(cfg, gateID, path, dir); err != nil {
			fmt.Fprintf(os.Stderr, "  Feishu publish failed for %s: %v\n", out, err)
		}
	}
}
