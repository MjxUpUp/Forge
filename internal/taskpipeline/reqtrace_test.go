package taskpipeline

import (
	"reflect"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// TestParseAcceptanceTraces 钉住 D1 两种追溯写法并存：前缀、行尾、围栏前缀、并集，
// 以及「非需求 ID 的 # 原样保留」边界。
func TestParseAcceptanceTraces(t *testing.T) {
	cases := []struct {
		name       string
		text       string
		wantRun    string
		wantExpect string
		wantTraces []string
	}{
		{"前缀写法", "accept[REQ-3]: go test ./x :: ok\n", "go test ./x", "ok", []string{"REQ-3"}},
		{"前缀多 ID", "- accept[REQ-1, NFR-2]: go vet ./...\n", "go vet ./...", "", []string{"REQ-1", "NFR-2"}},
		{"中文关键字前缀", "验收[INV-4]: go test ./y :: PASS\n", "go test ./y", "PASS", []string{"INV-4"}},
		{"行尾写法剥离后再拆期望", "accept: go test ./x :: ok #REQ-3 #REQ-4\n", "go test ./x", "ok", []string{"REQ-3", "REQ-4"}},
		{"前缀与行尾并集去重", "accept[REQ-1]: go test ./x :: ok #REQ-1 #REQ-2\n", "go test ./x", "ok", []string{"REQ-1", "REQ-2"}},
		{"围栏前缀", "```accept\n[REQ-7] go test ./z :: ok\n```\n", "go test ./z", "ok", []string{"REQ-7"}},
		{"围栏行尾", "```accept\ngo test ./z #REQ-8\n```\n", "go test ./z", "", []string{"REQ-8"}},
		{"非需求 ID 的 # 原样保留", "accept: echo a#b :: a#b\n", "echo a#b", "a#b", nil},
		{"shell test 方括号不是前缀", "```accept\n[ -f go.mod ] :: \n```\n", "[ -f go.mod ]", "", nil},
		{"行中 #REQ 不是行尾标记", "accept: grep '#REQ-1' f :: x\n", "grep '#REQ-1' f", "x", nil},
		{"Run/Expected 行对的行尾标记", "Run: go build ./...\nExpected: ok #REQ-9\n", "go build ./...", "ok", []string{"REQ-9"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseAcceptanceFromArtifact(tc.text)
			if len(got) != 1 {
				t.Fatalf("应提取 1 条, got %d: %+v", len(got), got)
			}
			c := got[0]
			if c.Run != tc.wantRun || c.Expected != tc.wantExpect {
				t.Errorf("Run/Expected = %q/%q, want %q/%q", c.Run, c.Expected, tc.wantRun, tc.wantExpect)
			}
			if !reflect.DeepEqual(c.Traces, tc.wantTraces) {
				t.Errorf("Traces = %v, want %v", c.Traces, tc.wantTraces)
			}
		})
	}

	t.Run("--accept 入口同样识别行尾写法", func(t *testing.T) {
		got := ParseAcceptance([]string{"go test ./a :: ok #REQ-5"})
		if got[0].Expected != "ok" || !reflect.DeepEqual(got[0].Traces, []string{"REQ-5"}) {
			t.Fatalf("got %+v", got[0])
		}
	})

	t.Run("同一道题两处追溯不同 ID 时合并取并集，身份不变", func(t *testing.T) {
		base := []AcceptanceCriterion{{Run: "go test ./x", Expected: "ok", Traces: []string{"REQ-1"}}}
		add := []AcceptanceCriterion{{Run: "go test ./x", Expected: "ok", Traces: []string{"REQ-2"}}}
		got := MergeAcceptance(base, add)
		if len(got) != 1 {
			t.Fatalf("同身份不应新增条目: %+v", got)
		}
		if !reflect.DeepEqual(got[0].Traces, []string{"REQ-1", "REQ-2"}) {
			t.Fatalf("Traces 应取并集: %v", got[0].Traces)
		}
	})
}

// TestRequirementCoverage 钉住覆盖对账三栏语义与产物声明扫描边界。
func TestRequirementCoverage(t *testing.T) {
	t.Run("声明扫描排除验收声明与代码围栏", func(t *testing.T) {
		text := "## 需求\n- REQ-1 登录\n- REQ-2 登出\n| NFR-1 | p99<200ms |\n\naccept[REQ-9]: go test ./x\n\n```go\n// REQ-77 示例\n```\n```accept\n[REQ-88] go vet ./...\n```\n"
		got := DeclaredRequirementIDs(text)
		want := []string{"NFR-1", "REQ-1", "REQ-2"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("declared = %v, want %v", got, want)
		}
	})

	t.Run("三栏对账", func(t *testing.T) {
		criteria := []AcceptanceCriterion{
			{Run: "a", Passed: true, Traces: []string{"REQ-1"}},
			{Run: "b", Passed: false, Traces: []string{"REQ-2"}},
			{Run: "c", Passed: true, Traces: []string{"REQ-2"}},
			{Run: "d", Passed: true, Traces: []string{"REQ-99"}},
		}
		cov := ComputeRequirementCoverage([]string{"REQ-1", "REQ-2", "REQ-3"}, criteria)
		if !reflect.DeepEqual(cov.Verified, []string{"REQ-1"}) {
			t.Errorf("verified = %v", cov.Verified)
		}
		if !reflect.DeepEqual(cov.UnverifiedIDs(), []string{"REQ-2", "REQ-3"}) {
			t.Errorf("unverified = %v", cov.UnverifiedIDs())
		}
		if cov.Unverified["REQ-3"] != "无验收追溯" || !strings.Contains(cov.Unverified["REQ-2"], "未通过") {
			t.Errorf("未验证原因不符: %v", cov.Unverified)
		}
		if !reflect.DeepEqual(cov.Orphans, []string{"REQ-99"}) {
			t.Errorf("orphans = %v", cov.Orphans)
		}
		out := FormatRequirementCoverage(cov)
		for _, frag := range []string{"已验证 1 / 声明 3", "未验证: REQ-3（无验收追溯）", "孤儿"} {
			if !strings.Contains(out, frag) {
				t.Errorf("渲染缺 %q:\n%s", frag, out)
			}
		}
	})

	t.Run("没用需求 ID 的任务不产生覆盖数据", func(t *testing.T) {
		cov := ComputeRequirementCoverage(nil, []AcceptanceCriterion{{Run: "go test ./..."}})
		if cov.HasTraceData() {
			t.Fatalf("无 ID 任务不应有覆盖数据: %+v", cov)
		}
	})

	t.Run("complete 检查：默认 advisory 落条目且不阻断", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		state := &TaskState{TaskRef: "feat/x"}
		ref, err := WriteArtifact(root, state.TaskRef, "spec", "- REQ-1 a\n- REQ-2 b\n")
		if err != nil {
			t.Fatal(err)
		}
		state.SpecArtifacts = map[string]ArtifactRef{"spec": ref}
		state.Acceptance = []AcceptanceCriterion{{Run: "true", Passed: true, Traces: []string{"REQ-1"}}}
		if err := CheckRequirementCoverageAtComplete(root, state); err != nil {
			t.Fatalf("默认链应只 advisory: %v", err)
		}
		entries, _ := checklog.LoadForTask(root, state.TaskRef)
		var found bool
		for _, e := range entries {
			if e.Check == CheckNameReqCoverage {
				found = true
				if e.Meta["unverified"] != "REQ-2" || e.Level != checklog.LevelAdvisory {
					t.Errorf("条目不符: %+v", e)
				}
			}
		}
		if !found {
			t.Fatal("应落 req-coverage 条目")
		}
	})

	t.Run("complete 检查：链把 spec 设为 human 时阻断，env 逃生放行", func(t *testing.T) {
		root := t.TempDir()
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		writeChainSchema(t, root, "version: 1\nstages:\n  - name: spec\n    mode: human\n")
		// 任务采用了追溯写法（REQ-2 有带追溯的验收），REQ-1 未验证 → 升档链上阻断。
		state := &TaskState{TaskRef: "feat/y", Acceptance: []AcceptanceCriterion{{Run: "true", Passed: true, Traces: []string{"REQ-2"}}}}
		ref, err := WriteArtifact(root, state.TaskRef, "spec", "- REQ-1 a\n- REQ-2 b\n")
		if err != nil {
			t.Fatal(err)
		}
		state.SpecArtifacts = map[string]ArtifactRef{"spec": ref}
		if err := CheckRequirementCoverageAtComplete(root, state); err == nil || !strings.Contains(err.Error(), "REQ-1") {
			t.Fatalf("应阻断并点名 REQ-1, got %v", err)
		}
		t.Setenv(reqCoverageDisableEnv, "disable")
		if err := CheckRequirementCoverageAtComplete(root, state); err != nil {
			t.Fatalf("逃生后应放行: %v", err)
		}
	})
}

// TestRequirementTraceReviewFixes 钉住审查轮修复：Run: 行末的追溯标记不丢、不混进
// 命令；Run:/Expected: 行不算需求声明；只在 spec 里提到 REQ 却从未写追溯的任务，
// 升档链上也不阻断。
func TestRequirementTraceReviewFixes(t *testing.T) {
	text := "Run: go test ./x #REQ-1\nExpected: ok\nRun: go vet ./... #REQ-2\n"
	for name, got := range map[string][]AcceptanceCriterion{
		"artifact": ParseAcceptanceFromArtifact(text),
		"plan":     ParseAcceptanceFromPlan(text),
	} {
		if len(got) != 2 || got[0].Run != "go test ./x" || got[0].Expected != "ok" || !reflect.DeepEqual(got[0].Traces, []string{"REQ-1"}) {
			t.Errorf("%s: 第 1 条 = %+v", name, got)
			continue
		}
		if got[1].Run != "go vet ./..." || !reflect.DeepEqual(got[1].Traces, []string{"REQ-2"}) {
			t.Errorf("%s: 裸 Run 行 = %+v", name, got[1])
		}
	}
	if ids := DeclaredRequirementIDs(text); len(ids) != 0 {
		t.Errorf("Run:/Expected: 行不是需求声明: %v", ids)
	}

	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	root := t.TempDir()
	writeChainSchema(t, root, "version: 1\nstages:\n  - name: spec\n    mode: human\n")
	state := &TaskState{TaskRef: "feat/legacy-ids", Acceptance: []AcceptanceCriterion{{Run: "true", Passed: true}}}
	ref, err := WriteArtifact(root, state.TaskRef, "spec", "- REQ-1 历史写法，从未写 accept[REQ-n]\n")
	if err != nil {
		t.Fatal(err)
	}
	state.SpecArtifacts = map[string]ArtifactRef{"spec": ref}
	if err := CheckRequirementCoverageAtComplete(root, state); err != nil {
		t.Fatalf("未采用追溯写法的任务不应被阻断: %v", err)
	}
}

// TestRefreshExtractedTraces 钉住重新提取的追溯刷新：spec-extract 条目以产物当前
// 声明为准（删掉的追溯不残留、改动的追溯替换、跨产物同题取并集）；其他来源的条目
// 不动；已不在产物里的 spec-extract 条目清空追溯。
func TestRefreshExtractedTraces(t *testing.T) {
	existing := []AcceptanceCriterion{
		{Run: "go test ./a", Source: AcceptanceSourceSpecExtract, Traces: []string{"REQ-1"}},
		{Run: "go test ./b", Source: AcceptanceSourceSpecExtract, Traces: []string{"REQ-2", "REQ-3"}},
		{Run: "go test ./c", Source: AcceptanceSourceSpecExtract, Traces: []string{"REQ-4"}},
		{Run: "go test ./d", Source: AcceptanceSourceStart, Traces: []string{"REQ-5"}},
	}
	fresh := []AcceptanceCriterion{
		{Run: "go test ./a", Traces: []string{"REQ-9"}},
		{Run: "go test ./b", Traces: []string{"REQ-2"}},
		{Run: "go test ./b", Traces: []string{"REQ-7"}},
		{Run: "go test ./d"},
	}
	got := RefreshExtractedTraces(existing, fresh)
	want := map[string][]string{
		"go test ./a": {"REQ-9"},
		"go test ./b": {"REQ-2", "REQ-7"},
		"go test ./c": nil,
		"go test ./d": {"REQ-5"},
	}
	for _, c := range got {
		if !reflect.DeepEqual(c.Traces, want[c.Run]) {
			t.Errorf("%s traces = %v, want %v", c.Run, c.Traces, want[c.Run])
		}
	}
}
