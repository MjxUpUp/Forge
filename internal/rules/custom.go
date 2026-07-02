package rules

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// CustomScriptEvaluator runs a custom hook script and checks its exit code.
type CustomScriptEvaluator struct{}

func (e *CustomScriptEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	scriptPath := filepath.Join(ctx.ProjectRoot, ".forge", "hooks", params.Script)

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		return Result{
			Name:    "custom_script",
			Passed:  false,
			Detail:  fmt.Sprintf("script not found: %s", params.Script),
			Message: fmt.Sprintf("Hook script %s not found in .forge/hooks/", params.Script),
		}
	}

	cmd := exec.Command(shell(), scriptPath)
	cmd.Dir = ctx.ProjectRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return Result{
			Name:    "custom_script",
			Passed:  false,
			Detail:  fmt.Sprintf("script %s failed: %s", params.Script, truncateOutput(output, 200)),
			Message: fmt.Sprintf("Hook %s failed: %v", params.Script, err),
		}
	}

	return Result{
		Name:   "custom_script",
		Passed: true,
		Detail: fmt.Sprintf("script %s passed", params.Script),
	}
}

func truncateOutput(output []byte, maxLen int) string {
	s := string(output)
	runes := []rune(s)
	if len(runes) > maxLen {
		return string(runes[:maxLen]) + "..."
	}
	return s
}

// shell returns the shell command for the current platform.
func shell() string {
	if runtime.GOOS == "windows" {
		return "sh" // Git Bash ships sh.exe on PATH
	}
	return "bash"
}
