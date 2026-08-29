package skillsqa

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// writeSkill 在 dir 下建 name/SKILL.md，返回 skill 目录路径（dir 名 = name，满足 R2）。
func writeSkill(t *testing.T, dir, name, content string) string {
	t.Helper()
	sd := filepath.Join(dir, name)
	must(t, os.MkdirAll(sd, 0755))
	must(t, os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0644))
	return sd
}

// makeSkill 组装 SKILL.md。pattern 为空时不写 metadata 块（测 R7 缺 pattern）。
func makeSkill(name, desc, pattern, body string) string {
	if pattern == "" {
		return "---\nname: " + name + "\ndescription: \"" + desc + "\"\n---\n\n" + body
	}
	return "---\nname: " + name + "\ndescription: \"" + desc +
		"\"\nmetadata:\n  pattern: " + pattern + "\n  domain: testing\n---\n\n" + body
}

// longDesc 含 Use when + SKIP 且 ≥80 rune（过 R4/R5/R6）。
func longDesc() string {
	return "合格描述前缀。" + strings.Repeat("测试内容段落", 12) + "Use when: 场景触发。SKIP: 跳过场景。"
}

// signalBody 含高信号关键词（过 R9）。
func signalBody() string {
	return "# 标题\n\n决策树：第一步先做这个。自查清单：检查项。\n"
}

func expectIssue(t *testing.T, r *SkillReport, wantSubstr string) {
	t.Helper()
	for _, iss := range r.Issues {
		if strings.Contains(iss, wantSubstr) {
			return
		}
	}
	t.Fatalf("expected issue containing %q, got: %v", wantSubstr, r.Issues)
}

func TestAuditSkill_Valid(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline + gate", signalBody()))
	r, err := AuditSkill(sd)
	must(t, err)
	if !r.Pass {
		t.Fatalf("expected pass, got issues: %v", r.Issues)
	}
	if r.Quality.DescLen < 80 {
		t.Fatalf("desc_len = %d, want >=80", r.Quality.DescLen)
	}
}

func TestAuditSkill_R1_BadName(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("My_Skill", longDesc(), "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "name 不符合 kebab-case")
}

func TestAuditSkill_R2_NameDirMismatch(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "dir-name", makeSkill("other-name", longDesc(), "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "不一致")
}

func TestAuditSkill_R3_UnknownField(t *testing.T) {
	// 顶层未知字段 patten（typo），必须在 Raw 且不在白名单
	md := "---\nname: my-skill\npatten: reviewer\ndescription: \"" + longDesc() +
		"\"\nmetadata:\n  pattern: reviewer\n---\n\n" + signalBody()
	sd := writeSkill(t, t.TempDir(), "my-skill", md)
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "frontmatter 未知字段")
}

func TestAuditSkill_R4_DescTooShort(t *testing.T) {
	// 短描述：缺 Use when/SKIP 也会触发，但 R4 长度独立判定
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", "太短 Use when x SKIP y", "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "description 过短")
}

func TestAuditSkill_R5_MissingUseWhen(t *testing.T) {
	desc := strings.Repeat("无触发词的描述内容段落", 10)
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc, "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "缺 Use when")
}

func TestAuditSkill_R6_MissingSkip(t *testing.T) {
	desc := strings.Repeat("有 Use when 但没跳过词的描述", 10) + " Use when: x."
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc, "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "缺 SKIP")
}

func TestAuditSkill_R7_PatternInvalid(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "bogus-pattern", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "pattern 非法")
}

func TestAuditSkill_R7_PatternMissing(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "缺 metadata.pattern")
}

func TestAuditSkill_R7_PatternComboValid(t *testing.T) {
	// "inversion + pipeline + reviewer" 每段合法 → 通过 R7
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "inversion + pipeline + reviewer", signalBody()))
	r, _ := AuditSkill(sd)
	for _, iss := range r.Issues {
		if strings.Contains(iss, "pattern") {
			t.Fatalf("combo pattern should be valid, got: %v", r.Issues)
		}
	}
}

func TestAuditSkill_R8_TooManyLines(t *testing.T) {
	body := "# t\n\n决策树。\n" + strings.Repeat("填充行内容\n", 500) // >500 行
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", body))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "SKILL.md 过长")
}

func TestAuditSkill_R9_NoHighSignal(t *testing.T) {
	body := "# 标题\n\n这是普通正文，没有任何高信号关键词在里面。\n"
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", body))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "缺高信号内容")
}

func TestAuditSkill_DescLenIsRuneCount(t *testing.T) {
	// 锁定 R4 用 rune 计数（非字节）：纯中文描述的 rune 数应远小于字节数
	desc := strings.Repeat("中", 30) // 30 rune / 90 字节
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc+" Use when: x SKIP: y", "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	if r.Quality.DescLen < 30 || r.Quality.DescLen > 60 {
		t.Fatalf("desc_len = %d, expected rune count (~30-50), byte count would be ~100+", r.Quality.DescLen)
	}
}

// expectAdvisory 检查 Advisories（非 Issues，不影响 Pass）含子串。
func expectAdvisory(t *testing.T, r *SkillReport, wantSubstr string) {
	t.Helper()
	for _, a := range r.Advisories {
		if strings.Contains(a, wantSubstr) {
			return
		}
	}
	t.Fatalf("expected advisory containing %q, got: %v", wantSubstr, r.Advisories)
}

// R4 上限：description >1024 rune 触发硬 issue（Anthropic skill 规范上限）。
func TestAuditSkill_R4_DescTooLong(t *testing.T) {
	desc := strings.Repeat("测试描述内容段", 150) + " Use when: x SKIP: y" // 7*150=1050 >1024
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc, "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "description 过长")
	if r.Pass {
		t.Fatalf(">1024 should fail Pass")
	}
}

// R4 偏长：description >500 且 ≤1024 走 advisory，不影响 Pass。
func TestAuditSkill_R4_DescLongAdvisory(t *testing.T) {
	desc := strings.Repeat("测试描述内容段", 80) + " Use when: x SKIP: y" // 7*80=560 >500 <1024
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc, "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	if !r.Pass {
		t.Fatalf("偏长是 advisory 不应失败, issues: %v", r.Issues)
	}
	expectAdvisory(t, r, "偏长")
}

// R10 CSO：description 含工作流总结词走 advisory，不阻断 Pass。
func TestAuditSkill_R10_CSO(t *testing.T) {
	desc := "这是一个完整工作流的描述。" + strings.Repeat("填充内容", 12) + " Use when: x SKIP: y"
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc, "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	if !r.Pass {
		t.Fatalf("CSO 是 advisory 不应失败, issues: %v", r.Issues)
	}
	expectAdvisory(t, r, "工作流总结词")
}

// R11 references 嵌套子目录：references/ 下不应有子目录（≤1 level），硬 issue。
func TestAuditSkill_R11_NestedRefs(t *testing.T) {
	dir := t.TempDir()
	sd := writeSkill(t, dir, "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
	must(t, os.MkdirAll(filepath.Join(sd, "references", "subdir"), 0755))
	r, _ := AuditSkill(sd)
	expectIssue(t, r, "子目录")
	if r.Pass {
		t.Fatalf("嵌套子目录应失败")
	}
}

// R11 references >100 行无 ToC：advisory，不阻断 Pass。
func TestAuditSkill_R11_RefNoToC(t *testing.T) {
	dir := t.TempDir()
	sd := writeSkill(t, dir, "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
	must(t, os.MkdirAll(filepath.Join(sd, "references"), 0755))
	longRef := strings.Repeat("参考内容行\n", 110) // 110 行，无 ## 目录
	must(t, os.WriteFile(filepath.Join(sd, "references", "long-ref.md"), []byte(longRef), 0644))
	r, _ := AuditSkill(sd)
	if !r.Pass {
		t.Fatalf("缺 ToC 是 advisory 不应失败, issues: %v", r.Issues)
	}
	expectAdvisory(t, r, "缺 ## 目录")
}

// R11 references >100 行有 ToC：不应触发 ToC advisory。
func TestAuditSkill_R11_RefWithToC(t *testing.T) {
	dir := t.TempDir()
	sd := writeSkill(t, dir, "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
	must(t, os.MkdirAll(filepath.Join(sd, "references"), 0755))
	ref := "# 标题\n\n## 目录\n\n- [x](#x)\n\n" + strings.Repeat("参考内容行\n", 110)
	must(t, os.WriteFile(filepath.Join(sd, "references", "ref.md"), []byte(ref), 0644))
	r, _ := AuditSkill(sd)
	for _, a := range r.Advisories {
		if strings.Contains(a, "缺 ## 目录") {
			t.Fatalf("有 ToC 不应触发 advisory, got: %v", r.Advisories)
		}
	}
}

// 无 references 目录：合法，不报（防 false positive）。
func TestAuditSkill_R11_NoRefsDir(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", longDesc(), "pipeline", signalBody()))
	r, _ := AuditSkill(sd)
	for _, a := range r.Advisories {
		if strings.Contains(a, "references/") {
			t.Fatalf("无 references 目录不应触发 advisory, got: %v", r.Advisories)
		}
	}
	if !r.Pass {
		t.Fatalf("valid skill should pass, issues: %v", r.Issues)
	}
}

// R4 边界：钉死 > 的严格性（=500/=1024 不触发，>500/>1024 触发），防误改成 >=。
func TestAuditSkill_R4_Boundaries(t *testing.T) {
	cases := []struct {
		runes        int
		wantIssue    bool // description 过长
		wantAdvisory bool // description 偏长
	}{
		{500, false, false}, // =500：不触发（>500 才 advisory）
		{501, false, true},  // >500 advisory
		{1024, false, true}, // =1024：仍走 advisory（>500 且 ≤1024 的 else-if 分支）
		{1025, true, false}, // >1024 硬 issue（else-if 不再 advisory）
	}
	for _, c := range cases {
		desc := strings.Repeat("测", c.runes) // 精确控制 rune 数（不含 Use when/SKIP，R5/R6 另报但不干扰 R4 断言）
		sd := writeSkill(t, t.TempDir(), "my-skill", makeSkill("my-skill", desc, "pipeline", signalBody()))
		r, _ := AuditSkill(sd)
		hasIssue := false
		for _, iss := range r.Issues {
			if strings.Contains(iss, "description 过长") {
				hasIssue = true
			}
		}
		hasAdv := false
		for _, a := range r.Advisories {
			if strings.Contains(a, "偏长") {
				hasAdv = true
			}
		}
		if hasIssue != c.wantIssue {
			t.Errorf("runes=%d: 过长 issue=%v want=%v (issues: %v)", c.runes, hasIssue, c.wantIssue, r.Issues)
		}
		if hasAdv != c.wantAdvisory {
			t.Errorf("runes=%d: 偏长 advisory=%v want=%v (advisories: %v)", c.runes, hasAdv, c.wantAdvisory, r.Advisories)
		}
	}
}

// R12 triggers 声明：advisory 校验。直接测 checkTriggers（单元级，快）+ 经 AuditSkill
// （集成，确认 advisory 永不阻断 Pass）。

// triggersAdvisories 对 raw 跑 checkTriggers，返回其追加的 advisory。
func triggersAdvisories(t *testing.T, raw string) []string {
	t.Helper()
	var adv []string
	checkTriggers(raw, &adv)
	return adv
}

// advisoryContains 判断是否有 advisory 含 sub。
func advisoryContains(adv []string, sub string) bool {
	for _, a := range adv {
		if strings.Contains(a, sub) {
			return true
		}
	}
	return false
}

func TestCheckTriggers_Empty(t *testing.T) {
	if adv := triggersAdvisories(t, ""); len(adv) != 0 {
		t.Errorf("空 triggers 应无 advisory，got %v", adv)
	}
	if adv := triggersAdvisories(t, "   "); len(adv) != 0 {
		t.Errorf("纯空白 triggers 应无 advisory，got %v", adv)
	}
}

func TestCheckTriggers_InvalidJSON(t *testing.T) {
	adv := triggersAdvisories(t, "not-json")
	if !advisoryContains(adv, "非合法 JSON") {
		t.Errorf("非法 JSON 应报 advisory，got %v", adv)
	}
}

func TestCheckTriggers_Valid(t *testing.T) {
	raw := `[{"event":"Stop","when":"task_active_no_review"},{"event":"UserPromptSubmit","when":"coding_intent"}]`
	if adv := triggersAdvisories(t, raw); len(adv) != 0 {
		t.Errorf("合法 triggers 应无 advisory，got %v", adv)
	}
}

func TestCheckTriggers_BadEvent(t *testing.T) {
	raw := `[{"event":"Foo","when":"coding_intent"}]`
	adv := triggersAdvisories(t, raw)
	if !advisoryContains(adv, "event 非法") {
		t.Errorf("非法 event 应报 advisory，got %v", adv)
	}
}

func TestCheckTriggers_MissingEvent(t *testing.T) {
	raw := `[{"when":"coding_intent"}]`
	adv := triggersAdvisories(t, raw)
	if !advisoryContains(adv, "缺 event") {
		t.Errorf("缺 event 应报 advisory，got %v", adv)
	}
}

func TestCheckTriggers_MissingKeywordsAndWhen(t *testing.T) {
	raw := `[{"event":"Stop"}]`
	adv := triggersAdvisories(t, raw)
	if !advisoryContains(adv, "至少需一") {
		t.Errorf("缺 keywords+when 应报 advisory，got %v", adv)
	}
}

func TestCheckTriggers_BadWhen(t *testing.T) {
	raw := `[{"event":"Stop","when":"foo"}]`
	adv := triggersAdvisories(t, raw)
	if !advisoryContains(adv, "when 非法") {
		t.Errorf("非法 when 应报 advisory，got %v", adv)
	}
}

func TestCheckTriggers_MatchOnNonToolEvent(t *testing.T) {
	raw := `[{"event":"Stop","when":"coding_intent","match":"Bash"}]`
	adv := triggersAdvisories(t, raw)
	if !advisoryContains(adv, "match 仅对") {
		t.Errorf("非 tool 事件设 match 应报 advisory，got %v", adv)
	}
}

func TestCheckTriggers_ToolEventSuggestMatch(t *testing.T) {
	raw := `[{"event":"PreToolUse","when":"coding_intent"}]`
	adv := triggersAdvisories(t, raw)
	if !advisoryContains(adv, "建议带 match") {
		t.Errorf("tool 事件无 match 无 keywords 应建议 match，got %v", adv)
	}
}

func TestCheckTriggers_ToolEventWithKeywordsNoSuggest(t *testing.T) {
	raw := `[{"event":"PostToolUse","keywords":["TDD"],"match":"Bash"}]`
	if adv := triggersAdvisories(t, raw); advisoryContains(adv, "建议带 match") {
		t.Errorf("tool 事件已有 keywords/match 不应再建议，got %v", adv)
	}
}

// makeSkillWithTriggers 组装含 metadata.triggers 行的 SKILL.md。triggers 须为裸 JSON——
// frontmatter.go nestedRe 不剥嵌套 metadata 引号（仅 topLevelRe 对顶层字段剥）。
func makeSkillWithTriggers(name, desc, pattern, triggers, body string) string {
	return "---\nname: " + name + "\ndescription: \"" + desc +
		"\"\nmetadata:\n  pattern: " + pattern + "\n  domain: testing\n  triggers: " + triggers + "\n---\n\n" + body
}

// TestAuditSkill_R12_ValidTriggersNoAdvisory: 合法 triggers 经 AuditSkill 后无 R12 advisory。
func TestAuditSkill_R12_ValidTriggersNoAdvisory(t *testing.T) {
	raw := `[{"event":"Stop","when":"task_active_no_review"}]`
	sd := writeSkill(t, t.TempDir(), "r12-valid", makeSkillWithTriggers("r12-valid", longDesc(), "tool-wrapper", raw, signalBody()))
	r, err := AuditSkill(sd)
	must(t, err)
	for _, a := range r.Advisories {
		if strings.Contains(a, "triggers") {
			t.Errorf("合法 triggers 不应有 R12 advisory，got: %s", a)
		}
	}
}

// TestAuditSkill_R12_BadTriggersAdvisoryButPass: 非法 triggers 报 R12 advisory 但仍 Pass（advisory 不阻断）。
func TestAuditSkill_R12_BadTriggersAdvisoryButPass(t *testing.T) {
	sd := writeSkill(t, t.TempDir(), "r12-bad", makeSkillWithTriggers("r12-bad", longDesc(), "tool-wrapper", "not-json", signalBody()))
	r, err := AuditSkill(sd)
	must(t, err)
	if !r.Pass {
		t.Errorf("R12 advisory 不应阻断 Pass（issues: %v）", r.Issues)
	}
	if !advisoryContains(r.Advisories, "非合法 JSON") {
		t.Errorf("应报 R12 advisory，got: %v", r.Advisories)
	}
}

// drift 守卫 TestValidConditions_MatchEngine 见 internal/skilltrigger/conditions_test.go——
// skillsqa 测试不能 import skilltrigger（经 taskpipeline→skillsdist→skillsqa 成环），故
// 守卫放在引擎侧（skilltrigger_test→skillsqa 无环）。
