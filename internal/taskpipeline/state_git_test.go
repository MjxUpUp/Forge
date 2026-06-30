package taskpipeline

import "testing"

// TestIsGitRepo_NonGitDirectory guards the degraded-mode signal: a plain temp
// dir is not a git repo, so IsGitRepo must return false. `task start` relies on
// this to print the non-git warning that a stranded code-knowledge-base-style
// session lacked — without it, an agent starting a task in a bare directory
// gets no signal it's running in degraded mode.
func TestIsGitRepo_NonGitDirectory(t *testing.T) {
	dir := t.TempDir()
	if IsGitRepo(dir) {
		t.Errorf("IsGitRepo(%q) = true, want false for non-git dir", dir)
	}
}

// TestIsGitRepo_GitDirectory confirms the helper returns true inside an actual
// git working tree (even before any commit — `git init` creates .git, which
// `rev-parse --git-dir` resolves). This is what suppresses the warning for
// normal projects.
func TestIsGitRepo_GitDirectory(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if !IsGitRepo(dir) {
		t.Errorf("IsGitRepo(%q) = false, want true for git dir", dir)
	}
}
