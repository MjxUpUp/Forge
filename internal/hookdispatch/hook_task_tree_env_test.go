package hookdispatch

import (
	"strings"
	"testing"
)

// TestTaskTreeEnv 钉住错树信号的注入契约（docs/design/task-tree-guard.md §4）：
// 空锚 = 不注入（与引入守护前完全一致）；非空 = 两个变量同值，且值经 sanitizeForShell
// 洗过（控制字符不能进 env）。
func TestTaskTreeEnv(t *testing.T) {
	if got := taskTreeEnv(""); got != nil {
		t.Fatalf("空锚必须不注入任何变量，got %v", got)
	}

	got := taskTreeEnv("C:/work/repo-wt/feat-x")
	if len(got) != 2 {
		t.Fatalf("非空锚必须注入 2 个变量，got %v", got)
	}
	if !strings.HasPrefix(got[0], "FORGE_TASK_OFFTREE=") || !strings.HasPrefix(got[1], "FORGE_TASK_WORKTREE=") {
		t.Fatalf("变量名不符：%v", got)
	}
	if got[0] != "FORGE_TASK_OFFTREE="+got[len(got)-1][len("FORGE_TASK_WORKTREE="):] {
		t.Fatalf("两个变量必须同值（判定信号与提示一致）：%v", got)
	}

	// 控制字符必须被洗掉，否则 bash 侧读到的路径会断裂。
	dirty := taskTreeEnv("/tmp/x\x00y\x07z")
	if len(dirty) != 2 {
		t.Fatalf("脏锚也必须注入 2 项，got %v", dirty)
	}
	if strings.ContainsRune(dirty[0], 0) || strings.ContainsRune(dirty[0], 7) {
		t.Fatalf("env 值不得含控制字符：%q", dirty[0])
	}
}

// TestDeadAnchorWarning 钉住死锚告警是 advisory：必须指名任务、锚路径与清理出路。
func TestDeadAnchorWarning(t *testing.T) {
	if got := deadAnchorWarning("feat/x", ""); got != "" {
		t.Fatalf("无锚必须不告警，got %q", got)
	}
	got := deadAnchorWarning("feat/x", "C:/gone/wt")
	for _, want := range []string{"feat/x", "C:/gone/wt", "janitor", "不阻断"} {
		if !strings.Contains(got, want) {
			t.Errorf("死锚告警缺 %q：%q", want, got)
		}
	}
}
