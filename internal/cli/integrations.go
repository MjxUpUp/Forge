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
	"github.com/MjxUpUp/Forge/internal/specroot"
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
项目级让位（不清宿主标记）：forge off；要停掉用户级资产，请先 disable 全部宿主。`,
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
	Use:   "enable <host>[,<host>...] [--all]",
	Short: "把 forge 接到指定宿主（显式写用户级配置）",
	Args: func(cmd *cobra.Command, args []string) error {
		// --all 时不收位置参数；否则至少点名一个宿主。flag 解析先于 Args
		// 校验，所以这里能把「enable --all」与「enable（空）」分开。
		if all, _ := cmd.Flags().GetBool("all"); all {
			return nil
		}
		return cobra.MinimumNArgs(1)(cmd, args)
	},
	RunE: runIntegrationsEnable,
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
// 只认宿主 marker：`.disabled` 墓碑等点文件不是接线。
func integrationsAnyEnabled() bool {
	return len(enabledHostSet()) > 0
}

func markIntegration(host string) error {
	if err := os.MkdirAll(integrationsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(integrationMarker(host), []byte(rootCmd.Version+"\n"), 0o644)
}

// enabledHostSet 读 markers，返回当前显式接线的宿主集合。
func enabledHostSet() map[string]bool {
	entries, err := os.ReadDir(integrationsDir())
	if err != nil {
		return map[string]bool{}
	}
	out := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		out[e.Name()] = true
	}
	return out
}

// sortedHosts 给 set 一个稳定顺序（translator 逐个写，顺序影响输出可读性）。
func sortedHosts(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for h := range set {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

// disabledMarkerPath 是「人显式全关」的墓碑：disable 摘完 marker 后写它，
// enable 时清掉。没有它，无 spec 契约的老仓库（legacyHostWiring 恒真）在
// disable 之后仍会被 autoSync 继续写用户级资产——disable 的输出就成了假话。
func disabledMarkerPath() string {
	return filepath.Join(integrationsDir(), ".disabled")
}

func integrationsExplicitlyDisabled() bool {
	_, err := os.Stat(disabledMarkerPath())
	return err == nil
}

func markIntegrationsDisabled() error {
	if err := os.MkdirAll(integrationsDir(), 0o755); err != nil {
		return err
	}
	return os.WriteFile(disabledMarkerPath(), []byte(rootCmd.Version+"\n"), 0o644)
}

func clearIntegrationsDisabled() {
	_ = os.Remove(disabledMarkerPath())
}

func runIntegrationsEnable(cmd *cobra.Command, args []string) error {
	dir, err := findProjectRoot()
	if err != nil {
		return fmt.Errorf("先在仓库里运行 forge init 声明契约：%w", err)
	}

	hosts, err := parseIntegrationHosts(args)
	if all, _ := cmd.Flags().GetBool("all"); all {
		// --all 不收位置参数（Args 已放行零参数）；这里先判 all 再解析，
		// 否则空 args 会被 parseIntegrationHosts 当成「没有指定宿主」。
		if len(args) > 0 {
			return fmt.Errorf("--all 与显式宿主不能同时给")
		}
		hosts, err = integrationHostNames(), nil
	}
	if err != nil {
		return err
	}
	// 显式接线即撤销「人已全关」墓碑，否则 autoSync 会一直跳过。
	clearIntegrationsDisabled()

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
	// 只有显式点名 claude-code 才写 Claude 的用户级资产；enable cursor 不该顺手
	// 把 ~/.claude/settings.json 的 hook 接上（Claude Code 会全机器执行它）。
	claudeHost := false
	codexHost := false
	for _, h := range hosts {
		switch h {
		case "claude-code":
			claudeHost = true
		case "codex":
			codexHost = true
		}
	}
	if claudeHost {
		if hooks.IsClaudePluginInstalled() {
			fmt.Fprintln(os.Stderr, "forge plugin 已接管 claude hooks，跳过 ~/.claude/settings.json 生成")
		} else if err := hooks.GenerateUserSettings(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate ~/.claude/settings.json: %v\n", err)
		}
		if err := skillgen.GenerateUserClaudeMD(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update ~/.claude/CLAUDE.md: %v\n", err)
		}
		if err := skillgen.GenerateUserQualitySkill(proto); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write quality skill: %v\n", err)
		}
	}
	if codexHost {
		if err := skillgen.GenerateUserAgentsMD(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to update ~/.codex/AGENTS.md: %v\n", err)
		}
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
		if err := markIntegrationsDisabled(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to record disabled state: %v\n", err)
		}
		if hooks.IsClaudePluginInstalled() {
			fmt.Println("已无接线标记。Claude plugin 仍在接管 hooks：autoSync 继续维护用户级指令/技能（仅跳过 settings.json）；" +
				"墓碑已记下，卸载 plugin 后同步即停。")
		} else {
			fmt.Println("已无任何宿主接线：autoSync 不再写用户级资产（这个选择已记下，重启后仍生效）。")
		}
	}
	if dir, err := findProjectRoot(); err == nil && !specroot.Declared(dir) {
		fmt.Println("注意：这个仓库还没有 spec 契约（阶段 C 之前的老项目）。接线标记已摘，" +
			"但用户级配置里已有的 hook 不会被撤回；后续不再自动同步。要重新接线：forge integrations enable <host>。")
	}
	return nil
}
