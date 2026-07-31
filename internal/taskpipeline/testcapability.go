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

// CheckNameTestCapability is the checklog name for the repository test-capability
// scan: does the repo actually have runnable tests? This dimension is orthogonal to
// test-coverage-gate — the latter only checks whether files changed in this task carry
// paired tests (writing tests ≠ running tests). This scan answers whether anything is
// runnable, and the gate advisory uses it to nudge the agent to actually execute before
// reporting completion.
//
// CheckNameTestCapability 是仓库 test-capability 扫描的 checklog 名：仓库是否真有
// 可跑的测试？此维度与 test-coverage-gate 正交——后者只检查本 task 改动的文件
// 是否带有配对测试（写测试 ≠ 跑测试）。本扫描回答「有没有可跑的东西」，gate 的
// advisory 据此提示 agent 在报告完成前实际执行。
const CheckNameTestCapability checklog.CheckName = "test-capability-scan"

// TestCapability describes the runnable test assets found in the repository.
//
// TestCapability 描述仓库中可跑的测试资产。
type TestCapability struct {
	HasTests  bool     // 找到任何单元或 e2e 测试文件
	UnitCount int      // 单元测试文件数
	E2ECount  int      // integration/e2e 测试文件数
	Samples   []string // 代表性测试路径（截断 + 排序）
	Stack     string   // 探测到的 stack：go/rust/node/python；空串为未识别
	Recommend string   // 该 stack 的推测运行命令；未识别为空串
	Scanned   int      // 总扫描文件数
	Disabled  bool     // FORGE_TEST_COVERAGE=disable 跳过扫描时为 true
}

// e2ePathMarkers identifies integration/end-to-end test directories. It carries over the
// integration/ detection of the since-removed verify-before-stop.sh hook, plus common JS
// e2e dirs. Stored without a leading slash; isE2ETest matches them as full path segments
// (root-level integration/... or .../integration/... after a slash), so dirs like
// myintegration/ or some_e2e/ do not match by accident.
//
// e2ePathMarkers 标识 integration/end-to-end 测试目录。沿用已删除的 verify-before-stop.sh
// 的 integration/ 检测，加上常见 JS e2e 目录。无前导斜杠存储；isE2ETest 把它们作为
// 完整路径段匹配（根级 integration/... 或斜杠后的 .../integration/...），故
// myintegration/ 或 some_e2e/ 之类目录不会误命中。
var e2ePathMarkers = []string{
	"e2e/", "integration/", "integrations/", "cypress/",
	"playwright/", "tests/e2e/", "test/e2e/",
}

// e2eFileMarkers identifies individual e2e tests by filename, directory-independent
// (e.g. login.e2e.test.ts, api.integration.test.go).
//
// e2eFileMarkers 按文件名标识单个 e2e 测试，不依赖目录
//（如 login.e2e.test.ts、api.integration.test.go）。
var e2eFileMarkers = []string{".e2e.", ".integration."}

// walkSkipDirs is pruned on the non-git filepath.Walk fallback path so the scan does
// not crawl into node_modules/vendor/build artifacts. git ls-files already excludes
// untracked/ignored files, so this table only matters for the fallback path.
//
// walkSkipDirs 在非 git 的 filepath.Walk 回退路径下被剪枝，避免扫描爬进
// node_modules/vendor/build 产物。git ls-files 已排除未跟踪/被忽略文件，故本表
// 只对回退路径有意义。
var walkSkipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".git": true, "dist": true,
	"build": true, "target": true, ".next": true, "out": true,
	".forge": true, "bin": true, "obj": true, "__pycache__": true,
	".venv": true, "venv": true,
}

const capabilitySampleCap = 5

// CheckTestCapability scans the repository (git ls-files for tracked files, falling
// back to a directory walk for non-git repos), reports which runnable tests exist, and
// suggests a likely execution command. Pure capability detection — it never looks at
// what this task changed (that is CheckTestCoverage's job).
//
// It honors FORGE_TEST_COVERAGE=disable: that env is the project's signal for "I do
// not practice test discipline" (set by the test-coverage escape hatch), so nagging to
// run tests would contradict it. When disabled, the scan is skipped (Disabled=true)
// and no advisory is emitted.
//
// CheckTestCapability 扫描仓库（git ls-files 跟踪文件，非 git 仓库回退到目录
// walk），报告存在哪些可跑测试，并给一个推测执行命令。纯能力检测——绝不看本
// task 改了什么（那是 CheckTestCoverage 的活）。
//
// 遵从 FORGE_TEST_COVERAGE=disable：该 env 是项目「我不做测试纪律」的信号
//（由 test-coverage 逃生舱设置），此时再 nag 跑测试与它相悖。disable 时跳过
// 扫描（Disabled=true），不发 advisory。
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

// repoFileList returns tracked files when root is a git repo (the common case — fast,
// excludes node_modules/vendor), and falls back to a pruned directory walk for non-git
// projects. Paths are repo-relative with forward slashes.
//
// repoFileList 在 root 是 git 仓库时返回跟踪文件（常见情况——快，排除
// node_modules/vendor），非 git 项目回退到剪枝后的目录 walk。路径是仓库相对、
// 用正斜杠。
func repoFileList(root string) []string {
	// git ls-files: tracked files only. Cheap, ignores build artifacts.
	//
	// git ls-files：仅跟踪文件。开销低，忽略 build 产物。
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
	// This scan is advisory/best-effort: a single unreadable file or a dangling symlink
	// must not abort the whole capability scan, so each error inside the callback returns
	// nil and the walk continues. The Walk return value is therefore always nil in
	// practice — we discard it (with _=) precisely for this reason, not to mask real
	// failures. If Walk did return a non-nil error, it would only mean our own callback
	// returned an error, which it never does.
	//
	// 回退：非 git 仓库用剪枝 walk。
	// 本扫描是 advisory/best-effort：单个不可读文件或失效符号链接都不能让整次
	// 能力扫描中止，故 callback 内每个错误都返回 nil 继续 walk。Walk 返回值在
	// 实践中因此始终为 nil——我们丢弃它（_=）正是为此，而非掩盖真实失败。如果
	// Walk 真返回非 nil 错误，那只意味着我们自己的 callback 返回了错误，而它从不
	// 这么做。
	var files []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // 跳过不可读条目；advisory 扫描须保持 best-effort
		}
		if info.IsDir() {
			if walkSkipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			return nil // 仅在非子孙路径失败；此处不可能
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	return files
}

// isE2ETest reports whether a forward-slash path is an integration/e2e test.
// It is checked before isTestFile, so integration/foo_test.go counts as e2e, not unit.
//
// isE2ETest 报告一条正斜杠路径是否 integration/e2e 测试。
// 在 isTestFile 之前判定，故 integration/foo_test.go 算 e2e 而非 unit。
func isE2ETest(norm string) bool {
	for _, m := range e2eFileMarkers {
		if strings.Contains(norm, m) {
			return true
		}
	}
	// Segment match: prepend a slash so a root-level dir (integration/...) aligns with
	// the marker, and require "/"+marker so myintegration/ does not match.
	//
	// 段匹配：前面补一个斜杠让根级目录（integration/...）与 marker 对齐，再要求
	// "/"+marker 防止 myintegration/ 命中。
	padded := "/" + strings.ToLower(norm)
	for _, m := range e2ePathMarkers {
		if strings.Contains(padded, "/"+m) {
			return true
		}
	}
	return false
}

// detectStackAndCmd inspects root's manifest files to pick a stack and the most likely
// command to run its tests. Returns two empty strings when nothing matches — the
// advisory then omits the command rather than guessing.
//
// detectStackAndCmd 检查 root 的 manifest 文件以挑选 stack 及最可能跑其测试的
// 命令。无匹配时返回两个空串——advisory 随即省略命令，不乱猜。
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

// nodeTestCmd reads package.json: prefer a framework runner (vitest/jest) when present;
// otherwise pick a configured non-placeholder test script; otherwise return an empty
// string (no reliable command). The npm init default script is a placeholder —
// recommending npm test there would run echo with an error message and exit 1, so we
// return an empty string and let the advisory omit the command.
//
// nodeTestCmd 读 package.json：有 framework runner（vitest/jest）时优先用它；
// 否则取一条已配置的非占位 test script；再否则返回空串（无可靠命令）。npm init
// 的默认脚本是占位——在那里推荐 npm test 会跑 echo 错误信息后 exit 1 直接失败，
// 故返回空串、让 advisory 省略命令。
func nodeTestCmd(pkgPath string) string {
	data, err := os.ReadFile(pkgPath)
	if err != nil {
		return ""
	}
	body := string(data)
	// devDeps or a real test script naming a framework → use its runner.
	//
	// devDeps 或一条命名 framework 的真 test script → 用它的 runner。
	switch {
	case strings.Contains(body, "vitest"):
		return "npx vitest run"
	case strings.Contains(body, "jest"):
		return "npx jest"
	}
	// A configured non-placeholder test script → npm test will run it. Match the
	// distinctive substring "no test specified" so npm default value variants with
	// different quotes/whitespace are still recognized as placeholders.
	//
	// 一条已配置的非占位 test script → npm test 会跑它。匹配 distinctive 子串
	// no test specified，让 npm 默认值的引号/空白变体仍被识别为占位。
	if strings.Contains(body, "\"test\":") &&
		!strings.Contains(body, "no test specified") {
		return "npm test"
	}
	return ""
}

// pyprojectHasPytest reports whether pyproject.toml exists and references pytest (some
// projects only declare it under [tool.pytest], without a pytest.ini).
//
// pyprojectHasPytest 报告 pyproject.toml 是否存在且引用了 pytest（有些项目只
// 在 [tool.pytest] 下声明、无 pytest.ini）。
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
//
// Detail 返回扫描结果的单行 checklog 摘要。
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

// Advisory returns a human-readable nudge printed to stderr when the repo has runnable
// tests. It writes out the recommended command so the agent can execute it directly,
// and lists a few sample paths so the agent knows where the tests are. Returns an empty
// string when there are no tests — the executor only calls this when HasTests, but the
// method stays self-consistent and never emits "0 tests exist".
//
// Advisory 返回可读的 nudge，在仓库存在可跑测试时打印到 stderr。它写出推荐命令
// 让 agent 可直接执行，并列几条示例路径让 agent 知道测试在哪。无测试时返回
// 空串——executor 只在 HasTests 时调用，但方法保持自洽，绝不 emit 0 tests exist。
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
