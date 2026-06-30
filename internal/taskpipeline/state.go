package taskpipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/util"
)

// LoadTaskState reads a task state file from .forge/tasks/.
func LoadTaskState(root, taskRef string) (*TaskState, error) {
	filename := taskcontext.SanitizeRef(taskRef) + ".json"
	path := filepath.Join(root, ".forge", "tasks", filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("task %q not found: run 'forge task start' first", taskRef)
		}
		return nil, fmt.Errorf("failed to read task state: %w", err)
	}
	var s TaskState
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to parse task state: %w", err)
	}
	return &s, nil
}

// SaveTaskState writes a task state file to .forge/tasks/.
func SaveTaskState(root string, state *TaskState) error {
	tasksDir := filepath.Join(root, ".forge", "tasks")
	if err := os.MkdirAll(tasksDir, 0755); err != nil {
		return fmt.Errorf("failed to create tasks directory: %w", err)
	}

	filename := taskcontext.SanitizeRef(state.TaskRef) + ".json"
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal task state: %w", err)
	}
	path := filepath.Join(tasksDir, filename)
	return util.AtomicWrite(path, data, 0644)
}

// ActiveTaskState detects the current task context and loads the matching state.
// Returns nil without error if no task context is detected.
//
// sessionID scopes the active-task-ref lookup so concurrent sessions on a shared
// checkout each resolve their own active task. Empty sessionID falls back to the
// legacy global file.
//
// Detection priority:
//  1. Explicit: .forge/active-task-ref file (written by `forge task start`)
//  2. Branch-based: feature branch name maps to task ref
//  3. Fallback: scan .forge/tasks/ for a single incomplete task
//     (ambiguous when multiple tasks exist — returns nil to avoid false matches)
func ActiveTaskState(root, sessionID string) (*TaskState, error) {
	// Priority 1: explicit active task ref file
	if ref := ReadActiveTaskRef(root, sessionID); ref != "" {
		state, err := LoadTaskState(root, ref)
		if err == nil && state != nil && state.CompletedAt == nil {
			return state, nil
		}
		// Stale ref file — fall through
	}

	// Priority 2: branch-based detection
	ctx := taskcontext.Detect(root)
	if ctx.IsSet() {
		state, err := LoadTaskState(root, ctx.TaskRef)
		if err != nil {
			return nil, err
		}
		if state.CompletedAt == nil {
			return state, nil
		}
		// Completed task on this branch — fall through to fallback
	}

	// Priority 3: scan for exactly one incomplete task (unambiguous context)
	all, err := ListTaskStates(root)
	if err != nil {
		return nil, nil
	}
	var incomplete []*TaskState
	for _, s := range all {
		if s.CompletedAt == nil {
			incomplete = append(incomplete, s)
		}
	}
	if len(incomplete) == 1 {
		return incomplete[0], nil
	}
	return nil, nil
}

const activeTaskRefFile = "active-task-ref"

// activeTaskRefPath returns the active-task-ref file path.
//
// When sessionID is non-empty, the file is session-scoped
// (.forge/active-task-ref-<sessionID>) so concurrent Claude Code sessions
// working in a shared checkout each resolve their OWN active task — the
// primary concurrency race (two sessions clobbering one global file, hooks
// attributing work to the wrong task) is eliminated.
//
// Empty sessionID falls back to the legacy global file (.forge/active-task-ref)
// for backward compatibility and non-Claude (manual terminal) usage.
func activeTaskRefPath(root, sessionID string) string {
	if sessionID != "" {
		// Sanitize session ID for filesystem safety before using in filename
		safeID := util.SanitizeSessionID(sessionID)
		return filepath.Join(root, ".forge", "active-task-ref-"+safeID)
	}
	return filepath.Join(root, ".forge", activeTaskRefFile)
}

// SetActiveTaskRef writes the task ref to the (session-scoped) active-task-ref.
// Called by `forge task start` to make the active task unambiguous
// regardless of how many incomplete tasks exist.
func SetActiveTaskRef(root, sessionID, taskRef string) error {
	return util.AtomicWrite(activeTaskRefPath(root, sessionID), []byte(taskRef), 0644)
}

// ClearActiveTaskRef removes the (session-scoped) active-task-ref file.
// Called by `forge task complete` to clear the active task.
func ClearActiveTaskRef(root, sessionID string) error {
	err := os.Remove(activeTaskRefPath(root, sessionID))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ReadActiveTaskRef reads the active task ref from the (session-scoped) file.
// Returns empty string if the file doesn't exist or is empty.
//
// Exported so `forge task abort` can decide whether the aborted task is the
// current active one (and thus whether the active-task-ref should be cleared).
func ReadActiveTaskRef(root, sessionID string) string {
	data, err := os.ReadFile(activeTaskRefPath(root, sessionID))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// NewTaskState creates a new task state from a detected context.
func NewTaskState(ctx *taskcontext.Context) *TaskState {
	gates := DefaultGates()
	return &TaskState{
		TaskRef:     ctx.TaskRef,
		Branch:      ctx.Branch,
		Source:      ctx.Source,
		Summary:     ctx.Summary,
		CurrentGate: gates[0].ID, // Start with first gate
		History:     nil,
		StartedAt:   ctx.DetectedAt,
	}
}

// GetHeadCommit returns the current short HEAD commit hash.
// Returns empty string silently if not a git repo.
func GetHeadCommit(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--short", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// IsGitRepo reports whether root is inside a git working tree.
//
// The task pipeline degrades gracefully without git — gates still pass
// (hasCodeChanges returns true, CheckTestCoverage treats the empty changed-set
// as "nothing to cover"), and `task complete` scores the task. But the
// git-backed scoring dimensions become neutral: scope has no diff to measure
// (fixed 70, "Diff stat unavailable"). Without surfacing this, an agent that
// starts a task in a bare directory has no signal it is in degraded mode — the
// exact blind spot that stranded a session in a non-git project. Callers use
// this to print that signal.
func IsGitRepo(root string) bool {
	cmd := exec.Command("git", "-C", root, "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// DeleteTaskState removes a task state file.
func DeleteTaskState(root, taskRef string) error {
	filename := taskcontext.SanitizeRef(taskRef) + ".json"
	path := filepath.Join(root, ".forge", "tasks", filename)
	return os.Remove(path)
}

// ListTaskStates returns all task state files in .forge/tasks/.
func ListTaskStates(root string) ([]*TaskState, error) {
	tasksDir := filepath.Join(root, ".forge", "tasks")
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var states []*TaskState
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			continue
		}
		var s TaskState
		if err := json.Unmarshal(data, &s); err != nil {
			continue
		}
		states = append(states, &s)
	}
	return states, nil
}
