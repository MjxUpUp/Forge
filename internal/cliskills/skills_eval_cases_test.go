package cliskills

// skills_eval_cases_test.go — 守护 eval-cases 输出视图：caseViews 恰好输出
// id/kind/prompt/target——agent dispatch 跑 prompt 与回填结果所需的字段。

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillseval"
)

func TestCaseViews_OutputShape(t *testing.T) {
	in := []skillseval.EvalCase{
		{ID: "t1", Kind: skillseval.KindTrigger, Skill: "s", Prompt: "trig", Target: "s", SourceFragment: "frag", DescHash: "dh"},
		{ID: "n1", Kind: skillseval.KindNotTrigger, Skill: "s", Prompt: "skip"},
	}
	out := caseViews(in)
	if len(out) != 2 {
		t.Fatalf("got %d cases, want 2", len(out))
	}
	if out[0].ID != "t1" || out[0].Kind != skillseval.KindTrigger || out[0].Prompt != "trig" || out[0].Target != "s" {
		t.Errorf("字段丢失：%+v", out[0])
	}
	if out[1].Target != "" {
		t.Errorf("not-trigger 类 Target 应空, got %q", out[1].Target)
	}

	// 序列化后的 JSON 不应带内部簿记字段（source_fragment / desc_hash）。
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if strings.Contains(s, "source_fragment") || strings.Contains(s, "desc_hash") {
		t.Errorf("输出视图不应含内部字段:\n%s", s)
	}
}
