package evalsuite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/shellexec"
)

// DefaultCaseTimeout 与 change verify 的单条判据超时一致：回归集不该变成
// 「一条挂死的判据让整次基线静默挂起」。
const DefaultCaseTimeout = 10 * time.Minute

// Case 是一条可执行的回归判据：来自某个已归档 change 的 acceptance 围栏。
// 仓库级回归集的意义是「改一处，跑所有历史判据」——它不是新的门禁，只是把
// 已经写在 acceptance 里的证据变成可重复的基线（跨 change 的 held-out）。
type Case struct {
	Change  string `json:"change"`
	Fence   int    `json:"fence"`
	Shell   string `json:"shell"`
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

var fenceRe = regexp.MustCompile("(?s)```(bash|sh|shell)[ \t]*\\r?\\n(.*?)```")

// Load 收集归档 change 的 acceptance 围栏（可选按 change id 过滤，max<=0 表示不限）。
//
// 语义与 change verify 对齐：围栏正文是**一整段脚本**，交给 shell 执行（多行、
// 注释行、重定向都按脚本语义走）。围栏里出现 `# suite: skip` 的整条跳过——
// 重判据（全量测试、长跑脚本）由 change 自己的 verify 负责，回归集只挑能反复跑的。
func Load(root string, only string, max int) ([]Case, error) {
	dirs, err := filepath.Glob(filepath.Join(root, "spec", "changes", "archive", "*", "acceptance.md"))
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	var cases []Case
	var available []string
	for _, path := range dirs {
		id := filepath.Base(filepath.Dir(path))
		// 归档目录名是 20261119-drop-score，对外用去掉日期前缀的 id
		if i := strings.Index(id, "-"); i == 8 {
			id = id[9:]
		}
		if only != "" && id != only {
			available = append(available, id)
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		n := 0
		for _, m := range fenceRe.FindAllStringSubmatch(string(raw), -1) {
			n++
			body := strings.TrimSpace(m[2])
			if body == "" || strings.Contains(body, "# suite: skip") {
				continue
			}
			cases = append(cases, Case{Change: id, Fence: n, Shell: m[1], Command: body})
			if max > 0 && len(cases) >= max {
				return cases, nil
			}
		}
	}
	if only != "" && len(cases) == 0 && len(available) > 0 {
		sort.Strings(available)
		return nil, fmt.Errorf("没有 change %q 的归档判据；可用的有：%s", only, strings.Join(available, "、"))
	}
	return cases, nil
}

// Run 逐条执行判据。每条都在 shell 里跑（与 verify 同语义），单条超时
// DefaultCaseTimeout，失败不中断（回归集要一次看到全部退化点）。
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
	shell, err := shellFor(c.Shell)
	if err != nil {
		res.Error = err.Error()
		res.Exit = -1
		res.DurationMS = time.Since(start).Milliseconds()
		return res
	}
	ctx, cancel := context.WithTimeout(context.Background(), DefaultCaseTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, shell, "-c", c.Command)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	res.DurationMS = time.Since(start).Milliseconds()
	res.Output = capOutput(string(out), 4000)
	if err != nil {
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			res.Exit = ee.ExitCode()
		case errors.Is(err, context.DeadlineExceeded):
			res.Exit = -1
			res.Error = fmt.Sprintf("判据超过 %s 未结束，已终止", DefaultCaseTimeout)
		default:
			res.Exit = -1
			res.Error = err.Error()
		}
		return res
	}
	res.Passed = true
	return res
}

// shellFor 把围栏语言映射到本地 shell：bash 优先用 shellexec 找到的 bash，
// sh/shell 用 sh 退到 bash（Windows 上没有 sh 时先看 bash）。
func shellFor(kind string) (string, error) {
	bash, err := shellexec.FindBash()
	if err == nil && bash != "" {
		return bash, nil
	}
	if kind == "sh" || kind == "shell" {
		if p, err := exec.LookPath("sh"); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("找不到可用的 shell（bash/sh），回归判据无法执行：%v", err)
}

// capOutput 截断到 max 字节但不切开 UTF-8；尾部标注截断。
func capOutput(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…（截断）"
}

// Save 把记录写进 .forge/evals/<ts>.json 并在 index.jsonl 追加一行摘要。
// 同一秒内重复记录会加后缀而不是覆盖（与 verify 的 uniqueEvidenceDir 同理）。
func Save(root string, rec Record) (string, error) {
	dir := filepath.Join(root, ".forge", "evals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ts := rec.At.Format("20060102T150405Z")
	path := filepath.Join(dir, ts+".json")
	for i := 2; exists(path); i++ {
		path = filepath.Join(dir, fmt.Sprintf("%s-%d.json", ts, i))
	}
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

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Trend 读取 index.jsonl 的最近 n 条（n<=0 表示全部）。
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
