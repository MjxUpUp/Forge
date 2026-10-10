package cli

import (
	"testing"

	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// isolateSessionIdentity clears every session-identity env the resolver reads,
// so a test that assumes "no session identity" holds even when the suite runs
// inside an agent host (e.g. Claude Code exports CLAUDE_CODE_SESSION_ID to its
// shell). The host env names are derived from the hostcap registry — never
// hand-listed — so a new host's ShellSessionEnv is covered automatically.
//
// isolateSessionIdentity 清空会话身份解析器读取的全部 env，使假定「无会话身份」的
// 测试在 agent 宿主内运行（如 Claude Code 向 shell 导出 CLAUDE_CODE_SESSION_ID）时
// 依然成立。宿主 env 名从 hostcap 注册表派生、不手抄——新宿主的 ShellSessionEnv
// 自动纳入。根因：selfcheck/wild 测试在 CI（无宿主 env）绿、在 Claude Code 会话内
// 读到真实会话 id 而红。
func isolateSessionIdentity(t *testing.T) {
	t.Helper()
	for _, h := range hostcap.Hosts {
		if h.ShellSessionEnv != "" {
			t.Setenv(h.ShellSessionEnv, "")
		}
	}
	t.Setenv("FORGE_SESSION_ID", "")
}

// TestIsolateSessionIdentityClearsEveryHostEnv pins the helper against the
// registry: with every host's identity env planted, isolation must leave the
// resolver with no session identity.
//
// TestIsolateSessionIdentityClearsEveryHostEnv 以注册表钉住本 helper：预埋所有
// 宿主身份 env 后，隔离必须让解析器拿不到任何会话身份。
func TestIsolateSessionIdentityClearsEveryHostEnv(t *testing.T) {
	planted := map[string]bool{}
	for _, h := range hostcap.Hosts {
		if h.ShellSessionEnv != "" {
			t.Setenv(h.ShellSessionEnv, "leaked-"+h.Name)
			planted[h.ShellSessionEnv] = true
		}
	}
	// 手写锚点：预埋与清空同源于注册表——若派生链被改空/改名，这里仍能红。
	// 新增注册表外的身份来源时，须同步 isolateSessionIdentity 与 TestMain。
	if !planted["CLAUDE_CODE_SESSION_ID"] {
		t.Fatalf("注册表派生的身份 env 集合应含 CLAUDE_CODE_SESSION_ID，got %v", planted)
	}
	t.Setenv("FORGE_SESSION_ID", "leaked-forge")
	if got := taskpipeline.CurrentSessionID(); got == "" {
		t.Fatal("预埋身份 env 后解析器应读到会话 id，前置不成立")
	}

	isolateSessionIdentity(t)

	if got := taskpipeline.CurrentSessionID(); got != "" {
		t.Errorf("隔离后 CurrentSessionID 应为空，got %q——有身份 env 未被清空", got)
	}
}
