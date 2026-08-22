package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/skillgen"
	"github.com/MjxUpUp/Forge/internal/userassets"
	"github.com/spf13/cobra"
)

// uninstall.go — `forge uninstall` one-shot reversal: npm global package + init-suggest markers.
//
// Design: clears `npm uninstall -g @agent_forge/forge` (binary) + `~/.forge/.init-suggested/`
// (per-project init prompt markers). Plugin uninstall must run interactively inside the agent
// CLI (`/plugin uninstall forge@forge` etc. are not scriptable) — print guidance instead.
// Project-level `.forge/` is left untouched (user decides whether to keep; to clear, run
// manually `rm -rf .forge/`.).
//
// Test hook: FORGE_UNINSTALL_SKIP_NPM=1 skips the npm call (for tests or when npm is unavailable).
//
// Chinese strings use raw strings (backticks) to dodge Windows input quote corruption.
//
// uninstall.go — `forge uninstall` 一键反装：npm 全局包 + init-suggest markers。
//
// 设计：清 `npm uninstall -g @agent_forge/forge`（binary）+ `~/.forge/.init-suggested/`
// （per-project init 提示标记）。Plugin 卸载必须在 agent CLI 内交互跑（`/plugin
// uninstall forge@forge` 等不可脚本化）——打印指引。项目级 `.forge/` 不动（用户
// 决定是否留；若要清手动 rm -rf .forge/）。
//
// 测试钩子：FORGE_UNINSTALL_SKIP_NPM=1 跳过 npm 调用（测试或 npm 不可用场景）。
//
// 中文字符串 raw string（反引号）规避 Windows 输入引号腐蚀。

// userLevelStripRoster is the 2c roster: agent name → user-level hook strip function.
// Hoisted to a package-level var (review M-1) so TestUninstall_StripRosterPinned can pin
// its key set — each host is only guarded by its own TestUninstall_StripsXxxHooks, so a
// host missing from THIS map (the codebuddy gap, 2026-08-21) would silently survive
// uninstall. Adding a host with a Strip function: add it here AND to the pin test —
// the test forces the second touch.
//
// userLevelStripRoster 是 2c 名册：agent 名 → 用户级 hook 剥除函数。提升为包级变量
// （评审 M-1）让 TestUninstall_StripRosterPinned 钉住 key 集合——每个 host 只被自己
// 的 TestUninstall_StripsXxxHooks 守卫，若 host 在本名册缺席（codebuddy 缺口，
// 2026-08-21）会静默躲过卸载。新增带 Strip 函数的 host：加这里 AND 加进钉扎测试
// ——测试逼你完成第二处。
var userLevelStripRoster = map[string]func() (bool, error){
	`codex`:    agentbridge.StripCodexHooksUserLevel,
	`cursor`:   agentbridge.StripCursorHooksUserLevel,
	`opencode`: agentbridge.StripOpenCodeUserPlugin,
	`windsurf`: agentbridge.StripWindsurfHooksUserLevel,
	`reasonix`: agentbridge.StripReasonixHooksUserLevel,
	`cline`:    agentbridge.StripClineHooks,
	// codebuddy 接线是三件套（marketplace 条目 + enabledPlugins 键 + forge 自有
	// 资产目录），StripCodeBuddyHooks 一次反转全部——此前缺席，卸载后 WorkBuddy
	// 仍持有指向已删二进制的目录指针。
	`codebuddy`: agentbridge.StripCodeBuddyHooks,
}

// runUserLevelStrips executes the 2c roster (agent name → user-level strip function),
// printing one line per agent: full success (已清除), partial failure (已部分清除 +
// warning — review L-4: changed=true is durable on-disk progress, report it even on
// error), or a failed strip with nothing removed (warning only). Best-effort: a failing
// agent never aborts the loop — the remaining hosts still get cleaned. Hoisted out of
// RunE so the L-4 partial-failure branch is testable with an injected fake roster.
//
// runUserLevelStrips 执行 2c 名册（agent 名 → 用户级剥除函数），每个 agent 打一行：
// 完全成功（已清除）、部分失败（已部分清除 + 警告——评审 L-4：changed=true 是已落盘
// 的持久进展，报错时也报它）、或零进展的失败（仅警告）。best-effort：单个 agent 失败
// 绝不中止循环——其余 host 仍被清理。从 RunE 提出使 L-4 部分失败分支可用注入的
// fake 名册测试。
func runUserLevelStrips(roster map[string]func() (bool, error)) {
	for name, strip := range roster {
		if stripped, err := strip(); err != nil {
			// Partial-failure honesty (review L-4): a strip may remove seam 1 and then fail
			// on seam 2 — changed=true is durable on-disk progress, so report it even on
			// error, then the warning. Re-running is idempotent and finishes the rest.
			//
			// 部分失败诚实性（评审 L-4）：strip 可能删完第 1 处后在第 2 处失败——
			// changed=true 是已落盘的持久进展，报错时也先报它再给警告。重跑幂等，补完其余。
			if stripped {
				fmt.Printf(`已部分清除 %s 用户级配置中的 forge hooks`+"\n", name)
			}
			fmt.Fprintf(os.Stderr, `警告：清理 %s 用户级 hooks 失败：%v`+"\n", name, err)
		} else if stripped {
			fmt.Printf(`已清除 %s 用户级配置中的 forge hooks`+"\n", name)
		}
	}
}

// uninstallClearMarkers removes the init-suggest marker directory (<GlobalHome>/.init-suggested/).
// It uses forgedata.GlobalHome() (FORGE_DATA_HOME first, otherwise ~/.forge) — refactor-data-home
// commit E is the single source of truth, sharing the same marker store as the suggest command +
// init-suggest hook (uninstall is the cleanup path of that store; it must use the same root,
// otherwise FORGE_DATA_HOME users clear the wrong place and leave stale markers behind).
// exported for testability — RunE calls this. Returns (dir, removed bool); on GlobalHome failure
// it returns an empty dir and false.
//
// uninstallClearMarkers 删 init-suggest marker 目录（<GlobalHome>/.init-suggested/）。
// 走 forgedata.GlobalHome()（FORGE_DATA_HOME 优先，否则 ~/.forge）——refactor-data-home
// commit E 统一真相源，与 suggest 命令 + init-suggest hook 读写同一 marker store（uninstall
// 是该 store 的清理路径，必须同根，否则 FORGE_DATA_HOME 用户清错地方留残留 marker）。
// exported for testability — RunE 调用此。返 (dir, removed bool)；GlobalHome 失败返 ("", false)。
func uninstallClearMarkers() (string, bool) {
	home, err := forgedata.GlobalHome()
	if err != nil {
		return ``, false
	}
	dir := filepath.Join(home, `.init-suggested`)
	if err := os.RemoveAll(dir); err != nil {
		return dir, false
	}
	return dir, true
}

var uninstallCmd = &cobra.Command{
	Use:   `uninstall`,
	Short: `卸载 forge 二进制 + init-suggest 标记（plugin 卸载需在 agent CLI 内进行）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		restore, _ := cmd.Flags().GetBool("restore")

		// 1. npm uninstall -g @agent_forge/forge (skip via SKIP_NPM in test/offline scenarios)
		//
		// 1. npm uninstall -g @agent_forge/forge（测试 / 离线场景可 SKIP_NPM 跳过）
		if os.Getenv(`FORGE_UNINSTALL_SKIP_NPM`) != `1` {
			if _, err := exec.LookPath(`npm`); err == nil {
				npmCmd := exec.Command(`npm`, `uninstall`, `-g`, `@agent_forge/forge`)
				npmCmd.Stdout = os.Stdout
				npmCmd.Stderr = os.Stderr
				if err := npmCmd.Run(); err != nil {
					fmt.Fprintf(os.Stderr, `警告：npm uninstall 失败：%v（可能未通过 npm 装）`+"\n", err)
				}
			} else {
				fmt.Fprintf(os.Stderr, `警告：npm 不可用，跳过 binary 卸载`+"\n")
			}
		}

		// 2. remove init-suggest markers
		//
		// 2. 删除 init-suggest markers
		dir, ok := uninstallClearMarkers()
		if !ok {
			fmt.Fprintf(os.Stderr, `警告：删除 %s 失败`+"\n", dir)
		} else {
			fmt.Printf(`已清除 init-suggest 标记：%s`+"\n", dir)
		}

		// 2b. strip forge hooks from kimi-code's user-level config.toml (they would call a
		// binary that no longer exists; kimi fails open on hook errors, but the noise on
		// every tool call is not a clean uninstall)
		//
		// 2b. 从 kimi-code 的 user-level config.toml 剥除 forge hooks（否则它们会调用
		// 一个已不存在的二进制；kimi 对 hook 错误 fail-open，但每次工具调用的报错
		// 噪声不算干净卸载）
		if stripped, err := agentbridge.StripKimiHooks(); err != nil {
			fmt.Fprintf(os.Stderr, `警告：清理 kimi hooks 失败：%v`+"\n", err)
		} else if stripped {
			fmt.Println(`已清除 kimi-code config.toml 中的 forge hooks`)
		}

		// 2c. strip forge hooks from every agent's USER-LEVEL config (the post
		//     user-level-assets registration points). Best-effort, warn-not-fail.
		//
		// 2c. 剥除各 agent 用户级配置里的 forge hooks（user-level-assets 之后的
		//     注册点）。best-effort，告警不阻断。
		if stripped, err := hooks.StripForgeHooksUserLevel(); err != nil {
			fmt.Fprintf(os.Stderr, `警告：清理 claude 用户级 settings 失败：%v`+"\n", err)
		} else if stripped {
			fmt.Println(`已清除 claude 用户级 settings 中的 forge hooks`)
		}
		runUserLevelStrips(userLevelStripRoster)

		// 2d. strip the forge instruction sections from user-level CLAUDE.md / AGENTS.md /
		//     windsurf global_rules.md (preserving all user content outside the markers).
		//
		// 2d. 剥除用户级 CLAUDE.md / AGENTS.md / windsurf global_rules.md 的 forge
		//     指令段（标记外的用户内容全部保留）。
		if err := skillgen.StripUserInstructions(); err != nil {
			fmt.Fprintf(os.Stderr, `警告：清理用户级指令段失败：%v`+"\n", err)
		} else {
			fmt.Println(`已清除用户级 CLAUDE.md / AGENTS.md 中的 forge 段`)
		}
		if stripped, err := agentbridge.StripWindsurfGlobalRules(); err != nil {
			fmt.Fprintf(os.Stderr, `警告：清理 windsurf global_rules.md 失败：%v`+"\n", err)
		} else if stripped {
			fmt.Println(`已清除 windsurf global_rules.md 中的 forge 段`)
		}

		// 2e. remove the user-level forge-quality skill (~/.claude/skills/forge-quality/,
		//     respecting CLAUDE_CONFIG_DIR) — it is forge-generated content, left behind
		//     by every init/autoSync otherwise.
		//
		// 2e. 删除用户级 forge-quality skill（~/.claude/skills/forge-quality/，尊重
		//     CLAUDE_CONFIG_DIR）——它是 forge 生成的内容，不删会被历次
		//     init/autoSync 一直留下。
		if home := hooks.ClaudeHome(); home != `` {
			skillDir := filepath.Join(home, `skills`, `forge-quality`)
			if _, err := os.Stat(skillDir); err == nil {
				if err := os.RemoveAll(skillDir); err != nil {
					fmt.Fprintf(os.Stderr, `警告：删除用户级 forge-quality skill 失败：%v`+"\n", err)
				} else {
					fmt.Println(`已删除用户级 forge-quality skill`)
				}
			}
		}

		// 2e'. remove the user-level reasonix forge-quality skill
		// (<reasonix home>/skills/forge-quality/ = %APPDATA%\reasonix on Windows,
		// respecting REASONIX_HOME) — written by the reasonix translator; symmetric
		// uninstall.
		//
		// 2e'. 删除 reasonix 用户级 forge-quality skill
		// （<reasonix home>/skills/forge-quality/，Windows 上 = %APPDATA%\reasonix，
		// 尊重 REASONIX_HOME）——由 reasonix translator 写入；对称卸载。
		if rHome, err := agentbridge.ReasonixConfigHome(); err == nil {
			rSkillDir := filepath.Join(rHome, `skills`, `forge-quality`)
			if _, statErr := os.Stat(rSkillDir); statErr == nil {
				if err := os.RemoveAll(rSkillDir); err != nil {
					fmt.Fprintf(os.Stderr, `警告：删除 reasonix 用户级 forge-quality skill 失败：%v`+"\n", err)
				} else {
					fmt.Println(`已删除 reasonix 用户级 forge-quality skill`)
				}
			}
		}

		// 2f. --restore: roll every user-level file forge touched back to its pre-forge
		//     bytes (backup+append contract; backups at ~/.forge/backups/).
		//
		// 2f. --restore：把 forge 碰过的用户级文件回滚到 forge 修改前的原始字节
		//     （备份+追加契约；备份在 ~/.forge/backups/）。
		if restore {
			restored, errs := userassets.RestoreAll()
			for _, p := range restored {
				fmt.Printf(`已回滚：%s`+"\n", p)
			}
			for _, e := range errs {
				fmt.Fprintf(os.Stderr, `警告：回滚失败：%v`+"\n", e)
			}
			if len(restored) == 0 && len(errs) == 0 {
				fmt.Println(`无可回滚的备份（~/.forge/backups/ 为空）`)
			}
		} else {
			fmt.Println(`提示：加 --restore 可把用户级文件回滚到 forge 修改前的原始内容（备份在 ~/.forge/backups/）`)
		}

		// 3. plugin uninstall guidance (interactive inside agent CLI, not scriptable)
		//
		// 3. plugin 卸载指引（agent CLI 内交互，不可脚本化）
		fmt.Println(`plugin 卸载须在 agent CLI 内交互运行：`)
		fmt.Println(`  Claude Code / Cursor:  /plugin uninstall forge@forge`)
		fmt.Println(`  Codex:                 codex plugin uninstall forge@forge`)
		fmt.Println(`  Copilot CLI:           copilot plugin uninstall forge@forge`)
		fmt.Println(`  Kimi Code:             /plugins remove forge`)
		fmt.Println(`项目级 .forge/ 未动（团队模式/老项目残留）。如需清，手动 rm -rf .forge/；用户级资产可用 --restore 回滚`)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
	uninstallCmd.Flags().Bool("restore", false, "回滚用户级文件到 forge 修改前的原始内容（~/.forge/backups/）")
}
