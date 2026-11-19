package evalsuite

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Case 是一条可执行的回归判据：来自某个已归档 change 的 acceptance 围栏。
// 仓库级回归集的意义是「改一处，跑所有历史判据」——它不是新的门禁，只是把
// 已经写在 acceptance 里的证据变成可重复的基线（跨 change 的 held-out）。
type Case struct {
	Change  string `json:"change"`
	Fence   int    `json:"fence"`
	Command string `json:"command"`
}

// Result 是一次执行的结果。
type Result struct {
	Case
	Exit       int    `json:"exit"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output,omitempty"`
	Passed     bool   `json:"passed"`
	Error      string `json:"error,omitempty"`
}

// Record 是一次 suite 运行的落盘记录。
type Record struct {
	At      time.Time `json:"at"`
	Head    string    `json:"head,omitempty"`
	Total   int       `json:"total"`
	Passed  int       `json:"passed"`
	Results []Result  `json:"results"`
}

var fenceRe = regexp.MustCompile("(?s)```bash\\s*\\n(.*?)```")

// Load 收集归档 change 的 acceptance 围栏（可选按 change id 过滤，max<=0 表示不限）。
// 围栏里出现 `# suite: skip` 的整条跳过——重判据（全量测试、长跑脚本）由 change
// 自己的 verify 负责，回归集只挑能反复跑的短判据。
func Load(root string, only string, max int) ([]Case, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "spec", "changes", "archive", "*", "acceptance.md"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	var cases []Case
	for _, path := range dirs {
		id := filepath.Base(filepath.Dir(path))
		// 归档目录名是 20261119-drop-score，对外用去掉日期前缀的 id
		if i := strings.Index(id, "-"); i == 8 {
			id = id[9:]
		}
		if only != "" && id != only {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		for i, m := range fenceRe.FindAllStringSubmatch(string(raw), -1) {
			body := strings.TrimSpace(m[1])
			if body == "" || strings.Contains(body, "# suite: skip") {
				continue
			}
			cases = append(cases, Case{Change: id, Fence: i + 1, Command: body})
			if max > 0 && len(cases) >= max {
				return cases, nil
			}
		}
	}
	return cases, nil
}

// Run 逐条执行判据。命令用与 acceptance 相同的语义（shellquote 切分后直接 exec），
// 单条超时 10 分钟，失败不中断（回归集要一次看到全部退化点）。
func Run(root string, cases []Case, head string) Record {
	rec := Record{At: time.Now().UTC(), Head: head, Total: len(cases)}
	for _, c := range cases {
		res := runOne(root, c)
		rec.Results = append(rec.Results, res)
		if res.Passed {
			rec.Passed++
		}
	}
	return rec
}

func runOne(root string, c Case) Result {
	start := time.Now()
	res := Result{Case: c}
	args, err := splitCommand(c.Command)
	if err != nil {
		res.Error = err.Error()
		res.DurationMS = time.Since(start).Milliseconds()
		return res
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	res.DurationMS = time.Since(start).Milliseconds()
	res.Output = strings.TrimSpace(string(out))
	if len(res.Output) > 2000 {
		res.Output = res.Output[:2000] + "…（截断）"
	}
	if err != nil {
		var ee *exec.ExitError
		if ok := asExitError(err, &ee); ok {
			res.Exit = ee.ExitCode()
		} else {
			res.Exit = -1
			res.Error = err.Error()
		}
		return res
	}
	res.Passed = true
	return res
}

func asExitError(err error, target **exec.ExitError) bool {
	if ee, ok := err.(*exec.ExitError); ok {
		*target = ee
		return true
	}
	return false
}

// Save 把记录写进 .forge/evals/<ts>.json 并在 index.jsonl 追加一行摘要。
func Save(root string, rec Record) (string, error) {
	dir := filepath.Join(root, ".forge", "evals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ts := rec.At.Format("20060102T150405Z")
	path := filepath.Join(dir, ts+".json")
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return "", err
	}
	line, _ := json.Marshal(map[string]any{
		"at": rec.At.Format(time.RFC3339), "head": rec.Head,
		"total": rec.Total, "passed": rec.Passed, "file": filepath.Base(path),
	})
	f, err := os.OpenFile(filepath.Join(dir, "index.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, string(line)); err != nil {
		return "", err
	}
	return path, nil
}

// Trend 读取 index.jsonl 的最近 n 条。
func Trend(root string, n int) ([]map[string]any, error) {
	path := filepath.Join(root, ".forge", "evals", "index.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var rows []map[string]any
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			continue
		}
		rows = append(rows, m)
	}
	if n > 0 && len(rows) > n {
		rows = rows[len(rows)-n:]
	}
	return rows, nil
}

// splitCommand 用与 forge acceptance 相同的切分语义：引号内空格不切分，无 shell 展开。
func splitCommand(s string) ([]string, error) {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("命令里的引号没有闭合：%s", s)
	}
	flush()
	if len(out) == 0 {
		return nil, fmt.Errorf("空命令")
	}
	return out, nil
}
