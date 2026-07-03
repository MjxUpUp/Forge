package skillgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeMDCommonErrorsIncludesReviewBlock guards the common-errors table
// against losing the mandatory-review guidance. `forge task complete` blocks on
// a pending mandatory review ("Pending mandatory review detected"); agents
// relying on CLAUDE.md alone need the resolution path.
func TestClaudeMDCommonErrorsIncludesReviewBlock(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "Pending mandatory review detected") {
		t.Error("CLAUDE.md common-errors table missing 'Pending mandatory review detected' row")
	}
	if !strings.Contains(section, "forge experience accept") {
		t.Error("CLAUDE.md review error row must reference 'forge experience accept'")
	}
}

// TestClaudeMDCommonErrorsIncludesTestCoverage guards the common-errors table
// documents the task-verify test-coverage gate. Since v0.22 the verify gate
// enforces CLAUDE.md rule 4 ("测试伴随变更") — agents hitting it need the
// resolution path (add a test, or FORGE_TEST_COVERAGE=disable escape hatch)
// surfaced in CLAUDE.md, otherwise the gate looks opaque.
func TestClaudeMDCommonErrorsIncludesTestCoverage(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "without a corresponding test") {
		t.Error("CLAUDE.md common-errors table missing test-coverage gate row")
	}
	if !strings.Contains(section, "FORGE_TEST_COVERAGE=disable") {
		t.Error("CLAUDE.md test-coverage row must surface the escape hatch")
	}
}

// TestClaudeMDCommonErrorsIncludesRetention guards the common-errors table
// documents log retention. task start auto-prunes over-age checklog/toollog
// archives + completed task files per FORGE_LOG_RETENTION_DAYS; agents/users
// seeing "trace/老任务历史消失" need the env knob surfaced so silent pruning
// isn't opaque (and to flag the act rebuild interaction).
func TestClaudeMDCommonErrorsIncludesRetention(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "trace/老任务历史消失") {
		t.Error("CLAUDE.md common-errors table missing retention row")
	}
	if !strings.Contains(section, "FORGE_LOG_RETENTION_DAYS") {
		t.Error("CLAUDE.md retention row must surface the FORGE_LOG_RETENTION_DAYS knob")
	}
}

// TestClaudeMDDocumentsCommitTiming guards against the trap where agents commit
// AFTER `forge task complete`: complete clears the active task ref, so a
// post-complete source commit gets quarantined by file-sentinel. CLAUDE.md must
// state the correct order (commit before complete) and the chore/*-commit
// recovery path. This was a real trap hit in a DevWorkbench session.
func TestClaudeMDDocumentsCommitTiming(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "提交时机") {
		t.Error("CLAUDE.md missing commit-timing section (commit must precede complete)")
	}
	if !strings.Contains(section, "chore/*-commit") {
		t.Error("CLAUDE.md missing chore/*-commit recovery path for post-complete commits")
	}
}

// TestClaudeMDMatchesActualGuardBehavior guards against documenting fabricated
// thresholds or the wrong verb (deny vs warn). The task-guard and bash-guard
// hooks only WARN on source changes without an active task — they never deny
// Write/Edit (only .forge/* self-protection fails). And NO guard checks line
// count, so a ">10 行" threshold is fabricated and misleads agents.
func TestClaudeMDMatchesActualGuardBehavior(t *testing.T) {
	section := buildForgeSection(true)

	if strings.Contains(section, "非平凡变更（>10 行）") {
		t.Error("CLAUDE.md documents fabricated '>10 行' threshold — no guard checks line count")
	}
	if strings.Contains(section, "denied by task-guard") {
		t.Error("CLAUDE.md says 'denied by task-guard' — task-guard only WARNs source edits (never denies)")
	}
	if strings.Contains(section, "denied by bash-guard") {
		t.Error("CLAUDE.md says 'denied by bash-guard' — bash-guard only WARNs (never denies)")
	}
	if !strings.Contains(section, "experience resolve") {
		t.Error("CLAUDE.md mandatory-review row missing 'forge experience resolve' fallback")
	}
}

// TestClaudeMDDocumentsAuxHooks guards that the remaining auxiliary hook
// (session-health) appears in the security section, and that the sunk
// judgmental hooks (read-check/scope-guard/clone-check) are NOT listed as
// runtime hooks anymore — they moved to forge-quality's Red Flags text per the
// layered noise treatment.
func TestClaudeMDDocumentsAuxHooks(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "辅助检查") {
		t.Error("CLAUDE.md security section missing auxiliary hooks summary")
	}
	for _, gone := range []string{"read-check（", "scope-guard（", "clone-check（"} {
		if strings.Contains(section, gone) {
			t.Errorf("CLAUDE.md still lists sunk hook %q as runtime — should be gone from aux-checks line", gone)
		}
	}
}

// TestClaudeMDDocumentsSkillScan guards that CLAUDE.md documents the skill-scan
// SessionStart hook (advisory global skill audit). Agents reading CLAUDE.md must
// know skill-scan exists — it scans ~/.claude/skills at session start, covering
// skills that entered outside the install gate (manual clone/junction/git pull).
func TestClaudeMDDocumentsSkillScan(t *testing.T) {
	section := buildForgeSection(true)
	if !strings.Contains(section, "skill-scan") {
		t.Error("CLAUDE.md security section missing skill-scan hook")
	}
	if !strings.Contains(section, "SessionStart") {
		t.Error("CLAUDE.md must document skill-scan as a SessionStart hook")
	}
}

// TestClaudeMDDocumentsTaskAbort guards that CLAUDE.md documents the task abort
// command. Without an escape hatch, a task that can never progress (e.g. started
// in a non-git project, or abandoned mid-flight) lingers as a "ghost" task,
// polluting `task list` and tripping the task-verify Stop hook on every session
// end. Agents relying on CLAUDE.md need to know `forge task abort` exists — the
// 2026-06-16 code-knowledge-base session got stuck precisely because no abort
// path was documented and `.forge/*` self-protection blocks manual cleanup.
func TestClaudeMDDocumentsTaskAbort(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "中止任务") {
		t.Error("CLAUDE.md missing task-abort section")
	}
	if !strings.Contains(section, "forge task abort --ref <ref>") {
		t.Error("CLAUDE.md task-abort section must show the `forge task abort --ref <ref>` command")
	}
}

// TestClaudeMDTaskVerifyIsAdvisory guards the advisory rewrite in the abort
// section: it must not claim the Stop hook auto-passes after 3 failures (that
// counter no longer exists — task-verify is advisory and never blocks).
func TestClaudeMDTaskVerifyIsAdvisory(t *testing.T) {
	section := buildForgeSection(true)

	if strings.Contains(section, "连续 3 次失败") {
		t.Error("CLAUDE.md still references obsolete '连续 3 次失败' force-pass (task-verify is advisory)")
	}
	if !strings.Contains(section, "advisory") {
		t.Error("CLAUDE.md must document task-verify as advisory")
	}
}

// TestClaudeMDCompileAssertionRulesAdvisory guards the v0.25 advisory rewrite of
// the basic-rules section: the compile + assertion rules must document the hooks
// as advisory (agent self-checks), NOT "auto-check" — auto-compile.sh and
// assertion-check.sh no longer block. This is the CLAUDE.md surface of the
// embed.go advisory change, and carries the tech-stack-agnostic / loop-engineering
// intent (forge reminds; the agent owns the actual compile/assertion verdict).
func TestClaudeMDCompileAssertionRulesAdvisory(t *testing.T) {
	section := buildForgeSection(true)

	// New advisory wording: hooks only remind, the agent self-checks.
	if !strings.Contains(section, "auto-compile hook 仅 advisory 提醒") {
		t.Error("CLAUDE.md compile rule must document auto-compile as advisory (v0.25)")
	}
	if !strings.Contains(section, "assertion-check hook 检测到弱化仅 advisory 提醒") {
		t.Error("CLAUDE.md assertion rule must document assertion-check as advisory (v0.25)")
	}
	// The old "hook 自动检查" wording implied blocking enforcement — must be gone.
	if strings.Contains(section, "hook 自动检查") {
		t.Error("CLAUDE.md still uses obsolete 'hook 自动检查' (hooks are advisory now, not blocking)")
	}
	// Gate-order section: task-implement must not claim it "auto-checks compile+assertion".
	if strings.Contains(section, "自动检查编译+断言") {
		t.Error("CLAUDE.md task-implement row still claims '自动检查编译+断言' (advisory now)")
	}
}

// TestGenerateAgentsMD guards the cross-agent AGENTS.md generator. AGENTS.md is
// read by codex/cursor/copilot/windsurf/cline (detect.go treats it as a codex
// signal), so it must carry the agent-agnostic forge CLI/MCP surface — NOT the
// Claude-only slash commands — and preserve user content outside the FORGE
// markers on re-run (same idempotent section-replace contract as CLAUDE.md).
func TestGenerateAgentsMD(t *testing.T) {
	dir := t.TempDir()

	if err := GenerateAgentsMD(dir); err != nil {
		t.Fatalf(`GenerateAgentsMD: %v`, err)
	}
	path := filepath.Join(dir, `AGENTS.md`)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(`AGENTS.md not written: %v`, err)
	}
	got := string(b)

	if !strings.Contains(got, `Forge 质量协议`) {
		t.Error(`AGENTS.md missing Forge protocol header`)
	}
	if !strings.Contains(got, forgeSectionStart) || !strings.Contains(got, forgeSectionEnd) {
		t.Error(`AGENTS.md missing FORGE section markers`)
	}
	// Cross-agent surface, not Claude slash commands.
	if !strings.Contains(got, `通过 forge CLI`) {
		t.Error(`AGENTS.md missing agent-agnostic CLI/MCP surface line`)
	}
	for _, claudeOnly := range []string{`/forge-pipeline`, `/forge-quality`} {
		if strings.Contains(got, claudeOnly) {
			t.Errorf(`AGENTS.md must not carry Claude-only slash command (cross-agent file): %s`, claudeOnly)
		}
	}

	// Idempotent: user content outside markers survives a re-run; the marked
	// Forge section is replaced in place.
	userContent := `# Project notes

This is user-maintained content outside the Forge section.
`
	seed := userContent + forgeSectionStart + `
## STALE
` + forgeSectionEnd
	if err := os.WriteFile(path, []byte(seed), 0644); err != nil {
		t.Fatalf(`seed AGENTS.md: %v`, err)
	}
	if err := GenerateAgentsMD(dir); err != nil {
		t.Fatalf(`GenerateAgentsMD re-run: %v`, err)
	}
	b2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf(`re-read AGENTS.md: %v`, err)
	}
	got2 := string(b2)
	if !strings.Contains(got2, `This is user-maintained content outside the Forge section.`) {
		t.Error(`AGENTS.md re-run clobbered user content outside FORGE markers`)
	}
	if strings.Contains(got2, `## STALE`) {
		t.Error(`AGENTS.md re-run left stale content inside the marked Forge section`)
	}
}

// TestGenerateClaudeMDCarriesSlashCommands is the symmetric guard: CLAUDE.md is
// Claude-only and must carry /forge-pipeline + /forge-quality, and must NOT
// carry the AGENTS.md cross-agent surface line. Together with TestGenerateAgentsMD
// this locks the forClaude branch of buildForgeSection.
func TestGenerateClaudeMDCarriesSlashCommands(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateClaudeMD(dir); err != nil {
		t.Fatalf(`GenerateClaudeMD: %v`, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, `.claude`, `CLAUDE.md`))
	if err != nil {
		t.Fatalf(`CLAUDE.md not written: %v`, err)
	}
	got := string(b)
	if !strings.Contains(got, `/forge-pipeline`) || !strings.Contains(got, `/forge-quality`) {
		t.Error(`CLAUDE.md missing Claude slash commands (/forge-pipeline, /forge-quality)`)
	}
	if strings.Contains(got, `通过 forge CLI`) {
		t.Error(`CLAUDE.md must not carry the AGENTS.md cross-agent surface line`)
	}
}
