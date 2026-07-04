package forgedata

import (
	"errors"
	"os"
	"path/filepath"
)

// Project 描述一个 forge 项目的"双根"身份：
//   - DataDir  用户级数据 home = ~/.forge/projects/<key>/ （runtime state）
//   - ConfigDir 项目级配置 = <cwd>/.forge/                （pipeline/protocol/CLAUDE.md/hooks）
//
// 双根决策（docs/plans/refactor-data-home.md §1.1）：runtime state 进用户级（state.json/tasks/
// gates/checklog/toollog/act/sessions/quarantine/stamps/hazards/reviews/experience/
// active-task-ref/.task-verify-throttle.last），项目配置留项目级（pipeline.yml/protocol.yml/
// CLAUDE.md/hooks/—— git tracked + user-editable + task-guard 豁免）。
type Project struct {
	Key       string // hash12 of .git common dir
	DataDir   string // ~/.forge/projects/<key>/  (or FORGE_DATA_HOME override)
	ConfigDir string // <cwd>/.forge/             (project-level config)
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
		DataDir:   RootDir(key),
		ConfigDir: configDir,
	}, nil
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

// StatePath returns DataDir/state.json
func (p *Project) StatePath() string { return filepath.Join(p.DataDir, "state.json") }

// HooksDir returns DataDir/hooks/ (项目级 ConfigHooksDir 不同——见下)
func (p *Project) HooksDir() string { return filepath.Join(p.DataDir, "hooks") }

// HookScriptPath returns DataDir/hooks/<name>
func (p *Project) HookScriptPath(name string) string { return filepath.Join(p.DataDir, "hooks", name) }

// TasksDir
func (p *Project) TasksDir() string { return filepath.Join(p.DataDir, "tasks") }

// TaskStatePath returns DataDir/tasks/<ref>.json
func (p *Project) TaskStatePath(ref string) string { return filepath.Join(p.DataDir, "tasks", ref+".json") }

// GatesDir
func (p *Project) GatesDir() string { return filepath.Join(p.DataDir, "gates") }

// GateDir returns DataDir/gates/<id>/
func (p *Project) GateDir(gateID string) string { return filepath.Join(p.DataDir, "gates", gateID) }

// GateStatusPath returns DataDir/gates/<id>/status.json
func (p *Project) GateStatusPath(gateID string) string {
	return filepath.Join(p.DataDir, "gates", gateID, "status.json")
}

// ExperienceDir
func (p *Project) ExperienceDir() string { return filepath.Join(p.DataDir, "experience") }

// ExperienceProposedDir
func (p *Project) ExperienceProposedDir() string {
	return filepath.Join(p.DataDir, "experience", "proposed")
}

// ReviewsDir
func (p *Project) ReviewsDir() string { return filepath.Join(p.DataDir, "reviews") }

// HazardsDir
func (p *Project) HazardsDir() string { return filepath.Join(p.DataDir, "hazards") }

// HazardsEventsPath returns DataDir/hazards/events.jsonl
func (p *Project) HazardsEventsPath() string {
	return filepath.Join(p.DataDir, "hazards", "events.jsonl")
}

// HazardsConfirmPath returns DataDir/hazards/<fp>.json
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

// ActDir
func (p *Project) ActDir() string { return filepath.Join(p.DataDir, "act") }

// ActConclusionsPath
func (p *Project) ActConclusionsPath() string {
	return filepath.Join(p.DataDir, "act", "conclusions.jsonl")
}

// StampsDir
func (p *Project) StampsDir() string { return filepath.Join(p.DataDir, "stamps") }

// StampPath returns DataDir/stamps/<branch>.stamp
func (p *Project) StampPath(branch string) string {
	return filepath.Join(p.DataDir, "stamps", branch+".stamp")
}

// SessionsDir
func (p *Project) SessionsDir() string { return filepath.Join(p.DataDir, "sessions") }

// SessionPath returns DataDir/sessions/<sid>.json
func (p *Project) SessionPath(sid string) string {
	return filepath.Join(p.DataDir, "sessions", sid+".json")
}

// SessionsLogPath returns DataDir/sessions.jsonl
func (p *Project) SessionsLogPath() string { return filepath.Join(p.DataDir, "sessions.jsonl") }

// SessionFilePath returns DataDir/session.json (legacy single-session)
func (p *Project) SessionFilePath() string { return filepath.Join(p.DataDir, "session.json") }

// ActiveTaskRefPath (legacy single-file)
func (p *Project) ActiveTaskRefPath() string {
	return filepath.Join(p.DataDir, "active-task-ref")
}

// ActiveTaskRefSessionPath returns DataDir/active-task-ref-<sid> (session-scoped)
func (p *Project) ActiveTaskRefSessionPath(sid string) string {
	return filepath.Join(p.DataDir, "active-task-ref-"+sid)
}

// ActiveTaskRefGlob returns DataDir/active-task-ref* (covers legacy + session-scoped)
func (p *Project) ActiveTaskRefGlob() string { return filepath.Join(p.DataDir, "active-task-ref*") }

// QuarantineDir
func (p *Project) QuarantineDir() string { return filepath.Join(p.DataDir, "quarantine") }

// TaskVerifyThrottleStamp
func (p *Project) TaskVerifyThrottleStamp() string {
	return filepath.Join(p.DataDir, ".task-verify-throttle.last")
}

// ---- Project-config accessor（p.ConfigDir 下，仍项目级 .forge/）----

// PipelineYAMLPath returns ConfigDir/pipeline.yml
func (p *Project) PipelineYAMLPath() string { return filepath.Join(p.ConfigDir, "pipeline.yml") }

// ProtocolYAMLPath returns ConfigDir/protocol.yml
func (p *Project) ProtocolYAMLPath() string { return filepath.Join(p.ConfigDir, "protocol.yml") }

// CLAUDEMDPath returns ConfigDir/CLAUDE.md
func (p *Project) CLAUDEMDPath() string { return filepath.Join(p.ConfigDir, "CLAUDE.md") }

// ConfigHooksDir returns ConfigDir/hooks/
func (p *Project) ConfigHooksDir() string { return filepath.Join(p.ConfigDir, "hooks") }

// ConfigHookScriptPath returns ConfigDir/hooks/<name>
func (p *Project) ConfigHookScriptPath(name string) string {
	return filepath.Join(p.ConfigDir, "hooks", name)
}
