package clitask

import (
	"strings"
	"testing"
)

// task_continuity_banner_test.go — P3 managed 会话横幅钉子：`forge task resume
// --hook`（SessionStart 通道）在 managed 项目恒输出 [forge-session] 激活信号——
// 用户级指令段（~/.claude/CLAUDE.md 等）与 forge-quality skill 的条件文案都锚定
// 该横幅（模型可见的机械激活判据）。无活跃任务时原静默，现输出横幅。
func TestTaskResumeHook_ManagedSessionBanner(t *testing.T) {
	t.Setenv(`FORGE_DATA_HOME`, t.TempDir())
	dir := t.TempDir()
	initGitProject(t, dir)
	runForge(t, dir, `task`, `start`, `--ref`, `feat/banner`, `--title`, `横幅`)

	// 有活跃任务：横幅 + handoff（单 PASS 协议，横幅在 handoff 之前）。
	out, _, code := runForge(t, dir, `task`, `resume`, `--hook`)
	if code != 0 {
		t.Fatalf(`resume --hook exit %d: %s`, code, out)
	}
	if strings.Count(out, `PASS`) != 1 {
		t.Errorf("hook 输出应单 PASS（协议），实得 %d 处:\n%s", strings.Count(out, `PASS`), out)
	}
	if !strings.Contains(out, `[forge-session]`) {
		t.Errorf("有活跃任务的 hook 输出缺 managed 横幅:\n%s", out)
	}
	if !strings.Contains(out, `feat/banner`) {
		t.Errorf("横幅不得取代 handoff 本体（任务上下文应在）:\n%s", out)
	}

	// 无活跃任务（abort 清任务状态、项目保持注册）：横幅独立输出（取代原静默）。
	if _, _, code := runForge(t, dir, `task`, `abort`, `--ref`, `feat/banner`); code != 0 {
		t.Fatalf(`task abort failed`)
	}
	out2, _, code := runForge(t, dir, `task`, `resume`, `--hook`)
	if code != 0 {
		t.Fatalf(`resume --hook (no task) exit %d: %s`, code, out2)
	}
	if !strings.Contains(out2, `[forge-session]`) {
		t.Errorf("无活跃任务时横幅仍应输出（激活信号与任务无关）:\n%s", out2)
	}
	if strings.Contains(out2, `feat/banner`) {
		t.Errorf("无活跃任务不应注入任务上下文:\n%s", out2)
	}
}
