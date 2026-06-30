package rules

import (
	"fmt"
	"os"
	"path/filepath"
)

// FileExistsEvaluator checks whether a file exists.
type FileExistsEvaluator struct{}

func (e *FileExistsEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	var absPath string
	switch params.In {
	case "project_root":
		absPath = filepath.Join(ctx.ProjectRoot, params.File)
	default:
		absPath = filepath.Join(ctx.GateDir, params.File)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		loc := "gate dir"
		if params.In == "project_root" {
			loc = "project root"
		}
		return Result{
			Name:    "file_exists",
			Passed:  false,
			Detail:  fmt.Sprintf("%s not found (%s)", params.File, loc),
			Message: fmt.Sprintf("Required file %s not found", params.File),
		}
	}
	loc := "gate dir"
	if params.In == "project_root" {
		loc = "project root"
	}
	return Result{
		Name:   "file_exists",
		Passed: true,
		Detail: fmt.Sprintf("%s exists (%s)", params.File, loc),
	}
}
