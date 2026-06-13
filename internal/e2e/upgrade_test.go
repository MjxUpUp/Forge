// Package e2e contains multi-perspective end-to-end tests for Forge.
// These tests verify different user scenarios: fresh install, version upgrades,
// master branch warnings, and experience flow — all via subprocess invocations
// of the compiled forge binary.
package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var forgeBin string

func TestMain(m *testing.M) {
	// Build forge binary once for all tests.
	tmpDir, err := os.MkdirTemp("", "forge-e2e-build")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to create temp build dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	binPath := filepath.Join(tmpDir, "forge")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}

	cmd := exec.Command("go", "build", "-o", binPath, "./cmd/forge/")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: failed to build forge binary: %v\n%s\n", err, output)
		os.Exit(1)
	}

	forgeBin = binPath
	os.Exit(m.Run())
}

// repoRoot returns the forge repository root (the directory containing go.mod).
func repoRoot() string {
	// Walk up from current file to find go.mod.
	dir := "."
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			abs, _ := filepath.Abs(dir)
			return abs
		}
		dir = filepath.Join(dir, "..")
	}
	panic("cannot find repo root (go.mod)")
}

// forge runs a forge command in the given working directory.
func forge(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forge %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// forgeErr runs a forge command and returns (stdout+stderr, error).
// It does NOT fatal on non-zero exit — the caller decides.
func forgeErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// git runs a git command in the given working directory.
func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\noutput: %s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// gitErr runs a git command and returns (output, error) without fatalling.
func gitErr(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// initGoProject creates a minimal Go project in dir.
func initGoProject(t *testing.T, dir string) {
	t.Helper()
	goMod := `module example.com/test

go 1.24
`
	writeFile(t, dir, "go.mod", goMod)
	writeFile(t, dir, "main.go", "package main\n\nfunc main() {}\n")
}

// writeFile writes a file with content inside dir.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// fileExists checks if a file (or directory) exists.
func fileExists(t *testing.T, dir, name string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// readFile reads a file inside dir.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("failed to read %s: %v", name, err)
	}
	return string(data)
}

// readJSON reads a JSON file inside dir into target.
func readJSON(t *testing.T, dir, name string, target interface{}) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("failed to read %s: %v", name, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("failed to parse %s: %v", name, err)
	}
}

// freshProject creates a temp dir with git init + go project + forge init.
// Returns the project directory.
func freshProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)
	forge(t, dir, "init")
	return dir
}

// freshProjectOnBranch creates a fresh project on a feature branch.
func freshProjectOnBranch(t *testing.T, branch string) string {
	t.Helper()
	dir := freshProject(t)
	// Commit everything so we can branch
	git(t, dir, "add", ".")
	git(t, dir, "commit", "-m", "initial")
	git(t, dir, "checkout", "-b", branch)
	return dir
}

// ---------- Test Scenarios ----------

// TestFreshInstall verifies a clean init from an empty directory.
func TestFreshInstall(t *testing.T) {
	dir := freshProject(t)

	// Verify core files exist.
	for _, path := range []string{
		".forge",
		".forge/pipeline.yml",
		".forge/state.json",
		".forge/protocol.yml",
		".forge/hooks/auto-compile.sh",
		".forge/hooks/assertion-check.sh",
		".forge/hooks/experience-check.sh",
		".forge/hooks/task-verify.sh",
		".claude/settings.local.json",
		".claude/skills/forge-pipeline/SKILL.md",
		".claude/skills/forge-quality/SKILL.md",
	} {
		if !fileExists(t, dir, path) {
			t.Errorf("expected %s to exist after forge init", path)
		}
	}

	// forge status should succeed.
	out := forge(t, dir, "status")
	if !strings.Contains(out, "Project:") {
		t.Errorf("forge status output should contain 'Project:', got: %s", out)
	}

	// forge experience list should show "(none)" for both sections.
	out = forge(t, dir, "experience", "list")
	if !strings.Contains(out, "(none)") {
		t.Errorf("forge experience list should show '(none)' for fresh project, got: %s", out)
	}
}

// TestMasterBranchReminder verifies the task-verify hook warns about
// code changes on master without an active task.
func TestMasterBranchReminder(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/EXP-1-test")

	// Start task, pass all 3 gates, complete.
	forge(t, dir, "task", "start", "--ref", "EXP-1", "--title", "test experience")
	passAllGates(t, dir, "EXP-1")
	forge(t, dir, "task", "complete", "--ref", "EXP-1")

	// Switch back to master.
	git(t, dir, "checkout", "master")

	// Create a source code file, commit it, then modify it.
	// This ensures git diff shows tracked-but-modified code changes.
	writeFile(t, dir, "foo.go", "package main\n\nfunc Foo() int { return 42 }\n")
	git(t, dir, "add", "foo.go")
	git(t, dir, "commit", "-m", "add foo.go")
	// Now modify it so it appears in git diff.
	writeFile(t, dir, "foo.go", "package main\n\nfunc Foo() int { return 99 }\n")

	// Run task-verify hook directly.
	// The hook uses forge internally, so we need forge on PATH.
	hookPath := filepath.Join(dir, ".forge", "hooks", "task-verify.sh")
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	binDir := filepath.Dir(forgeBin)
	cmd.Env = append(os.Environ(), "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	output, _ := cmd.CombinedOutput()
	outStr := string(output)

	// On master with modified (uncommitted) code changes and no active task,
	// the hook should warn about code changes without an active task.
	if !strings.Contains(outStr, "without active task") {
		t.Errorf("task-verify hook should warn 'without active task' on master with code changes, got: %q", outStr)
	}
}

// TestExperienceFlow verifies the full experience lifecycle:
// start task -> pass gates -> complete (score) -> review -> propose -> accept.
func TestExperienceFlow(t *testing.T) {
	dir := freshProjectOnBranch(t, "feature/EXP-1-test")

	// Start task.
	forge(t, dir, "task", "start", "--ref", "EXP-1", "--title", "test experience")

	// Pass all 3 gates.
	passAllGates(t, dir, "EXP-1")

	// Complete task — this auto-scores.
	out, err := forgeErr(t, dir, "task", "complete", "--ref", "EXP-1")
	if err != nil {
		t.Fatalf("forge task complete failed: %v\noutput: %s", err, out)
	}

	// Load task state to check score.
	type taskState struct {
		TaskRef     string      `json:"task_ref"`
		Score       interface{} `json:"score"`
		CompletedAt interface{} `json:"completed_at"`
	}
	var ts taskState
	readJSON(t, dir, ".forge/tasks/EXP-1.json", &ts)
	if ts.Score == nil {
		t.Fatal("expected task to have a score after complete")
	}

	// Check if review was created (depends on score < 80).
	type scoreResult struct {
		Overall float64 `json:"overall"`
		Grade   string  `json:"grade"`
	}
	type taskStateFull struct {
		Score *scoreResult `json:"score"`
	}
	var tsFull taskStateFull
	readJSON(t, dir, ".forge/tasks/EXP-1.json", &tsFull)

	if tsFull.Score != nil && tsFull.Score.Overall < 80 {
		// Review should have been created.
		if !fileExists(t, dir, ".forge/reviews/EXP-1.json") {
			t.Error("expected review file for low-scoring task")
		}
	}

	// Create a mock proposed rule in .forge/experience/proposed/.
	proposalDir := filepath.Join(dir, ".forge", "experience", "proposed")
	if err := os.MkdirAll(proposalDir, 0755); err != nil {
		t.Fatal(err)
	}
	proposal := map[string]interface{}{
		"id":            "exp-test001",
		"source_review": "EXP-1",
		"category":      "gotchas",
		"title":         "Test gotcha rule",
		"description":   "A test experience rule for E2E verification",
		"patterns":      []string{"test\\.Fatal\\("},
		"severity":      "error",
		"status":        "proposed",
		"created_at":    "2025-01-01T00:00:00Z",
	}
	proposalJSON, _ := json.MarshalIndent(proposal, "", "  ")
	writeFile(t, dir, ".forge/experience/proposed/exp-test001.json", string(proposalJSON))

	// Set HOME to temp dir so knowledge store writes there instead of real HOME.
	tmpHome := t.TempDir()
	cmd := exec.Command(forgeBin, "experience", "accept", "exp-test001")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+tmpHome, "USERPROFILE="+tmpHome)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("forge experience accept failed: %v\noutput: %s", err, output)
	}

	// Verify: proposal status = accepted.
	type proposalFile struct {
		Status string `json:"status"`
	}
	var accepted proposalFile
	readJSON(t, dir, ".forge/experience/proposed/exp-test001.json", &accepted)
	if accepted.Status != "accepted" {
		t.Errorf("expected proposal status 'accepted', got %q", accepted.Status)
	}

	// Verify: review status = resolved (if review was created).
	if fileExists(t, dir, ".forge/reviews/EXP-1.json") {
		type reviewFile struct {
			Status string `json:"status"`
		}
		var review reviewFile
		readJSON(t, dir, ".forge/reviews/EXP-1.json", &review)
		if review.Status != "resolved" {
			t.Errorf("expected review status 'resolved', got %q", review.Status)
		}
	}

	// Verify: knowledge store has entry.
	knowledgeIdx := filepath.Join(tmpHome, ".forge", "knowledge", "index.json")
	data, err := os.ReadFile(knowledgeIdx)
	if err != nil {
		t.Fatalf("knowledge index not found at %s: %v", knowledgeIdx, err)
	}
	if !strings.Contains(string(data), "exp-test001") {
		t.Errorf("knowledge index should contain accepted rule, got: %s", string(data))
	}
}

// TestUpgradeFromV040State verifies that upgrading from a v0.4.0-like state
// triggers auto-sync and creates new hooks/skills without breaking user config.
func TestUpgradeFromV040State(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)

	// Create a v0.4.0-like .forge/ structure.
	for _, d := range []string{".forge/hooks", ".forge/tasks", ".forge/gates"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Basic pipeline.yml.
	writeFile(t, dir, ".forge/pipeline.yml", `version: "2.0"
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

	// State with old version.
	writeFile(t, dir, ".forge/state.json", `{
  "pipeline_version": "2.0",
  "mode": "medium",
  "current_gate": "",
  "started_at": "2025-01-01T00:00:00Z",
  "history": [],
  "overrides": [],
  "last_sync_version": "v0.4.0"
}`)

	// User's customized protocol.yml with scoring config.
	writeFile(t, dir, ".forge/protocol.yml", `version: "1.0"
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

	// Old hooks — only 2, missing experience-check.sh and task-verify.sh.
	writeFile(t, dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old-auto-compile\n")
	writeFile(t, dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old-assertion-check\n")

	// Run forge status — this triggers auto-sync.
	out := forge(t, dir, "status")
	if !strings.Contains(out, "Project:") {
		t.Fatalf("forge status output should contain 'Project:', got: %s", out)
	}

	// Verify: new hooks now exist.
	for _, hook := range []string{
		".forge/hooks/auto-compile.sh",
		".forge/hooks/assertion-check.sh",
		".forge/hooks/experience-check.sh",
		".forge/hooks/task-verify.sh",
	} {
		if !fileExists(t, dir, hook) {
			t.Errorf("expected %s to exist after upgrade sync", hook)
		}
	}

	// Verify hooks were actually updated (not the old content).
	hookContent := readFile(t, dir, ".forge/hooks/auto-compile.sh")
	if strings.Contains(hookContent, "old-auto-compile") {
		t.Error("auto-compile.sh should have been overwritten, still has old content")
	}

	// Verify: SKILL.md regenerated with experience section.
	skillContent := readFile(t, dir, ".claude/skills/forge-pipeline/SKILL.md")
	if skillContent == "" {
		t.Error("expected SKILL.md to be regenerated")
	}

	// Verify: protocol.yml NOT overwritten — still has user's custom standard.
	protoContent := readFile(t, dir, ".forge/protocol.yml")
	if !strings.Contains(protoContent, "my-custom-standard") {
		t.Error("protocol.yml should still contain user's custom standard after upgrade")
	}
	if !strings.Contains(protoContent, "my-custom-rule") {
		t.Error("protocol.yml should still contain user's custom session rule after upgrade")
	}

	// Verify: state.json updated with new sync version.
	type stateFile struct {
		LastSyncVersion string `json:"last_sync_version"`
	}
	var state stateFile
	readJSON(t, dir, ".forge/state.json", &state)
	if state.LastSyncVersion == "v0.4.0" {
		t.Error("state.json last_sync_version should have been updated from v0.4.0")
	}
}

// TestUpgradePreservesUserProtocol verifies that auto-sync never overwrites
// a user's customized protocol.yml, even from older versions.
func TestUpgradePreservesUserProtocol(t *testing.T) {
	dir := t.TempDir()
	git(t, dir, "init")
	git(t, dir, "config", "user.email", "test@example.com")
	git(t, dir, "config", "user.name", "Test")
	initGoProject(t, dir)

	// Create a v0.3.0-like state with customized protocol.
	for _, d := range []string{".forge/hooks", ".forge/tasks", ".forge/gates"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	writeFile(t, dir, ".forge/pipeline.yml", `version: "2.0"
project: "old-project"
mode: medium

pipeline:
  gates:
    - id: gate-4-implement
      name: "Code Implementation"
      enabled: true
      depends_on: []
`)

	writeFile(t, dir, ".forge/state.json", `{
  "pipeline_version": "2.0",
  "mode": "medium",
  "current_gate": "",
  "started_at": "2025-01-01T00:00:00Z",
  "history": [],
  "overrides": [],
  "last_sync_version": "v0.3.0"
}`)

	// User's protocol with custom standards they don't want to lose.
	writeFile(t, dir, ".forge/protocol.yml", `version: "1.0"
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

	// Old hooks.
	writeFile(t, dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old\n")
	writeFile(t, dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old\n")

	// Run any forge command to trigger auto-sync.
	forge(t, dir, "status")

	// Verify: protocol.yml still has user's custom standards.
	protoContent := readFile(t, dir, ".forge/protocol.yml")
	if !strings.Contains(protoContent, "no-console-log") {
		t.Error("protocol.yml should still contain 'no-console-log' standard")
	}
	if !strings.Contains(protoContent, "require-error-handling") {
		t.Error("protocol.yml should still contain 'require-error-handling' standard")
	}
	if !strings.Contains(protoContent, "review-before-merge") {
		t.Error("protocol.yml should still contain 'review-before-merge' session rule")
	}

	// Verify: hooks were updated (not old content).
	for _, hook := range []string{
		".forge/hooks/auto-compile.sh",
		".forge/hooks/experience-check.sh",
		".forge/hooks/task-verify.sh",
	} {
		if fileExists(t, dir, hook) {
			content := readFile(t, dir, hook)
			if strings.Contains(content, "echo old\n") {
				t.Errorf("%s should have been updated", hook)
			}
		}
	}

	// Verify: settings.local.json updated.
	if !fileExists(t, dir, ".claude/settings.local.json") {
		t.Error("settings.local.json should exist after auto-sync")
	}

	// Verify: SKILL.md updated.
	if !fileExists(t, dir, ".claude/skills/forge-pipeline/SKILL.md") {
		t.Error("SKILL.md should exist after auto-sync")
	}
}

// ---------- Helpers ----------

// passAllGates passes all 3 task gates (v0.17: reduced from 5) for the given task ref.
func passAllGates(t *testing.T, dir, ref string) {
	t.Helper()

	// Disable gate timing for E2E tests (gates pass in rapid sequence)
	os.Setenv("FORGE_GATE_MIN_INTERVAL", "0s")
	defer os.Unsetenv("FORGE_GATE_MIN_INTERVAL")
	os.Setenv("FORGE_WORK_ACTIVITY", "disable")
	defer os.Unsetenv("FORGE_WORK_ACTIVITY")

	// Commit so HEAD moves ahead of the base branch — task-implement's
	// code-change check requires a new commit on the feature branch.
	git(t, dir, "commit", "--allow-empty", "-m", "e2e: move HEAD for task-implement")

	// Pass all gates in order: task-implement, task-verify, task-complete.
	for _, g := range []string{"task-implement", "task-verify", "task-complete"} {
		out, err := forgeErr(t, dir, "task", "gate", g, "--ref", ref)
		if err != nil {
			t.Fatalf("forge task gate %s failed: %v\noutput: %s", g, err, out)
		}
	}
}
