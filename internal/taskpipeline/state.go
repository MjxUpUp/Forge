package taskpipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Harness/forge/internal/taskcontext"
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
	return os.WriteFile(path, data, 0644)
}

// ActiveTaskState detects the current task context and loads the matching state.
// Returns nil without error if no task context is detected.
func ActiveTaskState(root string) (*TaskState, error) {
	ctx := taskcontext.Detect(root)
	if !ctx.IsSet() {
		return nil, nil
	}
	state, err := LoadTaskState(root, ctx.TaskRef)
	if err != nil {
		return nil, err
	}
	// Completed tasks are not active - agent must start a new task.
	if state.CompletedAt != nil {
		return nil, nil
	}
	return state, nil
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
