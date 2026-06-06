package knowledge

import (
	"encoding/json"
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
