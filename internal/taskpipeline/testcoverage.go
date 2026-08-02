package taskpipeline

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// testCoverageDisableEnv lets a project turn off the test-coverage gate
// (symmetric with FORGE_WORK_ACTIVITY). Reasonable scenarios: docs-only repos,
// modules dominated by generated code, or tasks that only touch whitelist files.
// The CLI surfaces this escape hatch in the gate failure message so it cannot be
// silently bypassed.
//
// testCoverageDisableEnv 让项目可关闭 test-coverage 门控
// （与 FORGE_WORK_ACTIVITY 对称）。合理场景：仅文档仓库、
// 生成代码占比高的模块、或 task 只动到 whitelist 文件。
// CLI 在门禁失败消息里明示此逃生舱，避免被静默绕过。
const testCoverageDisableEnv = "FORGE_TEST_COVERAGE"

// sourceExts is the set of file extensions the test-coverage gate treats as source.
// An earlier bash advisory hook (embedded via hooks/embed.go) mirrored this set and was
// deleted during hook trimming; this gate is now the single source of truth.
//
// sourceExts 是 test-coverage 门控认定的「源码」后缀集。早期 hooks/embed.go 内嵌的一层
// bash advisory hook 镜像此集合，hook 精简时已删——本门控现是唯一真相源。
var sourceExts = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".py": true, ".java": true,
	".rb": true, ".zig": true, ".nim": true,
}

// testCoverageWhitelist describes source files exempt from the test requirement:
//   - Entry points: main.go, cmd/** main binaries
//   - Generated code: *.gen.*, *_generated.*, *.pb.* protobuf bindings
//   - Pure type/protocol definitions: no executable logic to test
//   - Embedded asset directories: go:embed payloads distributed as runtime data
//
// Matched against forward-slash repo-relative paths.
// (An earlier version mirrored this list in a bash advisory hook; that hook was
// removed during trimming and this gate layer is now the single source of truth.)
//
// testCoverageWhitelist 描述免测试要求的源码文件：
//   - 入口：main.go、cmd/** main 入口二进制
//   - 生成代码：*.gen.*、*_generated.*、*.pb.* protobuf 绑定
//   - 纯类型/协议定义：无可执行逻辑需测试
//   - 内嵌资产目录：go:embed 内容作运行时数据分发
//
// 按 forward-slash 仓库相对路径匹配。
// （早期版本在 bash advisory hook 里镜像此清单；该 hook 在精简时已删，
// 本门控层现是唯一真相源。）
type whitelistEntry struct {
	// substr matches anywhere in the path as a substring (e.g. .gen., /dto/).
	//
	// substr 在路径任意位置子串匹配（如 .gen.、/dto/）。
	substr string
	// baseExact matches the final path segment exactly (e.g. main.go).
	//
	// baseExact 精确匹配路径末段（如 main.go）。
	baseExact string
}

var testCoverageWhitelist = []whitelistEntry{
	// Entry points.
	//
	// 入口。
	{baseExact: "main.go"},
	{substr: "cmd/"},
	// Generated code.
	//
	// 生成代码。
	{substr: ".gen."},
	{substr: "_generated."},
	{substr: ".pb.go"},
	{substr: ".pb.rs"},
	{substr: ".pb.dart"},
	// Pure type/protocol/dto definitions.
	//
	// 纯类型/协议/dto 定义。
	{baseExact: "types.ts"},
	{baseExact: "types.js"},
	{baseExact: "types.go"},
	{substr: "/dto/"},
	{baseExact: "dto.go"},
	{substr: "/models/"},
	// Embedded asset directories: packaged payload distributed as runtime data,
	// not project source under test. Forge keeps its skill library under skills/*
	// (distributed skill scripts/docs consumed by the AI — not compile/test units).
	// Without this exemption every committed skill script (.ts/.py) would falsely
	// trip the gate. Match skills/ to release the root asset directory without
	// affecting same-named source like internal/cli/skills_install.go.
	//
	// 内嵌资产目录：作为运行时数据分发的打包内容，非受测项目源码。
	// forge 把 skill 库放在 skills/*（分发的 skill 脚本/文档供 AI 消费——
	// 非编译/测试单元）。无此豁免，每个提交的 skill 脚本（.ts/.py）
	// 都会假阳性触发门控失败。匹配 skills/ 以放行根资产目录，
	// 同时不影响 internal/cli/skills_install.go 等同名源码。
	{substr: "skills/"},
	// Embedded hook-script container: internal/hooks/embed.go holds shell scripts as
	// Go string constants (HazardGuardHook, WorkflowTestGuardHook, etc.). There is no
	// Go logic to unit test — script behavior is verified end-to-end by internal/e2e
	// (e.g. TestHook_HazardGuard_BlocksHazardousCommand). Without this exemption the
	// file-level hasMatchingTest check (which looks for embed_test.go in the same
	// package) would falsely flag it as changed source without a paired test.
	//
	// 内嵌 hook 脚本容器：internal/hooks/embed.go 把 shell 脚本作为 Go string 常量
	// （HazardGuardHook、WorkflowTestGuardHook 等）持有。无 Go 逻辑可单元测试——
	// 脚本行为由 internal/e2e 端到端验证（如 TestHook_HazardGuard_BlocksHazardousCommand）。
	// 无此豁免，文件级 hasMatchingTest 检查（在同 package 找 embed_test.go）会
	// 误把它标为改动源码无配对测试。
	{baseExact: "embed.go"},
	// Rust entry points — symmetric to main.go in a Go crate. baseExact matches the
	// basename, so both src/main.rs and src-tauri/src/main.rs hit it. Rust convention:
	// binaries live at src/main.rs, libraries at src/lib.rs (Tauri: src-tauri/src/lib.rs).
	// Integration tests live under tests/ rather than sibling _test.rs files, and the
	// harness exercises binaries via cargo run/cargo test — file-level hasMatchingTest
	// would mis-flag these entry crates. dogfood 2.1②.
	//
	// Rust 入口——与 Go crate 的 main.go 对等。baseExact 匹配 basename，
	// 故 src/main.rs 和 src-tauri/src/main.rs 都命中。Rust 惯例：二进制
	// 声明 src/main.rs，库声明 src/lib.rs（Tauri 侧为 src-tauri/src/lib.rs）。
	// 集成测试位于 tests/ 而非平级 _test.rs 兄弟文件，harness 通过
	// cargo run/cargo test 测试二进制——文件级 hasMatchingTest 会把这类
	// 入口 crate 误标。dogfood 2.1②。
	{baseExact: "main.rs"},
	{baseExact: "lib.rs"},
	// Tauri command glue directory — src-tauri/src/ holds #[tauri::command] handlers
	// and tokio::spawn IPC bridge code. The Tauri runtime verifies these end-to-end via
	// cargo tauri dev/build rather than through same-file unit tests; the conventional
	// __tests__ layout does not apply. The trailing slash scopes the match to the
	// directory — the root src/ of mixed Rust+TS projects is unaffected. dogfood 2.1②.
	//
	// Tauri command 胶水目录——src-tauri/src/ 持有 #[tauri::command] handler 和
	// tokio::spawn IPC 桥代码。Tauri runtime 通过 cargo tauri dev/build 端到端
	// 验证它们，而非通过同文件单元测试；惯用的 __tests__ 摆放不适用。结尾的
	// 斜杠限定到目录 scope——混合 Rust+TS 项目的根 src/ 不受影响。dogfood 2.1②。
	{substr: "src-tauri/"},
}

// CheckNameTestCoverage is the checklog entry name for the test-coverage gate
// decision, letting trace surface the gate verdict (not just per-edit WARNs).
//
// CheckNameTestCoverage 是 test-coverage 门控决策的 checklog 条目名，
// 使 trace 能展示门禁裁定（而非仅各次 edit 的 WARN）。
const CheckNameTestCoverage checklog.CheckName = "test-coverage-gate"

// CheckTestCoverage enforces CLAUDE.md rule 4 (tests accompany changes): every
// non-whitelisted source file changed during the task must have a corresponding test
// file also changed. Returns (ok, missing, total): ok=true when there is no changed
// source, all changed source is whitelisted, or every changed source has a paired
// test; total is the number of changed source files that need a paired test
// (covered = total-len(missing)), and the scoring dimension grades this continuously
// rather than as a binary ok.
//
// Exported so scoring (cli.scoreTask) can reuse the precise verdict computed by the
// task-verify gate instead of re-deriving coverage from git diff — the latter
// underestimates coverage when the task change was committed before task start
// (HeadCommit == HEAD → empty diff → the testing dimension sees no tests).
//
// Graceful degradation: non-git repo or empty diff → ok=true (no false positive).
//
// CheckTestCoverage enforces CLAUDE.md rule 4 (测试伴随变更): every non-whitelisted
// source file changed during the task must have a corresponding test file also
// changed. 返回 (ok, missing, total)：未改源码 / 所有改动源码均在 whitelist /
// 每个都有配对测试时 ok=true；total 是需配对测试的改动源码文件数
// （covered = total-len(missing)），评分维度据此连续打分而非二元 ok。
//
// 导出是为了让 scoring（cli.scoreTask）能复用 task-verify 门禁算出的精确裁定，
// 而非从 git diff 重新推导覆盖——后者在 task 改动于 task start 前已 commit 时
// 低估覆盖（HeadCommit == HEAD → 空 diff → testing 维度看不见测试）。
//
// 优雅降级：非 git 仓库或空 diff → ok=true（不误报）。
func CheckTestCoverage(root string, state *TaskState) (ok bool, missing []string, total int) {
	if escapeDisabled(state, escapeTestCoverage, testCoverageDisableEnv) {
		// A4 + plan 5: audit the bypass (per-task override OR global env). The hatch is
		// meant for reasonable scenarios (docs-only repo, generated code, whitelist-only
		// task), but its use must leave a trace — otherwise agents silently bypass the
		// test-coverage gate. UsedEscapeHatch → Strength cap Weak makes escape carry a
		// cost rather than being free.
		//
		// A4 + 方案5: audit the bypass (per-task override OR global env). The hatch is
		// 是合理场景（仅文档仓库、生成代码、whitelist-only task），但其使用必须
		// 留痕——否则 agent 会静默绕过 test-coverage 门控。UsedEscapeHatch → Strength
		// cap Weak 让逃生有代价而非免费。
		taskRef := ""
		if state != nil {
			taskRef = state.TaskRef
		}
		recordAudit(root, &checklog.Entry{
			Check:   checklog.CheckEscapeHatch,
			Passed:  true,
			Checked: true,
			Level:   checklog.LevelWarn, // 逃生舱使用是 warn 语义（bypass 已生效但须留痕），derive 只会给 pass
			TaskRef: taskRef,
			Detail:  "escape-hatch: test-coverage gate bypassed (per-task override or FORGE_TEST_COVERAGE=disable)",
		})
		return true, nil, 0
	}

	changed := taskChangedFiles(root, state)
	if len(changed) == 0 {
		return true, nil, 0
	}

	changedSet := make(map[string]bool, len(changed))
	for _, f := range changed {
		changedSet[f] = true
	}

	for _, f := range changed {
		if !isSourceFile(f) || isTestFile(f) {
			continue
		}
		if isWhitelisted(f) {
			continue
		}
		total++ // 应配对测试的源码文件数（评分按 covered/total 连续打分）
		if hasMatchingTest(f, changedSet) {
			continue
		}
		missing = append(missing, f)
	}

	return len(missing) == 0, missing, total
}

// testCoverageHardGateThreshold is the minimum count of changed source files without
// a paired test for the task-complete hard block. Blocks only when this threshold is
// met AND assertion count is zero — small changes (<3 files) get a fudge-factor
// exemption (aligning with the spirit of Sonar's <20-line exemption, but using file
// count as a proxy for line count to avoid git diff line counting + circular
// dependencies). Eval evidence: feat/eval-core 0/19 and feat/m2 0/25 are typical
// corrupt successes (large change, zero coverage); fix/m2-review-fixes 0/2 is a
// 13-line small fix where the fudge-factor exemption is appropriate.
//
// testCoverageHardGateThreshold 是 task-complete 兜底硬阻断的最小「无配对测试的源文件数」。
// ≥此阈值且零断言才阻断——小改（<3 文件）fudge factor 豁免（对齐业界 Sonar <20 行豁免精神，
// 但用文件数代理行数以避免 git diff 行数计算 + 循环依赖）。eval 证据：feat/eval-core 0/19、
// feat/m2 0/25 是典型 corrupt success（大改零覆盖）；fix/m2-review-fixes 0/2 是 13 行小修，
// fudge factor 豁免合理。
const testCoverageHardGateThreshold = 3

// testCoverageShouldBlock decides whether the task-complete fallback hard-blocks for
// missing tests. missingN is the count of changed source files WITHOUT a paired test
// (NOT total changed source files) — matching the threshold doc above: many missing
// (≥ threshold) AND zero assertions → block (corrupt success: changed source with
// neither paired tests nor any assertion). Few missing (< threshold, fudge factor —
// e.g. partial coverage of a well-tested change) or has-assertions-but-zero-paired
// coverage (tests live elsewhere / refactor scenario) → advisory pass. The assertion
// signal reuses scoring.CollectAssertionDensity (13 cross-language markers) to avoid the
// tautology trap of only checking whether a test exists (AI test fossilization bug).
//
// testCoverageShouldBlock 决定 task-complete 兜底是否对缺测试硬阻断。missingN 是
// 「无配对测试的改动源文件数」（非全部改动源文件数）——与上方阈值文档一致：缺测
// 多（≥阈值）且零断言 → 阻断（corrupt success：改了源码既无配对测试也无任何断言）。
// 缺测少（<阈值，fudge factor——如测试充分改动只漏 1 个文件的部分覆盖）或「有断言
// 但 0 配对覆盖」（测试在别处/重构场景）→ advisory 放行。断言信号复用
// scoring.CollectAssertionDensity（13 个跨语言 marker），避免只看「测试是否存在」
// 的同义反复陷阱（AI 测试固化 bug）。
func testCoverageShouldBlock(missingN, assertN int) bool {
	return missingN >= testCoverageHardGateThreshold && assertN == 0
}

// taskChangedFiles returns the set of files changed during the task.
// It includes committed changes after the task's HeadCommit (recorded at task start)
// plus the working tree — so covered/total only counts this task's files, aligning
// with scoring.resolveDiffBase. Otherwise tasks sharing a feature branch accumulate
// earlier tasks' commits into main...HEAD (feat/evidence-chain regression: a
// fully-tested change got testing=20). Falls back to main...HEAD only when HeadCommit
// is missing (legacy state). Empty when the tree is clean and there is no task-specific
// commit.
//
// taskChangedFiles 返回 task 期间改动的文件集合。
// 含 task 的 HeadCommit（task start 时记录）之后的已提交改动加工作树——
// 使 covered/total 只计本 task 文件，与 scoring.resolveDiffBase 对齐。否则共享
// 同一 feature 分支的 task 会把前序 task 的 commit 累积进 main...HEAD
// （feat/evidence-chain 回归：fully-tested change 上 testing=20）。HeadCommit 缺失
// （legacy state）时才回落 main...HEAD。clean tree 且无 task 专属 commit 时为空。
func taskChangedFiles(root string, state *TaskState) []string {
	var files []string
	seen := make(map[string]bool)

	add := func(out []byte) {
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || seen[line] {
				continue
			}
			seen[line] = true
			files = append(files, line)
		}
	}

	// 1. Committed changes during this task. Prefer the task's HeadCommit (recorded
	// at task start) so covered/total only counts this task's files — aligning with
	// scoring.resolveDiffBase. Otherwise tasks sharing a feature branch accumulate
	// earlier tasks' commits into main...HEAD, inflating the testing dimension and
	// false-flagging the current task's well-tested files as missing (feat/evidence-chain
	// regression: a fully-tested change got testing=20). Falls back to main...HEAD only
	// when no HeadCommit is recorded (legacy state / test-suite-modeled pre-start form).
	//
	// 1. 本 task 期间的已提交改动。优先用 task 的 HeadCommit（task start 时
	// 记录），使 covered/total 只计本 task 文件——与 scoring.resolveDiffBase
	// 对齐。否则共享同一 feature 分支的 task 会把前序 task 的 commit 累积进
	// main...HEAD，虚高 testing 维度并把当前 task 测试充分的文件误标为缺失
	// （feat/evidence-chain 回归：fully-tested change 上 testing=20）。无 HeadCommit
	// 记录时才回落 main...HEAD（legacy state / 测试套件建模的 pre-start 形态）。
	if state != nil {
		if state.HeadCommit != "" {
			out, err := exec.Command("git", "-C", root, "diff", "--name-only", state.HeadCommit+"..HEAD").Output()
			if err == nil {
				add(out)
			}
		} else if state.Branch != "" && state.Branch != "main" && state.Branch != "master" {
			for _, base := range []string{"main", "origin/main", "master", "origin/master"} {
				out, err := exec.Command("git", "-C", root, "diff", "--name-only", base+"...HEAD").Output()
				if err == nil {
					add(out)
					break
				}
			}
		}
	}

	// Working tree (staged + unstaged), always relevant — covers uncommitted changes.
	//
	// 工作树（staged + unstaged），始终相关——覆盖未提交改动。
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD").Output()
	if err == nil {
		add(out)
	}

	// Untracked files — newly created and not yet git-added. At task-verify time the
	// agent's newly created files are usually still untracked, so without this source
	// the gate cannot see them: a freshly written foo_test.go cannot satisfy the
	// sibling-file requirement for a just-changed foo.go, and test-coverage falsely
	// reports no paired test for files that definitely have one (feat/task-scope hit:
	// task.go modified-tracked + task_scope_test.go untracked → false advisory).
	// --exclude-standard skips .gitignored contents (node_modules, build output, stray
	// dashboard-render.png) so only genuine working-tree source is considered. Repo-wide
	// to match the working-tree semantics above — only the committed HeadCommit..HEAD
	// portion is task-scoped. Also fixes the symmetric blind spot of scope-drift
	// (untracked files outside PlanScope were previously invisible).
	//
	// Untracked 文件——新建尚未 git add。task-verify 时刻 agent 的新建文件
	// 通常仍 untracked，故无此来源门控看不见它们：刚写的 foo_test.go 无法满足
	// 刚改的 foo.go 兄弟文件，test-coverage 对确切有测试的文件假报无配对测试
	// （feat/task-scope 命中：task.go modified-tracked + task_scope_test.go
	// untracked → 假 advisory）。--exclude-standard 排除 .gitignored 内容
	// （node_modules、build output、游离的 dashboard-render.png），只考虑真正
	// 的工作树源码。Repo-wide 以匹配上文工作树语义——只有已提交的
	// HeadCommit..HEAD 部分是 task-scoped。也修了 scope-drift 的对称盲点
	// （PlanScope 之外的 untracked 文件原本不可见）。
	out, err = exec.Command("git", "-C", root, "ls-files", "--others", "--exclude-standard").Output()
	if err == nil {
		add(out)
	}

	return files
}

// isSourceFile reports whether path is a source file (not a test, not config).
// Vendored dependencies (any vendor/ path segment) are excluded: they are
// third-party baselines, never project code the task must pair tests with —
// a vendored update otherwise floods "missing tests" (cooking project: 986
// files). The exclusion also benefits scope-drift and DesignPhases inference,
// which share taskChangedFiles — dropping vendor noise is directionally
// consistent everywhere.
//
// isSourceFile 报告 path 是否为源码文件（非测试、非 config）。vendor/ 依赖
// 排除：它们是第三方基线，不是本任务要配对测试的项目代码——一次 vendor
// 更新会把 "missing tests" 打爆（cooking 项目报 986 文件）。该排除同时惠及
// 共用 taskChangedFiles 的 scope-drift 与 DesignPhases 推断——排除 vendor
// 噪声在各处方向一致。
func isSourceFile(path string) bool {
	norm := filepath.ToSlash(path)
	if strings.Contains(norm, "vendor/") {
		return false
	}
	ext := filepath.Ext(path)
	if !sourceExts[ext] {
		return false
	}
	return true
}

// isTestFile reports whether path itself looks like a test file.
//
// isTestFile 报告 path 自身是否疑似测试文件。
func isTestFile(path string) bool {
	for _, pat := range []string{"_test.", "_spec.", ".test.", ".spec.", "test/", "tests/", "__tests__/"} {
		if strings.Contains(path, pat) {
			return true
		}
	}
	return false
}

// isWhitelisted reports whether a source file is exempt from the test requirement.
//
// isWhitelisted 报告源码文件是否豁免测试要求。
func isWhitelisted(path string) bool {
	base := filepath.Base(path)
	// Normalize to forward slashes for cross-platform substring matching.
	//
	// 归一为 forward slashes 以跨平台子串匹配。
	norm := filepath.ToSlash(path)
	for _, w := range testCoverageWhitelist {
		if w.baseExact != "" && base == w.baseExact {
			return true
		}
		if w.substr != "" && strings.Contains(norm, w.substr) {
			return true
		}
	}
	return false
}

// hasMatchingTest infers the conventional test file path for a changed source file and
// looks it up in the changed set (per-language conventions below).
//
// hasMatchingTest 推断改动源码文件的惯用测试文件路径，并在改动集合里查它
// （各语言惯例见下）。
func hasMatchingTest(src string, changed map[string]bool) bool {
	// git reports repo-relative paths with forward slashes on every platform.
	// Normalize the source path to match: filepath.Dir runs Clean, which on Windows
	// converts '/' into the OS separator '\', while the keys of changed stay
	// forward-slashed. Without ToSlash, the package-level fallback below silently
	// never matches on Windows — breaking the gate's (and scoreTask's B3 live
	// fallback) judgment for any multi-directory package.
	//
	// git 在所有平台都以 forward slashes 报仓库相对路径。
	// 归一源码路径以匹配：filepath.Dir 跑 Clean，Windows 下会把 '/' 转成
	// OS 分隔符 '\'，而 changed 的 key 保持 forward-slash。不 ToSlash 时，
	// 下面的 package-level 兜底在 Windows 上静默永不命中——破坏门控
	// （及 scoreTask 的 B3 live 兜底）对任何多目录 package 的判定。
	src = filepath.ToSlash(src)
	base := strings.TrimSuffix(src, filepath.Ext(src))
	ext := filepath.Ext(src)
	dir := filepath.ToSlash(filepath.Dir(src))
	name := filepath.ToSlash(filepath.Base(src))

	switch ext {
	case ".go":
		// Convention: foo.go ↔ foo_test.go (preferred, most precise).
		//
		// 惯例：foo.go ↔ foo_test.go（首选、最精确）。
		if changed[base+"_test.go"] {
			return true
		}
		// Package-level fallback: Go testing is package-scoped by convention, so a test
		// file in the same directory as the source covers it even if the names do not
		// pair (e.g. tests for executor.go live in testcoverage_test.go). Without this
		// fallback the gate would falsely fail a well-tested package over sibling-naming
		// expectations. The fallback is still strict: a _test.go under the source
		// directory must exist in the changed set — directories with no test changes
		// still fail, so genuinely untested code is still caught.
		//
		// Package-level 兜底：Go 测试惯例 package-scoped，故源码同目录下的
		// 测试文件即便名字不配对也覆盖它（如 executor.go 的测试在
		// testcoverage_test.go 里）。无此兜底，门控会把测试命名按兄弟概念
		// 的测试充分 package 假性判失败。兜底仍严格：changed 集合中必须存在
		// 源码目录下的 _test.go——无测试改动的目录仍判失败，故真正未测
		// 代码仍能被抓到。
		pkgDir := strings.TrimSuffix(dir, "/") + "/"
		if pkgDir == "./" {
			pkgDir = ""
		}
		for f := range changed {
			if !strings.HasSuffix(f, "_test.go") {
				continue
			}
			if pkgDir == "" {
				// Root-level source: any root-level _test.go suffices.
				//
				// Root-level 源码：任一 root-level _test.go 即可。
				if !strings.Contains(strings.TrimSuffix(f, "_test.go"), "/") {
					return true
				}
				continue
			}
			if strings.HasPrefix(f, pkgDir) {
				return true
			}
		}
		return false
	case ".rs":
		if changed[base+"_test.rs"] {
			return true
		}
		// A Rust inline #[cfg(test)] module is also acceptable — but here we only see
		// file names, so we only accept the same-stem _test.rs.
		//
		// Rust inline #[cfg(test)] module 也可接受——但此处只能看文件名，
		// 故仅接受同文件名的 _test.rs。
		return false
	case ".ts", ".tsx":
		for _, p := range []string{base + ".test.ts", base + ".test.tsx", base + ".spec.ts", base + ".spec.tsx"} {
			if changed[p] {
				return true
			}
		}
		return false
	case ".js", ".jsx":
		for _, p := range []string{base + ".test.js", base + ".test.jsx", base + ".spec.js", base + ".spec.jsx"} {
			if changed[p] {
				return true
			}
		}
		return false
	case ".py":
		for _, p := range []string{dir + "/test_" + name, dir + "/" + base + "_test.py", "tests/test_" + name} {
			if changed[p] {
				return true
			}
		}
		return false
	default:
		// java/rb/zig/nim: accept any *_test.* or test_*.* with the same base name in
		// the changed set — conservative match.
		//
		// java/rb/zig/nim：在 changed 集合里接受同 base name 的任意 *_test.* 或
		// test_*.*——保守匹配。
		wantBase := base
		for f := range changed {
			if (strings.HasPrefix(f, wantBase) || strings.HasSuffix(filepath.Dir(f)+"/"+name, "_test")) && isTestFile(f) {
				return true
			}
		}
		return false
	}
}

// testCoverageDetail returns a short checklog detail string for the gate verdict
// (intentionally short — checklog Detail is a single line, not the user-facing
// failure message produced by formatMissing).
//
// testCoverageDetail 返回门禁裁定的简短 checklog detail 字符串（刻意短——
// checklog Detail 是单行，不是 formatMissing 产出的面向用户失败消息）。
func testCoverageDetail(ok bool, missing []string) string {
	if ok {
		return "all changed source files have corresponding tests"
	}
	if len(missing) > 3 {
		return fmt.Sprintf("missing tests for %d files: %s ...", len(missing), strings.Join(missing[:3], ", "))
	}
	return "missing tests for: " + strings.Join(missing, ", ")
}

// formatMissing produces the user-facing gate failure message.
// dogfood 4.2/2.1: the original advisory taught escape at the end (To bypass:
// FORGE_TEST_COVERAGE=disable), and in practice agents wrote disable into task plans
// as fixed procedure (DevWorkbench 330 times). Switched to injecting the test-discipline
// skill directive (mirroring code-review-gate's hook-enforced driving path) — escape is
// demoted to a trailing audit footnote instead of headline instruction.
//
// formatMissing 产出面向用户的门禁失败消息。
// dogfood 4.2/2.1：原 advisory 末尾教 escape（"To bypass: FORGE_TEST_COVERAGE=disable"），
// 实测让 agent 把 disable 写进 task plan 当固定流程（DevWorkbench 330 次）。改为注入
// test-discipline skill 指引（复刻 code-review-gate 的 hook 强制驱动路径）——escape 降级为
// 末尾审计脚注，不再当头条教学。
func formatMissing(missing []string) string {
	primary := "Add tests for changed source (CLAUDE.md rule 4: 测试伴随变更). " +
		"→ 加载 test-discipline skill（/test-discipline 或 skills/test-discipline/SKILL.md）：" +
		"测试质量守卫，区分单元测试与端到端、防断言弱化与假测试。" +
		"入口(main.go/cmd)/生成物(.gen./_generated/.pb.)/纯类型文件(types/dto/models)白名单免测。"
	footnote := " 确不可测时设 FORGE_TEST_COVERAGE=disable（落 checklog 审计，可 forge trace 追溯）。"
	if len(missing) > 5 {
		return fmt.Sprintf("%d source files changed without a corresponding test: %s ... (and %d more). %s%s",
			len(missing), strings.Join(missing[:5], ", "), len(missing)-5, primary, footnote)
	}
	return fmt.Sprintf("source files changed without a corresponding test: %s. %s%s",
		strings.Join(missing, ", "), primary, footnote)
}
