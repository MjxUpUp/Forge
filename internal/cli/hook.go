package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Harness/forge/internal/checklog"
	"github.com/Harness/forge/internal/hooks"
	"github.com/Harness/forge/internal/taskpipeline"
	"github.com/Harness/forge/internal/toolusage"
	"github.com/spf13/cobra"
)

// HookInput represents the JSON Claude Code sends to hooks via stdin.
type HookInput struct {
	SessionID     string          `json:"session_id"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolOutput    json.RawMessage `json:"tool_output,omitempty"`
}

// toolInputFields holds extracted fields from tool_input JSON.
type toolInputFields struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	Command  string `json:"command"` // Bash tool_input.command
}

// HookOutput represents the structured JSON Claude Code expects on stdout.
// See Claude Code hooks documentation for field semantics.
type HookOutput struct {
	Decision           string              `json:"decision"`
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput contains fields that control Claude Code behavior.
type HookSpecificOutput struct {
	HookEventName    string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

// maxAdditionalContextLen is the Claude Code limit for additionalContext (10,000 chars).
// We use 9,500 to leave room for the JSON envelope.
const maxAdditionalContextLen = 9500

// maxChecklogDetail is the truncation limit for checklog entry details.
const maxChecklogDetail = 500

var hookCmd = &cobra.Command{
	Use:    "hook <name>",
	Short:  "Run an embedded hook script by name",
	Long:   "Executes the named hook script embedded in the forge binary. Extracts fields from Claude Code's stdin JSON into env vars, runs the script, and wraps its plain-text output into structured JSON.",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE:   runHook,
}

func runHook(cmd *cobra.Command, args []string) error {
	name := args[0]
	content, ok := hooks.EmbeddedContent(name)
	if !ok {
		return fmt.Errorf("unknown hook: %s", name)
	}

	// Not in a forge project - output allow and exit silently.
	root, err := findProjectRoot()
	if err != nil {
		outputAllow("")
		return nil
	}

	// 1. Read Claude Code's stdin JSON.
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		stdinData = []byte{}
	}

	var hookInput HookInput
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &hookInput); err != nil {
			// Log parse failure for diagnostics, but continue with empty input.
			fmt.Fprintf(os.Stderr, "[forge] warning: hook stdin JSON parse failed: %v\n", err)
		}
	}

	// 2. Extract tool_input fields in Go (reliable JSON parsing).
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: tool_input parse failed: %v\n", err)
		}
	}

	// 2b. Detect active task for task-guard hook context.
	var activeTaskRef string
	var activeTaskGate string
	if active, err := taskpipeline.ActiveTaskState(root); err == nil && active != nil {
		activeTaskRef = active.TaskRef
		activeTaskGate = active.CurrentGate
	}

	// 3. Write embedded script to temp file.
	tmpFile, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write script: %w", err)
	}
	tmpFile.Close()
	// No chmod needed — bash reads the file as argument, doesn't execute it directly.

	// 4. Execute the script with extracted fields as environment variables.
	bash, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("bash not found in PATH: %w", err)
	}

	shCmd := exec.Command(bash, tmpPath)
	shCmd.Dir = root
	shCmd.Env = append(os.Environ(),
		"FORGE_FILE_PATH="+toRelPath(root, fields.FilePath),
		"FORGE_CONTENT="+fields.Content,
		"FORGE_COMMAND="+fields.Command,
		"FORGE_TOOL_NAME="+hookInput.ToolName,
		"FORGE_TASK_REF="+activeTaskRef,
		"FORGE_TASK_GATE="+activeTaskGate,
		"FORGE_SESSION_ID="+hookInput.SessionID,
	)

	var stdoutBuf, stderrBuf bytes.Buffer
	shCmd.Stdout = &stdoutBuf
	shCmd.Stderr = &stderrBuf

	exitErr := shCmd.Run()

	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())
	passed := exitErr == nil

	// 5. Parse script output into structured JSON for Claude Code.
	// Scripts output plain text: "PASS [detail]" or "FAIL [reason]".
	// We wrap this into the Claude Code hook protocol JSON format.
	eventName := hookInput.HookEventName
	var output HookOutput
	if passed {
		detail := extractDetail(stdout, "PASS")
		output = HookOutput{Decision: "approve"}
		if detail != "" {
			output.HookSpecificOutput = &HookSpecificOutput{
				HookEventName:    eventName,
				AdditionalContext: truncate(detail, maxAdditionalContextLen),
			}
		}
	} else {
		detail := stdout
		if detail == "" {
			detail = stderr
		}
		output = HookOutput{
			Decision: "block",
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:    eventName,
				AdditionalContext: truncate(detail, maxAdditionalContextLen),
			},
		}
	}

	// 6. Record to check log.
	checkName := checklog.CheckName(name)
	logDetail := firstNonEmpty(stderr, stdout, "completed")

	// Reuse task ref detected earlier for audit traceability.
	taskRef := activeTaskRef

	// When blocked (e.g. task-guard), clear tool_name to prevent ghost
	// activity records. A blocked Write should not inflate WorkActivity counts.
	recordedToolName := hookInput.ToolName
	if !passed {
		recordedToolName = ""
	}

	if err := checklog.Record(root, &checklog.Entry{
		Check:    checkName,
		Passed:   passed,
		Checked:  true,
		ToolName: recordedToolName,
		TaskRef:  taskRef,
		Detail:   truncate(logDetail, maxChecklogDetail),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
	}

	// 6b. Record tool usage for scoring (auto-compile and tool-track hooks).
	if name == "auto-compile" || name == "tool-track" {
		if err := toolusage.Record(root, &toolusage.ToolCall{
			ToolName:  hookInput.ToolName,
			ToolInput: toolusage.TruncateInput(string(hookInput.ToolInput)),
			TaskRef:   taskRef,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: toollog record failed: %v\n", err)
		}
	}

	// 7. Output structured JSON to Claude Code.
	outputJSON, err := json.Marshal(output)
	if err != nil {
		// Should never happen — HookOutput contains only strings.
		fmt.Fprintf(os.Stderr, "[forge] error: failed to marshal hook output: %v\n", err)
		fmt.Println(`{"decision":"approve"}`)
	} else {
		fmt.Println(string(outputJSON))
	}

	if !passed {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

// extractDetail parses "PASS optional detail" or "FAIL optional detail" output.
// Returns the detail portion after the PASS/FAIL keyword, or the full output
// if it doesn't start with PASS/FAIL.
func extractDetail(stdout, prefix string) string {
	if stdout == "" {
		return ""
	}
	after, ok := strings.CutPrefix(stdout, prefix)
	if !ok {
		return stdout
	}
	return strings.TrimSpace(after)
}

func outputAllow(msg string) {
	out := HookOutput{Decision: "approve"}
	if msg != "" {
		out.HookSpecificOutput = &HookSpecificOutput{AdditionalContext: msg}
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// toRelPath converts an absolute file path to a project-root-relative path
// with forward slashes. This ensures shell script patterns like ".forge/*"
// work correctly regardless of OS path format.
// Returns the original path unchanged if conversion fails.
func toRelPath(root, absPath string) string {
	if root == "" || absPath == "" {
		return absPath
	}
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return absPath
	}
	return filepath.ToSlash(rel)
}

func init() {
	rootCmd.AddCommand(hookCmd)
}
