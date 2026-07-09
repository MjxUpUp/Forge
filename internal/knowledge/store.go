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
	idx.coalesce() // F4：内存自愈，让 list/search/match 读到收敛后的 index（147→3）
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

	// 经验的真身是 (Category, Title)，不是 proposal 临时 ID。dimensionTemplates 只有 6 个固定
	// title，AutoAcceptHighConfidence 对每低分 task 生成新 proposal（ID=exp-{hex} 纳秒戳每次
	// 不同）→ accept 进 store 旧逻辑仅按 ID 去重，同 title+category 无限 append（dogfood 实测
	// 147 条/3 唯一）。按 (Category, Title) 合并：折叠 index 中所有历史同 key 重复进保留项
	// （union Patterns/Source、最早 CreatedAt、保留首条 ID），让 index 收敛。同 ID 精确更新仍
	// 留作 fallback（无 title 匹配但 ID 撞了，替换而非 append）。
	var finalEntry Entry
	if existing := idx.findEntry(entry.Category, entry.Title); existing >= 0 {
		// F1：先折叠 index 中所有历史同 key 重复进 base 槽位（不丢 Patterns/Source），再合并
		// incoming。dedupeByTitle 保序、仅删 keep 之后的同 key 重复，故 existing 索引删除后仍有效。
		idx.Entries = dedupeByTitle(idx.Entries, entry.Category, entry.Title, existing)
		finalEntry = mergeEntry(idx.Entries[existing], entry)
		idx.Entries[existing] = finalEntry
	} else {
		finalEntry = entry
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

	// F2：.md 用最终保留 ID 写。合并分支覆盖 base.ID.md 为合并后内容（不写 incoming.ID.md），
	// 杜绝孤立 .md——旧实现合并决策前无条件写 incoming.ID.md，合并后 index 指向 base.ID，
	// incoming.ID.md 永不被引用不被清理，dogfood 6 模板 ×24 accept 残留 ~144 孤立 .md。
	if err := writeEntryMD(catDir, finalEntry); err != nil {
		return err
	}
	return idx.Save()
}

// writeEntryMD 写 <catDir>/<entry.ID>.md。format 用 raw string 规避 Windows 输入引号腐蚀。
func writeEntryMD(catDir string, entry Entry) error {
	mdPath := filepath.Join(catDir, entry.ID+`.md`)
	mdContent := fmt.Sprintf(`# %s

**Category:** %s
**Severity:** %s
**Source:** %s
**Patterns:** %s

%s
`, entry.Title, entry.Category, entry.Severity, entry.Source,
		strings.Join(entry.Patterns, ", "), entry.Description)
	if err := os.WriteFile(mdPath, []byte(mdContent), 0644); err != nil {
		return fmt.Errorf("failed to write knowledge file: %w", err)
	}
	return nil
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

// dedupeByTitle 收敛 entries 中所有 (category, title) 匹配的重复为 1 条：把每个非 keep 的
// 重复 mergeEntry 进 keep（折叠 Patterns/Source/CreatedAt，F1：不丢历史数据），再删除它们。
// dogfood 147→3 的收敛靠此——旧实现直接删历史重复不合并，通用路径下用户手工加同 title 不同
// patterns 的条目会被静默丢真实数据。keep 是首个匹配 index（findEntry 返回）；保序——keep 之前
// 均为非同 key，删除只发生在 keep 之后，故循环内 out[keep] 始终是有效合并锚点。返回新切片。
func dedupeByTitle(entries []Entry, category, title string, keep int) []Entry {
	if keep < 0 || keep >= len(entries) {
		return entries
	}
	out := make([]Entry, 0, len(entries))
	for i, e := range entries {
		if i == keep {
			out = append(out, entries[keep])
			continue
		}
		if e.Category == category && e.Title == title {
			// i==keep 时已 append 使 out[keep] 有效（前 keep 个均非同 key 已 append，len=keep，
			// append entries[keep] 后 out[keep] 落位）；i>keep 的同 key 项折叠进 out[keep]。
			out[keep] = mergeEntry(out[keep], e)
			continue
		}
		out = append(out, e)
	}
	return out
}

// coalesce 内存自愈：按 (Category, Title) 合并所有重复（F4）。dogfood 147 条脏数据若只靠下次
// AddEntry 同 title 触发清理，长尾 title（不再被 accept）永远停留脏状态。LoadIndex 调用让任何
// 读（list/search/match）立即看到收敛结果（147→3）。纯内存不写盘——磁盘 index.json 的收敛靠
// 下次 AddEntry 同 title 时 Save 落盘；磁盘存量孤立 .md 不自动清理（F2 仅阻止产生新孤立，存量
// 残留对用户不可见：list 读 coalesce 后视图，故无需补 os.Remove 清理逻辑）。
func (idx *Index) coalesce() {
	if len(idx.Entries) <= 1 {
		return
	}
	type key struct{ cat, title string }
	firstAt := make(map[key]int)
	out := make([]Entry, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		k := key{cat: e.Category, title: e.Title}
		if pos, ok := firstAt[k]; ok {
			out[pos] = mergeEntry(out[pos], e)
		} else {
			firstAt[k] = len(out)
			out = append(out, e)
		}
	}
	idx.Entries = out
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
