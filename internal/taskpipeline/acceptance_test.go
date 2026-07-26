package taskpipeline

import (
	"os/exec"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestParseAcceptance locks down --accept string parsing: the ` :: ` separator splits Run/Expected, no separator →
// Expected empty (only exit code 0 matters), both sides trimmed. Entry parsing for #3; if broken, acceptance criteria never reach TaskState.
//
// TestParseAcceptance 锁定 --accept 串解析：分隔符" :: "切 Run/Expected，无分隔符→
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

// TestVerifyAcceptance_RunsAndClassifies end-to-end real run: VerifyAcceptance uses real go subcommands
// to run each criterion and classifies by `exit code + Expected substring`. Pins four outcome categories — pass (substring matched)/pass (empty expectation,
// exit code 0)/fail (non-zero exit)/fail (substring missing) — and on failure backfills Output for traceability.
//
// TestVerifyAcceptance_RunsAndClassifies 端到端实跑：VerifyAcceptance 用真实 go 子命令
// 跑每条标准并按「退出码 + Expected 子串」分类。钉住四类结果——pass(含子串)/pass(空期望
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

// TestTruncateAcceptanceOutput_ValidUTF8 pins the P0 fix: multi-byte UTF-8 (Chinese) output after truncation
// must still be valid UTF-8. The original byte slice landed the cut point mid-character → invalid UTF-8 → json.Marshal on disk
// becomes � garbage, losing traceability value (this feature is exactly for traceable evidence). 200 `测` characters = 600 bytes, must trigger truncation.
//
// TestTruncateAcceptanceOutput_ValidUTF8 钉住 P0 修复：多字节 UTF-8（中文）输出截断后
// 必须仍是有效 UTF-8。原字节切片把切点落在字符中间 → 无效 UTF-8 → json.Marshal 落盘
// 成 � 乱码，丢排查价值（本特性要的就是可追溯证据）。200 个「测」字=600 字节，必触发截断。
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

// TestTruncateAcceptanceOutput_ShortUnchanged pins that short output is returned as-is (no truncation, no prefix added).
//
// TestTruncateAcceptanceOutput_ShortUnchanged 钉住短输出原样返回（不截断、不加前缀）。
func TestTruncateAcceptanceOutput_ShortUnchanged(t *testing.T) {
	s := `短输出`
	if got := truncateAcceptanceOutput(s); got != s {
		t.Errorf(`短输出应原样返回, got %q`, got)
	}
}

// TestParseAcceptanceFromPlan locks down extracting acceptance criteria from Plan markdown: line-scan Run:/Expected:
// pairing, merged into `run :: expected` fed to parseOneAcceptance to reuse :: boundary handling. Covers centralized block / Task
// inline multi-block / no block / bare Run / orphan Expected / consecutive Run — any breakage makes --plan-file auto-extraction void,
// and the acceptance dimension keeps spinning empty (dogfood evidence: this project's 28 task conclusions all have acceptance 0/0).
//
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

// TestMergeAcceptance locks down that explicit --accept takes priority, and plan extraction deduplicates by Run to supplement.
// On coexistence, explicit entries expressing override/fine-tuning should win, plan only supplements non-conflicting Runs.
//
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

// TestVerifyAcceptance_RecordsAcceptedHeadCommit pins the AcceptedHeadCommit backfill semantics: the proof v2
// fast path (AcceptedHeadCommit == current HEAD to judge Passed fresh) depends on VerifyAcceptance recording this
// snapshot during the real run. Under a git repo it must == git rev-parse --short HEAD.
//
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

// TestVerifyAcceptance_AcceptedHeadCommit_NonGitEmpty pins the non-git directory degradation: GetHeadCommit
// fails and returns an empty string (no panic), proof v1 rerun fallback relies on this empty value to judge going through rerun. t.TempDir() is not a git repo.
//
// TestVerifyAcceptance_AcceptedHeadCommit_NonGitEmpty 钉住非 git 目录的退化：GetHeadCommit
// 失败返""（不 panic），proof v1 重跑兜底靠此空值判定走重跑。t.TempDir() 非 git 仓库。
func TestVerifyAcceptance_AcceptedHeadCommit_NonGitEmpty(t *testing.T) {
	dir := t.TempDir()
	state := &TaskState{Acceptance: ParseAcceptance([]string{`go version :: go version`})}
	VerifyAcceptance(dir, state)
	if got := state.Acceptance[0].AcceptedHeadCommit; got != "" {
		t.Errorf("非 git 目录 AcceptedHeadCommit 应为空（GetHeadCommit 失败返），got %q", got)
	}
}

// TestParseAcceptanceFromPlan_FencedCodeBlock pins fenced-code-block recognition: Run:/Expected: lines inside ```/~~~ code example blocks
// are not extracted — shell snippets pasted in plan that happen to start with `Run:` would be misextracted as acceptance criteria.
// isFenceMarker uses the same decision branch for backtick (96) and tilde; here we test with ~~~ fences (plan uses backtick raw
// strings to write real newlines across lines, avoiding the double-quote corruption pitfall + avoiding raw-string backtick-termination conflicts).
//
// TestParseAcceptanceFromPlan_FencedCodeBlock 钉住 fenced 围栏识别：```/~~~ 代码示例块内的
// Run:/Expected: 不提取——plan 贴的 shell 片段恰好含"Run:"开头行会被误提取成验收标准。
// isFenceMarker 对反引号(96)/波浪号走同一判定分支；这里用 ~~~ 围栏测（plan 用反引号 raw
// string 跨行写真实换行，规避双引号腐蚀坑 + 避开 raw string 反引号终止冲突）。
func TestParseAcceptanceFromPlan_FencedCodeBlock(t *testing.T) {
	plan := `## 验收
~~~bash
Run: echo 代码示例伪验收
Expected: 不该被提取
~~~
Run: go test ./...
Expected: ok
`
	got := ParseAcceptanceFromPlan(plan)
	want := []AcceptanceCriterion{{Run: `go test ./...`, Expected: `ok`}}
	if len(got) != len(want) {
		t.Fatalf(`围栏内 Run:/Expected: 应跳过；提取条数 %d want 1 (got=%v)`, len(got), got)
	}
	if got[0].Run != want[0].Run || got[0].Expected != want[0].Expected {
		t.Errorf(`got {Run:%q Expected:%q}, want {%q %q}`, got[0].Run, got[0].Expected, want[0].Run, want[0].Expected)
	}
}

// TestCheckAcceptanceFresh locks down the task-complete acceptance pre-flight deterministic judgment —
// providing a consumer for AcceptedHeadCommit (after MCP teardown this field is written by VerifyAcceptance but has no production consumer,
// becoming an orphan field). This test pins that `claimed acceptance passed` must have a verifiable consumer: not actually run / snapshot stale / not passed → BLOCKED.
// Corresponds to Emergence World affordance gate + Proof of Work.
//
// TestCheckAcceptanceFresh 锁定 task-complete acceptance pre-flight 的 deterministic 判定——
// 给 AcceptedHeadCommit 补消费方（MCP 拆除后该字段在 VerifyAcceptance 写入但无生产消费方，
// 成孤儿字段）。本测试钉住「声称验收过」必须有可验证 consumer：未实跑/快照过期/未通过 → BLOCKED。
// 对应 Emergence World affordance gate + Proof of Work。
func TestCheckAcceptanceFresh(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "T")
	runGit(t, dir, "commit", "--allow-empty", "-m", "c1")
	head := headShort(t, dir)

	cases := []struct {
		name       string
		acc        []AcceptanceCriterion
		override   string
		wantOK     bool
		wantReason string // 仅 wantOK=false 时检查 reasons 含此子串
	}{
		{name: `无 acceptance 放行`, acc: nil, wantOK: true},
		{name: `未实跑（AcceptedHeadCommit 空）BLOCKED`,
			acc:    []AcceptanceCriterion{{Run: `go version`, AcceptedHeadCommit: ``}},
			wantOK: false, wantReason: `未实跑`},
		{name: `快照过期（AcceptedHeadCommit ≠ HEAD）BLOCKED`,
			acc:    []AcceptanceCriterion{{Run: `go version`, AcceptedHeadCommit: `deadbeef`, Passed: true}},
			wantOK: false, wantReason: `基于旧代码`},
		{name: `未通过（Passed=false）BLOCKED`,
			acc:    []AcceptanceCriterion{{Run: `go version`, AcceptedHeadCommit: head, Passed: false}},
			wantOK: false, wantReason: `未通过`},
		{name: `全 fresh 放行（AcceptedHeadCommit==HEAD 且 Passed）`,
			acc:    []AcceptanceCriterion{{Run: `go version`, AcceptedHeadCommit: head, Passed: true}},
			wantOK: true},
		{name: `escape override 放行（per-task）`,
			acc:      []AcceptanceCriterion{{Run: `go version`, AcceptedHeadCommit: ``}},
			override: `disable`, wantOK: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			state := &TaskState{TaskRef: `feat/x`, Acceptance: c.acc}
			if c.override != "" {
				state.Overrides.AcceptanceGate = c.override
			}
			ok, reasons := CheckAcceptanceFresh(dir, state)
			if ok != c.wantOK {
				t.Fatalf(`ok=%v want %v, reasons=%v`, ok, c.wantOK, reasons)
			}
			if c.wantOK {
				if len(reasons) != 0 {
					t.Errorf(`放行时 reasons 应空，got %v`, reasons)
				}
				return
			}
			found := false
			for _, r := range reasons {
				if strings.Contains(r, c.wantReason) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf(`reasons %v 应含 %q`, reasons, c.wantReason)
			}
		})
	}
}

// TestCheckAcceptanceFresh_NonGitShortCircuit pins the non-git directory documentation contract: when GetHeadCommit returns empty
// the fresh check short-circuits to pass. Under non-git, verify-acceptance writes AcceptedHeadCommit to an empty string (Passed may be true),
// without the short-circuit case 1 `not actually run` would falsely match and forever BLOCKED — non-git projects (explicitly supported by Forge) running acceptance would fail to complete.
//
// TestCheckAcceptanceFresh_NonGitShortCircuit 钉住非 git 目录的文档契约：GetHeadCommit 返空时
// fresh 检查短路放行。非 git 下 verify-acceptance 写 AcceptedHeadCommit=""（Passed 可能 true），
// 无短路则 case 1「未实跑」误命中致永远 BLOCKED——非 git 项目（Forge 显式支持）跑验收反而过不了 complete。
func TestCheckAcceptanceFresh_NonGitShortCircuit(t *testing.T) {
	dir := t.TempDir() // 非 git 仓库
	state := &TaskState{TaskRef: `feat/nogit`, Acceptance: []AcceptanceCriterion{
		{Run: `go version`, AcceptedHeadCommit: ``, Passed: true}, // 非 git verify 回填空 + Passed
	}}
	ok, reasons := CheckAcceptanceFresh(dir, state)
	if !ok {
		t.Fatalf(`非 git 目录应短路放行（head 空），got reasons=%v`, reasons)
	}
}
