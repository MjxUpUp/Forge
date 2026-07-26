package skillsdecisions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidOutcome(t *testing.T) {
	for _, o := range []string{OutcomeAccept, OutcomeReject, OutcomeRevise, OutcomeDefer} {
		if !ValidOutcome(o) {
			t.Errorf("ValidOutcome(%q) = false, want true", o)
		}
	}
	for _, o := range []string{"", "unknown", "ACCEPT", "accept "} {
		if ValidOutcome(o) {
			t.Errorf("ValidOutcome(%q) = true, want false", o)
		}
	}
}

func TestNewDecisionID_Unique(t *testing.T) {
	seen := make(map[string]bool, 100)
	for i := 0; i < 100; i++ {
		id := NewDecisionID()
		if !strings.HasPrefix(id, "d-") {
			t.Fatalf("NewDecisionID() = %q, want prefix %q", id, "d-")
		}
		if seen[id] {
			t.Fatalf("NewDecisionID() collision: %q", id)
		}
		seen[id] = true
	}
}

func TestAppendLoadDecision_RoundTrip(t *testing.T) {
	canonical := t.TempDir()
	ts := time.Date(2026, 7, 25, 10, 0, 0, 0, time.UTC)
	in := SkillDecision{
		Skill:      "code-review-gate",
		Diagnosis:  "reviewer 漏检跨层数据类型不一致——DB 字段 int 但 API 返回 string",
		Revision:   "references/review-checklist.md 加「跨层数据类型一致性」检查项",
		Evidence:   "probe-run-1234: 5/6 pass，失败 1 例为跨层类型漂移；补检后 6/6",
		Outcome:    OutcomeAccept,
		Rationale:  "失败模式高频且可机械检测，纳入 checklist 收益大于噪声成本",
		CommitHash: "abc1234",
		ProbeRunID: "run-1234-abcd",
		By:         "claude-code",
		DecidedAt:  ts,
	}

	if err := AppendDecision(canonical, "code-review-gate", in); err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}

	out, err := LoadDecisions(canonical, "code-review-gate")
	if err != nil {
		t.Fatalf("LoadDecisions: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("LoadDecisions got %d decisions, want 1", len(out))
	}
	got := out[0]

	if got.ID == "" {
		t.Errorf("ID empty; AppendDecision should fill it")
	}
	in.ID = got.ID // AppendDecision 填了 ID，对齐后比较
	if !got.DecidedAt.Equal(ts) {
		t.Errorf("DecidedAt = %v, want %v", got.DecidedAt, ts)
	}
	// Compare fields one by one (DecidedAt already checked via Equal; remaining are string equality).
	//
	// 字段逐一比对（DecidedAt 用 Equal 已查；其余字符串相等）
	if got.Skill != in.Skill || got.Diagnosis != in.Diagnosis || got.Revision != in.Revision ||
		got.Evidence != in.Evidence || got.Outcome != in.Outcome || got.Rationale != in.Rationale ||
		got.CommitHash != in.CommitHash || got.ProbeRunID != in.ProbeRunID || got.By != in.By {
		t.Errorf("round-trip mismatch:\n got  = %+v\n want = %+v", got, in)
	}
}

func TestAppendLoadDecision_AllOutcomes(t *testing.T) {
	for _, o := range []string{OutcomeAccept, OutcomeReject, OutcomeRevise, OutcomeDefer} {
		canonical := t.TempDir()
		d := SkillDecision{
			Skill:     "s",
			Diagnosis: "d",
			Revision:  "r",
			Evidence:  "e",
			Outcome:   o,
		}
		if err := AppendDecision(canonical, "s", d); err != nil {
			t.Fatalf("AppendDecision(%s): %v", o, err)
		}
		out, _ := LoadDecisions(canonical, "s")
		if len(out) != 1 || out[0].Outcome != o {
			t.Fatalf("outcome %q round-trip: got %+v", o, out)
		}
	}
}

func TestAppendDecision_Multiple_AppendOnly(t *testing.T) {
	canonical := t.TempDir()
	for i := 0; i < 3; i++ {
		d := SkillDecision{
			Diagnosis: "diag",
			Revision:  "rev",
			Evidence:  "evi",
			Outcome:   OutcomeRevise,
			By:        "codex",
		}
		if err := AppendDecision(canonical, "skill-x", d); err != nil {
			t.Fatalf("AppendDecision #%d: %v", i, err)
		}
	}
	out, _ := LoadDecisions(canonical, "skill-x")
	if len(out) != 3 {
		t.Fatalf("got %d decisions, want 3 (append-only)", len(out))
	}
	for _, d := range out {
		if d.By != "codex" {
			t.Errorf("By = %q, want codex", d.By)
		}
	}
	// File starts with header (not ## [d-).
	//
	// 文件以 header 开头（不是 ## [d-）
	data, _ := os.ReadFile(DecisionsFile(canonical, "skill-x"))
	if !strings.HasPrefix(string(data), "# skill-x") {
		t.Errorf("decisions.md should start with header, got: %q", string(data)[:40])
	}
}

func TestLoadDecisions_NoFile(t *testing.T) {
	canonical := t.TempDir()
	out, err := LoadDecisions(canonical, "missing")
	if err != nil {
		t.Fatalf("LoadDecisions missing file: %v", err)
	}
	if out != nil {
		t.Errorf("LoadDecisions missing file got %v, want nil", out)
	}
}

func TestParseDecisions_TolerantMalformed(t *testing.T) {
	// Mix in a malformed level-2 heading (## [BAD-SECTION...) plus normal decisions. The bad heading and
	// its following lines must not pollute the previous decision body (Evidence etc.), nor be collected
	// into a fake decision.
	//
	// 混入坏 2 级标题（## [BAD-SECTION...）+ 正常决策。坏标题及其后续行不该污染
	// 前一条决策的 body（Evidence 等），也不该被收成一条假决策。
	md := `# skill — 持久决策历史

## [d-good1] accept

- **Skill**: skill

### Diagnosis

good diag

### Revision

good rev

### Evidence

good evi

## [BAD-SECTION no bracket

garbage line

## [d-good2] reject

- **Skill**: skill

### Diagnosis

d2

### Revision

r2

### Evidence

e2
`
	got := parseDecisions(md)
	// BAD-SECTION is not a decision header (not ## [d-); its whole section should be dropped, leaving only good1/good2.
	//
	// BAD-SECTION 非决策头（非 ## [d-），其整段应被丢弃，只留 good1/good2。
	if len(got) != 2 {
		t.Fatalf("got %d decisions, want 2（BAD-SECTION 不该成决策）: %+v", len(got), got)
	}
	var good1, good2 *SkillDecision
	for i := range got {
		switch got[i].ID {
		case "d-good1":
			good1 = &got[i]
		case "d-good2":
			good2 = &got[i]
		}
	}
	if good1 == nil {
		t.Fatal("good1 未解析出")
	}
	// body must stay clean — the garbage line after BAD-SECTION must not be sucked into good1.Evidence.
	//
	// body 必须干净——BAD-SECTION 后的 garbage line 不该吸进 good1.Evidence。
	if good1.Diagnosis != "good diag" || good1.Revision != "good rev" || good1.Evidence != "good evi" {
		t.Errorf("good1 body 被污染：Diagnosis=%q Revision=%q Evidence=%q", good1.Diagnosis, good1.Revision, good1.Evidence)
	}
	if good1.Outcome != OutcomeAccept {
		t.Errorf("good1 Outcome=%q want accept", good1.Outcome)
	}
	if good2 == nil || good2.Diagnosis != "d2" || good2.Evidence != "e2" || good2.Outcome != OutcomeReject {
		t.Errorf("good2 解析错: %+v", good2)
	}
}

func TestNormalizeOutcome(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"accept", "accept"},
		{"accept (with caveat)", "accept"}, // 尾注剥离
		{"accept  备注", "accept"},          // 空白后尾注剥离
		{"reject", "reject"},
		{"bogus", "bogus"},        // 无效原值，保留
		{"bogus (x)", "bogus (x)"}, // 首 token 也无效，保留原值不丢数据
	}
	for _, c := range cases {
		if got := normalizeOutcome(c.raw); got != c.want {
			t.Errorf("normalizeOutcome(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestAppendDecision_FillsDefaults(t *testing.T) {
	canonical := t.TempDir()
	// Fill only business fields; leave ID/Skill/DecidedAt empty — AppendDecision should populate them.
	//
	// 只填业务字段，ID/Skill/DecidedAt 留空，AppendDecision 应补全
	d := SkillDecision{
		Diagnosis: "diag",
		Revision:  "rev",
		Evidence:  "evi",
		Outcome:   OutcomeAccept,
	}
	if err := AppendDecision(canonical, "auto-skill", d); err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	out, _ := LoadDecisions(canonical, "auto-skill")
	if len(out) != 1 {
		t.Fatalf("got %d, want 1", len(out))
	}
	got := out[0]
	if !strings.HasPrefix(got.ID, "d-") {
		t.Errorf("ID not filled: %q", got.ID)
	}
	if got.Skill != "auto-skill" {
		t.Errorf("Skill not filled: %q", got.Skill)
	}
	if got.DecidedAt.IsZero() {
		t.Errorf("DecidedAt not filled")
	}
}

func TestDecisionsFile_Path(t *testing.T) {
	got := DecisionsFile(filepath.Join("a", "b"), "my-skill")
	want := filepath.Join("a", "b", "my-skill", "decisions.md")
	if got != want {
		t.Errorf("DecisionsFile = %q, want %q", got, want)
	}
}

func TestHeader_Output(t *testing.T) {
	got := header("my-skill")
	// Title line interpolates the skill name.
	//
	// 标题行插值 skill 名
	if !strings.HasPrefix(got, "# my-skill — 持久决策历史") {
		t.Fatalf("header title mismatch: %q", got)
	}
	// Descriptive tags + four-tuple explanation must be kept — aligns with skills/code-review-gate/decisions.md sample
	// (guards against generator/sample drift; this tag was once mistakenly deleted during third-party attribution cleanup, causing inconsistency).
	//
	// 描述性标签 + 四元组说明须保留——与 skills/code-review-gate/decisions.md 范例对齐
	//（防 generator 与 sample 漂移；曾因清理第三方归因时连此标签误删致两者不一致）。
	for _, want := range []string{"persistent decision history：", "每条决策记", "append-only", "审计/可复现"} {
		if !strings.Contains(got, want) {
			t.Errorf("header missing %q: %s", want, got)
		}
	}
	// Third-party project attribution should have been removed from the generator (header is the opening explanation of a new decisions.md, does not carry project names).
	//
	// 第三方项目归因应已从生成器清除（header 是新 decisions.md 的开头说明，不带项目名）。
	for _, banned := range []string{"SkillHone", "arXiv", "借鉴"} {
		if strings.Contains(got, banned) {
			t.Errorf("header still carries attribution %q: %s", banned, got)
		}
	}
}
