package docsconsistency

import (
	"testing"

	"github.com/spf13/cobra"
)

// withTree registers a temporary command tree to run fn, and restores afterwards (avoids sharing global cmdTreeFn across tests).
//
// withTree 注册一个临时命令树运行 fn，测试后还原（避免测试间共享全局 cmdTreeFn）。
func withTree(t *testing.T, root *cobra.Command, fn func()) {
	t.Helper()
	mu.Lock()
	prev := cmdTreeFn
	cmdTreeFn = func() *cobra.Command { return root }
	mu.Unlock()
	defer func() {
		mu.Lock()
		cmdTreeFn = prev
		mu.Unlock()
	}()
	fn()
}

// TestValidateForgePath_Mechanism proves the detection mechanism actually catches drift — rather than a stub that always returns "".
// Covers parent-exists-child-not (experience propose: experience exists, propose does not) and top-level-not-exists.
//
// TestValidateForgePath_Mechanism 证明检测机制真能抓 drift——而非恒返回 "" 的空壳。
// 含父命令存在子命令不存在（experience propose：experience 有，propose 无）和顶层不存在。
func TestValidateForgePath_Mechanism(t *testing.T) {
	root := &cobra.Command{Use: "forge"}
	exp := &cobra.Command{Use: "experience"}
	exp.AddCommand(&cobra.Command{Use: "accept"})
	root.AddCommand(exp)
	root.AddCommand(&cobra.Command{Use: "init"})
	root.AddCommand(&cobra.Command{Use: "sync"})

	cases := []struct {
		name string
		ref  string
		want string // 空 = 路径完整；非空 = 首个断链的子命令
	}{
		{"单层命令", "init", ""},
		{"两层命令完整", "experience accept", ""},
		{"父存在子不存在", "experience propose", "propose"},
		{"顶层不存在", "nonexistent", "nonexistent"},
		{"flag 后即停", "init --mode small", ""},
		{"占位符后即停", "init <name>", ""},
		{"方括号后即停", "sync [--force]", ""},
		{"分隔符后即停", "init small|medium", ""},
		{"裸 forge", "", ""},
	}
	withTree(t, root, func() {
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if got := ValidateForgePath(tc.ref); got != tc.want {
					t.Fatalf("ValidateForgePath(%q) = %q, want %q", tc.ref, got, tc.want)
				}
			})
		}
	})
}

// TestValidateForgePath_UnregisteredTree must pass through (return "") when the command tree is unregistered, not report false drift.
// Ensures callers using this package without a registered callback do not get false positives — advisory stays silent rather than noisy.
//
// TestValidateForgePath_UnregisteredTree 命令树未注册时必须放行（返回 ""），不报假 drift。
// 这保证本包被未注册回调的调用方使用时不误报——advisory 宁静默不噪声。
func TestValidateForgePath_UnregisteredTree(t *testing.T) {
	withTree(t, nil, func() {
		if got := ValidateForgePath("totally-bogus propose"); got != "" {
			t.Fatalf("unregistered tree should pass-through (empty), got %q", got)
		}
	})
}

// TestDriftedCommands end-to-end proof that the pipeline of regex-extracting backtick forge references → ValidateForgePath
// can catch every ghost from document text: real commands pass, multiple ghosts all caught (order preserved).
//
// TestDriftedCommands 端到端证明 regex 抽取反引号 forge 引用 → ValidateForgePath 校验
// 的管道能从文档文本中抓出所有 ghost：真命令放行，多个 ghost 全抓（顺序保留）。
func TestDriftedCommands(t *testing.T) {
	root := &cobra.Command{Use: "forge"}
	exp := &cobra.Command{Use: "experience"}
	exp.AddCommand(&cobra.Command{Use: "accept"})
	root.AddCommand(exp)

	doc := "运行 `forge experience accept` 接纳；勿用不存在的 `forge experience propose` 或 `forge bogus`。"
	withTree(t, root, func() {
		drifted := DriftedCommands(doc)
		want := []string{"experience propose", "bogus"}
		if len(drifted) != len(want) {
			t.Fatalf("DriftedCommands = %v, want %v", drifted, want)
		}
		for i := range want {
			if drifted[i] != want[i] {
				t.Errorf("drifted[%d] = %q, want %q", i, drifted[i], want[i])
			}
		}
	})
}

// TestDriftedCommands_Dedup reports a drift command only once even if it appears N times in the doc —
// prevents advisory stderr from repeating the same command ("experience propose, experience propose").
//
// TestDriftedCommands_Dedup 同一 drift 命令在文档出现 N 次只报一次——
// 避免 advisory stderr 重复刷同一命令（"experience propose, experience propose"）。
func TestDriftedCommands_Dedup(t *testing.T) {
	root := &cobra.Command{Use: "forge"}
	root.AddCommand(&cobra.Command{Use: "init"})

	doc := "见 `forge bogus`；再强调 `forge bogus`；还有 `forge bogus`。"
	withTree(t, root, func() {
		drifted := DriftedCommands(doc)
		if len(drifted) != 1 || drifted[0] != "bogus" {
			t.Fatalf("duplicated ghost must be reported once, got %v", drifted)
		}
	})
}

// TestDanglingSkillRefs_Mechanism proves the detection catches a real dangling skill
// ref while exempting single-segment tokens (tools/keywords) and honoring knownSkills +
// allowlist. Built with rune(0x60) backtick splicing + raw strings to dodge the Windows
// quote-corruption that breaks double-quoted Go literals. This is the skill-ref analogue
// of TestDriftedCommands.
//
// TestDanglingSkillRefs_Mechanism 证明检测能抓真 skill 断链，同时豁免单段 token（工具/
// 关键字）并尊重 knownSkills + allowlist。用 rune(0x60) 反引号拼接 + raw string 构造，
// 绕过 Windows 双引号腐蚀。这是 TestDriftedCommands 的 skill 引用对应物。
func TestDanglingSkillRefs_Mechanism(t *testing.T) {
	bt := string(rune(0x60)) // 反引号字符，绕过双引号腐蚀
	known := map[string]bool{`code-review-gate`: true, `tdd-cycle`: true}
	allow := map[string]bool{`hazard-guard`: true} // hook 名，非 skill

	cases := []struct {
		name string
		text string
		want []string
	}{
		{name: `真 skill 引用放行`, text: `用 ` + bt + `code-review-gate` + bt + ` 审查`, want: nil},
		{name: `断链 skill 抓到`, text: `走 ` + bt + `frontend-stack-selection` + bt + ` 选型`, want: []string{`frontend-stack-selection`}},
		{name: `单段工具豁免`, text: `跑 ` + bt + `grep` + bt + ` 与 ` + bt + `curl` + bt, want: nil},
		{name: `单段关键字豁免`, text: bt + `any` + bt + ` 与 ` + bt + `while` + bt + ` 不报`, want: nil},
		{name: `allowlist 非skill放行`, text: bt + `hazard-guard` + bt + ` hook 已拦`, want: nil},
		{name: `CamelCase 命名示例豁免`, text: `不叫 ` + bt + `MyCard` + bt + `/` + bt + `Card1` + bt, want: nil},
		{name: `多段断链与真skill混合`, text: `用 ` + bt + `code-review-gate` + bt + ` 后走 ` + bt + `ai-generated-ui-review` + bt, want: []string{`ai-generated-ui-review`}},
		{name: `同token去重`, text: bt + `frontend-stack-selection` + bt + ` 与 ` + bt + `frontend-stack-selection` + bt, want: []string{`frontend-stack-selection`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DanglingSkillRefs(tc.text, known, allow)
			if len(got) != len(tc.want) {
				t.Fatalf(`DanglingSkillRefs = %v, want %v (text=%q)`, got, tc.want, tc.text)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf(`got[%d]=%q want %q`, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestDriftedCommands_NoCrossLinePhantom: the regex character class excludes \n, so a code
// span broken by a line break is not spliced into a phantom reference. Before the fix,
// "`forge experience\npropose`" matched with m[1]="experience\npropose" and was reported as a
// drifted path that no doc author ever wrote.
//
// TestDriftedCommands_NoCrossLinePhantom：正则字符类排除 \n，被换行截断的 code span 不会
// 被拼成幻影引用。修复前 "`forge experience\npropose`" 会匹配出 m[1]="experience\npropose"，
// 报出一个文档作者从未写过的 drift 路径。
func TestDriftedCommands_NoCrossLinePhantom(t *testing.T) {
	root := &cobra.Command{Use: "forge"}
	exp := &cobra.Command{Use: "experience"}
	exp.AddCommand(&cobra.Command{Use: "accept"})
	root.AddCommand(exp)

	doc := "第一段 `forge experience\npropose` 跨行；正常的 `forge experience accept` 同行。"
	withTree(t, root, func() {
		drifted := DriftedCommands(doc)
		if len(drifted) != 0 {
			t.Fatalf("cross-line code span must not form a phantom reference, got %v", drifted)
		}
	})
}
