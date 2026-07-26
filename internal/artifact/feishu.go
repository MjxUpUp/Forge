package artifact

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// FeishuConfig holds the feishu publishing configuration.
//
// FeishuConfig 持有 feishu 发布配置。
type FeishuConfig struct {
	SpaceID         string
	ParentNodeToken string
	Enabled         bool
}

// DefaultFeishuConfig reads the config from environment variables.
//
// DefaultFeishuConfig 从环境变量读 config。
func DefaultFeishuConfig() FeishuConfig {
	return FeishuConfig{
		SpaceID:         os.Getenv("FORGE_FEISHU_SPACE_ID"),
		ParentNodeToken: os.Getenv("FORGE_FEISHU_WIKI_NODE"),
		Enabled:         os.Getenv("FORGE_FEISHU_AUTO_PUBLISH") != "false",
	}
}

// PublishMarkdown publishes a .md file to the feishu wiki.
//
// PublishMarkdown 把 .md 文件发布到 feishu wiki。
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

// PublishAllOutputs publishes all .md artifacts of a given gate.
// gateID is the gate identifier (e.g. gate-0-research), not the display name.
//
// PublishAllOutputs 发布某 gate 的全部 .md 产物。
// gateID 是 gate 标识（如 gate-0-research），不是 display name。
func PublishAllOutputs(cfg FeishuConfig, gateID string, outputs []string, p *forgedata.Project) {
	if !cfg.Enabled {
		return
	}

	for _, out := range outputs {
		if !strings.HasSuffix(out, ".md") {
			continue
		}
		path := p.GateArtifactPath(gateID, out)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		// lark-cli uses GitRoot (project root) as its working directory, matching historical behavior (the original dir was the project root).
		//
		// lark-cli 工作目录用 GitRoot（项目根），与历史行为一致（原 dir 即项目根）
		if err := PublishMarkdown(cfg, gateID, path, p.GitRoot); err != nil {
			fmt.Fprintf(os.Stderr, "  Feishu publish failed for %s: %v\n", out, err)
		}
	}
}
