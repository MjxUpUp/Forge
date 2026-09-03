package evalkit

// resume.go — 接续演练（Track B · C3，docs/design/forge-evaluation-system.md
// §六 P2）：脚本化的"断点续做"演练——在隔离环境跑真实 forge 命令序列，断言任务
// 状态跨会话可见、审计时间线可回放。演练数字只做回归对比，绝对值不外宣
// （指标字典的误用注记承载）。
//
// resume.go — continuity drills: scripted "resume from breakpoint" exercises —
// real forge command sequences in isolated environments, asserting task state
// stays visible across sessions and audit timelines replay. Drill numbers are
// for regression comparison only; absolute values are never quoted externally
// (the metric dictionary's misuse note carries this).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"gopkg.in/yaml.v3"
)

// DrillStep is one command of a drill with its stdout expectations.
//
// DrillStep 是演练的一条命令及其 stdout 期望。
type DrillStep struct {
	Argv          []string `yaml:"argv"           json:"argv"` // {forge} 占位
	ExpectContains []string `yaml:"expect_contains" json:"expect_contains"`
}

// ResumeDrill is one scripted continuity exercise.
//
// ResumeDrill 是一个脚本化接续演练。
type ResumeDrill struct {
	ID          string      `yaml:"id"          json:"id"`
	Description string      `yaml:"description" json:"description"`
	Files       []FileSpec  `yaml:"files"       json:"files"`
	Steps       []DrillStep `yaml:"steps"       json:"steps"`
}

// DrillResult is one drill's outcome.
//
// DrillResult 是一个演练的结果。
type DrillResult struct {
	ID      string   `json:"id"`
	Passed  bool     `json:"passed"`
	FailedAt string  `json:"failed_at,omitempty"`
	Expect  []string `json:"expect,omitempty"`
	Got     string   `json:"got,omitempty"`
	Error   string   `json:"error,omitempty"`
}

// LoadResumeDrills loads drill YAML files (fail-closed).
//
// LoadResumeDrills 加载演练 YAML（fail-closed）。
func LoadResumeDrills(dir string) ([]ResumeDrill, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var drills []ResumeDrill
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var d ResumeDrill
		if err := yaml.Unmarshal(data, &d); err != nil {
			return nil, fmt.Errorf("evalkit: 解析演练 %s: %w", filepath.Base(path), err)
		}
		if d.ID == "" || len(d.Steps) == 0 {
			return nil, fmt.Errorf("evalkit: 演练 %s 缺 id/steps", filepath.Base(path))
		}
		drills = append(drills, d)
	}
	if len(drills) == 0 {
		return nil, fmt.Errorf("evalkit: 演练目录 %s 无演练", dir)
	}
	return drills, nil
}

// RunResumeDrills replays the drills in isolated environments.
//
// RunResumeDrills 在隔离环境重放演练。
func RunResumeDrills(drills []ResumeDrill, forgeBin string) ([]DrillResult, error) {
	if forgeBin == "" {
		return nil, fmt.Errorf("evalkit: 接续演练需要真实 forge 二进制")
	}
	var results []DrillResult
	for _, d := range drills {
		res := DrillResult{ID: d.ID}
		tmp, err := os.MkdirTemp("", "evalkit-resume-")
		if err != nil {
			results = append(results, DrillResult{ID: d.ID, Error: err.Error()})
			continue
		}
		func() {
			defer os.RemoveAll(tmp)
			fixture := filepath.Join(tmp, "repo")
			dataHome := filepath.Join(tmp, "data")
			for _, dir := range []string{fixture, dataHome} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					res.Error = err.Error()
					return
				}
			}
			for _, f := range d.Files {
				p := filepath.Join(fixture, filepath.FromSlash(f.Path))
				if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
					res.Error = err.Error()
					return
				}
				if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
					res.Error = err.Error()
					return
				}
			}
			env := append(os.Environ(), "HOME="+tmp, "FORGE_DATA_HOME="+dataHome)
			run := func(argv []string) (string, int) {
				cmd := exec.Command(argv[0], argv[1:]...)
				cmd.Dir = fixture
				cmd.Env = env
				var out strings.Builder
				cmd.Stdout = &out
				cmd.Stderr = &out
				err := cmd.Run()
				return out.String(), exitCode(err)
			}
			if out, code := run([]string{"git", "init", "-q"}); code != 0 {
				res.Error = "git init: " + out
				return
			}
			if out, code := run([]string{forgeBin, "init"}); code != 0 {
				res.Error = "forge init: " + out
				return
			}
			for i, step := range d.Steps {
				argv := make([]string, len(step.Argv))
				for j, a := range step.Argv {
					argv[j] = strings.ReplaceAll(a, "{forge}", forgeBin)
				}
				out, code := run(argv)
				if code != 0 {
					res.Passed = false
					res.FailedAt = fmt.Sprintf("step %d exit %d", i+1, code)
					res.Got = truncate(out, 400)
					return
				}
				for _, want := range step.ExpectContains {
					if !strings.Contains(out, want) {
						res.Passed = false
						res.FailedAt = fmt.Sprintf("step %d", i+1)
						res.Expect = append(res.Expect, want)
						res.Got = truncate(out, 400)
						return
					}
				}
			}
			res.Passed = true
		}()
		results = append(results, res)
	}
	return results, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ResumeFidelity computes the pass ratio over drill results (regression
// comparison only — see misuse note).
//
// ResumeFidelity 计算演练通过率（仅回归对比——见误用注记）。
func ResumeFidelity(results []DrillResult) (RateValue, error) {
	pass := 0
	for _, r := range results {
		if r.Passed {
			pass++
		}
	}
	return newRateValue(&MetricDef{ID: "resume_fidelity", MinSamples: 1}, pass, len(results)), nil
}

// PersistResumeReport writes the drill report and the audit row.
//
// PersistResumeReport 写演练报告与审计行。
func PersistResumeReport(evalDir string, repoRoot string, results []DrillResult, fidelity RateValue) (string, error) {
	dir := evalDataDir(evalDir)
	payload := map[string]any{"generated_at": time.Now().UTC(), "fidelity": fidelity, "drills": results}
	data, err := jsonMarshal(payload)
	if err != nil {
		return "", err
	}
	path := filepathJoin(dir, fmt.Sprintf("resume-drill-%s.json", time.Now().UTC().Format("20060102-150405")))
	if err := atomicWriteFile(path, data); err != nil {
		return "", err
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckEvalResumeDrill,
		Passed:  fidelity.Insufficient || fidelity.Value >= 1,
		Checked: true,
		Detail:  fmt.Sprintf(`resume drills: %d/%d passed`, fidelity.Numerator, fidelity.Denominator),
	})
	return path, nil
}
