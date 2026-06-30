package taskpipeline

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestParseAcceptance 锁定 --accept 串解析：分隔符 " :: " 切 Run/Expected，无分隔符→
// Expected 空（只看退出码 0），两侧 trim。#3 的入口解析，断裂则验收标准进不了 TaskState。
func TestParseAcceptance(t *testing.T) {
	cases := []struct {
		in               string
		wantRun, wantExp string
	}{
		{`go test ./... :: PASS`, `go test ./...`, `PASS`},
		{`go version`, `go version`, ``},
		{`  echo hi :: hi  `, `echo hi`, `hi`},
		{`gofmt -l . ::`, `gofmt -l .`, ``}, // 尾部裸 :: → 空期望（只看退出码 0），:: 不漏进 Run
	}
	for _, c := range cases {
		got := ParseAcceptance([]string{c.in})
		if len(got) != 1 || got[0].Run != c.wantRun || got[0].Expected != c.wantExp {
			t.Errorf(`ParseAcceptance(%q) = {Run:%q Expected:%q}, want {%q %q}`,
				c.in, got[0].Run, got[0].Expected, c.wantRun, c.wantExp)
		}
	}
}

// TestVerifyAcceptance_RunsAndClassifies 端到端实跑：VerifyAcceptance 用真实 go 子命令
// 跑每条标准并按"退出码 + Expected 子串"分类。钉住四类结果——pass(含子串)/pass(空期望
// 退出码0)/fail(非0退出)/fail(子串缺失)——以及失败也回填 Output 供排查。
func TestVerifyAcceptance_RunsAndClassifies(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{Acceptance: ParseAcceptance([]string{
		`go version :: go version`,  // pass: exit 0 + 输出含 "go version"
		`go version ::`,             // pass: exit 0, 无期望子串
		`go forge-nope-nope ::`,     // fail: 非零退出
		`go version :: NONEXISTENT`, // fail: 退出 0 但期望子串缺失
	})}
	VerifyAcceptance(dir, state)

	if !state.Acceptance[0].Passed {
		t.Error(`criterion 0 (go version :: go version) should pass`)
	}
	if !state.Acceptance[1].Passed {
		t.Error(`criterion 1 (go version ::) should pass on exit 0`)
	}
	if state.Acceptance[2].Passed {
		t.Error(`criterion 2 (unknown subcommand) should fail (non-zero exit)`)
	}
	if state.Acceptance[3].Passed {
		t.Error(`criterion 3 (expected substring absent) should fail`)
	}
	if state.Acceptance[2].Output == `` {
		t.Error(`failing criterion should capture Output for traceability`)
	}
}

// TestTruncateAcceptanceOutput_ValidUTF8 钉住 P0 修复：多字节 UTF-8（中文）输出截断后
// 必须仍是有效 UTF-8。原字节切片把切点落在字符中间 → 无效 UTF-8 → json.Marshal 落盘
// 成 � 乱码，丢排查价值（本特性要的就是可追溯证据）。200 个"测"字=600 字节，必触发截断。
func TestTruncateAcceptanceOutput_ValidUTF8(t *testing.T) {
	s := strings.Repeat(`测`, 200) // 600 字节（3 字节/字），> 500 cap 必截断
	got := truncateAcceptanceOutput(s)
	if !utf8.ValidString(got) {
		t.Errorf(`截断后产出无效 UTF-8（落盘会乱码）: valid=false, got len=%d`, len(got))
	}
	if !strings.HasPrefix(got, `...(省略前部)...`) {
		t.Errorf(`截断串缺前缀: %q`, got)
	}
}

// TestTruncateAcceptanceOutput_ShortUnchanged 钉住短输出原样返回（不截断、不加前缀）。
func TestTruncateAcceptanceOutput_ShortUnchanged(t *testing.T) {
	s := `短输出`
	if got := truncateAcceptanceOutput(s); got != s {
		t.Errorf(`短输出应原样返回, got %q`, got)
	}
}
