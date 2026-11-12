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
	if !strings.Contains(text, "本段由 forge 维护") {
		t.Fatalf("托管块未自证身份：%q", text)
	}
	if !strings.HasPrefix(text, "# AGENTS.md\n\n") {
		t.Fatalf("新建文件应以标题开头（标记外），got %q", text)
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
	broken := agentsBegin + "\n只有开头\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureAgentsBlock(root); err == nil {
		t.Fatal("标记不成对应报错")
	}
	if got := readAgents(t, root); got != broken {
		t.Fatalf("报错时不应改动文件：%q", got)
	}
}

func TestEnsureAgentsBlock_RejectsMultipleBlocks(t *testing.T) {
	root := t.TempDir()
	two := agentsBegin + "\n甲\n" + agentsEnd + "\n\n" + agentsBegin + "\n乙\n" + agentsEnd + "\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(two), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureAgentsBlock(root); err == nil {
		t.Fatal("多对标记应报错，而不是只替换第一对")
	}
	if got := readAgents(t, root); got != two {
		t.Fatalf("报错时不应改动文件：%q", got)
	}
}

func TestEnsureAgentsBlock_AppendKeepsOriginalBytes(t *testing.T) {
	root := t.TempDir()
	human := "# 约定\n\n- 行一\n\n\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(human), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatalf("EnsureAgentsBlock: %v", err)
	}
	text := readAgents(t, root)
	if !strings.HasPrefix(text, human) {
		t.Fatalf("追加路径改动了原有字节：got %q, want 前缀 %q", text, human)
	}
	if !strings.Contains(text, agentsBegin) {
		t.Fatalf("追加后无托管块：%q", text)
	}
}
