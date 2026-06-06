package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FileContainsEvaluator checks whether a file contains (or does not contain) a keyword.
type FileContainsEvaluator struct {
	Negated bool // true = file_not_contains, false = file_contains
}

func (e *FileContainsEvaluator) Evaluate(ctx Context, params CheckParams) Result {
	dir := ctx.GateDir
	if params.In == "project_root" {
		dir = ctx.ProjectRoot
	}
	absPath := filepath.Join(dir, params.File)
	data, err := os.ReadFile(absPath)
	if err != nil {
		if e.Negated {
			return Result{
				Name:   "file_not_contains",
				Passed: true,
				Detail: fmt.Sprintf("file %s not found (nothing to contain)", params.File),
			}
		}
		return Result{
			Name:    "file_contains",
			Passed:  false,
			Detail:  fmt.Sprintf("file not found: %s", params.File),
			Message: fmt.Sprintf("Required file %s not found in gate artifacts", params.File),
		}
	}

	content := string(data)
	keyword := params.Keyword

	if !params.CaseSensitive {
		content = strings.ToLower(content)
		keyword = strings.ToLower(keyword)
	}

	found := strings.Contains(content, keyword)

	if e.Negated {
		if found {
			return Result{
				Name:    "file_not_contains",
				Passed:  false,
				Detail:  fmt.Sprintf("found '%s' in %s", params.Keyword, params.File),
				Message: fmt.Sprintf("Forbidden keyword '%s' found in %s", params.Keyword, params.File),
			}
		}
		return Result{
			Name:   "file_not_contains",
			Passed: true,
			Detail: fmt.Sprintf("no '%s' found in %s", params.Keyword, params.File),
		}
	}

	if !found {
		return Result{
			Name:    "file_contains",
			Passed:  false,
			Detail:  fmt.Sprintf("'%s' not found in %s", params.Keyword, params.File),
			Message: fmt.Sprintf("Expected keyword '%s' not found in %s", params.Keyword, params.File),
		}
	}
	return Result{
		Name:   "file_contains",
		Passed: true,
		Detail: fmt.Sprintf("'%s' found in %s", params.Keyword, params.File),
	}
}
