package skillgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

// TestClaudeMDCoversAllWiredHooks is the docs-consistency guard for the security
// section: every hook name wired in ForgeHookSpec (the single source of truth for
// the hook roster, including the Go-native skill-trigger) must appear somewhere in
// the generated CLAUDE.md / AGENTS.md forge section. Root cause this guards: the
// 2026-08 audit found 9 wired hooks (incl. two HARD blockers hazard-guard /
// freeze-guard and the review-stop exit-2 block) absent from the docs — agents hit
// their BLOCKED messages cold with no resolution path. Adding a hook without
// documenting it now fails this test instead of shipping silently.
//
// TestClaudeMDCoversAllWiredHooks 是安全机制段的一致性守卫：ForgeHookSpec（hook
// 名册单一真相源，含 Go 原生 skill-trigger）里接线的每个 hook 名都必须出现在生成的
// CLAUDE.md / AGENTS.md forge 段中。守护的根因：2026-08 审计发现 9 个已接线 hook
// （含 hazard-guard / freeze-guard 两个硬阻断与 review-stop exit-2 阻断）文档缺席
// ——agent 冷撞 BLOCKED 无解法。此后加 hook 不写文档会在这里红，不再静默出仓。
func TestClaudeMDCoversAllWiredHooks(t *testing.T) {
	wired := map[string]bool{}
	for _, groups := range hooks.ForgeHookSpec() {
		for _, g := range groups {
			for _, h := range g.Hooks {
				name := strings.TrimPrefix(h.Command, "forge hook ")
				if name == "" || name == h.Command {
					t.Fatalf("ForgeHookSpec command not in `forge hook <name>` form: %q", h.Command)
				}
				wired[name] = true
			}
		}
	}
	if len(wired) < 15 {
		t.Fatalf("wired hook roster unexpectedly small (%d) — ForgeHookSpec shape changed?", len(wired))
	}
	for _, section := range []struct{ label, body string }{
		{"CLAUDE.md", buildForgeSection(true)},
		{"AGENTS.md", buildForgeSection(false)},
	} {
		for name := range wired {
			// Anchored match: the name must appear in markup form — a bold bullet
			// (`- **name**`) or inline code (`` `name` ``). Bare substring matching
			// would let a future hook named e.g. "review" pass on the strength of
			// "review-stop" already being documented (review 2026-08-21, L-1).
			//
			// 锚定匹配：名字必须以标记形态出现——粗体条目（`- **name**`）或行内
			// 代码（`` `name` ``）。裸子串匹配会让未来名为 "review" 的 hook 借
			// 已文档化的 "review-stop" 假通过（2026-08-21 复审 L-1）。
			if !strings.Contains(section.body, "**"+name+"**") && !strings.Contains(section.body, "`"+name+"`") {
				t.Errorf("%s forge section does not mention wired hook %q in anchored form (**%s** or `%s`) — every wired hook (esp. blocking ones) needs a doc line + a common-errors row when it can BLOCK", section.label, name, name, name)
			}
		}
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

// TestClaudeMDCommonErrorsIncludesSkillDecisions guards the common-errors table
// documents the task-verify skill-decisions guardrail (B 组件). 改 SKILL.md 未记
// 决策 → BLOCKED——agents 遇到需在 CLAUDE.md 看到 forge skills decide 记决策路径 +
// --skill-decisions disable 逃生舱（否则 BLOCKED 不透明）。对齐 test-coverage 守卫模式。
func TestClaudeMDCommonErrorsIncludesSkillDecisions(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "SKILL.md 未记决策") {
		t.Error("CLAUDE.md common-errors table missing skill-decisions guardrail row")
	}
	if !strings.Contains(section, "forge skills decide") {
		t.Error("CLAUDE.md skill-decisions row must surface the decide resolution path")
	}
	if !strings.Contains(section, "FORGE_SKILL_DECISIONS=disable") {
		t.Error("CLAUDE.md skill-decisions row must surface the escape hatch")
	}
	if !strings.Contains(section, "task-verify 拒绝（HARD stop）") {
		t.Error("CLAUDE.md skill-decisions row must document HARD stop (not advisory)")
	}
}

// TestClaudeMDCommonErrorsIncludesAcceptancePreflight guards the common-errors
// table documents the task-complete acceptance pre-flight (A 组件). task 声明
// acceptance 时 complete 校验 AcceptedHeadCommit==HEAD + Passed → BLOCKED——agents
// 遇到需在 CLAUDE.md 看到 verify-acceptance 回扣路径 + --acceptance-gate disable
// 逃生舱。对齐 test-coverage 守卫模式。
func TestClaudeMDCommonErrorsIncludesAcceptancePreflight(t *testing.T) {
	section := buildForgeSection(true)

	if !strings.Contains(section, "验收 #N 未实跑") {
		t.Error("CLAUDE.md common-errors table missing acceptance pre-flight row")
	}
	if !strings.Contains(section, "forge task verify-acceptance") {
		t.Error("CLAUDE.md acceptance row must surface the verify-acceptance resolution path")
	}
	if !strings.Contains(section, "FORGE_ACCEPTANCE_GATE=disable") {
		t.Error("CLAUDE.md acceptance row must surface the escape hatch")
	}
	if !strings.Contains(section, "task-complete 拒绝") {
		t.Error("CLAUDE.md acceptance row must document blocking (not advisory)")
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
	// The task-complete gate now emits an ADVISORY on uncommitted changes
	// (CheckNameUncommittedAtComplete) — the doc must tell agents what seeing
	// that advisory means: commit first, then complete.
	//
	// task-complete 门禁现在会对未提交变更发 ADVISORY
	// （CheckNameUncommittedAtComplete）——文档必须告诉 agent 见到该 advisory
	// 的含义：先 commit 再 complete。
	if !strings.Contains(section, "未提交变更") {
		t.Error("CLAUDE.md missing the uncommitted-changes ADVISORY note (commit before complete, gate now surfaces the inverted order)")
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
	if !strings.Contains(section, "mcp-scan") {
		t.Error("CLAUDE.md security section missing mcp-scan hook (project-level .mcp.json scan)")
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
// counter no longer exists). task-verify is mixed-mode: advisory (test-coverage/
// compile/assertion — what this test locks) + HARD stop (skill-decisions/work-
// activity — locked by TestClaudeMDCommonErrorsIncludesSkillDecisions). The name
// IsAdvisory refers to the aspect it locks, not "purely advisory".
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

	// New advisory wording: hooks only remind, the agent self-checks. Hook names
	// are backtick-anchored (covers-all-wired-hooks guard requires markup form).
	//
	// 新 advisory 措辞：hook 只提醒，agent 自检。hook 名带反引号锚定
	// （covers-all-wired-hooks 守卫要求标记形态）。
	if !strings.Contains(section, "`auto-compile` hook 仅 advisory 提醒") {
		t.Error("CLAUDE.md compile rule must document auto-compile as advisory (v0.25)")
	}
	if !strings.Contains(section, "`assertion-check` hook 检测到弱化仅 advisory 提醒") {
		t.Error("CLAUDE.md assertion rule must document assertion-check as advisory (v0.25)")
	}
	// The obsolete wording that referenced hook auto-checks implied blocking enforcement — must be gone.
	//
	// The old 「hook 自动检查」 wording implied blocking enforcement — must be gone.
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
	for _, claudeOnly := range []string{`/forge-quality`} {
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
// Claude-only and must carry /forge-quality, and must NOT carry the AGENTS.md
// cross-agent surface line. Together with TestGenerateAgentsMD this locks the
// forClaude branch of buildForgeSection. (/forge-pipeline was removed with the
// project pipeline.)
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
	if !strings.Contains(got, `/forge-quality`) {
		t.Error(`CLAUDE.md missing Claude slash command /forge-quality`)
	}
	if strings.Contains(got, `通过 forge CLI`) {
		t.Error(`CLAUDE.md must not carry the AGENTS.md cross-agent surface line`)
	}
}

// TestGenerateClaudeMDCarriesRecurrentHardening locks the recurrence-driven advisory→hard section in
// the generated CLAUDE.md. Without it an agent on a recurrent project hits the BLOCKED message cold
// (the whole point of documenting the soft↔hard balance). Guards the claudemd.go generator against
// silent removal of the section, table-row, and escape-hatch env names.
//
// TestGenerateClaudeMDCarriesRecurrentHardening 锁定生成的 CLAUDE.md 里复发驱动 advisory→hard 升硬
// 小节。无它则复发项目里 agent 会冷不丁撞 BLOCKED（文档化软↔硬平衡的全部意义）。守护 claudemd.go
// 生成器不静默删小节、表格行与逃生舱 env 名。
func TestGenerateClaudeMDCarriesRecurrentHardening(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateClaudeMD(dir); err != nil {
		t.Fatalf(`GenerateClaudeMD: %v`, err)
	}
	got, err := os.ReadFile(filepath.Join(dir, `.claude`, `CLAUDE.md`))
	if err != nil {
		t.Fatalf(`CLAUDE.md not written: %v`, err)
	}
	s := string(got)
	for _, want := range []string{
		`复发驱动升硬`,
		`FORGE_RECURRENT_HARDEN=disable`,
		`FORGE_RECURRENT_THRESHOLD=N`,
		`复发升 HARD stop`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf(`CLAUDE.md missing recurrence-hardening marker %q`, want)
		}
	}
}

// TestGenerateClaudeMDAtomicWriteNoResidue pins the durability contract after the
// os.WriteFile → util.AtomicWrite switch: generation leaves a complete file and no
// temp-file residue behind, and regeneration over an existing file preserves user
// content outside the Forge section (section-replace stays idempotent).
//
// TestGenerateClaudeMDAtomicWriteNoResidue 钉住 os.WriteFile → util.AtomicWrite
// 后的耐久契约：生成产出完整文件且无临时文件残留，对已有文件重复生成时标记外
// 的用户内容保留（section-replace 幂等）。
func TestGenerateClaudeMDAtomicWriteNoResidue(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateClaudeMD(dir); err != nil {
		t.Fatalf("GenerateClaudeMD: %v", err)
	}
	first, err := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read generated CLAUDE.md: %v", err)
	}
	if !strings.Contains(string(first), "FORGE:START") {
		t.Error("generated CLAUDE.md should contain the Forge section markers")
	}

	// Regenerate over user content: user text outside the markers must survive.
	//
	// 对用户内容重复生成：标记外的用户文本必须保留。
	user := "# my project\n\nuser notes stay\n"
	if err := os.WriteFile(filepath.Join(dir, ".claude", "CLAUDE.md"),
		[]byte(user+string(first)), 0644); err != nil {
		t.Fatal(err)
	}
	if err := GenerateClaudeMD(dir); err != nil {
		t.Fatalf("GenerateClaudeMD regenerate: %v", err)
	}
	second, _ := os.ReadFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	if !strings.Contains(string(second), "user notes stay") {
		t.Error("regeneration must preserve user content outside the Forge section")
	}

	// No atomic-write temp residue anywhere under .claude.
	//
	// .claude 下不得有原子写临时文件残留。
	entries, err := os.ReadDir(filepath.Join(dir, ".claude"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("atomic write residue left behind: %s", e.Name())
		}
	}
}

// TestGenerateUserQualitySkillTo pins the shared user-level skill writer used by
// both GenerateUserQualitySkill (~/.claude/skills) and the reasonix translator
// (~/.reasonix/skills): content is the conditional-activation form, and a missing
// agent home is a no-op (self-poison guard — Forge never creates an agent's config
// home itself).
//
// TestGenerateUserQualitySkillTo 钉住共享的用户级 skill 写入器（GenerateUserQualitySkill
// 与 reasonix translator 共用）：内容为条件激活形态；agent home 缺失时 no-op
// （自毒防护——Forge 绝不自行创建 agent 的配置 home）。
func TestGenerateUserQualitySkillTo(t *testing.T) {
	// Home exists → skill written with conditional-activation content.
	//
	// home 存在 → skill 写入，内容为条件激活形态。
	home := t.TempDir()
	skillsRoot := filepath.Join(home, "skills")
	if err := GenerateUserQualitySkillTo(skillsRoot, protocol.DefaultProtocol()); err != nil {
		t.Fatalf("GenerateUserQualitySkillTo: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(skillsRoot, "forge-quality", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "仅当当前项目已执行过 `forge init`") {
		t.Errorf("user-level skill 必须是条件激活措辞")
	}
	if strings.Contains(content, "## 当前项目信息") {
		t.Errorf("user-level skill 必须移除项目信息章节")
	}

	// Missing home → clean no-op, nothing created.
	//
	// home 缺失 → 干净 no-op，不创建任何东西。
	missing := filepath.Join(t.TempDir(), "no-such-home")
	if err := GenerateUserQualitySkillTo(filepath.Join(missing, "skills"), protocol.DefaultProtocol()); err != nil {
		t.Fatalf("GenerateUserQualitySkillTo (missing home): %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("缺失的 agent home 不得被创建（自毒防护），stat err=%v", err)
	}
}
