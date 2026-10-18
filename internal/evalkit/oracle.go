package evalkit

// oracle.go — 沙箱 oracle v0（docs/design/sandbox-oracle.md 的最小可跑闭环）。
//
// 目标不是打榜，是把"门禁在野外到底拦得住什么"变成一个可复现的数字：
//   - cheat 臂：对真实 git 变更跑确定性 cheat-scan，量召回与误报（in-process，
//     与被测代码同机会话——这是 v0 的诚实边界，隔离执行留给 acceptance 臂）；
//   - acceptance 臂：把变更放进容器跑测试，先只跑可见测试（OFF 臂），再挂
//     held-out 测试（ON 臂），量"没有 held-out 时会逃逸、有了之后能拦住"的差。
//
// 用例自带 ground truth，加载 fail-closed（畸形/重复 id 是错误不是跳过——会
// 悄悄丢用例的考卷是被操纵的考卷）；期望与指纹进报告，结果可复算。
//
// oracle.go — sandbox oracle v0: replay labeled changes against the real
// deterministic cheat-scan and a containerized acceptance arm, measuring recall,
// false positives and held-out escape. Cases are fail-closed and fingerprinted.

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
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"gopkg.in/yaml.v3"
)

// 用例类别与门禁名（与 golden.go 的 kind 词汇刻意区分：oracle 测的是"野外
// 变更"，golden 测的是"门禁自身探测命令"）。
const (
	OracleKindCheat  = "cheat"  // 作弊变更——期望被 cheat-scan 拦截
	OracleKindDefect = "defect" // 语义缺陷——期望被 held-out 验收拦截
	OracleKindClean  = "clean"  // 干净变更——期望放过（误报基线）

	OracleGateScan       = "cheat-scan"
	OracleGateAcceptance = "acceptance"

	oracleBaseTag      = "oracle-base"
	oracleDefaultImage = "golang:1.25-alpine"
)

// OracleMutation 是一条文本替换变更（v0 用 find/replace 而非 unified diff：
// 补丁格式的行尾/上下文漂移是另一类噪声，先不混进来）。
//
// OracleMutation is one textual change applied to the fixture before commit.
type OracleMutation struct {
	File    string `yaml:"file"    json:"file"`
	Find    string `yaml:"find"    json:"find"`
	Replace string `yaml:"replace" json:"replace"`
}

// OracleCase 是一条标注用例：fixture + 变更 + ground truth。
//
// OracleCase is one labeled case: fixture, change, and ground truth.
type OracleCase struct {
	ID          string           `yaml:"id"          json:"id"`
	Kind        string           `yaml:"kind"        json:"kind"` // cheat | defect | clean
	Gate        string           `yaml:"gate"        json:"gate"` // cheat-scan | acceptance
	Description string           `yaml:"description" json:"description"`
	Fixture     string           `yaml:"fixture"     json:"fixture"`
	Category    string           `yaml:"category"    json:"category,omitempty"`
	KnownFP     bool             `yaml:"known_fp"    json:"known_fp,omitempty"`
	Mutations   []OracleMutation `yaml:"mutations"   json:"mutations"`
	// Heldout 是验收臂挂载的隐藏测试：源文件（相对 suite）→ 目标目录（相对仓库根）。
	Heldout       string `yaml:"heldout"        json:"heldout,omitempty"`
	HeldoutTarget string `yaml:"heldout_target" json:"heldout_target,omitempty"`
	HeldoutPkg    string `yaml:"heldout_pkg"    json:"heldout_pkg,omitempty"`
	Expect        string `yaml:"expect"         json:"expect"` // blocked | pass
}

// OracleCaseResult 是单条用例的实跑结果。
//
// OracleCaseResult is the observed outcome of one case.
type OracleCaseResult struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Gate       string   `json:"gate"`
	Expect     string   `json:"expect"`
	Category   string   `json:"category,omitempty"`
	Verdict    string   `json:"verdict"` // blocked | pass | error
	Matched    bool     `json:"matched"`
	Findings   []string `json:"findings,omitempty"`
	Detail     string   `json:"detail,omitempty"`
	Sandbox    string   `json:"sandbox"`
	KnownFP    bool     `json:"known_fp,omitempty"`
	OffVerdict string   `json:"off_verdict,omitempty"`
	OnVerdict  string   `json:"on_verdict,omitempty"`
	Flips      int      `json:"flips,omitempty"`
	DurationMS int64    `json:"duration_ms"`
}

// OracleReport 是一次套件运行的聚合结果（JSON 即证据）。
//
// OracleReport aggregates one suite run; the JSON is the evidence artifact.
type OracleReport struct {
	Suite       string             `json:"suite"`
	Fingerprint string             `json:"fingerprint"`
	Image       string             `json:"image"`
	Arm         string             `json:"arm"`
	StartedAt   time.Time          `json:"started_at"`
	DurationMS  int64              `json:"duration_ms"`
	Cases       []OracleCaseResult `json:"cases"`

	CheatTotal   int `json:"cheat_total"`
	CheatBlocked int `json:"cheat_blocked"`
	CheatMatched int `json:"cheat_matched"`
	CleanTotal   int `json:"clean_total"`
	CleanFP      int `json:"clean_fp"`
	KnownFP      int `json:"known_fp"`
	DefectTotal  int `json:"defect_total"`
	EscapeOff    int `json:"escape_off"`
	EscapeOn     int `json:"escape_on"`
	Flips        int `json:"flips,omitempty"`
	ErrorCases   int `json:"error_cases,omitempty"`

	Recall        float64 `json:"cheat_recall"`
	CategoryAcc   float64 `json:"cheat_category_accuracy"`
	FPR           float64 `json:"clean_fpr"`
	EscapeRateOff float64 `json:"escape_rate_off"`
	EscapeRateOn  float64 `json:"escape_rate_on"`
}

// OracleOptions 控制臂选择、重复次数与镜像。
type OracleOptions struct {
	Arm     string // both | on | off | scanner
	Repeats int
	Image   string
}

// LoadOracleDir 加载目录内全部 *.yaml 用例（cases/ 子目录优先，兼容平铺）。
//
// LoadOracleDir loads every case in dir/cases (falling back to dir), fail-closed.
func LoadOracleDir(dir string) ([]OracleCase, error) {
	base := filepath.Join(dir, "cases")
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		base = dir
	}
	matches, err := filepath.Glob(filepath.Join(base, "*.yaml"))
	if err != nil {
		return nil, err
	}
	var cases []OracleCase
	seen := map[string]bool{}
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("evalkit: 读取 oracle 用例 %s: %w", filepath.Base(path), err)
		}
		var c OracleCase
		if err := yaml.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("evalkit: 解析 oracle 用例 %s: %w", filepath.Base(path), err)
		}
		if err := validateOracleCase(&c); err != nil {
			return nil, fmt.Errorf("evalkit: oracle 用例 %s: %w", filepath.Base(path), err)
		}
		if seen[c.ID] {
			return nil, fmt.Errorf("evalkit: oracle 用例 id 重复: %s", c.ID)
		}
		seen[c.ID] = true
		cases = append(cases, c)
	}
	if len(cases) == 0 {
		return nil, fmt.Errorf("evalkit: oracle 目录 %s 无任何用例", base)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].ID < cases[j].ID })
	return cases, nil
}

// oracleCheatPatterns 用字面量而非 taskpipeline 常量：考卷的类别词汇是对外
// 证据契约，不应随内部常量重命名而静默漂移。
//
// "none" 是显式的"无机械签名"标注：语义作弊（如硬编码返回值让测试通过）不
// 该被拒收，它存在的意义正是记录确定性门禁的盲区——考卷必须能装下"抓不到"。
var oracleCheatPatterns = map[string]bool{
	"type-suppression": true,
	"error-swallow":    true,
	"dead-branch":      true,
	"comment-only-fix": true,
	"comment-as-debt":  true,
	"phantom-import":   true,
	"path-assumption":  true,
	"none":             true,
}

func validateOracleCase(c *OracleCase) error {
	if c.ID == "" || c.Kind == "" || c.Gate == "" || c.Description == "" || c.Fixture == "" {
		return fmt.Errorf("缺 id/kind/gate/description/fixture")
	}
	switch c.Kind {
	case OracleKindCheat, OracleKindDefect, OracleKindClean:
	default:
		return fmt.Errorf("kind %q 非法（cheat|defect|clean）", c.Kind)
	}
	wantGate := OracleGateScan
	if c.Kind == OracleKindDefect {
		wantGate = OracleGateAcceptance
	}
	if c.Gate != wantGate {
		return fmt.Errorf("kind=%s 要求 gate=%s，得到 %s", c.Kind, wantGate, c.Gate)
	}
	switch c.Kind {
	case OracleKindCheat:
		if !oracleCheatPatterns[c.Category] {
			return fmt.Errorf("cheat 用例 category %q 不是七类检测器之一（或 none）", c.Category)
		}
		if c.Expect != "blocked" {
			return fmt.Errorf("cheat 用例 expect 必须是 blocked")
		}
	case OracleKindClean:
		if c.Expect != "pass" {
			return fmt.Errorf("clean 用例 expect 必须是 pass")
		}
	case OracleKindDefect:
		if c.Expect != "blocked" {
			return fmt.Errorf("defect 用例 expect 必须是 blocked")
		}
		if c.Heldout == "" || c.HeldoutTarget == "" || c.HeldoutPkg == "" {
			return fmt.Errorf("defect 用例缺 heldout/heldout_target/heldout_pkg")
		}
	}
	if len(c.Mutations) == 0 {
		return fmt.Errorf("缺 mutations（空变更不是可测的变更）")
	}
	return nil
}

// OracleFingerprint 是对套件输入（用例 YAML 字节）的 sha256——结果可复算的前提。
//
// OracleFingerprint hashes the suite's case bytes so results are reproducible.
func OracleFingerprint(dir string) (string, error) {
	base := filepath.Join(dir, "cases")
	if st, err := os.Stat(base); err != nil || !st.IsDir() {
		base = dir
	}
	paths, err := filepath.Glob(filepath.Join(base, "*.yaml"))
	if err != nil {
		return "", err
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return "", err
		}
		h.Write([]byte(filepath.Base(p)))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}

// RunOracleSuite 实跑整个套件。任一条用例执行失败都不中断（记 verdict=error
// 并计数）——一条坏用例不该吞掉整卷证据；但加载错误会直接返回。
//
// RunOracleSuite runs the whole suite; individual case failures are recorded as
// verdict=error instead of aborting the run.
func RunOracleSuite(suiteDir string, opts OracleOptions) (OracleReport, error) {
	if opts.Arm == "" {
		opts.Arm = "both"
	}
	if opts.Repeats < 1 {
		opts.Repeats = 1
	}
	if opts.Image == "" {
		opts.Image = oracleDefaultImage
	}
	cases, err := LoadOracleDir(suiteDir)
	if err != nil {
		return OracleReport{}, err
	}
	fp, err := OracleFingerprint(suiteDir)
	if err != nil {
		return OracleReport{}, err
	}
	start := time.Now()
	rep := OracleReport{
		Suite:       suiteDir,
		Fingerprint: fp,
		Image:       opts.Image,
		Arm:         opts.Arm,
		StartedAt:   start,
		Cases:       make([]OracleCaseResult, 0, len(cases)),
	}
	for _, c := range cases {
		if opts.Arm == "scanner" && c.Gate != OracleGateScan {
			continue
		}
		if opts.Arm == "acceptance" && c.Gate != OracleGateAcceptance {
			continue
		}
		res := runOracleCase(suiteDir, c, opts)
		rep.Cases = append(rep.Cases, res)
	}
	rep.DurationMS = time.Since(start).Milliseconds()
	rep.finalize()
	return rep, nil
}

func (r *OracleReport) finalize() {
	for _, c := range r.Cases {
		if c.Verdict == "error" {
			r.ErrorCases++
		}
		r.Flips += c.Flips
		switch c.Kind {
		case OracleKindCheat:
			r.CheatTotal++
			if c.Verdict == "blocked" {
				r.CheatBlocked++
			}
			if c.Matched {
				r.CheatMatched++
			}
		case OracleKindClean:
			r.CleanTotal++
			if c.Verdict == "blocked" {
				if c.KnownFP {
					r.KnownFP++
				} else {
					r.CleanFP++
				}
			}
		case OracleKindDefect:
			r.DefectTotal++
			if c.OffVerdict == "pass" {
				r.EscapeOff++
			}
			if c.OnVerdict == "pass" {
				r.EscapeOn++
			}
		}
	}
	ratio := func(n, d int) float64 {
		if d == 0 {
			return 0
		}
		return float64(n) / float64(d)
	}
	r.Recall = ratio(r.CheatBlocked, r.CheatTotal)
	r.CategoryAcc = ratio(r.CheatMatched, r.CheatTotal)
	// 已知取舍不进误报率的分母：它们是被明确接受的形态，混进分母会让误报率
	// 随"诚实标注"变好看——标注越多数字越漂亮，那是被操纵的指标。
	r.FPR = ratio(r.CleanFP, r.CleanTotal-r.KnownFP)
	r.EscapeRateOff = ratio(r.EscapeOff, r.DefectTotal)
	r.EscapeRateOn = ratio(r.EscapeOn, r.DefectTotal)
}

func runOracleCase(suiteDir string, c OracleCase, opts OracleOptions) (res OracleCaseResult) {
	start := time.Now()
	res = OracleCaseResult{
		ID: c.ID, Kind: c.Kind, Gate: c.Gate, Expect: c.Expect,
		Category: c.Category, KnownFP: c.KnownFP, Sandbox: "in-process",
	}
	// 具名返回值：defer 里改 DurationMS 才能落到返回值上（非具名时 defer 改的是
	// 已拷贝的局部副本，报告里会恒为 0）。
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	root, cleanup, err := prepareOracleRepo(suiteDir, c)
	if err != nil {
		res.Verdict, res.Detail = "error", err.Error()
		return res
	}
	defer cleanup()

	switch c.Gate {
	case OracleGateScan:
		runOracleScan(root, c, opts, &res)
	case OracleGateAcceptance:
		runOracleAcceptance(root, suiteDir, c, opts, &res)
	default:
		res.Verdict, res.Detail = "error", "未知 gate: "+c.Gate
	}
	return res
}

// prepareOracleRepo 铺 fixture → git init → 提交基线 → 应用变更 → 提交变更。
// 返回的 cleanup 在调用方 defer，最后清理临时目录。
func prepareOracleRepo(suiteDir string, c OracleCase) (string, func(), error) {
	tmp, err := os.MkdirTemp("", "forge-oracle-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	root := filepath.Join(tmp, "repo")
	fixture := filepath.Join(suiteDir, "fixtures", c.Fixture)
	if st, err := os.Stat(fixture); err != nil || !st.IsDir() {
		return "", cleanup, fmt.Errorf("fixture 不存在: %s", fixture)
	}
	if err := copyOracleTree(fixture, root); err != nil {
		return "", cleanup, fmt.Errorf("铺设 fixture: %w", err)
	}
	if err := oracleGitInit(root); err != nil {
		return "", cleanup, fmt.Errorf("初始化 fixture 仓库: %w", err)
	}
	if err := applyOracleMutations(root, c.Mutations); err != nil {
		return "", cleanup, err
	}
	if _, err := oracleGit(root, "add", "-A"); err != nil {
		return "", cleanup, fmt.Errorf("git add: %w", err)
	}
	if _, err := oracleGit(root, "commit", "-qm", "oracle change"); err != nil {
		return "", cleanup, fmt.Errorf("git commit: %w", err)
	}
	return root, cleanup, nil
}

func runOracleScan(root string, c OracleCase, opts OracleOptions, res *OracleCaseResult) {
	var last []taskpipeline.CheatFinding
	signatures := map[string]bool{}
	for i := 0; i < opts.Repeats; i++ {
		findings := taskpipeline.ScanCheatPatternsRange(root, oracleBaseTag+"..HEAD")
		last = findings
		signatures[oracleFindingSignature(findings)] = true
	}
	if len(signatures) > 1 {
		res.Detail = "确定性漂移：重复运行发现集合不一致"
		res.Flips = 1
	}
	res.Findings = oracleFindingLines(last)
	if len(last) > 0 {
		res.Verdict = "blocked"
	} else {
		res.Verdict = "pass"
	}
	for _, f := range last {
		if string(f.Pattern) == c.Category {
			res.Matched = true
			break
		}
	}
	if c.Kind == OracleKindCheat && !res.Matched && res.Verdict == "blocked" {
		res.Detail = "拦截了，但命中类别与标注不一致（仍记 blocked，类别准确率单列）"
	}
}

func runOracleAcceptance(root, suiteDir string, c OracleCase, opts OracleOptions, res *OracleCaseResult) {
	visiblePass, visibleOut, sandbox := oracleGoTest(root, opts.Image, "./...")
	res.Sandbox = sandbox
	if !visiblePass {
		res.Verdict, res.OnVerdict, res.OffVerdict = "error", "error", "error"
		res.Detail = "可见测试在变更后不通过（用例设计错误：变更应保持可见测试为绿）: " + clipOracleOutput(visibleOut)
		return
	}
	res.OffVerdict = "pass"

	heldSrc := filepath.Join(suiteDir, c.Heldout)
	heldDst := filepath.Join(root, c.HeldoutTarget, filepath.Base(c.Heldout))
	if err := copyOracleFile(heldSrc, heldDst); err != nil {
		res.Verdict, res.OnVerdict = "error", "error"
		res.Detail = "挂载 held-out 失败: " + err.Error()
		return
	}
	onPass, onOut, _ := oracleGoTest(root, opts.Image, c.HeldoutPkg)
	_ = os.Remove(heldDst)
	if onPass {
		res.OnVerdict = "pass"
	} else {
		res.OnVerdict = "blocked"
		res.Detail = "held-out 拒绝：" + firstFailLine(onOut)
	}
	res.Verdict = res.OnVerdict
}

func oracleFindingLines(findings []taskpipeline.CheatFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, fmt.Sprintf("%s@%s:%d", f.Pattern, f.File, f.Line))
	}
	return out
}

func oracleFindingSignature(findings []taskpipeline.CheatFinding) string {
	lines := oracleFindingLines(findings)
	sort.Strings(lines)
	return strings.Join(lines, "|")
}

// --- 仓库与进程辅助 ---

func oracleGit(root string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, clipOracleOutput(string(out)))
	}
	return string(out), nil
}

func oracleGitInit(root string) error {
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "oracle@forge.local"},
		{"config", "user.name", "forge-oracle"},
		{"config", "commit.gpgsign", "false"},
		{"add", "-A"},
		{"commit", "-qm", "oracle baseline"},
		{"tag", oracleBaseTag},
	} {
		if _, err := oracleGit(root, args...); err != nil {
			return err
		}
	}
	return nil
}

func applyOracleMutations(root string, mutations []OracleMutation) error {
	for i, m := range mutations {
		path := filepath.Join(root, filepath.FromSlash(m.File))
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("mutation[%d] 读取 %s: %w", i, m.File, err)
		}
		if !strings.Contains(string(data), m.Find) {
			return fmt.Errorf("mutation[%d] 在 %s 中找不到锚点（fixture 与用例漂移）: %q", i, m.File, clipOracleOutput(m.Find))
		}
		updated := strings.Replace(string(data), m.Find, m.Replace, 1)
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return fmt.Errorf("mutation[%d] 写入 %s: %w", i, m.File, err)
		}
	}
	return nil
}

func copyOracleTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyOracleFile(path, target)
	})
}

func copyOracleFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

var (
	dockerOnce      sync.Once
	dockerAvailable bool
)

func oracleDockerAvailable() bool {
	dockerOnce.Do(func() {
		err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").Run()
		dockerAvailable = err == nil
	})
	return dockerAvailable
}

// oracleGoTest 在一个模块里跑 go test，优先容器（--network none + 只读不了
// 工作区——模块无依赖，只读根会挡住 go test 的缓存写入，故 GOCACHE 指到 tmpfs
// 外的 /tmp）。docker 不可用或首次失败时回退宿主执行并如实标注 sandbox。
func oracleGoTest(root, image, pkg string) (bool, string, string) {
	if oracleDockerAvailable() {
		args := []string{
			"run", "--rm", "--network", "none",
			"-v", root + ":/work", "-w", "/work",
			"-e", "GOCACHE=/tmp/gocache", "-e", "GOFLAGS=-mod=mod", "-e", "GO111MODULE=on",
			image, "go", "test", pkg,
		}
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err == nil {
			return true, string(out), "docker:" + image
		}
		return false, string(out), "docker:" + image
	}
	cmd := exec.Command("go", "test", pkg)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(root, ".gocache"))
	out, err := cmd.CombinedOutput()
	return err == nil, string(out), "host"
}

func clipOracleOutput(s string) string {
	s = strings.TrimSpace(s)
	const max = 400
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

func firstFailLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "--- FAIL") || strings.Contains(t, "FAIL") {
			return clipOracleOutput(t)
		}
	}
	return clipOracleOutput(out)
}

// --- 报告渲染 ---

// RenderOracleMarkdown 渲染可归档的 Markdown 证据（表 + 指标 + 边界声明）。
//
// RenderOracleMarkdown renders the archivable evidence report.
func RenderOracleMarkdown(r OracleReport) string {
	var b strings.Builder
	b.WriteString("# Sandbox oracle v0 报告\n\n")
	fmt.Fprintf(&b, "- 套件指纹：`%s`\n- 镜像：`%s`\n- 臂：`%s`\n- 用例数：%d\n- 耗时：%.1fs\n\n",
		r.Fingerprint, r.Image, r.Arm, len(r.Cases), float64(r.DurationMS)/1000)

	b.WriteString("## 指标\n\n| 指标 | 值 | 口径 |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| cheat 召回 | %.0f%%（%d/%d） | 标注为 cheat 的变更里被拦下的比例 |\n",
		r.Recall*100, r.CheatBlocked, r.CheatTotal)
	fmt.Fprintf(&b, "| 类别准确率 | %.0f%%（%d/%d） | 拦下且命中标注类别的比例 |\n",
		r.CategoryAcc*100, r.CheatMatched, r.CheatTotal)
	fmt.Fprintf(&b, "| 误报率 | %.0f%%（%d/%d） | 干净变更被拦下的比例（known_fp 不进分母） |\n",
		r.FPR*100, r.CleanFP, r.CleanTotal-r.KnownFP)
	fmt.Fprintf(&b, "| 已知取舍 | %d | 标注 known_fp、不计入误报率 |\n", r.KnownFP)
	fmt.Fprintf(&b, "| 逃逸（OFF 臂） | %.0f%%（%d/%d） | 只看可见测试时的漏检率 |\n",
		r.EscapeRateOff*100, r.EscapeOff, r.DefectTotal)
	fmt.Fprintf(&b, "| 逃逸（ON 臂） | %.0f%%（%d/%d） | 挂 held-out 后的漏检率 |\n",
		r.EscapeRateOn*100, r.EscapeOn, r.DefectTotal)
	if r.ErrorCases > 0 {
		fmt.Fprintf(&b, "\n执行错误：%d 条（见逐条结果）。\n", r.ErrorCases)
	}

	b.WriteString("\n## 逐条结果\n\n| 用例 | 类别 | gate | 期望 | 实得 | 命中类别 | 发现 |\n|---|---|---|---|---|---|---|\n")
	for _, c := range r.Cases {
		matched := "—"
		if c.Kind == OracleKindCheat && c.Category == "none" {
			matched = "（无签名）"
		} else if c.Kind == OracleKindCheat && c.Verdict == "blocked" {
			matched = boolMark(c.Matched)
		}
		findings := "—"
		if len(c.Findings) > 0 {
			findings = strings.Join(c.Findings, "<br>")
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s | %s | %s |\n",
			c.ID, c.Kind, c.Gate, c.Expect, c.Verdict, matched, findings)
	}

	b.WriteString("\n## 边界声明\n\n")
	b.WriteString("1. cheat 臂 in-process 运行，未隔离——被测代码与评测器同机；隔离执行只在 acceptance 臂。\n")
	b.WriteString("2. 用例由人标注（v0 为 6 条按已知检测器构造的样例 + 4 条干净对照 + 2 条真实缺陷），不是野外采样，召回率不能外推为真实世界比例。\n")
	b.WriteString("3. defect 臂的 ON/OFF 是同一份代码的两次测试：OFF=只跑可见测试，ON=再挂 held-out。它量的是 held-out 层的价值，不是整条门禁链的价值。\n")
	b.WriteString("4. 镜像按 tag 钉住，未按 digest 钉——v1 需要 digest + 只读根 + 非 root。\n")
	return b.String()
}

func boolMark(v bool) string {
	if v {
		return "✅"
	}
	return "❌"
}

// MarshalOracleJSON 是 JSON 证据的稳定入口（CLI 与测试共用）。
func MarshalOracleJSON(r OracleReport) ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
