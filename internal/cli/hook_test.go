package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookOutput_AllowOnMissingProject(t *testing.T) {
	// Run in a temp dir without .forge/ — should output allow JSON
	tmpDir := t.TempDir()
	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Reset command output capture
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Simulate calling hook with no project
	err := runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout

	// Should not error (silently allow)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}

	// Read captured stdout
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	// Should be valid JSON with decision: allow
	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %q, err: %v", output, err)
	}
	if result.Decision != "approve" {
		t.Errorf("decision = %q, want %q", result.Decision, "approve")
	}
}

func TestHookOutput_UnknownHook(t *testing.T) {
	err := runHook(nil, []string{"nonexistent-hook"})
	if err == nil {
		t.Fatal("expected error for unknown hook")
	}
	if !strings.Contains(err.Error(), "unknown hook") {
		t.Errorf("error = %q, want 'unknown hook'", err.Error())
	}
}

func TestHookOutput_StructuredJSON(t *testing.T) {
	// Create a temp project with .forge/ directory
	tmpDir := t.TempDir()
	forgeDir := filepath.Join(tmpDir, ".forge", "hooks")
	os.MkdirAll(forgeDir, 0755)
	// Write a minimal state.json to make it look like a forge project
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Provide stdin JSON (simulating Claude Code input)
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"src/main.go","content":"package main"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout

	// May error if go build fails — that's OK, we just check the JSON output
	_ = err

	buf := make([]byte, 8192)
	n, _ := r.Read(buf)
	output := strings.TrimSpace(string(buf[:n]))

	if output == "" {
		t.Fatal("no output from hook")
	}

	// Must be valid JSON
	var result HookOutput
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("output is not valid JSON: %q, err: %v", output, err)
	}

	// Decision must be "approve" or "block"
	if result.Decision != "approve" && result.Decision != "block" {
		t.Errorf("decision = %q, want 'approve' or 'block'", result.Decision)
	}

	// If hookSpecificOutput is present, it must include hookEventName
	if result.HookSpecificOutput != nil && result.HookSpecificOutput.HookEventName == "" {
		t.Error("hookSpecificOutput has no hookEventName")
	}
	if result.HookSpecificOutput != nil && result.HookSpecificOutput.HookEventName != "PostToolUse" {
		t.Errorf("hookEventName = %q, want %q", result.HookSpecificOutput.HookEventName, "PostToolUse")
	}
}

func TestHookOutput_CheckLogRecorded(t *testing.T) {
	// Create a temp project with .forge/ directory
	tmpDir := t.TempDir()
	forgeDir := filepath.Join(tmpDir, ".forge", "hooks")
	os.MkdirAll(forgeDir, 0755)
	os.WriteFile(filepath.Join(tmpDir, ".forge", "state.json"), []byte(`{"pipeline_version":"2.0","mode":"small"}`), 0644)

	originalWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(originalWd)

	// Provide stdin JSON
	oldStdin := os.Stdin
	tmpStdin, _ := os.CreateTemp("", "hook-stdin-*.json")
	tmpStdin.WriteString(`{"hook_event_name":"PostToolUse","tool_name":"Write","tool_input":{"file_path":"README.md","content":"hello"}}`)
	tmpStdin.Seek(0, 0)
	os.Stdin = tmpStdin
	defer func() {
		os.Stdin = oldStdin
		tmpStdin.Close()
		os.Remove(tmpStdin.Name())
	}()

	// Capture stdout
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	runHook(nil, []string{"auto-compile"})

	w.Close()
	os.Stdout = oldStdout
	r.Read(make([]byte, 8192))

	// Check that checklog.jsonl was created
	checklogPath := filepath.Join(tmpDir, ".forge", "checklog.jsonl")
	data, err := os.ReadFile(checklogPath)
	if err != nil {
		t.Fatalf("checklog.jsonl not created: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line in checklog, got %d", len(lines))
	}

	// Parse the entry
	type logEntry struct {
		Check   string `json:"check"`
		Passed  bool   `json:"passed"`
		Checked bool   `json:"checked"`
		Detail  string `json:"detail"`
	}
	var entry logEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("checklog entry not valid JSON: %v", err)
	}
	if entry.Check != "auto-compile" {
		t.Errorf("check = %q, want %q", entry.Check, "auto-compile")
	}
	if !entry.Checked {
		t.Error("checked = false, want true")
	}
}

func TestToRelPath(t *testing.T) {
	tests := []struct {
		name    string
		root    string
		absPath string
		want    string
	}{
		{
			name:    "absolute path to .forge state file",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/DevWorkbench/.forge/tasks/feature-v1-layout-refactor.json"),
			want:    ".forge/tasks/feature-v1-layout-refactor.json",
		},
		{
			name:    "absolute path to source file",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/DevWorkbench/src/components/chat/ChatView.tsx"),
			want:    "src/components/chat/ChatView.tsx",
		},
		{
			name:    "absolute path to .claude/settings",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/DevWorkbench/.claude/settings.local.json"),
			want:    ".claude/settings.local.json",
		},
		{
			name:    "empty root returns original",
			root:    "",
			absPath: filepath.FromSlash("E:/DevWorkbench/.forge/tasks/x.json"),
			want:    filepath.FromSlash("E:/DevWorkbench/.forge/tasks/x.json"),
		},
		{
			name:    "empty path returns empty",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: "",
			want:    "",
		},
		{
			name:    "path outside root uses ..",
			root:    filepath.FromSlash("E:/DevWorkbench"),
			absPath: filepath.FromSlash("E:/OtherProject/src/main.go"),
			want:    "../OtherProject/src/main.go",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toRelPath(tt.root, tt.absPath)
			if got != tt.want {
				t.Errorf("toRelPath(%q, %q) = %q, want %q", tt.root, tt.absPath, got, tt.want)
			}
		})
	}
}
