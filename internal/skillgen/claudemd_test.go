package skillgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/util"
)

// TestClaudeMDCoversAllWiredHooks is the docs-consistency guard for the security section.
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
			// 锚定匹配：名字必须以标记形态出现——粗体条目（`- **name**`）或行内
			// 代码（`` `name` ``）。裸子串匹配会让未来名为 "review" 的 hook 借
			// 已文档化的 "review-stop" 假通过（2026-08-21 复审 L-1）。
			if !strings.Contains(section.body, "**"+name+"**") && !strings.Contains(section.body, "`"+name+"`") {
				t.Errorf("%s forge section does not mention wired hook %q in anchored form (**%s** or `%s`) — every wired hook (esp. blocking ones) needs a doc line + a common-errors row when it can BLOCK", section.label, name, name, name)
			}
		}
	}
}

// TestClaudeMDSectionContract table-drives the generated CLAUDE.md forge-section
// content guards (the 21 former per-topic Contains tests, merged 2026-08-30
// slim-down): each row pins the anchor strings the section MUST carry and the
// stale/wrong strings it must NOT carry. Per-row rationale, chronologically from
// the former tests:
//   - test-coverage / retention / skill-decisions / acceptance rows: the
//     common-errors table must surface each gate, its resolution path, and its
//     escape-hatch env knob — an undocumented BLOCK is opaque to agents.
//   - work-activity-escape wording: work-activity is a RHYTHM gate
//     (checklog.isRhythmEscapeHatch excludes it from the Strength cap), so docs
//     must NOT claim it downgrades evidence to Weak; verification-class hatches
//     keep the Weak wording with the evidence-scaled carve-out note on EVERY row.
//   - review-fix-recheck: the resolution path must include the RE-REVIEW step
//     (修复者不能自证) — the old copy implied stamp-right-after-fix.
//   - commit-timing: complete clears the active ref, so a post-complete commit
//     gets quarantined — the doc must state commit-before-complete + the
//     chore/*-commit recovery path + the uncommitted-changes advisory meaning.
//   - hazard-guard copy: pre-authorization path (confirm --last, no second ask),
//     generalized tool reference (no per-tool enumeration that missed
//     kimi/copilot/zcode), no stale FORGE_ALLOW_HAZARD migration note.
//   - guard-behavior truth: task-guard/bash-guard only WARN (never deny
//     Write/Edit), and no fabricated ">10 行" threshold (no guard checks lines).
//   - aux-hooks: session-health stays; sunk hooks (read-check/scope-guard/
//     clone-check) must not be listed as runtime anymore.
//   - skill-scan/mcp-scan: SessionStart advisory global skill audit is documented.
//   - task-abort: `forge task abort --ref <ref>` must be documented (ghost-task
//     escape; .forge/* self-protection blocks manual cleanup).
//   - task-verify advisory: no obsolete "连续 3 次失败" force-pass counter.
//   - compile/assertion advisory (v0.25): hooks only remind; no "hook 自动检查"
//     blocking-enforcement wording, no "自动检查编译+断言" gate-order claim.
//   - observation hooks (#4-A): failure-track/subagent-track/test-nudge must
//     document their ACTUAL trigger events + advisory-never-blocks semantics.
//   - stop-chain facts: review-stop is the ONLY hard Stop gate (non-task mode);
//     task-verify Stop is pure advisory — the old copy claimed both were hard.
//   - aux-checks read-before-edit: 「先读再改」 is a HARD gate inside tasks, it
//     must not be listed among WARN-only sunk rules.
//   - task-guard per-host: dsh promotes the advisory to a block (hostcap
//     PromoteAdvisory); a flat "WARN 不拦截" claim is a lie.
//   - no-archaeology: the security list is read every session — no dates/counts/
//     "补齐" notes.
//   - basic rule 7: conclusion-first + backtick-wrapped banned phrases (the
//     generated file must survive its own D1 doclint).
//   - doc-gate row: points at the doc-review skill (truth source), never the
//     migrated code-review-gate internal path.
//
// TestClaudeMDSectionContract 表驱动生成 CLAUDE.md forge 段的内容守卫（2026-08-30
// 瘦身合并原 21 个逐主题 Contains 测试）：每行钉该段【必须】携带的锚串与【不得】
// 再出现的陈旧/错误串。逐行缘由按原测试年代：
//   - test-coverage / retention / skill-decisions / acceptance：common-errors 表必须
//     浮出各门禁、解决路径与逃生舱 env——未文档化的 BLOCK 对 agent 不透明。
//   - work-activity 逃生文案：work-activity 是节奏门禁（isRhythmEscapeHatch 不参与
//     Strength cap），文档不得声称降 evidence 到 Weak；验证类逃生保留 Weak 措辞且
//     每一行都带证据缩放豁免说明。
//   - review-fix-recheck：解决路径必须含复审步骤（修复者不能自证）。
//   - commit-timing：complete 清活跃 ref，其后 commit 会被隔离——文档必须写明先
//     commit 再 complete、chore/*-commit 恢复路径、未提交变更 advisory 的含义。
//   - hazard-guard 文案：授权路径（confirm --last 免二次确认）、泛化工具指代、
//     无 FORGE_ALLOW_HAZARD 迁移残迹。
//   - guard 行为真相：task-guard/bash-guard 只 WARN 不拦截；无编造的「>10 行」阈值。
//   - aux-hooks：session-health 保留；下沉 hook 不得再列为运行时。
//   - skill-scan/mcp-scan：SessionStart advisory 全局 skill 审计已文档化。
//   - task-abort：`forge task abort --ref <ref>` 必须文档化（僵尸任务逃生）。
//   - task-verify advisory：无过时的「连续 3 次失败」计数器。
//   - compile/assertion advisory（v0.25）：hook 只提醒；无阻断语义旧措辞。
//   - observation hooks（#4-A）：三个观察 hook 必须写明真实触发事件 + advisory 不阻断。
//   - stop-chain facts：review-stop 是唯一 Stop 硬门禁（非 task 模式）；task-verify
//     Stop 纯 advisory——旧文案声称两者同为硬门禁。
//   - aux-checks read-before-edit：「先读再改」任务内是硬门禁，不得列入仅 WARN 下沉规则。
//   - task-guard 按宿主：dsh 提升为阻断；笼统「WARN 不拦截」是谎言。
//   - no-archaeology：安全清单每会话都被读——不留日期/计数/「补齐」注记。
//   - 基本规则 7：结论先行 + 禁令短语反引号包裹（生成文件要过自身 D1 doclint）。
//   - doc-gate 行：指引 doc-review skill（真相源），不引用已迁移的内部路径。
func TestClaudeMDSectionContract(t *testing.T) {
	section := buildForgeSection(true)
	for _, tc := range []struct {
		name    string
		want    []string
		notWant []string
	}{
		{"common-errors: test-coverage gate", []string{
			"without a corresponding test",
			"FORGE_TEST_COVERAGE=disable",
		}, nil},
		{"common-errors: log retention", []string{
			"trace/老任务历史消失",
			"FORGE_LOG_RETENTION_DAYS",
		}, nil},
		{"work-activity escape wording (rhythm gate, never Weak)", []string{
			"work-activity 是节奏门禁，不降 evidence 强度",
			"重证据任务按证据缩放豁免",
			// 豁免说明须覆盖全部验证类逃生行（复审第二轮：曾只补 test-coverage 行）。
			"FORGE_TEST_COVERAGE=disable`（降 Weak；重证据任务按证据缩放豁免）",
			"--skill-decisions disable`（per-task，优先于 `FORGE_SKILL_DECISIONS=disable` env，降 evidence 到 Weak；重证据任务按证据缩放豁免）",
			"--acceptance-gate disable`（per-task，优先于 `FORGE_ACCEPTANCE_GATE=disable` env，降 evidence 到 Weak；重证据任务按证据缩放豁免）",
		}, []string{
			// 撒谎文案不得回归：work-activity 不触发 cap（防陈旧第二拷贝回归）。
			"work-activity disable`（降 evidence 强度到 Weak）",
		}},
		{"review-fix-recheck protocol (修复者不能自证)", []string{
			"复审修复",
			"修复者不能自证",
		}, nil},
		{"common-errors: skill-decisions guardrail (B 组件)", []string{
			"SKILL.md 未记决策",
			"forge skills decide",
			"FORGE_SKILL_DECISIONS=disable",
			"task-verify 拒绝（HARD stop）",
		}, nil},
		{"common-errors: acceptance pre-flight (A 组件)", []string{
			"验收 #N 未实跑",
			"forge task verify-acceptance",
			"FORGE_ACCEPTANCE_GATE=disable",
			"task-complete 拒绝",
		}, nil},
		{"commit timing (commit must precede complete)", []string{
			"提交时机",
			"chore/*-commit",
			"未提交变更", // CheckNameUncommittedAtComplete advisory 的含义
		}, nil},
		{"hazard-guard copy (2026-08 HITL protocol revision)", []string{
			"无需二次确认",
			"forge hazard confirm --last",
			"所在工具的提问确认机制",
		}, []string{
			"AskUserQuestion", // 逐工具枚举漏了 kimi/copilot/zcode
			"FORGE_ALLOW_HAZARD",
		}},
		{"guard behavior truth (task/bash-guard only WARN)", nil, []string{
			"非平凡变更（>10 行）", // 编造阈值——没有任何 guard 查行数
			"denied by task-guard",
			"denied by bash-guard",
		}},
		{"aux hooks (sunk judgmental hooks gone)", []string{
			"辅助检查",
		}, []string{
			"read-check（",
			"scope-guard（",
			"clone-check（",
		}},
		{"skill-scan / mcp-scan documented as SessionStart", []string{
			"skill-scan",
			"mcp-scan",
			"SessionStart",
		}, nil},
		{"task abort escape hatch", []string{
			"中止任务",
			"forge task abort --ref <ref>",
		}, nil},
		{"task-verify is advisory (no force-pass counter)", []string{
			"advisory",
		}, []string{
			"连续 3 次失败",
		}},
		{"compile/assertion rules advisory (v0.25)", []string{
			"`auto-compile` hook 仅 advisory 提醒",
			"`assertion-check` hook 检测到弱化仅 advisory 提醒",
		}, []string{
			"hook 自动检查",
			"自动检查编译+断言",
		}},
		{"observation hooks (#4-A) event accuracy + advisory", []string{
			"**failure-track**（PostToolUseFailure",
			"**subagent-track**（SubagentStop",
			"**test-nudge**（PostToolUse Write|Edit",
			"advisory 不阻断",
		}, nil},
		{"stop-chain facts (review-stop is the only hard Stop gate)", []string{
			"task 模式下直接放行",
			"task-complete 门禁强制",
			"只 advisory 不阻塞",
		}, []string{
			"与 task-verify 同为 Stop 链硬门禁",
		}},
		{"aux-checks must not sink read-before-edit", []string{
			"聚焦变更/避免重复",
		}, []string{
			"先读再改/聚焦变更",
		}},
		{"task-guard per-host truth (dsh/zcode promote to block)", []string{
			"dsh/zcode",
			"提升为阻断",
		}, []string{
			"只触发 task-guard 警告（WARN，不拦截）",
		}},
		{"no archaeology notes in the security list", nil, []string{
			"27.7k",
			"零记录缺口",
			"空头支票",
		}},
		{"basic rule 7: reply concision (phrases backtick-wrapped)", []string{
			"结论先行，禁空转措辞",
			"forge docs lint",
			"`综上所述`",
			"`基本可以`",
			"`问题不大`",
		}, nil},
		{"doc-gate row references doc-review skill", []string{
			"按 doc-review skill 评审",
		}, []string{
			"code-review-gate/references/rubric-docs.md",
		}},
	} {
		for _, w := range tc.want {
			if !strings.Contains(section, w) {
				t.Errorf("%s: CLAUDE.md forge section missing %q", tc.name, w)
			}
		}
		for _, nw := range tc.notWant {
			if strings.Contains(section, nw) {
				t.Errorf("%s: CLAUDE.md forge section must not contain %q", tc.name, nw)
			}
		}
	}
}

// TestClaudeMDFailureTrackMatcherTracksSpec is the docs-consistency guard for the failure-track line's matcher text: the generated docs must quote the LIVE ForgeHookSpec matcher, not a hardcoded string.
//
// TestClaudeMDFailureTrackMatcherTracksSpec 是 failure-track 行 matcher 文案的一
// 致性守卫：生成的文档必须引用**活的** ForgeHookSpec matcher，而非钉死的字符串。
// 守护的根因：NIT-4（2026-08-22 复审）——spec 把 Bash|PowerShell 收窄为 Bash，而
// 文档行硬编码 matcher，这种形态在每次未来 spec 变更时都会静默腐烂。从 spec 读
// matcher 让该 drift 变成测试失败而非静默出仓。
func TestClaudeMDFailureTrackMatcherTracksSpec(t *testing.T) {
	var matcher string
	for _, g := range hooks.ForgeHookSpec()["PostToolUseFailure"] {
		if g.Matcher != "" {
			matcher = g.Matcher
			break
		}
	}
	if matcher == "" {
		t.Fatal("ForgeHookSpec PostToolUseFailure matcher unexpectedly empty — spec shape changed?")
	}
	for _, section := range []struct{ label, body string }{
		{"CLAUDE.md", buildForgeSection(true)},
		{"AGENTS.md", buildForgeSection(false)},
	} {
		// Anchored on the full-width paren form the docs line uses
		// （PostToolUseFailure <matcher>）— a bare substring would pass on stale
		// prefixes (e.g. "Bash" inside "Bash|PowerShell").
		//
		// 锚定文档行使用的全角括号形态（PostToolUseFailure <matcher>）——裸子串
		// 会让过期前缀假通过（如 "Bash|PowerShell" 里的 "Bash"）。
		if want := "（PostToolUseFailure " + matcher + "）"; !strings.Contains(section.body, want) {
			t.Errorf("%s failure-track line must quote the live spec matcher (want %q in section) — docs-vs-spec drift", section.label, want)
		}
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

// TestGenerateClaudeMDCarriesRecurrentHardening locks the recurrence-driven advisory→hard section in the generated CLAUDE.md.
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

// TestGenerateClaudeMDAtomicWriteNoResidue pins the durability contract after the AtomicWrite switch.
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

// TestGenerateUserQualitySkillTo pins the shared user-level skill writer.
//
// TestGenerateUserQualitySkillTo 钉住共享的用户级 skill 写入器（GenerateUserQualitySkill
// 与 reasonix translator 共用）：内容为条件激活形态；agent home 缺失时 no-op
// （自毒防护——Forge 绝不自行创建 agent 的配置 home）。
func TestGenerateUserQualitySkillTo(t *testing.T) {
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

	// home 缺失 → 干净 no-op，不创建任何东西。
	missing := filepath.Join(t.TempDir(), "no-such-home")
	if err := GenerateUserQualitySkillTo(filepath.Join(missing, "skills"), protocol.DefaultProtocol()); err != nil {
		t.Fatalf("GenerateUserQualitySkillTo (missing home): %v", err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("缺失的 agent home 不得被创建（自毒防护），stat err=%v", err)
	}
}

// TestClaudeMD_ConventionsHooksAdvisory pins the doc contract for the two
// conventions hooks: their doc lines must state advisory/不阻断 semantics. These
// hooks never block — a doc line that reads as a gate would send agents hunting
// for escape hatches that do not exist (and would drift from the hook's actual
// fail-open behavior).
//
// TestClaudeMD_ConventionsHooksAdvisory 钉住两个 conventions hook 的文档契约：
// 文档行必须声明 advisory/不阻断语义。这两个 hook 永不阻断——写成门禁语义的
// 文档会让 agent 去找根本不存在的逃生舱，且与 hook 实际的 fail-open 行为漂移。
func TestClaudeMD_ConventionsHooksAdvisory(t *testing.T) {
	for _, section := range []struct{ label, body string }{
		{"CLAUDE.md", buildForgeSection(true)},
		{"AGENTS.md", buildForgeSection(false)},
	} {
		for _, name := range []string{"conventions-context", "conventions-write"} {
			for _, line := range strings.Split(section.body, "\n") {
				if strings.Contains(line, "**"+name+"**") {
					if !strings.Contains(line, "advisory") && !strings.Contains(line, "不阻断") {
						t.Errorf("%s: %s doc line must state advisory/不阻断 semantics, got: %s", section.label, name, line)
					}
					break
				}
			}
		}
	}
}

// TestForgeSectionMarkersAliasUtil pins the alias contract: skillgen's forgeSectionStart/End must equal util.ForgeSectionStart/End.
//
// TestForgeSectionMarkersAliasUtil 钉住别名契约：skillgen 的
// forgeSectionStart/End 必须等于 util.ForgeSectionStart/End。常量已下沉 util
// （单一真相源，同时解开 taskpipeline→conventions 的依赖环），skillgen 保留
// 文件局部别名——将来任一侧被手改都会静默分叉「forge 生成段」契约；本测试
// 是绊线。
func TestForgeSectionMarkersAliasUtil(t *testing.T) {
	if forgeSectionStart != util.ForgeSectionStart || forgeSectionEnd != util.ForgeSectionEnd {
		t.Fatalf("skillgen markers forked from util: %q/%q vs %q/%q",
			forgeSectionStart, forgeSectionEnd, util.ForgeSectionStart, util.ForgeSectionEnd)
	}
	// 既有生成文件里的字面量形态也不能漂移（GenerateClaudeMD 产物被外部
	// 工具按字面量识别）。
	if forgeSectionStart != "<!-- FORGE:START -->" || forgeSectionEnd != "<!-- FORGE:END -->" {
		t.Fatalf("marker literal drifted: %q/%q", forgeSectionStart, forgeSectionEnd)
	}
}
