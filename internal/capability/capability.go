package capability

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/agentproto"
)

type Drift struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Path string `json:"path"`
	Text string `json:"text"`
}

type Table struct {
	Commands  int     `json:"commands"`
	Flags     int     `json:"flags"`
	Annotated int     `json:"annotated"`
	Drifts    []Drift `json:"drifts,omitempty"`
}

var deletedMarkers = []string{"已删除", "已移除", "deprecated", "removed in"}

var cmdToken = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func Surface(cmds []agentproto.Command, annotations map[string]string) Table {
	t := Table{Commands: len(cmds), Annotated: len(annotations)}
	for _, c := range cmds {
		t.Flags += len(c.Flags)
	}
	return t
}

func paths(cmds []agentproto.Command) map[string]bool {
	out := make(map[string]bool, len(cmds)*2)
	for _, c := range cmds {
		out[c.Path] = true
		// 前缀也算已知：文档写 `forge task` 这种父命令是合法引用，
		// 子命令名写错不在这里判（与 internal/agentproto.Check 同一口径）。
		parts := strings.Split(c.Path, " ")
		for i := 1; i < len(parts); i++ {
			out[strings.Join(parts[:i], " ")] = true
		}
	}
	return out
}

func ScanText(file, src string, cmds []agentproto.Command) []Drift {
	known := paths(cmds)
	var out []Drift
	for i, line := range strings.Split(src, "\n") {
		if hasDeletedMarker(line) {
			continue
		}
		rest := line
		for {
			idx := strings.Index(rest, "forge ")
			if idx < 0 {
				break
			}
			rest = rest[idx+len("forge "):]
			tok := firstToken(rest)
			if tok == "" || !cmdToken.MatchString(tok) {
				continue
			}
			// 「只清 forge state，不动代码」这种是散文里的 forge 字样，
			// 命令名后面直接接中文（或另一个字母）时不算命令引用——
			// 行级启发式只能做到这一步，宁可漏报也不制造假漂移。
			if after := rest[strings.Index(rest, tok)+len(tok):]; after != "" {
				r, _ := utf8.DecodeRuneInString(after)
				if r > 127 {
					continue
				}
			}
			if !known[tok] {
				out = append(out, Drift{File: file, Line: i + 1, Path: tok, Text: strings.TrimSpace(line)})
			}
		}
	}
	return out
}

func hasDeletedMarker(line string) bool {
	for _, m := range deletedMarkers {
		if strings.Contains(line, m) {
			return true
		}
	}
	return false
}

func firstToken(s string) string {
	s = strings.TrimLeft(s, " (`")
	if s == "" {
		return ""
	}
	i := 0
	for i < len(s) {
		ch := s[i]
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			i++
			continue
		}
		break
	}
	return s[:i]
}

func ScanFiles(root string, files []string, cmds []agentproto.Command) ([]Drift, error) {
	var out []Drift
	for _, f := range files {
		body, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			return out, err
		}
		out = append(out, ScanText(filepath.ToSlash(f), string(body), cmds)...)
	}
	return out, nil
}

func LiveDocs(root string) ([]string, error) {
	var files []string
	for _, f := range []string{"README.md", "AGENTS.md"} {
		if st, err := os.Stat(filepath.Join(root, f)); err == nil && !st.IsDir() {
			files = append(files, f)
		}
	}
	for _, dir := range []string{filepath.Join("docs", "reference"), filepath.Join("internal", "change", "templates")} {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			rel, rerr := filepath.Rel(root, p)
			if rerr != nil {
				return nil
			}
			files = append(files, filepath.ToSlash(rel))
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}
	sort.Strings(files)
	return files, nil
}
