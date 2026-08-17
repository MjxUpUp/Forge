package skillseval

// mutex_test.go — tests for the mutex-set eval: edge parsing (incl. the trailing-qualifier
// variant and dangling-target drop), case generation stability (same input → same ID/prompt),
// record judgment (pass / fail / unknown skip / all-unknown error), confusion matrix, and
// the runs.jsonl round-trip.
//
// mutex_test.go — 互斥集 eval 测试：边解析（含尾缀限定变体与悬空目标丢弃）、case 生成
// 稳定性（同输入同 ID/prompt）、record 判定（pass/fail/未知跳过/全未知报错）、混淆矩阵、
// runs.jsonl 存读闭环。

import (
	"os"
	"path/filepath"
	"testing"
)

// writeMutexSkill writes a minimal SKILL.md with the given description into canonical.
//
// writeMutexSkill 在 canonical 下写一个带指定 description 的最小 SKILL.md。
func writeMutexSkill(t *testing.T, canonical, name, desc string) {
	t.Helper()
	sd := filepath.Join(canonical, name)
	if err := os.MkdirAll(sd, 0755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: \"" + desc + "\"\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// mutexFixture builds a canonical with three skills wired so that edge parsing can be
// asserted deterministically: a --skip--> b (plain CJK form + trailing-qualifier variant),
// a --skip--> gone (dangling, must be dropped), a --skip--> a (self, must be dropped),
// b --skip--> c (ASCII form).
//
// mutexFixture 造一个三 skill 的 canonical，让边解析可确定性断言：a --skip--> b
// （中文原形 + 尾缀限定变体）、a --skip--> gone（悬空，须丢弃）、a --skip--> a
// （自指，须丢弃）、b --skip--> c（英文写法）。
func mutexFixture(t *testing.T) string {
	t.Helper()
	canonical := t.TempDir()
	writeMutexSkill(t, canonical, "skill-a",
		"Use when: 处理 A 域任务时。SKIP: B 域任务（用 skill-b）、Rust 审查（用 skill-b，与门控叠加不替代）、不存在的目标（用 skill-gone）、自我让渡（用 skill-a）。")
	writeMutexSkill(t, canonical, "skill-b",
		"Use when: 处理 B 域任务时、写 B 组件时。SKIP: C 域任务 (use skill-c)。")
	writeMutexSkill(t, canonical, "skill-c",
		"Use when: 处理 C 域任务时。SKIP: 无让渡。")
	return canonical
}

func TestMutexEdges_Parsing(t *testing.T) {
	canonical := mutexFixture(t)
	edges, err := MutexEdges(canonical)
	if err != nil {
		t.Fatalf("MutexEdges: %v", err)
	}

	type pair struct{ from, to string }
	got := map[pair][]string{}
	for _, e := range edges {
		got[pair{e.From, e.To}] = append(got[pair{e.From, e.To}], e.Fragment)
	}

	// Plain CJK form + trailing-qualifier variant both land on skill-b: the regex takes
	// only the skill name — "，与门控叠加不替代" never leaks into the target. Note the
	// CJK comma inside the parenthesis is ALSO a skipSplitRe delimiter (same extraction
	// as ExtractTriggers), so the variant's fragment ends at "用 skill-b".
	//
	// 中文原形 + 尾缀限定变体都落到 skill-b：正则只取 skill 名——「，与门控叠加
	// 不替代」绝不漏进目标。注意括号内的中文逗号同样是 skipSplitRe 分隔符（与
	// ExtractTriggers 同一套提取），故变体的 fragment 止于「用 skill-b」。
	ab := got[pair{"skill-a", "skill-b"}]
	if len(ab) != 2 {
		t.Fatalf("a→b 边数=%d want 2（原形+尾缀变体）: %v", len(ab), ab)
	}
	if ab[0] != "B 域任务（用 skill-b）" || ab[1] != "Rust 审查（用 skill-b" {
		t.Fatalf("a→b fragment 不符预期（尾缀限定不得进目标/须被分隔）: %q", ab)
	}

	// ASCII form.
	//
	// 英文写法。
	if len(got[pair{"skill-b", "skill-c"}]) != 1 {
		t.Fatalf("b→c 边应解析出 1 条: %v", edges)
	}

	// Dangling target and self-delegation are dropped.
	//
	// 悬空目标与自我让渡被丢弃。
	if _, ok := got[pair{"skill-a", "skill-gone"}]; ok {
		t.Fatal("悬空目标（用 skill-gone）的边应被丢弃")
	}
	if _, ok := got[pair{"skill-a", "skill-a"}]; ok {
		t.Fatal("自我让渡边应被丢弃")
	}

	// Sorted by (From, To).
	//
	// 按 (From, To) 排序。
	for i := 1; i < len(edges); i++ {
		if edges[i].From < edges[i-1].From ||
			(edges[i].From == edges[i-1].From && edges[i].To < edges[i-1].To) {
			t.Fatalf("边未按 (From,To) 排序: %v → %v", edges[i-1], edges[i])
		}
	}
}

func TestMutexCases_GenerationAndStability(t *testing.T) {
	canonical := mutexFixture(t)
	c1, err := MutexCases(canonical)
	if err != nil {
		t.Fatalf("MutexCases: %v", err)
	}
	c2, err := MutexCases(canonical)
	if err != nil {
		t.Fatalf("MutexCases(second): %v", err)
	}
	if len(c1) == 0 {
		t.Fatal("应派生出 case")
	}
	if len(c1) != len(c2) {
		t.Fatalf("同输入 case 数漂移: %d vs %d", len(c1), len(c2))
	}
	for i := range c1 {
		if c1[i] != c2[i] {
			t.Fatalf("case[%d] 不稳定: %+v vs %+v", i, c1[i], c2[i])
		}
		if c1[i].ID == "" || c1[i].Prompt == "" || c1[i].Source == "" {
			t.Fatalf("case[%d] 字段缺失: %+v", i, c1[i])
		}
		if len(c1[i].ID) != 12 {
			t.Fatalf("case ID 长度=%d want 12: %q", len(c1[i].ID), c1[i].ID)
		}
	}

	// Per-edge cap: skill-b has 2 triggers, so a→b yields exactly 2 cases (cap not hit);
	// each case anchors Positive=b / Negative=a.
	//
	// 每边上限：skill-b 有 2 个 trigger，a→b 恰好 2 个 case（未触顶）；每个 case
	// 锚定 Positive=b / Negative=a。
	abCount := 0
	for _, c := range c1 {
		if c.Negative == "skill-a" && c.Positive == "skill-b" {
			abCount++
		}
	}
	if abCount != 2 {
		t.Fatalf("a→b case 数=%d want 2", abCount)
	}

	// ID anchors on the raw fragment: sha1("mutex:a:b:fragment")[:12].
	//
	// ID 锚定原始片段：sha1("mutex:a:b:fragment")[:12]。
	for _, c := range c1 {
		want := mutexCaseID(c.Negative, c.Positive, c.Source)
		if c.ID != want {
			t.Fatalf("case ID=%q want %q（锚定原始片段）", c.ID, want)
		}
	}

	// Prompt uses the trigger rendering (same renderer as single-skill eval).
	//
	// prompt 用 trigger 渲染（与单 skill eval 同一渲染器）。
	for _, c := range c1 {
		if c.Prompt != renderTriggerPrompt(c.Source) {
			t.Fatalf("prompt=%q want renderTriggerPrompt(%q)", c.Prompt, c.Source)
		}
	}
}

func TestMutexCases_SaveLoadRoundTrip(t *testing.T) {
	canonical := mutexFixture(t)
	cases, err := MutexCases(canonical)
	if err != nil {
		t.Fatalf("MutexCases: %v", err)
	}
	dir := t.TempDir()
	if err := SaveMutexCases(dir, cases); err != nil {
		t.Fatalf("SaveMutexCases: %v", err)
	}
	loaded, err := LoadMutexCases(dir)
	if err != nil {
		t.Fatalf("LoadMutexCases: %v", err)
	}
	if len(loaded) != len(cases) {
		t.Fatalf("回读 case 数=%d want %d", len(loaded), len(cases))
	}
	for i := range cases {
		if loaded[i] != cases[i] {
			t.Fatalf("回读 case[%d] 不等: %+v vs %+v", i, loaded[i], cases[i])
		}
	}

	// Missing file → nil,nil.
	//
	// 文件不存在 → nil,nil。
	none, err := LoadMutexCases(t.TempDir())
	if err != nil || none != nil {
		t.Fatalf("缺失文件应返回 nil,nil, got %v, %v", none, err)
	}

	// Empty set is a no-op write.
	//
	// 空集不写文件。
	dir2 := t.TempDir()
	if err := SaveMutexCases(dir2, nil); err != nil {
		t.Fatalf("SaveMutexCases(nil): %v", err)
	}
	if _, err := os.Stat(mutexCasesFile(dir2)); !os.IsNotExist(err) {
		t.Fatalf("空集不应落盘, stat err=%v", err)
	}
}

func TestRecordMutexRun_Judgment(t *testing.T) {
	canonical := mutexFixture(t)
	cases, err := MutexCases(canonical)
	if err != nil {
		t.Fatalf("MutexCases: %v", err)
	}
	names := []string{"skill-a", "skill-b", "skill-c"}
	dir := t.TempDir()

	// Pick one case per verdict: pass (actual==Positive), confusion (actual==Negative),
	// plain miss (actual==other), plus one unknown case_id (skipped).
	//
	// 每类判定各取一条：pass（actual==Positive）、混淆（actual==Negative）、
	// 普通失误（actual==其他），外加一条未知 case_id（跳过）。
	raw := []SubmitResult{
		{CaseID: cases[0].ID, ActualTriggered: cases[0].Positive},
		{CaseID: cases[1].ID, ActualTriggered: "（" + cases[1].Negative + "）。"}, // 带标点——归一化须剥掉
		{CaseID: cases[0].ID, ActualTriggered: "none"},
		{CaseID: "unknown-case", ActualTriggered: "skill-a"},
	}
	run, err := RecordMutexRun(dir, cases, names, "sonnet", "v1", raw)
	if err != nil {
		t.Fatalf("RecordMutexRun: %v", err)
	}
	if len(run.Results) != 3 {
		t.Fatalf("results=%d want 3（未知 case_id 跳过）", len(run.Results))
	}
	if !run.Results[0].Pass {
		t.Fatal("actual==Positive 应 pass")
	}
	if run.Results[1].Actual != cases[1].Negative {
		t.Fatalf("归一化应剥标点得 %q, got %q", cases[1].Negative, run.Results[1].Actual)
	}
	if run.Results[1].Pass {
		t.Fatal("actual==Negative 应 fail（头号混淆行）")
	}
	if run.Results[2].Actual != "" || run.Results[2].Pass {
		t.Fatalf("none 应归一化为 \"\"并 fail, got %+v", run.Results[2])
	}
	if run.RunID == "" || run.ForgeVersion != "v1" || run.AgentModel != "sonnet" {
		t.Fatalf("run 元数据未盖章: %+v", run)
	}

	// Persisted + readable.
	//
	// 已落盘且可读。
	latest, err := LatestMutexRun(dir)
	if err != nil || latest == nil {
		t.Fatalf("LatestMutexRun: %v, %v", latest, err)
	}
	if latest.RunID != run.RunID {
		t.Fatalf("latest.RunID=%q want %q", latest.RunID, run.RunID)
	}

	// All-unknown → explicit error (SubmitRun semantics), nothing persisted.
	//
	// 全未知 → 显式报错（SubmitRun 语义），不落盘。
	runsBefore, _ := LoadMutexRuns(dir)
	if _, err := RecordMutexRun(dir, cases, names, "m", "v", []SubmitResult{{CaseID: "nope", ActualTriggered: "x"}}); err == nil {
		t.Fatal("全未知 case_id 应显式报错")
	}
	runsAfter, _ := LoadMutexRuns(dir)
	if len(runsAfter) != len(runsBefore) {
		t.Fatal("全未知报错时不应落盘 run")
	}
}

func TestConfusionMatrix(t *testing.T) {
	cases := []MutexCase{
		{ID: "c1", Positive: "skill-b", Negative: "skill-a", Prompt: "p1"},
		{ID: "c2", Positive: "skill-b", Negative: "skill-a", Prompt: "p2"},
		{ID: "c3", Positive: "skill-c", Negative: "skill-b", Prompt: "p3"},
	}
	run := &MutexRun{Results: []MutexResult{
		{CaseID: "c1", Actual: "skill-b", Pass: true},
		{CaseID: "c2", Actual: "skill-a", Pass: false}, // 混淆行
		{CaseID: "c3", Actual: "", Pass: false},        // 普通失误
		{CaseID: "stale", Actual: "skill-a", Pass: false},
	}}
	m := ConfusionMatrix(run, cases)
	if m.Total != 3 || m.Passed != 1 {
		t.Fatalf("total=%d passed=%d want 3/1（stale 结果跳过）", m.Total, m.Passed)
	}
	if !m.GateBlocked {
		t.Fatal("存在 actual==Negative 行应 GateBlocked=true")
	}
	if len(m.Confusions) != 1 || m.Confusions[0].CaseID != "c2" {
		t.Fatalf("混淆清单=%+v want 仅 c2", m.Confusions)
	}
	// (positive, actual) aggregation: (b,b)=1, (b,a)=1, (c,"")=1.
	//
	// (positive, actual) 聚合：(b,b)=1、(b,a)=1、(c,"")=1。
	if len(m.Cells) != 3 {
		t.Fatalf("cells=%d want 3: %+v", len(m.Cells), m.Cells)
	}

	// No confusion → gate open.
	//
	// 无混淆 → 门禁放行。
	clean := &MutexRun{Results: []MutexResult{{CaseID: "c1", Actual: "skill-c", Pass: false}}}
	if m2 := ConfusionMatrix(clean, cases); m2.GateBlocked {
		t.Fatal("无 actual==Negative 行不应 GateBlocked")
	}

	// nil latest → empty matrix, Total==0 (nothing checked), gate open.
	//
	// nil latest → 空矩阵、Total==0（没检查任何东西）、门禁放行。
	m3 := ConfusionMatrix(nil, cases)
	if m3.Total != 0 || m3.GateBlocked {
		t.Fatalf("nil run 应得空矩阵: %+v", m3)
	}
}

func TestMutexRuns_BadLineSkipped(t *testing.T) {
	dir := t.TempDir()
	if err := AppendMutexRun(dir, &MutexRun{RunID: "r1"}); err != nil {
		t.Fatal(err)
	}
	// Append a corrupt line by hand — LoadMutexRuns must skip it, mirroring LoadRuns.
	//
	// 手工追加一行坏行——LoadMutexRuns 必须跳过，对齐 LoadRuns。
	f, err := os.OpenFile(mutexRunsFile(dir), os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{bad json\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := AppendMutexRun(dir, &MutexRun{RunID: "r2"}); err != nil {
		t.Fatal(err)
	}
	runs, err := LoadMutexRuns(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 2 || runs[0].RunID != "r1" || runs[1].RunID != "r2" {
		t.Fatalf("runs=%+v want r1,r2（坏行跳过）", runs)
	}
	latest, _ := LatestMutexRun(dir)
	if latest == nil || latest.RunID != "r2" {
		t.Fatalf("latest=%+v want r2", latest)
	}
}
