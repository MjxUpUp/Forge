package skilltrigger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseTriggers_ValidJSON(t *testing.T) {
	ts := ParseTriggers(`[{"event":"Stop","when":"x","cooldown":120}]`)
	if len(ts) != 1 || ts[0].Event != "Stop" || ts[0].When != "x" || ts[0].Cooldown != 120 {
		t.Fatalf("解析错误: %+v", ts)
	}
}

func TestParseTriggers_InvalidJSON(t *testing.T) {
	if ParseTriggers(`not json`) != nil {
		t.Fatal("非法 JSON 应返 nil")
	}
}

func TestParseTriggers_Empty(t *testing.T) {
	// ""/"  " → JSON 失败返 nil；"[]" → 合法空数组返空切片。三者对 LoadAll 的 len==0 判断等价。
	for _, raw := range []string{"", "  ", `[]`} {
		if len(ParseTriggers(raw)) != 0 {
			t.Fatalf("空输入 %q 应 len 0", raw)
		}
	}
}

// withCond 注册临时 condition 供 Eval 测试，测完自动清理（与 skillsqa.ValidConditions 解耦）。
func withCond(t *testing.T, name string, fn func(Context) bool) {
	t.Helper()
	Conditions[name] = fn
	t.Cleanup(func() { delete(Conditions, name) })
}

func TestEval_FilterByEvent(t *testing.T) {
	withCond(t, "c1", func(Context) bool { return true })
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop", When: "c1"}}}}
	if hits, _ := Eval(Context{Event: "UserPromptSubmit"}, all, nil); len(hits) != 0 {
		t.Fatalf("event 不匹配应 0 命中，got %d", len(hits))
	}
	hits, _ := Eval(Context{Event: "Stop"}, all, nil)
	if len(hits) != 1 || hits[0].Skill != "foo" {
		t.Fatalf("应命中 foo，got %+v", hits)
	}
}

func TestEval_KeywordsMatch(t *testing.T) {
	all := []SkillTriggers{{Skill: "bar", Triggers: []Trigger{
		{Event: "UserPromptSubmit", Keywords: []string{"飞书", "feishu"}},
	}}}
	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "帮我查飞书日程"}, all, nil); len(hits) != 1 {
		t.Fatalf("关键词命中应 1，got %d", len(hits))
	}
	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "今天天气"}, all, nil); len(hits) != 0 {
		t.Fatalf("无关键词命中应 0，got %d", len(hits))
	}
	// 大小写不敏感
	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "FEISHU login"}, all, nil); len(hits) != 1 {
		t.Fatalf("大小写不敏感应命中，got %d", len(hits))
	}
}

func TestEval_KeywordsAndCondition(t *testing.T) {
	Conditions["kwand"] = func(Context) bool { return true }
	defer delete(Conditions, "kwand")
	all := []SkillTriggers{{Skill: "baz", Triggers: []Trigger{
		{Event: "UserPromptSubmit", When: "kwand", Keywords: []string{"x"}},
	}}}
	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "x"}, all, nil); len(hits) != 1 {
		t.Fatalf("AND 都满足应命中，got %d", len(hits))
	}
	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "y"}, all, nil); len(hits) != 0 {
		t.Fatalf("关键词不满足 AND 应 0，got %d", len(hits))
	}
	Conditions["kwand"] = func(Context) bool { return false } // 覆盖测 false 分支
	if hits, _ := Eval(Context{Event: "UserPromptSubmit", Prompt: "x"}, all, nil); len(hits) != 0 {
		t.Fatalf("condition false AND 应 0，got %d", len(hits))
	}
}

func TestEval_DeniedSkills(t *testing.T) {
	withCond(t, "d", func(Context) bool { return true })
	all := []SkillTriggers{
		{Skill: "code-review-gate", Triggers: []Trigger{{Event: "Stop", When: "d"}}},
		{Skill: "skill-routing", Triggers: []Trigger{{Event: "Stop", When: "d"}}},
		{Skill: "normal", Triggers: []Trigger{{Event: "Stop", When: "d"}}},
	}
	hits, _ := Eval(Context{Event: "Stop"}, all, nil)
	if len(hits) != 1 || hits[0].Skill != "normal" {
		t.Fatalf("应只命中 normal（denied 跳过），got %+v", hits)
	}
}

func TestEval_StopMaxRounds(t *testing.T) {
	withCond(t, "s", func(Context) bool { return true })
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop", When: "s"}}}}
	noise := NewInMemoryNoiseController()
	now := time.Now()
	for i := 0; i < MaxStopRounds; i++ {
		hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: now}, all, noise)
		if len(hits) != 1 {
			t.Fatalf("第 %d 轮应命中，got %d", i+1, len(hits))
		}
		noise.IncrStopRound("s1") // 模拟 CLI 层落盘
	}
	if hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: now}, all, noise); len(hits) != 0 {
		t.Fatalf("第 %d 轮应被 max-rounds 抑制，got %d", MaxStopRounds+1, len(hits))
	}
}

func TestEval_Cooldown(t *testing.T) {
	withCond(t, "c", func(Context) bool { return true })
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop", When: "c"}}}}
	noise := NewInMemoryNoiseController()
	t0 := time.Now()
	hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0}, all, noise)
	if len(hits) != 1 {
		t.Fatal("首次应命中")
	}
	noise.Mark("s1", "foo", t0)
	if hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0.Add(10 * time.Second)}, all, noise); len(hits) != 0 {
		t.Fatalf("cooldown 内应不命中，got %d", len(hits))
	}
}

func TestEval_MultiTriggerCooldownMax(t *testing.T) {
	// F10 回归：同 skill 多 trigger 命中时 cooldown 取最大值，而非数组首条。
	// 若实现退化回首条 cooldown，90s（>首条60 但 <max120）会误命中，测试即暴露。
	withCond(t, "c", func(Context) bool { return true })
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{
		{Event: "Stop", When: "c", Cooldown: 60},  // 短冷却
		{Event: "Stop", When: "c", Cooldown: 120}, // 长冷却
	}}}
	noise := NewInMemoryNoiseController()
	t0 := time.Now()
	// 两 trigger 都命中 → cooldown = max(60, 120) = 120
	hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0}, all, noise)
	if len(hits) != 1 {
		t.Fatalf("首次应命中（两 trigger 都满足），got %d", len(hits))
	}
	noise.Mark("s1", "foo", t0)
	// 90s：在首条 60 cooldown 外，但在 max(120) cooldown 内 → 不应命中（验证取 max）
	if hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0.Add(90 * time.Second)}, all, noise); len(hits) != 0 {
		t.Fatalf("90s 在 max(120) cooldown 内应不命中（验证 cooldown 取 max 而非首条 60），got %d", len(hits))
	}
	// 121s：超 max(120) cooldown → 命中
	if hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0.Add(121 * time.Second)}, all, noise); len(hits) != 1 {
		t.Fatalf("121s 超 max(120) cooldown 应命中，got %d", len(hits))
	}
}

func TestEval_MatchToolName(t *testing.T) {
	withCond(t, "c", func(Context) bool { return true })
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{
		{Event: "PostToolUse", Match: "Bash", When: "c"},
	}}}
	if hits, _ := Eval(Context{Event: "PostToolUse", ToolName: "Bash"}, all, nil); len(hits) != 1 {
		t.Fatal("Bash 应命中")
	}
	if hits, _ := Eval(Context{Event: "PostToolUse", ToolName: "Write"}, all, nil); len(hits) != 0 {
		t.Fatal("Write 不应命中 Bash matcher")
	}
}

func TestLoadAll(t *testing.T) {
	dir := t.TempDir()
	// 有 triggers（裸 JSON——frontmatter.go nestedRe 不剥嵌套 metadata 引号，必须无外层引号）
	os.MkdirAll(filepath.Join(dir, "foo"), 0755)
	os.WriteFile(filepath.Join(dir, "foo", "SKILL.md"),
		[]byte("---\nname: foo\nmetadata:\n  triggers: [{\"event\":\"Stop\"}]\n---\nbody"), 0644)
	// 无 triggers
	os.MkdirAll(filepath.Join(dir, "bar"), 0755)
	os.WriteFile(filepath.Join(dir, "bar", "SKILL.md"), []byte("---\nname: bar\n---\n"), 0644)
	// denied
	os.MkdirAll(filepath.Join(dir, "code-review-gate"), 0755)
	os.WriteFile(filepath.Join(dir, "code-review-gate", "SKILL.md"),
		[]byte("---\nname: code-review-gate\nmetadata:\n  triggers: [{\"event\":\"Stop\"}]\n---\n"), 0644)
	// 非法 JSON triggers
	os.MkdirAll(filepath.Join(dir, "bad"), 0755)
	os.WriteFile(filepath.Join(dir, "bad", "SKILL.md"),
		[]byte("---\nname: bad\nmetadata:\n  triggers: not json\n---\n"), 0644)

	all := LoadAll(dir)
	if len(all) != 1 || all[0].Skill != "foo" {
		t.Fatalf("应只载入 foo（无 triggers/非法/denied 跳过），got %+v", all)
	}
}

func TestLoadAll_EmptyDir(t *testing.T) {
	if all := LoadAll(t.TempDir()); all != nil {
		t.Fatalf("空目录应返 nil，got %+v", all)
	}
	if all := LoadAll("/nonexistent/path"); all != nil {
		t.Fatalf("不存在目录应返 nil，got %+v", all)
	}
}

func TestMatchToolName(t *testing.T) {
	cases := []struct {
		match, tool string
		want        bool
	}{
		{"", "Bash", true},
		{"Bash", "Bash", true},
		{"Bash", "bash", true}, // 大小写不敏感
		{"Write|Edit", "Edit", true},
		{"Write|Edit", "Bash", false},
		{"Bash", "Write", false},
	}
	for _, c := range cases {
		if got := matchToolName(c.match, c.tool); got != c.want {
			t.Errorf("matchToolName(%q,%q)=%v want %v", c.match, c.tool, got, c.want)
		}
	}
}

// TestEval_SessionCap 钉住 session 级每 skill 硬封顶（2026-08 噪音审计：单 skill 单
// session 注入 79 次）：第 1 次完整注入（Reminder=false）、第 2 次短提醒
// （Reminder=true）、第 3 次起一律 SuppressSessionCap——cooldown 过期也不再放行。
func TestEval_SessionCap(t *testing.T) {
	withCond(t, "scap", func(Context) bool { return true })
	all := []SkillTriggers{{Skill: "foo", Triggers: []Trigger{{Event: "Stop", When: "scap"}}}}
	noise := NewInMemoryNoiseController()
	t0 := time.Now()

	hits, _ := Eval(Context{Event: "Stop", SessionID: "s1", Now: t0}, all, noise)
	if len(hits) != 1 || hits[0].Reminder {
		t.Fatalf("第 1 次应完整注入（Reminder=false），got %+v", hits)
	}
	noise.Mark("s1", "foo", t0)

	// cooldown（默认 60s）后第 2 次：放行但标 Reminder。
	t1 := t0.Add(61 * time.Second)
	hits, _ = Eval(Context{Event: "Stop", SessionID: "s1", Now: t1}, all, noise)
	if len(hits) != 1 || !hits[0].Reminder {
		t.Fatalf("第 2 次应放行且 Reminder=true，got %+v", hits)
	}
	noise.Mark("s1", "foo", t1)

	// 第 3 次：硬封顶抑制（cooldown 已过也不行），归因 SuppressSessionCap。
	t2 := t1.Add(61 * time.Second)
	hits, suppressed := Eval(Context{Event: "Stop", SessionID: "s1", Now: t2}, all, noise)
	if len(hits) != 0 {
		t.Fatalf("第 3 次应被 session 硬封顶抑制，got hits=%+v", hits)
	}
	if len(suppressed) != 1 || suppressed[0].Cause != SuppressSessionCap {
		t.Fatalf("抑制归因应为 SuppressSessionCap，got %+v", suppressed)
	}

	// 不同 session 不受影响。
	hits, _ = Eval(Context{Event: "Stop", SessionID: "s2", Now: t2}, all, noise)
	if len(hits) != 1 {
		t.Fatalf("新 session 应可注入，got %+v", hits)
	}
}

// TestEval_EventCap 钉住单次事件注入上限（2026-08-18 证据：一条 UserPromptSubmit 6ms
// 内 6 个 skill 全注入）：超 MaxHitsPerEvent 的按 RankHits 落选（prompt 命中优先于
// stdout 命中，同类保持声明顺序），归因 SuppressEventCap。
func TestEval_EventCap(t *testing.T) {
	mk := func(skill string) SkillTriggers {
		return SkillTriggers{Skill: skill, Triggers: []Trigger{
			{Event: "PostToolUse", Keywords: []string{"kw-" + skill}},
		}}
	}
	all := []SkillTriggers{mk("s1"), mk("s2"), mk("s3"), mk("s4")}
	// s2/s4 命中 prompt，s1/s3 命中 stdout → 注入序应为 s2, s4, s1（prompt 优先），s3 落选。
	ctx := Context{
		Event:      "PostToolUse",
		SessionID:  "s1",
		Prompt:     "kw-s2 kw-s4",
		ToolOutput: map[string]any{"stdout": "kw-s1 kw-s3"},
	}
	hits, suppressed := Eval(ctx, all, NewInMemoryNoiseController())
	if len(hits) != MaxHitsPerEvent {
		t.Fatalf("应只注入 %d 个，got %d", MaxHitsPerEvent, len(hits))
	}
	gotOrder := []string{hits[0].Skill, hits[1].Skill, hits[2].Skill}
	wantOrder := []string{"s2", "s4", "s1"}
	for i := range wantOrder {
		if gotOrder[i] != wantOrder[i] {
			t.Fatalf("注入序 %v, want %v（prompt 命中优先、同类声明序）", gotOrder, wantOrder)
		}
	}
	if len(suppressed) != 1 || suppressed[0].Skill != "s3" || suppressed[0].Cause != SuppressEventCap {
		t.Fatalf("落选应为 s3 + SuppressEventCap，got %+v", suppressed)
	}
	// 落选者不 Mark：不消耗 cooldown/封顶预算，下个事件仍可命中（SuppressEventCap
	// 与 SuppressSessionCap 的关键区别）。
	noise := NewInMemoryNoiseController()
	if _, sup2 := Eval(ctx, all, noise); len(sup2) != 1 {
		t.Fatalf("复跑应仍落选 s3，got %+v", sup2)
	}
	if got := noise.FireCount("s1", "s3"); got != 0 {
		t.Fatalf("落选者 FireCount 应为 0（未 Mark），got %d", got)
	}
	if !noise.ShouldFire("s1", "s3", DefaultCooldown*time.Second, time.Now()) {
		t.Fatal("落选者 ShouldFire 应仍为 true（cooldown 未被消耗）")
	}
}
