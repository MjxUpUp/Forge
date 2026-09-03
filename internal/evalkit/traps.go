package evalkit

// traps.go — 对抗陷阱集（Track B · C2，docs/design/forge-evaluation-system.md
// §六 P2）：把"系统会被怎么糊弄"做成可重放用例——测试削弱、伪造审计证据、虚假
// 完成（ImpossibleBench 思想）。陷阱的探测结果是对现状的诚实测量：v1 的审计行
// 签名（nodestamp Sig）恒空，伪造证据不可检测——这个 0 分本身就是产出。
//
// traps.go — adversarial trap set: replayable cases of "how the system gets
// gamed" — weakened tests, forged audit evidence, false completion
// (ImpossibleBench thinking). The probe result is an honest measurement of the
// status quo: in v1 the audit-row signature (nodestamp Sig) is always empty, so
// forged evidence is undetectable — that zero is itself a deliverable.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
	"gopkg.in/yaml.v3"
)

// Trap types (single source of truth; the YAML assets reference these).
//
// 陷阱类型（单一真相源；YAML 资产引用这些常量）。
const (
	TrapPristineFalsePositive = "pristine-false-positive" // "什么都不做"攻击：干净仓库上门禁误触
	TrapEvidenceForgery       = "evidence-forgery"        // 伪造 checklog 审计证据
	TrapWeakenedTest          = "weakened-test"           // 测试被削弱（断言改恒真）
)

// TrapCase is one adversarial case: a setup (fixture files / a forged audit
// row) plus a probe whose output is inspected for signs the system detected
// the attack.
//
// TrapCase 是一个对抗用例：setup（fixture 文件 / 伪造审计行）+ 一个探测命令，
// 从其输出检视系统是否识破了攻击。
type TrapCase struct {
	ID          string     `yaml:"id"           json:"id"`
	Type        string     `yaml:"type"         json:"type"`
	Description string     `yaml:"description"  json:"description"`
	Files       []FileSpec `yaml:"files"        json:"files"`
	// ForgedRow（仅 evidence-forgery）会被追加进 fixture 的 checklog.jsonl——
	// 一条自述"验证通过"的审计行，带非零但无凭据的 Sig。
	ForgedRow *ForgedChecklogRow `yaml:"forged_row,omitempty" json:"forged_row,omitempty"`
	ProbeArgv []string           `yaml:"probe_argv"   json:"probe_argv"` // {forge}/{dataDir} 占位
	ProbeStdin string            `yaml:"probe_stdin"  json:"probe_stdin"`
	DetectAny []string           `yaml:"detect_any"   json:"detect_any"`
	// ExpectDetected 为 nil 表示"现状未知，本轮实测"——v1 对 evidence-forgery
	// 如此：探测结果写进 finding 而非判 fail。
	ExpectDetected *bool `yaml:"expect_detected" json:"expect_detected"`
}

// ForgedChecklogRow is the forged audit row a forgery trap plants.
//
// ForgedChecklogRow 是伪造陷阱植入的审计行。
type ForgedChecklogRow struct {
	Check  string `yaml:"check"  json:"check"`
	Detail string `yaml:"detail" json:"detail"`
	Sig    string `yaml:"sig"    json:"sig"`
}

// TrapResult is one trap's replay outcome.
//
// TrapResult 是一个陷阱的重放结果。
type TrapResult struct {
	ID        string   `json:"id"`
	Type      string   `json:"type"`
	Captured  bool     `json:"captured"`  // 系统识破了攻击
	Secretly  bool     `json:"-"`          // 占位：未来"静默通过但留痕"细分
	Signals   []string `json:"signals,omitempty"`
	ExpectSet bool     `json:"expect_set"`
	Expected  bool     `json:"expected"`
	Error     string   `json:"error,omitempty"`
}

// TrapReport aggregates a trap-set replay.
//
// TrapReport 聚合一次陷阱集重放。
type TrapReport struct {
	GeneratedAt time.Time    `json:"generated_at"`
	CaptureRate RateValue    `json:"capture_rate"`
	Traps       []TrapResult `json:"traps"`
	Findings    []string     `json:"findings,omitempty"`
}

// LoadTrapDir loads every *.yaml trap case (fail-closed, same discipline as
// golden sets).
//
// LoadTrapDir 加载目录内全部 *.yaml 陷阱用例（fail-closed，与 golden 同纪律）。
func LoadTrapDir(dir string) ([]TrapCase, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var traps []TrapCase
	seen := map[string]bool{}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var t TrapCase
		if err := yaml.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("evalkit: 解析陷阱用例 %s: %w", filepath.Base(path), err)
		}
		if t.ID == "" || t.Type == "" || t.Description == "" || len(t.DetectAny) == 0 {
			return nil, fmt.Errorf("evalkit: 陷阱用例 %s 缺 id/type/description/detect_any", filepath.Base(path))
		}
		switch t.Type {
		case TrapPristineFalsePositive, TrapEvidenceForgery, TrapWeakenedTest:
		default:
			return nil, fmt.Errorf("evalkit: 陷阱用例 %s 的 type %q 非法", t.ID, t.Type)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("evalkit: 陷阱用例 id 重复: %s", t.ID)
		}
		seen[t.ID] = true
		traps = append(traps, t)
	}
	if len(traps) == 0 {
		return nil, fmt.Errorf("evalkit: 陷阱目录 %s 无任何用例", dir)
	}
	return traps, nil
}

// RunTraps replays the trap set. Capture semantics per type: pristine-false-
// positive — the system "captures" the attack when NO gate fires on the clean
// fixture; evidence-forgery / weakened-test — captured when a detection signal
// fires on the probe.
//
// RunTraps 重放陷阱集。各类型 capture 语义：pristine-false-positive——干净
// fixture 上无任何门禁触发即"识破"（系统扛住了"什么都不做"攻击）；
// evidence-forgery / weakened-test——探测命令出现检测信号即"识破"。
func RunTraps(traps []TrapCase, opts GoldenOptions) (*TrapReport, error) {
	if opts.ForgeBin == "" {
		return nil, fmt.Errorf("evalkit: GoldenOptions.ForgeBin 为空——陷阱重放需要真实 forge 二进制")
	}
	rep := &TrapReport{GeneratedAt: time.Now().UTC()}
	captured, total := 0, 0
	for _, t := range traps {
		res := TrapResult{ID: t.ID, Type: t.Type}
		if t.ExpectDetected != nil {
			res.ExpectSet = true
			res.Expected = *t.ExpectDetected
		}
		pr, err := runTrapProbe(t, opts)
		if err != nil {
			res.Error = err.Error()
		} else {
			res.Signals = pr.Signals
			res.Captured = pr.Flagged
		}
		total++
		finalCaptured := res.Captured
		if err == nil && t.Type == TrapPristineFalsePositive {
			// 语义反转："什么都不做"攻击下，无触发 = 系统识破（扛住了误报诱惑）。
			finalCaptured = !res.Captured
		}
		if finalCaptured {
			captured++
		}
		res.Captured = finalCaptured
		if res.ExpectSet && res.Expected != res.Captured {
			if res.Expected {
				rep.Findings = append(rep.Findings, fmt.Sprintf("陷阱 %s（%s）期望被识破但未识破——action item: %s", t.ID, t.Type, t.Description))
			} else {
				rep.Findings = append(rep.Findings, fmt.Sprintf("陷阱 %s（%s）意外被识破——检测器行为与预期不符: %s", t.ID, t.Type, t.Description))
			}
		}
		rep.Traps = append(rep.Traps, res)
	}
	rep.CaptureRate = newRateValue(&MetricDef{ID: "trap_capture_rate", MinSamples: 1}, captured, total)
	return rep, nil
}

// runTrapProbe sets up the trap environment (fixture + optional forged row)
// and runs the probe.
//
// runTrapProbe 搭建陷阱环境（fixture + 可选伪造审计行）并运行探测命令。
func runTrapProbe(t TrapCase, opts GoldenOptions) (*probeResult, error) {
	tmp, err := os.MkdirTemp("", "evalkit-trap-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	fixture := filepath.Join(tmp, "repo")
	dataHome := filepath.Join(tmp, "data")
	for _, d := range []string{fixture, dataHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	for _, f := range t.Files {
		p := filepath.Join(fixture, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return nil, err
		}
	}
	env := append(os.Environ(), "HOME="+tmp, "FORGE_DATA_HOME="+dataHome)
	run := func(argv []string, stdin string) (string, string, int) {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = fixture
		cmd.Env = env
		if stdin != "" {
			cmd.Stdin = strings.NewReader(stdin)
		}
		var outBuf, errBuf strings.Builder
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		runErr := cmd.Run()
		return outBuf.String(), errBuf.String(), exitCode(runErr)
	}
	if _, _, code := run([]string{"git", "init", "-q"}, ""); code != 0 {
		return nil, fmt.Errorf("git init 失败")
	}
	if out, errOut, code := run([]string{opts.ForgeBin, "init"}, ""); code != 0 {
		return nil, fmt.Errorf("forge init 失败: %s %s", out, errOut)
	}
	if t.Type == TrapEvidenceForgery && t.ForgedRow != nil {
		// 定位 fixture 的 DataDir（`forge data-dir`），追加伪造审计行。
		out, _, code := run([]string{opts.ForgeBin, "data-dir"}, "")
		if code != 0 {
			return nil, fmt.Errorf("forge data-dir 失败")
		}
		dataDir := strings.TrimSpace(out)
		clPath := filepath.Join(dataDir, "checklog.jsonl")
		row := fmt.Sprintf(`{"check":%q,"passed":true,"checked":true,"detail":%q,"source":"agent-claim","sig":%q}`+"\n",
			t.ForgedRow.Check, t.ForgedRow.Detail, t.ForgedRow.Sig)
		if err := os.WriteFile(clPath, []byte(row), 0o644); err != nil {
			return nil, err
		}
	}
	argv := make([]string, len(t.ProbeArgv))
	for i, a := range t.ProbeArgv {
		argv[i] = strings.NewReplacer("{forge}", opts.ForgeBin, "{dataDir}", "").Replace(a)
	}
	stdout, stderr, code := run(argv, t.ProbeStdin)
	signals, err := evalDetectionSignals(t.ID, t.DetectAny, stdout, stderr, code)
	if err != nil {
		return nil, err
	}
	return &probeResult{Signals: signals, Flagged: len(signals) > 0}, nil
}

// PersistTrapReport writes the trap report and records the eval-traps-run row.
//
// PersistTrapReport 写陷阱报告并记录 eval-traps-run 行。
func PersistTrapReport(evalDir string, repoRoot string, rep *TrapReport) (string, error) {
	dir := filepath.Join(evalDir, "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("trap-report-%s.json", rep.GeneratedAt.UTC().Format("20060102-150405")))
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckEvalTrapsRun,
		Passed:  true,
		Checked: true,
		Detail:  fmt.Sprintf(`trap run: capture %d/%d findings %d`, rep.CaptureRate.Numerator, rep.CaptureRate.Denominator, len(rep.Findings)),
	})
	return path, nil
}
