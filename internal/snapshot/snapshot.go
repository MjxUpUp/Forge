package snapshot

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Signals represents detected project characteristics.
type Signals struct {
	HasPkgManager   bool     `json:"has_pkg_manager"`
	PkgManagerFiles []string `json:"pkg_manager_files,omitempty"`

	HasSourceCode   bool   `json:"has_source_code"`
	SourceFileCount int    `json:"source_file_count"`
	SourceDirs      int    `json:"source_dirs"`

	HasTests      bool `json:"has_tests"`
	TestFileCount int  `json:"test_file_count"`

	HasCI bool `json:"has_ci"`

	HasREADME    bool `json:"has_readme"`
	HasCHANGELOG bool `json:"has_changelog"`

	HasGitHistory bool `json:"has_git_history"`
	CommitCount   int  `json:"commit_count"`
}

// ProjectSnapshot records the project state at init time.
type ProjectSnapshot struct {
	TakenAt time.Time `json:"taken_at"`
	Signals Signals   `json:"signals"`
}

// InferredGate records one gate that was auto-detected as completed.
type InferredGate struct {
	GateID  string   `json:"gate_id"`
	Reason  string   `json:"reason"`
	Signals []string `json:"signals"`
}

// Take scans the project directory and returns a snapshot of detected signals.
func Take(dir string) (*ProjectSnapshot, error) {
	snap := &ProjectSnapshot{
		TakenAt: time.Now(),
	}
	s := &snap.Signals

	detectPkgManagers(dir, s)
	detectSourceCode(dir, s)
	detectTests(dir, s)
	detectCI(dir, s)
	detectDocs(dir, s)
	detectGitHistory(dir, s)

	return snap, nil
}

// pkgManagerFiles are files that indicate a language-specific package manager.
var pkgManagerFiles = []string{
	"go.mod",
	"Cargo.toml",
	"package.json",
	"pom.xml",
	"build.gradle",
	"pyproject.toml",
	"requirements.txt",
	"Gemfile",
	"Makefile",
}

func detectPkgManagers(dir string, s *Signals) {
	for _, f := range pkgManagerFiles {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			s.HasPkgManager = true
			s.PkgManagerFiles = append(s.PkgManagerFiles, f)
		}
	}
}

// Source file extensions by language.
var sourceExtensions = map[string]bool{
	".go": true, ".rs": true, ".py": true, ".java": true,
	".js": true, ".ts": true, ".tsx": true, ".jsx": true,
	".c": true, ".cpp": true, ".h": true, ".hpp": true,
	".cs": true, ".rb": true, ".php": true, ".swift": true,
	".kt": true, ".scala": true, ".clj": true, ".ex": true,
	".erl": true, ".zig": true, ".nim": true,
}

// Directories to skip during source file scanning.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	"__pycache__": true, ".cache": true, "dist": true,
	"build": true, "target": true, ".forge": true,
	".claude": true, "bin": true, ".next": true,
}

func detectSourceCode(dir string, s *Signals) {
	sourceCount := 0
	sourceDirSet := make(map[string]bool)

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if sourceExtensions[ext] {
			sourceCount++
			relDir := filepath.Dir(path)
			if rel, err := filepath.Rel(dir, relDir); err == nil {
				sourceDirSet[rel] = true
			}
		}
		return nil
	})

	s.SourceFileCount = sourceCount
	s.SourceDirs = len(sourceDirSet)
	s.HasSourceCode = sourceCount > 0
}

// Test file patterns (filename contains).
var testPatterns = []string{
	"_test.go",   // Go
	"_test.rs",   // Rust
	".test.",     // JS/TS (.test.js, .test.ts)
	".spec.",     // JS/TS (.spec.js, .spec.ts)
	"Test.java",  // Java
	"test_",      // Python
	"_test.py",   // Python
	"_test.cpp",  // C++
	"_test.c",    // C
}

func detectTests(dir string, s *Signals) {
	testCount := 0

	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if skipDirs[base] {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		for _, pattern := range testPatterns {
			if strings.Contains(name, pattern) {
				testCount++
				break
			}
		}
		return nil
	})

	s.TestFileCount = testCount
	s.HasTests = testCount > 0
}

func detectCI(dir string, s *Signals) {
	ciPaths := []string{
		filepath.Join(".github", "workflows"),
		filepath.Join(".gitlab-ci.yml"),
		filepath.Join(".circleci"),
		filepath.Join("Jenkinsfile"),
		filepath.Join(".cirrus.yml"),
	}
	for _, p := range ciPaths {
		if _, err := os.Stat(filepath.Join(dir, p)); err == nil {
			s.HasCI = true
			return
		}
	}
}

func detectDocs(dir string, s *Signals) {
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
		s.HasREADME = true
	}
	if _, err := os.Stat(filepath.Join(dir, "CHANGELOG.md")); err == nil {
		s.HasCHANGELOG = true
	}
}

func detectGitHistory(dir string, s *Signals) {
	gitDir := filepath.Join(dir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return
	}

	cmd := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return
	}

	count, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return
	}

	s.HasGitHistory = count > 0
	s.CommitCount = count
}

// FormatSignals returns a human-readable summary of detected signals.
func FormatSignals(s *Signals) string {
	var lines []string

	if s.HasGitHistory {
		lines = append(lines, fmt.Sprintf("  ✓ Git history (%d commits)", s.CommitCount))
	}
	if len(s.PkgManagerFiles) > 0 {
		lines = append(lines, fmt.Sprintf("  ✓ Package manager (%s)", strings.Join(s.PkgManagerFiles, ", ")))
	}
	if s.HasSourceCode {
		lines = append(lines, fmt.Sprintf("  ✓ Source code (%d files in %d dirs)", s.SourceFileCount, s.SourceDirs))
	}
	if s.HasTests {
		lines = append(lines, fmt.Sprintf("  ✓ Test files (%d)", s.TestFileCount))
	}
	if s.HasREADME {
		lines = append(lines, "  ✓ README.md")
	}
	if s.HasCHANGELOG {
		lines = append(lines, "  ✓ CHANGELOG.md")
	}
	if s.HasCI {
		lines = append(lines, "  ✓ CI configuration")
	}

	if len(lines) == 0 {
		return "  (empty project)"
	}
	return strings.Join(lines, "\n")
}
