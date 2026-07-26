package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/docsconsistency"
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
