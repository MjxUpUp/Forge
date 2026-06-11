package toolusage

import "testing"

func TestDetectAntiPatterns_BashCat(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "cat /tmp/test.go"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	if len(violations) == 0 {
		t.Fatal("expected bash-cat violation")
	}
	found := false
	for _, v := range violations {
		if v.RuleID == "bash-cat-vs-read" {
			found = true
			if v.Severity != "major" {
				t.Errorf("expected major severity, got %s", v.Severity)
			}
			if v.PreferTool != "Read" {
				t.Errorf("expected PreferTool=Read, got %s", v.PreferTool)
			}
		}
	}
	if !found {
		t.Error("bash-cat-vs-read rule not matched")
	}
}

func TestDetectAntiPatterns_BashGrep(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "grep -r pattern src/"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	found := false
	for _, v := range violations {
		if v.RuleID == "bash-grep-vs-grep" {
			found = true
		}
	}
	if !found {
		t.Error("bash-grep-vs-grep rule not matched")
	}
}

func TestDetectAntiPatterns_BashFind(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "find . -name '*.go'"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	found := false
	for _, v := range violations {
		if v.RuleID == "bash-find-vs-glob" {
			found = true
		}
	}
	if !found {
		t.Error("bash-find-vs-glob rule not matched")
	}
}

func TestDetectAntiPatterns_BashSedEdit(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "sed -i 's/old/new/g' file.go"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	found := false
	for _, v := range violations {
		if v.RuleID == "bash-sed-vs-edit" {
			found = true
		}
	}
	if !found {
		t.Error("bash-sed-vs-edit rule not matched")
	}
}

func TestDetectAntiPatterns_NoViolation(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Read", ToolInput: `{"file_path": "/tmp/test.go"}`},
		{ToolName: "Edit", ToolInput: `{"file_path": "/tmp/test.go"}`},
		{ToolName: "Bash", ToolInput: `{"command": "go test ./..."}`},
		{ToolName: "Grep", ToolInput: `{"pattern": "TODO"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for valid tool usage, got %d", len(violations))
	}
}

func TestDetectAntiPatterns_MultipleViolations(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "cat file.go"}`},
		{ToolName: "Bash", ToolInput: `{"command": "grep -r TODO ."}`},
		{ToolName: "Bash", ToolInput: `{"command": "ls -la src/"}`},
		{ToolName: "Read", ToolInput: `{"file_path": "ok.go"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	if len(violations) != 3 {
		t.Errorf("expected 3 violations, got %d", len(violations))
	}
}

func TestDetectAntiPatterns_MinorViolations(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "head -20 file.go"}`},
		{ToolName: "Bash", ToolInput: `{"command": "wc -l *.go"}`},
	}
	violations := DetectAntiPatterns(calls, DefaultAntiPatterns)
	if len(violations) != 2 {
		t.Fatalf("expected 2 minor violations, got %d", len(violations))
	}
	for _, v := range violations {
		if v.Severity != "minor" {
			t.Errorf("expected minor severity for %s, got %s", v.RuleID, v.Severity)
		}
	}
}

func TestCountBySeverity(t *testing.T) {
	violations := []AntiPatternViolation{
		{RuleID: "r1", Severity: "major"},
		{RuleID: "r2", Severity: "minor"},
		{RuleID: "r3", Severity: "major"},
	}
	major, minor := CountBySeverity(violations)
	if major != 2 {
		t.Errorf("expected 2 major, got %d", major)
	}
	if minor != 1 {
		t.Errorf("expected 1 minor, got %d", minor)
	}
}

func TestCountByRule(t *testing.T) {
	violations := []AntiPatternViolation{
		{RuleID: "bash-cat-vs-read"},
		{RuleID: "bash-cat-vs-read"},
		{RuleID: "bash-grep-vs-grep"},
	}
	counts := CountByRule(violations)
	if counts["bash-cat-vs-read"] != 2 {
		t.Errorf("expected 2 for bash-cat-vs-read, got %d", counts["bash-cat-vs-read"])
	}
	if counts["bash-grep-vs-grep"] != 1 {
		t.Errorf("expected 1 for bash-grep-vs-grep, got %d", counts["bash-grep-vs-grep"])
	}
}
