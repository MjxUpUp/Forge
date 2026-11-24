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

// HistoricalMarker 是一份文档「只作历史记录」的状态行。
// 带它的文档不参与漂移判定：里面出现的已删命令是历史记载，不是教用户怎么做。
const HistoricalMarker = "状态：历史"

// liveDocFiles 是逐份检查的活文档（用户最先读到的那几份）。
var liveDocFiles = []string{"README.md", "AGENTS.md", "npm/README.md", "homepage/index.md"}

// liveDocDirs 是整目录参与的活文档范围。
// 2026-11 的审查发现：只收 docs/reference 与模板时，READMEs/、npm、homepage、
// docs/design、docs/compliance 长期积累已删命令而 docs closure 一直报 0。
var liveDocDirs = []string{
	filepath.Join("docs", "reference"),
	filepath.Join("docs", "design"),
	filepath.Join("docs", "compliance"),
	filepath.Join("docs", "migration"),
	"READMEs",
	filepath.Join("internal", "change", "templates"),
}

// IsHistoricalDoc 判断文档首屏是否声明了自己是历史记录。
func IsHistoricalDoc(path string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	return strings.Contains(string(head), HistoricalMarker)
}

func LiveDocs(root string) ([]string, error) {
	var files []string
	for _, f := range liveDocFiles {
		if st, err := os.Stat(filepath.Join(root, f)); err == nil && !st.IsDir() {
			files = append(files, f)
		}
	}
	for _, dir := range liveDocDirs {
		walkErr := filepath.WalkDir(filepath.Join(root, dir), func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
				return nil
			}
			if IsHistoricalDoc(p) {
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
