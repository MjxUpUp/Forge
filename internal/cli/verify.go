package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/Harness/forge/internal/hooks"
	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/protocol"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(verifyCmd)
	verifyCmd.Flags().Bool("regression", false, "运行所有 E2E 回归场景")
	verifyCmd.Flags().String("scenario", "", "运行指定场景 (fresh-install, master-reminder, experience-flow, upgrade-v040, upgrade-v030)")
}

var verifyCmd = &cobra.Command{
	Use:   "verify [--regression] [--scenario <name>]",
	Short: "验证项目完整性和运行回归测试",
	Long: `forge verify 检查当前项目的 Forge 配置完整性：
  - pipeline.yml 结构验证
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

	if regression || scenario != "" {
		return runRegressionMode(scenario)
	}

	return runDefaultChecks()
}

// ---------- Default mode ----------

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
		{"Pipeline 验证", checkPipeline},
		{"Hook 脚本", checkHooks},
		{"Protocol 配置", checkProtocol},
		{"Quality Skill", checkQualitySkill},
		{"Pipeline Skill", checkPipelineSkill},
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

	// Print results
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

func checkPipeline(root string) checkResult {
	errs := pipeline.ValidateOnly(root)
	if len(errs) == 0 {
		return checkResult{name: "Pipeline 验证", ok: true, msg: "结构正确"}
	}
	msg := fmt.Sprintf("%d 个错误", len(errs))
	if len(errs) == 1 {
		msg = errs[0].Error()
	}
	return checkResult{name: "Pipeline 验证", ok: false, msg: msg}
}

func checkHooks(root string) checkResult {
	hooksDir := filepath.Join(root, ".forge", "hooks")
	missing := []string{}
	for _, name := range hooks.HookNames() {
		p := filepath.Join(hooksDir, name)
		if _, err := os.Stat(p); err != nil {
			missing = append(missing, name)
		}
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
	p := filepath.Join(root, ".claude", "skills", "forge-quality", "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		return checkResult{name: "Quality Skill", ok: false, msg: ".claude/skills/forge-quality/SKILL.md 不存在"}
	}
	return checkResult{name: "Quality Skill", ok: true}
}

func checkPipelineSkill(root string) checkResult {
	p := filepath.Join(root, ".claude", "skills", "forge-pipeline", "SKILL.md")
	if _, err := os.Stat(p); err != nil {
		return checkResult{name: "Pipeline Skill", ok: false, msg: ".claude/skills/forge-pipeline/SKILL.md 不存在"}
	}
	return checkResult{name: "Pipeline Skill", ok: true}
}

func checkSettings(root string) checkResult {
	p := filepath.Join(root, ".claude", "settings.local.json")
	if _, err := os.Stat(p); err != nil {
		return checkResult{name: "Settings 配置", ok: false, msg: ".claude/settings.local.json 不存在"}
	}
	return checkResult{name: "Settings 配置", ok: true}
}

// ---------- Regression mode ----------

func runRegressionMode(scenario string) error {
	// Build forge binary to temp location
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

	// Collect scenarios to run
	scenarios := map[string]func(string) ScenarioResult{
		"fresh-install":    runScenarioFreshInstall,
		"master-reminder":  runScenarioMasterReminder,
		"experience-flow":  runScenarioExperienceFlow,
		"upgrade-v040":     runScenarioUpgradeV040,
		"upgrade-v030":     runScenarioUpgradeV030,
	}

	if scenario != "" {
		fn, ok := scenarios[scenario]
		if !ok {
			return fmt.Errorf("unknown scenario: %s\navailable: fresh-install, master-reminder, experience-flow, upgrade-v040, upgrade-v030", scenario)
		}
		fmt.Printf("Running scenario: %s\n\n", scenario)
		result := fn(binPath)
		printScenarioResult(result)
		if !result.Passed {
			return fmt.Errorf("scenario %s failed", scenario)
		}
		return nil
	}

	// Run all scenarios
	fmt.Println("Forge E2E Regression Tests")
	fmt.Println()

	results := make([]ScenarioResult, 0, len(scenarios))
	// Deterministic order
	order := []string{"fresh-install", "master-reminder", "experience-flow", "upgrade-v040", "upgrade-v030"}
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
			// Indent output for readability
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
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			line := s[start:i]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
			if line != "" {
				lines = append(lines, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		remaining := s[start:]
		if remaining != "" {
			lines = append(lines, remaining)
		}
	}
	return lines
}
