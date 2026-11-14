package clitask

import "testing"

// 阶段 E：task resume 不再有 hook 兼容模式。flag 复活等于把已删除的 `forge hook`
// 调用面重新打开，这里把它钉死。
func TestTaskResume_NoHookModeFlags(t *testing.T) {
	for _, name := range []string{"hook", "compact-flag", "reinject"} {
		if f := taskResumeCmd.Flags().Lookup(name); f != nil {
			t.Errorf("task resume 仍有已废弃的 %q flag: %s", name, f.Usage)
		}
	}
}
