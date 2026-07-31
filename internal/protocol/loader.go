package protocol

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/util"
)

// Load reads .forge/protocol.yml from the project directory.
//
// Load 从项目目录读 .forge/protocol.yml。
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

// Save writes the protocol to .forge/protocol.yml. It uses util.AtomicWrite (temp+rename) rather
// than a plain os.WriteFile: Load treats a YAML parse error as corruption, so a half-written file
// from a crash or concurrent write would make the project permanently unloadable. AtomicWrite
// creates the .forge directory itself, so no MkdirAll is needed here.
//
// Save 把 protocol 写到 .forge/protocol.yml。用 util.AtomicWrite（temp+rename）而非裸
// os.WriteFile：Load 对 YAML 解析错误直接报损坏，崩溃/并发写留下的半文件会让项目永久
// 不可加载。AtomicWrite 自建 .forge 目录，这里无需 MkdirAll。
func Save(dir string, p *Protocol) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal protocol: %w", err)
	}
	path := filepath.Join(dir, ".forge", "protocol.yml")
	if err := util.AtomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write protocol.yml: %w", err)
	}
	return nil
}
