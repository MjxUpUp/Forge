package skillsdecisions

// verify_test.go — VerifyDecision 的测试：回填 round-trip、只验一次守卫、原地补丁的
// 字节保留（其余 section 逐字不动）、mid-file/EOF 两种插入位、双空行回归守卫
// （曾因条件性尾部空行在下一个 ## 前产生双空行）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// appendForVerify 写一条决策并返回解析出的 ID（测试辅助）。
func appendForVerify(t *testing.T, canonical, skill string, d SkillDecision) string {
	t.Helper()
	if err := AppendDecision(canonical, skill, d); err != nil {
		t.Fatalf("AppendDecision: %v", err)
	}
	out, err := LoadDecisions(canonical, skill)
	if err != nil || len(out) == 0 {
		t.Fatalf("LoadDecisions after append: %v, %d decisions", err, len(out))
	}
	return out[len(out)-1].ID
}

func TestVerifyDecision_RoundTrip(t *testing.T) {
	canonical := t.TempDir()
	id := appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis:  "触发率低",
		Revision:   "description 加触发词",
		Evidence:   "baseline 15%",
		Outcome:    OutcomeAccept,
		Prediction: "触发率应 15%→30%",
	})
	verifiedAt := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	if err := VerifyDecision(canonical, "skill-v", id, "命中：触发率 32%，超预测", verifiedAt); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}

	out, err := LoadDecisions(canonical, "skill-v")
	if err != nil || len(out) != 1 {
		t.Fatalf("LoadDecisions: %v, %d decisions", err, len(out))
	}
	got := out[0]
	if got.Prediction != "触发率应 15%→30%" {
		t.Errorf("Prediction = %q, want 声明值（round-trip）", got.Prediction)
	}
	if got.Verification != "命中：触发率 32%，超预测" {
		t.Errorf("Verification = %q, want 回填值", got.Verification)
	}
	if !got.VerifiedAt.Equal(verifiedAt) {
		t.Errorf("VerifiedAt = %v, want %v", got.VerifiedAt, verifiedAt)
	}
}

func TestVerifyDecision_AlreadyVerified(t *testing.T) {
	canonical := t.TempDir()
	id := appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d", Revision: "r", Evidence: "e", Outcome: OutcomeAccept,
	})
	if err := VerifyDecision(canonical, "skill-v", id, "命中", time.Now()); err != nil {
		t.Fatalf("首次 VerifyDecision: %v", err)
	}
	if err := VerifyDecision(canonical, "skill-v", id, "二次意见", time.Now()); err == nil {
		t.Fatal("第二次 VerifyDecision 应报错（每条决策只验证一次）")
	}
}

func TestVerifyDecision_NotFound(t *testing.T) {
	canonical := t.TempDir()
	appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d", Revision: "r", Evidence: "e", Outcome: OutcomeAccept,
	})
	if err := VerifyDecision(canonical, "skill-v", "d-nonexistent", "命中", time.Now()); err == nil {
		t.Fatal("未知决策 ID 应报错")
	}
}

func TestVerifyDecision_MissingFile(t *testing.T) {
	canonical := t.TempDir()
	err := VerifyDecision(canonical, "no-such-skill", "d-1", "命中", time.Now())
	if err == nil {
		t.Fatal("decisions.md 不存在应报错（提示先 forge skills decide）")
	}
	if !strings.Contains(err.Error(), "decide") {
		t.Errorf("错误应提示先记录决策: %v", err)
	}
}

func TestVerifyDecision_InvalidInputs(t *testing.T) {
	canonical := t.TempDir()
	// 路径遍历：与 Append/Load 同款守卫。
	if err := VerifyDecision(canonical, "../../x", "d-1", "命中", time.Now()); err == nil {
		t.Error("非法 skill 名应拒绝")
	}
	id := appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d", Revision: "r", Evidence: "e", Outcome: OutcomeAccept,
	})
	if err := VerifyDecision(canonical, "skill-v", "", "命中", time.Now()); err == nil {
		t.Error("空决策 ID 应拒绝")
	}
	if err := VerifyDecision(canonical, "skill-v", id, "  ", time.Now()); err == nil {
		t.Error("空验证结果应拒绝")
	}
}

// TestVerifyDecision_PreservesOtherSections 钉死原地补丁契约：验证「文件中部」的决策
// 时其余每个 section 必须逐字节不变，且不得在下一个头前产生双空行（条件性尾部空行
// bug 的回归守卫——end-1 处已有空行分隔）。
func TestVerifyDecision_PreservesOtherSections(t *testing.T) {
	canonical := t.TempDir()
	first := appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d1", Revision: "r1", Evidence: "e1", Outcome: OutcomeAccept,
	})
	appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d2", Revision: "r2", Evidence: "e2", Outcome: OutcomeRevise,
	})
	before, err := os.ReadFile(DecisionsFile(canonical, "skill-v"))
	if err != nil {
		t.Fatalf("read before: %v", err)
	}
	// 第二个 section = 从第二个 `## [` 头到 EOF；文件头 = 首个决策头之前的全部。
	// 补丁后都须原样。
	firstHeader := strings.Index(string(before), "## [")
	secondHeader := strings.Index(string(before)[firstHeader+1:], "## [")
	if firstHeader < 0 || secondHeader < 0 {
		t.Fatalf("找不到两个决策头: first=%d second=%d", firstHeader, secondHeader)
	}
	secondHeader += firstHeader + 1

	if err := VerifyDecision(canonical, "skill-v", first, "命中", time.Now()); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}
	after, err := os.ReadFile(DecisionsFile(canonical, "skill-v"))
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	afterStr := string(after)
	if !strings.HasSuffix(afterStr[secondHeader:], string(before)[secondHeader:]) {
		t.Errorf("第二个 section 被改动（应逐字保留）:\nbefore=%q\nafter =%q",
			string(before)[secondHeader:], afterStr[secondHeader:])
	}
	if !strings.HasPrefix(afterStr, string(before)[:strings.Index(string(before), "## [")]) {
		t.Errorf("文件头被改动（应逐字保留）: %q", afterStr[:40])
	}
	// 任何位置都不得出现三连换行 = 无双空行（已修的 bug）。
	if strings.Contains(afterStr, "\n\n\n") {
		t.Errorf("出现双空行（曾因条件性尾部空行引入）:\n%q", afterStr)
	}
}

// TestVerifyDecision_LastSectionEOF 覆盖 EOF 情形：补丁最后一个 section 时文件必须以
// 单个末换行收尾（EOF 处无双空行），且仍能解析回读。
func TestVerifyDecision_LastSectionEOF(t *testing.T) {
	canonical := t.TempDir()
	appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d1", Revision: "r1", Evidence: "e1", Outcome: OutcomeAccept,
	})
	last := appendForVerify(t, canonical, "skill-v", SkillDecision{
		Diagnosis: "d2", Revision: "r2", Evidence: "e2", Outcome: OutcomeDefer,
	})
	if err := VerifyDecision(canonical, "skill-v", last, "不可判：样本不足", time.Now()); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}
	data, err := os.ReadFile(DecisionsFile(canonical, "skill-v"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	s := string(data)
	if !strings.HasSuffix(s, "\n") || strings.HasSuffix(s, "\n\n") {
		t.Errorf("文件应以单个换行收尾: %q", tailOf(s, 30))
	}
	if strings.Contains(s, "\n\n\n") {
		t.Errorf("EOF 补丁引入双空行: %q", tailOf(s, 40))
	}
	out, err := LoadDecisions(canonical, "skill-v")
	if err != nil || len(out) != 2 {
		t.Fatalf("回读: %v, %d decisions", err, len(out))
	}
	if out[1].Verification != "不可判：样本不足" {
		t.Errorf("末条 Verification = %q", out[1].Verification)
	}
}

// TestVerifyDecision_HandWrittenMinimalSection 覆盖无字段行的手写极简 section：
// VerifiedAt 行紧跟头行插入，且该 section 仍可解析。
func TestVerifyDecision_HandWrittenMinimalSection(t *testing.T) {
	canonical := t.TempDir()
	path := DecisionsFile(canonical, "skill-v")
	// 复用标准文件头使文件形状与 AppendDecision 产物一致，再放手写极简 section
	//（无字段行，仅 Diagnosis 子节）。
	content := header("skill-v") + "\n## [d-hand] accept\n\n### Diagnosis\n\nhand diag\n"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := VerifyDecision(canonical, "skill-v", "d-hand", "命中", time.Now()); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}
	out, err := LoadDecisions(canonical, "skill-v")
	if err != nil || len(out) != 1 {
		t.Fatalf("回读: %v, %d decisions", err, len(out))
	}
	if out[0].ID != "d-hand" || out[0].VerifiedAt.IsZero() || out[0].Verification != "命中" {
		t.Errorf("手写极简 section 回填错: %+v", out[0])
	}
	if out[0].Diagnosis != "hand diag" {
		t.Errorf("Diagnosis 被污染: %q", out[0].Diagnosis)
	}
}

// tailOf 返回 s 末尾 n 字节（失败信息辅助）。
func tailOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// TestVerifyDecision_FieldAnchorStaysOutOfSubsections（审查 F1）：子节正文里的
// `- **粗体**: 值` 行是内容不是字段（parseDecisions 只在首个 ### 前取字段）。VerifiedAt
// 锚点必须留在连续字段块内才能解析回读——锚到正文时 VerifiedAt 读回零值而守卫拒绝重试：
// CLI 无法自愈的矛盾态。
func TestVerifyDecision_FieldAnchorStaysOutOfSubsections(t *testing.T) {
	canonical := t.TempDir()
	path := DecisionsFile(canonical, "skill-v")
	content := header("skill-v") +
		"\n## [d-f1] accept\n" +
		"- **Skill**: skill-v\n" +
		"- **DecidedAt**: 2026-08-16T09:00:00Z\n" +
		"\n### Diagnosis\n\n问题存在\n- **severity**: high（子节正文里的粗体列表项，非字段）\n" +
		"\n### Evidence\n\n- **pass rate**: 15%\n"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	verifiedAt := time.Date(2026, 8, 16, 10, 0, 0, 0, time.UTC)
	if err := VerifyDecision(canonical, "skill-v", "d-f1", "命中", verifiedAt); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}

	// 字段必须解析回读（锚点修复的意义所在）。
	out, err := LoadDecisions(canonical, "skill-v")
	if err != nil || len(out) != 1 {
		t.Fatalf("回读: %v, %d decisions", err, len(out))
	}
	if !out[0].VerifiedAt.Equal(verifiedAt) {
		t.Errorf("VerifiedAt = %v, want %v（字段被插进子节正文，解析不到）", out[0].VerifiedAt, verifiedAt)
	}
	if out[0].Verification != "命中" {
		t.Errorf("Verification = %q", out[0].Verification)
	}
	// 正文原样——粗体列表项仍是内容。
	if !strings.Contains(out[0].Evidence, "pass rate") || strings.Contains(out[0].Evidence, "VerifiedAt") {
		t.Errorf("Evidence 被字段行污染: %q", out[0].Evidence)
	}
	if !strings.Contains(out[0].Diagnosis, "severity") || strings.Contains(out[0].Diagnosis, "VerifiedAt") {
		t.Errorf("Diagnosis 被字段行污染: %q", out[0].Diagnosis)
	}
	// 字节级：VerifiedAt 行在首个子节之前，不在正文里。
	data, _ := os.ReadFile(path)
	s := string(data)
	if !(strings.Index(s, "- **VerifiedAt**") < strings.Index(s, "### Diagnosis")) {
		t.Errorf("VerifiedAt 行应位于字段块（### Diagnosis 之前）:\n%s", s)
	}
}

// TestVerifyDecision_HandWrittenVerificationHeaderGuard（审查 F4）：手写的
// `### Verification` 子节（无 `- **VerifiedAt**` 字段行）也必须触发只验一次守卫——
// 否则二次 verify 追加重复子节，解析器 last-wins 静默偏好后者（台账语义破坏）。
func TestVerifyDecision_HandWrittenVerificationHeaderGuard(t *testing.T) {
	canonical := t.TempDir()
	path := DecisionsFile(canonical, "skill-v")
	content := header("skill-v") +
		"\n## [d-f4] accept\n" +
		"- **Skill**: skill-v\n" +
		"\n### Diagnosis\n\nd\n" +
		"\n### Verification\n\n手写验证：命中\n"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := VerifyDecision(canonical, "skill-v", "d-f4", "二次验证", time.Now()); err == nil {
		t.Fatal("已有 ### Verification 子节（无字段行）应拒绝再验证")
	}
}

// TestVerifyDecision_MissingTrailingBlankSeparator（审查 F5）：手改 section 紧贴下一个
// 头收尾（无尾空行）时，`### Verification` 前必须补空行分隔——绝不紧贴正文。
func TestVerifyDecision_MissingTrailingBlankSeparator(t *testing.T) {
	canonical := t.TempDir()
	path := DecisionsFile(canonical, "skill-v")
	// 第一个 section 紧凑收尾（正文行直接接下一个 ## 头）。
	content := header("skill-v") +
		"\n## [d-f5a] accept\n" +
		"- **Skill**: skill-v\n" +
		"\n### Evidence\n\nflush ending" +
		"\n## [d-f5b] defer\n" +
		"- **Skill**: skill-v\n" +
		"\n### Evidence\n\nsecond\n"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := VerifyDecision(canonical, "skill-v", "d-f5a", "命中", time.Now()); err != nil {
		t.Fatalf("VerifyDecision: %v", err)
	}
	data, _ := os.ReadFile(path)
	s := string(data)
	if !strings.Contains(s, "flush ending\n\n### Verification") {
		t.Errorf("缺尾空行的 section 应补空行分隔再插 ### Verification:\n%s", s)
	}
	// 且仍可解析回读。
	out, err := LoadDecisions(canonical, "skill-v")
	if err != nil || len(out) != 2 {
		t.Fatalf("回读: %v, %d decisions", err, len(out))
	}
	if out[0].VerifiedAt.IsZero() || out[0].Verification != "命中" {
		t.Errorf("回填错: %+v", out[0])
	}
	if out[1].ID != "d-f5b" {
		t.Errorf("第二个 section 应原样: %+v", out[1])
	}
}
