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

func TestEnsureAgentsBlock_SeparatorIsOneBlankLine(t *testing.T) {
	cases := []struct {
		name  string
		human string
		want  string // 人工内容与块之间的分隔符
	}{
		{"no trailing newline", "x", "\n\n"},
		{"one newline", "x\n", "\n"},
		{"two newlines", "x\n\n", ""},
		{"crlf", "x\r\n", "\r\n"},
		{"crlf two", "x\r\n\r\n", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(tc.human), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := EnsureAgentsBlock(root); err != nil {
				t.Fatalf("EnsureAgentsBlock: %v", err)
			}
			text := readAgents(t, root)
			want := tc.human + tc.want + agentsBegin
			if !strings.HasPrefix(text, want) {
				t.Fatalf("分隔符不对：got %q, want 前缀 %q", text, want)
			}
		})
	}
}

func TestEnsureAgentsBlock_UpdateIsByteExact(t *testing.T) {
	root := t.TempDir()
	head := "# 头部\n\n人工前言\n\n"
	tail := "\n\n## 尾部\n\n人工附录\n"
	old := head + agentsBegin + "\n旧内容\n" + agentsEnd + tail
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatalf("EnsureAgentsBlock: %v", err)
	}
	got := readAgents(t, root)
	if !strings.HasPrefix(got, head+agentsBegin) {
		t.Fatalf("块前内容被改动：%q", got)
	}
	if !strings.HasSuffix(got, agentsEnd+tail) {
		t.Fatalf("块后内容被改动：%q", got)
	}
	if strings.Contains(got, "旧内容") {
		t.Fatalf("旧块内容未替换：%q", got)
	}
}

func TestEnsureAgentsBlock_RejectsReversedMarkers(t *testing.T) {
	root := t.TempDir()
	reversed := agentsEnd + "\n中间\n" + agentsBegin + "\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(reversed), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentsBlock(root); err == nil {
		t.Fatal("end 在 begin 之前应报错")
	}
	if got := readAgents(t, root); got != reversed {
		t.Fatalf("报错时不应改动文件：%q", got)
	}
}

func TestEnsureAgentsBlock_WhitespaceOnlyFileGetsTitle(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte("\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatalf("EnsureAgentsBlock: %v", err)
	}
	if got := readAgents(t, root); !strings.HasPrefix(got, "# AGENTS.md\n\n"+agentsBegin) {
		t.Fatalf("纯空白文件应整份重建带标题：%q", got)
	}
}

func TestEnsureAgentsBlock_CRLFStaysIdempotent(t *testing.T) {
	root := t.TempDir()
	crlf := "# hi\r\n\r\n" + agentsBegin + "\r\n旧\r\n" + agentsEnd + "\r\n\r\ntail\r\n"
	if err := os.WriteFile(filepath.Join(root, AgentsFile), []byte(crlf), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentsBlock(root); err != nil {
		t.Fatalf("首次: %v", err)
	}
	first := readAgents(t, root)
	action, err := EnsureAgentsBlock(root)
	if err != nil {
		t.Fatalf("二次: %v", err)
	}
	if action != AgentsUnchanged || readAgents(t, root) != first {
		t.Fatalf("CRLF 文件第二次应 unchanged 且不改文件，action=%q", action)
	}
	if !strings.HasPrefix(first, "# hi\r\n\r\n"+agentsBegin) || !strings.HasSuffix(first, agentsEnd+"\r\n\r\ntail\r\n") {
		t.Fatalf("CRLF 文件的标记外字节被改动：%q", first)
	}
}
