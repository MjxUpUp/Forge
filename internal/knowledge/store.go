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

	// dedup：经验的真身是 (Category, Title)，不是 proposal 的临时 ID。dimensionTemplates
	// 只有 6 个固定 title，AutoAcceptHighConfidence 对每个低分 task 生成新 proposal（ID 是
	// exp-{hex} 纳秒戳，每次不同）→ accept 进 store 时 ID 各异 → 旧逻辑仅按 ID 去重，同
	// title+category 无限 append。dogfood 实测 147 条/3 唯一（"新代码配测试"×93 等）。
	// 按 (Category, Title) 合并：union Patterns/Source、最早 CreatedAt、保留已存在 ID；
	// 同时清理任何已存在的同 key 重复，让 index 收敛（下次 AddEntry 即清理历史脏数据）。
	// 同 ID（精确更新）仍保留作 fallback。
	if existing := idx.findEntry(entry.Category, entry.Title); existing >= 0 {
		idx.Entries[existing] = mergeEntry(idx.Entries[existing], entry)
		idx.Entries = dedupeByTitle(idx.Entries, entry.Category, entry.Title, existing)
	} else {
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
	}
	return idx.Save()
}

// findEntry 返回 (Category, Title) 匹配的首条 index，无则 -1。
func (idx *Index) findEntry(category, title string) int {
	for i := range idx.Entries {
		if idx.Entries[i].Category == category && idx.Entries[i].Title == title {
			return i
		}
	}
	return -1
}

// mergeEntry 合并同 title+category 的两条：保留 base 的 ID（稳定锚点），union Patterns，
// Source 记录所有来源（去重保序），取最早 CreatedAt。Description 取 base（模板内容稳定）。
func mergeEntry(base, incoming Entry) Entry {
	merged := base
	seenPat := make(map[string]bool)
	for _, p := range base.Patterns {
		seenPat[p] = true
	}
	for _, p := range incoming.Patterns {
		if !seenPat[p] {
			merged.Patterns = append(merged.Patterns, p)
			seenPat[p] = true
		}
	}
	merged.Source = unionSource(base.Source, incoming.Source)
	if incoming.CreatedAt.Before(base.CreatedAt) {
		merged.CreatedAt = incoming.CreatedAt
	}
	return merged
}

// sourceSep 分隔 Source 多来源。raw string 规避 Windows 输入引号腐蚀。
const sourceSep = `; `

// unionSource 合并两个 Source 字符串（各自可能含多个 sourceSep 分隔来源），去重保序。
func unionSource(a, b string) string {
	seen := make(map[string]bool)
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for _, p := range strings.Split(a, sourceSep) {
		add(p)
	}
	for _, p := range strings.Split(b, sourceSep) {
		add(p)
	}
	return strings.Join(out, sourceSep)
}

// dedupeByTitle 删除 entries 中除 keep 外所有 (category, title) 匹配的重复条目，
// 清理历史累积的脏数据（dogfood 147→3 的收敛靠此）。返回新切片。
func dedupeByTitle(entries []Entry, category, title string, keep int) []Entry {
	var out []Entry
	for i, e := range entries {
		if e.Category == category && e.Title == title && i != keep {
			continue
		}
		out = append(out, e)
	}
	return out
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
