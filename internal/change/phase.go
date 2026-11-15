package change

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// phase.go —— change 的阶段机（入口路由 A2）。
//
// 与 change.go 里既有的 Stage（empty/proposal/design/tasks/verified，描述「产物到哪了」）
// 不同，Phase 描述的是「开工前还缺什么」，序列固定且短路：
//
//	specify → clarify → plan → tasks → ready
//
// 推导只读文件、不写状态：阶段永远可以从磁盘重建，所以没有「状态文件过期」的漂移。
// 它服务两个入口：`forge change phase` 告诉人/agent 下一步补什么；`forge task start
// --change` 要求 ready 才放行。

// Phase 是「开工前」的推进阶段。
type Phase string

const (
	PhaseSpecify Phase = "specify"
	PhaseClarify Phase = "clarify"
	PhasePlan    Phase = "plan"
	PhaseTasks   Phase = "tasks"
	PhaseReady   Phase = "ready"
)

// FileClarify 是澄清记录的文件名。
const FileClarify = "clarify.md"

// Clarify 是 clarify.md 的解析结果。
type Clarify struct {
	// Boundary 是「做什么 / 不做什么」的正文。
	Boundary string
	// NonGoals 是明确不做的事。
	NonGoals string
	// ConfirmedAt 是人确认的时间（零值表示文件缺 `人确认：` 行）。
	ConfirmedAt time.Time
}

// ParseClarify 解析 clarify.md：必须有非空的 `## 边界` 正文与一行 `人确认：<UTC>`。
// 其余章节不强制——澄清是记录，不是模板填空比赛。
func ParseClarify(raw []byte) (*Clarify, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	c := &Clarify{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "## "):
			section = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))
			section = strings.Join(strings.Fields(section), "")
			continue
		case strings.HasPrefix(trimmed, "人确认："):
			stamp := strings.TrimSpace(strings.TrimPrefix(trimmed, "人确认："))
			if t, err := time.Parse(time.RFC3339, stamp); err == nil {
				c.ConfirmedAt = t.UTC()
			}
			continue
		}
		switch {
		case strings.HasPrefix(section, "边界") || strings.HasPrefix(section, "boundary"):
			c.Boundary += line + "\n"
		case strings.HasPrefix(section, "非目标") || strings.HasPrefix(section, "nongoals") || strings.HasPrefix(section, "outofscope"):
			c.NonGoals += line + "\n"
		}
	}
	c.Boundary = strings.TrimSpace(c.Boundary)
	c.NonGoals = strings.TrimSpace(c.NonGoals)
	if c.Boundary == "" {
		return nil, fmt.Errorf("change: %s 缺非空的「## 边界」段", FileClarify)
	}
	if c.ConfirmedAt.IsZero() {
		return nil, fmt.Errorf("change: %s 缺「人确认：<UTC 时间>」行——先问用户，得到明确回答后再记录", FileClarify)
	}
	return c, nil
}

// RenderClarify 渲染一份 clarify.md。
func RenderClarify(id, boundary, nonGoals string, now time.Time) string {
	var b strings.Builder
	b.WriteString("# 澄清：边界与非目标\n\n")
	fmt.Fprintf(&b, "change: `%s`\n\n", id)
	b.WriteString("## 边界（做什么 / 不做什么）\n\n")
	b.WriteString(strings.TrimSpace(boundary))
	b.WriteString("\n\n## 非目标\n\n")
	if ng := strings.TrimSpace(nonGoals); ng != "" {
		b.WriteString(ng)
	} else {
		b.WriteString("（沿用 goal.md 的 Out of Scope）")
	}
	fmt.Fprintf(&b, "\n\n人确认：%s\n", now.UTC().Format(time.RFC3339))
	return b.String()
}

// UnboundTasks 返回没有写判据编号的任务序号（1-based）。stage 推导与 status 共用。
func UnboundTasks(tasks []Task) []int {
	var out []int
	for _, t := range tasks {
		if len(t.Criteria) == 0 {
			out = append(out, t.Index)
		}
	}
	return out
}

// ComputePhase 只读文件推导 change 的阶段；missing 是给人看的「还缺什么 / 下一步」。
// 老 change（没有 clarify.md）会停在 clarify——一次 `forge change clarify` 即可放行，
// verify/gate 不受影响。
func ComputePhase(root, id string) (Phase, []string, error) {
	if err := ValidID(id); err != nil {
		return "", nil, err
	}
	dir := filepath.Join(root, "spec", "changes", id)
	if _, err := os.Stat(dir); err != nil {
		return "", nil, fmt.Errorf("change: 不存在 %s", relTo(root, dir))
	}

	if _, _, _, err := LoadGoal(root, id); err != nil {
		return PhaseSpecify, []string{fmt.Sprintf("goal.md 五段没填完：%v", err), "填 spec/changes/" + id + "/goal.md 并删掉 forge:fill 标记"}, nil
	}

	clarifyRaw, err := os.ReadFile(filepath.Join(dir, FileClarify))
	if err != nil {
		return PhaseClarify, []string{"缺 clarify.md（边界与非目标 + 人确认）", "forge change clarify " + id + " --by-human --note \"边界/非目标\""}, nil
	}
	if _, err := ParseClarify(clarifyRaw); err != nil {
		return PhaseClarify, []string{err.Error(), "补齐后重跑 forge change clarify " + id + " --by-human --note …（或 --force 覆盖）"}, nil
	}

	designRaw, err := os.ReadFile(filepath.Join(dir, FileDesign))
	if err != nil {
		return PhasePlan, []string{"缺 design.md", "写 design.md：怎么改、为什么这么改、影响面与风险"}, nil
	}
	if strings.Contains(string(designRaw), scaffoldMarker) || strings.TrimSpace(string(designRaw)) == "" {
		return PhasePlan, []string{"design.md 还是脚手架（含 " + scaffoldHint + "）", "把 design.md 写成真正的方案"}, nil
	}

	tasksRaw, err := os.ReadFile(filepath.Join(dir, FileTasks))
	if err != nil {
		return PhaseTasks, []string{"缺 tasks.md", "写 tasks.md，每条任务末尾带「（判据：acceptance #N）」"}, nil
	}
	if strings.Contains(string(tasksRaw), scaffoldMarker) {
		return PhaseTasks, []string{"tasks.md 还是脚手架（含 " + scaffoldHint + "）", "把示例换成真实任务，每条带「（判据：acceptance #N）」"}, nil
	}
	tasks := ParseTasks(tasksRaw)
	if len(tasks) == 0 {
		return PhaseTasks, []string{"tasks.md 里没有任务", "至少列一条任务（- [ ] 1. …（判据：acceptance #N））"}, nil
	}
	if unbound := UnboundTasks(tasks); len(unbound) > 0 {
		parts := make([]string, 0, len(unbound))
		for _, n := range unbound {
			parts = append(parts, fmt.Sprintf("#%d", n))
		}
		return PhaseTasks, []string{"这些任务没写判据编号：" + strings.Join(parts, "、"), "在任务行末尾加「（判据：acceptance #N）」"}, nil
	}

	return PhaseReady, nil, nil
}