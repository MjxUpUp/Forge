package forgedata

import (
	"errors"
	"os"
	"path/filepath"
)

// Project describes the identity of a forge project:
//   - Key       git: hash of .git common dir; non-git: PathKey ("p"+path hash)
//   - Root      project root (git working-tree root, or the registered path for non-git)
//   - GitRoot   git working tree root = base for `git -C` ops ("" for non-git projects)
//   - DataDir   user-level data home = ~/.forge/projects/<key>/  (runtime state + default config)
//   - ConfigDir config home = <root>/.forge/ when it exists (team mode / legacy),
//               otherwise DataDir (zero-project-write default)
//
// Why the split: DataDir is a hash-derived user-level path independent of the git
// working tree location; git ops (rev-parse/diff) must use GitRoot. After the
// user-level-assets refactor, ALL forge writes default to user level — ConfigDir
// only points at a project-level .forge/ when one already exists (a project-level
// protocol.yml there acts as the team-shared override layer).
//
// Project 描述一个 forge 项目的身份：
//   - Key       git：.git common dir hash；非 git：PathKey（"p"+路径 hash）
//   - Root      项目根（git working tree 根，或非 git 的注册路径）
//   - GitRoot   git working tree 根 = `git -C` 操作基准（非 git 项目为 ""）
//   - DataDir   用户级数据 home = ~/.forge/projects/<key>/（runtime state + 默认配置）
//   - ConfigDir 配置 home = <root>/.forge/（存在时：团队模式/老项目），否则 DataDir
//               （零项目写入默认）
//
// 拆分必要性：DataDir 是 hash 派生的用户级路径，与 git working tree 物理位置无关；
// git 操作（rev-parse/diff）必须用 GitRoot。user-level-assets 重构后 forge 全部写入
// 默认在用户级——ConfigDir 仅在项目级 .forge/ 已存在时指向它（其中的 protocol.yml
// 作为团队共享覆盖层）。
type Project struct {
	Key       string // git: .git common dir hash12；非 git: PathKey
	Root      string // 项目根（git 项目 = GitRoot）
	GitRoot   string // git working tree root（git -C 操作基准；非 git 为 ""）
	DataDir   string // ~/.forge/projects/<key>/（或 FORGE_DATA_HOME 覆盖）
	ConfigDir string // <root>/.forge/（存在时）否则 DataDir
}

// Directory-name constant for the config directory.
//
// 配置目录的目录名常量
const configDirName = ".forge"

// ErrNoForgeConfig: the cwd is not inside a forge project (not registered in the
// global registry and no legacy project-level `.forge/` found). Returned by
// projectroot.Find/FindProject — forgedata.ProjectFor itself is a pure path
// derivation and no longer judges init state.
//
// ErrNoForgeConfig：cwd 不在 forge 项目内（全局注册表无登记且找不到遗留的项目级
// `.forge/`）。由 projectroot.Find/FindProject 返回——forgedata.ProjectFor 本身
// 是纯路径推导，不再判定 init 状态。
var ErrNoForgeConfig = errors.New(`forgedata: not a forge project; run ` + "`forge init` first")

// ProjectFor derives the project identity from cwd. Pure derivation — it does NOT
// check registry membership (that is projectroot's job); any cwd gets a stable
// DataDir so stores (checklog/toollog/...) can record even outside forge projects.
//
// Errors:
//   - ErrKeyDerivation: .git file corrupted (F1 fix refines ErrInvalidGitFile)
//
// ProjectFor 从 cwd 推导项目身份。纯推导——不查注册表成员资格（那是 projectroot
// 的职责）；任意 cwd 都得到稳定 DataDir，让 store（checklog/toollog/...）在
// forge 项目外也能记录。
//
// 错误：
//   - ErrKeyDerivation：.git file 损坏（F1 修复中细分 ErrInvalidGitFile）
func ProjectFor(cwd string) (*Project, error) {
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	gitRoot := FindGitRoot(cwdAbs)
	root := gitRoot
	var key string
	if gitRoot != "" {
		key, err = Key(cwdAbs)
		if err != nil {
			return nil, err
		}
	} else {
		// Non-git project: callers pass the project root (registry-matched by
		// projectroot). Identity = normalized absolute path.
		//
		// 非 git 项目：调用方传入项目根（projectroot 经注册表匹配）。
		// 身份 = 归一化绝对路径。
		root = cwdAbs
		key = PathKey(root)
	}

	dataDir := RootDir(key)
	configDir := filepath.Join(root, configDirName)
	if info, serr := os.Stat(configDir); serr != nil || !info.IsDir() {
		// Zero-project-write default: no project-level .forge/ → config
		// (protocol.yml) lives in the user-level DataDir.
		//
		// 零项目写入默认：无项目级 .forge/ → 配置（protocol.yml）在用户级 DataDir。
		configDir = dataDir
	}

	return &Project{
		Key:       key,
		Root:      root,
		GitRoot:   gitRoot,
		DataDir:   dataDir,
		ConfigDir: configDir,
	}, nil
}

// DataDirFor returns the runtime-state DataDir for root, without a full Project:
// git projects get ~/.forge/projects/<key>/, non-git projects get
// ~/.forge/projects/<path-key>/ — ALWAYS user-level, so stores never write into
// the user's project tree (the pre-refactor fallback created <root>/.forge/).
//
// It depends only on Key/PathKey derivation — the resolution stays stable under
// the MkdirAll side effect (no store may create project-level dirs anymore).
// Stores (checklog / task state) should prefer this helper over re-deriving on their own.
//
// DataDirFor 返回 root 的 runtime-state DataDir，无需完整 Project：
// git 项目返 ~/.forge/projects/<key>/，非 git 项目返 ~/.forge/projects/<path-key>/
// ——始终用户级，store 永不写用户项目树（重构前的回落会创建 <root>/.forge/）。
//
// 仅依赖 Key/PathKey 推导——解析在 MkdirAll 副作用下保持稳定（store 不再
// 创建项目级目录）。store（checklog / task state）优先用此函数而非自己重新推导。
func DataDirFor(root string) string {
	if key, err := Key(root); err == nil {
		return RootDir(key)
	}
	return RootDir(PathKey(root))
}

// Ensure creates DataDir (including subdirs) and stamps .migration-meta.json.
//
// It only handles DataDir; ConfigDir is the project .forge/, owned by cli init.
//
// Ensure 创建 DataDir（含子目录）并 stamp .migration-meta.json。
//
// 仅处理 DataDir；ConfigDir 是项目 .forge/，由 cli init 的责任。
func (p *Project) Ensure() error {
	if err := os.MkdirAll(p.DataDir, 0755); err != nil {
		return err
	}
	return p.ensureMeta()
}

// ensureMeta stamps DataDir/.migration-meta.json (schema_version field, to avoid confusion on later reads).
//
// ensureMeta stamp DataDir/.migration-meta.json （schema_version 字段，避免后续读时混淆）。
func (p *Project) ensureMeta() error {
	metaPath := p.MetaPath()
	if _, err := os.Stat(metaPath); err == nil {
		return nil // 已有，不覆写
	}
	// Minimal bytes version; Stage 1 commit E will extend it to a proper JSON.
	//
	// 简版写：bytes 后续 Stage 1 commit E 扩到正式 JSON
	return os.WriteFile(metaPath, []byte(`{"schema_version":1}`+"\n"), 0644)
}

// ---- Runtime state accessors (all under p.DataDir) ----
//
// Only accessors with real callers live here. The refactor-data-home zombie accessors
// (TasksDir/TaskStatePath/GatesDir/GateDir/GateStatusPath/ChecklogGlob/ToollogPath/ToollogGlob/
// StampsDir/StampPath/SessionsDir/SessionPath/SessionsLogPath/SessionFilePath/ActiveTaskRefPath/
// ActiveTaskRefSessionPath/ActiveTaskRefGlob/ProtocolYAMLPath/CLAUDEMDPath) had zero production
// callers and were deleted — the unsanitized ones (TaskStatePath/StampPath/GateArtifactPath joining caller input
// straight into a path) were a path-traversal hazard if ever revived without sanitize.
// GateArtifactPath and ChecklogPath later re-audited to zero callers and joined the list.
//
// ---- Runtime state accessor（全部 p.DataDir 下）----
//
// 只保留有真实调用方的 accessor。refactor-data-home 遗留的僵尸 accessor
// （清单见上）零生产调用已删除——其中不做 sanitize 的（TaskStatePath/StampPath/GateArtifactPath
// 把调用方输入直接拼进路径）若不经 sanitize 复活即是路径穿越雷。
// GateArtifactPath 与 ChecklogPath 复查零调用方后并入该清单。

// MetaPath returns DataDir/.migration-meta.json
//
// MetaPath 返回 DataDir/.migration-meta.json
func (p *Project) MetaPath() string { return filepath.Join(p.DataDir, ".migration-meta.json") }

// HazardsDir returns DataDir/hazards
//
// HazardsDir 返回 DataDir/hazards
func (p *Project) HazardsDir() string { return filepath.Join(p.DataDir, "hazards") }

// HazardsEventsPath returns DataDir/hazards/events.jsonl
//
// HazardsEventsPath 返回 DataDir/hazards/events.jsonl
func (p *Project) HazardsEventsPath() string {
	return filepath.Join(p.DataDir, "hazards", "events.jsonl")
}

// HazardsConfirmPath returns DataDir/hazards/<fp>.json
//
// HazardsConfirmPath 返回 DataDir/hazards/<fp>.json
func (p *Project) HazardsConfirmPath(fp string) string {
	return filepath.Join(p.DataDir, "hazards", fp+".json")
}

// ActDir returns DataDir/act
//
// ActDir 返回 DataDir/act
func (p *Project) ActDir() string { return filepath.Join(p.DataDir, "act") }

// FreezeDir returns DataDir/freeze
//
// FreezeDir 返回 DataDir/freeze
func (p *Project) FreezeDir() string { return filepath.Join(p.DataDir, "freeze") }

// FreezeStatePath returns DataDir/freeze/state.json
//
// FreezeStatePath 返回 DataDir/freeze/state.json
func (p *Project) FreezeStatePath() string {
	return filepath.Join(p.DataDir, "freeze", "state.json")
}

// ActConclusionsPath returns DataDir/act/conclusions.jsonl
//
// ActConclusionsPath 返回 DataDir/act/conclusions.jsonl
func (p *Project) ActConclusionsPath() string {
	return filepath.Join(p.DataDir, "act", "conclusions.jsonl")
}
