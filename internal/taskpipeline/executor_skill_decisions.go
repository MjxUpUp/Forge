package taskpipeline

// executor_skill_decisions.go — the skill-decisions check for task-verify: split
// into advisory and guardrail tiers (component B: advisory promoted to guardrail).
//
// guardrail (blocking): editing skills/<name>/SKILL.md (the behavior contract) is
// a behavior change — this task must add a decision entry to decisions.md (via
// forge skills decide), otherwise task-verify BLOCKs. SKILL.md is the skill's
// behavior definition (Use when / SKIP / workflow); editing it changes the
// behavior, so a why-trace must be left for the next-round agent to understand,
// avoiding re-exploring failed directions (dogfood rule: pure self-awareness
// always leaks, advisory has zero triggers, only blocking works).
//
// advisory (kept, non-blocking): editing auxiliary resources under skills/<name>/
// (scripts/references/cases, neither SKILL.md nor decisions.md) is a resource
// update — still just a reminder to record a decision; the blast radius of
// auxiliary-resource edits is smaller than the behavior contract, and trivial
// edits (typos/formatting) cluster in auxiliary resources, so keeping advisory
// avoids false positives.
//
// Decision anchor (deterministic signal, not semantic guessing): whether
// decisions.md gained a `## [d-` entry between task base..HEAD. base =
// state.HeadCommit (task start HEAD), reusing the base semantics of
// taskChangedFiles. The current content is read from the working tree (including
// uncommitted decisions.md — agents may not commit immediately after recording);
// the base version comes from git show <base>:path. fail-open: empty base / base
// commit unreachable (amend/rebase) → do not block (aligned with the review
// snapshot philosophy: strict when reachable, lenient when not; forced re-review
// would loop forever).
//
// Boundary: audit/reproducible, not generalized learning
// ([[forge-experience-knowledge-demolished]]).
//
// executor_skill_decisions.go — task-verify 的 skill-decisions 检查：分 advisory 与
// guardrail 两档（B 组件：advisory 升 guardrail）。
//
// guardrail（阻断）：改 skills/<name>/SKILL.md（行为契约）= 行为变更，此 task 必须在
// decisions.md 新增一条决策（forge skills decide），否则 task-verify BLOCKED。SKILL.md
// 是 skill 的行为定义（Use when/SKIP/流程），改它就是改行为——必须留 why 痕迹让下一轮
// agent 理解，避免重复探索已失败方向（dogfood 铁律：纯自觉必漏，advisory 0 触发，必须
// blocking 才生效）。
//
// advisory（保持，不阻断）：改 skills/<name>/ 下辅助资源（scripts/references/cases，非
// SKILL.md 非 decisions.md）= 资源更新，仍只提醒记决策——辅助资源改动的影响面小于行为契约，
// trivial 改动（typo/格式）集中在辅助资源，保持 advisory 不误伤。
//
// 判定锚点（确定信号，非语义猜测）：decisions.md 在 task base..HEAD 间是否新增 `## [d-`
// 条目。base = state.HeadCommit（task start HEAD），复用 taskChangedFiles 的 base 语义。
// 当前读工作区文件（含未提交的 decisions.md——agent 记决策后未必立即 commit），base 版本
// 用 git show <base>:path。fail-open：base 空 / base commit 不可达（amend/rebase）→ 不阻断
// （对齐 review snapshot 哲学：可达则严、不可达则松，强复审会死循环）。
//
// 边界：审计/可复现，非泛化学习（[[forge-experience-knowledge-demolished]]）。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameSkillDecisions is the checklog name for the task-verify skill-decisions
// check. It records the skill-decisions state trace: advisory path (auxiliary
// resource edits) + guardrail pass (decision recorded, Passed=true) / BLOCKED
// (not recorded, Passed=false) / fail-open (base unreachable, check skipped);
// escape-hatch downgrade lands separately on CheckEscapeHatch (Weak ceiling cost).
//
// CheckNameSkillDecisions 是 task-verify skill-decisions 检查的 checklog 名。
// 记 skill-decisions 各态 trace：advisory 路径（辅助资源改动）+ guardrail 通过（已记决策
// Passed=true）/ BLOCKED（未记 Passed=false）/ fail-open（base 不可达跳过校验）；escape-hatch
// 降级另落 CheckEscapeHatch（Weak ceiling 代价）。
const CheckNameSkillDecisions checklog.CheckName = "skill-decisions-advisory"

// skillDecisionsBlockingAffected returns the names of skills whose SKILL.md was
// edited (behavior change → guardrail). It only matches skills/<name>/SKILL.md
// (exact filename SKILL.md) — SKILL.md is the behavior contract, and editing it
// triggers the guardrail. Other files (scripts/references/cases/decisions.md) do
// not enter blocking.
//
// skillDecisionsBlockingAffected 返回改了 SKILL.md（行为变更 → guardrail）的 skill 名。
// 只匹配 skills/<name>/SKILL.md（精确文件名 SKILL.md）——SKILL.md 是行为契约，改它触发
// guardrail。其他文件（scripts/references/cases/decisions.md）不进 blocking。
func skillDecisionsBlockingAffected(changed []string) []string {
	seen := make(map[string]bool)
	for _, f := range changed {
		f = filepath.ToSlash(f)
		if !strings.HasPrefix(f, "skills/") {
			continue
		}
		rest := strings.TrimPrefix(f, "skills/")
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			continue
		}
		name := rest[:i]
		if name == "" || seen[name] {
			continue
		}
		if rest[i+1:] != "SKILL.md" {
			continue
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// skillDecisionsAdvisoryAffected returns the names of skills whose auxiliary
// resources (scripts/references/cases, neither SKILL.md nor decisions.md) were
// edited but whose SKILL.md was **not edited** — these only get an advisory
// reminder, not a block. Skills already in the blocking set (SKILL.md edited) do
// not re-enter advisory (their guardrail already covers them).
//
// skillDecisionsAdvisoryAffected 返回改了辅助资源（scripts/references/cases，非 SKILL.md
// 非 decisions.md）但**未改 SKILL.md**的 skill 名——这些只 advisory 提醒，不阻断。
// 已在 blocking 集（改了 SKILL.md）的 skill 不重复进 advisory（它的 guardrail 已覆盖）。
func skillDecisionsAdvisoryAffected(changed []string) []string {
	blocking := skillDecisionsBlockingAffected(changed)
	bset := make(map[string]bool, len(blocking))
	for _, b := range blocking {
		bset[b] = true
	}
	seen := make(map[string]bool)
	for _, f := range changed {
		f = filepath.ToSlash(f)
		if !strings.HasPrefix(f, "skills/") {
			continue
		}
		rest := strings.TrimPrefix(f, "skills/")
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			continue
		}
		name := rest[:i]
		if name == "" || seen[name] {
			continue
		}
		if bset[name] {
			continue
		}
		// Only decisions.md is excluded (the recording carrier, not a change signal).
		// Skills with canonical SKILL.md (skills/<name>/SKILL.md) are already covered
		// by bset (in the blocking set, continued above) and cannot reach here;
		// subdirectory SKILL.md (skills/<name>/archive/SKILL.md and other non-canonical
		// paths) should not be excluded — going through advisory avoids zero-signal
		// slip-through.
		//
		// 只排除 decisions.md（记录载体，非改动信号）。canonical SKILL.md（skills/<name>/SKILL.md）
		// 的 skill 已被 bset 覆盖（在 blocking 集，上面 continue 了），到不了这里；子目录 SKILL.md
		// （skills/<name>/archive/SKILL.md 等非 canonical）不应排除——走 advisory 避免零信号溜过。
		base := filepath.Base(f)
		if base == "decisions.md" {
			continue
		}
		seen[name] = true
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// skillDecisionsRecorded decides whether a given skill gained a decisions.md
// entry between task base..HEAD. base = state.HeadCommit (task start HEAD). New =
// current `## [d-` count > count at base.
//
// The current content is read from the working tree (including uncommitted
// decisions.md — agents may not commit immediately after recording, so reading
// git HEAD would miss it); the base version comes from git show (historical
// commit; file absent at base returns empty = 0 entries).
//
// When failopen=true the caller must not block (empty base / base commit
// unreachable due to amend/rebase — aligned with the review snapshot philosophy
// of strict-when-reachable, lenient-when-not). When failopen=false the recorded
// value is authoritative.
//
// skillDecisionsRecorded 判定给定 skill 在 task base..HEAD 间是否新增 decisions.md 条目。
// base = state.HeadCommit（task start HEAD）。新增 = 当前 `## [d-` 计数 > base 时计数。
//
// 当前读工作区文件（含未提交的 decisions.md——agent 记决策后未必立即 commit，读 git HEAD
// 会漏判）；base 版本用 git show（历史 commit，base 时文件不存在返空 = 0 条目）。
//
// failopen=true 时调用方不应阻断（base 空 / base commit 不可达 amend/rebase——对齐 review
// snapshot「可达则严、不可达则松」）。failopen=false 时 recorded 真值有效。
func skillDecisionsRecorded(root, base, skill string) (recorded, failopen bool) {
	if base == "" {
		return false, true
	}
	// base commit reachability (amend/rebase rewrites history and the object disappears).
	//
	// base commit 可达性（amend/rebase 改写历史致对象消失）。
	if err := exec.Command("git", "-C", root, "cat-file", "-e", base+"^{commit}").Run(); err != nil {
		return false, true
	}
	cur := countDecisionEntries(currentDecisionsContent(root, skill))
	old := countDecisionEntries(gitShowPath(root, base, "skills/"+skill+"/decisions.md"))
	return cur > old, false
}

// countDecisionEntries counts the `## [d-` decision-entry markers in content
// (skillsdecisions.AppendDecision renders a `## [d-<id>] <outcome>` section). Pure
// string count, does not depend on LoadDecisions parsing — the judgment only
// needs whether the entry count increased, not structured fields.
//
// countDecisionEntries 数 content 里 `## [d-` 决策条目标记数（skillsdecisions.AppendDecision
// 渲染 `## [d-<id>] <outcome>` section）。纯字符串计数，不依赖 LoadDecisions 的解析——
// 判定只需「条目数是否增加」，不需要结构化字段。
func countDecisionEntries(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "## [d-")
}

// currentDecisionsContent reads the current content of skills/<skill>/decisions.md
// from the working tree (returns empty if absent). Reads the file rather than git
// HEAD — agents may not commit immediately after recording, so reading git HEAD
// would miss uncommitted entries and falsely conclude not-recorded.
//
// currentDecisionsContent 读工作区 skills/<skill>/decisions.md 当前内容（不存在返空）。
// 读文件而非 git HEAD——agent 记决策后可能未立即 commit，读 git HEAD 会漏掉未提交条目
// 误判「未记」。
func currentDecisionsContent(root, skill string) string {
	data, err := os.ReadFile(filepath.Join(root, "skills", skill, "decisions.md"))
	if err != nil {
		return ""
	}
	return string(data)
}

// gitShowPath reads the content of path at the base version (git show <base>:<path>).
// Returns empty if path is absent at base (= 0 entries). Used to compare the
// decisions.md entry count at base.
//
// gitShowPath 读 base 版本的 path 内容（git show <base>:<path>）。base 时 path 不存在
// 返空（=0 条目）。用于比对 base 时的 decisions.md 条目数。
func gitShowPath(root, base, path string) string {
	out, err := exec.Command("git", "-C", root, "show", base+":"+path).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// formatSkillDecisionsAdvisory generates the advisory reminder (auxiliary-resource
// edits, non-blocking). Wraps command names in single quotes to avoid the Windows
// Edit double-quote corruption pitfall (see windows-input-quote-corruption).
//
// formatSkillDecisionsAdvisory 生成 advisory 提醒（辅助资源改动，不阻断）。
// 用单引号包裹命令名，避免 Windows Edit 双引号腐蚀坑（见 windows-input-quote-corruption）。
func formatSkillDecisionsAdvisory(skills []string) string {
	cmds := make([]string, len(skills))
	for i, s := range skills {
		cmds[i] = "decide --skill " + s
	}
	return fmt.Sprintf(
		"变更涉及 skill %s 的辅助资源（scripts/references/cases）——若为非平凡优化，"+
			"用 'forge skills %s' 记录决策（诊断/修订/证据/结果四元组，让下一轮 agent "+
			"理解 why）。trivial 改动（typo/格式）可忽略",
		strings.Join(skills, ", "), strings.Join(cmds, "; forge skills "))
}
