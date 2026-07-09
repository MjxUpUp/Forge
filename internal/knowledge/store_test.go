package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setHomeTemp sets HOME (and USERPROFILE on Windows) to a temp dir and returns a cleanup function.
func setHomeTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })
	// On Windows, os.UserHomeDir() reads USERPROFILE, not HOME.
	origUserProfile := os.Getenv("USERPROFILE")
	os.Setenv("USERPROFILE", dir)
	t.Cleanup(func() { os.Setenv("USERPROFILE", origUserProfile) })
	return dir
}

func TestLoadIndexEmpty(t *testing.T) {
	setHomeTemp(t)

	idx, err := LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex() returned error: %v", err)
	}
	if idx.Version != "2.0" {
		t.Fatalf("expected version 2.0, got %q", idx.Version)
	}
	if len(idx.Entries) != 0 {
		t.Fatalf("expected empty entries, got %d", len(idx.Entries))
	}
}

func TestSaveAndLoadIndex(t *testing.T) {
	setHomeTemp(t)

	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{
				ID:          "gotchas-123",
				Category:    "gotchas",
				Title:       "Test entry",
				Description: "desc",
				Severity:    "error",
				CreatedAt:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}

	if err := idx.Save(); err != nil {
		t.Fatalf("Save() returned error: %v", err)
	}

	loaded, err := LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex() after Save() returned error: %v", err)
	}
	if loaded.Version != idx.Version {
		t.Fatalf("version mismatch: got %q, want %q", loaded.Version, idx.Version)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(loaded.Entries))
	}
	if loaded.Entries[0].ID != "gotchas-123" {
		t.Fatalf("entry ID mismatch: got %q", loaded.Entries[0].ID)
	}
	if !loaded.Entries[0].CreatedAt.Equal(idx.Entries[0].CreatedAt) {
		t.Fatalf("CreatedAt mismatch: got %v, want %v", loaded.Entries[0].CreatedAt, idx.Entries[0].CreatedAt)
	}
}

func TestAddEntry(t *testing.T) {
	home := setHomeTemp(t)

	idx := &Index{Version: "2.0"}

	entry := Entry{
		Category:    "gotchas",
		Title:       "Do not use X",
		Description: "X is bad",
		Patterns:    []string{`bad\.X`},
		Source:      "test-project",
	}

	before := time.Now()
	if err := idx.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry() returned error: %v", err)
	}
	after := time.Now()

	if len(idx.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(idx.Entries))
	}

	got := idx.Entries[0]

	// ID should have been auto-generated
	if got.ID == "" {
		t.Fatal("ID was not auto-generated")
	}
	if !strings.HasPrefix(got.ID, "gotchas-") {
		t.Fatalf("auto-generated ID should have category prefix, got %q", got.ID)
	}

	// CreatedAt should have been auto-filled
	if got.CreatedAt.Before(before) || got.CreatedAt.After(after) {
		t.Fatalf("CreatedAt %v not in expected range [%v, %v]", got.CreatedAt, before, after)
	}

	// Severity should default to "error"
	if got.Severity != "error" {
		t.Fatalf("expected default severity 'error', got %q", got.Severity)
	}

	// .md file should exist and contain **Patterns:**
	mdPath := filepath.Join(home, ".forge", "knowledge", "gotchas", got.ID+".md")
	data, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("failed to read md file %s: %v", mdPath, err)
	}
	content := string(data)
	if !strings.Contains(content, "**Patterns:**") {
		t.Fatalf("md file does not contain **Patterns:**, content:\n%s", content)
	}
	if !strings.Contains(content, `bad\.X`) {
		t.Fatalf("md file does not contain the pattern, content:\n%s", content)
	}
	if !strings.Contains(content, "# Do not use X") {
		t.Fatalf("md file does not contain title heading, content:\n%s", content)
	}

	// Verify round-trip via LoadIndex
	loaded, err := LoadIndex()
	if err != nil {
		t.Fatalf("LoadIndex() returned error: %v", err)
	}
	if len(loaded.Entries) != 1 {
		t.Fatalf("expected 1 loaded entry, got %d", len(loaded.Entries))
	}
}

func TestAddEntryWithExplicitID(t *testing.T) {
	setHomeTemp(t)

	idx := &Index{Version: "2.0"}

	entry := Entry{
		ID:          "my-custom-id",
		Category:    "patterns",
		Title:       "Custom ID entry",
		Description: "desc",
		CreatedAt:   time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := idx.AddEntry(entry); err != nil {
		t.Fatalf("AddEntry() returned error: %v", err)
	}

	if idx.Entries[0].ID != "my-custom-id" {
		t.Fatalf("explicit ID should not be overwritten, got %q", idx.Entries[0].ID)
	}
}

func TestListEntriesAll(t *testing.T) {
	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{ID: "1", Category: "gotchas", Title: "A"},
			{ID: "2", Category: "patterns", Title: "B"},
			{ID: "3", Category: "apis", Title: "C"},
		},
	}

	result := idx.ListEntries("")
	if len(result) != 3 {
		t.Fatalf("ListEntries('') should return all 3 entries, got %d", len(result))
	}
}

func TestListEntriesByCategory(t *testing.T) {
	idx := &Index{
		Version: "2.0",
		Entries: []Entry{
			{ID: "1", Category: "gotchas", Title: "A"},
			{ID: "2", Category: "patterns", Title: "B"},
			{ID: "3", Category: "gotchas", Title: "C"},
			{ID: "4", Category: "apis", Title: "D"},
		},
	}

	result := idx.ListEntries("gotchas")
	if len(result) != 2 {
		t.Fatalf("ListEntries('gotchas') should return 2 entries, got %d", len(result))
	}
	for _, e := range result {
		if e.Category != "gotchas" {
			t.Fatalf("expected category 'gotchas', got %q", e.Category)
		}
	}

	patterns := idx.ListEntries("patterns")
	if len(patterns) != 1 {
		t.Fatalf("ListEntries('patterns') should return 1 entry, got %d", len(patterns))
	}

	none := idx.ListEntries("nonexistent")
	if len(none) != 0 {
		t.Fatalf("ListEntries('nonexistent') should return 0 entries, got %d", len(none))
	}
}

func TestAddEntryDuplicateCategory(t *testing.T) {
	// AddEntry does not validate against ValidCategories — it accepts any category.
	// This test verifies the current behavior: any category string is accepted.
	setHomeTemp(t)

	idx := &Index{Version: "2.0"}

	entry := Entry{
		ID:          "bad-cat",
		Category:    "invalid-category",
		Title:       "Bad category",
		Description: "desc",
		CreatedAt:   time.Now(),
	}

	err := idx.AddEntry(entry)
	if err != nil {
		t.Fatalf("AddEntry with invalid category returned error: %v", err)
	}
	if idx.Entries[0].Category != "invalid-category" {
		t.Fatalf("expected category 'invalid-category', got %q", idx.Entries[0].Category)
	}

	// Verify the index.json was written correctly by reading it directly
	root := filepath.Join(os.Getenv("HOME"), ".forge", "knowledge")
	data, err := os.ReadFile(filepath.Join(root, "index.json"))
	if err != nil {
		t.Fatalf("failed to read index.json: %v", err)
	}
	var loaded Index
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("failed to unmarshal index.json: %v", err)
	}
	if loaded.Entries[0].Category != "invalid-category" {
		t.Fatalf("persisted category mismatch: got %q", loaded.Entries[0].Category)
	}
}

// TestAddEntry_DedupByID：同 ID 反复 AddEntry 必须替换而非追加。
// 修复 exp-accept1 被加 29 次的 bug——AddEntry 旧实现无脑 append，反复 accept/添加
// 让 index 膨胀重复条目，污染 list 输出并放大 CheckViolations。dedup 后同 ID 只留最新一份。
func TestAddEntry_DedupByID(t *testing.T) {
	setHomeTemp(t)
	idx := &Index{Version: "2.0"}
	e := Entry{ID: "exp-x", Category: "gotchas", Title: "T", Description: "D", Severity: "error"}

	for _, title := range []string{"T", "T-updated", "T-updated-again"} {
		e.Title = title
		if err := idx.AddEntry(e); err != nil {
			t.Fatalf("AddEntry(%s): %v", title, err)
		}
	}

	if len(idx.Entries) != 1 {
		t.Fatalf("len(Entries)=%d want 1（同 ID 应 dedup 替换，不重复 append）", len(idx.Entries))
	}
	if idx.Entries[0].Title != "T-updated-again" {
		t.Errorf("Title=%q want T-updated-again（应保留最新替换）", idx.Entries[0].Title)
	}
}

// TestAddEntry_DedupByTitleAndCategory 钉死 dogfood 1.3：同 (Category, Title) 不同 ID 必须
// 合并而非 append。AutoAcceptHighConfidence 对每低分 task 生成新 proposal（ID=exp-{hex} 不同），
// accept 进 store 旧逻辑仅按 ID dedup 无效 → dimensionTemplates 6 个固定 title 跨 task 无限累积
// （dogfood 实测 147 条/3 唯一）。合并：union Patterns/Source、最早 CreatedAt、保留首条 ID。
func TestAddEntry_DedupByTitleAndCategory(t *testing.T) {
	setHomeTemp(t)
	idx := &Index{Version: "2.0"}

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// 三次 accept 同 title+category，不同 ID（模拟跨 task 的 proposal，ID 各异）
	adds := []Entry{
		{ID: "exp-aaa", Category: "patterns", Title: "新代码配测试", Description: "D1", Patterns: []string{`p1`}, Source: "auto:t1", CreatedAt: base},
		{ID: "exp-bbb", Category: "patterns", Title: "新代码配测试", Description: "D1", Patterns: []string{`p2`}, Source: "auto:t2", CreatedAt: base.Add(time.Hour)},
		{ID: "exp-ccc", Category: "patterns", Title: "新代码配测试", Description: "D1", Patterns: []string{`p1`, `p3`}, Source: "auto:t3", CreatedAt: base.Add(2 * time.Hour)},
	}
	for _, e := range adds {
		if err := idx.AddEntry(e); err != nil {
			t.Fatalf("AddEntry(%s): %v", e.ID, err)
		}
	}

	if len(idx.Entries) != 1 {
		t.Fatalf("len(Entries)=%d want 1（同 title+category 必须合并，非 append 3 份）", len(idx.Entries))
	}
	got := idx.Entries[0]
	if got.ID != "exp-aaa" {
		t.Errorf("ID=%q want exp-aaa（保留首条 ID 作稳定锚点）", got.ID)
	}
	// Patterns union 去重：p1,p2,p3（p1 重复只一份）
	wantPats := map[string]bool{`p1`: true, `p2`: true, `p3`: true}
	if len(got.Patterns) != 3 {
		t.Errorf("Patterns=%v want 3 个去重 union", got.Patterns)
	}
	for _, p := range got.Patterns {
		if !wantPats[p] {
			t.Errorf("unexpected pattern %q", p)
		}
	}
	// Source 记录所有来源（union 去重）
	for _, s := range []string{"auto:t1", "auto:t2", "auto:t3"} {
		if !strings.Contains(got.Source, s) {
			t.Errorf("Source=%q 缺少 %q（应 union 所有来源）", got.Source, s)
		}
	}
	// CreatedAt 取最早
	if !got.CreatedAt.Equal(base) {
		t.Errorf("CreatedAt=%v want 最早 %v", got.CreatedAt, base)
	}
}

// TestAddEntry_DedupeCleansHistory 钉死历史脏数据清理：index 已含同 title+category 多条
// 重复（dogfood 147 条现状），下次 AddEntry 触发 dedupeByTitle 收敛为 1 条。这是修复落
// 地后无需单独 migrate 命令、靠运行时自然清理的关键。
func TestAddEntry_DedupeCleansHistory(t *testing.T) {
	setHomeTemp(t)
	idx := &Index{Version: "2.0", Entries: []Entry{
		{ID: "old1", Category: "gotchas", Title: "T", Description: "D"},
		{ID: "old2", Category: "gotchas", Title: "T", Description: "D"},
		{ID: "old3", Category: "gotchas", Title: "T", Description: "D"},
		{ID: "other", Category: "gotchas", Title: "Other", Description: "D"},
	}}
	if err := idx.AddEntry(Entry{ID: "new", Category: "gotchas", Title: "T", Description: "D"}); err != nil {
		t.Fatal(err)
	}
	tCount := 0
	for _, e := range idx.Entries {
		if e.Title == "T" {
			tCount++
		}
	}
	if tCount != 1 {
		t.Errorf("title T 条数=%d want 1（历史重复应被清理收敛）", tCount)
	}
	if len(idx.Entries) != 2 {
		t.Errorf("总条数=%d want 2（T×1 + Other×1）", len(idx.Entries))
	}
}

// TestAddEntry_DedupeFoldsHistoryData 钉死 F1：dedupeByTitle 收敛历史同 key 重复时，必须把
// 每个被删项的 Patterns/Source/CreatedAt 折叠进保留项，不能直接删除丢弃数据。dogfood 147 条
// 现状下各重复 patterns 相同（无影响），但这是通用路径——用户手工加同 title 不同 patterns 的
// 条目会被静默丢真实数据。审查探针证实现实现只合并首条 + incoming，删其余不合并。
func TestAddEntry_DedupeFoldsHistoryData(t *testing.T) {
	setHomeTemp(t)
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	idx := &Index{Version: "2.0", Entries: []Entry{
		{ID: "h1", Category: "gotchas", Title: "T", Description: "D", Patterns: []string{`only-in-h1`}, Source: "src:h1", CreatedAt: base.Add(2 * time.Hour)},
		{ID: "h2", Category: "gotchas", Title: "T", Description: "D", Patterns: []string{`only-in-h2`}, Source: "src:h2", CreatedAt: base.Add(3 * time.Hour)},
		{ID: "h3", Category: "gotchas", Title: "T", Description: "D", Patterns: []string{`only-in-h3`}, Source: "src:h3", CreatedAt: base.Add(4 * time.Hour)},
	}}
	incoming := Entry{ID: "new", Category: "gotchas", Title: "T", Description: "D", Patterns: []string{`pnew`}, Source: "src:new", CreatedAt: base}
	if err := idx.AddEntry(incoming); err != nil {
		t.Fatal(err)
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("len=%d want 1（4 同 title 应收敛为 1）", len(idx.Entries))
	}
	got := idx.Entries[0]
	wantPats := map[string]bool{`only-in-h1`: true, `only-in-h2`: true, `only-in-h3`: true, `pnew`: true}
	if len(got.Patterns) != 4 {
		t.Errorf("Patterns=%v want 4（h1/h2/h3/new 各 1，全 union 不丢历史数据）", got.Patterns)
	}
	for _, p := range got.Patterns {
		if !wantPats[p] {
			t.Errorf("unexpected pattern %q（历史重复的 pattern 必须折叠进保留项）", p)
		}
	}
	for _, s := range []string{"src:h1", "src:h2", "src:h3", "src:new"} {
		if !strings.Contains(got.Source, s) {
			t.Errorf("Source=%q 缺 %q（历史重复的 source 必须折叠进保留项，不能随删除丢弃）", got.Source, s)
		}
	}
	// CreatedAt 取最早（incoming base 早于 h1/h2/h3）
	if !got.CreatedAt.Equal(base) {
		t.Errorf("CreatedAt=%v want 最早 %v（应折叠进所有条目的最早时间）", got.CreatedAt, base)
	}
}

// TestAddEntry_MergeNoOrphanMD 钉死 F2：合并分支不能泄漏孤立 incoming .md。同 title 多次
// accept（不同 ID），磁盘 patterns/ 下只应有最终保留 ID（首条）的 1 个 .md，非每个 incoming.ID
// 各留一个。审查探针证实旧实现每个 accept 都写 incoming.ID.md，合并后 index 指向 base.ID，
// incoming.ID.md 永不被引用 → dogfood 6 模板 ×24 accept 残留 ~144 孤立 .md。
func TestAddEntry_MergeNoOrphanMD(t *testing.T) {
	home := setHomeTemp(t)
	idx := &Index{Version: "2.0"}
	for i, id := range []string{"exp-aaa", "exp-bbb", "exp-ccc"} {
		if err := idx.AddEntry(Entry{
			ID:          id,
			Category:    "patterns",
			Title:       "新代码配测试",
			Description: "D",
			Patterns:    []string{fmt.Sprintf("p%d", i)},
			Source:      fmt.Sprintf("auto:t%d", i),
			CreatedAt:   time.Date(2026, 7, 1, i, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("AddEntry(%s): %v", id, err)
		}
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("len=%d want 1（同 title+category 合并）", len(idx.Entries))
	}
	keepID := idx.Entries[0].ID
	catDir := filepath.Join(home, ".forge", "knowledge", "patterns")
	entries, err := os.ReadDir(catDir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", catDir, err)
	}
	var mdFiles []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			mdFiles = append(mdFiles, e.Name())
		}
	}
	if len(mdFiles) != 1 {
		t.Fatalf("patterns/ 下 .md=%v want 1 个（只 %s.md，无孤立 incoming .md）", mdFiles, keepID)
	}
	if mdFiles[0] != keepID+".md" {
		t.Errorf("md 文件名=%q want %s.md（应是最终保留 ID，非 incoming ID）", mdFiles[0], keepID)
	}
	// 保留项 .md 内容应是合并后的（含所有 accept 的 pattern），非首条孤立内容
	data, err := os.ReadFile(filepath.Join(catDir, mdFiles[0]))
	if err != nil {
		t.Fatalf("read md: %v", err)
	}
	for _, p := range []string{"p0", "p1", "p2"} {
		if !strings.Contains(string(data), p) {
			t.Errorf("保留 .md 缺 pattern %q（应是合并后内容，含所有 accept 的 pattern）", p)
		}
	}
}
