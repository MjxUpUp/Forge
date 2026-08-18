package skillseval

// keyword_test.go — per-keyword 分析层的钉子测试：计数/engaged/suppressed 归位、
// condition-only 分行、死关键词检测、排序稳定性、advisory 排除。
//
// keyword_test.go — pins for the per-keyword analysis layer: counts/engaged/
// suppressed placement, condition-only row separation, dead-keyword detection,
// sort stability, advisory exclusion.

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

func kwEntry(skill, keyword, session string, at time.Time, suppressed string) checklog.Entry {
	meta := map[string]string{checklog.MetaKeyTriggerIndex: "0"}
	if keyword != "" {
		meta[checklog.MetaKeyMatchedKeyword] = keyword
	}
	if suppressed != "" {
		meta[checklog.MetaKeySuppressedSinceLast] = suppressed
	}
	return checklog.Entry{
		Check: checklog.CheckSkillTrigger, Passed: true, Checked: true,
		SessionID: session,
		Detail:    checklog.DetailForSkillTrigger(skill, "UserPromptSubmit", "r"),
		Meta:      meta, RecordedAt: at,
	}
}

// TestAnalyzeKeywords_Basic 计数归位：matched_keyword 切片、engaged join、suppressed
// 求和、condition-only 分行、TotalHits 含全部命中。
//
// TestAnalyzeKeywords_Basic counts placement: matched_keyword slicing, engaged join,
// suppressed sum, condition-only separation, TotalHits covers every hit.
func TestAnalyzeKeywords_Basic(t *testing.T) {
	t0 := time.Now()
	entries := []checklog.Entry{
		// foo×编译报错：2 次命中，仅第 2 次 engaged——engagedAfter 是 per-hit 归因
		//（命中后 10min 窗口内有 Read 即算），第 1 次命中与 Read 相隔 11min 不算。
		kwEntry("foo", "编译报错", "s1", t0, ""),
		kwEntry("foo", "编译报错", "s1", t0.Add(11*time.Minute), "3"),
		// foo×发版：1 次命中，无 engaged。
		kwEntry("foo", "发版", "s2", t0, ""),
		// condition-only（无 matched_keyword 键）。
		kwEntry("bar", "", "s3", t0, ""),
	}
	calls := []toolusage.ToolCall{{
		ToolName: "Read", SessionID: "s1", Timestamp: t0.Add(13 * time.Minute),
		ToolInput: `{"file_path":"/x/skills/foo/SKILL.md"}`,
	}}
	rep := AnalyzeKeywords(entries, calls, nil)
	if rep.TotalHits != 4 {
		t.Fatalf("TotalHits=%d, want 4", rep.TotalHits)
	}
	byKw := map[string]KeywordStat{}
	for _, st := range rep.Stats {
		byKw[st.Skill+"|"+st.Keyword] = st
	}
	foo := byKw["foo|编译报错"]
	if foo.Hits != 2 || foo.Engaged != 1 || foo.Suppressed != 3 {
		t.Fatalf("foo|编译报错 = %+v, want {2 1 3}", foo)
	}
	if fa := byKw["foo|发版"]; fa.Hits != 1 || fa.Engaged != 0 {
		t.Fatalf("foo|发版 = %+v, want {1 0 _}", fa)
	}
	if len(rep.ConditionOnly) != 1 || rep.ConditionOnly[0].Hits != 1 || rep.ConditionOnly[0].Skill != "bar" {
		t.Fatalf("condition-only 行 = %+v（per-skill，不得 last-writer-wins）", rep.ConditionOnly)
	}
	if rep.V2Hits != 4 {
		t.Fatalf("V2Hits=%d, want 4（每条均带 trigger_index）", rep.V2Hits)
	}
	// 命中降序：编译报错(2) 在 发版(1) 前。
	if rep.Stats[0].Keyword != "编译报错" {
		t.Fatalf("排序应命中降序: %+v", rep.Stats)
	}
}

// TestAnalyzeKeywords_DeadKeywords 声明 ∖ 命中 = 死关键词（排序稳定、nil declared 跳过）。
//
// TestAnalyzeKeywords_DeadKeywords declared ∖ hit = dead keywords (sorted stable,
// nil declared skips detection).
func TestAnalyzeKeywords_DeadKeywords(t *testing.T) {
	t0 := time.Now()
	entries := []checklog.Entry{kwEntry("foo", "编译报错", "s1", t0, "")}
	declared := map[string][]string{
		"foo": {"编译报错", "死词b", "死词a"}, // 死词a/b 声明未命中
		"bar": {"bar独有死词"},            // bar 零命中，全部声明为死词
	}
	rep := AnalyzeKeywords(entries, nil, declared)
	var got []string
	for _, d := range rep.DeadKeywords {
		got = append(got, d.Skill+"|"+d.Keyword)
	}
	want := []string{"bar|bar独有死词", "foo|死词a", "foo|死词b"} // skill 升序、词升序
	if len(got) != len(want) {
		t.Fatalf("死关键词 = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("死关键词[%d]=%s, want %s（排序不稳定）", i, got[i], want[i])
		}
	}
	// nil declared → 跳过检测。
	rep2 := AnalyzeKeywords(entries, nil, nil)
	if len(rep2.DeadKeywords) != 0 {
		t.Fatalf("nil declared 应跳过死关键词检测, got %v", rep2.DeadKeywords)
	}
}

// TestAnalyzeKeywords_DeadGatedOnV2Era 死关键词门槛：全 v1 窗口（无 Meta 归因）时检测
// 整体停用——"零命中"混同「从未命中」与「归因上线前命中过」（v2 上线首日 382 个幻影
// 死词的生产实况）。
//
// TestAnalyzeKeywords_DeadGatedOnV2Era the dead-keyword gate: with an all-v1 window
// (no Meta attribution) detection is disabled entirely — "zero hits" would conflate
// "never fired" with "fired before attribution existed" (production reality: 382
// phantom dead keywords on v2's first day).
func TestAnalyzeKeywords_DeadGatedOnV2Era(t *testing.T) {
	v1 := checklog.Entry{
		Check: checklog.CheckSkillTrigger, Passed: true, Checked: true,
		SessionID:  "s1",
		Detail:     checklog.DetailForSkillTrigger("foo", "UserPromptSubmit", "r"),
		RecordedAt: time.Now(),
	} // 无 Meta——v1 时代条目
	declared := map[string][]string{"foo": {"从未命中词"}}
	rep := AnalyzeKeywords([]checklog.Entry{v1}, nil, declared)
	if rep.V2Hits != 0 {
		t.Fatalf("V2Hits=%d, want 0", rep.V2Hits)
	}
	if len(rep.DeadKeywords) != 0 {
		t.Fatalf("全 v1 窗口不得报死关键词（幻影死词）, got %v", rep.DeadKeywords)
	}
	if len(rep.ConditionOnly) != 1 || rep.ConditionOnly[0].Hits != 1 {
		t.Fatalf("v1 条目应归入 condition/legacy 行: %+v", rep.ConditionOnly)
	}
}

// TestAnalyzeKeywords_MixedWindowDisablesDead 混合窗口门槛（review M1）：窗口内混有
// 任一 v1 条目即停用死词检测——v1 命中对 hit 集零贡献，某词"最后一次命中落在归因
// 上线前"会被误判死。retention 30 天内 v2 上线初期窗口必然混合。
//
// TestAnalyzeKeywords_MixedWindowDisablesDead the mixed-window gate (review M1): ANY
// v1 entry in the window disables dead-keyword detection — v1 hits contribute nothing
// to the hit set, so a word whose last hit predates attribution would falsely read
// dead. Within the 30-day retention window, early post-v2 windows are inevitably
// mixed.
func TestAnalyzeKeywords_MixedWindowDisablesDead(t *testing.T) {
	t0 := time.Now()
	entries := []checklog.Entry{
		kwEntry("foo", "编译报错", "s1", t0, ""), // v2 条目
		{ // v1 条目（无 Meta）
			Check: checklog.CheckSkillTrigger, Passed: true, Checked: true,
			SessionID:  "s0",
			Detail:     checklog.DetailForSkillTrigger("foo", "UserPromptSubmit", "r"),
			RecordedAt: t0.Add(-time.Hour),
		},
	}
	declared := map[string][]string{"foo": {"编译报错", "混合窗死词"}}
	rep := AnalyzeKeywords(entries, nil, declared)
	if rep.TotalHits != 2 || rep.V2Hits != 1 {
		t.Fatalf("TotalHits=%d V2Hits=%d, want 2/1", rep.TotalHits, rep.V2Hits)
	}
	if len(rep.DeadKeywords) != 0 {
		t.Fatalf("混合窗口不得报死关键词（v1 命中不可见≠词已死）, got %v", rep.DeadKeywords)
	}
}

// TestAnalyzeKeywords_ConditionOnlyCountsAsV2 v2 的 condition-only 命中（无关键词但有
// trigger_index）计入 V2Hits——纯 condition 窗口满足归因完备、死词检测开启（载荷路径，
// review n3）。
//
// TestAnalyzeKeywords_ConditionOnlyCountsAsV2 a v2 condition-only hit (no keyword but
// trigger_index present) counts toward V2Hits — a pure-condition window satisfies
// attribution completeness and dead detection opens (the load-bearing path, review n3).
func TestAnalyzeKeywords_ConditionOnlyCountsAsV2(t *testing.T) {
	entries := []checklog.Entry{
		kwEntry("foo", "", "s1", time.Now(), ""), // condition-only 但带 trigger_index
	}
	declared := map[string][]string{"foo": {"纯condition窗口死词"}}
	rep := AnalyzeKeywords(entries, nil, declared)
	if rep.V2Hits != 1 || rep.TotalHits != 1 {
		t.Fatalf("V2Hits=%d TotalHits=%d, want 1/1（condition-only v2 命中计入门槛）", rep.V2Hits, rep.TotalHits)
	}
	if len(rep.DeadKeywords) != 1 || rep.DeadKeywords[0].Keyword != "纯condition窗口死词" {
		t.Fatalf("归因完备窗口应报死词, got %+v", rep.DeadKeywords)
	}
}

// TestAnalyzeKeywords_AdvisoryExcluded stop-cap advisory（无 hit 标记）不进任何统计。
//
// TestAnalyzeKeywords_AdvisoryExcluded stop-cap advisories (no hit marker) never
// enter any stat.
func TestAnalyzeKeywords_AdvisoryExcluded(t *testing.T) {
	warn := checklog.Entry{
		Check: checklog.CheckSkillTrigger, SessionID: "s1",
		Detail:     "skill-trigger: stop-round-cap 达到上限，抑制 1 个潜在注入（foo）",
		Meta:       map[string]string{checklog.MetaKeyCause: "stop-max-rounds"},
		RecordedAt: time.Now(),
	}
	rep := AnalyzeKeywords([]checklog.Entry{warn}, nil, nil)
	if rep.TotalHits != 0 || len(rep.Stats) != 0 || len(rep.ConditionOnly) != 0 {
		t.Fatalf("advisory 不得计入: %+v", rep)
	}
}
