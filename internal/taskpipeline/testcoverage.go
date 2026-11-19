package taskpipeline

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// testCoverageDisableEnv 让项目可关闭 test-coverage 门控
// （与 FORGE_WORK_ACTIVITY 对称）。合理场景：仅文档仓库、
// 生成代码占比高的模块、或 task 只动到 whitelist 文件。
// CLI 在门禁失败消息里明示此逃生舱，避免被静默绕过。
const testCoverageDisableEnv = "FORGE_TEST_COVERAGE"

// sourceExts 是 test-coverage 门控认定的「源码」后缀集。
var sourceExts = map[string]bool{
	".go": true, ".rs": true, ".ts": true, ".tsx": true,
	".js": true, ".jsx": true, ".py": true, ".java": true,
	".rb": true, ".zig": true, ".nim": true,
}

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
	// substr 在路径任意位置子串匹配（如 .gen.、/dto/）。
	substr string
	// baseExact 精确匹配路径末段（如 main.go）。
	baseExact string
}

var testCoverageWhitelist = []whitelistEntry{
	// 入口。
	{baseExact: "main.go"},
	{substr: "cmd/"},
	// 生成代码。
	{substr: ".gen."},
	{substr: "_generated."},
	{substr: ".pb.go"},
	{substr: ".pb.rs"},
	{substr: ".pb.dart"},
	// 纯类型/协议/dto 定义。
	{baseExact: "types.ts"},
	{baseExact: "types.js"},
	{baseExact: "types.go"},
	{substr: "/dto/"},
	{baseExact: "dto.go"},
	{substr: "/models/"},
	// 内嵌资产目录：作为运行时数据分发的打包内容，非受测项目源码。
	// forge 把 skill 库放在 skills/*（分发的 skill 脚本/文档供 AI 消费——
	// 非编译/测试单元）。无此豁免，每个提交的 skill 脚本（.ts/.py）
	// 都会假阳性触发门控失败。匹配 skills/ 以放行根资产目录，
	// 同时不影响 internal/cli/skills_install.go 等同名源码。
	{substr: "skills/"},
	// skills-forge/（2026-08 迁移后的 forge 原生 skill 覆盖层）同属分发的打包资产：
	// 「skills/」子串匹配不到「skills-forge/」（斜杠位置不同），单独放行。带尾斜杠，
	// 避免误放行 internal/cli/skills-forge-utils.go 类未来源码文件（review W6）。
	{substr: "skills-forge/"},
	// 内嵌资产容器：embed.go 只持有 //go:embed 指令与静态资源声明（dashboard UI、
	// skills 资产），无 Go 逻辑可单元测试。无此豁免，文件级 hasMatchingTest 检查
	// 会把它标为改动源码无配对测试。
	{baseExact: "embed.go"},
	// Rust 入口——与 Go crate 的 main.go 对等。baseExact 匹配 basename，
	// 故 src/main.rs 和 src-tauri/src/main.rs 都命中。Rust 惯例：二进制
	// 声明 src/main.rs，库声明 src/lib.rs（Tauri 侧为 src-tauri/src/lib.rs）。
	// 集成测试位于 tests/ 而非平级 _test.rs 兄弟文件，harness 通过
	// cargo run/cargo test 测试二进制——文件级 hasMatchingTest 会把这类
	// 入口 crate 误标。dogfood 2.1②。
	{baseExact: "main.rs"},
	{baseExact: "lib.rs"},
	// Tauri command 胶水目录——src-tauri/src/ 持有 #[tauri::command] handler 和
	// tokio::spawn IPC 桥代码。Tauri runtime 通过 cargo tauri dev/build 端到端
	// 验证它们，而非通过同文件单元测试；惯用的 __tests__ 摆放不适用。结尾的
	// 斜杠限定到目录 scope——混合 Rust+TS 项目的根 src/ 不受影响。dogfood 2.1②。
	{substr: "src-tauri/"},
}

// CheckNameTestCoverage 是 test-coverage 门控决策的 checklog 条目名，
// 使 trace 能展示门禁裁定（而非仅各次 edit 的 WARN）。
const CheckNameTestCoverage checklog.CheckName = "test-coverage-gate"

// coverageEscapeActive 记录 bypass 审计条目，并在 test-coverage 门控被逃生（per-task
// override 或全局 env）时返回 true——由 CheckTestCoverage 及其预计算列表变体共用，
// 任何调用路径都绕不过审计（A4 + 方案5：逃生舱使用必须留痕）。审计理由见下方内联注释。
func coverageEscapeActive(root string, state *TaskState) bool {
	if !escapeDisabled(state, escapeTestCoverage, testCoverageDisableEnv) {
		return false
	}
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
		Meta:    map[string]string{"escape.gate": "test-coverage", "escape.reason": checklog.EscapeReasonOverride, "escape.owner": state.TaskRef},
	})
	return true
}

// CheckTestCoverage computes the changed set itself and reports missing test coverage.
//
// CheckTestCoverage enforces CLAUDE.md rule 4 (测试伴随变更): every non-whitelisted
// source file changed during the task must have a corresponding test file also
// changed. 返回 (ok, missing, total)：未改源码 / 所有改动源码均在 whitelist /
// 每个都有配对测试时 ok=true；total 是需配对测试的改动源码文件数
// （covered = total-len(missing)），供覆盖率报告连续展示而非二元 ok。
//
// 导出是为了让完成事实采集（BuildTaskEvidence）能复用 task-verify 门禁算出的精确裁定，
// 而非从 git diff 重新推导覆盖——后者在 task 改动于 task start 前已 commit 时
// 低估覆盖（HeadCommit == HEAD → 空 diff → testing 维度看不见测试）。
//
// 优雅降级：非 git 仓库或空 diff → ok=true（不误报）。
//
// 本入口自行计算 changed 集。已经算过 taskChangedFiles 的调用方（executor 各 gate
// 为 phase 推断/行为面提示算过一次）必须改用 checkTestCoverageChanged——
// taskChangedFiles 起多个 git 子进程，不应每 gate 跑两遍（2026-08-29 审查轮：双算消除）。
func CheckTestCoverage(root string, state *TaskState) (ok bool, missing []string, total int) {
	if coverageEscapeActive(root, state) {
		return true, nil, 0
	}
	return checkTestCoverageChanged(root, state, taskChangedFiles(root, state))
}

// checkTestCoverageChanged 是 CheckTestCoverage 的内部变体，接受【预计算】的改动文件
// 列表（来自 taskChangedFiles）。行为与 CheckTestCoverage 完全一致——含逃生舱审计，
// 传入 gitChanged 的 executor 调用点不会意外跳过审计。门控语义的完整注释见上方
// CheckTestCoverage。
func checkTestCoverageChanged(root string, state *TaskState, changed []string) (ok bool, missing []string, total int) {
	if coverageEscapeActive(root, state) {
		return true, nil, 0
	}
	if len(changed) == 0 {
		return true, nil, 0
	}
	missing, total = coveragePairing(changed)
	return len(missing) == 0, missing, total
}

// coveragePairing is the pure source↔test pairing pass shared by the gate path
// (checkTestCoverageChanged, behind the escape early-out) and the disclosure
// path (CoveragePairingForDisclosure, deliberately WITHOUT the escape
// early-out): both surfaces must use the identical pairing or the evidence
// bundle would contradict the gate. No audit writes here — audit is the
// gate path's concern (coverageEscapeActive).
//
// coveragePairing 是源码↔测试配对的纯计算 pass，被门禁路径（checkTestCoverage-
// Changed，逃生早退之后）与披露路径（CoveragePairingForDisclosure，刻意【不做】
// 逃生早退）共享：两个面必须同一配对口径，否则证据束会与门禁互相矛盾。此处
// 不写审计行——留痕是门禁路径（coverageEscapeActive）的职责。
func coveragePairing(changed []string) (missing []string, total int) {
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
	return missing, total
}

// CoveragePairingForDisclosure computes the untested-area list for the evidence
// bundle, deliberately IGNORING the test-coverage escape: the gate may be
// bypassed, but the disclosure must still state what was not verified (the
// honesty rule "override 免的是门禁，不是报告的诚实性" — an empty
// UntestedAreas under an active escape would launder the gap). No audit rows —
// the escape usage is already recorded once by the gate path.
//
// CoveragePairingForDisclosure 为证据束计算未测区域，刻意【无视】test-coverage
// 逃生：门禁可以被绕过，但披露仍须声明什么没被验证（诚实规则「override 免的
// 是门禁，不是报告的诚实性」——逃生激活时 UntestedAreas 为空等于把缺口洗白）。
// 不写审计行——逃生使用已由门禁路径记录过一次。
func CoveragePairingForDisclosure(root string, state *TaskState) []string {
	missing, _ := coveragePairing(taskChangedFiles(root, state))
	return missing
}

// testCoverageHardGateThreshold 是 task-complete 兜底硬阻断的最小「无配对测试的源文件数」。
// ≥此阈值且零断言才阻断——小改（<3 文件）fudge factor 豁免（对齐业界 Sonar <20 行豁免精神，
// 但用文件数代理行数以避免 git diff 行数计算 + 循环依赖）。eval 证据：feat/eval-core 0/19、
// feat/m2 0/25 是典型 corrupt success（大改零覆盖）；fix/m2-review-fixes 0/2 是 13 行小修，
// fudge factor 豁免合理。
const testCoverageHardGateThreshold = 3

// TestCoverageHardGateThreshold exports the task-complete backstop threshold so user-facing copy outside this package (test-nudge tier 3) derives from the same constant instead of a hand-copied number.
//
// TestCoverageHardGateThreshold 导出 task-complete 兜底阈值，供包外面向用户的文案
// （test-nudge tier3）从同一常量取值，不再手抄数字。
const TestCoverageHardGateThreshold = testCoverageHardGateThreshold

// taskChangedFiles 返回 task 期间改动的文件集合。
// 含 task 的 HeadCommit（task start 时记录）之后的已提交改动加工作树——
// 使 covered/total 只计本 task 文件，与 scoring.resolveDiffBase 对齐。否则共享
// 同一 feature 分支的 task 会把前序 task 的 commit 累积进 main...HEAD
// （feat/evidence-chain 回归：fully-tested change 上 testing=20）。HeadCommit 缺失
// （legacy state）时才回落 main...HEAD。clean tree 且无 task 专属 commit 时为空。
//
// 已提交部分再经跨任务归属过滤（taskattribution.go）：在 HeadCommit..HEAD 内全部
// 触及 commit 都属于某个【已完成】兄弟任务跨度的文件被排除——它们在该任务
// complete 时已记过账，此处再计会让 scope/test-coverage 双重收费，并逼 agent 把
// 没碰过的文件 `scope add` 进声明（2026-08 usage 实证）。
//
// 跨任务归属之前，先剔除只经合并基线分支带入的文件（mergedin.go）：任务中途合并
// main 后，HeadCommit..HEAD 的净 diff 会含 main 的改动，scope-drift / test-coverage /
// cheat-scan / mutation 都会把它们当成本任务的工作。
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
				// 跨任务归属（2026-08-25，fix/gate-loopholes）：丢弃已被【已完成】
				// 兄弟任务记账的文件（见 taskattribution.go）——它们的 commit 落在
				// 本任务 HeadCommit 之后但不是本任务的工作。下方工作树/untracked
				// 来源保持不过滤。
				var committed []string
				for _, line := range strings.Split(string(out), "\n") {
					if line = strings.TrimSpace(line); line != "" {
						committed = append(committed, line)
					}
				}
				// 合并带入（mergedin.go，2026-10-22）：任务分支中途合并基线后，
				// 只经合并另一侧带进来的文件不是本任务的工作，先剔除再做跨任务归属。
				for _, f := range excludeForeignCommitted(root, state, ExcludeMergedInFiles(root, state.HeadCommit, committed)) {
					if !seen[f] {
						seen[f] = true
						files = append(files, f)
					}
				}
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

	// 工作树（staged + unstaged），始终相关——覆盖未提交改动。
	out, err := exec.Command("git", "-C", root, "diff", "--name-only", "HEAD").Output()
	if err == nil {
		add(out)
	}

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

	// L3 归属过滤（multi-task-concurrency §6，T3）：丢弃台账可证明归属【其他未完成任
	// 务】会话的工作树/untracked 路径——共享工作目录里那是另一个窗口的 WIP，不是本
	// 任务的变更集。反方向刻意 fail-safe：无主路径（台账解释不了）保留——bash 生
	// 成、台账漏记的新文件仍必须计入本任务的覆盖义务。FORGE_ATTRIBUTION=0 → 空集
	// → L3 之前的行为。
	ownRef := ""
	if state != nil {
		ownRef = state.TaskRef
	}
	if foreign := ForeignAttributedPaths(root, ownRef); len(foreign) > 0 {
		kept := files[:0]
		for _, f := range files {
			if !foreign[filepath.ToSlash(filepath.Clean(f))] {
				kept = append(kept, f)
			}
		}
		files = kept
	}

	return files
}

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
	return sourceExts[filepath.Ext(path)]
}

// isTestFile 报告 path 自身是否疑似测试文件。目录判定按【路径段整段】匹配
// （test/tests/__tests__）而非子串——子串匹配把 contest/、latest/、attest/ 等
// 生产目录整体判成测试目录，形成"豁免测试义务 + 逃出 cheat/unused 扫描 +
// scope 评分不计改动量"的可注册逃逸区（2026-08-29 审查轮功能实证）。与
// phase_detect.go 的 isTestPhasePath 同判据。
func isTestFile(path string) bool {
	base := filepath.Base(path)
	for _, pat := range []string{"_test.", "_spec.", ".test.", ".spec."} {
		if strings.Contains(base, pat) {
			return true
		}
	}
	// 「test_」前缀形态——仅 Python 与 Ruby（pytest test_*.py / minitest test_*.rb
	// 惯例）：配对探针需要识别 test_root.py，但 scoring 侧钉住的用例
	//（test_utils.go，scope_test.go）表明该前缀不得推广到 Go——Go 的惯例是
	// *_test.go 后缀，"test_utils.go" 是辅助文件不是测试。规则：ext ∈ {.py,.rb}
	// 且前缀后带真 stem。注意：scoring/evaluator.go 的 isTestPath 镜像此判定
	//（同样仅 .py/.rb）。
	if ext := filepath.Ext(base); (ext == ".py" || ext == ".rb") &&
		strings.HasPrefix(base, "test_") && len(base) > len("test_")+len(ext) {
		return true
	}
	// Java 驼峰测试名（JUnit/Maven 惯例）：FooTest.java / FooTests.java / FooIT.java。
	// 用于与 hasMatchingTest 的 default 分支配对自洽：配对接受 Main.java 的
	// stem+"Test.java"，若无此判定，刚配对成功的 MainTest.java 自身会再进 missing
	//（2026-08-29 验收：Main.java+MainTest.java 两侧都要成立）。限定 .java 扩展名，
	// 不影响其他语言的小写形态惯例。
	if filepath.Ext(base) == ".java" &&
		(strings.HasSuffix(base, "Test.java") || strings.HasSuffix(base, "Tests.java") || strings.HasSuffix(base, "IT.java")) {
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == "test" || seg == "tests" || seg == "__tests__" {
			return true
		}
	}
	return false
}

// isWhitelisted 报告源码文件是否豁免测试要求。
func isWhitelisted(path string) bool {
	base := filepath.Base(path)
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

// testCandidates 列出 src 的惯用**直接配对**测试路径（按语言惯例，同目录精确
// 配对为主 + py 的 tests/ 变体）。单一约定表：门禁（hasMatchingTest，对 changed
// 集合判存在）与 nudge（TestPairsSource，单文件探针）共用——两侧约定漂移会让
// nudge 预演的门禁与实际执行的门禁分叉（testcoverage.go 的单一真相源契约）。
func testCandidates(src string) []string {
	src = filepath.ToSlash(src)
	base := strings.TrimSuffix(src, filepath.Ext(src))
	ext := filepath.Ext(src)
	dir := filepath.ToSlash(filepath.Dir(src))
	name := filepath.ToSlash(filepath.Base(src))
	d := ""
	if dir != "." && dir != "" {
		d = dir + "/"
	}
	stem := filepath.ToSlash(filepath.Base(base))

	switch ext {
	case ".go":
		return []string{base + "_test.go"}
	case ".rs":
		return []string{base + "_test.rs"}
	case ".ts", ".tsx":
		// 四形态全列（与历史行为一致——a.ts 的测试允许以 a.test.tsx 形态出现）。
		return []string{base + ".test.ts", base + ".test.tsx", base + ".spec.ts", base + ".spec.tsx"}
	case ".js", ".jsx":
		return []string{base + ".test.js", base + ".test.jsx", base + ".spec.js", base + ".spec.jsx"}
	case ".py":
		// base 已含目录（pkg/mod）——同目录 foo_test.py 形态不得再叠目录前缀
		// （历史实现 pyDir+base 双拼出 pkg/pkg/mod_test.py，永不命中的死候选，
		// 由 TestTestPairsSource 2026-09-17 钉出修正）。
		return []string{d + "test_" + name, base + "_test.py", "tests/test_" + name}
	default:
		// java/rb 的语言特定精确候选；zig/nim 的 stem+"_test."+自身扩展形态由
		// hasMatchingTest 的前缀扫描 / TestPairsSource 的前缀判定覆盖（无法枚举）。
		switch ext {
		case ".java":
			return []string{d + stem + "Test.java", d + stem + "Tests.java", d + stem + "IT.java"}
		case ".rb":
			return []string{d + stem + "_spec.rb", d + stem + "_test.rb", d + "test_" + name}
		}
		return nil
	}
}

// hasMatchingTest 推断改动源码文件的惯用测试文件路径，并在改动集合里查它
// （各语言惯例见 testCandidates；Go 的 package 级兜底与 default 语言的前缀扫描
// 依赖 changed 集合，留在本函数）。
func hasMatchingTest(src string, changed map[string]bool) bool {
	// git 在所有平台都以 forward slashes 报仓库相对路径。
	// 归一源码路径以匹配：filepath.Dir 跑 Clean，Windows 下会把 '/' 转成
	// OS 分隔符 '\'，而 changed 的 key 保持 forward-slash。不 ToSlash 时，
	// 下面的 package-level 兜底在 Windows 上静默永不命中——破坏门控
	// （及测试覆盖事实的 B3 live 兜底）对任何多目录 package 的判定。
	src = filepath.ToSlash(src)
	for _, p := range testCandidates(src) {
		if changed[p] {
			return true
		}
	}
	ext := filepath.Ext(src)
	dir := filepath.ToSlash(filepath.Dir(src))

	switch ext {
	case ".go":
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
	case ".rs", ".ts", ".tsx", ".js", ".jsx", ".py":
		// 直接候选已判——这些语言无集合级兜底。
		return false
	default:
		// java/rb/zig/nim——同目录 stem+"_test."+任意扩展（zig foo_test.zig、nim
		// foo_test.nim、java foo_test.java……）。前缀被 d+"/" 限定，兄弟源码的
		// spec（poller_daemon_spec.rb）永不匹配 poller。
		base := strings.TrimSuffix(src, ext)
		stem := filepath.ToSlash(filepath.Base(base))
		d := ""
		if dir != "." && dir != "" {
			d = dir + "/"
		}
		prefix := d + stem + "_test."
		for f := range changed {
			if len(f) > len(prefix) && strings.HasPrefix(f, prefix) {
				return true
			}
		}
		return false
	}
}
