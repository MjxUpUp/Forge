package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ScenarioResult 持有单次 E2E scenario 运行的结果。
type ScenarioResult struct {
	Name     string
	Passed   bool
	Output   string
	Duration time.Duration
}

// ---------- scenario 实现 ----------

// runScenarioFreshInstall 验证从空目录的干净 init。
func runScenarioFreshInstall(forgeBin string) ScenarioResult {
	start := time.Now()
	var outputLines []string

	// 创建 temp dir 含 git + go 项目
	dir, err := os.MkdirTemp("", "forge-verify-fresh-*")
	if err != nil {
		return ScenarioResult{Name: "fresh-install", Passed: false, Output: err.Error(), Duration: time.Since(start)}
	}
	defer os.RemoveAll(dir)

	if output, err := verifyRunGit(dir, "init"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("git init failed: %v\n%s", err, output), start)
	}
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")

	// 写最小 go 项目
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// 跑 forge init
	if output, err := verifyRunForge(forgeBin, dir, "init"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("forge init failed: %v\n%s", err, output), start)
	}

	// 校验核心文件存在
	expectedFiles := []string{
		".forge",
		".forge/protocol.yml",
		".forge/hooks/auto-compile.sh",
		".forge/hooks/assertion-check.sh",
		".forge/hooks/task-verify.sh",
		".claude/settings.local.json",
		".claude/skills/forge-quality/SKILL.md",
	}
	for _, f := range expectedFiles {
		if !verifyFileExists(dir, f) {
			outputLines = append(outputLines, fmt.Sprintf("missing: %s", f))
		}
	}

	// 跑 forge status
	if output, err := verifyRunForge(forgeBin, dir, "status"); err != nil {
		outputLines = append(outputLines, fmt.Sprintf("forge status failed: %v\n%s", err, output))
	} else if !strings.Contains(output, "Project:") {
		outputLines = append(outputLines, "forge status output missing 'Project:'")
	}

	if len(outputLines) > 0 {
		return failResult("fresh-install", strings.Join(outputLines, "\n"), start)
	}
	return ScenarioResult{Name: "fresh-install", Passed: true, Duration: time.Since(start)}
}

// runScenarioMasterReminder 验证 task-verify hook 在 master 上改码时告警。
func runScenarioMasterReminder(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-master-*")
	if err != nil {
		return failResult("master-reminder", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// 准备：git init + go 项目 + forge init
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")
	if _, err := verifyRunForge(forgeBin, dir, "init"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("forge init failed: %v", err), start)
	}

	// 全部 commit，然后创建 feature branch
	verifyRunGit(dir, "add", ".")
	verifyRunGit(dir, "commit", "-m", "initial")
	verifyRunGit(dir, "checkout", "-b", "feature/EXP-1-test")

	// 启动 task、过门禁、complete
	if _, err := verifyRunForge(forgeBin, dir, "task", "start", "--ref", "EXP-1", "--title", "test experience"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("task start failed: %v", err), start)
	}
	if err := passAllVerifyGates(forgeBin, dir, "EXP-1"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("pass gates failed: %v", err), start)
	}
	if _, err := verifyRunForge(forgeBin, dir, "task", "complete", "--ref", "EXP-1"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("task complete failed: %v", err), start)
	}

	// 切回 master
	verifyRunGit(dir, "checkout", "master")

	// 创建源码文件、commit，再修改
	writeVerifyFile(dir, "foo.go", "package main\n\nfunc Foo() int { return 42 }\n")
	verifyRunGit(dir, "add", "foo.go")
	verifyRunGit(dir, "commit", "-m", "add foo.go")
	writeVerifyFile(dir, "foo.go", "package main\n\nfunc Foo() int { return 99 }\n")

	// 跑 task-verify hook
	hookPath := filepath.Join(dir, ".forge", "hooks", "task-verify.sh")
	binDir := filepath.Dir(forgeBin)
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, _ := cmd.CombinedOutput()
	outStr := string(output)

	if !strings.Contains(outStr, "without active task") {
		return failResult("master-reminder", fmt.Sprintf("hook should warn 'without active task', got: %q", outStr), start)
	}

	return ScenarioResult{Name: "master-reminder", Passed: true, Duration: time.Since(start)}
}

// runScenarioUpgradeV040 验证从 v0.4.0 类状态的升级。
func runScenarioUpgradeV040(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-v040-*")
	if err != nil {
		return failResult("upgrade-v040", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// 准备
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// 创建 v0.4.0 风格的 .forge/ 结构
	for _, d := range []string{".forge/hooks", ".forge/tasks", ".forge/gates"} {
		os.MkdirAll(filepath.Join(dir, d), 0755)
	}

	writeVerifyFile(dir, ".forge/pipeline.yml", `version: "2.0"
project: "old-project"
mode: medium

pipeline:
  gates:
    - id: gate-4-implement
      name: "Code Implementation"
      enabled: true
      depends_on: []
      hooks:
        - auto-compile.sh
        - assertion-check.sh
`)

	writeVerifyFile(dir, ".forge/state.json", `{
  "pipeline_version": "2.0",
  "mode": "medium",
  "current_gate": "",
  "started_at": "2025-01-01T00:00:00Z",
  "history": [],
  "overrides": [],
  "last_sync_version": "v0.4.0"
}`)

	// 用户自定义的 protocol.yml 含 scoring 配置
	writeVerifyFile(dir, ".forge/protocol.yml", `version: "1.0"
standards:
  - id: my-custom-standard
    name: "My Custom Standard"
    description: "A custom standard from v0.4.0"
    severity: error
    enabled: true
session_rules:
  - id: my-custom-rule
    trigger: always
    instruction: "Always follow my custom rule"
    mandatory: true
scoring:
  weights:
    process: 0.3
    testing: 0.2
    code-quality: 0.2
    assertions: 0.1
    scope: 0.1
    efficiency: 0.1
  thresholds:
    A: 90
    B: 80
    C: 70
    D: 60
    F: 0
`)

	// 旧 hook
	writeVerifyFile(dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old-auto-compile\n")
	writeVerifyFile(dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old-assertion-check\n")

	// 跑 forge status 触发 auto-sync
	if output, err := verifyRunForge(forgeBin, dir, "status"); err != nil {
		return failResult("upgrade-v040", fmt.Sprintf("forge status failed: %v\n%s", err, output), start)
	}

	var failures []string

	// 校验新 hook 存在
	for _, hook := range []string{
		".forge/hooks/auto-compile.sh",
		".forge/hooks/assertion-check.sh",
		".forge/hooks/task-verify.sh",
	} {
		if !verifyFileExists(dir, hook) {
			failures = append(failures, fmt.Sprintf("missing hook: %s", hook))
		}
	}

	// 校验 hook 已更新（不是旧内容）
	hookContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "hooks", "auto-compile.sh"))
	if strings.Contains(string(hookContent), "old-auto-compile") {
		failures = append(failures, "auto-compile.sh should have been overwritten")
	}

	// 校验 quality SKILL.md 已重生
	if !verifyFileExists(dir, ".claude/skills/forge-quality/SKILL.md") {
		failures = append(failures, "forge-quality SKILL.md should be regenerated")
	}

	// 校验 protocol.yml 未被覆盖
	protoContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "protocol.yml"))
	if !strings.Contains(string(protoContent), "my-custom-standard") {
		failures = append(failures, "protocol.yml should still contain user's custom standard")
	}

	if len(failures) > 0 {
		return failResult("upgrade-v040", strings.Join(failures, "\n"), start)
	}
	return ScenarioResult{Name: "upgrade-v040", Passed: true, Duration: time.Since(start)}
}

// runScenarioUpgradeV030 验证从 v0.3.0 类状态升级时 protocol 保留。
func runScenarioUpgradeV030(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-v030-*")
	if err != nil {
		return failResult("upgrade-v030", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// 准备
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// 创建 v0.3.0 类状态
	for _, d := range []string{".forge/hooks", ".forge/tasks", ".forge/gates"} {
		os.MkdirAll(filepath.Join(dir, d), 0755)
	}

	writeVerifyFile(dir, ".forge/pipeline.yml", `version: "2.0"
project: "old-project"
mode: medium

pipeline:
  gates:
    - id: gate-4-implement
      name: "Code Implementation"
      enabled: true
      depends_on: []
`)

	writeVerifyFile(dir, ".forge/state.json", `{
  "pipeline_version": "2.0",
  "mode": "medium",
  "current_gate": "",
  "started_at": "2025-01-01T00:00:00Z",
  "history": [],
  "overrides": [],
  "last_sync_version": "v0.3.0"
}`)

	// 用户 protocol 含自定义 standards
	writeVerifyFile(dir, ".forge/protocol.yml", `version: "1.0"
standards:
  - id: no-console-log
    name: "No console.log"
    description: "Production code must not contain console.log statements"
    severity: error
    enabled: true
  - id: require-error-handling
    name: "Error handling required"
    description: "All public functions must handle errors"
    severity: warning
    enabled: true
session_rules:
  - id: review-before-merge
    trigger: always
    instruction: "Always review code before merge"
    mandatory: true
scoring:
  weights:
    process: 0.25
    testing: 0.25
    code-quality: 0.20
    assertions: 0.15
    scope: 0.10
    efficiency: 0.05
  thresholds:
    A: 90
    B: 80
    C: 70
    D: 60
    F: 0
`)

	// 旧 hook
	writeVerifyFile(dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old\n")
	writeVerifyFile(dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old\n")

	// 跑 forge status 触发 auto-sync
	if output, err := verifyRunForge(forgeBin, dir, "status"); err != nil {
		return failResult("upgrade-v030", fmt.Sprintf("forge status failed: %v\n%s", err, output), start)
	}

	var failures []string

	// 校验 protocol.yml 仍含用户自定义 standards
	protoContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "protocol.yml"))
	for _, needle := range []string{"no-console-log", "require-error-handling", "review-before-merge"} {
		if !strings.Contains(string(protoContent), needle) {
			failures = append(failures, fmt.Sprintf("protocol.yml should still contain '%s'", needle))
		}
	}

	// 校验 hook 已更新
	for _, hook := range []string{".forge/hooks/auto-compile.sh", ".forge/hooks/task-verify.sh"} {
		if verifyFileExists(dir, hook) {
			content, _ := os.ReadFile(filepath.Join(dir, hook))
			if strings.Contains(string(content), "echo old\n") {
				failures = append(failures, fmt.Sprintf("%s should have been updated", hook))
			}
		}
	}

	// 校验 settings.local.json 存在
	if !verifyFileExists(dir, ".claude/settings.local.json") {
		failures = append(failures, "settings.local.json should exist after auto-sync")
	}

	// 校验 quality SKILL.md 存在
	if !verifyFileExists(dir, ".claude/skills/forge-quality/SKILL.md") {
		failures = append(failures, "forge-quality SKILL.md should exist after auto-sync")
	}

	if len(failures) > 0 {
		return failResult("upgrade-v030", strings.Join(failures, "\n"), start)
	}
	return ScenarioResult{Name: "upgrade-v030", Passed: true, Duration: time.Since(start)}
}

// ---------- 辅助函数 ----------

func failResult(name, output string, start time.Time) ScenarioResult {
	return ScenarioResult{Name: name, Passed: false, Output: output, Duration: time.Since(start)}
}

// verifyRunForge 在指定目录执行 forge 命令，返 (output, error)。
func verifyRunForge(forgeBin, dir string, args ...string) (string, error) {
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// verifyRunGit 在指定目录执行 git 命令。
func verifyRunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// writeVerifyFile 在 dir 内写入内容到文件。
func writeVerifyFile(dir, name, content string) {
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

// verifyFileExists 检查文件或目录是否存在。
func verifyFileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// passAllVerifyGates 为给定 task ref 通过全部 3 个 task gate（v0.17：从 5 个精简）。
func passAllVerifyGates(forgeBin, dir, ref string) error {
	// 这些回归 scenario 关闭 gate 时序约束（gate 快速连续通过）。
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	// commit 让 HEAD 超过 base branch——task-implement 的
	// 代码变更检查要求 feature branch 有新 commit。
	verifyRunGit(dir, "commit", "--allow-empty", "-m", "verify: move HEAD for task-implement")

	for _, g := range []string{"task-implement", "task-verify", "task-complete"} {
		out, err := verifyRunForge(forgeBin, dir, "task", "gate", g, "--ref", ref)
		if err != nil {
			return fmt.Errorf("gate %s failed: %v\n%s", g, err, out)
		}
	}
	return nil
}

// findVerifyRepoRoot 向上查 go.mod——用于构建 binary。
// 与 verify.go 的 findRepoRoot 同名分离避免冲突。
// 两者功能相同，本版本失败时返 "."。
