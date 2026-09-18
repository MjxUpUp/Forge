package hookdispatch

// hook_task_drift.go —— task-drift（escape-hatch-hardening P0-B,
// docs/plans/escape-hatch-hardening-2026-09.md）:git 边界动词
// （commit / merge / branch <name> / checkout -b / switch -c）发生在活跃任务
// 分支之外时的 choke-point advisory。
//
// 病灶（2026-09-18 sess_7e05f7e1 取证）:任务存在时 task-guard 直接 PASS、
// bash-guard 无 git 语义——agent 在任务分支之外 5 次 commit + 3 次建分支 +
// 2 次 merge,三道门全程零中介。执法原本只挂在 `forge task gate` 这个 agent
// 不会主动敲的动词上;本 hook 把中介挪到 agent 必经的 git 边界动词。
//
// P0 契约:advisory、永不阻断（BLOCK ratchet 属 P1,≥2 minor 预告）;任务分支
// 上的 commit 是既定合法顺序（commit-before-complete）,零输出零行;无任务/
// 非 git 边界动词/判定前提缺失（git 不可用）→ 静默放行;阶梯节流 1/2/10n——
// 审计不静默但不刷屏。

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// gitBoundaryVerb 返回命令中的 git 边界子命令（commit/merge/branch/checkout/
// switch）,非边界返回空串。扫描 "git" token 之后的首个非全局旗标 token（跳过
// -c/-C/--git-dir/--work-tree 及其取值）;branch 需带非旗标名字 token（git
// branch -a 是列表查询）,checkout/switch 需带建分支旗标（-b/-c/B/C）——纯切换
// 不触发（切到任务分支正是补救动作,不罚）。
func gitBoundaryVerb(command string) string {
	// 归一化命令替换形态:"$(git" 与 "`git" 是单个 token,strings.Fields 拆不出
	// git——`forge x --ref $(git commit ...)` 里的 git 会真执行,扫描器必须
	// 看得见(与 forge 豁免分隔符集的 "(" 互补:豁免侧拒掉伪造的 forge 前缀,
	// 扫描侧看得见替换里的真 git)。
	command = strings.ReplaceAll(command, "$(", "$( ")
	command = strings.ReplaceAll(command, "`", " ` ")
	fields := strings.Fields(command)
	for i := 0; i < len(fields); i++ {
		if fields[i] != "git" {
			continue
		}
		j := i + 1
		for j < len(fields) && strings.HasPrefix(fields[j], "-") {
			// 带值的 git 全局旗标连值一起跳过。
			switch fields[j] {
			case "-C", "-c", "--git-dir", "--work-tree", "--namespace":
				j++
			}
			j++
		}
		if j >= len(fields) {
			return ""
		}
		sub := strings.TrimSuffix(fields[j], ",")
		switch sub {
		case "commit", "merge":
			return sub
		case "branch":
			// 只认建分支形态 `git branch <name>`:后随 token 须是非旗标名字。
			// `git branch -a/-v/--list`（查询）与 `-d/-m`（删除/改名）不是建支
			// ——把高频查询报成"边界命令"是假事实（审查必改项 1）。
			if j+1 < len(fields) && !strings.HasPrefix(fields[j+1], "-") {
				return sub
			}
		case "checkout", "switch":
			for _, f := range fields[j+1:] {
				if f == "-b" || f == "-c" || f == "-B" || f == "-C" || f == "--create" {
					return sub
				}
				if f == "--" || !strings.HasPrefix(f, "-") {
					break // 首个非旗标参数是切换目标而非建分支
				}
			}
		}
		// 本处 git 不是边界形态——继续扫后续 git token（"git add X && git
		// commit ..." 的 commit 在第二个 git 段,首个 return 会漏检——测试
		// TestRunTaskDriftHook_AdvisoryOnBranchDrift 钉住该形态）。
	}
	return ""
}

// currentGitBranch 返回 root 的当前分支名;git 失败/非仓库返回空串（调用方
// 静默放行——advisory fail-open,git 前提缺失不构成漂移证据）。
func currentGitBranch(root string) string {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// driftLadderCount 会话级阶梯计数（DataDir markers,与 task-guard ignores 同款
// 根同语义）:返回本次的出现序号（已 +1 并落盘）。读-改-写无锁与 testNudgeState
// 同款取舍——并发窗口最坏重复一个序号,fail-open 可接受。
func driftLadderCount(root, sessionID string) int {
	path := filepath.Join(forgedata.DataDirFor(root), "markers", "forge-taskdrift-"+util.SanitizeSessionID(sessionID))
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	n := 0
	if data, err := os.ReadFile(path); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &n)
	}
	n++
	_ = util.AtomicWrite(path, []byte(fmt.Sprintf("%d", n)), 0644)
	return n
}

// taskDriftAdvisory 阶梯各档文案。第 1 次给全量出口（回分支/推门禁/显式弃）,
// 第 2 次升档,之后每第 10 次一行审计摘要——口径与 spec P0-B 契约 5 一致。
func taskDriftAdvisory(verb, branch, taskBranch, gate string, n int) string {
	switch {
	case n == 1:
		return fmt.Sprintf("[task-drift] git 边界命令（%s）发生在任务分支之外——当前分支 %s,任务分支 %s（门 %s）。任务分支外的提交不进门禁追踪与评分。出口：回任务分支提交（git checkout %s）;或推进门禁（forge task gate %s --ref <ref>）;若有意切换目标,先 forge task abort --ref <ref> 显式弃任务（留痕）。", verb, branch, taskBranch, gate, taskBranch, gate)
	case n == 2:
		return fmt.Sprintf("[task-drift]（第 2 次,已升档）仍在任务分支外执行 git 边界命令（%s,%s ≠ %s）。立即选择出口——第 3 次起记违规计数,每 10 次留审计行。", verb, branch, taskBranch)
	case n%10 == 0:
		return fmt.Sprintf("[task-drift] 第 %d 次任务分支外 git 边界命令（%s,%s ≠ %s）——审计留痕。", n, verb, branch, taskBranch)
	}
	return ""
}

// runTaskDriftHook 是 task-drift 的进程内 PreToolUse Bash hook。判定链:边界
// 动词 → 活跃任务 → 真实 git 分支 ≠ state.Branch → 阶梯 advisory + checklog
// warn 行;任一环缺失静默放行。永不阻断（P0 契约）。
func runTaskDriftHook(hookInput HookInput, root, version, agent string) error {
	if hookInput.ToolName != "Bash" || len(hookInput.ToolInput) == 0 {
		return nil
	}
	var fields struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil || fields.Command == "" {
		return nil
	}
	verb := gitBoundaryVerb(fields.Command)
	if verb == "" {
		return nil
	}
	// forge 前缀豁免:任务 CLI 自建/切分支（forge task start --branch）合法。
	// 严格形态对齐 bash-guard IS_FORGE_CMD 的分隔符集(含 '$('——命令替换
	// `forge x --ref $(git commit -m y)` 里的 git 会真执行,不能豁免;审查
	// 必改项 4)。换行分隔符是两者共享的已知缺口,另开小修。
	if trimmed := strings.TrimSpace(fields.Command); strings.HasPrefix(trimmed, "forge ") && !strings.ContainsAny(trimmed, ";|>&`(") {
		return nil
	}
	state, _, err := taskpipeline.ActiveTaskStateWithPath(root, util.SanitizeSessionID(hookInput.SessionID))
	if err != nil || state == nil {
		return nil
	}
	if state.Branch == "" {
		return nil
	}
	branch := currentGitBranch(root)
	if branch == "" || branch == state.Branch {
		return nil
	}

	n := driftLadderCount(root, hookInput.SessionID)
	if n != 1 && n != 2 && n%10 != 0 {
		return nil // 阶梯间静默计数（marker 即审计面）
	}
	detail := taskDriftAdvisory(verb, branch, state.Branch, state.CurrentGate, n)
	// 送达章（AdvisoryEmissionChannel）:发射走 EmitAdvisoryRouted 是真实送达
	// 通道（kimi 入队 = false + kimi/advisory-queue,其余宿主按通道实判）——
	// 漏斗读方可区分「送达/入队/丢失」,与「delivered 必须说真话」纪律对齐。
	delivered, channel := AdvisoryEmissionChannel(agent, hookInput.HookEventName)
	entry := &checklog.Entry{
		Check:        checklog.CheckTaskDrift,
		Passed:       false, // 漂移事实成立（advisory 形态,P0 不阻断）
		Checked:      true,
		ToolName:     hookInput.ToolName,
		SessionID:    hookInput.SessionID,
		TaskRef:      state.TaskRef,
		Detail:       util.TruncateRunes(detail, 400),
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelWarn,
		Delivered:    &delivered,
		Channel:      channel,
		ForgeVersion: version,
		Meta: map[string]string{
			"verb":        verb,
			"branch":      branch,
			"task_branch": state.Branch,
			"gate":        state.CurrentGate,
			"occurrence":  fmt.Sprintf("%d", n),
		},
	}
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[task-drift] warning: checklog record failed: %v\n", err)
	}
	// stdout allow-with-detail 通道（EmitAdvisoryRouted 与 test-nudge 同款:
	// kimi 入队攒发,其余宿主直达上下文）——choke point 的价值在 agent 看得见。
	// 注意传 passed=true：该参数是**发射动词**（allow+additionalContext vs
	// deny+exit 2）,不是检查结论——P0 永不阻断契约要求 allow 路径;checklog 行
	// 的 Passed=false 已单独记录漂移事实,两者解耦（emitClaudeOutput 的
	// passed=false 在 PreToolUse 会发 permissionDecision:deny——不能碰）。
	return EmitAdvisoryRouted(agent, hookInput.HookEventName, "task-drift", root, hookInput.SessionID, true, detail)
}
