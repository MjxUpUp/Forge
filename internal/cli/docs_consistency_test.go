package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/docsconsistency"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
)

// 文档一致性守卫——dogfood docs-consistency-guard skill。
//
// 背景：2026-06-27 发现 skills/code-review-gate/references/experience-loop.md
// 引用了不存在的 forge 命令（子命令级断链）。README.md 也落后（缺 skills 命令族）。
// 这类 drift 靠发布前人肉查挡不住——docs-consistency-guard 的结论：用守卫测试
// （每次 CI 跑）而非命令/hook/skill（命令靠人记得跑 = 同一个坑；hook 无合适触发点；
// skill 靠 agent 遵循会漏）。
//
// 真相源：rootCmd 的 cobra 命令树（程序可提取，见 internal/cli/*.go 的
// rootCmd.AddCommand / xxxCmd.AddCommand）。衍生文档：根 README + npm/README
// （npm 包页面）+ skills/**/*.md（canonical skill 库，分发到各 agent 的源）。
//
// 检测逻辑（regexp 抽反引号 forge 引用 → 逐级验证命令树）已下沉到 internal/docsconsistency
// 共享包，让两处消费方共用：本文件（CI 守卫 A/B）+ taskpipeline executor.go 的
// task-complete advisory（本地提交前提醒）。详见 skills/docs-consistency-guard/SKILL.md。

// repoRoot 相对 internal\cli 包目录上溯两级 = 仓库根（E:\Forge）。go test 的 cwd
// 是包目录，故测试用相对路径读仓库根的手维护文档。
const repoRoot = "../.."

// guardedDocs 收集待守卫的手维护文档：根 README + 完整命令参考 + Hook 参考 + npm 副本 +
// canonical skill 库全部 .md + forge 原生 skills-forge/ 覆盖层（2026-08 零反向依赖迁移
// 移出 skills/——不补进 walk 会让守卫面静默缩小）。不含 .claude/（gitignored 生成物，
// 其一致性由 skillgen 生成器的 content 断言测试守护）。
func guardedDocs(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, p := range []string{
		"README.md",
		"npm/README.md",
		// 2026-10 命令表从根 README 拆出（README 只留速查）——完整表仍是手维护文档，
		// 幽灵命令守卫不能因为搬家而静默失去覆盖面。
		"docs/reference/cli.md",
		"docs/reference/hooks.md",
	} {
		files = append(files, filepath.Join(repoRoot, p))
	}
	for _, root := range []string{filepath.Join(repoRoot, "skills"), filepath.Join(repoRoot, "skills-forge")} {
		if err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".md") {
				files = append(files, path)
			}
			return nil
		}); err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return files
}

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

// readmeDocs 是「根 README + 拆出去的完整命令参考」这一组文档：守卫 B/D 检查的是
// 用户可见文档面，而不是某一个文件。2026-10 命令表搬家到 docs/reference/cli.md 后，
// 若守卫仍只读 README，完整性检查会静默失效（搬家 = 覆盖面归零）。
func readmeDocs() []string {
	return []string{
		filepath.Join(repoRoot, "README.md"),
		filepath.Join(repoRoot, "docs/reference/cli.md"),
	}
}

// TestReadme_CoversAllTopLevelCommands 守卫 B：rootCmd 的每个非隐藏顶层命令
// 必须出现在根 README 或其完整命令参考中。防"新增命令组（如 mcp/skills）但文档漏写"。
// npm/README 是 npm 包页面的精简版（故意只列核心命令组），由守卫 A（无幽灵命令）单独
// 覆盖其正确性。Hidden 命令（如 forge hook，调用方是脚本不是用户）和 cobra 自动注入的
// help/completion 不要求进文档。
func TestReadme_CoversAllTopLevelCommands(t *testing.T) {
	var sb strings.Builder
	for _, p := range readmeDocs() {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		sb.Write(body)
		sb.WriteString("\n")
	}
	s := sb.String()
	for _, c := range rootCmd.Commands() {
		if c.Hidden {
			continue
		}
		name := c.Name()
		if name == "help" || name == "completion" {
			continue
		}
		if !strings.Contains(s, "forge "+name) {
			t.Errorf("README.md / docs/reference/cli.md 缺顶层命令 `forge %s`（rootCmd 注册了它；新增命令组须同步命令参考表）", name)
		}
	}
}

// TestCliReference_CoversAllTopLevelCommands 守卫 B'（拆家后的逐文件完备性）。
// 守卫 B 判的是 README∪cli.md 并集，单看并集有个洞：把某个命令从 cli.md 删掉、只在根
// README 速查里留一行，CI 不会响——本仓库实测 38 个非隐藏顶层命令里有 17 个可由 README
// 单独满足。命令表搬到 docs/reference/cli.md 后，**这一份才是"完整"的那份**，它的完备性
// 不能靠并集兜底，否则"完整命令参考"会慢慢退化成第二份速查表。
func TestCliReference_CoversAllTopLevelCommands(t *testing.T) {
	path := filepath.Join(repoRoot, "docs/reference/cli.md")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	s := string(body)
	for _, c := range rootCmd.Commands() {
		name := c.Name()
		if c.Hidden || name == "help" || name == "completion" {
			continue
		}
		if !strings.Contains(s, "forge "+name) {
			t.Errorf("docs/reference/cli.md 缺顶层命令 `forge %s`（本文是完整命令参考，新命令组须在此有行，不能只出现在根 README 速查）", name)
		}
	}
}

// flagDocGrandfather 钉住故意不进根 README 的 flag（棘轮基线，与
// skillRefAllowlist 同哲学）。README 是命令参考而非穷尽式 flag 手册——这里
// 大多是子命令细节 flag。不在此表的 flag 必须出现在根 README（守卫 D）——
// 新增用户可见 flag 不进 README 就会让 CI 挂，作者二选一：补文档，或注明
// 理由后加进本表。
var flagDocGrandfather = map[string]bool{
	// W0.2 batch 单入口的接线内部 flag——由 ForgeHookWiring 自动写入接线，
	// 用户不手输（hook 名册的增删走棘轮/普查，不走文档）。
	`batch --event`:      true,
	`batch --matcher`:    true,
	`adapters --apply`:   true,
	`attach --session`:   true,
	`audit --gate`:       true,
	`block --resolution`: true,
	`check --file`:       true, `check --threshold`: true,
	`confirm --fingerprint`: true,
	`decide --affects`:      true, `decide --by`: true, `decide --commit`: true,
	// skills 子命令的 scope flag（--global/--project 二选一）：此前从未真被文档化，
	// 守卫 D 的全局子串匹配被根 README 里 dashboard 行的 `--global` 假阳性掩盖；
	// dashboard 收敛为全局单面板删掉该 flag 后暴露。子命令细节 flag，进基线。
	`drift-check --global`: true,
	`install --global`:     true,
	`decide --diagnosis`:   true, `decide --evidence`: true, `decide --outcome`: true,
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

// TestReadme_NewFlagsAreDocumented 守卫 D（棘轮）：不在 flagDocGrandfather 里的
// 非隐藏 flag 必须出现在「根 README ∪ docs/reference/cli.md」这一用户可见文档面。
// 2026-08 user-level-assets 缺口的根治：`init --project` 与 `uninstall --restore`
// 零文档上线——守卫 A/B 只查命令引用不查 flag 层，drift 直接穿过。存量未文档 flag
// 已豁免进基线（文档面不是穷尽式 flag 手册）；守卫只强制新增 flag 做文档决策。
// 判定粒度是文档面级子串，不是「同一条命令行内」：AllFlags 的 id 由 c.Name() 拼成
// （internal/docsconsistency/check.go:197 `id := c.Name() + " --" + f.Name`），只带叶子
// 命令名、不带父命令路径——`start --ref` 无法反推“这一行属于 `forge task start`”。
// 按行绑定需要完整命令路径（或另建路径映射），属独立变更。
func TestReadme_NewFlagsAreDocumented(t *testing.T) {
	var sb strings.Builder
	for _, p := range readmeDocs() {
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		sb.Write(body)
		sb.WriteString("\n")
	}
	s := sb.String()
	for _, id := range docsconsistency.AllFlags(rootCmd) {
		if flagDocGrandfather[id] {
			continue
		}
		name := id[strings.Index(id, " --")+3:]
		if !strings.Contains(s, "--"+name) {
			t.Errorf("README.md / docs/reference/cli.md 缺 flag --%s（%s；新增用户可见 flag 须进文档，或注明理由加进 flagDocGrandfather）", name, id)
		}
	}
}

// skillRefAllowlist 代码化"哪些反引号多段 token 不是 skill 引用"的知识，由 canonical skills
// 人工审计得出（2026-07 frontend 断链梳理的 false-positive 类别）。此处 token 是已确认非
// skill。无此表 DanglingSkillRefs 守卫（C）会对每个工具/模式/示例 token 误报。不在
// knownSkills 也不在此表的新增多段反引号 token 使守卫 C fail——作者判断：真断链（改文档）
// 或非 skill（加此表并注明类别）。
var skillRefAllowlist = map[string]bool{
	// checklog 观察条目名（forge selfcheck 落痕，非 skill）
	`selfcheck-pairing`: true, `selfcheck-scope`: true,
	// checklog 观察条目名（skills decide --warrant 理由锚点核验行、证据来源核验行，非 skill）
	`decision-warrant`: true, `decision-evidence`: true,
	// checklog 观察条目名（golden test 层查询计数行，eval golden run --release 落痕，非 skill）
	`eval-golden-test-query`: true,
	// research-workflow status.json 的 mode 取值（调研路由模式名，非 skill）
	`file-only`: true, `file-augmented`: true,
	// acceptance v2 断言类型名与逐断言证据行 check 名（spec-as-gate L2 P1，非 skill）
	`exit`: true, `contains`: true, `not-contains`: true, `file-changed`: true,
	`file-untouched`: true, `acceptance-assert`: true,
	// review 模式名（cheat-scan deterministic 分类标签，非 skill）
	`assertion-strip`: true, `comment-only-fix`: true, `complexity-report`: true,
	`dead-branch`: true, `error-swallow`: true, `type-suppression`: true, `test-run`: true,
	`comment-as-debt`: true, `phantom-import`: true, `path-assumption`: true,
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
	// git 分支名（project sync 同步通道固定分支，非 skill）
	`forge-sync`: true,
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
	// eval 黄金集策展 case 的 ID 前缀约定（g-<skill>-t1，非 skill）
	`g-`: true,
	// forge-design pack skill（2026-09 设计族拆包至 plugins/forge-design，非核心 canonical；
	// 核心 skill 以反引号指针引用它们——"属 forge-design pack，未安装则忽略"，真引用非断链）
	`frontend-feature-development`: true, `frontend-stack-selection`: true,
	`frontend-aesthetics-execution`: true, `frontend-code-review`: true,
	`ai-generated-ui-review`: true, `ai-ui-generation-workflow`: true,
	`design-system-workflow`: true, `design-system-migration`: true,
	`design-review-snapshot`: true, `design-artifact-standards`: true,
	`design-audit`: true, `ui-iteration-feedback-loop`: true,
}

// skillRefDocs 是守卫 C（skill 断链）的文档集：canonical skill 两棵树 + 两份用户入口
// README。**不含 docs/reference/**——那两份是命令/flag 参考，通篇 kebab 术语
// （heldout-pool、data-dir、checklog、各 hook 名），喂进"多段反引号 token 必须是真 skill"
// 的判定后，后续任何 `foo-bar` 都会报"skill 断链"，而报文指向 skills/（与作者实际改的
// 文档无关）。这两份文档由守卫 A/B/B'/D 负责，命令层的 drift 已被机械钉住。
func skillRefDocs(t *testing.T, docs []string) []string {
	t.Helper()
	refPrefix := filepath.Join(repoRoot, "docs", "reference") + string(filepath.Separator)
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if strings.HasPrefix(d, refPrefix) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// TestSkills_NoDanglingSkillRefs 守卫 C：canonical skill 文档里每个反引号多段 kebab token
// 必须是真 skill（在 canonical 集）或人工确认非 skill（在 skillRefAllowlist）。单段 token
// （工具/关键字）豁免——见 DanglingSkillRefs 文档。这是 2026-07 frontend 断链回归的根治
// （frontend-development 引用了不存在的 frontend-stack-selection / ai-generated-ui-review /
// frontend-aesthetics-execution）。同守卫 A，hard（CI 失败）使 drift 被机械抓，不靠 agent 遵循。
func TestSkills_NoDanglingSkillRefs(t *testing.T) {
	// known = neutral skills/ ∪ forge-native skills-forge/：6 个中立 skill 的 SKILL.md
	// 仍引用 skill-authoring-standard 等 forge 原生 skill（forge 用户经合并缓存/插件
	// 全量可达，非断链——2026-08 迁移后两棵树都是合法引用目标）。
	known := make(map[string]bool)
	for _, root := range []string{filepath.Join(repoRoot, "skills"), filepath.Join(repoRoot, "skills-forge")} {
		names, err := skillsdist.ListSkills(root)
		if err != nil {
			t.Fatalf(`ListSkills(%s): %v`, root, err)
		}
		for _, n := range names {
			known[n] = true
		}
	}
	bt := string(rune(0x60)) // 反引号，绕过双引号腐蚀
	for _, file := range skillRefDocs(t, guardedDocs(t)) {
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

// TestHooksDoc_MatchesHookSpec 守卫 E：Hook 清单与"N 个内置 Hook"计数必须与真相源
// internal/hooks/settings.go 的 ForgeHookSpec() 对齐。Hook 表 2026-10 从根 README 搬到
// docs/reference/hooks.md 后存在两处静默面：(1) internal/cli/cli_test.go 只断言 .sh 脚本
// 数 == len(HookNames())，9 个 Go 原生 hook 与表行数之间没有任何守卫；(2) 计数在这三处
// 各自漂移过（README 27 / npm 22 / zh 19）——新增或删除 hook 不会让 CI 响，只有人读文档
// 时才会发现。表行首列形如 `| **task-guard** | Write/Edit 前 | …`。
func TestHooksDoc_MatchesHookSpec(t *testing.T) {
	truth := make(map[string]bool)
	for _, matchers := range hooks.ForgeHookSpec() {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if name, ok := strings.CutPrefix(h.Command, "forge hook "); ok {
					truth[name] = true
				}
			}
		}
	}
	if len(truth) == 0 {
		t.Fatal("ForgeHookSpec() 里没有 `forge hook <name>` 条目——真相源结构变了，改写本守卫而不是放行")
	}

	docPath := filepath.Join(repoRoot, "docs/reference/hooks.md")
	body, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	inDoc := make(map[string]bool)
	for _, line := range strings.Split(string(body), "\n") {
		rest, ok := strings.CutPrefix(line, "| **")
		if !ok {
			continue
		}
		if i := strings.Index(rest, "**"); i > 0 {
			inDoc[rest[:i]] = true
		}
	}
	for name := range truth {
		if !inDoc[name] {
			t.Errorf("docs/reference/hooks.md 缺 Hook `%s`（ForgeHookSpec() 注册了它；新增 hook 须进完整清单）", name)
		}
	}
	for name := range inDoc {
		if !truth[name] {
			t.Errorf("docs/reference/hooks.md 列了 Hook `%s`，但 ForgeHookSpec() 没注册（已删 hook 的陈旧行）", name)
		}
	}

	// 计数声明必须**每一处**都等于真相源。只断言「每份文档至少有一处正确声明」会放过同
	// 一文件里的第二处声明——hooks.md:19 的「完整清单（27 个）」与 :49 的「Go 原生共 9 个 /
	// 其余 18 个」漂了，同文件 :3 仍正确时守卫照样绿（首版守卫 E 就有这个洞，2026-10 二轮
	// review 用局部变异抓出）。各文档措辞不同，故按 pattern 逐声明匹配；某个 pattern 一个
	// 也匹配不到同样报错——否则改个措辞就静默解除覆盖。
	bashHooks := len(hooks.HookNames())
	claims := []struct {
		file    string
		pattern *regexp.Regexp
		want    int
		desc    string
	}{
		{"README.md", regexp.MustCompile(`(\d+) 个内置 [Hh]ook`), len(truth), "内置 Hook 总数"},
		{"npm/README.md", regexp.MustCompile(`(\d+) 个内置 [Hh]ook`), len(truth), "内置 Hook 总数"},
		{"npm/README.md", regexp.MustCompile(`完整 (\d+) 个清单`), len(truth), "完整清单计数"},
		{"READMEs/README.zh-CN.md", regexp.MustCompile(`(\d+) 个 [Hh]ook`), len(truth), "hook 总数（zh 措辞）"},
		{"docs/reference/hooks.md", regexp.MustCompile(`(\d+) 个内置 [Hh]ook`), len(truth), "内置 Hook 总数"},
		{"docs/reference/hooks.md", regexp.MustCompile(`完整清单（(\d+) 个）`), len(truth), "完整清单计数"},
		{"docs/reference/hooks.md", regexp.MustCompile(`Go 原生共 (\d+) 个`), len(truth) - bashHooks, "Go 原生 hook 数"},
		{"docs/reference/hooks.md", regexp.MustCompile(`其余 (\d+) 个为内嵌`), bashHooks, "内嵌 bash hook 数"},
	}
	for _, c := range claims {
		b, err := os.ReadFile(filepath.Join(repoRoot, c.file))
		if err != nil {
			t.Fatalf("read %s: %v", c.file, err)
		}
		matches := c.pattern.FindAllStringSubmatch(string(b), -1)
		if len(matches) == 0 {
			t.Errorf("%s 里找不到 %q 形式的计数声明（%s）——措辞变了请同步本守卫，否则该计数无人钉",
				c.file, c.pattern.String(), c.desc)
			continue
		}
		for _, m := range matches {
			n, convErr := strconv.Atoi(m[1])
			if convErr != nil {
				t.Errorf("%s 的计数 %q 解析失败: %v", c.file, m[0], convErr)
				continue
			}
			if n != c.want {
				t.Errorf("%s 的计数 %q = %d，真相源 %d（%s）", c.file, m[0], n, c.want, c.desc)
			}
		}
	}
}
