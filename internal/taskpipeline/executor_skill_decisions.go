package taskpipeline

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

// CheckNameSkillDecisions 是 task-verify skill-decisions 检查的 checklog 名。
// 记 skill-decisions 各态 trace：advisory 路径（辅助资源改动）+ guardrail 通过（已记决策
// Passed=true）/ BLOCKED（未记 Passed=false）/ fail-open（base 不可达跳过校验）；escape-hatch
// 降级另落 CheckEscapeHatch（Weak ceiling 代价）。
const CheckNameSkillDecisions checklog.CheckName = "skill-decisions-advisory"

// skillDecisionsBlockingAffected 返回改了 SKILL.md（行为变更 → guardrail）的 skill 名。
// 匹配 skills/<name>/SKILL.md 与插件 pack 树 plugins/<pack>/skills/<name>/SKILL.md
// （2026-09 设计族拆包引入——pack 里的 SKILL.md 同样是行为契约，改它同样触发
// guardrail；决策查找见 skillDecisionsRecorded 的双树候选）。其他文件
// （scripts/references/cases/decisions.md）不进 blocking。
func skillDecisionsBlockingAffected(changed []string) []string {
	seen := make(map[string]bool)
	for _, f := range changed {
		f = filepath.ToSlash(f)
		name, ok := skillNameFromSkillPath(f)
		if !ok || seen[name] {
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

// skillDirFromSkillPath 从变更路径解析 (skill 名, 路径剩余部分, 是否命中 skill 树)。
// 与 skillNameFromSkillPath 同两棵树识别，但接受任意文件（供 advisory 面用）。
func skillDirFromSkillPath(f string) (string, string, bool) {
	rest := ""
	switch {
	case strings.HasPrefix(f, "skills/"):
		rest = strings.TrimPrefix(f, "skills/")
	case strings.HasPrefix(f, "plugins/"):
		i := strings.Index(f, "/skills/")
		if i < 0 {
			return "", "", false
		}
		rest = f[i+len("/skills/"):]
	default:
		return "", "", false
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// skillNameFromSkillPath 从变更路径解析 (skill 名, 是否 SKILL.md 顶层契约)。
// 识别两棵树：canonical skills/<name>/SKILL.md 与 pack plugins/<pack>/skills/<name>/SKILL.md。
func skillNameFromSkillPath(f string) (string, bool) {
	rest := ""
	switch {
	case strings.HasPrefix(f, "skills/"):
		rest = strings.TrimPrefix(f, "skills/")
	case strings.HasPrefix(f, "plugins/"):
		i := strings.Index(f, "/skills/")
		if i < 0 {
			return "", false
		}
		rest = f[i+len("/skills/"):]
	default:
		return "", false
	}
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return "", false
	}
	name := rest[:i]
	if name == "" || rest[i+1:] != "SKILL.md" {
		return "", false
	}
	return name, true
}

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
		name, dir, ok := skillDirFromSkillPath(f)
		if !ok || name == "" || seen[name] {
			continue
		}
		if bset[name] {
			continue
		}
		// 只排除 decisions.md（记录载体，非改动信号）。canonical SKILL.md（<tree>/<name>/SKILL.md）
		// 的 skill 已被 bset 覆盖（在 blocking 集，上面 continue 了），到不了这里；子目录 SKILL.md
		// （<tree>/<name>/archive/SKILL.md 等非 canonical）不应排除——走 advisory 避免零信号溜过。
		// 2026-09 拆包：advisory 面与 blocking 面同步扩双树（canonical + plugins pack）——
		// 拆包后 pack 内辅助资源改动不得零信号（对抗审查 should-fix）。
		base := filepath.Base(dir)
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
	// base commit 可达性（amend/rebase 改写历史致对象消失）。
	if err := exec.Command("git", "-C", root, "cat-file", "-e", base+"^{commit}").Run(); err != nil {
		return false, true
	}
	// 双树候选（2026-09 拆包）：canonical skills/<s>/decisions.md 与 pack
	// plugins/*/skills/<s>/decisions.md——skill 在哪棵树，决策就记在哪棵树旁。
	// 逐路径判净增（任一路径条目数比 base 净增即已记），绝不对路径求和——拆包迁移
	// 场景 canonical 旧条目被整体搬到 pack：求和会把「搬运」误算进基线（cur(pack)=
	// old(canonical)+1 时总和不变，漏判已记）。
	candidates := decisionsCandidates(root, skill)
	for _, path := range candidates {
		cur := 0
		if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))); err == nil {
			cur = countDecisionEntries(string(data))
		}
		old := countDecisionEntries(gitShowPath(root, base, path))
		if cur > old {
			return true, false
		}
	}
	return false, false
}

// decisionsCandidates 列 skill 的 decisions.md 候选路径（canonical + 仓内全部
// plugin pack）。pack 用 glob 现查——pack 目录名不固定（forge-design / 未来更多）。
func decisionsCandidates(root, skill string) []string {
	out := []string{"skills/" + skill + "/decisions.md"}
	matches, _ := filepath.Glob(filepath.Join(root, "plugins", "*", "skills", skill, "decisions.md"))
	for _, m := range matches {
		rel, err := filepath.Rel(root, m)
		if err != nil {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

// countDecisionEntries 数 content 里 `## [d-` 决策条目标记数（skillsdecisions.AppendDecision
// 渲染 `## [d-<id>] <outcome>` section）。纯字符串计数，不依赖 LoadDecisions 的解析——
// 判定只需「条目数是否增加」，不需要结构化字段。
func countDecisionEntries(content string) int {
	if content == "" {
		return 0
	}
	return strings.Count(content, "## [d-")
}

// gitShowPath 读 base 版本的 path 内容（git show <base>:<path>）。base 时 path 不存在
// 返空（=0 条目）。用于比对 base 时的 decisions.md 条目数。
func gitShowPath(root, base, path string) string {
	out, err := exec.Command("git", "-C", root, "show", base+":"+path).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

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
