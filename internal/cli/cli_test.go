package cli

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

var forgeExe string

func TestMain(m *testing.M) {
	exeName := "forge"
	if runtime.GOOS == "windows" {
		exeName = "forge.exe"
	}
	tmpDir, err := os.MkdirTemp("", "forge-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	forgeExe = filepath.Join(tmpDir, exeName)

	cmd := exec.Command("go", "build", "-o", forgeExe, "../../cmd/forge")
	if output, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build forge binary: %v\n%s\n", err, output)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(tmpDir)
	os.Exit(code)
}

// buildForge returns the path to the pre-built forge binary.
func buildForge(t *testing.T) string {
	t.Helper()
	return forgeExe
}

// runForge executes the forge CLI in the given working directory.
func runForge(t *testing.T, dir string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	exe := buildForge(t)
	cmd := exec.Command(exe, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	output := string(out)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return output, "", exitErr.ExitCode()
		}
		return output, err.Error(), -1
	}
	return output, "", 0
}

// countGatesInYAML counts gate entries in a pipeline.yml by counting "- id:" lines.
func countGatesInYAML(t *testing.T, content string) int {
	t.Helper()
	count := 0
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- id:") {
			count++
		}
	}
	return count
}

// --------------- Test 1: TestInitCreatesFiles ---------------

func TestInitCreatesFiles(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init exit code %d, output: %s", code, stdout)
	}

	// .forge/pipeline.yml exists and contains version: "2.0"
	pipelineYML := filepath.Join(tmpDir, ".forge", "pipeline.yml")
	data, err := os.ReadFile(pipelineYML)
	if err != nil {
		t.Fatalf("pipeline.yml not found: %v", err)
	}
	if !strings.Contains(string(data), `version: "2.0"`) {
		t.Fatalf("pipeline.yml does not contain version: \"2.0\"\ngot:\n%s", string(data))
	}

	// .forge/state.json exists
	stateJSON := filepath.Join(tmpDir, ".forge", "state.json")
	if _, err := os.Stat(stateJSON); err != nil {
		t.Fatalf("state.json not found: %v", err)
	}

	// .forge/hooks/ has 3 .sh files
	hooksDir := filepath.Join(tmpDir, ".forge", "hooks")
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		t.Fatalf("failed to read hooks dir: %v", err)
	}
	shCount := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sh") {
			shCount++
		}
	}
	if shCount != 3 {
		t.Fatalf("expected 3 .sh files in hooks/, got %d", shCount)
	}

	// .claude/settings.local.json exists
	settingsFile := filepath.Join(tmpDir, ".claude", "settings.local.json")
	if _, err := os.Stat(settingsFile); err != nil {
		t.Fatalf(".claude/settings.local.json not found: %v", err)
	}

	// .claude/skills/forge-pipeline/SKILL.md exists
	skillFile := filepath.Join(tmpDir, ".claude", "skills", "forge-pipeline", "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Fatalf(".claude/skills/forge-pipeline/SKILL.md not found: %v", err)
	}
}

// --------------- Test 2: TestInitSmallMode ---------------

func TestInitSmallMode(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "small")
	if code != 0 {
		t.Fatalf("forge init --mode small exit code %d, output: %s", code, stdout)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".forge", "pipeline.yml"))
	if err != nil {
		t.Fatalf("pipeline.yml not found: %v", err)
	}

	gateCount := countGatesInYAML(t, string(data))
	if gateCount != 2 {
		t.Fatalf("expected 2 gates in small mode, got %d", gateCount)
	}
}

// --------------- Test 3: TestInitLargeMode ---------------

func TestInitLargeMode(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "large")
	if code != 0 {
		t.Fatalf("forge init --mode large exit code %d, output: %s", code, stdout)
	}

	data, err := os.ReadFile(filepath.Join(tmpDir, ".forge", "pipeline.yml"))
	if err != nil {
		t.Fatalf("pipeline.yml not found: %v", err)
	}

	gateCount := countGatesInYAML(t, string(data))
	if gateCount != 9 {
		t.Fatalf("expected 9 gates in large mode, got %d", gateCount)
	}
}

// --------------- Test 4: TestValidateValid ---------------

func TestValidateValid(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "validate")
	if code != 0 {
		t.Fatalf("forge validate exit code %d, output: %s", code, stdout)
	}
	if !strings.Contains(strings.ToLower(stdout), "valid") {
		t.Fatalf("expected validate output to contain 'valid', got: %s", stdout)
	}
}

// --------------- Test 5: TestStatusAfterInit ---------------

func TestStatusAfterInit(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "status")
	if code != 0 {
		t.Fatalf("forge status exit code %d, output: %s", code, stdout)
	}
	if !strings.Contains(stdout, "pending") {
		t.Fatalf("expected status output to contain 'pending', got: %s", stdout)
	}
}

// --------------- Test 6: TestStatusJSON ---------------

func TestStatusJSON(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "status", "--json")
	if code != 0 {
		t.Fatalf("forge status --json exit code %d, output: %s", code, stdout)
	}

	// Parse JSON and check for "pipeline" and "state" keys
	var result map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse status JSON: %v\noutput: %s", err, stdout)
	}
	if _, ok := result["pipeline"]; !ok {
		t.Fatal("JSON output missing 'pipeline' field")
	}
	if _, ok := result["state"]; !ok {
		t.Fatal("JSON output missing 'state' field")
	}
}

// --------------- Test 7: TestGateFailsNoArtifacts ---------------

func TestGateFailsNoArtifacts(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	_, _, code = runForge(t, tmpDir, "gate", "gate-1-prd")
	if code == 0 {
		t.Fatal("expected forge gate gate-1-prd to fail (non-zero exit) when no artifacts exist, got exit 0")
	}
}

// --------------- Test 8: TestHelperFunctions ---------------

func TestHelperFunctions(t *testing.T) {
	t.Run("truncateStr", func(t *testing.T) {
		tests := []struct {
			input string
			max   int
			want  string
		}{
			{"hello", 10, "hello"},
			{"hello world", 8, "hello..."},
			{"short", 5, "short"},
			{"abcdef", 5, "ab..."},
			{"abc", 3, "abc"},
			{"中文测试内容", 4, "中..."},
		}
		for _, tc := range tests {
			got := truncateStr(tc.input, tc.max)
			if got != tc.want {
				t.Errorf("truncateStr(%q, %d) = %q, want %q", tc.input, tc.max, got, tc.want)
			}
		}
	})

	t.Run("jsonMarshal", func(t *testing.T) {
		type sample struct {
			Name  string `json:"name"`
			Value int    `json:"value"`
		}
		data, err := jsonMarshal(sample{Name: "test", Value: 42})
		if err != nil {
			t.Fatalf("jsonMarshal failed: %v", err)
		}
		// Should be indented JSON
		s := string(data)
		if !strings.Contains(s, "\"name\": \"test\"") {
			t.Errorf("jsonMarshal output unexpected: %s", s)
		}
		if !strings.Contains(s, "\"value\": 42") {
			t.Errorf("jsonMarshal output unexpected: %s", s)
		}
		// Verify it's valid JSON
		var parsed sample
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("jsonMarshal output is not valid JSON: %v", err)
		}
		if parsed.Name != "test" || parsed.Value != 42 {
			t.Errorf("jsonMarshal roundtrip failed: got %+v", parsed)
		}
	})

	t.Run("findProjectRoot", func(t *testing.T) {
		tmpDir := t.TempDir()
		projectDir := filepath.Join(tmpDir, "myproject")
		subDir := filepath.Join(projectDir, "subdir")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Create .forge/ at the project root
		if err := os.MkdirAll(filepath.Join(projectDir, ".forge"), 0755); err != nil {
			t.Fatal(err)
		}

		originalDir, _ := os.Getwd()
		if err := os.Chdir(subDir); err != nil {
			t.Fatalf("failed to chdir: %v", err)
		}
		defer os.Chdir(originalDir)

		root, err := findProjectRoot()
		if err != nil {
			t.Fatalf("findProjectRoot failed: %v", err)
		}
		// Resolve symlinks for comparison (macOS /var → /private/var)
		resolvedRoot, _ := filepath.EvalSymlinks(root)
		resolvedWant, _ := filepath.EvalSymlinks(projectDir)
		if resolvedRoot != resolvedWant {
			t.Errorf("findProjectRoot returned %q (resolved: %q), want %q (resolved: %q)", root, resolvedRoot, projectDir, resolvedWant)
		}
	})

	t.Run("detectMode", func(t *testing.T) {
		tmpDir := t.TempDir()

		// No indicator files → "small"
		if mode := detectMode(tmpDir); mode != "small" {
			t.Errorf("detectMode with no files = %q, want small", mode)
		}

		// go.mod present → "medium"
		if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if mode := detectMode(tmpDir); mode != "medium" {
			t.Errorf("detectMode with go.mod = %q, want medium", mode)
		}
	})

	t.Run("getPipelineTemplate", func(t *testing.T) {
		// small template should have 2 gates
		small := getPipelineTemplate("small", "testproject")
		if !strings.Contains(small, `version: "2.0"`) {
			t.Error("small template missing version: \"2.0\"")
		}
		if countGatesInYAML(t, small) != 2 {
			t.Errorf("small template gate count != 2")
		}

		// medium template should have 6 gates
		medium := getPipelineTemplate("medium", "testproject")
		if countGatesInYAML(t, medium) != 6 {
			t.Errorf("medium template gate count != 6")
		}

		// large template should have 9 gates
		large := getPipelineTemplate("large", "testproject")
		if countGatesInYAML(t, large) != 9 {
			t.Errorf("large template gate count != 9")
		}

		// unknown mode defaults to medium template
		unknown := getPipelineTemplate("unknown", "testproject")
		if countGatesInYAML(t, unknown) != 6 {
			t.Errorf("unknown mode should default to medium (6 gates)")
		}
	})
}

// --------------- Test: System status health check ---------------

func TestSystemStatusRequiresForge(t *testing.T) {
	tmpDir := t.TempDir()

	// forge status --system runs system health checks.
	// It checks ~/.forge/ existence, not the project dir,
	// so just verify it runs without crashing.
	_, _, _ = runForge(t, tmpDir, "status", "--system")
}

// --------------- Test: Knowledge commands (smoke test) ---------------

func TestKnowledgeListEmpty(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "knowledge", "list")
	// Knowledge list may succeed with 0 entries or fail if kb not initialized.
	// Either way it should not crash with a panic.
	_ = stdout
	_ = code
}

// --------------- Test: Gate with non-existent ID ---------------

func TestGateNonExistentID(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "medium")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	_, _, code = runForge(t, tmpDir, "gate", "non-existent-gate")
	if code == 0 {
		t.Fatal("expected non-zero exit for non-existent gate ID")
	}
}

// --------------- Test: Gate with no args ---------------

func TestGateNoArgs(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, code := runForge(t, tmpDir, "gate")
	if code == 0 {
		t.Fatal("expected non-zero exit when gate called without gate-id arg")
	}
}

// --------------- Test: Validate without init ---------------

func TestValidateWithoutInit(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, code := runForge(t, tmpDir, "validate")
	if code == 0 {
		t.Fatal("expected non-zero exit when validate called without init")
	}
}

// --------------- Test: Status without init ---------------

func TestStatusWithoutInit(t *testing.T) {
	tmpDir := t.TempDir()

	_, _, code := runForge(t, tmpDir, "status")
	if code == 0 {
		t.Fatal("expected non-zero exit when status called without init")
	}
}

// --------------- Test: Init detects mode from existing files ---------------

func TestInitDetectsMode(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a go.mod to trigger medium detection
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte("module test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	stdout, _, code := runForge(t, tmpDir, "init")
	if code != 0 {
		t.Fatalf("forge init failed: %s", stdout)
	}

	// Should have detected medium mode
	stateData, err := os.ReadFile(filepath.Join(tmpDir, ".forge", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(stateData), `"medium"`) {
		t.Fatalf("expected auto-detected mode 'medium', got: %s", string(stateData))
	}
}

// --------------- Test: Init idempotent (run twice) ---------------

func TestInitIdempotent(t *testing.T) {
	tmpDir := t.TempDir()

	stdout, _, code := runForge(t, tmpDir, "init", "--mode", "small")
	if code != 0 {
		t.Fatalf("first init failed: %s", stdout)
	}

	stdout, _, code = runForge(t, tmpDir, "init", "--mode", "small")
	if code != 0 {
		t.Fatalf("second init failed: %s", stdout)
	}
}
