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
		// D1: a missing file is fail-closed by default for BOTH variants. The
		// prior behavior passed file_not_contains on a missing file, which let an
		// agent delete the target file to dodge a "must not contain forbidden
		// keyword" gate — absence cannot verify the keyword is not there.
		// PassOnMissing opts back into "this artifact should not exist" semantics
		// (e.g. a debug.log the build must not produce), where missing = desired.
		if e.Negated && params.PassOnMissing {
			return Result{
				Name:   "file_not_contains",
				Passed: true,
				Detail: fmt.Sprintf("file %s absent (pass_on_missing) — nothing to contain", params.File),
			}
		}
		name := "file_contains"
		if e.Negated {
			name = "file_not_contains"
		}
		return Result{
			Name:    name,
			Passed:  false,
			Detail:  fmt.Sprintf("file not found: %s", params.File),
			Message: fmt.Sprintf("Required file %s not found — cannot verify content", params.File),
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
