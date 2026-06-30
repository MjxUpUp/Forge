package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// projectTagFor returns a stable hex tag for the given project root. It hashes
// the canonical (absolute, cleaned) path so the tag is invariant across path
// case, drive-letter form, and symlinks — unlike a $PWD cksum, which also
// depends on the host's cksum format (GNU vs BSD). Hooks use it via the
// FORGE_PROJECT_TAG env var to scope per-project state.
func projectTagFor(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(abs)))
	return strconv.FormatUint(h.Sum64(), 16)
}

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

// maxEnvValueLen is the maximum length for environment variable values
// passed to bash scripts to prevent memory issues.
const maxEnvValueLen = 100000

var hookCmd = &cobra.Command{
	Use:    "hook <name>",
	Short:  "Run an embedded hook script by name",
	Long:   "Executes the named hook script embedded in the forge binary. Extracts fields from Claude Code's stdin JSON into env vars, runs the script, and wraps its plain-text output into structured JSON.",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE:   runHook,
}

// hookAgent selects the non-Claude-Code stdin dialect to normalize. Set by
// translators on agents whose hook stdin differs from Claude Code's shape
// (Windsurf, Copilot) via the cross-platform `--agent` flag. opencode/pi build
// Claude-shape stdin in TS and omit it. FORGE_HOOK_AGENT is a fallback for
// translators that already wire env (and for TS code that sets env).
var hookAgent string

func init() {
	hookCmd.Flags().StringVar(&hookAgent, "agent", "", "agent whose stdin dialect to normalize (windsurf|copilot)")
	rootCmd.AddCommand(hookCmd)
}

// resolveHookAgent decides which agent stdin dialect to normalize. The --agent
// flag (set by translators, cross-platform — Windows cmd can't parse ENV=val
// cmd) takes precedence; FORGE_HOOK_AGENT is a fallback for callers that wire
// env instead (and for TS extensions that set env before spawning forge). Empty
// result means "Claude-Code-shape stdin, no normalization" — the default for
// claude-code/codex/cursor and for opencode/pi (which build Claude stdin in TS).
func resolveHookAgent(flagVal, envVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return envVal
}

// isGlobalHook reports whether a hook runs independent of a forge project.
// Global hooks scan $HOME-level state (skill-scan → ~/.claude/skills), which is
// relevant in any project — so runHook must not silently skip them when
// findProjectRoot fails (non-forge project). Project-scoped hooks (task-guard,
// file-sentinel, etc.) keep the original allow-and-exit behavior.
func isGlobalHook(name string) bool {
	return name == "skill-scan"
}

func runHook(cmd *cobra.Command, args []string) error {
	name := args[0]
	content, ok := hooks.EmbeddedContent(name)
	if !ok {
		return fmt.Errorf("unknown hook: %s", name)
	}

	// Not in a forge project - output allow and exit silently.
	// Global hooks (skill-scan scans $HOME/.claude/skills) are relevant in any
	// project, so they run even without a forge project root.
	root, err := findProjectRoot()
	if err != nil {
		if !isGlobalHook(name) {
			outputAllow("")
			return nil
		}
		root = "" // global hook: no project root needed; shCmd.Dir="" falls back to cwd
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

	// 1b. Normalize non-Claude-Code agent stdin. Windsurf/Copilot use different
	// hook stdin schemas (Windsurf: {agent_action_name, trajectory_id,
	// tool_info}); without this, forge extracts empty file_path/command and
	// intercept hooks (task-guard/bash-guard) fail open. The `--agent` flag
	// (cross-platform, set by translators) selects the dialect; FORGE_HOOK_AGENT
	// is a fallback. opencode/pi are code-based and build Claude stdin directly
	// in TS, so they don't need a normalizer here.
	agent := resolveHookAgent(hookAgent, os.Getenv("FORGE_HOOK_AGENT"))
	if agent != "" {
		normalizeAgentStdin(agent, stdinData, &hookInput)
	}

	// 2. Extract tool_input fields in Go (reliable JSON parsing).
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: tool_input parse failed: %v\n", err)
		}
	}

	// 2b. Detect active task for task-guard hook context.
	// Scope the lookup by the Claude Code session id from stdin so concurrent
	// sessions each resolve their own active task (not whichever wrote the
	// global file last).
	var activeTaskRef string
	var activeTaskGate string
	if active, err := taskpipeline.ActiveTaskState(root, util.SanitizeSessionID(hookInput.SessionID)); err == nil && active != nil {
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
		"FORGE_FILE_PATH="+sanitizeForShell(toRelPath(root, fields.FilePath)),
		"FORGE_CONTENT="+sanitizeForShell(fields.Content),
		"FORGE_COMMAND="+sanitizeForShell(fields.Command),
		"FORGE_TOOL_NAME="+sanitizeForShell(hookInput.ToolName),
		"FORGE_TASK_REF="+sanitizeForShell(activeTaskRef),
		"FORGE_TASK_GATE="+sanitizeForShell(activeTaskGate),
		"FORGE_SESSION_ID="+sanitizeForShell(hookInput.SessionID),
		// Stable project tag (fnv hash of the canonical project root) so hooks
		// can scope per-project state without relying on $PWD/cksum, which is
		// unstable across path case, drive letters, and BSD/GNU cksum formats.
		"FORGE_PROJECT_TAG="+projectTagFor(root),
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

	// 6. Record to check log (noise-gated).
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

	// Noise gate (axis A of checklog layered treatment): scoring reads only
	// the LATEST entry per check (LatestByCheckForSession in task.go scoreTask),
	// so a PASS written on every tool call is pure audit noise — empirically
	// 100% of 15946 checklog lines were PASS with zero FAIL. Record only FAILs
	// (the actual block/warn signal trace needs) plus PASS of scoring-dependent
	// checks (assertion-check/auto-compile), whose LatestByCheck feeds
	// CompilePassed/AssertionPassed. Non-scoring PASS is dropped, cutting
	// checklog volume ~86%. See shouldRecordCheck.
	if shouldRecordCheck(checkName, passed) {
		if err := checklog.Record(root, &checklog.Entry{
			Check:     checkName,
			Passed:    passed,
			Checked:   true,
			ToolName:  recordedToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
			Detail:    truncate(logDetail, maxChecklogDetail),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
		}
	}

	// 6b. Record tool usage for activity-ratio detection. auto-compile records
	// Write/Edit; tool-track records Read so the read-before-edit gate
	// (task-verify) has Read data — without it the gate was always-failing on
	// any task with edits (644b142 removed the original Read recorder). tool-track
	// omits tool_input: Read is frequent and the gate needs only tool_name +
	// timestamp, so skipping the file_path keeps the toollog lean.
	if name == "auto-compile" || name == "tool-track" {
		call := &toolusage.ToolCall{
			ToolName:  hookInput.ToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
		}
		if name == "auto-compile" {
			raw := string(hookInput.ToolInput)
			call.ToolInput = toolusage.TruncateInput(raw)
			call.InputLen = len(raw)
			call.EstTokens = toolusage.EstimateTokens(raw)
		}
		if err := toolusage.Record(root, call); err != nil {
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

// sanitizeForShell sanitizes a string for safe use in a shell environment variable.
// This prevents shell injection attacks when user-controlled content is passed
// to bash scripts via environment variables.
//
// Strategy:
//   - Truncate to maxEnvValueLen to prevent memory exhaustion
//   - Replace NULL bytes and control characters (except tab, newline, carriage return)
//   - Unicode-safe validation (rejects invalid UTF-8)
//   - Does NOT quote or escape - caller must use "export VAR=$value" with value in double quotes
//
// Note: This is a defense-in-depth measure. The hook scripts themselves should
// also validate inputs before use.
func sanitizeForShell(value string) string {
	if value == "" {
		return ""
	}

	// Truncate to prevent memory issues
	if len(value) > maxEnvValueLen {
		// Truncate at UTF-8 boundary
		for offset := maxEnvValueLen - 10; offset < maxEnvValueLen; offset++ {
			if offset >= len(value) {
				break
			}
			if utf8.RuneStart(value[offset]) {
				value = value[:offset]
				break
			}
		}
	}

	// Validate UTF-8 and remove control characters
	var result strings.Builder
	result.Grow(len(value))

	for _, r := range value {
		// Check for valid UTF-8
		if r == utf8.RuneError {
			// Skip invalid runes
			continue
		}

		// Remove NULL bytes and most control characters
		// Allow: tab (0x09), newline (0x0A), carriage return (0x0D)
		// Block: NULL (0x00) and other control chars (0x01-0x08, 0x0B-0x0C, 0x0E-0x1F)
		if r == 0 {
			// Replace NULL with space
			result.WriteRune(' ')
			continue
		}
		if r < 0x20 && r != 0x09 && r != 0x0A && r != 0x0D {
			// Skip other control characters
			continue
		}

		result.WriteRune(r)
	}

	return result.String()
}

// extractDetail parses "PASS optional detail", "WARN optional detail", or "FAIL optional detail" output.
// Returns the detail portion after the keyword, or the full output
// if it doesn't start with a known prefix.
func extractDetail(stdout, prefix string) string {
	if stdout == "" {
		return ""
	}
	for _, p := range []string{prefix, "WARN"} {
		after, ok := strings.CutPrefix(stdout, p)
		if ok {
			return strings.TrimSpace(after)
		}
	}
	return stdout
}

func outputAllow(msg string) {
	out := HookOutput{Decision: "approve"}
	if msg != "" {
		out.HookSpecificOutput = &HookSpecificOutput{AdditionalContext: msg}
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
}

// shouldRecordCheck decides whether a hook outcome warrants a checklog entry.
// It is the noise gate for checklog's dual role (scoring input + audit trail):
// scoring reads only the latest entry per check name (LatestByCheckForSession),
// so per-call PASS entries are redundant. Returns true for any FAIL (the
// block/warn signal trace and diagnostics need) and for PASS of
// scoring-dependent checks only.
func shouldRecordCheck(name checklog.CheckName, passed bool) bool {
	if !passed {
		return true
	}
	return isScoringCheck(name)
}

// isScoringCheck reports whether a hook check's PASS verdict is consumed by
// task scoring. scoreTask (task.go) reads LatestByCheckForSession for these
// checks to populate CompilePassed/AssertionPassed; their PASS must be logged
// so scoring sees "checked & passed". All other checks' PASS is dropped by the
// noise gate (only FAILs recorded). Note: test-coverage scoring reads the
// separate "test-coverage-gate" entry written by taskpipeline at task-verify
// (NOT this hook path), so test-coverage-check needs no PASS log here.
func isScoringCheck(name checklog.CheckName) bool {
	switch name {
	case checklog.CheckAssertion, checklog.CheckAutoCompile:
		return true
	}
	return false
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
// toRelPath returns absPath relative to root, slash-separated. Both inputs are
// symlink-resolved first: on macOS, a path like a t.TempDir() directory is
// reached via a symlink (/var/folders/... → /private/var/folders/...), and
// os.Getwd() — used by findProjectRoot — returns the physical form while the
// tool_input file_path arrives in the symlink form. Without resolving both
// sides, filepath.Rel yields a ../../... path across the link boundary that no
// longer matches hook glob patterns (.forge/*, .claude/settings*) — the root
// cause of task-guard's macOS-only failure to block .forge/state.json writes.
func toRelPath(root, absPath string) string {
	if root == "" || absPath == "" {
		return absPath
	}
	root = resolveSymlinks(root)
	absPath = resolveSymlinks(absPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	return filepath.ToSlash(rel)
}

// resolveSymlinks evaluates symlinks on path. If path does not yet exist (e.g.
// a PreToolUse Write target before the file is created), it resolves the
// longest existing parent directory and re-appends the base name, so a
// not-yet-existing file still gets its physical prefix on macOS. The original
// path is returned unchanged when nothing along it can be resolved, preserving
// the prior fallback behavior on systems without symlinks.
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir, base := filepath.Split(path)
	if dir == "" || dir == path {
		return path
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return path
	}
	return filepath.Join(resolvedDir, base)
}
