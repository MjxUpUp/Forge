package evalkit

// golden.go — 门禁 golden 标注集（Track B · C1/C2，docs/design/
// forge-evaluation-system.md §六 P1/P2）：在隔离 fixture 仓库里经真实 forge 二进制
// 重放标注用例，量出门禁的 precision/recall 与判定确定性。用例期望与样本指纹
// fail-closed；确定性门禁重放一致率 <1.0 直接记 bug finding。
//
// golden.go — gate golden-set framework: replays labeled cases against the real
// forge binary in isolated fixture repos, measuring gate precision/recall and
// verdict determinism. Case expectations and the set fingerprint are fail-closed;
// a deterministic gate replaying below 1.0 agreement is recorded as a bug finding.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
	"gopkg.in/yaml.v3"
)

// GoldenCaseKind labels whether a case models defective or pristine input.
//
// GoldenCaseKind 标注用例建模的是缺陷输入还是干净输入。
const (
	GoldenDefective = "defective" // 缺陷样本——期望门禁拦截
	GoldenClean     = "clean"     // 干净样本——期望门禁放过（fpr 基线）
)

// GoldenCase is one labeled gate case: fixture files + a probe command run
// against the real forge binary + detection signals marking "the gate fired".
//
// GoldenCase 是一个标注门禁用例：fixture 文件 + 对真实 forge 二进制的探测命令 +
// 标记"门禁触发"的检测信号。
type GoldenCase struct {
	ID           string        `yaml:"id"            json:"id"`
	Gate         string        `yaml:"gate"          json:"gate"`
	Kind         string        `yaml:"kind"          json:"kind"`   // defective | clean
	Expect       string        `yaml:"expect"        json:"expect"` // flagged | clean
	Description  string        `yaml:"description"   json:"description"`
	Files        []FileSpec    `yaml:"files"         json:"files"`
	ProbeArgv    []string      `yaml:"probe_argv"    json:"probe_argv"`    // {forge} 占位
	ProbeStdin   string        `yaml:"probe_stdin"   json:"probe_stdin"`
	DetectAny    []string      `yaml:"detect_any"    json:"detect_any"`    // exit_nonzero | stdout:PREFIX | stdout_contains:S
	Deterministic bool         `yaml:"deterministic" json:"deterministic"` // true → 重放一致率必须 1.0
}

// FileSpec is one fixture file laid into the case's temp repo.
//
// FileSpec 是铺进用例临时仓库的一个 fixture 文件。
type FileSpec struct {
	Path    string `yaml:"path"    json:"path"`
	Content string `yaml:"content" json:"content"`
}

// LoadGoldenDir loads every *.yaml case in dir (fail-closed: malformed or
// incomplete cases are errors, not skips — a labeled set that quietly drops
// cases is a rigged set).
//
// LoadGoldenDir 加载目录内全部 *.yaml 用例（fail-closed：畸形/不完整用例是错误，
// 不是跳过——会悄悄丢用例的标注集是被操纵的标注集）。
func LoadGoldenDir(dir string) ([]GoldenCase, error) {
	matches, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var cases []GoldenCase
	seen := map[string]bool{}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("evalkit: 读取 golden 用例 %s: %w", filepath.Base(path), err)
		}
		var c GoldenCase
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("evalkit: 解析 golden 用例 %s: %w", filepath.Base(path), err)
		}
		if err := validateGoldenCase(&c); err != nil {
			return nil, fmt.Errorf("evalkit: golden 用例 %s: %w", filepath.Base(path), err)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("evalkit: golden 用例 id 重复: %s", c.ID)
		}
		seen[c.ID] = true
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("evalkit: golden 目录 %s 无任何用例", dir)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

func validateGoldenCase(c *GoldenCase) error {
	if c.ID == "" || c.Gate == "" || c.Description == "" {
		return fmt.Errorf("缺 id/gate/description")
	}
	if c.Kind != GoldenDefective && c.Kind != GoldenClean {
		return fmt.Errorf("kind %q 非法（defective|clean）", c.Kind)
	}
	wantExpect := "flagged"
	if c.Kind == GoldenClean {
		wantExpect = "clean"
	}
	if c.Expect != wantExpect {
		return fmt.Errorf("expect %q 与 kind %s 不符（应为 %s）", c.Expect, c.Kind, wantExpect)
	}
	if len(c.ProbeArgv) == 0 {
		return fmt.Errorf("缺 probe_argv")
	}
	if len(c.DetectAny) == 0 {
		return fmt.Errorf("缺 detect_any")
	}
	return nil
}

// GoldenFingerprint hashes the case set (IDs + contents, order-independent).
// The hash is pinned in golden/MANIFEST.sha256; a run whose recomputed hash
// differs refuses to start (防"改样本凑数字") — rotation rewrites the manifest
// explicitly.
//
// GoldenFingerprint 对用例集（ID + 内容，与顺序无关）做哈希。哈希被钉在
// golden/MANIFEST.sha256；重算不一致的运行拒绝启动（防"改样本凑数字"）——轮换
// 显式重写 manifest。
func GoldenFingerprint(cases []GoldenCase) string {
	var b strings.Builder
	for _, c := range cases {
		fmt.Fprintf(&b, "case:%s|gate:%s|kind:%s|expect:%s|det:%v|desc:%s\n", c.ID, c.Gate, c.Kind, c.Expect, c.Deterministic, c.Description)
		for _, f := range c.Files {
			fmt.Fprintf(&b, "file:%s\n%s\n", f.Path, f.Content)
		}
		fmt.Fprintf(&b, "probe:%s\nstdin:%s\ndetect:%s\n", strings.Join(c.ProbeArgv, " "), c.ProbeStdin, strings.Join(c.DetectAny, "|"))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// LoadGoldenManifest reads the pinned fingerprint (empty string = no manifest
// yet — first run writes it via RewriteGoldenManifest).
//
// LoadGoldenManifest 读取被钉住的指纹（空串 = 尚无 manifest——首次运行经
// RewriteGoldenManifest 写入）。
func LoadGoldenManifest(goldenDir string) string {
	data, err := os.ReadFile(filepath.Join(goldenDir, "MANIFEST.sha256"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// RewriteGoldenManifest re-pins the fingerprint after an explicit rotation.
//
// RewriteGoldenManifest 在显式轮换后重新钉住指纹。
func RewriteGoldenManifest(goldenDir string, cases []GoldenCase) error {
	return util.AtomicWrite(filepath.Join(goldenDir, "MANIFEST.sha256"), []byte(GoldenFingerprint(cases)+"\n"), 0o644)
}

// GoldenOutcome labels one case's replay verdict.
//
// GoldenOutcome 标注一个用例的重放判定。
const (
	OutcomeCaptured     = "captured"      // 缺陷被拦 / 干净被放过
	OutcomeMissed       = "missed"        // 缺陷未被拦（recall 缺口）
	OutcomeFalsePositive = "false_positive" // 干净被误触（fpr）
	OutcomeSetupError   = "setup_error"   // fixture/环境失败（不计入 precision/fpr，单独暴露）
)

// GoldenCaseResult is one case's aggregated replay outcome.
//
// GoldenCaseResult 是一个用例聚合后的重放结果。
type GoldenCaseResult struct {
	ID            string   `json:"id"`
	Gate          string   `json:"gate"`
	Kind          string   `json:"kind"`
	Outcome       string   `json:"outcome"`
	Signals       []string `json:"signals,omitempty"`
	ReplayOutcomes []string `json:"replay_outcomes"`
	Agreement     float64  `json:"agreement"`
	Distinct      int      `json:"distinct"`
	DeterminismBug bool    `json:"determinism_bug,omitempty"`
	Error         string   `json:"error,omitempty"`
}

// GoldenReport aggregates a full set replay.
//
// GoldenReport 聚合一次全集重放。
type GoldenReport struct {
	GeneratedAt   time.Time          `json:"generated_at"`
	Fingerprint   string             `json:"fingerprint"`
	Repetitions   int                `json:"repetitions"`
	Precision     RateValue          `json:"precision"` // 缺陷被拦比例
	FalsePositive RateValue          `json:"false_positive"` // 干净误触比例
	Cases         []GoldenCaseResult `json:"cases"`
	Findings      []string           `json:"findings,omitempty"`
}

// GoldenOptions configures a set replay.
//
// GoldenOptions 配置一次全集重放。
type GoldenOptions struct {
	ForgeBin    string // 真实 forge 二进制路径（先构建再重放）
	Repetitions int    // 每用例重放次数（确定性判定），默认 3
	Timeout     time.Duration
}

// probeResult is one probe execution's detection surface.
//
// probeResult 是一次探测执行的检测面。
type probeResult struct {
	Flagged bool
	Signals []string
	Err     string
}

// RunGolden replays the whole labeled set against the real forge binary and
// produces the precision/recall-baseline report. Each replay gets a fresh
// fixture repo + isolated HOME/FORGE_DATA_HOME（verify_scenarios 同款隔离）。
//
// RunGolden 对真实 forge 二进制重放整个标注集并产出 precision/recall 基线报告。
// 每次重放都是全新 fixture 仓库 + 隔离 HOME/FORGE_DATA_HOME（与 verify_scenarios
// 同款隔离）。
func RunGolden(goldenDir string, cases []GoldenCase, opts GoldenOptions) (*GoldenReport, error) {
	if opts.ForgeBin == "" {
		return nil, fmt.Errorf("evalkit: GoldenOptions.ForgeBin 为空——golden 重放需要真实 forge 二进制")
	}
	if opts.Repetitions <= 0 {
		opts.Repetitions = 3
	}
	fp := GoldenFingerprint(cases)
	rep := &GoldenReport{GeneratedAt: time.Now().UTC(), Fingerprint: fp, Repetitions: opts.Repetitions}

	defectiveCaptured, defectiveTotal := 0, 0
	cleanClean, cleanFlagged := 0, 0

	for _, c := range cases {
		res := GoldenCaseResult{ID: c.ID, Gate: c.Gate, Kind: c.Kind}
		for r := 0; r < opts.Repetitions; r++ {
			pr, err := runGoldenProbe(c, opts)
			if err != nil {
				res.Error = err.Error()
				res.ReplayOutcomes = append(res.ReplayOutcomes, "setup_error")
				continue
			}
			outcome := outcomeClean
			if pr.Flagged {
				outcome = "flagged"
			}
			res.ReplayOutcomes = append(res.ReplayOutcomes, outcome)
			if r == 0 {
				res.Signals = pr.Signals
				res.Error = pr.Err
			}
		}
		agreement, distinct := ReplayAgreement(res.ReplayOutcomes)
		res.Agreement = agreement
		res.Distinct = distinct
		if c.Deterministic && agreement < 1.0 && len(res.ReplayOutcomes) > 0 {
			res.DeterminismBug = true
			rep.Findings = append(rep.Findings, fmt.Sprintf("确定性门禁 %s（用例 %s）重放一致率 %.2f（%d 个不同判定）——记 bug", c.Gate, c.ID, agreement, distinct))
		}
		if res.Error != "" && len(res.ReplayOutcomes) == 0 {
			res.Outcome = OutcomeSetupError
		} else {
			first := firstOutcome(res.ReplayOutcomes)
			switch c.Kind {
			case GoldenDefective:
				defectiveTotal++
				if first == "flagged" {
					defectiveCaptured++
					res.Outcome = OutcomeCaptured
				} else if first == "setup_error" {
					res.Outcome = OutcomeSetupError
				} else {
					res.Outcome = OutcomeMissed
				}
			case GoldenClean:
				if first == "flagged" {
					cleanFlagged++
					res.Outcome = OutcomeFalsePositive
				} else if first == "setup_error" {
					res.Outcome = OutcomeSetupError
				} else {
					cleanClean++
					res.Outcome = OutcomeCaptured
				}
			}
		}
		rep.Cases = append(rep.Cases, res)
	}
	rep.Precision = newRateValue(&MetricDef{ID: "golden_precision", MinSamples: 1}, defectiveCaptured, defectiveTotal)
	// fpr 分母 = 全部干净样本（含被误触的——它们正是假阳性本身）。
	rep.FalsePositive = newRateValue(&MetricDef{ID: "golden_fpr", MinSamples: 1}, cleanFlagged, cleanClean+cleanFlagged)
	return rep, nil
}

func firstOutcome(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	return xs[0]
}

const outcomeClean = "clean"

// runGoldenProbe lays the fixture, runs `forge init` + the probe in an isolated
// env, and evaluates the detection signals.
//
// runGoldenProbe 铺设 fixture，在隔离环境跑 forge init + 探测命令，并评估检测信号。
func runGoldenProbe(c GoldenCase, opts GoldenOptions) (*probeResult, error) {
	tmp, err := os.MkdirTemp("", "evalkit-golden-")
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
	for _, f := range c.Files {
		p := filepath.Join(fixture, filepath.FromSlash(f.Path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(p, []byte(f.Content), 0o644); err != nil {
			return nil, err
		}
	}
	env := append(os.Environ(), "HOME="+tmp, "FORGE_DATA_HOME="+dataHome)
	run := func(argv []string, stdin string) (string, string, int, error) {
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
		// 非零退出是探测结果的合法信号（BLOCKED 契约），不是执行错误——只有
		// 起不来的进程（二进制不存在等）才算 error 上抛为 setup_error。
		if runErr != nil {
			var ee *exec.ExitError
			if !asExitError(runErr, &ee) {
				return outBuf.String(), errBuf.String(), -1, runErr
			}
		}
		return outBuf.String(), errBuf.String(), exitCode(runErr), nil
	}
	// git init（项目 key 稳定）+ forge init（项目注册进隔离 registry）。
	if out, errOut, _, err := run([]string{"git", "init", "-q"}, ""); err != nil {
		return nil, fmt.Errorf("git init: %v %s %s", err, out, errOut)
	}
	if out, errOut, _, err := run([]string{opts.ForgeBin, "init"}, ""); err != nil {
		return nil, fmt.Errorf("forge init: %v %s %s", err, out, errOut)
	}
	argv := make([]string, len(c.ProbeArgv))
	for i, a := range c.ProbeArgv {
		argv[i] = strings.ReplaceAll(a, "{forge}", opts.ForgeBin)
	}
	stdout, stderr, code, err := run(argv, c.ProbeStdin)
	if err != nil {
		return nil, err
	}
	pr := &probeResult{}
	signals, err := evalDetectionSignals(c.ID, c.DetectAny, stdout, stderr, code)
	if err != nil {
		return nil, err
	}
	pr.Signals = signals
	pr.Flagged = len(signals) > 0
	return pr, nil
}

// evalDetectionSignals 是 golden 与 traps 共用的检测信号语法（单一真相源）：
// exit_nonzero | stdout:PREFIX | stdout_contains:S | audit_row:S。
// evalDetectionSignals is the detection-signal grammar shared by golden and
// traps probes (single source of truth for both probe paths).
func evalDetectionSignals(caseID string, detectAny []string, stdout, stderr string, code int) ([]string, error) {
	var signals []string
	combined := stdout + "\n" + stderr
	for _, d := range detectAny {
		switch {
		case d == "exit_nonzero":
			if code != 0 {
				signals = append(signals, fmt.Sprintf("exit_nonzero(%d)", code))
			}
		case strings.HasPrefix(d, "stdout:"):
			prefix := strings.TrimPrefix(d, "stdout:")
			if strings.Contains(stdout, prefix) || strings.Contains(stderr, prefix) {
				signals = append(signals, "stdout:"+prefix)
			}
		case strings.HasPrefix(d, "stdout_contains:"):
			s := strings.TrimPrefix(d, "stdout_contains:")
			if strings.Contains(combined, s) {
				signals = append(signals, "stdout_contains:"+s)
			}
		case strings.HasPrefix(d, "audit_row:"):
			s := strings.TrimPrefix(d, "audit_row:")
			if strings.Contains(combined, s) {
				signals = append(signals, "audit_row:"+s)
			}
		default:
			return nil, fmt.Errorf("evalkit: 用例 %s 的未知检测信号 %q", caseID, d)
		}
	}
	return signals, nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if asExitError(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// asExitError is a tiny local type-assertion helper (errors.As without pulling
// the errors import into every caller signature).
//
// asExitError 是一个本地类型断言小助手（免得每个调用方都拖 errors import）。
func asExitError(err error, target **exec.ExitError) bool {
	for err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// PersistGoldenReport writes the JSON report under <evalDir>/forge/ and records
// the eval-golden-run audit row (observation class) on the project root.
//
// PersistGoldenReport 把 JSON 报告写到 <evalDir>/forge/ 下，并在项目根记录
// eval-golden-run 审计行（观察类）。
func PersistGoldenReport(evalDir string, repoRoot string, rep *GoldenReport) (string, error) {
	dir := filepath.Join(evalDir, "forge")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("golden-report-%s.json", rep.GeneratedAt.UTC().Format("20060102-150405")))
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return "", err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	def := rep.Precision
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:  checklog.CheckEvalGoldenRun,
		Passed: true,
		Checked: true,
		Detail: fmt.Sprintf(`golden run: fingerprint %.12s… precision %d/%d fpr %d/%d cases %d findings %d`,
			rep.Fingerprint, int(def.Value*float64(def.Denominator)), def.Denominator,
			rep.FalsePositive.Numerator, rep.FalsePositive.Denominator, len(rep.Cases), len(rep.Findings)),
	})
	return path, nil
}
