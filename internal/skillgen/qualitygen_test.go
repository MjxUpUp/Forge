package skillgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/doclint"
	"github.com/MjxUpUp/Forge/internal/protocol"
)

func TestQualitySkillContainsTaskRule(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "每次修改后确认编译通过", Severity: "error", Enabled: true},
		},
		SessionRules: []protocol.SessionRule{
			{Instruction: "修改前先说意图", Mandatory: true, Trigger: "on_edit"},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "Task Bridge Protocol") {
		t.Error("quality SKILL.md missing Task Bridge Protocol section")
	}
	if !strings.Contains(content, "编码前必做") {
		t.Error("quality SKILL.md missing mandatory pre-coding task start instruction")
	}
	if !strings.Contains(content, "强制顺序") {
		t.Error("quality SKILL.md missing forced gate sequence instruction")
	}
	if !strings.Contains(content, "--branch") {
		t.Error("quality SKILL.md missing --branch flag reference")
	}
	if !strings.Contains(content, "forge task start") {
		t.Error("quality SKILL.md missing 'forge task start' command reference")
	}
	if !strings.Contains(content, "forge task list") {
		t.Error("quality SKILL.md missing 'forge task list' command reference")
	}
	if !strings.Contains(content, "--ref <ref>") {
		t.Error("quality SKILL.md missing --ref in gate commands")
	}
}

// TestQualitySkillDocumentsCommitTiming guards the same post-complete commit
// trap as TestClaudeMDDocumentsCommitTiming, but for the forge-quality skill:
// complete clears the active task ref, so the skill must tell agents to commit
// before complete.
func TestQualitySkillDocumentsCommitTiming(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "提交时机") {
		t.Error("quality SKILL.md missing commit-timing guidance (commit must precede complete)")
	}
}

// TestQualitySkillDocumentsAuxChecks guards that the remaining auxiliary hooks
// (assertion-check/auto-compile advisory), the sunk Red Flags rules, scoring thresholds,
// and the read-before-edit error are documented. read-check/scope-guard/
// clone-check were sunk from runtime hooks to the Red Flags section (layered
// noise treatment); agents still need the skill to explain session-health
// WARNs, the scoring A-F cutoffs (90/80/70/60), and the "passed without
// reading any code" fix.
func TestQualitySkillDocumentsAuxChecks(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "辅助质量检查") {
		t.Error("quality SKILL.md missing auxiliary-checks section")
	}
	if !strings.Contains(content, "Red Flags") {
		t.Error("quality SKILL.md missing Red Flags section (read-check/scope-guard/clone-check sunk here)")
	}
	if !strings.Contains(content, "先读再改") {
		t.Error("quality SKILL.md Red Flags missing the read-before-edit rule")
	}
	if !strings.Contains(content, "阈值") {
		t.Error("quality SKILL.md scoring table missing A-F thresholds")
	}
	if !strings.Contains(content, "passed without reading any code") {
		t.Error("quality SKILL.md error table missing 'passed without reading any code' row")
	}
	// Sunk hooks must NOT appear as runtime hook docs anymore.
	for _, gone := range []string{"read-check**（PreToolUse", "scope-guard**（PreToolUse", "clone-check**（PostToolUse"} {
		if strings.Contains(content, gone) {
			t.Errorf("quality SKILL.md still documents sunk hook %q as runtime — should be Red Flags text only", gone)
		}
	}
}

// TestQualitySkillDescriptionIsTriggerOriented guards the frontmatter description
// against regressing to a vague "what it is" phrasing (the old "每次开发会话自动
// 执行的质量标准" gave the model no signal for when to invoke the skill on
// demand). Per the Anthropic skill standard the description must name concrete
// trigger scenarios — advancing gates, recovering from guard warnings, aborting
// a stuck task. A skill no one knows when to load is the "没有什么用" failure.
func TestQualitySkillDescriptionIsTriggerOriented(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	// The vague non-trigger phrase must be gone.
	if strings.Contains(content, "每次开发会话自动执行的质量标准") {
		t.Error("quality SKILL.md description still uses vague non-trigger phrasing")
	}
	// The description must name concrete invocation scenarios.
	desc := content
	if !strings.Contains(desc, "门禁") {
		t.Error("description must mention gate advancement as a trigger")
	}
	if !strings.Contains(desc, "警告") {
		t.Error("description must mention guard-warning recovery as a trigger")
	}
}

// TestQualitySkillDocumentsAbort guards that the task command list includes
// `forge task abort` — the escape hatch for stuck/ghost tasks. Without it in the
// skill, an agent that needs to clean up a non-progressing task has no guidance.
func TestQualitySkillDocumentsAbort(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "forge task abort") {
		t.Error("quality SKILL.md task command list missing 'forge task abort'")
	}
}

// TestQualitySkillTaskVerifyIsAdvisory guards the advisory rewrite: task-verify
// must NOT be documented as blocking session end (the old "连续 3 次失败放行"
// semantics are gone — that counter was removed), and the mandatory-review
// force must be attributed to `forge task complete`, not the Stop hook. This is
// the doc-side guard for the enforcement transfer.
func TestQualitySkillTaskVerifyIsAdvisory(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)

	// Obsolete blocking / 3-fail phrasing must be gone.
	for _, gone := range []string{"连续 3 次失败", "阻塞会话结束", "task-verify 阻塞"} {
		if strings.Contains(content, gone) {
			t.Errorf("quality SKILL.md still uses obsolete blocking phrasing %q (task-verify is advisory)", gone)
		}
	}
	// Advisory wording must be present.
	if !strings.Contains(content, "advisory") {
		t.Error("quality SKILL.md must document task-verify as advisory")
	}
}

// TestGenerateQualitySkillAtomicWriteNoResidue 为 quality skill 文件钉住同款
// 耐久契约：util.AtomicWrite 切换后完整 SKILL.md 落盘且无临时残留。
func TestGenerateQualitySkillAtomicWriteNoResidue(t *testing.T) {
	dir := t.TempDir()
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "每次修改后确认编译通过", Severity: "error", Enabled: true},
		},
	}
	if err := GenerateQualitySkill(dir, proto); err != nil {
		t.Fatalf("GenerateQualitySkill: %v", err)
	}
	var skillPath string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, "SKILL.md") {
			skillPath = p
		}
		return nil
	})
	if skillPath == "" {
		t.Fatal("GenerateQualitySkill did not produce a SKILL.md")
	}
	content, _ := os.ReadFile(skillPath)
	if !strings.Contains(string(content), "forge") {
		t.Error("generated SKILL.md should reference forge")
	}
	entries, _ := os.ReadDir(filepath.Dir(skillPath))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("atomic write residue left behind: %s", e.Name())
		}
	}
}

// TestQualitySkillDropsRemovedEscapeHatch 钉住 FORGE_SKIP_VERIFY 清理：生成的
// skill 不得再教用户一个已不存在的逃生舱（TaskVerifyHook 自 v0.25 无条件
// advisory）。
func TestQualitySkillDropsRemovedEscapeHatch(t *testing.T) {
	proto := &protocol.Protocol{Version: "1"}
	content := buildQualitySkillContent(t.TempDir(), proto)
	if strings.Contains(content, "FORGE_SKIP_VERIFY") {
		t.Error("generated skill must not reference removed FORGE_SKIP_VERIFY escape hatch")
	}
}

// TestQualitySkillRenderedViaSharedHelper 钉住 skillgen 侧的 5 份 →
// protocol.Render* 重构：生成 skill 的质量标准段必须与 protocol.RenderStandards
// 用 skillgen 风格参数的输出一致，证明共享 helper 真实接线。
func TestQualitySkillRenderedViaSharedHelper(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "每次修改后确认编译通过", Severity: "error", Enabled: true},
		},
		SessionRules: []protocol.SessionRule{
			{Instruction: "修改前先说意图", Mandatory: true, Trigger: "on_edit"},
		},
	}
	content := buildQualitySkillContent(t.TempDir(), proto)
	if !strings.Contains(content, "🔴 **编译必须通过**: 每次修改后确认编译通过") {
		t.Error("generated skill standards section does not match shared-helper emoji rendering")
	}
	if !strings.Contains(content, "[必须] 修改前先说意图") {
		t.Error("generated skill session rules section does not match shared-helper 必须/建议 rendering")
	}
}

// TestQualitySkillReplyConcisionRules 守卫「回复详略规则」章节：结论先行原则
// 加从 internal/doclint 渲染的 L1 禁令清单（单一真相源——短语表不允许在这里
// 或 linter 自身文档之外手抄）。
func TestQualitySkillReplyConcisionRules(t *testing.T) {
	proto := &protocol.Protocol{Version: "1"}

	content := buildQualitySkillContent(t.TempDir(), proto)

	if !strings.Contains(content, "回复详略规则") {
		t.Error("quality SKILL.md missing 回复详略规则 section")
	}
	if !strings.Contains(content, "结论先行") {
		t.Error("quality SKILL.md missing conclusion-first principle")
	}
	if !strings.Contains(content, "forge docs lint") {
		t.Error("quality SKILL.md missing forge docs lint pointer")
	}
	// 每条 doclint 禁令短语都须出现在渲染出的 skill 文本——防止新增表项
	// 静默漏渲染。
	for _, p := range doclint.BannedPhrases {
		if !strings.Contains(content, p.Pattern.String()) {
			t.Errorf("quality SKILL.md 缺禁令短语 %q（渲染漂移）", p.Pattern.String())
		}
	}
	for _, p := range doclint.EvidenceFreeConclusions {
		if !strings.Contains(content, p.Pattern.String()) {
			t.Errorf("quality SKILL.md 缺无证据结论短语 %q（渲染漂移）", p.Pattern.String())
		}
	}
}

// TestQualitySkillDocGateReferencesDocReviewSkill 生成的 quality SKILL.md 中 doc gate
// 段须指引「按 doc-review skill 评审」且不得引用已迁移的
// code-review-gate/references/rubric-docs.md 旧路径（依赖倒置：skill 是流程真相源）。
func TestQualitySkillDocGateReferencesDocReviewSkill(t *testing.T) {
	proto := &protocol.Protocol{
		Version: "1",
		Standards: []protocol.Standard{
			{ID: "compile", Name: "编译必须通过", Description: "每次修改后确认编译通过", Severity: "error", Enabled: true},
		},
	}

	content := buildQualitySkillContent(t.TempDir(), proto)
	if !strings.Contains(content, "按 doc-review skill 评审") {
		t.Error("doc gate 段应指引按 doc-review skill 评审")
	}
	if strings.Contains(content, "code-review-gate/references/rubric-docs.md") {
		t.Error("doc gate 段不得引用已迁移的 code-review-gate/references/rubric-docs.md 旧路径")
	}
}
