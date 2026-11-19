package taskpipeline

import (
	"fmt"
	"os"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// AppendConclusion builds and persists an Act conclusion for a completed task
// (evidence-driven), returning (conclusion, directive, err): empty directive =
// no RetrospectiveNudge; non-nil err = project resolution or act append failure
// (caller decides stderr/ignore).
//
// AppendConclusion 构建 + 落盘一个完成任务的 Act 结论（证据驱动），返回 (conclusion,
// directive, err)：directive 空=无 RetrospectiveNudge；err 非 nil=project 解析或 act
// append 失败（调用方决定 stderr/忽略——CLI 打 warning，MCP 塞进 Message）。聚合
// checklog.ForTaskUntil 证据链 + 任务事实清单（BuildTaskEvidence），调
// act.BuildConclusion。从 cli/task.go 下沉，让 MCP complete 与 CLI 共用 Act 反馈臂。
func AppendConclusion(root string, state *TaskState) (act.Conclusion, string, error) {
	ec, err := checklog.ForTaskUntil(root, state.TaskRef, state.SealedAt())
	if err != nil {
		// An unreadable evidence chain would otherwise be silently persisted as a
		// "zero-evidence completion" — warn so the IO failure stays attributable.
		fmt.Fprintf(os.Stderr, "[forge] warning: evidence chain unavailable for %s: %v\n", state.TaskRef, err)
	}
	completedAt := time.Now()
	if state.CompletedAt != nil {
		completedAt = *state.CompletedAt
	}
	// 已落盘的事实清单优先：rebuild 等场景只拿到完成时的 TaskState（Acceptance 等
	// 明细可能已被后续迁移裁剪），用 recorded evidence 复原才与 complete 时一致。
	ev := state.Evidence
	if ev == nil {
		ev = BuildTaskEvidence(root, state)
	}
	conc := act.BuildConclusion(state.TaskRef, state.SessionID, ev, ec, completedAt, phaseKeys(state.DesignPhases))
	directive := conc.Directive()
	proj, perr := forgedata.ProjectFor(root)
	if perr != nil {
		return conc, directive, fmt.Errorf(`act conclusion append skipped (project not resolved): %w`, perr)
	}
	if err := act.Append(proj, &conc); err != nil {
		return conc, directive, fmt.Errorf(`act conclusion append failed: %w`, err)
	}
	return conc, directive, nil
}

// phaseKeys 把 DesignPhase slice 转 string slice（act.BuildConclusion 入参）。从 cli/task.go 下沉。
func phaseKeys(phases []DesignPhase) []string {
	if len(phases) == 0 {
		return nil
	}
	out := make([]string, len(phases))
	for i, p := range phases {
		out[i] = string(p)
	}
	return out
}
