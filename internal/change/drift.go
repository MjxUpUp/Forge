package change

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DriftResult is the advisory comparison between what a change's change.md says it
// touches and what the repository actually touched since the change was opened.
//
// DriftResult 是 change.md 声明的改动面与仓库实际改动面的对照结果。它只用于披露：
// 声明与实际的偏差本身不是错误——中途发现漏改/多改是正常的；错的是偏差没人说。
type DriftResult struct {
	ChangeID   string   `json:"change_id"`
	BaseCommit string   `json:"base_commit,omitempty"`
	Declared   []string `json:"declared"`
	Changed    []string `json:"changed"`
	Undeclared []string `json:"undeclared"` // 改了但 change.md 没提
	Untouched  []string `json:"untouched"`  // change.md 提了但没改
}

// Drift computes the advisory drift for one change. Uncommitted changes count as
// changed too: 冲突最常出现在「说好只动 A，实际顺手动了 B」，而那一步往往还没提交。
func Drift(root, id string) (*DriftResult, error) {
	c, _, err := Load(root, id)
	if err != nil {
		return nil, err
	}
	res := &DriftResult{ChangeID: c.ID}
	md := filepath.Join(c.Dir, FileChange)
	raw, err := os.ReadFile(md)
	if err == nil {
		res.Declared = DeclaredPaths(string(raw))
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read %s: %w", md, err)
	}

	base, _ := firstCommitFor(root, "spec/changes/"+c.ID+"/goal.md")
	res.BaseCommit = base
	changed, err := changedSince(root, base)
	if err != nil {
		return nil, err
	}
	res.Changed = changed

	declared := map[string]bool{}
	for _, p := range res.Declared {
		declared[p] = true
	}
	changedSet := map[string]bool{}
	for _, p := range res.Changed {
		changedSet[p] = true
		if !declared[p] {
			res.Undeclared = append(res.Undeclared, p)
		}
	}
	for _, p := range res.Declared {
		if !changedSet[p] {
			res.Untouched = append(res.Untouched, p)
		}
	}
	sort.Strings(res.Undeclared)
	sort.Strings(res.Untouched)
	return res, nil
}

// DeclaredPaths extracts backticked path-looking tokens from the change.md 改动 section.
// 只认带 / 或 . 的 token：`forge change drift` 这类命令名不该被当文件路径。
func DeclaredPaths(md string) []string {
	var out []string
	inSection := false
	for _, line := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## ") {
			inSection = strings.HasPrefix(trimmed, "## 改动")
			continue
		}
		if !inSection {
			continue
		}
		for _, seg := range strings.Split(line, "`") {
			tok := strings.TrimSpace(seg)
			if tok == "" || strings.ContainsAny(tok, " \t（）()，,。") {
				continue
			}
			if !strings.ContainsAny(tok, "/.") {
				continue
			}
			// 列表序号（"1."）不是路径：去掉数字与点后为空就跳过。
			if strings.Trim(tok, "0123456789.") == "" {
				continue
			}
			if strings.HasPrefix(tok, "http") || strings.Contains(tok, "*") {
				continue
			}
			out = append(out, filepath.ToSlash(tok))
		}
	}
	return dedupe(out)
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func firstCommitFor(root, path string) (string, error) {
	out, err := exec.Command("git", "-C", root, "log", "--diff-filter=A", "--format=%H", "-1", "--", path).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func changedSince(root, base string) ([]string, error) {
	seen := map[string]bool{}
	add := func(lines []string) {
		for _, p := range lines {
			p = strings.TrimSpace(p)
			if p == "" || strings.HasPrefix(p, "spec/changes/") {
				continue
			}
			seen[filepath.ToSlash(p)] = true
		}
	}
	if base != "" {
		if out, err := exec.Command("git", "-C", root, "diff", "--name-only", base, "HEAD").Output(); err == nil {
			add(strings.Split(string(out), "\n"))
		}
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		rest := line[3:]
		if i := strings.Index(rest, " -> "); i >= 0 {
			rest = rest[i+4:]
		}
		add([]string{rest})
	}
	var list []string
	for p := range seen {
		list = append(list, p)
	}
	sort.Strings(list)
	return list, nil
}
