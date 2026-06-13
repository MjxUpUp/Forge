package skillgen

import (
	"strings"
	"testing"
)

// TestClaudeMDCommonErrorsIncludesReviewBlock guards the common-errors table
// against losing the mandatory-review guidance. The task-verify Stop hook
// blocks session end on a pending mandatory review ("Pending mandatory review
// detected"); agents relying on CLAUDE.md alone need the resolution path.
func TestClaudeMDCommonErrorsIncludesReviewBlock(t *testing.T) {
	section := buildForgeSection()

	if !strings.Contains(section, "Pending mandatory review detected") {
		t.Error("CLAUDE.md common-errors table missing 'Pending mandatory review detected' row")
	}
	if !strings.Contains(section, "forge experience accept") {
		t.Error("CLAUDE.md review error row must reference 'forge experience accept'")
	}
}
