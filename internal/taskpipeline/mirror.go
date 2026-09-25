package taskpipeline

// mirror.go — issue-tracker 双向镜像的计划层（focus-batches §2c，方向 C）：Forge 台账
// 为主真相、GitHub Issues 为组织可见面。Symphony（27k stars）验证的入口需求——
// "把 issue tracker 变成控制平面"；Forge 的差异化是台账持久 + 人机混合认领 + 依赖
// 门禁，镜像层让非 Forge 用户在既有项目管理工具里可观察/干预任务。
//
// 本文件只做纯计划（状态 diff → 动作清单）与映射存储；gh CLI 执行在 clitask 侧
//（进程边界：执行失败不影响计划层的可测性）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/util"
)

// MirrorAction 是一条镜像动作：创建 issue / 同步 label / 关闭。
type MirrorAction struct {
	TaskRef string `json:"task_ref"`
	// Issue 为 0 表示"尚无映射——创建"。非 0 为既有 issue 编号。
	Issue int    `json:"issue"`
	Title string `json:"title"`
	// LabelAdd/LabelRemove：forge:<state> 状态 label 的增删（增量——只动 forge:
	// 前缀，不碰用户自己的 label）。
	LabelAdd    []string `json:"label_add,omitempty"`
	LabelRemove []string `json:"label_remove,omitempty"`
	Close       bool     `json:"close,omitempty"`
	Reason      string   `json:"reason"`
}

// mirrorLabel 是状态 label 的统一前缀（forge:offered / forge:claimed / ...）。
func mirrorLabel(status string) string { return "forge:" + status }

// BuildMirrorPlan 从任务状态 + 既有映射计算镜像动作：有 Assignment 的任务按其
// 状态应持有的 forge:<state> label 与映射现状做差集。映射缺 issue → 创建动作；
// label 已对 → no-op（不出现在计划里）。完成/取消/失败的任务 → 关闭动作（终态
// 不再需要组织面关注）。
func BuildMirrorPlan(states []*TaskState, mapping map[string]int) []MirrorAction {
	var actions []MirrorAction
	for _, s := range states {
		if s.Assignment == nil {
			continue
		}
		want := mirrorLabel(s.Assignment.Status)
		issue := mapping[s.TaskRef]
		if issue == 0 {
			actions = append(actions, MirrorAction{
				TaskRef:  s.TaskRef,
				Title:    mirrorTitle(s),
				Reason:   fmt.Sprintf("无映射——创建 issue 并打 %s", want),
				LabelAdd: []string{want},
			})
			continue
		}
		// 有映射：终态任务关闭；非终态无 label 增量需求（v1 不读远端 label——
		// 增量同步需要远端状态，gh 调用层做；计划层只输出终态关闭与缺映射创建）。
		switch s.Assignment.Status {
		case AssignDelivered, AssignCanceled:
			actions = append(actions, MirrorAction{
				TaskRef: s.TaskRef, Issue: issue, Close: true,
				Reason: "任务终态（" + s.Assignment.Status + "）——关闭镜像 issue",
			})
		case AssignFailed:
			actions = append(actions, MirrorAction{
				TaskRef: s.TaskRef, Issue: issue, Close: true,
				LabelAdd: []string{mirrorLabel(AssignFailed)},
				Reason:   "任务失败——打失败 label 并关闭",
			})
		}
	}
	sort.Slice(actions, func(i, j int) bool { return actions[i].TaskRef < actions[j].TaskRef })
	return actions
}

// mirrorTitle 生成 issue 标题：任务摘要 + ref（组织面可读 + 可回溯）。
func mirrorTitle(s *TaskState) string {
	summary := s.Summary
	if summary == "" {
		summary = s.Goal
		if i := strings.IndexByte(summary, '\n'); i > 0 {
			summary = summary[:i]
		}
	}
	if len([]rune(summary)) > 60 {
		summary = string([]rune(summary)[:60]) + "…"
	}
	if summary == "" {
		return "forge task " + s.TaskRef
	}
	return fmt.Sprintf("[forge] %s (%s)", summary, s.TaskRef)
}

// LoadMirrorMapping 读 DataDir/mirror-gh.json（taskRef → issue number）。
func LoadMirrorMapping(root string) (map[string]int, error) {
	body, err := os.ReadFile(mirrorPath(root))
	if os.IsNotExist(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out map[string]int
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("mirror 映射损坏（可删 %s 重建）: %w", mirrorPath(root), err)
	}
	return out, nil
}

// SaveMirrorMapping 原子写回映射。
func SaveMirrorMapping(root string, mapping map[string]int) error {
	body, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(mirrorPath(root), body, 0o644)
}

func mirrorPath(root string) string {
	return filepath.Join(dataHome(root), "mirror-gh.json")
}
