package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/clitask"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

func init() {
	// collect-golden 的执行体 RunCollectGoldenMode 住 clitask（task 域，普查 A2-3
	// 迁出）；flag 属 verify 命令面，故在 verify 域注册。
	verifyCmd.Flags().String(`collect-golden`, ``, `从已完成任务采集真实 golden case 到 testdata/golden_real/（开发工具：固化真实评分形状进 CI 回归）`)
	rootCmd.AddCommand(verifyCmd)
	verifyCmd.Flags().Bool("regression", false, "运行所有 E2E 回归场景")
	verifyCmd.Flags().Bool("run-tests", false, "运行项目测试套件并把真实结果记为 deterministic 证据（checklog: test-run）")
	verifyCmd.Flags().String("scenario", "", "运行指定场景 (fresh-install, master-reminder, upgrade-v040, upgrade-v030)")
}

var verifyCmd = &cobra.Command{
	Use:   "verify [--regression] [--scenario <name>]",
	Short: "验证项目完整性和运行回归测试",
	Long: `forge verify 检查当前项目的 Forge 配置完整性：
  - hook 脚本存在性
  - protocol.yml 可解析且含评分配置
  - Claude Code skills 存在
  - settings.local.json 存在

使用 --regression 运行所有 E2E 回归场景，
使用 --scenario <name> 运行指定场景。`,
	RunE: runVerify,
}

func runVerify(cmd *cobra.Command, args []string) error {
	regression, _ := cmd.Flags().GetBool("regression")
	scenario, _ := cmd.Flags().GetString("scenario")
	runTests, _ := cmd.Flags().GetBool("run-tests")
	collectGolden, _ := cmd.Flags().GetString(`collect-golden`)

	if len(collectGolden) > 0 {
		return clitask.RunCollectGoldenMode(collectGolden)
	}
	if runTests {
		return runProjectTestsMode()
	}
	if regression || scenario != "" {
		return runRegressionMode(scenario)
	}

	return runDefaultChecks()
}

// ---------- 默认模式 ----------

type checkResult struct {
	name string
	ok   bool
	msg  string
}

func runDefaultChecks() error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}

	checks := []struct {
		name string
		fn   func(string) checkResult
	}{
		{"Hook 脚本", checkHooks},
		{"Protocol 配置", checkProtocol},
		{"Quality Skill", checkQualitySkill},
		{"Settings 配置", checkSettings},
	}

	results := make([]checkResult, 0, len(checks))
	allOK := true

	for _, c := range checks {
		r := c.fn(root)
		results = append(results, r)
		if !r.ok {
			allOK = false
		}
	}

	// 打印结果
	fmt.Println("Forge 项目完整性检查")
	fmt.Println()
	for _, r := range results {
		icon := "OK"
		if !r.ok {
			icon = "FAIL"
		}
		if r.msg != "" {
			fmt.Printf("  [%s] %s: %s\n", icon, r.name, r.msg)
		} else {
			fmt.Printf("  [%s] %s\n", icon, r.name)
		}
	}
	fmt.Println()

	if allOK {
		fmt.Println("All checks passed.")
		return nil
	}
	return fmt.Errorf("some checks failed")
}

// ---------- Run-tests 模式 ----------

// runProjectTestsMode 运行探测到的项目测试套件，并把真实的 pass/fail 作为
// deterministic 证据（CheckNameTestRun）写入 checklog。由 forge 自己执行命令
// 并观察退出码，故记录不可伪造——对抗 agent 不跑测试就声称 PASS 的盲区。区别
// 于默认完整性检查（文件存在性）和 regression 模式（forge 自身的 e2e）：这里
// 跑的是项目方的测试。
func runProjectTestsMode() error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	return runProjectTestsModeAt(root)
}

// runProjectTestsModeAt 是 runProjectTestsMode 的 root 注入核心，拆出来便于
// 在临时 project 上做单元测试（不依赖 findProjectRoot）。
func runProjectTestsModeAt(root string) error {
	cmdStr := taskpipeline.DetectTestCommand(root)
	if cmdStr == "" {
		fmt.Println("未检测到项目测试命令（无 go.mod / Cargo.toml / package.json / pytest 配置）—— 无可运行的测试套件。")
		return nil
	}
	// 用 CurrentSessionID()（而非空串），让记录通过 session-scoped
	// active-task-ref 文件归到本 session 的 active task 上。空 sessionID 路径
	// 读的是 shared legacy DataDir/active-task-ref，可能残留前一个 session 的
	// 旧 ref（例如 fix/concurrent-session-race 遗留），把证据错归到错误 task，
	// `forge trace <real-task>` 也看不到。与 review.go/task.go 一致。
	taskRef := taskpipeline.ReadActiveTaskRef(root, taskpipeline.CurrentSessionID())
	fmt.Printf("运行测试套件: %s\n", cmdStr)
	passed, output := taskpipeline.RunTestCommand(root, cmdStr)
	recErr := checklog.Record(root, &checklog.Entry{
		Check:   taskpipeline.CheckNameTestRun,
		Passed:  passed,
		Checked: true,
		TaskRef: taskRef,
		Detail:  fmt.Sprintf("%s — %s", cmdStr, passFailWord(passed)),
	})
	if recErr != nil {
		fmt.Fprintf(os.Stderr, "⚠ checklog 记录失败（证据未落盘）: %v\n", recErr)
	}
	if passed {
		if recErr != nil {
			fmt.Printf("✅ 测试通过（⚠ checklog 记录失败: %v，证据未落盘）\n", recErr)
		} else {
			fmt.Printf("✅ 测试通过 — 真实结果已记为 deterministic 证据（checklog: %s）\n", taskpipeline.CheckNameTestRun)
		}
		return nil
	}
	fmt.Printf("❌ 测试失败 — 失败结果已记入 checklog：\n%s\n", boundOutput(output))
	return fmt.Errorf("test suite failed")
}

// passFailWord 根据测试运行结果返回 PASS/FAIL——用在 checklog Detail 里，让
// forge trace 一眼能看出套件结果。
func passFailWord(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

// boundOutput 把命令输出截到末尾约 40 行，避免失败套件刷屏、同时保留可操作的
// 尾部（go/cargo/npm test 的失败信息都在末尾）。
func boundOutput(s string) string {
	if s == "" {
		return "(no output)"
	}
	lines := splitLines(s)
	const capLines = 40
	if len(lines) <= capLines {
		return s
	}
	trimmed := strings.Join(lines[len(lines)-capLines:], "\n")
	return fmt.Sprintf("...(省略前 %d 行)...\n%s", len(lines)-capLines, trimmed)
}

func checkHooks(root string) checkResult {
	// user-level-assets 之后 hook 副本的正主在 DataDir/hooks/（嵌入脚本的参考
	// 副本；运行时从不从项目读）。团队模式兼容：项目级 .forge/hooks/ 副本也认——
	// 那些项目刻意保留项目级资产。
	userHooksDir := filepath.Join(forgedata.DataDirFor(root), "hooks")
	projectHooksDir := filepath.Join(root, ".forge", "hooks")
	missing := []string{}
	for _, name := range hooks.HookNames() {
		if _, err := os.Stat(filepath.Join(userHooksDir, name)); err == nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(projectHooksDir, name)); err == nil {
			continue // 团队模式项目级副本
		}
		missing = append(missing, name)
	}
	if len(missing) == 0 {
		return checkResult{name: "Hook 脚本", ok: true, msg: fmt.Sprintf("全部 %d 个 hook 存在", len(hooks.HookNames()))}
	}
	return checkResult{name: "Hook 脚本", ok: false, msg: fmt.Sprintf("缺少: %v", missing)}
}

func checkProtocol(root string) checkResult {
	proto, err := protocol.Load(root)
	if err != nil {
		return checkResult{name: "Protocol 配置", ok: false, msg: err.Error()}
	}
	if proto.Scoring == nil {
		return checkResult{name: "Protocol 配置", ok: false, msg: "缺少 scoring 配置"}
	}
	return checkResult{name: "Protocol 配置", ok: true, msg: fmt.Sprintf("%d standards, %d session_rules", len(proto.Standards), len(proto.SessionRules))}
}

func checkQualitySkill(root string) checkResult {
	// user-level-assets 之后质量 skill 在用户级：<ClaudeHome>/skills/forge-quality/
	// SKILL.md（ClaudeHome 尊重 CLAUDE_CONFIG_DIR）。团队模式兼容：项目级
	// .claude/skills/forge-quality/SKILL.md 也认。
	if home := hooks.ClaudeHome(); home != `` {
		if _, err := os.Stat(filepath.Join(home, "skills", "forge-quality", "SKILL.md")); err == nil {
			return checkResult{name: "Quality Skill", ok: true}
		}
	}
	p := filepath.Join(root, ".claude", "skills", "forge-quality", "SKILL.md")
	if _, err := os.Stat(p); err == nil {
		return checkResult{name: "Quality Skill", ok: true, msg: "项目级副本（团队模式）"}
	}
	return checkResult{name: "Quality Skill", ok: false, msg: "用户级 skills/forge-quality/SKILL.md 不存在"}
}

func checkSettings(root string) checkResult {
	// user-level-assets 之后 forge hook 注册在用户级 Claude settings.json
	// （GenerateUserSettings）——或由已 user-level 安装的 Claude plugin 接管
	// （plugin.json 全机器注册 ForgeHookSpec，无需 settings 文件）。团队模式
	// 兼容：项目级 .claude/settings.local.json 也认。
	if hooks.IsClaudePluginInstalled() {
		return checkResult{name: "Settings 配置", ok: true, msg: "forge plugin 已 user-level 接管 hooks"}
	}
	if home := hooks.ClaudeHome(); home != `` {
		for _, name := range []string{"settings.json", "settings.local.json"} {
			p := filepath.Join(home, name)
			if data, err := os.ReadFile(p); err == nil && containsForgeHook(data) {
				return checkResult{name: "Settings 配置", ok: true, msg: "用户级 " + name + " 含 forge hook"}
			}
		}
	}
	p := filepath.Join(root, ".claude", "settings.local.json")
	if _, err := os.Stat(p); err == nil {
		return checkResult{name: "Settings 配置", ok: true, msg: "项目级 settings.local.json（团队模式）"}
	}
	return checkResult{name: "Settings 配置", ok: false, msg: "用户级 claude settings.json 不存在或不含 forge hook"}
}

// containsForgeHook 报告 settings JSON 是否注册了 forge hook（ForgeHookSpec
// 写入的命令都以 "forge hook"/"forge gate" 开头）。
func containsForgeHook(data []byte) bool {
	s := string(data)
	return strings.Contains(s, "forge hook ") || strings.Contains(s, "forge gate ")
}

// ---------- Regression 模式 ----------

func runRegressionMode(scenario string) error {
	// 把 forge 二进制构建到临时位置
	tmpDir, err := os.MkdirTemp("", "forge-verify-build")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "forge")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	repoRoot := findRepoRoot()
	buildCmd := exec.Command("go", "build", "-o", binPath, "./cmd/forge/")
	buildCmd.Dir = repoRoot
	buildCmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := buildCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to build forge binary: %v\n%s", err, output)
	}

	// 收集要运行的 scenario
	scenarios := map[string]func(string) ScenarioResult{
		"fresh-install":   runScenarioFreshInstall,
		"master-reminder": runScenarioMasterReminder,
		"upgrade-v040":    runScenarioUpgradeV040,
		"upgrade-v030":    runScenarioUpgradeV030,
	}

	if scenario != "" {
		fn, ok := scenarios[scenario]
		if !ok {
			return fmt.Errorf("unknown scenario: %s\navailable: fresh-install, master-reminder, upgrade-v040, upgrade-v030", scenario)
		}
		fmt.Printf("Running scenario: %s\n\n", scenario)
		result := fn(binPath)
		printScenarioResult(result)
		if !result.Passed {
			return fmt.Errorf("scenario %s failed", scenario)
		}
		return nil
	}

	// 运行全部 scenario
	fmt.Println("Forge E2E Regression Tests")
	fmt.Println()

	results := make([]ScenarioResult, 0, len(scenarios))
	// deterministic 顺序
	order := []string{"fresh-install", "master-reminder", "upgrade-v040", "upgrade-v030"}
	for _, name := range order {
		fn := scenarios[name]
		fmt.Printf("  Running %-25s ", name+"...")
		start := time.Now()
		result := fn(binPath)
		result.Duration = time.Since(start)
		results = append(results, result)

		status := "PASS"
		if !result.Passed {
			status = "FAIL"
		}
		fmt.Printf("[%s] %s\n", status, result.Duration.Round(time.Millisecond))
		if !result.Passed {
			// 输出缩进以提高可读性
			for _, line := range splitLines(result.Output) {
				fmt.Printf("    %s\n", line)
			}
		}
	}

	fmt.Println()
	passed := 0
	failed := 0
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	fmt.Printf("Results: %d passed, %d failed\n", passed, failed)

	if failed > 0 {
		return fmt.Errorf("%d scenario(s) failed", failed)
	}
	return nil
}

func printScenarioResult(r ScenarioResult) {
	status := "PASS"
	if !r.Passed {
		status = "FAIL"
	}
	fmt.Printf("  [%s] %s (%s)\n", status, r.Name, r.Duration.Round(time.Millisecond))
	if r.Output != "" {
		fmt.Println()
		fmt.Println(r.Output)
	}
}

func findRepoRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

func splitLines(s string) []string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	return slices.DeleteFunc(lines, func(l string) bool { return l == "" })
}
