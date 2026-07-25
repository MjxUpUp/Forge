package taskpipeline

// executor_skill_decisions.go — task-verify 的 skill-decisions advisory：变更涉及
// skills/<name>/ 的实质内容（SKILL.md/scripts/references 等非 decisions.md 自身）时，
// 提醒用 'forge skills decide' 记录一条决策（诊断/修订/证据/结果四元组）。
//
// 纯 advisory（Passed 恒 true，不阻塞 gate，不 return error）——记录是 agent 自律，
// gate 只提醒"该记了"。persistent decision history：skill 优化的 why 留痕，让下一轮
// agent 理解「为什么这么改」，避免重复探索已失败方向。与 [[forge-experience-knowledge-demolished]]
// 边界一致：审计/可复现，非泛化学习。

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameSkillDecisions 是 task-verify skill-decisions advisory 的 checklog 名。
// trace 留"本次改了 skill、提醒记决策"的信号，gate 仍过（advisory，从不阻塞）。
const CheckNameSkillDecisions checklog.CheckName = "skill-decisions-advisory"

// skillDecisionsAffected 返回本次任务变更涉及「实质 skill 内容」的 skill 名。
//
// 实质内容 = skills/<name>/ 下任意文件变更（含 SKILL.md/scripts/references/cases/）。
// decisions.md 自身的变更不算——写决策正是「记录」动作本身，不需要再提醒「该记了」，
// 否则 decide 命令写完 decisions.md 立刻又触发自己的 advisory，循环噪声。
//
// changed 由调用方算好传入（复用 executor.go 的 gitChanged，避免重复 git 子进程）。
//
// 与 skillEvalAffected 区别：后者只在 skill 有 eval case 集（cases/<name>.json）时触发；
// decisions 提醒对所有 skill 实质变更触发——决策历史从 skill 第一轮优化就该建立，
// 不依赖是否先生成 eval 基准。
func skillDecisionsAffected(changed []string) []string {
	if len(changed) == 0 {
		return nil
	}
	return skillDecisionsNamesFromChanged(changed)
}

// skillDecisionsNamesFromChanged 从变更文件列表提取「实质内容被改」的 skill 名（纯函数）。
// 精确匹配 "skills/" 前缀（同 skillNamesFromChanged 的坑：避免误命中 internal/cli/skills_*.go）。
//
// 与 skillNamesFromChanged 的一处有意差异：rest 无斜杠时（裸 skills/foo 目录路径，
// 非 skills/foo/ 下文件）这里跳过——目录本身不是文件变更，decisions 只关心实质内容
// 文件被改；eval 辅助把裸目录名也计入（更宽松）。真实 diff 极少出现裸目录路径，
// 差异不显现，但语义上 decisions 更严。
func skillDecisionsNamesFromChanged(changed []string) []string {
	seen := make(map[string]bool)
	for _, f := range changed {
		f = filepath.ToSlash(f)
		if !strings.HasPrefix(f, "skills/") {
			continue
		}
		rest := strings.TrimPrefix(f, "skills/")
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			continue // skills/ 根文件（如 CONVENTIONS.md），非 skill 目录，跳过
		}
		name := rest[:i]
		if name == "" || seen[name] {
			continue
		}
		// decisions.md 自身变更不算"该记决策"触发（见函数注释）。
		if filepath.Base(f) == "decisions.md" {
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

// formatSkillDecisionsAdvisory 生成人类可读提醒（照 formatSkillEvalAdvisory 风格）。
// 用单引号包裹命令名，避免 Windows Edit 双引号腐蚀坑（见 windows-input-quote-corruption）。
func formatSkillDecisionsAdvisory(skills []string) string {
	cmds := make([]string, len(skills))
	for i, s := range skills {
		cmds[i] = "decide --skill " + s
	}
	return fmt.Sprintf(
		"变更涉及 skill %s 的实质内容——若为非平凡优化（改了行为/检查项/流程），"+
			"用 'forge skills %s' 记录决策（诊断/修订/证据/结果四元组，让下一轮 agent "+
			"理解 why）。trivial 改动（typo/格式）可忽略",
		strings.Join(skills, ", "), strings.Join(cmds, "; forge skills "))
}
