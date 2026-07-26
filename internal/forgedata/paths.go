package forgedata

import (
	"errors"
	"os"
	"path/filepath"
)

// Project 描述一个 forge 项目的"三根"身份：
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

// 配置目录的目录名常量
const configDirName = ".forge"

// ErrNoForgeConfig: project-level `.forge/` 不存在（项目未 init）
var ErrNoForgeConfig = errors.New(`forgedata: project-level .forge/ does not exist; run ` + "`forge init` first")

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
			// 存在但不是 dir（比如遗留下的 file），继续向上
		}
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

// Ensure 创建 DataDir（含子目录）并 stamp .migration-meta.json。
//
// 仅处理 DataDir；ConfigDir 是项目 .forge/，由 cli init 的责任。
func (p *Project) Ensure() error {
	if err := os.MkdirAll(p.DataDir, 0755); err != nil {
		return err
	}
	return p.ensureMeta()
}

// ensureMeta stamp DataDir/.migration-meta.json （schema_version 字段，避免后续读时混淆）。
func (p *Project) ensureMeta() error {
	metaPath := p.MetaPath()
	if _, err := os.Stat(metaPath); err == nil {
		return nil // 已有，不覆写
	}
	// 简版写：bytes 后续 Stage 1 commit E 扩到正式 JSON
	return os.WriteFile(metaPath, []byte(`{"schema_version":1}`+"\n"), 0644)
}

// ---- Runtime state accessor（全部 p.DataDir 下）----

// MetaPath 返回 DataDir/.migration-meta.json
func (p *Project) MetaPath() string { return filepath.Join(p.DataDir, ".migration-meta.json") }

// TasksDir 返回 DataDir/tasks
func (p *Project) TasksDir() string { return filepath.Join(p.DataDir, "tasks") }

// TaskStatePath 返回 DataDir/tasks/<ref>.json
func (p *Project) TaskStatePath(ref string) string {
	return filepath.Join(p.DataDir, "tasks", ref+".json")
}

// GatesDir 返回 DataDir/gates
func (p *Project) GatesDir() string { return filepath.Join(p.DataDir, "gates") }

// GateDir 返回 DataDir/gates/<id>/
func (p *Project) GateDir(gateID string) string { return filepath.Join(p.DataDir, "gates", gateID) }

// GateStatusPath 返回 DataDir/gates/<id>/status.json
func (p *Project) GateStatusPath(gateID string) string {
	return filepath.Join(p.DataDir, "gates", gateID, "status.json")
}

// GateArtifactPath returns DataDir/gates/<id>/<out>（gate 运行产物，如 feishu 报告附件）
func (p *Project) GateArtifactPath(gateID, out string) string {
	return filepath.Join(p.DataDir, "gates", gateID, out)
}

// HazardsDir 返回 DataDir/hazards
func (p *Project) HazardsDir() string { return filepath.Join(p.DataDir, "hazards") }

// HazardsEventsPath 返回 DataDir/hazards/events.jsonl
func (p *Project) HazardsEventsPath() string {
	return filepath.Join(p.DataDir, "hazards", "events.jsonl")
}

// HazardsConfirmPath 返回 DataDir/hazards/<fp>.json
func (p *Project) HazardsConfirmPath(fp string) string {
	return filepath.Join(p.DataDir, "hazards", fp+".json")
}

// ChecklogPath returns DataDir/checklog.jsonl（主）
func (p *Project) ChecklogPath() string { return filepath.Join(p.DataDir, "checklog.jsonl") }

// ChecklogGlob returns DataDir/checklog*.jsonl（含归档）
func (p *Project) ChecklogGlob() string { return filepath.Join(p.DataDir, "checklog*.jsonl") }

// ToollogPath returns DataDir/toollog.jsonl（主）
func (p *Project) ToollogPath() string { return filepath.Join(p.DataDir, "toollog.jsonl") }

// ToollogGlob returns DataDir/toollog*.jsonl（含归档）
func (p *Project) ToollogGlob() string { return filepath.Join(p.DataDir, "toollog*.jsonl") }

// ActDir 返回 DataDir/act
func (p *Project) ActDir() string { return filepath.Join(p.DataDir, "act") }

// ActConclusionsPath 返回 DataDir/act/conclusions.jsonl
func (p *Project) ActConclusionsPath() string {
	return filepath.Join(p.DataDir, "act", "conclusions.jsonl")
}

// StampsDir 返回 DataDir/stamps
func (p *Project) StampsDir() string { return filepath.Join(p.DataDir, "stamps") }

// StampPath 返回 DataDir/stamps/<branch>.stamp
func (p *Project) StampPath(branch string) string {
	return filepath.Join(p.DataDir, "stamps", branch+".stamp")
}

// SessionsDir 返回 DataDir/sessions
func (p *Project) SessionsDir() string { return filepath.Join(p.DataDir, "sessions") }

// SessionPath 返回 DataDir/sessions/<sid>.json
func (p *Project) SessionPath(sid string) string {
	return filepath.Join(p.DataDir, "sessions", sid+".json")
}

// SessionsLogPath 返回 DataDir/sessions.jsonl
func (p *Project) SessionsLogPath() string { return filepath.Join(p.DataDir, "sessions.jsonl") }

// SessionFilePath 返回 DataDir/session.json（legacy single-session）
func (p *Project) SessionFilePath() string { return filepath.Join(p.DataDir, "session.json") }

// ActiveTaskRefPath 返回 DataDir/active-task-ref（legacy single-file）
func (p *Project) ActiveTaskRefPath() string {
	return filepath.Join(p.DataDir, "active-task-ref")
}

// ActiveTaskRefSessionPath 返回 DataDir/active-task-ref-<sid>（session-scoped）
func (p *Project) ActiveTaskRefSessionPath(sid string) string {
	return filepath.Join(p.DataDir, "active-task-ref-"+sid)
}

// ActiveTaskRefGlob 返回 DataDir/active-task-ref*（覆盖 legacy 与 session-scoped）
func (p *Project) ActiveTaskRefGlob() string { return filepath.Join(p.DataDir, "active-task-ref*") }

// ---- Project-config accessor（p.ConfigDir 下，仍项目级 .forge/）----

// ProtocolYAMLPath 返回 ConfigDir/protocol.yml
func (p *Project) ProtocolYAMLPath() string { return filepath.Join(p.ConfigDir, "protocol.yml") }

// CLAUDEMDPath 返回 ConfigDir/CLAUDE.md
func (p *Project) CLAUDEMDPath() string { return filepath.Join(p.ConfigDir, "CLAUDE.md") }
