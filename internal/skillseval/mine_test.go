package skillseval

// mine_test.go — 挖矿核心的钉子测试：原料漏斗计数、prompt_hash 去重（跨 session）、
// engaged 正负例分类、stop-cap advisory 不混入、无摘录时的诚实报告。

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

func mineEntry(skill, hash, excerpt, session string, at time.Time) checklog.Entry {
	return checklog.Entry{
		Check:     checklog.CheckSkillTrigger,
		Passed:    true,
		Checked:   true,
		SessionID: session,
		Detail:    checklog.DetailForSkillTrigger(skill, "UserPromptSubmit", "r"),
		Meta: map[string]string{
			checklog.MetaKeyPromptHash: hash,
			checklog.MetaKeyExcerpt:    excerpt,
		},
		RecordedAt: at,
	}
}

// TestMineGoldenDrafts_Basic classification + counts: engaged=true → trigger positive; engaged=false → not-trigger negative; entries without excerpts count toward TotalHits but never become drafts.
//
// TestMineGoldenDrafts_Basic 分类 + 计数：engaged=true → trigger 正例；engaged=false →
// not-trigger 负例；无摘录条目计入 TotalHits 但不进草稿。
func TestMineGoldenDrafts_Basic(t *testing.T) {
	t0 := time.Now()
	entries := []checklog.Entry{
		// engaged=true：命中后 2min 同 session Read 了 SKILL.md → 正例。
		mineEntry("foo", "h1", "帮我看编译报错", "s1", t0),
		// engaged=false：无任何后续 → near-miss 负例。
		mineEntry("foo", "h2", "随便聊聊 build", "s2", t0),
		// 无摘录：计入 TotalHits，不进草稿。
		{
			Check: checklog.CheckSkillTrigger, SessionID: "s3",
			Detail:     checklog.DetailForSkillTrigger("foo", "UserPromptSubmit", "r"),
			Meta:       map[string]string{checklog.MetaKeyPromptHash: "h3"},
			RecordedAt: t0,
		},
	}
	calls := []toolusage.ToolCall{{
		ToolName: "Read", SessionID: "s1", Timestamp: t0.Add(2 * time.Minute),
		ToolInput: `{"file_path":"/x/skills/foo/SKILL.md"}`,
	}}
	rep := MineGoldenDrafts(entries, calls, "")
	if rep.TotalHits != 3 || rep.WithExcerpt != 2 || rep.Deduped != 2 {
		t.Fatalf("原料漏斗: hits=%d excerpt=%d deduped=%d, want 3/2/2", rep.TotalHits, rep.WithExcerpt, rep.Deduped)
	}
	drafts := rep.Skills["foo"]
	if len(drafts) != 2 {
		t.Fatalf("草稿数 %d, want 2", len(drafts))
	}
	byHash := map[string]MinedCase{}
	for _, d := range drafts {
		byHash[d.PromptHash] = d
		if d.PromptHash == "h3" {
			t.Fatal("无摘录条目（h3）不得成为草稿")
		}
	}
	if byHash["h1"].Kind != KindTrigger {
		t.Fatalf("h1 应为 trigger 正例（engaged=true）, got %q", byHash["h1"].Kind)
	}
	if byHash["h2"].Kind != KindNotTrigger {
		t.Fatalf("h2 应为 not-trigger 负例（engaged=false）, got %q", byHash["h2"].Kind)
	}
}

// TestMineGoldenDrafts_DedupedCountsPostCap pins review n2: Deduped counts the drafts that actually survive the per-skill cap — the funnel number must not exceed the artifact count.
//
// TestMineGoldenDrafts_DedupedCountsPostCap 钉死 review n2：Deduped 按截断后实际
// 落盘的草稿计数——漏斗数字不得大于产物数。
func TestMineGoldenDrafts_DedupedCountsPostCap(t *testing.T) {
	t0 := time.Now()
	var entries []checklog.Entry
	for i := 0; i < MaxMinedPerSkill+5; i++ {
		entries = append(entries, mineEntry("foo", fmt.Sprintf("h%03d", i), "excerpt", "s", t0.Add(time.Duration(i)*time.Minute)))
	}
	rep := MineGoldenDrafts(entries, nil, "")
	if rep.Deduped != MaxMinedPerSkill || len(rep.Skills["foo"]) != MaxMinedPerSkill {
		t.Fatalf("Deduped=%d drafts=%d, want both = cap %d", rep.Deduped, len(rep.Skills["foo"]), MaxMinedPerSkill)
	}
}

// TestMineGoldenDrafts_DedupByHash same prompt_hash dedups across sessions, keeping the latest hit.
//
// TestMineGoldenDrafts_DedupByHash 同 prompt_hash 跨 session 去重，保留最新命中。
func TestMineGoldenDrafts_DedupByHash(t *testing.T) {
	t0 := time.Now()
	entries := []checklog.Entry{
		mineEntry("foo", "same", "旧摘录", "s1", t0),
		mineEntry("foo", "same", "新摘录", "s2", t0.Add(time.Hour)),
	}
	rep := MineGoldenDrafts(entries, nil, "")
	if rep.Deduped != 1 || len(rep.Skills["foo"]) != 1 {
		t.Fatalf("同 hash 应去重为 1: deduped=%d drafts=%d", rep.Deduped, len(rep.Skills["foo"]))
	}
	if rep.Skills["foo"][0].Excerpt != "新摘录" {
		t.Fatalf("应保留最新命中的摘录, got %q", rep.Skills["foo"][0].Excerpt)
	}
}

// TestMineGoldenDrafts_StopCapExcluded a stop-cap warn advisory's Detail carries no " hit (" marker — SkillFromTriggerDetail returns "" and it is naturally excluded from drafts.
//
// TestMineGoldenDrafts_StopCapExcluded stop-cap warn advisory 的 Detail 无 " hit ("
// 标记——SkillFromTriggerDetail 返回 ""，天然排除在草稿外。
func TestMineGoldenDrafts_StopCapExcluded(t *testing.T) {
	warn := checklog.Entry{
		Check: checklog.CheckSkillTrigger, SessionID: "s1",
		Detail:     "skill-trigger: stop-round-cap 达到上限，抑制 1 个潜在注入（foo）",
		Meta:       map[string]string{checklog.MetaKeyCause: "stop-max-rounds"},
		RecordedAt: time.Now(),
	}
	rep := MineGoldenDrafts([]checklog.Entry{warn}, nil, "")
	if rep.TotalHits != 0 || rep.Deduped != 0 {
		t.Fatalf("advisory 不得计入原料: %+v", rep)
	}
}

// TestMineGoldenDrafts_SkillFilter --skill filter mines only the target skill.
//
// TestMineGoldenDrafts_SkillFilter --skill 过滤只挖目标 skill。
func TestMineGoldenDrafts_SkillFilter(t *testing.T) {
	t0 := time.Now()
	entries := []checklog.Entry{
		mineEntry("foo", "h1", "a", "s1", t0),
		mineEntry("bar", "h2", "b", "s1", t0),
	}
	rep := MineGoldenDrafts(entries, nil, "foo")
	if len(rep.Skills) != 1 || len(rep.Skills["foo"]) != 1 {
		t.Fatalf("--skill 过滤失效: %+v", rep.Skills)
	}
	if rep.TotalHits != 1 {
		t.Fatalf("TotalHits 应只计目标 skill, got %d", rep.TotalHits)
	}
}

// TestSanitizeDraft second-layer redaction + home-path folding.
//
// TestSanitizeDraft 二次脱敏 + home 路径折叠。
func TestSanitizeDraft(t *testing.T) {
	out := SanitizeDraft("path /Users/alice/proj 出现 token: ghp_abcdefghij0123456789klmnop", "/Users/alice")
	if out == "" {
		t.Fatal("sanitize 不得清空草稿")
	}
	if !strings.Contains(out, "~") {
		t.Fatalf("home 路径应折叠为 ~, got %q", out)
	}
	if strings.Contains(out, "ghp_abcdefghij0123456789klmnop") {
		t.Fatalf("token 应被二次脱敏, got %q", out)
	}
}
