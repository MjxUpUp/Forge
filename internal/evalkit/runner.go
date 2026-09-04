package evalkit

// runner.go — Track A 端到端运行器（docs/design/forge-evaluation-system.md §四/§六
// P2）：RunSpec 四元组 + 基准 manifest + 预算三上限 + pass^k Scorecard。v1 的执行
// 后端是"命令套件"manifest（每任务一条可执行命令 + 退出码判定）—— Terminal-Bench
// 等 Harbor 格式适配器是同一接口的后续实现，scorecard/统计层不感知后端差异。
// 真实外部执行（联网/容器）由 FORGE_EVAL_SMOKE 显式武装；离线测试走 ScriptedRunner。
//
// runner.go — Track A end-to-end runner: RunSpec four-tuple + benchmark manifest
// + triple budget + pass^k scorecard. The v1 execution backend is a "command
// suite" manifest (one executable command per task, exit-code verdict); Harbor/
// Terminal-Bench adapters implement the same interface later, and the
// scorecard/stats layer stays backend-agnostic. Real external execution
// (network/containers) requires FORGE_EVAL_SMOKE; offline tests use
// ScriptedRunner.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/util"
	"gopkg.in/yaml.v3"
)

// Profile is the Forge protocol-layer factor (the unified experimental factor
// across both tracks).
//
// Profile 是 Forge 协议层因子（贯穿两轨的统一实验因子）。
type Profile string

const (
	ProfileOff       Profile = "off"        // 裸宿主：不注入、不门禁
	ProfileGatesOnly Profile = "gates-only" // 仅门禁 hooks：不注入 skills/conventions
	ProfileFull      Profile = "full"       // 全量：C/S/V/G 四层全生效
)

// Profiles is the ladder in canonical order.
//
// Profiles 是规范顺序的阶梯。
var Profiles = []Profile{ProfileOff, ProfileGatesOnly, ProfileFull}

// ValidProfile reports whether p is a ladder member.
//
// ValidProfile 报告 p 是否阶梯成员。
func ValidProfile(p Profile) bool {
	for _, v := range Profiles {
		if v == p {
			return true
		}
	}
	return false
}

// Budget is the triple cost ceiling; a run exceeding any bound marks the task
// budget-cut (never fail, never dropped — disclosed in the scorecard).
//
// Budget 是三重成本上限；超任一界的任务记 budget-cut（非 fail、非剔除——在
// scorecard 里披露）。
type Budget struct {
	MaxTokens     int           `yaml:"max_tokens"      json:"max_tokens"`
	MaxCostUSD    float64       `yaml:"max_cost_usd"    json:"max_cost_usd"`
	WallclockEach time.Duration `yaml:"wallclock_each"  json:"wallclock_each"`
}

// RunSpec is the four-tuple plus experimental parameters. All fields must be
// set before a run may start (four-tuple is the scorecard header contract).
//
// RunSpec 是四元组加实验参数。开跑前必须全部就位（四元组是 scorecard 头部契约）。
type RunSpec struct {
	Profile   Profile `yaml:"profile"   json:"profile"`
	Model     string  `yaml:"model"     json:"model"`
	Benchmark string  `yaml:"benchmark" json:"benchmark"`
	Split     string  `yaml:"split"     json:"split"`
	Repeats   int     `yaml:"repeats"   json:"repeats"`
	Budget    Budget  `yaml:"budget"    json:"budget"`
	ForgeRef  string  `yaml:"forge_ref" json:"forge_ref"`
}

// Validate enforces the spec invariants.
//
// Validate 强制 RunSpec 不变量。
func (s *RunSpec) Validate() error {
	if !ValidProfile(s.Profile) {
		return fmt.Errorf("evalkit: profile %q 非法（off|gates-only|full）", s.Profile)
	}
	if s.Model == "" || s.Benchmark == "" || s.Split == "" || s.ForgeRef == "" {
		return fmt.Errorf("evalkit: RunSpec 缺 model/benchmark/split/forge_ref（四元组必须齐全）")
	}
	if s.Repeats <= 0 {
		return fmt.Errorf("evalkit: repeats 必须 ≥1，得到 %d", s.Repeats)
	}
	if s.Budget.MaxTokens <= 0 && s.Budget.MaxCostUSD <= 0 && s.Budget.WallclockEach <= 0 {
		return fmt.Errorf("evalkit: 预算三上限全空——无预算上限的精度分数不采信")
	}
	return nil
}

// BenchTask is one task of a manifest. Two backends share the schema:
// command-suite tasks carry `command` (exit 0 = pass); Terminal-Bench frozen
// tasks carry image/run_cmd/test_cmd (executed in the task container, pass =
// test exit 0). A task may carry both — the command then doubles as the
// fallback when Docker is unavailable (scorecard annotates the degradation).
//
// BenchTask 是 manifest 的一个任务。两种后端共用 schema：命令套件任务带
// `command`（退出码 0=通过）；Terminal-Bench 冻结任务带 image/run_cmd/test_cmd
// （任务容器内执行，test 退出 0=通过）。任务可同时携带两者——command 兼作
// Docker 不可用时的回退（scorecard 标注降级）。
type BenchTask struct {
	ID      string `yaml:"id"      json:"id"`
	Command string `yaml:"command" json:"command"` // 退出码 0 = 通过（回退后端）
	// Image/RunCmd/TestCmd are the Terminal-Bench frozen (Harbor-style) task shape.
	//
	// Image/RunCmd/TestCmd 是 Terminal-Bench 冻结（Harbor 风格）任务形态。
	Image   string `yaml:"image,omitempty"   json:"image,omitempty"`
	RunCmd  string `yaml:"run_cmd,omitempty"  json:"run_cmd,omitempty"`
	TestCmd string `yaml:"test_cmd,omitempty" json:"test_cmd,omitempty"`
	// DifficultyBand is the stratification label (kept from the sampling plan).
	DifficultyBand string `yaml:"difficulty_band" json:"difficulty_band"`
	// PollutionFlag marks contamination risk (empty = none).
	PollutionFlag string `yaml:"pollution_flag,omitempty" json:"pollution_flag,omitempty"`
}

// HasDockerShape reports whether the task declares the container backend.
//
// HasDockerShape 报告任务是否声明了容器后端。
func (t BenchTask) HasDockerShape() bool { return t.Image != "" }

// BenchmarkManifest is a versioned, fingerprinted task set.
//
// BenchmarkManifest 是版本化、带指纹的任务集。
type BenchmarkManifest struct {
	ID          string      `yaml:"id"         json:"id"`
	Version     string      `yaml:"version"    json:"version"`
	Split       string      `yaml:"split"      json:"split"`
	Tasks       []BenchTask `yaml:"tasks"     json:"tasks"`
	fingerprint string
}

// LoadManifest reads and validates a manifest YAML (fail-closed: duplicate IDs,
// empty commands, missing version).
//
// LoadManifest 读取并校验 manifest YAML（fail-closed：ID 重复、命令为空、缺版本）。
func LoadManifest(path string) (*BenchmarkManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("evalkit: 读取 manifest 失败: %w", err)
	}
	var m BenchmarkManifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("evalkit: 解析 manifest 失败: %w", err)
	}
	if m.ID == "" || m.Version == "" || m.Split == "" {
		return nil, fmt.Errorf("evalkit: manifest 缺 id/version/split")
	}
	if len(m.Tasks) == 0 {
		return nil, fmt.Errorf("evalkit: manifest %s 无任务", m.ID)
	}
	seen := map[string]bool{}
	for _, t := range m.Tasks {
		if t.ID == "" || t.Command == "" {
			return nil, fmt.Errorf("evalkit: manifest %s 任务缺 id/command", m.ID)
		}
		if seen[t.ID] {
			return nil, fmt.Errorf("evalkit: manifest %s 任务 id 重复: %s", m.ID, t.ID)
		}
		seen[t.ID] = true
	}
	sum := sha256.Sum256(data)
	m.fingerprint = hex.EncodeToString(sum[:])
	return &m, nil
}

// Fingerprint returns the manifest content hash (set by LoadManifest).
//
// Fingerprint 返回 manifest 内容哈希（由 LoadManifest 填充）。
func (m *BenchmarkManifest) Fingerprint() string { return m.fingerprint }

// TaskResult is one task attempt outcome.
//
// TaskResult 是一次任务尝试的结果。
type TaskResult struct {
	TaskID    string        `json:"task_id"`
	Pass      bool          `json:"pass"`
	BudgetCut bool          `json:"budget_cut,omitempty"`
	Error     string        `json:"error,omitempty"`
	Tokens    int           `json:"tokens,omitempty"`
	CostUSD   float64       `json:"cost_usd,omitempty"`
	Duration  time.Duration `json:"duration,omitempty"`
	// Sandbox is the backend THIS task actually ran on — per-task granularity
	// for mixed manifests (docker-shaped tasks → docker, command tasks in the
	// same run → exec). The scorecard aggregates into SandboxMix and flips its
	// top-level label to "mixed" so a host-exec task can never hide behind a
	// docker label (adversarial-review residue fix).
	//
	// Sandbox 是本任务实际执行的后端——混合 manifest 的任务级粒度（docker 形态
	// 任务→docker，同跑里的命令任务→exec）。scorecard 聚合成 SandboxMix，并在
	// 混合时把顶层标签翻成 mixed——宿主 exec 任务不得躲在 docker 标签后面
	// （对抗审查遗留修复）。
	Sandbox string `json:"sandbox,omitempty"`
}

// TaskRunner executes one task attempt under a spec. Implementations:
// ScriptedRunner (deterministic, offline tests/smoke) and ExecRunner (real
// command execution; external effects need FORGE_EVAL_SMOKE).
//
// TaskRunner 在 spec 下执行一次任务尝试。实现：ScriptedRunner（确定性，离线
// 测试/冒烟）与 ExecRunner（真实命令执行；外部效应需 FORGE_EVAL_SMOKE）。
type TaskRunner interface {
	RunTask(ctx context.Context, spec RunSpec, task BenchTask) (TaskResult, error)
}

// ScriptedRunner derives pass/fail deterministically from the task ID hash —
// the offline stand-in that makes the whole Track-A pipeline testable without
// networks or models.
//
// ScriptedRunner 从任务 ID 哈希确定性地导出 pass/fail——让 Track A 全管线在无
// 网络、无模型时可测的离线替身。
type ScriptedRunner struct{}

// RunTask implements TaskRunner.
//
// RunTask 实现 TaskRunner。
func (ScriptedRunner) RunTask(_ context.Context, spec RunSpec, task BenchTask) (TaskResult, error) {
	start := time.Now()
	sum := sha256.Sum256([]byte(string(spec.Profile) + "|" + spec.Model + "|" + task.ID))
	pass := sum[0]%2 == 0
	return TaskResult{TaskID: task.ID, Pass: pass, Tokens: int(sum[1])%900 + 100, Duration: time.Since(start), Sandbox: SandboxScripted}, nil
}

// ExecRunner executes each task's command with a per-task wallclock budget.
// The command runs with profile/model exposed as env (FORGE_EVAL_PROFILE /
// FORGE_EVAL_MODEL) so future model-driving backends hook in without changing
// the runner contract.
//
// ExecRunner 在单任务墙钟预算内执行任务的命令。命令以 env 暴露 profile/model
// （FORGE_EVAL_PROFILE / FORGE_EVAL_MODEL）——将来模型驱动后端接入无需改 runner
// 契约。
type ExecRunner struct{}

// RunTask implements TaskRunner.
//
// RunTask 实现 TaskRunner。
func (ExecRunner) RunTask(ctx context.Context, spec RunSpec, task BenchTask) (TaskResult, error) {
	res := TaskResult{TaskID: task.ID, Sandbox: SandboxExec}
	if spec.Budget.WallclockEach > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Budget.WallclockEach)
		defer cancel()
	}
	start := time.Now()
	cmd := exec.CommandContext(ctx, "sh", "-c", task.Command)
	cmd.Env = minimalEvalEnv(spec)
	out, err := cmd.CombinedOutput()
	res.Duration = time.Since(start)
	_ = out
	switch {
	case ctx.Err() != nil:
		res.BudgetCut = true
		res.Error = "wallclock budget exceeded"
	case err != nil:
		res.Pass = false
	default:
		res.Pass = true
	}
	return res, nil
}

// Attempt is one (task, repeat) outcome inside a run.
//
// Attempt 是一次运行内的一个（任务， 重放）结果。
type Attempt struct {
	TaskID    string  `json:"task_id"`
	Repeat    int     `json:"repeat"`
	Pass      bool    `json:"pass"`
	BudgetCut bool    `json:"budget_cut,omitempty"`
	Tokens    int     `json:"tokens,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`
}

// Scorecard is the run's honest summary. The header is the four-tuple + the
// evaluation-object declaration (ABC III.6) — a scorecard without it refuses
// to render (fail-closed).
//
// Scorecard 是运行的诚实摘要。头部即四元组 + 评测对象声明（ABC III.6）——缺
// 头部的 scorecard 拒绝渲染（fail-closed）。
type Scorecard struct {
	Spec        RunSpec   `json:"spec"`
	ManifestFP  string    `json:"manifest_fingerprint"`
	GeneratedAt time.Time `json:"generated_at"`
	// Sandbox states the execution backend: scripted / exec / docker /
	// fallback-exec. fallback-* means the primary backend was unavailable and
	// the scorecard is annotating the degradation per the design contract.
	//
	// Sandbox 声明执行后端：scripted / exec / docker / fallback-exec。
	// fallback-* 表示主后端不可用，scorecard 按设计契约标注降级。
	// Sandbox 声明执行后端：scripted / exec / docker / fallback-exec / mixed。
	// fallback-exec = 主后端不可用的降级；mixed = 混合 manifest 任务级后端
	// 不一致（分布见 SandboxMix——宿主 exec 任务不得躲在 docker 标签后，
	// 对抗审查遗留修复）。
	Sandbox      string         `json:"sandbox"`
	SandboxMix   map[string]int `json:"sandbox_mix,omitempty"`
	Header       string         `json:"header"` // 渲染契约的第一行
	Pass1        RateValue      `json:"pass1"`
	PassKCurve   []PassKPoint   `json:"pass_k_curve"`
	TotalTokens  int            `json:"total_tokens"`
	TotalCostUSD float64        `json:"total_cost_usd"`
	BudgetCuts   int            `json:"budget_cuts"`
	Note         string         `json:"note,omitempty"`
}

// PassKPoint is one point of the pass^k curve (k 次全对概率的估计).
//
// PassKPoint 是 pass^k 曲线上的一点（k 次全对概率的估计）。
type PassKPoint struct {
	K     int     `json:"k"`
	Value float64 `json:"value"`
}

// RunBenchmark executes spec over the manifest with the runner and aggregates
// the scorecard.
//
// RunBenchmark 用 runner 按 spec 在 manifest 上执行并聚合 scorecard。
func RunBenchmark(ctx context.Context, spec RunSpec, manifest *BenchmarkManifest, runner TaskRunner) (*Scorecard, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}
	if len(manifest.Tasks) == 0 {
		return nil, fmt.Errorf("evalkit: manifest 无任务（pass^k 分母为 0 会产出 NaN，拒绝空跑）")
	}
	var attempts []Attempt
	tokens, cost, cuts := 0, 0.0, 0
	taskSandbox := map[string]string{} // taskID → 该任务实际执行的后端（首试口径）
	for _, task := range manifest.Tasks {
		for r := 1; r <= spec.Repeats; r++ {
			res, err := runner.RunTask(ctx, spec, task)
			if err != nil {
				return nil, fmt.Errorf("evalkit: 任务 %s 执行失败: %w", task.ID, err)
			}
			if r == 1 && res.Sandbox != "" {
				taskSandbox[task.ID] = res.Sandbox
			}
			attempts = append(attempts, Attempt{TaskID: task.ID, Repeat: r, Pass: res.Pass, BudgetCut: res.BudgetCut, Tokens: res.Tokens, CostUSD: res.CostUSD})
			tokens += res.Tokens
			cost += res.CostUSD
			if res.BudgetCut {
				cuts++
			}
		}
	}
	return buildScorecard(spec, manifest, attempts, tokens, cost, cuts, sandboxLabel(runner), taskSandbox), nil
}

// summarizeSandboxes aggregates per-task sandbox labels into a mix histogram
// and the honest top-level label: uniform → that label; mixed (and not an
// already-degraded fallback) → "mixed" — a host-exec task must never present
// behind a docker banner (adversarial-review residue fix).
//
// summarizeSandboxes 把任务级沙箱标签聚合成分布与诚实的顶层标签：单一 → 该
// 标签；混合（且 runner 声明不是已降级的 fallback）→ mixed——宿主 exec 任务
// 不得顶着 docker 横幅出现（对抗审查遗留修复）。
func summarizeSandboxes(runnerLabel string, taskSandbox map[string]string) (string, map[string]int) {
	mix := make(map[string]int, len(taskSandbox))
	for _, sb := range taskSandbox {
		mix[sb]++
	}
	if runnerLabel == SandboxFallbackExec {
		return runnerLabel, mix // 降级声明优先：顶层保留 fallback-exec，mix 展示真实分布
	}
	if len(mix) > 1 {
		return "mixed", mix
	}
	if len(mix) == 1 {
		for k := range mix {
			return k, mix
		}
	}
	return runnerLabel, mix
}

// buildScorecard aggregates attempts into pass@1 and the pass^k curve.
//
// buildScorecard 把 attempts 聚合成 pass@1 与 pass^k 曲线。
func buildScorecard(spec RunSpec, manifest *BenchmarkManifest, attempts []Attempt, tokens int, cost float64, cuts int, runnerLabel string, taskSandbox map[string]string) *Scorecard {
	sandbox, mix := summarizeSandboxes(runnerLabel, taskSandbox)
	byTask := map[string][]bool{}
	for _, a := range attempts {
		byTask[a.TaskID] = append(byTask[a.TaskID], a.Pass)
	}
	taskIDs := make([]string, 0, len(byTask))
	for id := range byTask {
		taskIDs = append(taskIDs, id)
	}
	sort.Strings(taskIDs)
	passFirst := 0
	var curve []PassKPoint
	// pass^k 用组合无偏估计 E_task[C(c,k)/C(n,k)]（τ-bench 同式）——此前的"前 k
	// 次全对"实现系统性低估且对重放顺序敏感（对抗审查 M9）。
	maxK := spec.Repeats
	for k := 1; k <= maxK; k++ {
		sum := 0.0
		for _, id := range taskIDs {
			n := len(byTask[id])
			c := 0
			for _, p := range byTask[id] {
				if p {
					c++
				}
			}
			if c >= k {
				sum += combRatio(c, k, n)
			}
		}
		curve = append(curve, PassKPoint{K: k, Value: sum / float64(len(taskIDs))})
	}
	for _, id := range taskIDs {
		if len(byTask[id]) > 0 && byTask[id][0] {
			passFirst++
		}
	}
	sc := &Scorecard{
		Spec: spec, ManifestFP: manifest.Fingerprint(), GeneratedAt: time.Now().UTC(),
		Sandbox: sandbox, SandboxMix: mix,

		Pass1:      newRateValue(&MetricDef{ID: "e2e_run_pass1", MinSamples: 1}, passFirst, len(taskIDs)),
		PassKCurve: curve, TotalTokens: tokens, TotalCostUSD: cost, BudgetCuts: cuts,
	}
	sc.Header = fmt.Sprintf("profile=%s model=%s benchmark=%s@%s forge_ref=%s sandbox=%s — 本分数为 forge×model 组合评测（评测对象声明，ABC III.6）",
		spec.Profile, spec.Model, spec.Benchmark, spec.Split, spec.ForgeRef, sandbox)
	if cuts > 0 {
		sc.Note = fmt.Sprintf("预算截断 %d 次——截断任务计入 budget-cut，未计入 fail", cuts)
	}
	return sc
}

// combRatio returns C(c,k)/C(n,k) for the pass^k unbiased estimator.
//
// combRatio 返回 pass^k 无偏估计用的 C(c,k)/C(n,k)。
func combRatio(c, k, n int) float64 {
	if k > n || c < k {
		return 0
	}
	r := 1.0
	for i := 0; i < k; i++ {
		r *= float64(c-i) / float64(n-i)
	}
	return r
}

// PersistScorecard writes the scorecard JSON and the eval-run audit row.
//
// PersistScorecard 写 scorecard JSON 与 eval-run 审计行。
func PersistScorecard(evalDir string, repoRoot string, sc *Scorecard) (string, error) {
	dir := filepath.Join(evalDir, "forge", "runs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, fmt.Sprintf("scorecard-%s-%s-%s.json", sc.Spec.Profile, sc.Spec.Model, sc.GeneratedAt.UTC().Format("20060102-150405")))
	data, err := json.MarshalIndent(sc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckEvalRun,
		Passed:  true,
		Checked: true,
		Detail:  fmt.Sprintf(`eval run: %s pass@1 %.3f tokens %d budget_cuts %d`, sc.Header, sc.Pass1.Value, sc.TotalTokens, sc.BudgetCuts) + fmt.Sprintf(` sandbox=%s`, sc.Sandbox),
	})
	return path, nil
}

// SmokeManifestEnv is the escape hatch arming real (non-scripted) benchmark
// execution — external effects must never fire implicitly.
//
// SmokeManifestEnv 是武装真实（非 scripted）基准执行的逃生舱——外部效应绝不
// 隐式触发。
const SmokeManifestEnv = "FORGE_EVAL_SMOKE"

// dockerAvailableFunc is the indirection seam for Docker detection (tests stub
// it; production calls `docker info` once per process and caches).
//
// dockerAvailableFunc 是 Docker 检测的间接缝（测试打桩；生产每进程调用一次
// `docker info` 并缓存）。
var dockerAvailableFunc = func() bool {
	err := exec.Command("docker", "info").Run()
	return err == nil
}

var (
	dockerChecked bool
	dockerUsable  bool
)

// DockerAvailable reports whether the Docker CLI is usable (cached per process).
//
// DockerAvailable 报告 Docker CLI 是否可用（每进程缓存）。
func DockerAvailable() bool {
	if !dockerChecked {
		dockerUsable = dockerAvailableFunc()
		dockerChecked = true
	}
	return dockerUsable
}

// DockerRunner executes Terminal-Bench frozen tasks in their declared
// container: `docker run --rm <image> sh -c "<run_cmd> && <test_cmd>"`; pass =
// test exit 0. Wallclock budget applies via ctx.
//
// DockerRunner 在声明的容器里执行 Terminal-Bench 冻结任务：`docker run --rm
// <image> sh -c "<run_cmd> && <test_cmd>"`；test 退出 0 = 通过。墙钟预算经 ctx
// 生效。
type DockerRunner struct{}

// SandboxLabel implements the sandbox annotation.
//
// SandboxLabel 实现沙箱标注。
func (DockerRunner) SandboxLabel() string { return "docker" }

// RunTask implements TaskRunner. Mixed manifests are dispatched per task:
// a command-suite task inside a docker-selected run falls back to ExecRunner
// instead of executing against an empty image (adversarial review I5).
//
// RunTask 实现 TaskRunner。混合 manifest 按任务分派：docker 被选中的运行里，
// 纯命令任务回退 ExecRunner，而不是对着空镜像执行（对抗审查 I5）。
func (d DockerRunner) RunTask(ctx context.Context, spec RunSpec, task BenchTask) (TaskResult, error) {
	if !task.HasDockerShape() {
		return ExecRunner{}.RunTask(ctx, spec, task)
	}
	res := TaskResult{TaskID: task.ID, Sandbox: SandboxDocker}
	if !DockerAvailable() {
		return res, fmt.Errorf("evalkit: docker 不可用——容器任务 %s 无法执行", task.ID)
	}
	if spec.Budget.WallclockEach > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Budget.WallclockEach)
		defer cancel()
	}
	start := time.Now()
	inner := task.RunCmd
	if inner != "" {
		inner += " && "
	}
	inner += task.TestCmd
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm",
		"-e", "FORGE_EVAL_PROFILE="+string(spec.Profile),
		"-e", "FORGE_EVAL_MODEL="+spec.Model,
		"--network", "none", // 冻结任务自包含；评测容器默认无网（需要网的任务在 manifest 显式声明前不得静默放行）
		task.Image, "sh", "-c", inner)
	cmd.Env = minimalEvalEnv(spec)
	_, err := cmd.CombinedOutput()
	res.Duration = time.Since(start)
	switch {
	case ctx.Err() != nil:
		res.BudgetCut = true
		res.Error = "wallclock budget exceeded"
	case err != nil:
		res.Pass = false
	default:
		res.Pass = true
	}
	return res, nil
}

// minimalEvalEnv is the whitelist passed to benchmark commands — full
// os.Environ() would hand manifest-controlled processes every secret in the
// operator's environment (adversarial review M1).
//
// minimalEvalEnv 是传给基准命令的环境白名单——完整 os.Environ() 会把操作者
// 环境里的全部密钥交给 manifest 控制的进程（对抗审查 M1）。
func minimalEvalEnv(spec RunSpec) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"LANG=" + os.Getenv("LANG"),
		"TMPDIR=" + os.Getenv("TMPDIR"),
		"FORGE_EVAL_PROFILE=" + string(spec.Profile),
		"FORGE_EVAL_MODEL=" + spec.Model,
	}
	// docker CLI 进程需要与 DockerAvailable() 探测一致的路由环境（远程
	// DOCKER_HOST 部署下缺它会"探测通过、执行失败"——复审保留意见）。
	for _, k := range []string{"DOCKER_HOST", "DOCKER_CONTEXT"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Sandbox labels (single source of truth for the scorecard annotation).
//
// 沙箱标签（scorecard 标注的单一真相源）。
const (
	SandboxScripted     = "scripted"      // 确定性离线替身
	SandboxExec         = "exec"          // 命令套件真实执行
	SandboxDocker       = "docker"        // 容器内执行（Terminal-Bench 冻结任务）
	SandboxFallbackExec = "fallback-exec" // 容器不可用，回退命令套件（降级标注）
)

// sandboxLabeled is implemented by runners that declare their sandbox.
//
// sandboxLabeled 由声明自身沙箱的 runner 实现。
type sandboxLabeled interface{ SandboxLabel() string }

func sandboxLabel(r TaskRunner) string {
	if l, ok := r.(sandboxLabeled); ok {
		return l.SandboxLabel()
	}
	return SandboxScripted
}

// SandboxLabel implements the sandbox annotation.
//
// SandboxLabel 实现沙箱标注。
func (ScriptedRunner) SandboxLabel() string { return SandboxScripted }

// SandboxLabel implements the sandbox annotation.
//
// SandboxLabel 实现沙箱标注。
func (ExecRunner) SandboxLabel() string { return SandboxExec }

// degradedRunner wraps the fallback backend so the scorecard annotation keeps
// the degradation visible (label fallback-exec, not the wrapped runner's own).
//
// degradedRunner 包装回退后端，使 scorecard 标注保持降级可见（标签 fallback-exec，
// 而非被包装 runner 自己的标签）。
type degradedRunner struct{ TaskRunner }

// SandboxLabel implements the sandbox annotation with the degradation label.
//
// SandboxLabel 以降级标签实现沙箱标注。
func (degradedRunner) SandboxLabel() string { return SandboxFallbackExec }

// SelectRunner picks the backend per the manifest shape and environment, and
// reports the sandbox label plus whether the choice is a degradation. Ladder:
// docker tasks + smoke + docker usable → DockerRunner; docker tasks + smoke but
// no docker → ExecRunner on the task command (fallback-exec, degraded —
// scorecard annotates); docker tasks without smoke → ScriptedRunner; plain
// command manifests: smoke → ExecRunner, else ScriptedRunner.
//
// SelectRunner 按 manifest 形态与环境挑后端，并给出沙箱标签与是否降级。阶梯：
// 容器任务 + smoke + docker 可用 → DockerRunner；容器任务 + smoke 但无 docker →
// 命令回退（fallback-exec，降级——scorecard 标注）；容器任务无 smoke →
// ScriptedRunner；纯命令 manifest：smoke → ExecRunner，否则 ScriptedRunner。
func SelectRunner(manifest *BenchmarkManifest, smoke bool) (TaskRunner, string, bool) {
	hasDockerTask := false
	for _, t := range manifest.Tasks {
		if t.HasDockerShape() {
			hasDockerTask = true
			break
		}
	}
	if !hasDockerTask {
		if smoke {
			return ExecRunner{}, SandboxExec, false
		}
		return ScriptedRunner{}, SandboxScripted, false
	}
	if !smoke {
		return ScriptedRunner{}, SandboxScripted, false
	}
	if DockerAvailable() {
		return DockerRunner{}, SandboxDocker, false
	}
	return degradedRunner{ExecRunner{}}, SandboxFallbackExec, true
}
