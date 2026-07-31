package forgedata

import (
	"errors"
	"os"
	"path/filepath"
)

// Project describes the three-root identity of a forge project:
//   - GitRoot   git working tree root = base for `git -C` ops (used by GetHeadCommit/IsGitRepo/taskcontext.Detect)
//   - DataDir   user-level data home = ~/.forge/projects/<key>/  (runtime state)
//   - ConfigDir project-level config = <cwd>/.forge/             (protocol/CLAUDE.md/hooks)
//
// Why three roots: DataDir is a hash-derived user-level path independent of the git
// working tree location; git ops (rev-parse/diff) must use GitRoot; ConfigDir is the
// .forge ancestor found by walking up, usually equal to GitRoot but not guaranteed
// (.forge may live in a subdirectory). Carried independently; callers pick by purpose.
//
// Two-root decision (docs/plans/refactor-data-home.md §1.1): runtime state goes user-level
// (tasks/gates/checklog/toollog/act/sessions/quarantine/stamps/hazards/reviews/experience/
// active-task-ref/.task-verify-throttle.last), project config stays project-level
// (protocol.yml/CLAUDE.md/hooks — git tracked + user-editable + task-guard exempt).
//
// Project 描述一个 forge 项目的「三根」身份：
//   - GitRoot   git working tree 根 = `git -C` 操作基准（GetHeadCommit/IsGitRepo/taskcontext.Detect 用）
//   - DataDir   用户级数据 home = ~/.forge/projects/<key>/ （runtime state）
//   - ConfigDir 项目级配置 = <cwd>/.forge/                （protocol/CLAUDE.md/hooks）
//
// 三根必要性：DataDir 是 hash 派生的用户级路径，与 git working tree 物理位置无关；
// git 操作（rev-parse/diff）必须用 GitRoot；ConfigDir 是 walk-up 找到的 .forge 父目录，
// 通常等于 GitRoot 但不保证（.forge 可能在子目录）。三者独立携带，caller 按用途取。
//
// 双根决策（docs/plans/refactor-data-home.md §1.1）：runtime state 进用户级（tasks/
// gates/checklog/toollog/act/sessions/quarantine/stamps/hazards/reviews/experience/
// active-task-ref/.task-verify-throttle.last），项目配置留项目级（protocol.yml/
// CLAUDE.md/hooks—— git tracked + user-editable + task-guard 豁免）。
type Project struct {
	Key       string // .git common dir 的 hash12
	GitRoot   string // git working tree root（git -C 操作基准）
	DataDir   string // ~/.forge/projects/<key>/（或 FORGE_DATA_HOME 覆盖）
	ConfigDir string // <cwd>/.forge/（项目级 config）
}

// Directory-name constant for the config directory.
//
// 配置目录的目录名常量
const configDirName = ".forge"

// ErrNoForgeConfig: project-level `.forge/` does not exist (project not initialized).
//
// ErrNoForgeConfig: project-level `.forge/` 不存在（项目未 init）
var ErrNoForgeConfig = errors.New(`forgedata: project-level .forge/ does not exist; run ` + "`forge init` first")

// ProjectFor derives Key from cwd, plus the DataDir / ConfigDir two roots.
//
// Errors:
//   - ErrNotInGitRepo: cwd is not a git repo
//   - ErrKeyDerivation: .git file corrupted (F1 fix refines ErrInvalidGitFile)
//   - ErrNoForgeConfig: project not initialized (no .forge/)
//
// ProjectFor 从 cwd 推 Key，DataDir / ConfigDir 双根。
//
// 错误：
//   - ErrNotInGitRepo: cwd 非 git repo
//   - ErrKeyDerivation: .git file 损坏（F1 修复中细分 ErrInvalidGitFile）
//   - ErrNoForgeConfig: 项目未 init (无 .forge/)
func ProjectFor(cwd string) (*Project, error) {
	cwdAbs, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}

	key, err := Key(cwdAbs)
	if err != nil {
		return nil, err
	}

	// Walk up to find the ancestor containing `.forge/`, but do not cross the gitRoot
	// boundary (prevents false hit on ~/.forge).
	//
	// walk-up 找含 `.forge/` 的祖先，但不超过 gitRoot 边界（防 ~/.forge 漏检）
	gitRoot := FindGitRoot(cwdAbs)
	if gitRoot == "" {
		return nil, ErrNotInGitRepo
	}
	configDir, err := findForgeConfigDir(cwdAbs, gitRoot)
	if err != nil {
		return nil, err
	}

	return &Project{
		Key:       key,
		GitRoot:   gitRoot,
		DataDir:   RootDir(key),
		ConfigDir: configDir,
	}, nil
}

// DataDirFor returns the runtime-state DataDir for root, without a full Project (no .forge/ needed):
// git projects get the user-level ~/.forge/projects/<key>/, non-git projects fall back to <root>/.forge/,
// so hooks firing outside a forge project can still record runtime state.
//
// It depends only on git-derived Key (needs .git, not .forge/) — the resolution stays stable under
// the MkdirAll side effect: when some store's MkdirAll creates <root>/.forge/ on the fallback path,
// re-resolution must not flip to DataDir (that was a stateful bug silently dropping checklog Records).
// Stores (checklog / task state) should prefer this helper over re-deriving on their own.
//
// DataDirFor 返回 root 的 runtime-state DataDir，无需完整 Project（不要 .forge/）：
// git 项目返用户级 ~/.forge/projects/<key>/，非 git 项目回落 <root>/.forge/，
// 让 hook 在 forge 项目之外触发时仍能记录 runtime state。
//
// 仅依赖 git 的 Key（需 .git，不需要 .forge/）——解析在 MkdirAll 副作用下保持稳定：
// 某 store 的 MkdirAll 在 fallback 路径上创建 <root>/.forge/ 时，不得让重新解析翻到
// DataDir（那个静默丢弃 checklog Records 的 stateful bug）。store（checklog / task state）
// 优先用此函数而非自己重新推导。
func DataDirFor(root string) string {
	if key, err := Key(root); err == nil {
		return RootDir(key)
	}
	return filepath.Join(root, configDirName)
}

// findForgeConfigDir walks up to find the ancestor containing `.forge/`, without crossing stopAt.
// Returns ErrNoForgeConfig if not found (project not initialized).
//
// Design: the boundary stopAt = gitRoot. .forge must live in the same git repo as .git (semantic
// sanity); walking beyond gitRoot (e.g. up to ~/.forge) is forbidden (false-hit risk + nested-repo confusion).
//
// findForgeConfigDir walk-up 找含 `.forge/` 的祖先，但不超过 stopAt 边界。
// 找不到返 ErrNoForgeConfig（项目未 init）。
//
// 设计：边界 stopAt = gitRoot。.forge 必须与 .git 在同一 git repo 内（语义合理）；超出 gitRoot
// 的 walk-up（如到用户 ~/.forge）应被禁止（漏检风险 + 多 repo 嵌套混淆）。
func findForgeConfigDir(cwd, stopAt string) (string, error) {
	d := filepath.Clean(cwd)
	stop := filepath.Clean(stopAt)
	for {
		candidate := filepath.Join(d, configDirName)
		if info, err := os.Stat(candidate); err == nil {
			if info.IsDir() {
				return candidate, nil
			}
			// Exists but is not a dir (e.g. a leftover file); keep walking up.
			//
			// 存在但不是 dir（比如遗留下的 file），继续向上
		}
		// Stop at the gitRoot boundary (includes one lookup round at gitRoot itself).
		//
		// 到 gitRoot 边界停止（含 gitRoot 自身一轮的 lookup）
		if d == stop {
			return "", ErrNoForgeConfig
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", ErrNoForgeConfig
		}
		d = parent
	}
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

// ActConclusionsPath returns DataDir/act/conclusions.jsonl
//
// ActConclusionsPath 返回 DataDir/act/conclusions.jsonl
func (p *Project) ActConclusionsPath() string {
	return filepath.Join(p.DataDir, "act", "conclusions.jsonl")
}
