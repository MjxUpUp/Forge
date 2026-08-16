package protocol

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// pathFor resolves the effective protocol.yml path for project root dir:
//   - <dir>/.forge/protocol.yml when that FILE exists — the team-shared override
//     layer (git tracked, user-editable, written by `forge init --project`);
//   - otherwise the user-level DataDir copy (the zero-project-write default).
//
// An error is returned when the DataDir cannot be resolved (FORGE_DATA_HOME unset
// AND the user home unavailable): falling back to a bare "protocol.yml" would write
// into the process cwd — a silent mis-location that previously stranded the file
// wherever forge happened to run.
//
// pathFor 解析项目根 dir 的生效 protocol.yml 路径：
//   - <dir>/.forge/protocol.yml 文件存在时——团队共享覆盖层（git tracked、
//     用户可改、由 `forge init --project` 写入）；
//   - 否则用户级 DataDir 副本（零项目写入默认）。
//
// DataDir 无法解析时（FORGE_DATA_HOME 未设且用户主目录不可用）返回错误：
// 若回落成裸 "protocol.yml" 会写进进程 cwd——静默错位，文件丢在 forge 恰好
// 运行的目录。
func pathFor(dir string) (string, error) {
	projectLevel := filepath.Join(dir, ".forge", "protocol.yml")
	if info, err := os.Stat(projectLevel); err == nil && !info.IsDir() {
		return projectLevel, nil
	}
	dd, err := dataDirFor(dir)
	if err != nil {
		return ``, err
	}
	return filepath.Join(dd, "protocol.yml"), nil
}

// dataDirFor resolves the user-level DataDir for dir, failing fast when
// forgedata.DataDirFor returns "" (GlobalHome failed) instead of letting callers
// build relative paths.
//
// dataDirFor 解析 dir 的用户级 DataDir；forgedata.DataDirFor 返 ""（GlobalHome
// 失败）时 fail-fast，不让调用方拼出相对路径。
func dataDirFor(dir string) (string, error) {
	dd := forgedata.DataDirFor(dir)
	if dd == `` {
		return ``, fmt.Errorf("protocol: cannot resolve user-level DataDir (FORGE_DATA_HOME unset and user home unavailable); refusing to use a relative protocol.yml path")
	}
	return dd, nil
}

// DataDirPath returns the user-level DataDir copy path (<DataDir>/protocol.yml)
// regardless of any project-level override — for migration code that must write
// the DataDir copy while a project-level file still exists.
//
// DataDirPath 返回用户级 DataDir 副本路径（<DataDir>/protocol.yml），不问项目级
// 覆盖——供迁移代码在项目级文件仍存在时写 DataDir 副本。
func DataDirPath(dir string) (string, error) {
	dd, err := dataDirFor(dir)
	if err != nil {
		return ``, err
	}
	return filepath.Join(dd, "protocol.yml"), nil
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
	path, err := pathFor(dir)
	if err != nil {
		return nil, err
	}
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

// EnsureDefault makes sure an effective protocol.yml exists for dir, writing
// DefaultProtocol ONLY when the file is missing. A parse-corrupt file is never
// silently overwritten: it is first renamed aside to <path>.corrupt-<ts> with a
// stderr warning, then defaults are written. A valid existing file (project-level
// override or DataDir copy) is left untouched. Init/sync should call this instead
// of "Load fails → Save", which clobbers user-broken files with defaults and
// destroys the evidence.
//
// EnsureDefault 确保 dir 有生效的 protocol.yml：仅在文件缺失时写 DefaultProtocol。
// 解析失败的文件绝不静默覆盖——先改名备份为 <path>.corrupt-<ts> 并 stderr 告警，
// 再写默认值。已存在且合法的文件（项目级覆盖或 DataDir 副本）原样保留。
// init/sync 应调本函数代替「Load 失败 → Save」——后者会用默认值盖掉用户改坏的
// 文件，销毁现场。
func EnsureDefault(dir string) error {
	path, err := pathFor(dir)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return saveTo(path, DefaultProtocol())
		}
		return fmt.Errorf("failed to read protocol.yml: %w", err)
	}
	var p Protocol
	if err := yaml.Unmarshal(data, &p); err == nil {
		return nil // 合法文件——保留（含用户改动）
	}
	corrupt := fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
	if rerr := os.Rename(path, corrupt); rerr != nil {
		return fmt.Errorf("backup corrupt protocol.yml: %w", rerr)
	}
	fmt.Fprintf(os.Stderr, "warn: protocol.yml 解析失败，已备份到 %s，写入默认配置\n", corrupt)
	return saveTo(path, DefaultProtocol())
}

// SaveDataDir writes the protocol explicitly to the user-level DataDir copy,
// ignoring any project-level override — used by migration code that must create
// the DataDir copy while the project-level file still exists.
//
// SaveDataDir 显式把 protocol 写到用户级 DataDir 副本，无视项目级覆盖——供迁移
// 代码在项目级文件仍存在时创建 DataDir 副本。
func SaveDataDir(dir string, p *Protocol) error {
	path, err := DataDirPath(dir)
	if err != nil {
		return err
	}
	return saveTo(path, p)
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
