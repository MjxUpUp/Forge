package cliskills

// skills_decide_warrant.go — R2 Warrant-Aware Acceptance：`skills decide --warrant`
// 的锚点解析与核验。accept 决策缺可核验锚点、锚点核验失败、或 run 锚点相对
// baseline 有回归时给 WARN，并落一条 checklog decision-warrant 行。advisory：
// 决策照常写入，命令退出码不变——让"这条 accept 有没有可核验的理由"先变成可观测
// 的数字，升格为必填是后续 deferred 项。
//
// 动因：Phantom Guardrails（arXiv 2607.13083）——只看结果的接受门会放行"不掉分的
// 虚构改动"；要求改动引用可被核验的证据后虚构率降到 0。本仓 271 条 skill 决策全部
// accept、零 run 锚点（2026-10-13 实测）。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// Warrant verdicts (the values of checklog meta.verdict). Strong is reserved for
// a run anchor that was meaningfully compared with the skill's baseline and came
// out clean; everything that merely proves an artifact exists is weak.
//
// 锚点核验结论（checklog meta.verdict 的取值）。strong 只留给"与该 skill 的 baseline
// 做了有效比对且无回归"的 run 锚点；只证明某物存在的一律是 weak。
const (
	warrantStrong     = "strong"     // run 锚点与 baseline 有效比对且无回归
	warrantWeak       = "weak"       // 锚点存在但未做有效比对——证明"有依据"，不证明"有改进"
	warrantMissing    = "missing"    // accept 决策未带 --warrant
	warrantInvalid    = "invalid"    // 锚点格式错误或指向不存在的对象
	warrantRegression = "regression" // run 锚点相对 baseline 有回归
)

// commitSHAPattern restricts commit anchors to 4–64 hex chars: a format check
// that also stops values starting with "-" from reaching git as options.
//
// commitSHAPattern 限定 commit 锚点为 4–64 位十六进制：既是格式校验，也挡住以 "-"
// 开头的值被 git 当成选项（参数注入）。
var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{4,64}$`)

// warrantResult is the outcome of checking one --warrant value.
//
// warrantResult 是一次 --warrant 核验的结果。Kind 为 run/commit/doc（无法解析时为空）；
// Problems 只在 invalid/regression 时非空，逐条可读；Notes 说明为什么一个存在的
// run 锚点只算弱（未做有效比对的原因）。
type warrantResult struct {
	Kind     string
	Value    string
	Verdict  string
	Problems []string
	Notes    []string
}

// passed reports whether the warrant is acceptable (strong or weak).
//
// passed 报告锚点是否可接受（strong 或 weak）。
func (r warrantResult) passed() bool {
	return r.Verdict == warrantStrong || r.Verdict == warrantWeak
}

// evaluateWarrant parses and checks a --warrant value. evalDir is the eval data
// dir for run anchors; repoRoot is the project root for commit and doc anchors
// ("" when decide runs outside a project — those kinds are then invalid).
//
// evaluateWarrant 解析并核验 --warrant 值。evalDir 是 run 锚点的 eval 数据目录；
// repoRoot 是 commit/doc 锚点的项目根（decide 不在项目内运行时为 ""——这两类锚点
// 此时判 invalid）。空值判 missing（调用方只对 accept 决策要求锚点）。
func evaluateWarrant(raw, skill, evalDir, repoRoot string) warrantResult {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return warrantResult{Verdict: warrantMissing}
	}
	invalid := func(kind, value, format string, args ...any) warrantResult {
		return warrantResult{Kind: kind, Value: value, Verdict: warrantInvalid, Problems: []string{fmt.Sprintf(format, args...)}}
	}
	if strings.ContainsAny(raw, "\r\n") {
		return invalid("", raw, "锚点必须是单行")
	}
	kind, value, ok := strings.Cut(raw, ":")
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return invalid("", raw, "锚点格式应为 <类型>:<值>（run:<eval-run-id> | commit:<sha> | doc:<仓库内路径>），得到 %q", raw)
	}
	switch kind {
	case "run":
		return evaluateRunWarrant(skill, value, evalDir)
	case "commit":
		if repoRoot == "" {
			return invalid(kind, value, "不在 forge 项目内，无法核验 commit 锚点")
		}
		if !commitSHAPattern.MatchString(value) {
			return invalid(kind, value, "commit 锚点应为 4–64 位十六进制 sha，得到 %q", value)
		}
		if err := exec.Command("git", "-C", repoRoot, "rev-parse", "--verify", "--quiet", value+"^{commit}").Run(); err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return invalid(kind, value, "git 不可用，无法核验 commit 锚点")
			}
			return invalid(kind, value, "commit %s 在 %s 中不存在", value, repoRoot)
		}
		return warrantResult{Kind: kind, Value: value, Verdict: warrantWeak}
	case "doc":
		if repoRoot == "" {
			return invalid(kind, value, "不在 forge 项目内，无法核验 doc 锚点")
		}
		if problem := checkDocWarrant(repoRoot, value); problem != "" {
			return invalid(kind, value, "%s", problem)
		}
		return warrantResult{Kind: kind, Value: value, Verdict: warrantWeak}
	default:
		return invalid(kind, value, "未知锚点类型 %q（支持 run: / commit: / doc:）", kind)
	}
}

// evaluateRunWarrant resolves a run anchor and compares it with the skill's
// baseline. A run that exists is weak by default; it becomes strong only after
// a meaningful clean comparison (a distinct, loadable baseline; overlapping
// cases; no baseline case missing; comparable versions). Regressions are listed
// even when the runs are not comparable, with that caveat attached.
//
// evaluateRunWarrant 解析 run 锚点并与该 skill 的 baseline 比对。run 存在默认只算
// 弱；只有做了有效且干净的比对（baseline 存在、可加载且不是锚点本身；有重叠 case；
// 未缺 baseline 的 case；版本/模型/description 可比）才算强。有回归时即使不可比也
// 逐条列出，并注明不可比原因交给人判断。比对做不成一律落 Notes 说明原因——绝不在
// 没比对的情况下报强（fail-closed 地降级，而非 fail-open 地升级）。
func evaluateRunWarrant(skill, runID, evalDir string) warrantResult {
	res := warrantResult{Kind: "run", Value: runID}
	if evalDir == "" {
		res.Verdict = warrantInvalid
		res.Problems = []string{"eval 数据目录无法解析，无法核验 run 锚点"}
		return res
	}
	run, err := skillseval.LoadRunByID(evalDir, skill, runID)
	if err != nil {
		res.Verdict = warrantInvalid
		res.Problems = []string{fmt.Sprintf("读取 eval run 失败：%v", err)}
		return res
	}
	if run == nil {
		res.Verdict = warrantInvalid
		res.Problems = []string{fmt.Sprintf("run %s 在 %s 下不存在（skill %s）", runID, evalDir, skill)}
		return res
	}
	// run 已核验存在：默认弱，下面每一处比对做不成都带原因返回。
	res.Verdict = warrantWeak
	weak := func(format string, args ...any) warrantResult {
		res.Notes = append(res.Notes, fmt.Sprintf(format, args...))
		return res
	}
	baselines, err := skillseval.LoadBaselines(evalDir)
	if err != nil {
		return weak("baseline 无法读取（%v），未做比对", err)
	}
	bl, ok := baselines[skill]
	if !ok || bl.RunID == "" {
		return weak("该 skill 未设 baseline，未做比对")
	}
	if bl.RunID == runID {
		return weak("锚点就是 baseline 本身，没有对照")
	}
	base, err := skillseval.LoadRunByID(evalDir, skill, bl.RunID)
	if err != nil || base == nil {
		return weak("baseline run %s 无法加载，未做比对", bl.RunID)
	}
	rep := skillseval.CompareRuns(run, base)
	if len(rep.Regressions) > 0 {
		res.Verdict = warrantRegression
		for _, r := range rep.Regressions {
			res.Problems = append(res.Problems, fmt.Sprintf("case %s：baseline %s 通过 → run %s 未通过", r.CaseID, bl.RunID, runID))
		}
		if !rep.Comparable {
			res.Problems = append(res.Problems, "注意：两次 run 不可比（"+rep.IncomparableReason+"），回归可能是假回归")
		}
		return res
	}
	if rep.Matched == 0 {
		return weak("与 baseline %s 没有重叠 case，比对无信息", bl.RunID)
	}
	if len(rep.Removed) > 0 {
		return weak("缺少 baseline %s 的 %d 个 case，覆盖不完整", bl.RunID, len(rep.Removed))
	}
	if !rep.Comparable {
		return weak("与 baseline %s 不可比（%s），无回归不等于无退化", bl.RunID, rep.IncomparableReason)
	}
	res.Verdict = warrantStrong
	return res
}

// checkDocWarrant validates a repo-relative doc path: no absolute or rooted
// paths, no escape from repoRoot (after cleaning and resolving symlinks), and
// the target must be an existing regular file. It returns "" when valid.
//
// checkDocWarrant 校验仓库内相对路径：拒绝绝对路径与以分隔符开头的根路径（Windows
// 上 "/etc/hosts" 无盘符、IsAbs 为假，须单独拦）；规整并解析符号链接后不得跳出
// repoRoot；目标必须是存在的普通文件（目录只证明"有这个地方"）。合法返回 ""。
func checkDocWarrant(repoRoot, rel string) string {
	clean := filepath.Clean(filepath.FromSlash(rel))
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, `\`) || filepath.IsAbs(clean) || filepath.VolumeName(clean) != "" {
		return fmt.Sprintf("doc 锚点必须是仓库内相对路径，得到绝对路径 %q", rel)
	}
	if slashed := filepath.ToSlash(clean); slashed == ".." || strings.HasPrefix(slashed, "../") { // 统一成 / 再比较，跨平台一致
		return fmt.Sprintf("doc 锚点 %q 跳出了仓库根", rel)
	}
	rootReal, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return fmt.Sprintf("无法解析仓库根 %s：%v", repoRoot, err)
	}
	target := filepath.Join(rootReal, clean)
	targetReal, err := filepath.EvalSymlinks(target)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("doc %s 在仓库中不存在", rel)
		}
		return fmt.Sprintf("无法解析 doc %s：%v", rel, err)
	}
	relToRoot, err := filepath.Rel(rootReal, targetReal)
	relToRoot = filepath.ToSlash(relToRoot) // 统一成 / 再比较，跨平台一致
	if err != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, "../") {
		return fmt.Sprintf("doc 锚点 %q 经符号链接跳出了仓库根", rel)
	}
	if info, err := os.Stat(targetReal); err != nil || !info.Mode().IsRegular() {
		return fmt.Sprintf("doc 锚点必须指向普通文件，%q 不是", rel)
	}
	return ""
}

// warrantMessage renders the one-line CLI message for a warrant result.
//
// warrantMessage 渲染核验结果的 CLI 提示行（advisory 文案统一注明决策已照常记录）。
func warrantMessage(r warrantResult) string {
	const tail = "（advisory：决策已照常记录）"
	switch r.Verdict {
	case warrantStrong:
		return fmt.Sprintf("✅ [decision-warrant] 强锚点核验通过：run %s 与 baseline 比对无回归", r.Value)
	case warrantWeak:
		if len(r.Notes) > 0 {
			return fmt.Sprintf("ℹ️ [decision-warrant] 弱锚点：%s:%s 存在，但%s——它证明有依据、不证明有改进", r.Kind, r.Value, strings.Join(r.Notes, "；"))
		}
		return fmt.Sprintf("ℹ️ [decision-warrant] 弱锚点核验通过：%s:%s——它证明有依据、不证明有改进；有 eval run 时优先用 run:<id>", r.Kind, r.Value)
	case warrantMissing:
		return "⚠️ [decision-warrant] accept 决策未带可核验锚点——用 --warrant run:<eval-run-id>（强）| commit:<sha> | doc:<仓库内路径>（弱）指向支撑这次 accept 的证据" + tail
	case warrantRegression:
		return fmt.Sprintf("⚠️ [decision-warrant] run 锚点相对 baseline 有 %d 条回归：%s%s", countRegressionLines(r.Problems), strings.Join(r.Problems, "；"), tail)
	default:
		return "⚠️ [decision-warrant] 锚点核验失败：" + strings.Join(r.Problems, "；") + tail
	}
}

// countRegressionLines counts the per-case lines (excluding the trailing
// incomparability note).
//
// countRegressionLines 统计逐 case 的回归行数（不计末尾的不可比注释行）。
func countRegressionLines(problems []string) int {
	n := 0
	for _, p := range problems {
		if strings.HasPrefix(p, "case ") {
			n++
		}
	}
	return n
}

// reportWarrant checks the warrant of a just-recorded decision, prints the
// result to stderr and appends the checklog row. Outside a forge project the
// commit/doc kinds are invalid and no row is written.
//
// reportWarrant 核验刚记录决策的理由锚点：结果打到 stderr 并追加 checklog 行。
// 不在 forge 项目内时 commit/doc 两类锚点判 invalid，且不落行。
func reportWarrant(cmd *cobra.Command, skill, outcome, warrant string) {
	root, err := projectroot.Find()
	if err != nil {
		root = ""
	}
	evalDir, err := evalDataDir()
	if err != nil {
		evalDir = ""
	}
	r := evaluateWarrant(warrant, skill, evalDir, root)
	w := io.Writer(os.Stderr)
	if cmd != nil {
		w = cmd.ErrOrStderr()
	}
	fmt.Fprintln(w, warrantMessage(r))
	recordWarrantRow(root, skill, outcome, r)
}

// recordWarrantRow appends the decision-warrant checklog row. Failures to write
// are ignored: the row is an observation, never a reason to fail decide.
//
// recordWarrantRow 追加 decision-warrant checklog 行。写入失败忽略——该行是观测，
// 永远不能成为 decide 失败的理由。
func recordWarrantRow(root, skill, outcome string, r warrantResult) {
	if root == "" {
		return
	}
	level := checklog.LevelWarn
	if r.Verdict == warrantStrong {
		level = checklog.LevelPass
	}
	meta := map[string]string{
		"skill":   skill,
		"outcome": outcome,
		"verdict": r.Verdict,
	}
	if r.Kind != "" {
		meta["kind"] = r.Kind
	}
	switch {
	case r.Kind != "" && r.Value != "":
		meta["warrant"] = r.Kind + ":" + r.Value
	case r.Value != "":
		meta["warrant"] = r.Value
	}
	if len(r.Notes) > 0 {
		meta["note"] = strings.Join(r.Notes, "；")
	}
	_ = checklog.Record(root, &checklog.Entry{
		Check:   checklog.CheckDecisionWarrant,
		Passed:  r.passed(),
		Checked: true,
		Level:   level,
		Source:  checklog.EvidenceDeterministic,
		Detail:  warrantMessage(r),
		Meta:    meta,
	})
}
