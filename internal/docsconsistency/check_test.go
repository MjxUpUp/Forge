package docsconsistency

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

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

// TestValidateForgePath_UnregisteredTree 命令树未注册时必须放行（返回 ""），不报假 drift。
// 这保证本包被未注册回调的调用方使用时不误报——advisory 宁静默不噪声。
func TestValidateForgePath_UnregisteredTree(t *testing.T) {
	withTree(t, nil, func() {
		if got := ValidateForgePath("totally-bogus propose"); got != "" {
			t.Fatalf("unregistered tree should pass-through (empty), got %q", got)
		}
	})
}

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

// TestStaleBinaryHint 钉住追加在「命令不存在」drift advisory 尾部的版本提示（usage
// 日志修复：README 引用 `skills mine`——HEAD 存在——在 PATH 全局 v1.34.0 旧二进制
// 下触发 advisory，把 agent 引去「修」一份没错的文档）。注册了版本时提示必须带版本
// 号并指向 `forge update`；未注册时也必须带通用的版本过旧嫌疑。
func TestStaleBinaryHint(t *testing.T) {
	mu.Lock()
	prev := versionFn
	mu.Unlock()
	defer func() {
		mu.Lock()
		versionFn = prev
		mu.Unlock()
	}()

	RegisterVersion(func() string { return "v1.34.0" })
	hint := StaleBinaryHint()
	if !strings.Contains(hint, "v1.34.0") || !strings.Contains(hint, "@agent_forge/forge") {
		t.Errorf("注册版本后 hint 应带版本号并指向 npm 重装, got %q", hint)
	}

	RegisterVersion(nil)
	hint = StaleBinaryHint()
	if !strings.Contains(hint, "@agent_forge/forge") {
		t.Errorf("未注册版本时 hint 也应带 npm 重装指引, got %q", hint)
	}

	RegisterVersion(func() string { return "" })
	if hint := StaleBinaryHint(); !strings.Contains(hint, "@agent_forge/forge") {
		t.Errorf("空版本串时 hint 应降级为通用句, got %q", hint)
	}
}

// TestDanglingHomePaths pins ~/ path detection: existing paths pass, missing ones are reported once in order, templates and sentence punctuation are handled, and an empty home disables the check.
//
// TestDanglingHomePaths 钉住 ~/ 路径检测：存在的放过；缺失的按首次出现顺序各报一次；模板路径
// 与句末标点正确处理；home 为空时不检查。
func TestDanglingHomePaths(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".research-workflow", "agent-memory-20261014-1535"), 0o755); err != nil {
		t.Fatal(err)
	}
	text := "报告见 `~/.research-workflow/agent-memory-20261014-1535`。\n" +
		"旧证据：~/.forge/research/forge-direction-20260905-0956/report.md（已失效）。\n" +
		"再次引用 ~/.forge/research/forge-direction-20260905-0956/report.md，\n" +
		"模板：~/.research-workflow/{topic}-<时间戳>、~/.x/*.md、~/.x/$RUN、~/.x/...\n" +
		"另一个缺失：~/.forge/research/forge-mechanisms-20260905-1752."
	got := DanglingHomePaths(text, home, nil)
	want := []string{
		"~/.forge/research/forge-direction-20260905-0956/report.md",
		"~/.forge/research/forge-mechanisms-20260905-1752",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("DanglingHomePaths = %q, want %q", got, want)
	}
	if got := DanglingHomePaths(text, "", nil); got != nil {
		t.Fatalf("empty home must disable the check, got %q", got)
	}
	if got := DanglingHomePaths("no home paths here", home, nil); got != nil {
		t.Fatalf("text without ~/ must return nil, got %q", got)
	}

	// 只查证据前缀：配置文件类路径不报；同一行已标注失效的不报。
	mixed := "配置：~/.forge/config.json\n证据：~/.forge/research/gone-a/report.md\n旧证据：~/.forge/research/gone-b（失效：已清理）\n"
	got = DanglingHomePaths(mixed, home, EvidenceHomePrefixes)
	if !slices.Equal(got, []string{"~/.forge/research/gone-a/report.md"}) {
		t.Fatalf("evidence-prefix scan = %q, want only gone-a", got)
	}
}
