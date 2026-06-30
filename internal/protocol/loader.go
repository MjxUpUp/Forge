package protocol

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Load reads .forge/protocol.yml from the project directory.
func Load(dir string) (*Protocol, error) {
	path := filepath.Join(dir, ".forge", "protocol.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("protocol.yml not found: run 'forge init' first")
		}
		return nil, fmt.Errorf("failed to read protocol.yml: %w", err)
	}
	var p Protocol
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("failed to parse protocol.yml: %w", err)
	}
	return &p, nil
}

// Save writes the protocol to .forge/protocol.yml.
func Save(dir string, p *Protocol) error {
	forgeDir := filepath.Join(dir, ".forge")
	if err := os.MkdirAll(forgeDir, 0755); err != nil {
		return fmt.Errorf("failed to create .forge directory: %w", err)
	}
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal protocol: %w", err)
	}
	path := filepath.Join(forgeDir, "protocol.yml")
	return os.WriteFile(path, data, 0644)
}
