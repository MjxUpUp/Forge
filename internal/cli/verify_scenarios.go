package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/experience"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// ScenarioResult holds the outcome of a single E2E scenario run.
type ScenarioResult struct {
	Name     string
	Passed   bool
	Output   string
	Duration time.Duration
}

// ---------- Scenario implementations ----------

// runScenarioFreshInstall verifies a clean init from an empty directory.
func runScenarioFreshInstall(forgeBin string) ScenarioResult {
	start := time.Now()
	var outputLines []string

	// Create temp dir with git + go project
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

	// Write minimal go project
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// Run forge init
	if output, err := verifyRunForge(forgeBin, dir, "init"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("forge init failed: %v\n%s", err, output), start)
	}

	// Verify core files exist
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

	// Run forge status
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

// runScenarioMasterReminder verifies task-verify hook warns about code changes on master.
func runScenarioMasterReminder(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-master-*")
	if err != nil {
		return failResult("master-reminder", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// Setup: git init + go project + forge init
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")
	if _, err := verifyRunForge(forgeBin, dir, "init"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("forge init failed: %v", err), start)
	}

	// Commit everything, then create feature branch
	verifyRunGit(dir, "add", ".")
	verifyRunGit(dir, "commit", "-m", "initial")
	verifyRunGit(dir, "checkout", "-b", "feature/EXP-1-test")

	// Start task, pass gates, complete
	if _, err := verifyRunForge(forgeBin, dir, "task", "start", "--ref", "EXP-1", "--title", "test experience"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("task start failed: %v", err), start)
	}
	if err := passAllVerifyGates(forgeBin, dir, "EXP-1"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("pass gates failed: %v", err), start)
	}
	if _, err := verifyRunForge(forgeBin, dir, "task", "complete", "--ref", "EXP-1"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("task complete failed: %v", err), start)
	}

	// Switch back to master
	verifyRunGit(dir, "checkout", "master")

	// Create a source code file, commit, then modify it
	writeVerifyFile(dir, "foo.go", "package main\n\nfunc Foo() int { return 42 }\n")
	verifyRunGit(dir, "add", "foo.go")
	verifyRunGit(dir, "commit", "-m", "add foo.go")
	writeVerifyFile(dir, "foo.go", "package main\n\nfunc Foo() int { return 99 }\n")

	// Run task-verify hook
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

// runScenarioExperienceFlow verifies the full experience lifecycle.
func runScenarioExperienceFlow(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-exp-*")
	if err != nil {
		return failResult("experience-flow", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// Setup
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")
	if _, err := verifyRunForge(forgeBin, dir, "init"); err != nil {
		return failResult("experience-flow", fmt.Sprintf("forge init failed: %v", err), start)
	}
	verifyRunGit(dir, "add", ".")
	verifyRunGit(dir, "commit", "-m", "initial")
	verifyRunGit(dir, "checkout", "-b", "feature/EXP-1-test")

	// Start task
	if _, err := verifyRunForge(forgeBin, dir, "task", "start", "--ref", "EXP-1", "--title", "test experience"); err != nil {
		return failResult("experience-flow", fmt.Sprintf("task start failed: %v", err), start)
	}

	// Pass all gates
	if err := passAllVerifyGates(forgeBin, dir, "EXP-1"); err != nil {
		return failResult("experience-flow", fmt.Sprintf("pass gates failed: %v", err), start)
	}

	// Complete task
	if output, err := verifyRunForge(forgeBin, dir, "task", "complete", "--ref", "EXP-1"); err != nil {
		return failResult("experience-flow", fmt.Sprintf("task complete failed: %v\n%s", err, output), start)
	}

	// Check task score exists
	// task state migrated to user-level DataDir (refactor-data-home);
	// the forge subprocess writes tasks/EXP-1.json there for git projects.
	taskData, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(dir), "tasks", "EXP-1.json"))
	if err != nil {
		return failResult("experience-flow", fmt.Sprintf("task file missing: %v", err), start)
	}
	var taskMap map[string]interface{}
	if err := json.Unmarshal(taskData, &taskMap); err != nil {
		return failResult("experience-flow", fmt.Sprintf("task file parse error: %v", err), start)
	}
	if taskMap["score"] == nil {
		return failResult("experience-flow", "task should have a score after complete", start)
	}

	// Create a mock proposed rule via the store so it lands in the same
	// user-level DataDir the forge subprocess resolves (ProjectFor(dir) derives
	// the same key + DataDir the accept subprocess will read from).
	proj, perr := forgedata.ProjectFor(dir)
	if perr != nil {
		return failResult("experience-flow", fmt.Sprintf("project not resolved: %v", perr), start)
	}
	proposal := &experience.ExperienceProposal{
		ID:           "exp-test001",
		SourceReview: "EXP-1",
		Category:     "gotchas",
		Title:        "Test gotcha rule",
		Description:  "A test experience rule for E2E verification",
		Patterns:     []string{"test\\.Fatal\\("},
		Severity:     "error",
		Status:       experience.PropProposed,
		CreatedAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	if err := experience.SaveProposal(proj, proposal); err != nil {
		return failResult("experience-flow", fmt.Sprintf("seed proposal: %v", err), start)
	}

	// Accept the proposal with isolated HOME
	tmpHome, _ := os.MkdirTemp("", "forge-verify-home-*")
	defer os.RemoveAll(tmpHome)

	cmd := exec.Command(forgeBin, "experience", "accept", "exp-test001")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome, "USERPROFILE="+tmpHome)
	if output, err := cmd.CombinedOutput(); err != nil {
		return failResult("experience-flow", fmt.Sprintf("experience accept failed: %v\n%s", err, output), start)
	}

	// Verify proposal status = accepted
	accepted, err := experience.LoadProposal(proj, "exp-test001")
	if err != nil {
		return failResult("experience-flow", fmt.Sprintf("load proposal: %v", err), start)
	}
	if accepted.Status != experience.PropAccepted {
		return failResult("experience-flow", fmt.Sprintf("proposal status should be 'accepted', got %q", accepted.Status), start)
	}

	// Verify knowledge store has entry
	knowledgeIdx := filepath.Join(tmpHome, ".forge", "knowledge", "index.json")
	kData, err := os.ReadFile(knowledgeIdx)
	if err != nil {
		return failResult("experience-flow", fmt.Sprintf("knowledge index missing at %s: %v", knowledgeIdx, err), start)
	}
	if !strings.Contains(string(kData), "exp-test001") {
		return failResult("experience-flow", fmt.Sprintf("knowledge index should contain exp-test001, got: %s", string(kData)), start)
	}

	return ScenarioResult{Name: "experience-flow", Passed: true, Duration: time.Since(start)}
}

// runScenarioUpgradeV040 verifies upgrading from a v0.4.0-like state.
func runScenarioUpgradeV040(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-v040-*")
	if err != nil {
		return failResult("upgrade-v040", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// Setup
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// Create v0.4.0-like .forge/ structure
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

	// User's customized protocol.yml with scoring config
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

	// Old hooks
	writeVerifyFile(dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old-auto-compile\n")
	writeVerifyFile(dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old-assertion-check\n")

	// Run forge status to trigger auto-sync
	if output, err := verifyRunForge(forgeBin, dir, "status"); err != nil {
		return failResult("upgrade-v040", fmt.Sprintf("forge status failed: %v\n%s", err, output), start)
	}

	var failures []string

	// Verify new hooks exist
	for _, hook := range []string{
		".forge/hooks/auto-compile.sh",
		".forge/hooks/assertion-check.sh",
		".forge/hooks/task-verify.sh",
	} {
		if !verifyFileExists(dir, hook) {
			failures = append(failures, fmt.Sprintf("missing hook: %s", hook))
		}
	}

	// Verify hooks were updated (not old content)
	hookContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "hooks", "auto-compile.sh"))
	if strings.Contains(string(hookContent), "old-auto-compile") {
		failures = append(failures, "auto-compile.sh should have been overwritten")
	}

	// Verify quality SKILL.md regenerated
	if !verifyFileExists(dir, ".claude/skills/forge-quality/SKILL.md") {
		failures = append(failures, "forge-quality SKILL.md should be regenerated")
	}

	// Verify protocol.yml NOT overwritten
	protoContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "protocol.yml"))
	if !strings.Contains(string(protoContent), "my-custom-standard") {
		failures = append(failures, "protocol.yml should still contain user's custom standard")
	}

	if len(failures) > 0 {
		return failResult("upgrade-v040", strings.Join(failures, "\n"), start)
	}
	return ScenarioResult{Name: "upgrade-v040", Passed: true, Duration: time.Since(start)}
}

// runScenarioUpgradeV030 verifies upgrading from a v0.3.0-like state preserves protocol.
func runScenarioUpgradeV030(forgeBin string) ScenarioResult {
	start := time.Now()

	dir, err := os.MkdirTemp("", "forge-verify-v030-*")
	if err != nil {
		return failResult("upgrade-v030", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// Setup
	verifyRunGit(dir, "init")
	verifyRunGit(dir, "config", "user.email", "test@example.com")
	verifyRunGit(dir, "config", "user.name", "Test")
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// Create v0.3.0-like state
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

	// User's protocol with custom standards
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

	// Old hooks
	writeVerifyFile(dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old\n")
	writeVerifyFile(dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old\n")

	// Run forge status to trigger auto-sync
	if output, err := verifyRunForge(forgeBin, dir, "status"); err != nil {
		return failResult("upgrade-v030", fmt.Sprintf("forge status failed: %v\n%s", err, output), start)
	}

	var failures []string

	// Verify protocol.yml still has user's custom standards
	protoContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "protocol.yml"))
	for _, needle := range []string{"no-console-log", "require-error-handling", "review-before-merge"} {
		if !strings.Contains(string(protoContent), needle) {
			failures = append(failures, fmt.Sprintf("protocol.yml should still contain '%s'", needle))
		}
	}

	// Verify hooks were updated
	for _, hook := range []string{".forge/hooks/auto-compile.sh", ".forge/hooks/task-verify.sh"} {
		if verifyFileExists(dir, hook) {
			content, _ := os.ReadFile(filepath.Join(dir, hook))
			if strings.Contains(string(content), "echo old\n") {
				failures = append(failures, fmt.Sprintf("%s should have been updated", hook))
			}
		}
	}

	// Verify settings.local.json exists
	if !verifyFileExists(dir, ".claude/settings.local.json") {
		failures = append(failures, "settings.local.json should exist after auto-sync")
	}

	// Verify quality SKILL.md exists
	if !verifyFileExists(dir, ".claude/skills/forge-quality/SKILL.md") {
		failures = append(failures, "forge-quality SKILL.md should exist after auto-sync")
	}

	if len(failures) > 0 {
		return failResult("upgrade-v030", strings.Join(failures, "\n"), start)
	}
	return ScenarioResult{Name: "upgrade-v030", Passed: true, Duration: time.Since(start)}
}

// ---------- Helpers ----------

func failResult(name, output string, start time.Time) ScenarioResult {
	return ScenarioResult{Name: name, Passed: false, Output: output, Duration: time.Since(start)}
}

// verifyRunForge executes a forge command in the given directory and returns (output, error).
func verifyRunForge(forgeBin, dir string, args ...string) (string, error) {
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// verifyRunGit executes a git command in the given directory.
func verifyRunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// writeVerifyFile writes a file with content inside dir.
func writeVerifyFile(dir, name, content string) {
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

// verifyFileExists checks if a file or directory exists.
func verifyFileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// passAllVerifyGates passes all 3 task gates (v0.17: reduced from 5) for the given task ref.
func passAllVerifyGates(forgeBin, dir, ref string) error {
	// Disable gate timing for these regression scenarios (gates pass in rapid sequence).
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	// Commit so HEAD moves ahead of the base branch — task-implement's
	// code-change check requires a new commit on the feature branch.
	verifyRunGit(dir, "commit", "--allow-empty", "-m", "verify: move HEAD for task-implement")

	for _, g := range []string{"task-implement", "task-verify", "task-complete"} {
		out, err := verifyRunForge(forgeBin, dir, "task", "gate", g, "--ref", ref)
		if err != nil {
			return fmt.Errorf("gate %s failed: %v\n%s", g, err, out)
		}
	}
	return nil
}

// findVerifyRepoRoot walks up to find the go.mod — used to build the binary.
// Kept separate from findRepoRoot in verify.go to avoid name collision.
// Both do the same thing but this version returns "." on failure.
