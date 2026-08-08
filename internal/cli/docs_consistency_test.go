package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/docsconsistency"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
)

// Docs consistency guard — dogfood of the docs-consistency-guard skill.
//
// 文档一致性守卫——dogfood docs-consistency-guard skill。
//
// Background: on 2026-06-27 we found that skills/code-review-gate/references/experience-loop.md
// referenced forge commands that did not exist (subcommand-level broken chain). README.md was also behind
// (missing the skills command family). This kind of drift cannot be caught by manual pre-release checks — the
// docs-consistency-guard conclusion: use guard tests (run every CI) rather than commands/hooks/skills
// (commands rely on humans remembering to run = same trap; hooks have no good trigger point;
// skills depend on agent compliance and will leak).
//
// 背景：2026-06-27 发现 skills/code-review-gate/references/experience-loop.md
// 引用了不存在的 forge 命令（子命令级断链）。README.md 也落后（缺 skills 命令族）。
// 这类 drift 靠发布前人肉查挡不住——docs-consistency-guard 的结论：用守卫测试
// （每次 CI 跑）而非命令/hook/skill（命令靠人记得跑 = 同一个坑；hook 无合适触发点；
// skill 靠 agent 遵循会漏）。
//
// Source of truth: the cobra command tree of rootCmd (programmatically extractable, see internal/cli/*.go's
// rootCmd.AddCommand / xxxCmd.AddCommand). Derived docs: root README + npm/README
// (npm package page) + skills/**/*.md (canonical skill library, distributed to each agent as the source).
//
// 真相源：rootCmd 的 cobra 命令树（程序可提取，见 internal/cli/*.go 的
// rootCmd.AddCommand / xxxCmd.AddCommand）。衍生文档：根 README + npm/README
// （npm 包页面）+ skills/**/*.md（canonical skill 库，分发到各 agent 的源）。
//
// Detection logic (regexp extracts backtick forge references -> validates the command tree level by level) has been
// pushed down into the shared internal/docsconsistency package so both consumers reuse it: this file (CI guards A/B)
// + the task-complete advisory in taskpipeline executor.go (local pre-commit reminder). See
// skills/docs-consistency-guard/SKILL.md for details.
//
// 检测逻辑（regexp 抽反引号 forge 引用 → 逐级验证命令树）已下沉到 internal/docsconsistency
// 共享包，让两处消费方共用：本文件（CI 守卫 A/B）+ taskpipeline executor.go 的
// task-complete advisory（本地提交前提醒）。详见 skills/docs-consistency-guard/SKILL.md。

// repoRoot walks up two levels from the internal\cli package directory to the repository root (E:\Forge). go test's cwd
// is the package directory, so tests use relative paths to read the manually maintained docs at the repo root.
//
// repoRoot 相对 internal\cli 包目录上溯两级 = 仓库根（E:\Forge）。go test 的 cwd
// 是包目录，故测试用相对路径读仓库根的手维护文档。
const repoRoot = "../.."

// guardedDocs collects the manually maintained docs to be guarded: the root README + npm copy + all .md under the canonical skill library.
// Excludes .claude/ (gitignored generated artifacts whose consistency is guarded by the skillgen generator's content assertion tests).
//
// guardedDocs 收集待守卫的手维护文档：根 README + npm 副本 + canonical skill 库全部 .md。
// 不含 .claude/（gitignored 生成物，其一致性由 skillgen 生成器的 content 断言测试守护）。
func guardedDocs(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, p := range []string{"README.md", "npm/README.md"} {
		files = append(files, filepath.Join(repoRoot, p))
	}
	skillsRoot := filepath.Join(repoRoot, "skills")
	if err := filepath.WalkDir(skillsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".md") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk skills/: %v", err)
	}
	return files
}

// TestValidateForgePath proves under the real rootCmd that docsconsistency.ValidateForgePath catches drift.
// Mechanism unit tests (mock tree) live in internal/docsconsistency/check_test.go; here we prove real command-tree
// integration — cli init has registered the command-tree callbacks, so real experience/task/init/skills commands can be
// validated level by level. In particular it covers the real ghosts missed on 2026-06-27 (experience propose/review:
// parent command exists but subcommand does not) and the wrong-parent attachments (skills complete: complete is under
// task). If this fails = callbacks not registered or integration broken.
//
// TestValidateForgePath 在真实 rootCmd 下证明 docsconsistency.ValidateForgePath 抓 drift。
// 机制单测（mock 树）在 internal/docsconsistency/check_test.go；这里证明真实命令树集成——
// cli init 已注册命令树回调，真实 experience/task/init/skills 等命令可被逐级验证。
// 尤其含 2026-06-27 漏掉的真实 ghost（experience propose/review：父命令存在但子命令
// 不存在）和错挂父级（skills complete：complete 在 task 下）。这条不过 = 回调未注册或集成断链。
func TestValidateForgePath(t *testing.T) {
	cases := []struct {
		name string
		ref  string
		want string // 空 = 路径完整；非空 = 首个断链的子命令
	}{
		{"单层命令", "init", ""},
		{"三层命令", "task gate", ""},
		{"flag 后即停", "init --mode small", ""},
		{"占位符后即停", "task gate <gate-id>", ""},
		{"方括号后即停", "sync [--force]", ""},
		{"分隔符后即停", "init small|medium", ""},
		{"裸 forge", "", ""},
		{"错挂父级", "skills complete", "complete"},
		{"顶层就不存在", "nonexistent", "nonexistent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := docsconsistency.ValidateForgePath(tc.ref); got != tc.want {
				t.Fatalf("ValidateForgePath(%q) = %q, want %q", tc.ref, got, tc.want)
			}
		})
	}
}

// TestDocs_NoGhostForgeCommands guard A: every backtick-wrapped forge command path in the manually maintained docs
// must actually exist in the cobra command tree. Subcommand-level precise validation — for `forge experience propose`
// experience exists but propose is not its subcommand, still caught (this is exactly the drift missed on 2026-06-27).
// Wrong-parent attachments are also caught (e.g. mistakenly writing `forge skills complete`: complete is under task,
// not skills). Detection goes through docsconsistency.DriftedCommands (same logic as the task-complete advisory).
//
// TestDocs_NoGhostForgeCommands 守卫 A：所有手维护文档里反引号包裹的 forge 命令
// 路径必须真实存在于 cobra 命令树。子命令级精确验证——`forge experience propose`
// 里 experience 存在但 propose 不是其子命令，照样抓（这正是 2026-06-27 漏掉的 drift）。
// 路径错挂父级也抓（如误写 `forge skills complete`：complete 挂在 task 下非 skills）。
// 检测走 docsconsistency.DriftedCommands（与 task-complete advisory 同一逻辑）。
func TestDocs_NoGhostForgeCommands(t *testing.T) {
	for _, file := range guardedDocs(t) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		rel, _ := filepath.Rel(repoRoot, file)
		for _, drifted := range docsconsistency.DriftedCommands(string(body)) {
			t.Errorf("%s: 文档引用了不存在的 forge 命令 `forge %s`（真相源：internal/cli/*.go 的 cobra 命令树）",
				rel, drifted)
		}
	}
}

// TestReadme_CoversAllTopLevelCommands guard B: every non-hidden top-level command of rootCmd
// must appear in the root README. Prevents adding a new command group (e.g. mcp/skills) but forgetting the README
// command reference. Only guards the root README — npm/README is the slimmed-down npm package page (intentionally
// lists only the core command groups); its correctness is covered separately by guard A (no ghost commands). Hidden
// commands (e.g. forge hook, called by scripts not users) and cobra-auto-injected help/completion are not required
// in the README.
//
// TestReadme_CoversAllTopLevelCommands 守卫 B：rootCmd 的每个非隐藏顶层命令
// 必须出现在根 README。防"新增命令组（如 mcp/skills）但 README 命令参考漏写"。
// 只守卫根 README——npm/README 是 npm 包页面的精简版（故意只列核心命令组），
// 由守卫 A（无幽灵命令）单独覆盖其正确性。Hidden 命令（如 forge hook，调用方是
// 脚本不是用户）和 cobra 自动注入的 help/completion 不要求进 README。
func TestReadme_CoversAllTopLevelCommands(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	s := string(body)
	for _, c := range rootCmd.Commands() {
		if c.Hidden {
			continue
		}
		name := c.Name()
		if name == "help" || name == "completion" {
			continue
		}
		if !strings.Contains(s, "forge "+name) {
			t.Errorf("README.md 缺顶层命令 `forge %s`（rootCmd 注册了它；新增命令组须同步命令参考表）", name)
		}
	}
}

// flagDocGrandfather pins the flags that are intentionally undocumented in the
// root README (ratchet baseline, same philosophy as skillRefAllowlist). The
// README is a command reference, not an exhaustive flag manual — most of these
// are sub-command detail flags. Any flag NOT in this list must appear in the
// root README (guard D) — so a NEW user-facing flag fails CI until the author
// either documents it or consciously adds it here with a reason.
//
// flagDocGrandfather 钉住故意不进根 README 的 flag（棘轮基线，与
// skillRefAllowlist 同哲学）。README 是命令参考而非穷尽式 flag 手册——这里
// 大多是子命令细节 flag。不在此表的 flag 必须出现在根 README（守卫 D）——
// 新增用户可见 flag 不进 README 就会让 CI 挂，作者二选一：补文档，或注明
// 理由后加进本表。
var flagDocGrandfather = map[string]bool{
	`adapters --apply`:   true,
	`attach --session`:   true,
	`audit --gate`:       true,
	`block --resolution`: true,
	`check --file`:       true, `check --threshold`: true,
	`confirm --fingerprint`: true,
	`decide --affects`:      true, `decide --by`: true, `decide --commit`: true,
	`decide --diagnosis`: true, `decide --evidence`: true, `decide --outcome`: true,
	`decide --probe-run`: true, `decide --rationale`: true, `decide --revision`: true,
	`drift-check --target`:   true,
	`eval-baseline --run-id`: true, `eval-gen --all`: true,
	`eval-record --agent-model`: true, `eval-record --forge-version`: true,
	`eval-report --baseline`: true, `eval-report --verbose`: true,
	`finding --source`: true, `finding --evidence`: true,
	`gate --silent`:          true,
	`init --agents`:          true,
	`install --drift-policy`: true, `install --skip-quality`: true,
	`install --skip-require-check`: true, `install --with-adapters`: true, `install --target`: true,
	`list --timeline`:            true,
	`override --acceptance-gate`: true, `override --skill-decisions`: true, `override --test-coverage`: true,
	`resume --compact-flag`: true, `resume --hook`: true, `resume --no-attach`: true, `resume --reinject`: true,
	`revert --decision`: true, `revert --edit`: true,
	`score --history`:    true,
	`skills --canonical`: true,
	`start --from-issue`: true, `start --goal`: true, `start --kind`: true,
	`start --origin-tool`: true, `start --parent`: true, `start --plan-file`: true, `start --title`: true,
	`status --system`: true, `status --tasks`: true, `status --agents`: true,
	`usage --top`: true, `usage --undertrigger`: true,
	`verify --collect-golden`: true, `verify --regression`: true, `verify --run-tests`: true, `verify --scenario`: true,
}

// TestReadme_NewFlagsAreDocumented guard D (ratchet): every non-hidden flag NOT
// in flagDocGrandfather must appear in the root README. Root-cause fix for the
// 2026-08 user-level-assets gap: `init --project` and `uninstall --restore`
// shipped with zero doc coverage — guards A/B only check command references,
// not the flag layer, so the drift sailed through. Existing undocumented flags
// are grandfathered (README is not an exhaustive flag manual); the guard only
// forces a doc decision for NEW flags.
//
// TestReadme_NewFlagsAreDocumented 守卫 D（棘轮）：不在 flagDocGrandfather 里的
// 非隐藏 flag 必须出现在根 README。2026-08 user-level-assets 缺口的根治：
// `init --project` 与 `uninstall --restore` 零文档上线——守卫 A/B 只查命令
// 引用不查 flag 层，drift 直接穿过。存量未文档 flag 已豁免进基线（README
// 不是穷尽式 flag 手册）；守卫只强制新增 flag 做文档决策。
func TestReadme_NewFlagsAreDocumented(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(repoRoot, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	s := string(body)
	for _, id := range docsconsistency.AllFlags(rootCmd) {
		if flagDocGrandfather[id] {
			continue
		}
		name := id[strings.Index(id, " --")+3:]
		if !strings.Contains(s, "--"+name) {
			t.Errorf("README.md 缺 flag --%s（%s；新增用户可见 flag 须进 README，或注明理由加进 flagDocGrandfather）", name, id)
		}
	}
}

// skillRefAllowlist codifies the "which backtick multi-segment tokens are NOT skill
// references" knowledge, gathered by manual audit of canonical skills (the 2026-07
// frontend dangling-link sweep's false-positive categories). A token here is a
// confirmed non-skill. Without this the DanglingSkillRefs guard (C) would false-positive
// on every tool/pattern/example token. A NEW multi-segment backtick token not in
// knownSkills nor here makes guard C fail — the author then judges: real broken link
// (fix the doc) or non-skill (add it here with its category).
//
// skillRefAllowlist 代码化"哪些反引号多段 token 不是 skill 引用"的知识，由 canonical skills
// 人工审计得出（2026-07 frontend 断链梳理的 false-positive 类别）。此处 token 是已确认非
// skill。无此表 DanglingSkillRefs 守卫（C）会对每个工具/模式/示例 token 误报。不在
// knownSkills 也不在此表的新增多段反引号 token 使守卫 C fail——作者判断：真断链（改文档）
// 或非 skill（加此表并注明类别）。
var skillRefAllowlist = map[string]bool{
	// review 模式名（cheat-scan deterministic 分类标签，非 skill）
	`assertion-strip`: true, `comment-only-fix`: true, `complexity-report`: true,
	`dead-branch`: true, `error-swallow`: true, `type-suppression`: true, `test-run`: true,
	// code-review-gate 子 agent 预设契约名（轨道 A/B，非 skill）
	`cheat-detector`: true, `eng-reviewer`: true,
	// release-readiness 子角色（审计维度 M1-M7/R1，非独立 skill）
	`release-risks-auditor`: true, `runtime-readiness-auditor`: true,
	// hook 名（on-demand-guards 自动挡，非 skill）
	`hazard-guard`: true, `freeze-guard`: true,
	// forge 命令/门禁（task gate ID，非 skill）
	`task-implement`: true, `task-verify`: true,
	// forge skills 子命令名（eval 命令族，CONVENTIONS 命令清单引用，非 skill）
	`eval-gen`: true, `eval-cases`: true, `eval-record`: true, `eval-report`: true, `eval-baseline`: true,
	// metadata 扩展字段名示例（CONVENTIONS §4，非 skill）
	`severity-levels`: true,
	// 外部 GitHub 仓库名（品牌 DESIGN.md 资产库，非 skill）
	`awesome-design-md`: true,
	// forge 机制 / deterministic 扫描名（PlanScope drift、cheat-scan，非 skill）
	`scope-drift`: true, `cheat-scan`: true,
	// metadata.pattern 合法值（ValidPatterns，非 skill 名）
	`tool-wrapper`: true,
	// references 文件前缀 / design-artifact 环节类型 / backend-development §2.1 类别
	`template-`: true, `test-design`: true, `api-design`: true,
	// 工具 / 技术指令（非 skill）
	`eslint-disable`: true, `force-push`: true, `git-secrets`: true, `v-html`: true, `yt-dlp`: true,
	// 命名示例（正例 / 反例 / 演示，非 skill）
	`async-test-helpers`: true, `condition-based-waiting`: true, `debug-techniques`: true,
	`my-skill-name`: true, `smic-fa`: true, `text-color-primary`: true, `user-avatar-card`: true,
	// code-review-gate 非正式简称（code-review-gate 的缩写引用）
	`code-review`: true,
	// 外部 lark skill（用户全局 skill，非 Forge canonical）
	`lark-workflow-meeting-summary`: true, `lark-doc`: true, `lark-shared`: true,
	// 飞书 CLI 工具（独立安装，非 forge 自带，非 skill）
	`lark-cli`: true,
	// CSS 属性 / 媒体查询（纳入的 frontend/design specialist 文档引用，非 skill）
	`grid-template-columns`: true, `box-shadow`: true, `backdrop-filter`: true,
	`background-image`: true, `prefers-reduced-motion`: true,
	// HTML / data 属性（非 skill）
	`aria-label`: true, `data-testid`: true, `data-palette`: true, `data-theme`: true,
	// frontmatter / 规范字段名与高信号关键词（validation-rules.md 引用，非 skill）
	`allowed-tools`: true, `post-generation`: true,
	// 工具（非 skill）
	`axe-core`: true,
	// 命名示例（token / store / 组件 / 品牌风格 / 快照，非 skill）
	`bg-brand-primary`: true, `cart-store`: true, `pulse-dot`: true, `active-stripe`: true,
	`dell-1996`: true, `nintendo-2001`: true, `bmw-m`: true, `snapshot-2026-06-21-pre-redesign`: true,
	// API key 占位符示例（非 skill）
	`sk-xxx`: true,
}

// TestSkills_NoDanglingSkillRefs guard C: every backtick-wrapped multi-segment kebab
// token in canonical skill docs must be either a real skill (in the canonical set) or a
// human-confirmed non-skill (in skillRefAllowlist). Single-segment tokens (tools/keywords)
// are exempt — see DanglingSkillRefs doc. This is the root-cause fix for the 2026-07
// frontend dangling-link regression (frontend-development referenced
// frontend-stack-selection / ai-generated-ui-review / frontend-aesthetics-execution as
// skills that did not exist). Like guard A, it is hard (CI fails) so drift is caught
// mechanically, not by agent compliance.
//
// TestSkills_NoDanglingSkillRefs 守卫 C：canonical skill 文档里每个反引号多段 kebab token
// 必须是真 skill（在 canonical 集）或人工确认非 skill（在 skillRefAllowlist）。单段 token
// （工具/关键字）豁免——见 DanglingSkillRefs 文档。这是 2026-07 frontend 断链回归的根治
// （frontend-development 引用了不存在的 frontend-stack-selection / ai-generated-ui-review /
// frontend-aesthetics-execution）。同守卫 A，hard（CI 失败）使 drift 被机械抓，不靠 agent 遵循。
func TestSkills_NoDanglingSkillRefs(t *testing.T) {
	skillsRoot := filepath.Join(repoRoot, "skills")
	names, err := skillsdist.ListSkills(skillsRoot)
	if err != nil {
		t.Fatalf(`ListSkills(skills/): %v`, err)
	}
	known := make(map[string]bool, len(names))
	for _, n := range names {
		known[n] = true
	}
	bt := string(rune(0x60)) // 反引号，绕过双引号腐蚀
	for _, file := range guardedDocs(t) {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf(`read %s: %v`, file, err)
		}
		rel, _ := filepath.Rel(repoRoot, file)
		for _, ref := range docsconsistency.DanglingSkillRefs(string(body), known, skillRefAllowlist) {
			t.Error(rel + `: 引用疑似断链 skill ` + bt + ref + bt + `（不在 canonical skill 集；若是模式名/工具/示例/契约等非 skill，加入 skillRefAllowlist 并注明类别）`)
		}
	}
}

// TestCanonicalSkillNamesAreMultiSegment is the meta-guard for DanglingSkillRefs's
// single-segment exemption: that exemption is only safe while every canonical skill
// name is multi-segment kebab (so exempting single-segment tokens never masks a real
// skill). If a single-segment skill name is ever added, this fails and flags the
// exemption for review — otherwise the guard would silently miss a dangling
// single-segment skill ref.
//
// TestCanonicalSkillNamesAreMultiSegment 是 DanglingSkillRefs 单段豁免的 meta 守卫：该豁免
// 仅当所有 canonical skill 名都是多段 kebab 时安全（豁免单段 token 才不会掩盖真 skill）。
// 若新增单段 skill 名，此测试 fail 并提示复审豁免——否则守卫会静默漏抓单段 skill 断链。
func TestCanonicalSkillNamesAreMultiSegment(t *testing.T) {
	skillsRoot := filepath.Join(repoRoot, "skills")
	names, err := skillsdist.ListSkills(skillsRoot)
	if err != nil {
		t.Fatalf(`ListSkills(skills/): %v`, err)
	}
	for _, n := range names {
		if !strings.Contains(n, "-") {
			t.Errorf(`canonical skill %q 是单段名——DanglingSkillRefs 单段豁免会漏抓其断链；调整守卫策略`, n)
		}
	}
}
