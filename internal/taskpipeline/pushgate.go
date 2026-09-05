package taskpipeline

// pushgate.go — git/PR 收口 v1（focus-batches §1c，方向 A）：门禁终裁从本地会话
// 扩到 git 推送边界。云端 agent（Cursor Cloud Agents / Copilot cloud / Devin /
// Factory）不经本地 hook，但全部以 git/PR 为界面——push 是唯一同时覆盖本地会话、
// 云端分支与 CI 的汇聚点。本地 hook 层降格为"证据生产者"（Codex #28365 遥测欺骗
// 证明本地自我报告需上层复核），push 时确定性复检。
//
// 三项检查（全部 deterministic，不依赖本地 hook 曾生效）：
//  1. 工作树未提交变更（warn——推送边界不应夹带未审工作）
//  2. 推送范围（base..HEAD）新增行跑 cheat-scan 批量模式（复用 7 类检测器）
//  3. 本分支未完成任务的最新 BLOCKED 行未消解 → 阻断（任务门禁的推送侧消费）
//
// 结果落 checklog CheckGatePush + 推送证据快照 DataDir/pushes/<ts>.json（不进
// 仓库——证据留在治理侧，git 对象只承载代码）。逃生舱 FORGE_GATE_PUSH=disable
// （紧急放行路径，落 escape-hatch 留痕）。

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
)

// pushGateDisableEnv 是 push 门禁的逃生舱（紧急放行：pre-push 阻断了不该阻断的
// 推送时，显式绕过并留痕——沿 FORGE_DOC_GATE 模式）。
const pushGateDisableEnv = "FORGE_GATE_PUSH"

// PushGateResult 是 RunPushGate 的结构化结果（CLI 文案与快照共用）。
type PushGateResult struct {
	Ref          string         `json:"ref"`
	Base         string         `json:"base"`
	Dirty        bool           `json:"dirty"`
	Findings     []CheatFinding `json:"findings,omitempty"`
	BlockedTasks []string       `json:"blocked_tasks,omitempty"`
	// Skipped=true 表示门禁未运行（非 git 仓库 / 无基准 / 逃生舱）——区分
	// "未检查"与"检查通过"（insufficient ≠ pass 的诚实边界）。
	Skipped bool      `json:"skipped"`
	Reason  string    `json:"reason,omitempty"`
	At      time.Time `json:"at"`
}

// Blocked 报告推送是否应被拒绝：cheat 命中或未消解 BLOCKED 任务。
func (r PushGateResult) Blocked() bool {
	return !r.Skipped && (len(r.Findings) > 0 || len(r.BlockedTasks) > 0)
}

// RunPushGate 执行推送边界检查并落 checklog 行 + DataDir/pushes 快照。
// ref 为空时取当前分支；base 解析顺序 @{push} 合并基 → origin/HEAD → origin/main
// → origin/master → main → master（全部失败则 Skipped——孤儿分支不设障）。
func RunPushGate(root, ref string) PushGateResult {
	res := PushGateResult{At: time.Now()}
	if os.Getenv(pushGateDisableEnv) == "disable" {
		res.Skipped = true
		res.Reason = "escape-hatch: FORGE_GATE_PUSH=disable"
		recordAudit(root, &checklog.Entry{
			Check: checklog.CheckEscapeHatch, Passed: true, Checked: true,
			Level:  checklog.LevelWarn,
			Detail: `escape-hatch: push gate bypassed (FORGE_GATE_PUSH=disable)`,
		})
		writePushSnapshot(root, res)
		return res
	}
	if !IsGitRepo(root) {
		res.Skipped = true
		res.Reason = "not a git repo"
		return res
	}
	if ref == "" {
		ref = gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
		if ref == "" {
			res.Skipped = true
			res.Reason = "no current branch"
			return res
		}
	}
	res.Ref = ref
	res.Dirty = gitStatusDirty(root)
	res.Base = resolvePushBase(root)
	if res.Base == "" {
		res.Skipped = true
		res.Reason = "no merge-base found（孤儿分支/无 origin）"
		recordPushGate(root, res)
		return res
	}
	res.Findings = ScanCheatPatternsRange(root, res.Base+"...HEAD")
	res.BlockedTasks = blockedTasksOnBranch(root, ref)

	recordPushGate(root, res)
	writePushSnapshot(root, res)
	return res
}

// ScanCheatPatternsRange 对一个 git 修订范围（如 base...HEAD）的新增行跑同一组
// 确定性作弊检测器（push 门禁的复检面——与 task-verify 的 ScanCheatPatterns 同
// 检测器不同 diff 基，任务态无关）。文件面由范围自身决定（--name-only），不依赖
// taskChangedFiles。
func ScanCheatPatternsRange(root, gitRange string) []CheatFinding {
	nameOut := gitOut(root, "diff", "--name-only", gitRange)
	if nameOut == "" {
		return nil
	}
	sourceSet := map[string]bool{}
	for _, f := range strings.Split(nameOut, "\n") {
		f = strings.TrimSpace(filepath.ToSlash(f))
		if f != "" && isSourceFile(f) {
			sourceSet[f] = true
		}
	}
	if len(sourceSet) == 0 {
		return nil
	}
	added := parseGitAddedLines(root, []string{"-U0", gitRange}, sourceSet)
	var prod []addedLine
	for _, a := range added {
		if !isTestFile(a.file) {
			prod = append(prod, a)
		}
	}
	if len(prod) == 0 {
		return nil
	}
	var code []addedLine
	for _, a := range prod {
		if !isCommentOrBlank(a.text) {
			code = append(code, a)
		}
	}
	var findings []CheatFinding
	findings = append(findings, detectTypeSuppression(prod)...)
	findings = append(findings, detectErrorSwallow(code)...)
	findings = append(findings, detectDeadBranch(code)...)
	findings = append(findings, detectCommentOnly(prod)...)
	findings = append(findings, detectCommentDebt(prod)...)
	findings = append(findings, detectPhantomImport(root, code)...)
	findings = append(findings, detectPathAssumption(code)...)
	return findings
}

// blockedTasksOnBranch 找本分支未完成任务里"最新状态仍是 BLOCKED"的任务：按
// check 名取该任务最后一条记录，EffectiveLevel 为 blocked 的存在即未消解。
func blockedTasksOnBranch(root, branch string) []string {
	states, err := ListTaskStates(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range states {
		if s.Branch != branch || s.CompletedAt != nil {
			continue
		}
		entries, err := checklog.LoadForTask(root, s.TaskRef)
		if err != nil {
			continue
		}
		latest := map[checklog.CheckName]checklog.Entry{}
		for _, e := range entries {
			latest[e.Check] = e // entries 时间序——后写覆盖即最新
		}
		for _, e := range latest {
			if e.EffectiveLevel() == checklog.LevelBlocked {
				out = append(out, s.TaskRef)
				break
			}
		}
	}
	return out
}

// resolvePushBase 依序尝试推送基（@{push} 合并基优先——那正是本次要推的范围）。
func resolvePushBase(root string) string {
	candidates := []string{
		"@{push}",
		"origin/HEAD",
		"origin/main",
		"origin/master",
		"main",
		"master",
	}
	for _, c := range candidates {
		base := gitOut(root, "merge-base", "HEAD", c)
		if base != "" {
			return base
		}
	}
	return ""
}

func gitOut(root string, args ...string) string {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gitStatusDirty(root string) bool {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// recordPushGate 落 checklog 行（blocked→fail；dirty-only→warn；否则 pass）。
func recordPushGate(root string, res PushGateResult) {
	e := &checklog.Entry{
		Check:   checklog.CheckGatePush,
		Passed:  !res.Blocked(),
		Checked: !res.Skipped,
	}
	var parts []string
	for _, f := range res.Findings {
		parts = append(parts, fmt.Sprintf("cheat:%s(%s)", f.Pattern, f.File))
	}
	for _, t := range res.BlockedTasks {
		parts = append(parts, "blocked-task:"+t)
	}
	if res.Dirty {
		parts = append(parts, "工作树有未提交变更")
	}
	switch {
	case res.Blocked():
		e.Level = checklog.LevelFail
		e.Detail = "BLOCKED: " + strings.Join(parts, "; ")
	case res.Skipped:
		e.Level = checklog.LevelWarn
		e.Detail = "ADVISORY: push gate skipped: " + res.Reason
	case len(parts) > 0:
		e.Level = checklog.LevelWarn
		e.Detail = "ADVISORY: " + strings.Join(parts, "; ")
	default:
		e.Level = checklog.LevelPass
		e.Detail = fmt.Sprintf("pass: %s...HEAD 干净（%s）", shortSHA(res.Base), res.Ref)
	}
	recordAudit(root, e)
}

// writePushSnapshot 把推送证据快照写 DataDir/pushes/<ts>.json（AtomicWrite；不进
// 仓库——git 对象只承载代码，证据留治理侧）。
func writePushSnapshot(root string, res PushGateResult) {
	dir := filepath.Join(dataHome(root), "pushes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	body, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return
	}
	name := fmt.Sprintf("push-%s.json", res.At.UTC().Format("20060102-150405"))
	_ = util.AtomicWrite(filepath.Join(dir, name), body, 0o644)
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
