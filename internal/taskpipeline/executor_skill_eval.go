package taskpipeline

// executor_skill_eval.go — task-verify 的 skill-eval advisory：变更涉及 skills/<name>/
// 且该 skill 已有 eval case 集时，提醒跑回归。纯 advisory（Passed 恒 true，不阻塞 gate，
// 不 return error）——"有 case 集"本身非判定，trace 只保留信号让 agent 自检。

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// CheckNameSkillEval is the checklog entry name for the task-verify skill-eval
// advisory, so trace surfaces the "changed skill has eval baseline" signal even
// though the gate passes (advisory, never blocking).
const CheckNameSkillEval checklog.CheckName = "skill-eval-gate"

// skillEvalAffected 返回本次任务变更涉及、且已生成 eval case 集的 skill 名。
// 无变更、无 EvalDir、或受影响 skill 都没 case 集时返回 nil（无基准可跑回归→不提醒）。
//
// EvalDir 失败（os.UserHomeDir 出错，极罕见）时静默返回 nil——advisory 不该因目录
// 问题阻塞 gate，与下方 case 集缺失的静默处理一致。
func skillEvalAffected(root string, state *TaskState) []string {
	changed := taskChangedFiles(root, state)
	if len(changed) == 0 {
		return nil
	}
	dir, err := skillseval.EvalDir()
	if err != nil {
		return nil
	}
	return skillNamesFromChanged(changed, dir)
}

// skillNamesFromChanged 从变更文件列表提取「有 case 集」的 skill 名（纯函数，便于测试）。
//
// 精确匹配 "skills/" 前缀而非 substring——避免误命中 internal/cli/skills_*.go、
// internal/skillseval/*.go 这类同名词源码（testcoverage.go:67 的同样坑：用 substring
// "skills/" 会放过 internal/cli/skills_install.go，但这里反过来——我们要的是真 skill
// 目录，HasPrefix "skills/" 正好排掉 internal/ 下的）。
//
// 只纳入 cases/<name>.json 存在的 skill——没生成过 case 集的 skill 没有回归基准，
// 提醒也跑不了，故静默（提醒只给"真有基准可对比"的）。
func skillNamesFromChanged(changed []string, casesDir string) []string {
	seen := make(map[string]bool)
	for _, f := range changed {
		f = filepath.ToSlash(f)
		if !strings.HasPrefix(f, "skills/") {
			continue
		}
		rest := strings.TrimPrefix(f, "skills/")
		// skill 名是 skills/ 后的第一段：skills/<name>/SKILL.md 或 skills/<name>。
		name := rest
		if i := strings.IndexByte(rest, '/'); i >= 0 {
			name = rest[:i]
		}
		if name == "" || seen[name] {
			continue
		}
		if _, err := os.Stat(filepath.Join(casesDir, "cases", name+".json")); err == nil {
			seen[name] = true
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// formatSkillEvalAdvisory 生成人类可读提醒（照 formatMissing 风格）。
func formatSkillEvalAdvisory(skills []string) string {
	cmds := make([]string, len(skills))
	for i, s := range skills {
		cmds[i] = "eval-report --skill " + s
	}
	return fmt.Sprintf(
		"变更涉及 skill %s（已有 eval case 集）——建议跑回归：forge skills %s。"+
			"若改了 description，旧 case 集会 DescHash 失配，先 eval-gen --save 重建基准再 record",
		strings.Join(skills, ", "), strings.Join(cmds, "; forge skills "))
}
