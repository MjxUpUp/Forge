package change

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readAgents(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, AgentsFile))
	if err != nil {
		t.Fatalf("读 AGENTS.md: %v", err)
	}
	return string(raw)
}

func TestEnsureAgentsBlock_CreatesFile(t *testing.T) {
	root := t.TempDir()

	action, err := EnsureAgentsBlock(root)
	if err != nil {
		t.Fatalf("EnsureAgentsBlock: %v", err)
	}
	if action != AgentsCreated {
		t.Fatalf("action = %q, want %q", action, AgentsCreated)
	}

	text := readAgents(t, root)
	if !strings.Contains(text, agentsBegin) || !strings.Contains(text, agentsEnd) {
		t.Fatalf("托管块标记缺失：%q", text)
	}
	if !strings.Contains(text, "forge change status") {
		t.Fatalf("协议正文缺失：%q", text)
	}
}

func TestEnsureAgentsBlock_PreservesHumanContent(t *testing.T) {
	root := t.TempDir()
	human := "# 本仓库约定\n\n- 提交前跑 make check\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(human), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := EnsureAgentsBlock(root)
	if err != nil {
		t.Fatalf("EnsureAgentsBlock: %v", err)
	}
	if action != AgentsAppended {
		t.Fatalf("action = %q, want %q", action, AgentsAppended)
	}

	text := readAgents(t, root)
	if !strings.HasPrefix(text, human) {
		t.Fatalf("人工内容被改动：%q", text)
	}
	if !strings.Contains(text, agentsBegin) {
		t.Fatalf("追加后仍无托管块：%q", text)
	}
}

func TestEnsureAgentsBlock_UpdatesBlockOnly(t *testing.T) {
	root := t.TempDir()
	before := "# 本仓库约定\n\n" + agentsBegin + "\n旧协议\n" + agentsEnd + "\n\n## 人工附录\n\n保留这段\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}

	action, err := EnsureAgentsBlock(root)
	if err != nil {
		t.Fatalf("EnsureAgentsBlock: %v", err)
	}
	if action != AgentsUpdated {
		t.Fatalf("action = %q, want %q", action, AgentsUpdated)
	}

	text := readAgents(t, root)
	if strings.Contains(text, "旧协议") {
		t.Fatalf("旧托管块未替换：%q", text)
	}
	if !strings.HasPrefix(text, "# 本仓库约定\n") || !strings.Contains(text, "## 人工附录") {
		t.Fatalf("标记外内容被改动：%q", text)
	}
}

func TestEnsureAgentsBlock_Idempotent(t *testing.T) {
	root := t.TempDir()
	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatalf("首次: %v", err)
	}
	first := readAgents(t, root)

	action, err := EnsureAgentsBlock(root)
	if err != nil {
		t.Fatalf("二次: %v", err)
	}
	if action != AgentsUnchanged {
		t.Fatalf("二次 action = %q, want %q", action, AgentsUnchanged)
	}
	if got := readAgents(t, root); got != first {
		t.Fatalf("二次写入改动了文件")
	}
}

func TestEnsureAgentsBlock_RejectsUnpairedMarkers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(agentsBegin+"\n只有开头\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureAgentsBlock(root); err == nil {
		t.Fatal("标记不成对应报错")
	}
}
