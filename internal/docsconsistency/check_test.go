package docsconsistency

import (
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
