package taskcontext

import "testing"

func TestParseBranchName(t *testing.T) {
	tests := []struct {
		branch       string
		wantRef      string
		wantSummary  string
	}{
		{"feature/login-flow", "feature/login-flow", "login-flow"},
		{"fix/PROJ-123-crash", "PROJ-123", "crash"},
		{"bugfix/TASK-456", "TASK-456", ""},
		{"hotfix/ABC-789-urgent-fix", "ABC-789", "urgent-fix"},
		{"TASK-789", "TASK-789", ""},
		{"PROJ-123-add-auth", "PROJ-123", "add-auth"},
		{"XY-1-fix", "XY-1", "fix"},
		{"my-feature", "my-feature", "my-feature"},
		{"feature/simple", "feature/simple", "simple"},
		{"fix/minor-typo", "fix/minor-typo", "minor-typo"},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			ref, summary := ParseBranchName(tt.branch)
			if ref != tt.wantRef {
				t.Errorf("ref = %q, want %q", ref, tt.wantRef)
			}
			if summary != tt.wantSummary {
				t.Errorf("summary = %q, want %q", summary, tt.wantSummary)
			}
		})
	}
}

func TestIsMainBranch(t *testing.T) {
	tests := []struct {
		branch string
		want   bool
	}{
		{"main", true},
		{"master", true},
		{"develop", true},
		{"Main", true},
		{"MAIN", true},
		{"trunk", true},
		{"feature/login", false},
		{"fix/bug", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			if got := isMainBranch(tt.branch); got != tt.want {
				t.Errorf("isMainBranch(%q) = %v, want %v", tt.branch, got, tt.want)
			}
		})
	}
}

func TestSanitizeRef(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"feature/login", "feature-login"},
		{"PROJ-123", "PROJ-123"},
		{"fix/ABC-456 crash", "fix-ABC-456-crash"},
		{`my\branch`, "my-branch"},
	}
	for _, tt := range tests {
		got := SanitizeRef(tt.input)
		if got != tt.want {
			t.Errorf("SanitizeRef(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestContextIsSet(t *testing.T) {
	tests := []struct {
		ctx  *Context
		want bool
	}{
		{&Context{Source: "branch", TaskRef: "PROJ-123"}, true},
		{&Context{Source: "explicit", TaskRef: "my-task"}, true},
		{&Context{Source: "unknown", TaskRef: ""}, false},
		{&Context{Source: "branch", TaskRef: ""}, false},
		{&Context{Source: "unknown", TaskRef: "PROJ-123"}, false},
	}
	for _, tt := range tests {
		if got := tt.ctx.IsSet(); got != tt.want {
			t.Errorf("Context{Source:%q, TaskRef:%q}.IsSet() = %v, want %v",
				tt.ctx.Source, tt.ctx.TaskRef, got, tt.want)
		}
	}
}

func TestFormatContext(t *testing.T) {
	tests := []struct {
		ctx  *Context
		want string
	}{
		{&Context{Source: "branch", TaskRef: "PROJ-123", Branch: "fix/PROJ-123-bug"},
			"Task: PROJ-123 (from branch, branch: fix/PROJ-123-bug)"},
		{&Context{Source: "unknown", TaskRef: "", Branch: "main"},
			"Branch: main (no task context detected)"},
		{&Context{Source: "unknown", TaskRef: "", Branch: ""},
			"No task context detected"},
	}
	for _, tt := range tests {
		got := FormatContext(tt.ctx)
		if got != tt.want {
			t.Errorf("FormatContext() = %q, want %q", got, tt.want)
		}
	}
}

func TestIsProjectKey(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"PROJ", true},
		{"AB", true},
		{"ABCDEF", true},
		{"A", false},
		{"ABCDEFG", false},
		{"abc", false},
		{"AB1", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isProjectKey(tt.input); got != tt.want {
			t.Errorf("isProjectKey(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
