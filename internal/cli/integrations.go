package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hookdispatch"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(integrationsCmd)
	integrationsCmd.AddCommand(integrationsListCmd)
	integrationsCmd.AddCommand(integrationsEnableCmd)
	integrationsCmd.AddCommand(integrationsDisableCmd)
	integrationsEnableCmd.Flags().Bool("all", false, "接手所有已知宿主")
}

var integrationsCmd = &cobra.Command{
	Use:     "integrations",
	Aliases: []string{"integration"},
	Short:   "管理宿主接线（可选，显式 opt-in）",
	Long: `宿主接线把 hooks 与指令层写进各 agent 的用户级配置（~/.claude/settings.json、
~/.codex/hooks.json、~/.cursor/… 等）。forge init 不做这件事——它只写仓库内产物。

要接哪几个宿主就点名开哪几个：
  forge integrations enable claude-code
  forge integrations enable codex,cursor
  forge integrations enable --all

关闭某个宿主：forge integrations disable <host>（只摘标记，不改已写的宿主配置）。
完全退出：forge off。`,
}

var integrationsListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出已知宿主与接线状态",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		names := integrationHostNames()
		fmt.Printf("%-12s %s\n", "HOST", "STATUS")
		for _, n := range names {
			status := "未接线"
			if integrationEnabled(n) {
				status = "已接线"
			}
			fmt.Printf("%-12s %s\n", n, status)
		}
		if !integrationsAnyEnabled() {
			fmt.Println()
			fmt.Println("当前没有宿主接线。需要 hooks/指令自动生效时运行：forge integrations enable <host>")
		}
		return nil
	},
}

var integrationsEnableCmd = &cobra.Command{
	Use:   "enable <host>[,<host>...]",
	Short: "把 forge 接到指定宿主（显式写用户级配置）",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runIntegrationsEnable,
}

var integrationsDisableCmd = &cobra.Command{
	Use:   "disable <host>[,<host>...]",
	Short: "停用指定宿主的接线标记",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runIntegrationsDisable,
}

// integrationHostNames 返回全部可接线宿主的稳定顺序（agentbridge 的 translator 顺序）。
func integrationHostNames() []string {
	var names []string
	for _, t := range agentbridge.AllTranslators() {
		names = append(names, string(t.AgentType()))
	}
	return names
}

// integrationAliases 接受少量常见别名；其余必须是 translator 的规范名。
var integrationAliases = map[string]string{
	"claude":      string(agentbridge.AgentClaudeCode),
	"claude_code": string(agentbridge.AgentClaudeCode),
	"codex-cli":   string(agentbridge.AgentCodex),
	"copilot-cli": string(agentbridge.AgentCopilot),
}

// parseIntegrationHosts 解析并校验宿主名单（逗号分隔），拒绝未知名与空名。
func parseIntegrationHosts(args []string) ([]string, error) {
	known := map[string]bool{}
	for _, n := range integrationHostNames() {
		known[n] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, arg := range args {
		for _, raw := range strings.Split(arg, ",") {
			name := strings.ToLower(strings.TrimSpace(raw))
			if name == "" {
				continue
			}
			if mapped, ok := integrationAliases[name]; ok {
				name = mapped
			}
			if !known[name] {
				return nil, fmt.Errorf("未知宿主 %q；可选：%s", raw, strings.Join(integrationHostNames(), ", "))
			}
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("没有指定宿主；可选：%s", strings.Join(integrationHostNames(), ", "))
	}
	sort.Strings(out)
	return out, nil
}

// integrationsDir 是宿主接线标记的存放处（~/.forge/integrations/<host>）。标记存在
// = 用户显式开启过该宿主；autoSync 只在存在任一标记（或装了 claude plugin）时才
// 维护用户级资产。
func integrationsDir() string {
	home, err := forgedata.GlobalHome()
	if err != nil {
		home = ".forge"
	}
	return filepath.Join(home, "integrations")
}

func integrationMarker(host string) string {
	return filepath.Join(integrationsDir(), host)
}

func integrationEnabled(host string) bool {
	info, err := os.Stat(integrationMarker(host))
	return err == nil && !info.IsDir()
}

// integrationsAnyEnabled 判断是否有任何宿主接线（autoSync 的 HOME 写入闸门）。
func integrationsAnyEnabled() bool {
	entries, err := os.ReadDir(integrationsDir())
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			return true
		}
	}
	return false
}

func markIntegration(host string) error {
	if err := os.MkdirAll(integrationsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(integrationMarker(host), []byte(rootCmd.Version+"\n"), 0o644)
}

func runIntegrationsEnable(cmd *cobra.Command, args []string) error {
	dir, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("先在仓库里运行 forge init 声明契约：%w", err)
	}

	hosts, err := parseIntegrationHosts(args)
	if err != nil {
		return err
	}
	if all, _ := cmd.Flags().GetBool("all"); all {
		hosts = integrationHostNames()
	}

	// 以下写入全在用户主目录——这是显式 opt-in 的边界：init 不写、enable 才写。
	proj, err := forgedata.ProjectFor(dir)
	if err != nil {
		return fmt.Errorf("无法推导项目身份：%w", err)
	}
	if err := proj.Ensure(); err != nil {
		return fmt.Errorf("创建状态目录失败：%w", err)
	}
	if err := protocol.EnsureDefault(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write default protocol: %v\n", err)
	}
	proto, err := protocol.Load(dir)
	if err != nil {
		proto = protocol.DefaultProtocol()
	}
	if err := hooks.WriteHookDeployStamp(forgedata.DataDirFor(dir), hookdispatch.ProjectTagFor(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write hook deploy stamp: %v\n", err)
	}
	if err := hooks.WriteHookTemplates(forgedata.DataDirFor(dir)); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to copy hook templates: %v\n", err)
	}
	if hooks.IsClaudePluginInstalled() {
		fmt.Fprintln(os.Stderr, "forge plugin 已接管 claude hooks，跳过 ~/.claude/settings.json 生成")
	} else if err := hooks.GenerateUserSettings(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate ~/.claude/settings.json: %v\n", err)
	}
	if err := skillgen.GenerateUserClaudeMD(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update ~/.claude/CLAUDE.md: %v\n", err)
	}
	if err := skillgen.GenerateUserAgentsMD(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to update ~/.codex/AGENTS.md: %v\n", err)
	}
	if err := skillgen.GenerateUserQualitySkill(proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write quality skill: %v\n", err)
	}

	agents := make([]agentbridge.AgentType, 0, len(hosts))
	for _, h := range hosts {
		agents = append(agents, agentbridge.AgentType(h))
	}
	bridgeInput := &agentbridge.TranslationInput{Protocol: proto, HookNames: hooks.HookNames()}
	for _, e := range agentbridge.TranslateForAgents(dir, agents, bridgeInput) {
		fmt.Fprintf(os.Stderr, "Warning: host translation failed: %v\n", e)
	}

	if err := registry.Add(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to register project globally: %v\n", err)
	}
	// sync 戳：让 autoSync 在接线后按当前版本维护用户级资产（与旧 init 对齐）。
	if err := os.WriteFile(filepath.Join(forgedata.DataDirFor(dir), ".sync-version"), []byte(rootCmd.Version), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write sync stamp: %v\n", err)
	}
	for _, h := range hosts {
		if err := markIntegration(h); err != nil {
			return fmt.Errorf("记录宿主接线标记失败：%w", err)
		}
	}

	fmt.Printf("已接线（显式 opt-in；用户级配置在 %s）：\n", integrationHostsRootHint())
	for _, h := range hosts {
		fmt.Printf("  %-12s 已开启\n", h)
	}
	fmt.Println()
	fmt.Println("退出：forge integrations disable <host>（摘标记）或 forge off（项目级让位）。")
	MaybeOfferHarness("forge integrations enable")
	return nil
}

// integrationHostsRootHint 给用户一个可读的落点提示（不同宿主落点不同）。
func integrationHostsRootHint() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "用户主目录"
	}
	return home
}

func runIntegrationsDisable(_ *cobra.Command, args []string) error {
	hosts, err := parseIntegrationHosts(args)
	if err != nil {
		return err
	}
	for _, h := range hosts {
		if err := os.Remove(integrationMarker(h)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("移除 %s 标记失败：%w", h, err)
		}
		fmt.Printf("  %-12s 已停用\n", h)
	}
	fmt.Println("宿主配置未被删除（避免误删你自己的设置）；要彻底移除请跑 forge off 或手动清理。")
	if !integrationsAnyEnabled() {
		fmt.Println("已无任何宿主接线：后续 autoSync 不再写用户级资产。")
	}
	return nil
}
