package protocol

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// pathFor resolves the effective protocol.yml path for project root dir:
//   - <dir>/.forge/protocol.yml when that FILE exists — the team-shared override
//     layer (git tracked, user-editable, written by `forge init --project`);
//   - otherwise the user-level DataDir copy (the zero-project-write default).
//
// pathFor 解析项目根 dir 的生效 protocol.yml 路径：
//   - <dir>/.forge/protocol.yml 文件存在时——团队共享覆盖层（git tracked、
//     用户可改、由 `forge init --project` 写入）；
//   - 否则用户级 DataDir 副本（零项目写入默认）。
func pathFor(dir string) string {
	projectLevel := filepath.Join(dir, ".forge", "protocol.yml")
	if info, err := os.Stat(projectLevel); err == nil && !info.IsDir() {
		return projectLevel
	}
	return filepath.Join(forgedata.DataDirFor(dir), "protocol.yml")
}

// ProjectLevelPath returns the team-override path (<dir>/.forge/protocol.yml)
// regardless of existence — used by `forge init --project` to write the override
// explicitly.
//
// ProjectLevelPath 返回团队覆盖路径（<dir>/.forge/protocol.yml），不问存在性——
// 供 `forge init --project` 显式写覆盖层。
func ProjectLevelPath(dir string) string {
	return filepath.Join(dir, ".forge", "protocol.yml")
}

// Load reads the effective protocol.yml for the project (project-level override
// first, then the user-level DataDir copy).
//
// Load 读项目的生效 protocol.yml（项目级覆盖优先，其次用户级 DataDir 副本）。
func Load(dir string) (*Protocol, error) {
	path := pathFor(dir)
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

// Save writes the protocol to the effective location: the project-level override
// when it already exists, otherwise the user-level DataDir copy. It uses
// util.AtomicWrite (temp+rename) rather than a plain os.WriteFile: Load treats a
// YAML parse error as corruption, so a half-written file from a crash or
// concurrent write would make the project permanently unloadable. AtomicWrite
// creates the parent directory itself, so no MkdirAll is needed here.
//
// Save 把 protocol 写到生效位置：项目级覆盖已存在时写它，否则写用户级 DataDir
// 副本。用 util.AtomicWrite（temp+rename）而非裸 os.WriteFile：Load 对 YAML
// 解析错误直接报损坏，崩溃/并发写留下的半文件会让项目永久不可加载。
// AtomicWrite 自建父目录，这里无需 MkdirAll。
func Save(dir string, p *Protocol) error {
	return saveTo(pathFor(dir), p)
}

// SaveProjectLevel writes the protocol explicitly to <dir>/.forge/protocol.yml
// (team-shared override layer, `forge init --project`).
//
// SaveProjectLevel 显式把 protocol 写到 <dir>/.forge/protocol.yml
// （团队共享覆盖层，`forge init --project`）。
func SaveProjectLevel(dir string, p *Protocol) error {
	return saveTo(ProjectLevelPath(dir), p)
}

// saveTo marshals + atomically writes the protocol to an explicit path.
//
// saveTo marshal 并原子写 protocol 到显式路径。
func saveTo(path string, p *Protocol) error {
	data, err := yaml.Marshal(p)
	if err != nil {
		return fmt.Errorf("failed to marshal protocol: %w", err)
	}
	if err := util.AtomicWrite(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write protocol.yml: %w", err)
	}
	return nil
}
