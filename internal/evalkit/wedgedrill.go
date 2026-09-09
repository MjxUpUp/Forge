package evalkit

// wedgedrill.go — 楔子演练（docs/design/leverage-points-landing.md L1，P0）：
// 脚本化的「首证据」路径——隔离环境（HOME 与 FORGE_DATA_HOME 均在临时目录，
// agent 宿主零接线）跑 init → 建任务并登记一条刻意先红的验收标准 →
// verify-acceptance 如实记下失败 → 修复 → 实跑通过 → trace 可见证据链。
// 双用途：新用户的 60 秒自检（TTFE——首次拿到不可伪造证据的时间）与发布冒烟。
// 演练数字只做回归对比，绝对值不外宣（指标字典误用注记承载）。
//
// wedgedrill.go — wedge drill (docs/design/leverage-points-landing.md L1, P0):
// the scripted "first evidence" path — in an isolated environment (HOME and
// FORGE_DATA_HOME under a temp dir, zero agent-host wiring) run init → start a
// task with a deliberately red acceptance criterion → verify-acceptance
// honestly records the failure → fix → the run passes → trace shows the
// evidence chain. Dual use: a 60-second self-check for new users (TTFE — time
// to first unfakeable evidence) and a release smoke. Drill numbers are for
// regression comparison only (the metric dictionary's misuse note carries this).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// WedgeStepResult is one scripted step with its measured duration and verdict.
//
// WedgeStepResult 是一条脚本步骤的结果（含实测耗时与判定）。
type WedgeStepResult struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Duration string `json:"duration"`
	Detail   string `json:"detail,omitempty"`
}

// WedgeDrillResult is the whole drill's outcome (per-step timing feeds the
// TTFE story — the drill IS the time-to-first-evidence measurement).
//
// WedgeDrillResult 是整场演练的结果（逐步耗时即 TTFE 叙事的数据——演练本身就是
// 「首证据时间」的度量）。
type WedgeDrillResult struct {
	Passed   bool              `json:"passed"`
	FailedAt string            `json:"failed_at,omitempty"`
	Got      string            `json:"got,omitempty"`
	Total    string            `json:"total"`
	Steps    []WedgeStepResult `json:"steps"`
}

// wedgeStep is the internal script unit. expectFail marks steps whose NONZERO
// exit is the expected verdict (the red verify must fail for the drill to pass).
//
// wedgeStep 是内部脚本单元。expectFail 标记「非零退出才是期望判定」的步骤
// （红态 verify 必须如实挂掉，演练才算过）。
type wedgeStep struct {
	name           string
	argv           []string
	env            []string
	expectContains []string
	expectFail     bool
}

// wedgeScript builds the fixed first-evidence script. Fixture writes go through
// `sh -c` with inline `git -c` identity (the resume-drill pattern — proven on
// all three CI platforms). The acceptance starts red on purpose: evidence you
// only ever saw passing is not evidence.
//
// wedgeScript 构建固定的首证据脚本。fixture 写入走 `sh -c` + 内联 `git -c` 身份
// （resume-drill 模式——三平台 CI 实证）。验收刻意先红：只见过通过的证据不是证据。
func wedgeScript(forgeBin string) []wedgeStep {
	return []wedgeStep{
		{name: "git init", argv: []string{"git", "init", "-q"}},
		{name: "forge init", argv: []string{forgeBin, "init"}},
		{name: "fixture(红)", argv: []string{"sh", "-c",
			"printf '#!/bin/sh\\necho NOT-READY\\nexit 1\\n' > main_check.sh && git add main_check.sh && git -c user.name=wedge -c user.email=wedge@localhost -c commit.gpgsign=false commit -qm init"}},
		{name: "task start", argv: []string{forgeBin, "task", "start", "--ref", "feat/wedge-demo", "--branch",
			"--accept", "sh main_check.sh :: ALL-GOOD"}, expectContains: []string{"feat/wedge-demo"}},
		{name: "verify(红·如实挂)", argv: []string{forgeBin, "task", "verify-acceptance"},
			expectFail: true, expectContains: []string{"存在未通过项"}},
		{name: "fixture(绿)", argv: []string{"sh", "-c", "printf '#!/bin/sh\\necho ALL-GOOD\\n' > main_check.sh"}},
		{name: "verify(绿·证据落链)", argv: []string{forgeBin, "task", "verify-acceptance"},
			expectContains: []string{"全部通过"}},
		{name: "trace 证据链", argv: []string{forgeBin, "trace", "feat/wedge-demo"},
			expectContains: []string{"acceptance"}},
	}
}

// RunWedgeDrill replays the first-evidence script in an isolated environment.
// It is a scripted assertion, not an LLM judgment: every step has a mechanical
// verdict (exit code + stdout substring), so a drill pass is a fact.
//
// RunWedgeDrill 在隔离环境重放首证据脚本。这是脚本化断言而非 LLM 判断：每步都有
// 机械判定（退出码 + stdout 子串），演练通过是一个事实。
func RunWedgeDrill(forgeBin string) (WedgeDrillResult, error) {
	if forgeBin == "" {
		return WedgeDrillResult{}, fmt.Errorf("evalkit: 楔子演练需要真实 forge 二进制")
	}
	tmp, err := os.MkdirTemp("", "evalkit-wedge-")
	if err != nil {
		return WedgeDrillResult{}, err
	}
	defer os.RemoveAll(tmp)
	return runDrill(tmp, wedgeScript(forgeBin))
}

// runDrill 是脚本化演练的通用执行器（artifact-drill 与 wedge-drill 共用）：在
// tmp 下建隔离环境（fixture repo + FORGE_DATA_HOME，宿主零接线），逐步执行
// steps 并做机械判定（退出码 + expectContains 子串；expectFail 步骤以非零退出
// 为期望判定——红态如实挂掉才算过）。首个失败步骤短路返回，带 FailedAt/Got。
func runDrill(tmp string, steps []wedgeStep) (WedgeDrillResult, error) {
	fixture := filepath.Join(tmp, "repo")
	dataHome := filepath.Join(tmp, "data")
	for _, dir := range []string{fixture, dataHome} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return WedgeDrillResult{}, err
		}
	}
	// 隔离打底（HOME/FORGE_DATA_HOME → 临时目录；宿主零接线）：与 RunResumeDrills
	// 同构。git 身份由步骤内联 -c 提供，不依赖环境。
	baseEnv := drillEnv(tmp, dataHome)
	run := func(argv, stepEnv []string) (string, int) {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = fixture
		cmd.Env = slices.Concat(baseEnv, stepEnv)
		var out strings.Builder
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.String(), exitCode(err)
	}
	res := WedgeDrillResult{}
	start := time.Now()
	for _, st := range steps {
		t0 := time.Now()
		out, code := run(st.argv, st.env)
		dur := time.Since(t0).Round(time.Millisecond)
		step := WedgeStepResult{Name: st.name, Duration: dur.String(), Detail: truncate(out, 200)}
		ok := code == 0
		if st.expectFail {
			// 红态步骤：非零退出才是期望判定。
			ok = code != 0
		}
		if ok {
			for _, want := range st.expectContains {
				if !strings.Contains(out, want) {
					ok = false
					step.Detail = fmt.Sprintf("stdout 缺 %q：%s", want, truncate(out, 200))
					break
				}
			}
		}
		step.Passed = ok
		res.Steps = append(res.Steps, step)
		if !ok {
			res.Passed = false
			res.FailedAt = st.name
			res.Got = truncate(out, 400)
			res.Total = time.Since(start).Round(time.Millisecond).String()
			return res, nil
		}
	}
	res.Passed = true
	res.Total = time.Since(start).Round(time.Millisecond).String()
	return res, nil
}

// PersistWedgeReport writes the wedge drill report and the audit row.
//
// PersistWedgeReport 写楔子演练报告与审计行。
func PersistWedgeReport(evalDir string, repoRoot string, res WedgeDrillResult) (string, error) {
	dir := evalDataDir(evalDir)
	payload := map[string]any{"generated_at": time.Now().UTC(), "passed": res.Passed, "total": res.Total, "steps": res.Steps}
	data, err := jsonMarshal(payload)
	if err != nil {
		return "", err
	}
	path := filepathJoin(dir, fmt.Sprintf("wedge-drill-%s.json", time.Now().UTC().Format("20060102-150405")))
	if err := atomicWriteFile(path, data); err != nil {
		return "", err
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckWedgeDrill,
		Passed:  res.Passed,
		Checked: true,
		Detail:  fmt.Sprintf(`wedge drill: %d steps, total %s`, len(res.Steps), res.Total),
	})
	return path, nil
}
