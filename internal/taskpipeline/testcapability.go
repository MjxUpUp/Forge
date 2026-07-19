package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// CheckNameTestCapability is the checklog entry name for the repo test-capability
// scan: does the repo HAVE runnable tests? This is orthogonal to test-coverage-gate,
// which only checks whether the files changed THIS TASK came with matching tests
// (writing a test ≠ running it). The scan answers "is there anything to run", and
// the gate advisory then nudges the agent to actually execute before reporting done.
const CheckNameTestCapability checklog.CheckName = "test-capability-scan"

// TestCapability describes the runnable test assets found in a repo.
type TestCapability struct {
	HasTests  bool     // any unit or e2e test file found
	UnitCount int      // unit test files
	E2ECount  int      // integration/e2e test files
	Samples   []string // representative test paths (capped, sorted)
	Stack     string   // detected stack: "go"/"rust"/"node"/"python"/""
	Recommend string   // best-guess run command for the stack ("" if undetected)
	Scanned   int      // total files scanned
	Disabled  bool     // true when FORGE_TEST_COVERAGE=disable skipped the scan
}

// e2ePathMarkers flag integration/end-to-end test directories. Mirrors
// verify-before-stop.sh's integration/ detection plus the common JS e2e dirs.
// Stored WITHOUT a leading slash; isE2ETest matches them as a full path segment
// (root-level "integration/..." or after a slash ".../integration/...") so a
// directory like "myintegration/" or "some_e2e/" doesn't falsely match.
var e2ePathMarkers = []string{
	"e2e/", "integration/", "integrations/", "cypress/",
	"playwright/", "tests/e2e/", "test/e2e/",
}

// e2eFileMarkers flag an individual e2e test by name regardless of directory
// (e.g. login.e2e.test.ts, api.integration.test.go).
var e2eFileMarkers = []string{".e2e.", ".integration."}

// walkSkipDirs are pruned during the non-git filepath.Walk fallback so the scan
// doesn't crawl node_modules/vendor/build outputs. git ls-files already excludes
// untracked/ignored files, so this only matters for the fallback.
var walkSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".git": true, "dist": true,
	"build": true, "target": true, ".next": true, "out": true,
	".forge": true, "bin": true, "obj": true, "__pycache__": true,
	".venv": true, "venv": true,
}

const capabilitySampleCap = 5

// CheckTestCapability scans the repo (tracked files via git ls-files, falling back
// to a directory walk for non-git repos) and reports what runnable tests exist,
// plus a best-guess command to execute them. Pure capability detection — it never
// looks at what changed THIS task (that's CheckTestCoverage's job).
//
// Respects FORGE_TEST_COVERAGE=disable: that env is the project's "I'm not doing
// test discipline" signal (set by the test-coverage escape hatch), so nagging to
// run tests would contradict it. When disabled the scan is skipped (Disabled=true)
// and no advisory fires.
func CheckTestCapability(root string) TestCapability {
	if os.Getenv(testCoverageDisableEnv) == "disable" {
		return TestCapability{Disabled: true}
	}

	files := repoFileList(root)
	cap := TestCapability{Scanned: len(files)}
	seen := make(map[string]bool, capabilitySampleCap)

	for _, f := range files {
		norm := filepath.ToSlash(f)
		e2e := isE2ETest(norm)
		if !e2e && !isTestFile(norm) {
			continue
		}
		if e2e {
			cap.E2ECount++
		} else {
			cap.UnitCount++
		}
		cap.HasTests = true
		if len(cap.Samples) < capabilitySampleCap && !seen[norm] {
			seen[norm] = true
			cap.Samples = append(cap.Samples, norm)
		}
	}
	slices.Sort(cap.Samples)

	cap.Stack, cap.Recommend = detectStackAndCmd(root)
	return cap
}

// repoFileList returns tracked files when root is a git repo (the common case —
// fast, excludes node_modules/vendor), falling back to a pruned directory walk
// for non-git projects. Paths are repo-relative with forward slashes.
func repoFileList(root string) []string {
	// git ls-files: tracked files only. Cheap and ignores build output.
	if out, err := exec.Command("git", "-C", root, "ls-files").Output(); err == nil {
		var files []string
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				files = append(files, line)
			}
		}
		if len(files) > 0 {
			return files
		}
	}

	// Fallback: pruned walk for non-git repos.
	// This scan is advisory/best-effort: a single unreadable file or a stale
	// symlink must not abort the whole capability sweep, so every error inside
	// the callback returns nil to keep walking. The Walk return value is
	// therefore always nil in practice — we discard it (_ =) for that reason,
	// not to hide a real failure. If Walk ever surfaced a non-nil error it would
	// only mean our own callback returned one, which it never does.
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries; advisory scan must stay best-effort
		}
		if info.IsDir() {
			if walkSkipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil // only fails on non-descendant paths; impossible here
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files
}

// isE2ETest reports whether a forward-slash path is an integration/e2e test.
// Checked before isTestFile so integration/foo_test.go counts as e2e, not unit.
func isE2ETest(norm string) bool {
	for _, m := range e2eFileMarkers {
		if strings.Contains(norm, m) {
			return true
		}
	}
	// Segment match: pad with a leading slash so root-level dirs ("integration/...")
	// align with markers, then require "/"+marker so "myintegration/" can't match.
	padded := "/" + strings.ToLower(norm)
	for _, m := range e2ePathMarkers {
		if strings.Contains(padded, "/"+m) {
			return true
		}
	}
	return false
}

// detectStackAndCmd inspects manifest files at root to pick a stack and the
// command most likely to run its tests. Returns ("", "") when nothing matches —
// the advisory then omits a command rather than guessing.
func detectStackAndCmd(root string) (stack, cmd string) {
	switch {
	case fileExists(filepath.Join(root, "go.mod")):
		return "go", "go test ./..."
	case fileExists(filepath.Join(root, "Cargo.toml")):
		return "rust", "cargo test"
	case fileExists(filepath.Join(root, "package.json")):
		return "node", nodeTestCmd(filepath.Join(root, "package.json"))
	case fileExists(filepath.Join(root, "pytest.ini")) ||
		fileExists(filepath.Join(root, "setup.py")) ||
		fileExists(filepath.Join(root, "tox.ini")):
		return "python", "python -m pytest"
	case pyprojectHasPytest(root):
		return "python", "python -m pytest"
	}
	return "", ""
}

// nodeTestCmd reads package.json and prefers the framework runner when present
// (vitest/jest), else a configured non-placeholder test script, else "" (no
// reliable command). npm init's default script is a placeholder — recommending
// `npm test` there would run `echo "Error: no test specified" && exit 1` and
// just fail, so we return "" and let the advisory omit a command.
func nodeTestCmd(pkgPath string) string {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	body := string(data)
	// devDeps or a real test script naming the framework → use its runner.
	switch {
	case strings.Contains(body, "vitest"):
		return "npx vitest run"
	case strings.Contains(body, "jest"):
		return "npx jest"
	}
	// A configured, non-placeholder test script → npm test runs it. Match on the
	// distinctive "no test specified" substring so quote/whitespace variants of
	// the npm default still register as placeholders.
	if strings.Contains(body, "\"test\":") &&
		!strings.Contains(body, "no test specified") {
		return "npm test"
	}
	return ""
}

// pyprojectHasPytest reports whether pyproject.toml exists and references pytest
// (some projects only declare it under [tool.pytest] without a pytest.ini).
func pyprojectHasPytest(root string) bool {
	data, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "pytest")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// Detail returns a one-line checklog summary of the scan result.
func (c TestCapability) Detail() string {
	if c.Disabled {
		return "skipped: FORGE_TEST_COVERAGE=disable"
	}
	if !c.HasTests {
		return fmt.Sprintf("no test files found (scanned %d)", c.Scanned)
	}
	parts := []string{fmt.Sprintf("%d unit", c.UnitCount)}
	if c.E2ECount > 0 {
		parts = append(parts, fmt.Sprintf("%d e2e", c.E2ECount))
	}
	detail := "tests: " + strings.Join(parts, ", ")
	if c.Stack != "" {
		detail += " (" + c.Stack + ")"
	}
	if c.Recommend != "" {
		detail += "; run: " + c.Recommend
	}
	return detail
}

// Advisory returns the human-readable nudge printed to stderr when runnable
// tests exist. It names the recommended command so the agent can execute it
// directly, and lists a few sample paths so it knows where tests live. Returns
// "" when there are no tests — the executor only calls this when HasTests, but
// the method stays self-consistent so it can never emit "0 tests exist".
func (c TestCapability) Advisory() string {
	if !c.HasTests {
		return ""
	}
	var breakdown string
	if c.E2ECount > 0 {
		breakdown = fmt.Sprintf("%d 单元 + %d e2e/integration", c.UnitCount, c.E2ECount)
	} else {
		breakdown = fmt.Sprintf("%d 个单元测试", c.UnitCount)
	}
	cmdPart := "建议过 verify 前执行测试验证"
	if c.Recommend != "" {
		cmdPart = "建议过 verify 前执行测试验证：`" + c.Recommend + "`"
	}
	samplePart := ""
	if len(c.Samples) > 0 {
		shown := c.Samples
		if len(shown) > 3 {
			shown = shown[:3]
		}
		samplePart = "（示例：" + strings.Join(shown, ", ") + " …）"
	}
	return fmt.Sprintf("仓库存在测试（%s），%s%s", breakdown, cmdPart, samplePart)
}
