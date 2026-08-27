package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/spf13/cobra"
)

// harness.go implements the harness repo (multi-task-concurrency design §3 物理承载 +
// §11.4): the user-level home becomes a PRIVATE git repository — the process state of
// every managed repo gains git history, diff, remote sync — while machine-local state,
// ephemeral sentinels and TRUST ANCHORS (stamps/hazards) are gitignored and NEVER leave
// the machine (a clone is attacker-controllable input; migrate.go's 2026-08-15 trust
// boundary review precedent applies verbatim).
//
// HITL contract (§13): init's interactive confirmation runs only when stdin is a TTY —
// an agent's Bash tool has no TTY, so a non-interactive init is REFUSED unless --yes
// (scripted CI escape; agent discipline forbids self-serving --yes). Configuring a
// remote and the first push are separate human confirmations (push itself lands with T9).
//
// harness.go 实现 harness repo（multi-task-concurrency 设计 §3 物理承载 + §11.4）：
// 用户级 home 变成私有 git 仓库——所有受管 repo 的过程状态获得 git 史、diff、远端
// 同步——而机器本地态、短命哨兵与【信任锚】（stamps/hazards）被 gitignore、永不离
// 机（clone 是攻击者可控输入；migrate.go 2026-08-15 信任边界评审先例逐字适用）。
//
// HITL 契约（§13）：init 的交互确认仅在 stdin 是 TTY 时进行——agent 的 Bash 工具没
// 有 TTY，非交互 init 被拒绝，除非 --yes（脚本化 CI 逃生口；agent 纪律禁止自行使
// 用）。配置远端与首次 push 是独立的人工确认（push 本体随 T9 落地）。

// harnessGitignore is the exclusion list (§11.4): machine-local path bindings, ephemeral
// sentinels, per-session bookkeeping, deployed config copies, and trust anchors. Anything
// NOT listed is process state worth versioning and syncing.
//
// harnessGitignore 是排除清单（§11.4）：机器本地路径绑定、短命哨兵、会话簿记、部署
// 副本与信任锚。未列出的即是值得版本化与同步的过程状态。
const harnessGitignore = `# harness repo 排除清单（multi-task-concurrency §11.4）
# 机器本地 / 短命态 / 信任锚——永不外发（clone = 攻击者可控输入）

# 用户级：注册表与清单是路径键控的机器本地配置；节点身份不迁移；安装缓存随版本走
projects.json
workspaces.json
node.json
node-seq
harness-state
skills-cache/
update-cache.json

# 每项目：路径绑定（wtid 机器本地）
projects/*/workspaces/

# 每项目：归属台账与标记（短命观测）
projects/*/attribution/
projects/*/markers/
projects/*/toollog.jsonl
projects/*/toollog-*.jsonl

# 每项目：会话簿记与指针/哨兵（session 生命周期）
projects/*/sessions/
projects/*/active-task-ref
projects/*/active-task-ref-*
projects/*/session.json
projects/*/sessions.jsonl
projects/*/last-session.json
projects/*/.*.last
projects/*/.*-*
projects/*/*.lock

# 每项目：信任锚——review 印章与 hazard 确认绝不出本机（跨机走重验）
projects/*/stamps/
projects/*/hazards/

# 每项目：部署副本与运行时治理（随 forge 版本走/机器本地）
projects/*/hooks/
projects/*/protocol.yml
projects/*/quarantine/
projects/*/freeze/
projects/*/act/
`

var (
	harnessCommitMu sync.Mutex // 串行化本进程内的 harness 提交
)

func harnessHome() (string, error) {
	return forgedata.GlobalHome()
}

func harnessGit(home string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", home}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=forge", "GIT_AUTHOR_EMAIL=forge@localhost",
		"GIT_COMMITTER_NAME=forge", "GIT_COMMITTER_EMAIL=forge@localhost")
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// HarnessInitialized reports whether the user-level home is already a harness repo.
//
// HarnessInitialized 报告用户级 home 是否已是 harness repo。
func HarnessInitialized() bool {
	home, err := harnessHome()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".git"))
	return err == nil
}

// stdinIsTTY reports whether stdin is an interactive terminal (the HITL gate: an agent's
// Bash tool has no TTY, so interactive confirmation cannot be answered by an agent).
// POSIX: a subshell `test -t 0` inheriting OUR stdin — the shell's own tty check is
// accurate, unlike Stat heuristics (macOS chains /dev/stdin → /dev/fd/0 → target, which
// defeats name/mode matching). Windows: char-device heuristic minus NUL (no /bin/sh).
//
// stdinIsTTY 报告 stdin 是否交互终端（HITL 门：agent 的 Bash 工具无 TTY，交互确认
// 无法由 agent 代答）。POSIX：继承【本进程】stdin 的子壳 `test -t 0`——shell 自己
// 的 tty 判定是准的，Stat 启发式不准（macOS 的 /dev/stdin → /dev/fd/0 → 目标多层
// 间接击穿名字/mode 匹配）。Windows：字符设备启发式减 NUL（无 /bin/sh）。
func stdinIsTTY() bool {
	if runtime.GOOS != "windows" {
		cmd := exec.Command("sh", "-c", "test -t 0")
		cmd.Stdin = os.Stdin
		return cmd.Run() == nil
	}
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	name := os.Stdin.Name()
	if name == "/dev/stdin" {
		if real, rerr := filepath.EvalSymlinks(name); rerr == nil {
			name = real
		}
	}
	switch name {
	case "/dev/null", "NUL":
		return false
	}
	return true
}

func runHarnessInit(cmd *cobra.Command, args []string) error {
	home, err := harnessHome()
	if err != nil {
		return err
	}
	fromExisting, _ := cmd.Flags().GetBool("from-existing")
	remote, _ := cmd.Flags().GetString("remote")
	yes, _ := cmd.Flags().GetBool("yes")

	if HarnessInitialized() {
		return fmt.Errorf("harness repo 已存在于 %s（幂等拒绝；配置远端用 --remote 重复执行无害？否——请用 git remote add）", home)
	}

	// HITL：非 TTY（agent Bash）拒绝，除非 --yes（CI 逃生口，agent 纪律禁用）。
	if !stdinIsTTY() && !yes {
		return fmt.Errorf("harness init 需要人在终端确认（stdin 非 TTY——agent 不得代批，multi-task-concurrency §13）。" +
			"脚本化场景请由人显式加 --yes")
	}
	// 交互确认（TTY 场景）：展示将纳入/排除的内容后等待 yes。
	if stdinIsTTY() && !yes {
		fmt.Printf("将在 %s 建立私有 harness repo（multi-task-concurrency T6）\n", home)
		fmt.Printf("  纳入 git：projects/<key>/tasks、checklog、archive（过程状态与证据）\n")
		fmt.Printf("  永不纳入：stamps/hazards（信任锚）、workspaces/attribution（机器本地）、哨兵/锁\n")
		if fromExisting {
			fmt.Printf("  --from-existing：存量数据做一次基线提交（史前史入库，不重写任何文件）\n")
		}
		fmt.Printf("输入 yes 确认: ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil || !strings.EqualFold(answer, "yes") {
			return fmt.Errorf("未确认，放弃 init")
		}
	}

	if err := os.MkdirAll(home, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(home, ".gitignore"), []byte(harnessGitignore), 0o644); err != nil {
		return err
	}
	// Deterministic branch name（-b main，git<2.28 回落裸 init——默认名随环境漂移
	// 会造成 push/pull 的 upstream 错配，实测踩坑）。
	if _, err := harnessGit(home, "init", "-b", "main"); err != nil {
		if out, err2 := harnessGit(home, "init"); err2 != nil {
			return fmt.Errorf("git init 失败: %v\n%s", err2, out)
		}
	}
	if remote != "" {
		if out, err := harnessGit(home, "remote", "add", "origin", remote); err != nil {
			return fmt.Errorf("remote 配置失败: %v\n%s", err, out)
		}
		fmt.Printf("远端已记录: %s（首次 push 属外发动作，需人工另行确认——T9/T7）\n", remote)
	}
	// 基线提交（--from-existing 或空仓都做一次：.gitignore 本身也要入库）。
	if out, err := harnessGit(home, "add", "-A"); err != nil {
		return fmt.Errorf("git add 失败: %v\n%s", err, out)
	}
	msg := "harness init: 排除清单入库"
	if fromExisting {
		msg = "harness init --from-existing: 存量基线入库（史前史，不重写数据）"
	}
	if out, err := harnessGit(home, "commit", "-m", msg); err != nil {
		return fmt.Errorf("基线提交失败: %v\n%s", err, out)
	}
	MarkHarnessInitialized(remote != "")
	fmt.Printf("harness repo 已建立: %s\n", home)
	return nil
}

// HarnessCommitBestEffort batches the tracked process state into one git commit
// (multi-task-concurrency §13 提交策略). Called at task boundaries (start/complete) —
// never per hook (latency). Silent on every failure: versioning is an observability
// gain, its absence must not break the task flow. Serialized in-process; cross-process
// races degrade to git's own index lock retry window (acceptable for append-dominant
// stores).
//
// HarnessCommitBestEffort 把受管过程状态批量提交进 harness repo
// （multi-task-concurrency §13 提交策略）。在任务边界（start/complete）调用——绝不逐
// hook 提交（延迟）。一切失败静默：版本化是可观测性增益，其缺席不得打断任务流。
// 进程内串行化；跨进程竞态降级为 git 自身的 index 锁重试窗口（对 append 为主的
// store 可接受）。
func HarnessCommitBestEffort(reason string) {
	harnessCommitMu.Lock()
	defer harnessCommitMu.Unlock()
	if !HarnessInitialized() {
		return
	}
	home, err := harnessHome()
	if err != nil {
		return
	}
	if out, err := harnessGit(home, "add", "-A"); err != nil {
		_ = out
		return
	}
	out, err := harnessGit(home, "commit", "-m", "forge: "+reason)
	if err != nil && !strings.Contains(out, "nothing to commit") {
		_ = out
	}
}

func runHarnessStatus(cmd *cobra.Command, args []string) error {
	home, err := harnessHome()
	if err != nil {
		return err
	}
	if !HarnessInitialized() {
		fmt.Printf("harness: 未建立（%s 非 git 仓库）——forge harness init 引导建立\n", home)
		return nil
	}
	fmt.Printf("harness: 本地（%s）\n", home)
	if out, err := harnessGit(home, "remote", "get-url", "origin"); err == nil {
		fmt.Printf("remote: %s", out)
	} else {
		fmt.Printf("remote: 未配置（git remote add origin <私有仓库> 后可跨机同步）\n")
	}
	if out, err := harnessGit(home, "log", "--oneline", "-3"); err == nil {
		fmt.Printf("近期提交:\n%s", out)
	}
	if out, err := harnessGit(home, "status", "--porcelain"); err == nil && strings.TrimSpace(out) != "" {
		n := len(strings.Split(strings.TrimSpace(out), "\n"))
		fmt.Printf("未提交变更: %d 项（forge harness commit 或下一任务边界批量提交）\n", n)
	}
	return nil
}

var harnessCmd = &cobra.Command{
	Use:   "harness",
	Short: "研发控制面仓库（multi-task-concurrency T6：git 化的用户级台账）",
}

var harnessInitCmd = &cobra.Command{
	Use:   "init [--from-existing] [--remote <url>] [--yes]",
	Short: "把用户级 home 变成私有 harness repo（HITL：需终端确认，agent 不得代批）",
	RunE:  runHarnessInit,
}

var harnessStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "harness repo 状态：未建立/本地/已连远端 + 近期提交",
	RunE:  runHarnessStatus,
}

var harnessCommitCmd = &cobra.Command{
	Use:   "commit [reason]",
	Short: "批量提交受管过程状态（任务边界自动触发，此为手动入口）",
	RunE: func(cmd *cobra.Command, args []string) error {
		reason := "手动提交"
		if len(args) > 0 {
			reason = args[0]
		}
		if !HarnessInitialized() {
			return fmt.Errorf("harness repo 未建立——先 forge harness init")
		}
		HarnessCommitBestEffort(reason)
		fmt.Println("已提交（无变更时为空操作）")
		return nil
	},
}

func init() {
	harnessCmd.AddCommand(harnessInitCmd)
	harnessCmd.AddCommand(harnessStatusCmd)
	harnessCmd.AddCommand(harnessCommitCmd)
	harnessPushCmd := &cobra.Command{
		Use:   "push [--yes]",
		Short: "推送 harness repo 到私有远端（首推需人工确认数据出境清单）",
		RunE:  runHarnessPush,
	}
	harnessPushCmd.Flags().Bool("yes", false, "跳过首推确认（仅脚本化 CI；agent 纪律禁止自行使用）")
	harnessCmd.AddCommand(harnessPushCmd)
	harnessCmd.AddCommand(&cobra.Command{
		Use:   "pull",
		Short: "从远端拉取 harness repo（冲突上浮人工解决，不自动裁决）",
		RunE:  runHarnessPull,
	})
	rootCmd.AddCommand(harnessCmd)

	harnessInitCmd.Flags().Bool("from-existing", false, "存量 DataDir 做一次基线提交（史前史入库，不重写数据）")
	harnessInitCmd.Flags().String("remote", "", "私有远端 URL（仅记录；首次 push 需人工另行确认）")
	harnessInitCmd.Flags().Bool("yes", false, "跳过交互确认（仅脚本化 CI；agent 纪律禁止自行使用）")
}

// ── T7 引导层（multi-task-concurrency §13）：onboarding 状态机 + 触发点 + 防 nag ──

// harnessState 状态机：uninitialized → offered（含提示计数与 cooldown）→
// initialized(local) → linked(remote)。offered 的 mtime 即 cooldown 基准，同日第二
// 次触点静默；超过 maxHarnessOffers 次后停止提示（尊重不感兴趣的用户）。
const (
	harnessStateUninitialized = "uninitialized"
	harnessStateOffered       = "offered"
	harnessStateInitialized   = "initialized"
	harnessStateLinked        = "linked"
)

const (
	harnessOfferCooldown = 24 * time.Hour
	maxHarnessOffers     = 3
)

func harnessStatePath(home string) string {
	return filepath.Join(home, "harness-state")
}

// readHarnessState derives the onboarding state: the state file when present, else
// linked/initialized if the repo already exists (pre-T7 machines that git-init'd by hand),
// else uninitialized.
//
// readHarnessState 推导 onboarding 状态：优先状态文件；repo 已存在时推导
// initialized/linked（手工 git init 过的存量机器）；否则 uninitialized。
func readHarnessState(home string) string {
	if data, err := os.ReadFile(harnessStatePath(home)); err == nil {
		s := strings.TrimSpace(string(data))
		switch {
		case s == harnessStateInitialized, s == harnessStateLinked:
			return s
		case strings.HasPrefix(s, harnessStateOffered): // "offered N"（带提示计数）
			return harnessStateOffered
		}
	}
	if HarnessInitialized() {
		if _, err := harnessGit(home, "remote", "get-url", "origin"); err == nil {
			return harnessStateLinked
		}
		return harnessStateInitialized
	}
	return harnessStateUninitialized
}

// MaybeOfferHarness is the advisory trigger point (multi-task-concurrency §13): called
// from natural adoption moments (forge init / forge status). Never blocking, cooldown'd
// (same-day second touch silent), offer-count-capped. Old-version upgraders and new users
// both land here; the HITL confirmation itself lives in harness init (T6's TTY gate).
//
// MaybeOfferHarness 是 advisory 触发点（multi-task-concurrency §13）：挂在天然的引导
// 时机（forge init / forge status）。绝不阻断、有 cooldown（同日第二次触点静默）、有
// 提示次数上限。老版本升级用户与新用户都会到达这里；HITL 确认本体在 harness init
// （T6 的 TTY 门）。
func MaybeOfferHarness(trigger string) {
	home, err := harnessHome()
	if err != nil {
		return
	}
	switch readHarnessState(home) {
	case harnessStateInitialized, harnessStateLinked:
		return // 已建立，无事可引导
	}
	statePath := harnessStatePath(home)
	info, statErr := os.Stat(statePath)
	if statErr == nil && time.Since(info.ModTime()) < harnessOfferCooldown {
		return // cooldown 窗口内：静默
	}
	offers := 0
	if data, err := os.ReadFile(statePath); err == nil {
		fmt.Sscanf(strings.TrimSpace(string(data)), "offered %d", &offers)
	}
	offers++
	if offers > maxHarnessOffers {
		return // 上限后永久静默（尊重不感兴趣的用户）
	}
	_ = os.MkdirAll(home, 0o755)
	_ = os.WriteFile(statePath, []byte(fmt.Sprintf("offered %d", offers)), 0o644)
	fmt.Fprintf(os.Stderr, "[forge] 建议：forge harness init 建立私有研发台账仓库（过程状态获得 git 史；触发点 %s；agent 不得代批，需人在终端确认）\n", trigger)
}

// MarkHarnessInitialized records the state-machine transition on a successful init
// （initialized，配 --remote 时 linked）。
func MarkHarnessInitialized(linked bool) {
	home, err := harnessHome()
	if err != nil {
		return
	}
	s := harnessStateInitialized
	if linked {
		s = harnessStateLinked
	}
	_ = os.WriteFile(harnessStatePath(home), []byte(s), 0o644)
}

// harnessStateLabel renders the status line label for the onboarding state.
//
// harnessStateLabel 渲染 onboarding 状态行的展示标签。
func harnessStateLabel(state string) string {
	switch state {
	case harnessStateLinked:
		return "已连远端（harness repo）"
	case harnessStateInitialized:
		return "本地（harness repo）"
	case harnessStateOffered:
		return "未建立（已提示过）"
	default:
		return "未建立（forge harness init 引导建立）"
	}
}

// ── T9 传输换代（multi-task-concurrency §11.3/§13）：bundle → git remote ──

// runHarnessPush is the outbound half of the transport (multi-task-concurrency §13 外发
// 动作独立确认)：the FIRST push to a new remote is a second HITL with the data-export
// manifest (什么会同步、什么永不外发) — separate from init's confirmation; later pushes
// are ordinary. Non-TTY first push is REFUSED (agent 不得代批外发). Trust anchors are
// structurally absent (gitignored since init) — the manifest says so truthfully.
//
// runHarnessPush 是传输的出站半边（multi-task-concurrency §13 外发动作独立确认）：
// 到新远端的【首次】push 是第二次 HITL，附数据出境清单（什么会同步、什么永不外
// 发）——与 init 的确认相互独立；后续 push 是常规操作。非 TTY 的首推被拒绝（agent
// 不得代批外发）。信任锚自 init 起就被 gitignore，结构上不存在——出境清单如此陈述
// 是真话。
func runHarnessPush(cmd *cobra.Command, args []string) error {
	home, err := harnessHome()
	if err != nil {
		return err
	}
	if !HarnessInitialized() {
		return fmt.Errorf("harness repo 未建立——先 forge harness init")
	}
	if out, err := harnessGit(home, "remote", "get-url", "origin"); err != nil {
		return fmt.Errorf("未配置远端——forge harness init --remote <私有仓库> 或 git -C %s remote add origin <url>", home)
	} else {
		fmt.Printf("远端: %s", out)
	}
	yes, _ := cmd.Flags().GetBool("yes")

	// 首推判定：无 upstream 跟踪分支。
	firstPush := true
	if out, err := harnessGit(home, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err == nil && strings.TrimSpace(out) != "" {
		firstPush = false
	}
	if firstPush {
		if !stdinIsTTY() && !yes {
			return fmt.Errorf("首次 push 属外发动作，需人在终端确认数据出境清单（multi-task-concurrency §13；agent 不得代批）")
		}
		if stdinIsTTY() && !yes {
			fmt.Println("数据出境清单（首次 push）：")
			fmt.Println("  将同步（git tracked）：projects/<key>/tasks、specs、checklog、archive——过程状态与证据")
			fmt.Println("  永不外发（gitignored）：stamps/hazards（信任锚）、workspaces/attribution（机器本地）、会话簿记/哨兵")
			fmt.Println("  远端必须是【私有】仓库；凭据走你自己的 git credential helper，forge 不持有凭据")
			fmt.Printf("输入 yes 确认首推: ")
			var answer string
			if _, err := fmt.Scanln(&answer); err != nil || !strings.EqualFold(answer, "yes") {
				return fmt.Errorf("未确认，放弃 push")
			}
		}
	}
	HarnessCommitBestEffort("pre-push 批量提交")
	if out, err := harnessGit(home, "push", "-u", "origin", "HEAD"); err != nil {
		return fmt.Errorf("push 失败: %v\n%s", err, out)
	}
	fmt.Println("已推送")
	return nil
}

// runHarnessPull is the inbound half: plain `git pull --no-edit` in the harness repo.
// Conflicts surface as errors with manual-resolution guidance — never auto-resolved
//（过程状态是 append 为主，冲突面小；机器裁决优先人可见）。
//
// runHarnessPull 是入站半边：harness repo 里裸的 git pull --no-edit。冲突以错误上浮
// 并给手工解决指引——绝不自动裁决（过程状态 append 为主冲突面小；机器裁决不如人
// 可见）。
func runHarnessPull(cmd *cobra.Command, args []string) error {
	home, err := harnessHome()
	if err != nil {
		return err
	}
	if !HarnessInitialized() {
		return fmt.Errorf("harness repo 未建立——先 forge harness init")
	}
	if out, err := harnessGit(home, "pull", "--no-edit"); err != nil {
		return fmt.Errorf("pull 失败（冲突请到 %s 手工解决后 commit；机器不自动裁决）: %v\n%s", home, err, out)
	}
	fmt.Println("已拉取")
	return nil
}
