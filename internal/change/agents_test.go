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

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s: %v", path, err)
	}
	return string(raw)
}

// 默认只维护已存在的入口文件：不替不用 Claude Code / Cursor 的仓库建文件。
func TestEnsureAgentPointers_SkipsMissingByDefault(t *testing.T) {
	root := t.TempDir()
	reports, err := EnsureAgentPointers(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 || !reports[0].Skipped || !reports[1].Skipped {
		t.Fatalf("没有入口文件时应全部跳过，got %+v", reports)
	}
	for _, p := range []string{ClaudeFile, CursorRuleFile} {
		if _, err := os.Stat(filepath.Join(root, p)); !os.IsNotExist(err) {
			t.Errorf("%s 不该被创建", p)
		}
	}
}

// 点名创建：CLAUDE.md 里放 @AGENTS.md 导入，Cursor 规则文件带 alwaysApply
// frontmatter；第二次运行不改文件。
func TestEnsureAgentPointers_CreateWhenRequestedAndIdempotent(t *testing.T) {
	root := t.TempDir()
	reports, err := EnsureAgentPointers(root, []string{"claude", "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		if r.Skipped || r.Action != AgentsCreated {
			t.Fatalf("应新建两个指针文件，got %+v", reports)
		}
	}

	claude := readFile(t, filepath.Join(root, ClaudeFile))
	if !strings.Contains(claude, "@AGENTS.md") || !strings.Contains(claude, pointerBegin) {
		t.Fatalf("CLAUDE.md 应含 @AGENTS.md 导入与托管标记：%q", claude)
	}
	cursor := readFile(t, filepath.Join(root, CursorRuleFile))
	if !strings.HasPrefix(cursor, "---\n") || !strings.Contains(cursor, "alwaysApply: true") {
		t.Fatalf("Cursor 规则缺 frontmatter：%q", cursor)
	}
	if !strings.Contains(cursor, "AGENTS.md") {
		t.Fatalf("Cursor 规则应指向 AGENTS.md：%q", cursor)
	}

	// 幂等：第二次不改字节。
	before := map[string]string{
		ClaudeFile:     claude,
		CursorRuleFile: cursor,
	}
	reports, err = EnsureAgentPointers(root, []string{"claude", "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range reports {
		if r.Action != AgentsUnchanged {
			t.Fatalf("第二次应 unchanged，got %+v", reports)
		}
		if got := readFile(t, filepath.Join(root, r.File)); got != before[r.File] {
			t.Errorf("%s 第二次运行时被改了", r.File)
		}
	}
}

// 已存在的入口文件保留人工内容：只追加/替换标记之间。
func TestEnsureAgentPointers_PreservesHumanContent(t *testing.T) {
	root := t.TempDir()
	human := "# 我的 CLAUDE.md\n\n- 先跑 make check\n"
	if err := os.WriteFile(filepath.Join(root, ClaudeFile), []byte(human), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentPointers(root, nil); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, filepath.Join(root, ClaudeFile))
	if !strings.HasPrefix(got, human) {
		t.Fatalf("人工内容必须原样保留，got %q", got)
	}
	if !strings.Contains(got, pointerBegin) || !strings.Contains(got, "@AGENTS.md") {
		t.Fatalf("应追加指针块，got %q", got)
	}

	// Cursor 目录存在（仓库已用 Cursor）时补 forge.mdc，不必点名。
	if err := os.MkdirAll(filepath.Join(root, ".cursor", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentPointers(root, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, CursorRuleFile)); err != nil {
		t.Fatalf("已有 .cursor/rules 时应补 pointer：%v", err)
	}
}

// 不成对的标记拒绝动手，也不改文件。
func TestEnsureAgentPointers_RejectsUnpairedMarkers(t *testing.T) {
	root := t.TempDir()
	body := "# CLAUDE.md\n\n" + pointerBegin + "\n缺收尾\n"
	if err := os.WriteFile(filepath.Join(root, ClaudeFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentPointers(root, []string{"claude"}); err == nil {
		t.Fatal("不成对标记应报错")
	}
	if got := readFile(t, filepath.Join(root, ClaudeFile)); got != body {
		t.Fatal("报错时不该改文件")
	}
}

func TestEnsureAgentPointers_AddsCursorFrontmatterToExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".cursor", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	plain := "## 手写规则\n\n- 先读 AGENTS.md\n"
	path := filepath.Join(root, CursorRuleFile)
	if err := os.WriteFile(path, []byte(plain), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentPointers(root, nil); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if !strings.HasPrefix(got, "---\n") || !strings.Contains(got, "alwaysApply: true") {
		t.Fatalf("已有文件缺 frontmatter 时要补在开头：%q", got)
	}
	if !strings.Contains(got, plain) {
		t.Fatalf("原有内容不能丢：%q", got)
	}
	again, err := EnsureAgentPointers(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got2 := readFile(t, path); got2 != got {
		t.Fatalf("二次运行应字节不变：\n旧 %q\n新 %q", got, got2)
	}
	if again[1].Action != AgentsUnchanged {
		t.Fatalf("二次运行应 unchanged，got %s", again[1].Action)
	}
}

func TestEnsureManagedBlock_PreservesCRLF(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ClaudeFile)
	if err := os.WriteFile(path, []byte("line1\r\nline2\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureAgentPointers(root, nil); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, path)
	if strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatalf("CRLF 文件里混进了 LF：%q", got)
	}
	if !strings.Contains(got, pointerBegin) {
		t.Fatalf("应追加指针块：%q", got)
	}
}

func TestInit_ReportsCreatedOnPointerFailure(t *testing.T) {
	root := t.TempDir()
	// .cursor/rules/ 存在会触发补 forge.mdc；把目标做成目录让写入失败。
	if err := os.MkdirAll(filepath.Join(root, CursorRuleFile), 0o755); err != nil {
		t.Fatal(err)
	}
	created, err := Init(root, "mini")
	if err == nil {
		t.Fatal("写入失败应报错")
	}
	joined := strings.Join(created, "\n")
	if !strings.Contains(joined, "spec/forge.yaml") {
		t.Fatalf("失败前的落盘要能看见，got %v", created)
	}
}
