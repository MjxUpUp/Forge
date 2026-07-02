package knowledge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func getGlobalKnowledgeRoot() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".forge", "knowledge")
}

// LoadIndex reads ~/.forge/knowledge/index.json.
func LoadIndex() (*Index, error) {
	root := getGlobalKnowledgeRoot()
	path := filepath.Join(root, "index.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Index{Version: "2.0"}, nil
		}
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// Save writes the index back to ~/.forge/knowledge/index.json.
func (idx *Index) Save() error {
	root := getGlobalKnowledgeRoot()
	if err := os.MkdirAll(root, 0755); err != nil {
		return fmt.Errorf("failed to create knowledge dir: %w", err)
	}
	for _, cat := range []string{"gotchas", "patterns", "apis"} {
		os.MkdirAll(filepath.Join(root, cat), 0755)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "index.json"), data, 0644)
}

// AddEntry adds a knowledge entry and writes the .md file.
func (idx *Index) AddEntry(entry Entry) error {
	if entry.ID == "" {
		entry.ID = fmt.Sprintf("%s-%d", entry.Category, time.Now().Unix())
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	if entry.Severity == "" {
		entry.Severity = "error"
	}

	root := getGlobalKnowledgeRoot()
	catDir := filepath.Join(root, entry.Category)
	os.MkdirAll(catDir, 0755)

	mdPath := filepath.Join(catDir, entry.ID+".md")
	mdContent := fmt.Sprintf("# %s\n\n**Category:** %s\n**Severity:** %s\n**Source:** %s\n**Patterns:** %s\n\n%s\n",
		entry.Title, entry.Category, entry.Severity, entry.Source,
		strings.Join(entry.Patterns, ", "), entry.Description)
	if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
		return fmt.Errorf("failed to write knowledge file: %w", err)
	}

	// dedup by ID：同 ID 已存在则原地替换，不重复 append。修复反复 accept/添加
	// 让 index 膨胀重复条目——实测 exp-accept1 被加 29 次，导致 forge knowledge list
	// 全是重复行、CheckViolations 输出放大 29 倍（35380 条违规里绝大多数是同一条重复）。
	// 同 ID 替换（更新）而非追加。
	replaced := false
	for i := range idx.Entries {
		if idx.Entries[i].ID == entry.ID {
			idx.Entries[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Entries = append(idx.Entries, entry)
	}
	return idx.Save()
}

// ListEntries returns entries filtered by category.
func (idx *Index) ListEntries(category string) []Entry {
	if category == "" {
		return idx.Entries
	}
	var filtered []Entry
	for _, e := range idx.Entries {
		if e.Category == category {
			filtered = append(filtered, e)
		}
	}
	return filtered
}
