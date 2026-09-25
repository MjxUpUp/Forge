package taskpipeline

// assertion_judge_test.go — spec-as-gate v2 判定分派（L2 P1）的测试面：
// 五型断言 × 通过/失败的分派表、v1 行为逐字节一致钉、Run-less file-* 跳执行、
// 退出码底座、声明期校验（含叙述性拒绝）、--assert 绑定规则、--accept-file
// YAML 解析、verify-acceptance 的逐断言 verdict。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestJudgeAssertion_Dispatch 钉住五型分派的单一真相源语义：每型给命中/不命中
// 两例；未知类型 fail-closed 判负（声明期已拒，判定绝不猜测）。
func TestJudgeAssertion_Dispatch(t *testing.T) {
	ctx := judgeContext{
		ran:      true,
		exitCode: 0,
		output:   "PASS\nok  demo 0.1s",
		changed:  []string{"internal/a.go", "docs/adr/0001.md"},
	}
	cases := []struct {
		name string
		a    Assertion
		want bool
	}{
		{"exit 0 hit", Assertion{Type: tasktypes.AssertionTypeExit, Expected: "0"}, true},
		{"exit 0 default", Assertion{Type: tasktypes.AssertionTypeExit}, true},
		{"exit 1 miss", Assertion{Type: tasktypes.AssertionTypeExit, Expected: "1"}, false},
		{"exit non-numeric fail-closed", Assertion{Type: tasktypes.AssertionTypeExit, Expected: "零"}, false},
		{"exit without run", Assertion{Type: tasktypes.AssertionTypeExit, Expected: "0"}, false},
		{"contains hit", Assertion{Type: tasktypes.AssertionTypeContains, Expected: "PASS"}, true},
		{"contains miss", Assertion{Type: tasktypes.AssertionTypeContains, Expected: "NONEXISTENT"}, false},
		{"contains CJK substring", Assertion{Type: tasktypes.AssertionTypeContains, Expected: `验收`}, false}, // output 无中文 → miss
		{"not-contains clean", Assertion{Type: tasktypes.AssertionTypeNotContains, Expected: "t.Skip"}, true},
		{"not-contains cheat hit", Assertion{Type: tasktypes.AssertionTypeNotContains, Expected: "PASS"}, false},
		{"file-changed glob hit", Assertion{Type: tasktypes.AssertionTypeFileChanged, Arg: "internal/*.go"}, true},
		{"file-changed dir hit", Assertion{Type: tasktypes.AssertionTypeFileChanged, Arg: "docs/adr"}, true},
		{"file-changed miss", Assertion{Type: tasktypes.AssertionTypeFileChanged, Arg: "internal/freeze/**"}, false},
		{"file-untouched protected", Assertion{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"}, true},
		{"file-untouched violated", Assertion{Type: tasktypes.AssertionTypeFileUntouched, Arg: "docs/adr/**"}, false},
		{"file-untouched non-source covered", Assertion{Type: tasktypes.AssertionTypeFileUntouched, Arg: "docs/adr"}, false}, // .md 在变更集内可被目录前缀命中——保护语义覆盖非源码（changedSourceFilesSince 的盲区）
		{"unknown type fail-closed", Assertion{Type: "regex", Arg: "x"}, false},
	}
	for _, tc := range cases {
		c := tc
		t.Run(c.name, func(t *testing.T) {
			jctx := ctx
			if c.name == "exit without run" {
				jctx.ran = false
			}
			if got := judgeAssertion(c.a, jctx); got != c.want {
				t.Errorf("judgeAssertion(%+v) = %v, want %v", c.a, got, c.want)
			}
		})
	}
}

// TestRunAndJudgeCriterion_V1ByteIdentical 钉住 v1 兼容：无断言的条目判定行为与
// 旧实现（RunTestCommand bool + judgeAcceptance）逐字节一致——P0 承诺在分派落地后依然成立。
func TestRunAndJudgeCriterion_V1ByteIdentical(t *testing.T) {
	dir := t.TempDir()
	cases := ParseAcceptance([]string{
		`go version :: go version`,  // pass: exit 0 + 输出含 "go version"
		`go version ::`,             // pass: exit 0, 无期望子串
		`go forge-nope-nope ::`,     // fail: 非零退出
		`go version :: NONEXISTENT`, // fail: 退出 0 但期望子串缺失
	})
	want := []bool{true, true, false, false}
	for i := range cases {
		verdicts := runAndJudgeCriterion(dir, &cases[i], nil)
		if len(verdicts) != 0 {
			t.Errorf(`criterion %d：v1 条目不应产出断言 verdict，got %d`, i, len(verdicts))
		}
		if cases[i].Passed != want[i] {
			t.Errorf(`criterion %d (%s :: %s) Passed = %v, want %v`, i, cases[i].Run, cases[i].Expected, cases[i].Passed, want[i])
		}
	}
	if cases[2].Output == `` {
		t.Error(`失败条目应保留 Output 供排查`)
	}
}

// TestRunAndJudgeCriterion_V2 钉住 v2 组合语义：断言 AND 进 criterion 级 Passed；
// not-contains 命中（反作弊形态）单独就能把整条判负；纯 file-* 的 Run-less 条目
// 跳过命令执行（无命令可跑）。
func TestRunAndJudgeCriterion_V2(t *testing.T) {
	dir := t.TempDir()

	// v1 判定过 + 断言全过 → Passed
	ok := AcceptanceCriterion{Run: `go version`, Assertions: []Assertion{
		{Type: tasktypes.AssertionTypeContains, Expected: "go version"},
		{Type: tasktypes.AssertionTypeExit, Expected: "0"},
	}}
	runAndJudgeCriterion(dir, &ok, nil)
	if !ok.Passed {
		t.Errorf(`v1 过 + 断言全过应 Passed，got %+v`, ok)
	}

	// v1 判定过 + not-contains 命中 → 判负（反作弊复合断言）
	cheat := AcceptanceCriterion{Run: `go version`, Assertions: []Assertion{
		{Type: tasktypes.AssertionTypeNotContains, Expected: "go version"},
	}}
	verdicts := runAndJudgeCriterion(dir, &cheat, nil)
	if cheat.Passed {
		t.Errorf(`not-contains 命中应把整条判负，got %+v`, cheat)
	}
	if len(verdicts) != 1 || verdicts[0].Passed {
		t.Errorf(`应产出一例 fail verdict，got %+v`, verdicts)
	}

	// Run-less 纯 file-*：不执行命令（Output 空），断言按变更集判定
	protect := AcceptanceCriterion{Assertions: []Assertion{
		{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"},
	}}
	runAndJudgeCriterion(dir, &protect, []string{"internal/a.go"})
	if !protect.Passed {
		t.Errorf(`保护 glob 未被触碰应过，got %+v`, protect)
	}
	if protect.Output != `` {
		t.Errorf(`Run-less 条目不应有命令输出，got %q`, protect.Output)
	}

	violated := AcceptanceCriterion{Assertions: []Assertion{
		{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"},
	}}
	runAndJudgeCriterion(dir, &violated, []string{"internal/freeze/freeze.go"})
	if violated.Passed {
		t.Errorf(`保护 glob 被触碰（freeze 语义的任务级版）应判负，got %+v`, violated)
	}
}

// TestRunTestCommandCode 钉住执行底座：真实退出码（0 / 非 0）、spawn 失败 -1、
// 空命令 -1；RunTestCommand 是 code==0 的等价包装。
func TestRunTestCommandCode(t *testing.T) {
	dir := t.TempDir()
	if code, out := RunTestCommandCode(dir, `go version`); code != 0 || !strings.Contains(out, "go version") {
		t.Errorf(`go version = (%d, %q), want (0, 含 "go version")`, code, out)
	}
	if code, _ := RunTestCommandCode(dir, `go forge-nope-nope-subcommand`); code == 0 {
		t.Errorf(`未知子命令应非 0 退出码，got %d`, code)
	}
	if code, out := RunTestCommandCode(dir, `forge-binary-that-does-not-exist`); code != -1 {
		t.Errorf(`spawn 失败应 -1，got %d（out=%q）`, code, out)
	}
	if code, out := RunTestCommandCode(dir, ``); code != -1 || out != "empty command" {
		t.Errorf(`空命令 = (%d, %q), want (-1, "empty command")`, code, out)
	}
	// 包装等价：passed == (code == 0)，输出逐字节一致
	p1, o1 := RunTestCommand(dir, `go version`)
	c1, out1 := RunTestCommandCode(dir, `go version`)
	if p1 != (c1 == 0) || o1 != out1 {
		t.Errorf(`RunTestCommand/Code 不一致: (%v,%q) vs (%d,%q)`, p1, o1, c1, out1)
	}
}

// TestParseAssertion 钉住 `type:arg :: expected` 声明串解析的形状规则。
func TestParseAssertion(t *testing.T) {
	a, err := ParseAssertion(`not-contains: :: t.Skip`)
	if err != nil || a.Type != `not-contains` || a.Arg != `` || a.Expected != `t.Skip` {
		t.Errorf(`got (%+v, %v)`, a, err)
	}
	a, err = ParseAssertion(`file-untouched:internal/freeze/**`)
	if err != nil || a.Type != `file-untouched` || a.Arg != `internal/freeze/**` || a.Expected != `` {
		t.Errorf(`无 :: 的裸声明：got (%+v, %v)`, a, err)
	}
	a, err = ParseAssertion(`exit: :: 1`)
	if err != nil || a.Type != `exit` || a.Expected != `1` {
		t.Errorf(`got (%+v, %v)`, a, err)
	}
	if _, err := ParseAssertion(` :: whatever`); err == nil {
		t.Error(`缺类型应报错`)
	}
}

// TestValidateAssertion 钉住单条断言声明门：类型枚举、Negate 预留拒绝、
// arg/expected 的型别规则、file-* glob 的叙述性拒绝（CJK 启发式，与 --invariant 同规）。
func TestValidateAssertion(t *testing.T) {
	valid := []Assertion{
		{Type: tasktypes.AssertionTypeExit, Expected: "0"},
		{Type: tasktypes.AssertionTypeContains, Expected: "PASS"},
		{Type: tasktypes.AssertionTypeNotContains, Expected: "t.Skip"},
		{Type: tasktypes.AssertionTypeFileChanged, Arg: "docs/adr/*"},
		{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"},
		{Type: tasktypes.AssertionTypeContains, Expected: `验收通过`}, // 中文子串合法（匹配中文输出）
	}
	for _, a := range valid {
		if err := ValidateAssertion(a); err != nil {
			t.Errorf(`%+v 应合法，got %v`, a, err)
		}
	}
	invalid := []Assertion{
		{Type: "regex", Arg: "^a+$"},                                                        // 未支持类型（ReDoS 宪法）
		{Type: tasktypes.AssertionTypeContains, Expected: "x", Negate: true},                // Negate 预留
		{Type: tasktypes.AssertionTypeExit, Expected: "0", Arg: "junk"},                     // exit 无 arg
		{Type: tasktypes.AssertionTypeExit, Expected: "zero"},                               // 非数字退出码
		{Type: tasktypes.AssertionTypeExit},                                                 // 缺期望码
		{Type: tasktypes.AssertionTypeContains},                                             // 缺期望子串
		{Type: tasktypes.AssertionTypeContains, Expected: "x", Arg: "anchor"},               // contains 无 arg
		{Type: tasktypes.AssertionTypeFileChanged},                                          // 缺 glob
		{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/**", Expected: "never"}, // file-* 无 expected
		{Type: tasktypes.AssertionTypeFileChanged, Arg: `把所有文件都改掉再加一行`},                     // 叙述性 glob（CJK 主导）
	}
	for _, a := range invalid {
		if err := ValidateAssertion(a); err == nil {
			t.Errorf(`%+v 应被声明期拒绝`, a)
		}
	}
}

// TestValidateAssertions 钉住 criterion 级绑定规则：无 Run 只能挂 file-*；
// 消费输出的断言必须有 Run；空条目与无 Run 带 expected 均拒绝。
func TestValidateAssertions(t *testing.T) {
	if err := ValidateAssertions([]AcceptanceCriterion{
		{Run: `go test ./...`, Assertions: []Assertion{{Type: tasktypes.AssertionTypeContains, Expected: "PASS"}}},
		{Assertions: []Assertion{{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"}}},
	}); err != nil {
		t.Errorf(`合法组合被拒: %v`, err)
	}
	bad := [][]AcceptanceCriterion{
		{{Assertions: []Assertion{{Type: tasktypes.AssertionTypeContains, Expected: "x"}}}}, // 无 Run 挂输出断言
		{{Run: ``, Assertions: nil}}, // 空条目
		{{Expected: `ok`}},           // 无 Run 无断言却带 expected
		{{Run: `go test ./...`, Assertions: []Assertion{{Type: "bogus", Expected: "x"}}}}, // 断言本身非法
	}
	for i, cs := range bad {
		if err := ValidateAssertions(cs); err == nil {
			t.Errorf(`非法组合 %d 应被拒绝（got nil）`, i)
		}
	}
}

// TestAttachAssertions 钉住 --assert 绑定规则：非 file-* 挂 preceding 条目；
// file-* 有条目挂尾条、无条目自建 Run-less 条目；输出型断言无宿主报错。
func TestAttachAssertions(t *testing.T) {
	base := []AcceptanceCriterion{{Run: `go test ./...`}}
	got, err := AttachAssertions(base, []Assertion{
		{Type: tasktypes.AssertionTypeContains, Expected: "PASS"},
		{Type: tasktypes.AssertionTypeNotContains, Expected: "t.Skip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].Assertions) != 2 {
		t.Errorf(`两条输出断言应都挂到 preceding 条目，got %+v`, got)
	}

	got, err = AttachAssertions(nil, []Assertion{{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Run != `` || len(got[0].Assertions) != 1 {
		t.Errorf(`file-* 无宿主应自建 Run-less 条目，got %+v`, got)
	}

	if _, err := AttachAssertions(nil, []Assertion{{Type: tasktypes.AssertionTypeExit, Expected: "0"}}); err == nil {
		t.Error(`输出型断言无宿主应报错`)
	}
}

// TestParseAcceptanceYAML 钉住 --accept-file 的 YAML schema 与 round-trip。
func TestParseAcceptanceYAML(t *testing.T) {
	doc := `criteria:
  - run: go test ./...
    expected: PASS
    assertions:
      - type: not-contains
        expected: t.Skip
  - assertions:
      - type: file-untouched
        arg: internal/freeze/**
`
	cs, err := ParseAcceptanceYAML([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf(`应解析 2 条，got %d`, len(cs))
	}
	if cs[0].Run != `go test ./...` || cs[0].Expected != `PASS` || len(cs[0].Assertions) != 1 ||
		cs[0].Assertions[0].Type != `not-contains` || cs[0].Assertions[0].Expected != `t.Skip` {
		t.Errorf(`[0] = %+v`, cs[0])
	}
	if cs[1].Run != `` || cs[1].Assertions[0].Arg != `internal/freeze/**` {
		t.Errorf(`[1] = %+v`, cs[1])
	}
	if _, err := ParseAcceptanceYAML([]byte("criteria: [}")); err == nil {
		t.Error(`坏 YAML 应报错`)
	}
}

// TestEnsureGoTestVerbose_OutputAssertion 钉住 L2 P1 的 -v 收紧：Expected 空但
// 断言集含输出消费型（contains/not-contains）的 go test 条目也要补 -v；纯 file-*
// 与退出码条目不动。
func TestEnsureGoTestVerbose_OutputAssertion(t *testing.T) {
	cs := []AcceptanceCriterion{
		{Run: `go test ./...`, Assertions: []Assertion{{Type: tasktypes.AssertionTypeContains, Expected: "PASS"}}},
		{Run: `go test ./...`, Assertions: []Assertion{{Type: tasktypes.AssertionTypeFileUntouched, Arg: "x/**"}}},
		{Run: `go test ./...`, Assertions: []Assertion{{Type: tasktypes.AssertionTypeExit, Expected: "0"}}},
	}
	adjusted := EnsureGoTestVerbose(cs)
	if len(adjusted) != 1 {
		t.Fatalf(`仅输出消费型应补 -v，got %d 条改写 (%v)`, len(adjusted), adjusted)
	}
	if cs[0].Run != `go test -v ./...` {
		t.Errorf(`[0] Run = %q, want "go test -v ./..."`, cs[0].Run)
	}
	if cs[1].Run != `go test ./...` || cs[2].Run != `go test ./...` {
		t.Errorf(`file-*/exit 型不应改写，got %q / %q`, cs[1].Run, cs[2].Run)
	}
}

// TestVerifyAcceptanceWithVerdicts_GitFileAssertions 端到端：git 仓库里改了
// internal/a.go、没碰 internal/freeze/——file-changed 命中、file-untouched 保护
// 成立；verdict 的 CriterionIdx 对齐条目下标（证据行按此定位）。
func TestVerifyAcceptanceWithVerdicts_GitFileAssertions(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "internal_a_base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "--allow-empty", "-m", "initial")

	state := &TaskState{TaskRef: "feat/assert-e2e", Acceptance: []AcceptanceCriterion{
		{Run: `go version`, Assertions: []Assertion{{Type: tasktypes.AssertionTypeExit, Expected: "0"}}},
		{Assertions: []Assertion{{Type: tasktypes.AssertionTypeFileChanged, Arg: "internal/*.go"}}},
		{Assertions: []Assertion{{Type: tasktypes.AssertionTypeFileUntouched, Arg: "internal/freeze/**"}}},
	}}
	// 工作树改动：internal/a.go（untracked 新文件）——任务窗口的变更来源之一。
	if err := os.MkdirAll(filepath.Join(dir, "internal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "a.go"), []byte("package internal\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	verdicts := VerifyAcceptanceWithVerdicts(dir, state)
	if !state.Acceptance[0].Passed {
		t.Errorf(`[0] go version + exit:0 应过，got %+v`, state.Acceptance[0])
	}
	if !state.Acceptance[1].Passed {
		t.Errorf(`[1] file-changed internal/*.go 应命中 untracked 新文件，got %+v`, state.Acceptance[1])
	}
	if !state.Acceptance[2].Passed {
		t.Errorf(`[2] file-untouched internal/freeze/** 应保护成立，got %+v`, state.Acceptance[2])
	}
	if len(verdicts) != 3 {
		t.Fatalf(`应产出 3 个 verdict，got %d (%+v)`, len(verdicts), verdicts)
	}
	for i, v := range verdicts {
		if v.CriterionIdx != i {
			t.Errorf(`verdict[%d].CriterionIdx = %d, want %d`, i, v.CriterionIdx, i)
		}
	}
}

// TestJudgeAssertion_ExitNonZeroHit 钉住 exit 断言的正例（审查 P3-7）：非零退出码
// 命中期望值判真——把比较回归成恒比 0 时本测试炸。
func TestJudgeAssertion_ExitNonZeroHit(t *testing.T) {
	ctx := judgeContext{ran: true, exitCode: 1, output: "boom"}
	if !judgeAssertion(Assertion{Type: tasktypes.AssertionTypeExit, Expected: "1"}, ctx) {
		t.Error(`exitCode=1 且期望 1 应判真`)
	}
	if judgeAssertion(Assertion{Type: tasktypes.AssertionTypeExit, Expected: "0"}, ctx) {
		t.Error(`exitCode=1 且期望 0 应判假`)
	}
}

// TestChangedMatches_Globstar 钉住尾缀 "/**" 的递归翻译（审查 P2-1）：path.Match
// 无 globstar，未翻译时 internal/freeze/** 静默只护一层——嵌套子目录必须被覆盖。
func TestChangedMatches_Globstar(t *testing.T) {
	changed := []string{
		"internal/freeze/freeze.go",          // 顶层
		"internal/freeze/sub/deep/freeze.go", // 嵌套两层
		"internal/other/x.go",
	}
	if !changedMatches(changed, "internal/freeze/**") {
		t.Error(`尾缀 /** 应覆盖嵌套子目录（递归翻译），单层 path.Match 是静默弱保护`)
	}
	if changedMatches(changed, "internal/other/**") == false {
		t.Error(`internal/other/** 应命中 internal/other/x.go`)
	}
	if changedMatches(changed, "internal/nothere/**") {
		t.Error(`未触碰目录不应命中`)
	}
	// 输出消费型断言在未执行上下文 fail-closed（审查 P3-4）。
	if judgeAssertion(Assertion{Type: tasktypes.AssertionTypeNotContains, Expected: "x"}, judgeContext{}) {
		t.Error(`not-contains 在 ran=false 的上下文应 fail-closed 判负`)
	}
}

// TestValidateAssertion_GlobstarAndExitRange 钉住声明期新门：路径中间 globstar
// 拒绝（P2-1）、exit 期望超 0-255 拒绝、负值哨兵拒绝（P3-5）。
func TestValidateAssertion_GlobstarAndExitRange(t *testing.T) {
	if err := ValidateAssertion(Assertion{Type: tasktypes.AssertionTypeFileChanged, Arg: "internal/**/x.go"}); err == nil {
		t.Error(`路径中间 globstar 应被声明期拒绝`)
	}
	if err := ValidateAssertion(Assertion{Type: tasktypes.AssertionTypeFileChanged, Arg: "internal/freeze/**"}); err != nil {
		t.Errorf(`尾缀 /** 应合法: %v`, err)
	}
	if err := ValidateAssertion(Assertion{Type: tasktypes.AssertionTypeExit, Expected: "-1"}); err == nil {
		t.Error(`exit 期望 -1（执行层失败哨兵）应被拒绝`)
	}
	if err := ValidateAssertion(Assertion{Type: tasktypes.AssertionTypeExit, Expected: "256"}); err == nil {
		t.Error(`exit 期望 256 超退出码域应被拒绝`)
	}
}

// TestRunAndJudgeCriterion_EmptyRunV1Edge 钉住空 Run 边角的 v1 逐字节保留
// （审查 P2-2）：旧 RunTestCommand("") → (false,"empty command") 恒负，v2 跳过
// 执行的路径不得把它翻成 vacuous pass。
func TestRunAndJudgeCriterion_EmptyRunV1Edge(t *testing.T) {
	c := AcceptanceCriterion{}
	runAndJudgeCriterion(t.TempDir(), &c, nil)
	if c.Passed || c.Output != "empty command" {
		t.Errorf(`空 Run 无断言应保持 v1 语义（false/"empty command"），got (%v,%q)`, c.Passed, c.Output)
	}
}

// TestParseAcceptanceYAML_UnknownKeyRejected 钉住 KnownFields 严格模式（审查
// P3-6）：拼错的键在声明期报错，不得静默丢断言集。
func TestParseAcceptanceYAML_UnknownKeyRejected(t *testing.T) {
	bad := "criteria:\n  - run: go version\n    assertion: [{type: exit, expected: \"0\"}]\n"
	if _, err := ParseAcceptanceYAML([]byte(bad)); err == nil {
		t.Error(`拼错键 assertions→assertion 应被严格模式拒绝（否则断言集静默消失）`)
	}
}

// TestRunAndJudgeCriterion_ExitTakeover 钉住 exit 断言接管（L3 精化）：期望失败
// 形态（退出码 1 + exit: :: 1）整条通过——隐式 exit==0 被显式断言取代。
func TestRunAndJudgeCriterion_ExitTakeover(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fail.sh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := AcceptanceCriterion{Run: `sh fail.sh`, Assertions: []Assertion{
		{Type: tasktypes.AssertionTypeExit, Expected: "1"},
	}}
	runAndJudgeCriterion(dir, &c, nil)
	if !c.Passed {
		t.Errorf(`exit:1 接管后退出 1 应整条通过（期望失败形态），got %+v`, c)
	}
	// 期望 0 实际 1：仍判负（mismatch 方向 fail-closed）。
	c2 := AcceptanceCriterion{Run: `sh fail.sh`, Assertions: []Assertion{
		{Type: tasktypes.AssertionTypeExit, Expected: "0"},
	}}
	runAndJudgeCriterion(dir, &c2, nil)
	if c2.Passed {
		t.Errorf(`exit:0 遇退出 1 应判负，got %+v`, c2)
	}
}
