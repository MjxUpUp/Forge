package taskpipeline

import (
	"os/exec"
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

// TestParseAcceptanceFromPlan 锁定从 Plan markdown 提取验收标准：行扫描 Run:/Expected:
// 配对，合并成 `run :: expected` 喂 parseOneAcceptance 复用 :: 边界处理。覆盖集中块/Task
// 内联多块/无块/裸 Run/孤立 Expected/连续 Run——任一断裂则 --plan-file 自动提取失效，
// acceptance 维度继续空转（dogfood 实证：本项目 28 条任务结论 acceptance 全 0/0）。
func TestParseAcceptanceFromPlan(t *testing.T) {
	cases := []struct {
		name string
		plan string
		want []AcceptanceCriterion
	}{
		{
			name: `集中验收块`,
			plan: "## 验收标准\nRun: cargo test --test integration\nExpected: PASS\n",
			want: []AcceptanceCriterion{{Run: `cargo test --test integration`, Expected: `PASS`}},
		},
		{
			name: `Task 内联多块（全文扫描）`,
			plan: "Task 1:\nRun: go build ./...\nExpected: \nTask 2:\nRun: go vet ./...\nExpected: no issues\n",
			want: []AcceptanceCriterion{
				{Run: `go build ./...`, Expected: ``},
				{Run: `go vet ./...`, Expected: `no issues`},
			},
		},
		{
			name: `无验收块返空`,
			plan: "## 计划\n只讲做什么，没有 Run/Expected 行\n",
			want: nil,
		},
		{
			name: `裸 Run 无 Expected（只看退出码 0）`,
			plan: "Run: gofmt -l .\n",
			want: []AcceptanceCriterion{{Run: `gofmt -l .`, Expected: ``}},
		},
		{
			name: `孤立 Expected 前无 Run 丢弃`,
			plan: "Expected: 孤儿\nRun: go test ./...\nExpected: ok\n",
			want: []AcceptanceCriterion{{Run: `go test ./...`, Expected: `ok`}},
		},
		{
			name: `连续两 Run 中间无 Expected（前者裸落盘）`,
			plan: "Run: cmd-a\nRun: cmd-b\nExpected: out-b\n",
			want: []AcceptanceCriterion{
				{Run: `cmd-a`, Expected: ``},
				{Run: `cmd-b`, Expected: `out-b`},
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseAcceptanceFromPlan(c.plan)
			if len(got) != len(c.want) {
				t.Fatalf(`提取条数 %d, want %d (got=%v)`, len(got), len(c.want), got)
			}
			for i := range got {
				if got[i].Run != c.want[i].Run || got[i].Expected != c.want[i].Expected {
					t.Errorf(`[%d] = {Run:%q Expected:%q}, want {%q %q}`,
						i, got[i].Run, got[i].Expected, c.want[i].Run, c.want[i].Expected)
				}
			}
		})
	}
}

// TestMergeAcceptance 锁定显式 --accept 优先、plan 提取按 Run 去重补充。
// 共存时显式条目表达覆盖/微调应胜出，plan 只补未冲突的 Run。
func TestMergeAcceptance(t *testing.T) {
	base := []AcceptanceCriterion{{Run: `a`, Expected: `1`}, {Run: `b`, Expected: `2`}}
	addition := []AcceptanceCriterion{
		{Run: `b`, Expected: `override`}, // Run 冲突 → 丢弃（base 优先）
		{Run: `c`, Expected: `3`},        // 新 Run → 补充
	}
	got := MergeAcceptance(base, addition)
	want := []AcceptanceCriterion{
		{Run: `a`, Expected: `1`},
		{Run: `b`, Expected: `2`}, // 保留 base，未被 override 覆盖
		{Run: `c`, Expected: `3`},
	}
	if len(got) != len(want) {
		t.Fatalf(`merge 条数 %d, want %d (got=%v)`, len(got), len(want), got)
	}
	for i := range want {
		if got[i].Run != want[i].Run || got[i].Expected != want[i].Expected {
			t.Errorf(`[%d] = {Run:%q Expected:%q}, want {%q %q}`,
				i, got[i].Run, got[i].Expected, want[i].Run, want[i].Expected)
		}
	}
}

// TestVerifyAcceptance_RecordsAcceptedHeadCommit 钉住 AcceptedHeadCommit 回填语义：proof v2
// 快路径（AcceptedHeadCommit == 当前 HEAD 判 Passed fresh）依赖 VerifyAcceptance 实跑时记此
// 快照。git 仓库下须 == git rev-parse --short HEAD。
func TestVerifyAcceptance_RecordsAcceptedHeadCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	runGit(t, dir, "commit", "--allow-empty", "-m", "initial")

	want, err := exec.Command("git", "-C", dir, "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	wantShort := strings.TrimSpace(string(want))

	state := &TaskState{Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	VerifyAcceptance(dir, state)

	if got := state.Acceptance[0].AcceptedHeadCommit; got != wantShort {
		t.Errorf("AcceptedHeadCommit = %q, want %q (HEAD short)", got, wantShort)
	}
}

// TestVerifyAcceptance_AcceptedHeadCommit_NonGitEmpty 钉住非 git 目录的退化：GetHeadCommit
// 失败返 ""（不 panic），proof v1 重跑兜底靠此空值判定走重跑。t.TempDir() 非 git 仓库。
func TestVerifyAcceptance_AcceptedHeadCommit_NonGitEmpty(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	VerifyAcceptance(dir, state)
	if got := state.Acceptance[0].AcceptedHeadCommit; got != "" {
		t.Errorf("非 git 目录 AcceptedHeadCommit 应为空（GetHeadCommit 失败返），got %q", got)
	}
}
