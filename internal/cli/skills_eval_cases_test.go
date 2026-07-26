package cli

// skills_eval_cases_test.go guards the C-component redaction contract: redactCases output omits Oracle.
// eval-cases is the core of privilege separation (the probe-running agent fetches cases through it); an Oracle leak would break
// the isolation of semi-automated evaluation — if a future refactor mistakenly adds Oracle back into redactedCase, this test must fail.
//
// skills_eval_cases_test.go — 守护 C 组件脱敏契约：redactCases 输出不含 Oracle。
// eval-cases 是权限分离的核心（跑 probe 的 agent 经它拿 case），Oracle 泄露会破坏
// 半自动评估的隔离——未来重构若误把 Oracle 加回 redactedCase，本测试必须失败。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

func TestRedactCases_OmitsOracle(t *testing.T) {
	in := []skillseval.EvalCase{
		{
			ID: "p1", Kind: skillseval.KindBehavior, Skill: "s",
			Prompt:         "in1",
			ProbeInput:     "in1",
			Oracle:         "contains:SECRET-ORACLE-MARKER",
			ProbeRationale: "why this probe",
		},
		{
			ID: "t1", Kind: skillseval.KindTrigger, Skill: "s",
			Prompt: "trig",
			Target: "s",
			Oracle: "trigger-case-oracle-should-also-be-redacted",
		},
	}
	out := redactCases(in)
	if len(out) != 2 {
		t.Fatalf("got %d redacted cases, want 2", len(out))
	}
	// Visible fields are preserved (ProbeInput / ProbeRationale must not be mistakenly dropped).
	//
	// 可显字段保留（ProbeInput / ProbeRationale 不该被误删）。
	if out[0].ProbeInput != "in1" || out[0].ProbeRationale != "why this probe" {
		t.Errorf("可显字段丢失：ProbeInput=%q ProbeRationale=%q", out[0].ProbeInput, out[0].ProbeRationale)
	}

	// After serialization the JSON must not contain the oracle field name, nor the original oracle value.
	//
	// 序列化后 JSON 不应含 oracle 字段名，也不应含 oracle 原文值。
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(strings.ToLower(s), "oracle") {
		t.Errorf("redacted JSON 含 oracle 字段/值（泄露）:\n%s", s)
	}
	if strings.Contains(s, "SECRET-ORACLE-MARKER") || strings.Contains(s, "trigger-case-oracle-should-also-be-redacted") {
		t.Errorf("redacted JSON 含 oracle 原文值（泄露）:\n%s", s)
	}
}

// TestBehaviorOnlyStats covers the pure-behavior detection helper of eval-record (C7):
// all-behavior → allBehavior=true with counts; mixed/empty → false.
//
// TestBehaviorOnlyStats 覆盖 eval-record 的纯 behavior 检测 helper（C7）：
// 全 behavior → allBehavior=true + 计数；混合/空 → false。
func TestBehaviorOnlyStats(t *testing.T) {
	allBeh := []skillseval.CaseResult{
		{Kind: skillseval.KindBehavior, Pass: true},
		{Kind: skillseval.KindBehavior, Pass: false},
	}
	ok, pass, total := behaviorOnlyStats(allBeh)
	if !ok || pass != 1 || total != 2 {
		t.Errorf("全 behavior：ok=%v pass=%d total=%d，want true/1/2", ok, pass, total)
	}

	mixed := []skillseval.CaseResult{
		{Kind: skillseval.KindBehavior, Pass: true},
		{Kind: skillseval.KindTrigger, Pass: true},
	}
	if ok, _, _ := behaviorOnlyStats(mixed); ok {
		t.Error("混合 results 应 allBehavior=false")
	}
	if ok, _, _ := behaviorOnlyStats(nil); ok {
		t.Error("空 results 应 allBehavior=false")
	}
}
