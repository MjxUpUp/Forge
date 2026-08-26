// Package review implements the persistent marker and gate decision for code-review-passed,
// supporting two trigger paths:
//   - within task flow: ReviewPassed field stored in DataDir/tasks/<ref>.json (managed by taskpipeline)
//   - non-task flow: diff hash stamp stored in DataDir/stamps/<branch>.stamp (managed by this package)
//
// Both serve the same goal: turning code-review-gate from manual human shouts into automatic
// gate/hook enforcement. This package manages the non-task stamp and exports SourceChangesSince
// for taskpipeline to compute the post-review code-change snapshot in task mode (task-complete gate
// enforces re-review based on it). No circular dependency: review only depends on taskcontext+util,
// never imports taskpipeline; taskpipeline one-way imports review.
//
// Package review 实现「代码审查通过」的持久化标记与门禁决策，支撑两条触发路径：
//   - task 流程内：ReviewPassed 字段存 DataDir/tasks/<ref>.json（taskpipeline 管）
//   - 非 task 流程：diff hash stamp 存 DataDir/stamps/<branch>.stamp（本包管）
//
// 两者服务于同一目标——让 code-review-gate 从「靠人手动喊」变成「门禁/hook 自动挡」。
// 本包管非 task 模式的 stamp，并导出 SourceChangesSince 供 taskpipeline 在 task 模式算
// 「审查后代码是否变更」的快照（task-complete 门禁据此强制复审）。循环依赖不存在：review 只
// 依赖 taskcontext+util，不 import taskpipeline；taskpipeline 单向 import review。
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/util"
)

// MaxReviewRounds is the fallback cap for the Stop hook repeatedly blocking the same diff.
// Beyond this, it advisory-passes to prevent a Stop infinite loop caused by agents never calling
// forge review pass (task-verify historical lesson: Stop blocks trigger retry-loop; see
// TaskVerifyHook comments in hooks/embed.go).
//
// MaxReviewRounds 是 Stop hook 反复 block 同一 diff 的兜底上限。超过后 advisory
// 放行——防 agent 不调 forge review pass 导致的 Stop 死循环（task-verify 历史教训：
// Stop block 会触发 retry-loop，见 hooks/embed.go TaskVerifyHook 注释）。
const MaxReviewRounds = 3

// Decision is the review decision produced by Evaluate for the non-task current diff.
//
// Decision 是 Evaluate 对非 task 模式当前 diff 的审查决策。
type Decision int

const (
	DecisionPass         Decision = iota // 无需审：已审过这版 diff，或无变更/非 git
	DecisionNeedReview                   // 需审：有未审变更，Stop hook 应 block
	DecisionPassAdvisory                 // 兜底放行：撞 MaxReviewRounds，advisory 提醒
)

// Stamp records the review state of the current diff for a branch, stored at
// DataDir/stamps/<branch>.stamp.
//
// Stamp 记录某分支当前 diff 的审查状态，存 DataDir/stamps/<branch>.stamp。
type Stamp struct {
	DiffHash   string    `json:"diff_hash"`             // 审查范围（git diff）的 sha256；空=无变更
	Reviewed   bool      `json:"reviewed"`              // 该 diff_hash 是否已通过 code-review-gate
	BlockCount int       `json:"block_count"`           // 该 diff_hash 被 Stop hook block 的次数
	ReviewedAt time.Time `json:"reviewed_at,omitempty"` // 最近一次 forge review pass 时间
	Branch     string    `json:"branch,omitempty"`
	// Note is the optional reviewer conclusion from `forge review pass --note`
	// (non-task mode audit trail counterpart of ReviewRound.Note).
	//
	// Note 是 `forge review pass --note` 的可选审查结论文本（非 task 模式的审计留痕，
	// 对应 task 模式的 ReviewRound.Note）。
	Note string `json:"note,omitempty"`
}

// Evaluate is the atomic decision entry for the Stop hook (non-task mode): compute the current
// diff hash, compare against the stamp, decide pass/need-review/fallback, and on need-review
// increment block_count and persist it.
// Returns (decision, human-readable reason).
//
// Evaluate 是 Stop hook（非 task 模式）的原子决策入口：算当前 diff hash →
// 对比 stamp → 决定放行/需审/兜底，并在需审时累加 block_count 持久化。
// 返回 (决策, 人读原因)。
func Evaluate(root string) (Decision, string, error) {
	hash, hasChanges, err := computeDiffHash(root)
	if err != nil {
		return DecisionPass, "", err
	}
	if !hasChanges {
		return DecisionPass, "无未提交变更，无需审查", nil
	}

	stamp := loadStamp(root)

	// Same diff already reviewed -> pass
	//
	// 同一 diff 已审过 → 放行
	if stamp.DiffHash == hash && stamp.Reviewed {
		return DecisionPass, "当前 diff 已通过 code-review-gate", nil
	}

	// Same diff already reviewed on ANOTHER branch -> pass. A review pass is bound to the code
	// snapshot (diff hash), not to the branch/mode scope the stamp lives under, so a legitimate
	// zero-content transition that only changes the scope key — fast-forward merging a reviewed
	// branch into master then checking out master (identical HEAD + worktree → identical hash) —
	// must not re-block code that already passed review. Safe: a hash match means byte-identical
	// source content; differing content differs in hash and still needs review below.
	//
	// 同一 diff 已在其他分支审过 → 放行。审查通过绑的是代码快照（diff hash），不是戳所在的
	// branch/mode scope，故只改 scope key、内容不变的合法迁移——把已审分支 ff-merge 进 master
	// 再 checkout master（同一 HEAD + 工作区 → 同一 hash）——不该重新 block 已审过的代码。
	// 安全：hash 命中即源码字节一致；内容不同则 hash 不同，下面照常需审。
	if other, ok := knownReviewed(root, hash); ok {
		branch := other.Branch
		if branch == "" {
			branch = "其他分支"
		}
		return DecisionPass, fmt.Sprintf("当前 diff 已在分支 %s 通过 code-review-gate（内容一致，跨分支放行）", branch), nil
	}

	// Hit max rounds fallback pass (prevents Stop infinite loop)
	//
	// 撞 max rounds 兜底放行（防 Stop 死循环）
	if stamp.DiffHash == hash && stamp.BlockCount >= MaxReviewRounds {
		return DecisionPassAdvisory,
			fmt.Sprintf("已达 max review rounds (%d)，advisory 放行——请人工确认未审变更", MaxReviewRounds), nil
	}

	// Need review: increment block_count and persist (new diff resets counter to 1)
	//
	// 需审：累加 block_count 并持久化（新 diff 重置计数从 1 起）
	next := *stamp
	if stamp.DiffHash != hash {
		next = Stamp{DiffHash: hash, Reviewed: false, BlockCount: 1, Branch: currentBranch(root)}
	} else {
		next.BlockCount = stamp.BlockCount + 1
	}
	if err := saveStamp(root, &next); err != nil {
		return DecisionPass, "", err
	}
	return DecisionNeedReview,
		fmt.Sprintf("检测到未审查的代码变更（block %d/%d）", next.BlockCount, MaxReviewRounds), nil
}

// MarkPassed marks the current diff as having passed review (called by forge review pass).
// Computes the current hash, writes a reviewed stamp, resets block_count.
//
// MarkPassed 标记当前 diff 已通过审查（forge review pass 调用）。
// 算当前 hash 写 reviewed stamp，重置 block_count。
func MarkPassed(root string) error {
	return MarkPassedWithNote(root, "")
}

// MarkPassedWithNote is MarkPassed plus the optional reviewer conclusion text
// (`forge review pass --note`) persisted on the stamp.
//
// MarkPassedWithNote 在 MarkPassed 之上把可选审查结论文本
// （`forge review pass --note`）持久化进 stamp。
func MarkPassedWithNote(root, note string) error {
	hash, _, err := computeDiffHash(root)
	if err != nil {
		return err
	}
	stamp := &Stamp{
		DiffHash:   hash,
		Reviewed:   true,
		BlockCount: 0,
		ReviewedAt: time.Now(),
		Branch:     currentBranch(root),
		Note:       note,
	}
	return saveStamp(root, stamp)
}

// loadStamp reads the review stamp of the current branch; missing/corrupt returns an empty
// Stamp. By design there is no error return: the stamp is a hint, and every failure mode
// (absent, unreadable, corrupt) degrades to "empty stamp -> re-review", which is the safe
// direction. A non-IsNotExist read failure (permission/IO) is logged so it stays observable
// instead of masquerading as "no stamp yet".
//
// loadStamp 读取当前分支的审查 stamp；不存在/损坏返回空 Stamp。设计上没有 error 返回：
// stamp 只是提示，所有失败模式（不存在/不可读/损坏）都降级为「空 stamp → 重审」，
// 这是安全方向。非 IsNotExist 的读失败（权限/IO）记 log 保持可观测，不再伪装成「还没有 stamp」。
func loadStamp(root string) *Stamp {
	data, err := os.ReadFile(stampPath(root))
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[review] stamp read failed (%v) — treating as empty stamp, re-review required", err)
		}
		return &Stamp{}
	}
	var s Stamp
	if err := json.Unmarshal(data, &s); err != nil {
		return &Stamp{} // 损坏视为空，下次重审
	}
	return &s
}

// knownReviewed reports whether the given diff hash has been marked reviewed on ANY branch stamp
// (not only the current branch). This makes a review pass portable across scope-only transitions
// (see Evaluate): the same content reviewed elsewhere still counts. Returns the matching stamp so the
// caller can name the originating branch. Failures (no stamps dir, unreadable/corrupt files) degrade
// to "not found" — safe direction, re-review.
//
// knownReviewed 报告给定 diff hash 是否在【任意】分支 stamp 上被标过已审（不限当前分支）。
// 这让审查通过跨「只改 scope」的迁移可移植（见 Evaluate）：别处审过的同一内容照样认。返回
// 命中 stamp 供调用方点名来源分支。失败（无 stamps 目录/不可读/损坏）降级为「未找到」——
// 安全方向，重审。
func knownReviewed(root, hash string) (*Stamp, bool) {
	dir := filepath.Join(forgedata.DataDirFor(root), "stamps")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".stamp") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var s Stamp
		if json.Unmarshal(data, &s) == nil && s.DiffHash == hash && s.Reviewed {
			return &s, true
		}
	}
	return nil, false
}

// CurrentState returns a human-readable view of the current review state (for forge review status).
//
// CurrentState 返回人读的当前审查状态（forge review status 用）。
func CurrentState(root string) (string, error) {
	hash, hasChanges, err := computeDiffHash(root)
	if err != nil {
		return "", err
	}
	stamp := loadStamp(root)
	branch := currentBranch(root)

	var b strings.Builder
	fmt.Fprintf(&b, "Branch:        %s\n", branch)
	fmt.Fprintf(&b, "Has src chg:   %v\n", hasChanges)
	if hasChanges {
		fmt.Fprintf(&b, "Current diff:  %s\n", hash[:12])
	} else {
		fmt.Fprintf(&b, "Current diff:  (none)\n")
	}
	fmt.Fprintf(&b, "Stamped diff:  %s\n", diffShort(stamp.DiffHash))
	fmt.Fprintf(&b, "Reviewed:      %v\n", stamp.Reviewed)
	if stamp.Reviewed && !stamp.ReviewedAt.IsZero() {
		fmt.Fprintf(&b, "Reviewed at:   %s\n", stamp.ReviewedAt.Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&b, "Block count:   %d/%d\n", stamp.BlockCount, MaxReviewRounds)

	// Cross-branch portability for display: if the current branch has no matching reviewed stamp,
	// but another branch reviewed this exact hash, surface that (consistent with Evaluate's pass).
	//
	// 跨分支可移植的展示：当前分支无匹配已审戳，但别处审过同一 hash 时点出来（与 Evaluate 放行一致）。
	crossBranch := ""
	if hasChanges && !(stamp.DiffHash == hash && stamp.Reviewed) {
		if other, ok := knownReviewed(root, hash); ok {
			crossBranch = other.Branch
			if crossBranch == "" {
				crossBranch = "其他分支" // mirror Evaluate's fallback so display never drifts from the decision
			}
		}
	}

	switch {
	case !hasChanges:
		b.WriteString("\n→ 无未提交变更，无需审查\n")
	case crossBranch != "":
		fmt.Fprintf(&b, "\n→ 当前 diff 已在分支 %s 通过审查（内容一致，跨分支放行）\n", crossBranch)
	case stamp.DiffHash == hash && stamp.Reviewed:
		b.WriteString("\n→ 当前 diff 已通过审查\n")
	default:
		b.WriteString("\n→ 当前 diff 未审查：加载 code-review-gate 派只读子 agent 审查，通过后 forge review pass\n")
	}
	return b.String(), nil
}

// --- internal implementation ---
//
// --- 内部实现 ---

// computeDiffHash/SourceChangesSince compute the source-code change fingerprint (sha256) for review.
//
// computeDiffHash computes the [source-code] change fingerprint (sha256) of the current worktree
// relative to HEAD, used to decide whether this version of code has been reviewed.
// False-trigger protection (2026-06-27): the review scope counts ONLY source files:
//   - excludes .forge/ (otherwise writing the stamp changes the diff -> hash changes -> infinite loop)
//   - excludes non-source extensions (.md/.txt/.yml/.json/.toml etc., docs and configs); editing a
//     README should not force a code review
//   - excludes generated artifacts (paths containing .gen./_generated/.pb./vendor/ etc.); auto-
//     generated code refreshing the hash is meaningless
//
// Pure doc/config/generated changes -> changedSourceFiles empty -> ("", false, nil) -> no review needed.
// Non-git repos likewise -> no review needed.
//
// SourceChangesSince computes the [source-code] change fingerprint (sha256) from baseCommit to the
// current worktree. baseCommit=="" degrades to HEAD (i.e. worktree-relative-to-HEAD, the non-task
// stamp semantics).
//
// False-trigger protection (inherited from computeDiffHash, 2026-06-27): the same source-only
// scope rule applies here (excludes .forge/, non-source extensions, and generated paths, as above).
//
// Key difference from the old computeDiffHash: the fingerprint uses [current file contents] rather
// than git diff output, so unchanged content means unchanged fingerprint across the untracked-to-
// tracked transition around a commit. If untracked files were recorded by name only at review time,
// they would become tracked after commit (diff contains full content) and the two dimensions would
// differ, producing false positives; reading current worktree contents unifies both paths, so
// commit only changes git state, not content -> same hash. Tracked changes use
// git diff --name-only <base> single-tree form (including base..HEAD committed + worktree
// uncommitted), so commit-then-review flows (clean worktree at review time) can compare correctly.
//
// baseCommit unreachable (amend/rebase rewrote history and the git object vanished) -> returns err,
// caller fail-opens. Pure doc/config/generated changes -> ("", false, nil) -> no review needed.
// Non-git repos -> no review needed.
//
// computeDiffHash 算当前工作区相对 HEAD 的【源码】变更指纹（sha256），用于判断「这版代码审过没」。
// 误触发防护（2026-06-27）：审查范围**只统计源码文件**——
//   - 排除 .forge/（否则写 stamp 改 diff → hash 变 → 死循环）
//   - 排除非源码扩展（.md/.txt/.yml/.json/.toml 等文档与配置）——改 README 不该被逼审代码
//   - 排除生成物（路径含 .gen./_generated/.pb./vendor/ 等）——自动生成代码刷 hash 无意义
//
// 纯文档/配置/生成物变更 → changedSourceFiles 空 → ("", false, nil) → 无需审。
// 非 git 仓库同样 → 无需审。
// SourceChangesSince 算「自 baseCommit 起到当前工作区」的【源码】变化指纹（sha256）。
// baseCommit=="" → 退化成 HEAD（= 工作区相对 HEAD，非 task 模式 stamp 语义）。
//
// 误触发防护（沿用原 computeDiffHash，2026-06-27）：审查范围**只统计源码文件**——
//   - 排除 .forge/（否则写 stamp 改 diff → hash 变 → 死循环）
//   - 排除非源码扩展（.md/.txt/.yml/.json/.toml 等文档与配置）——改 README 不该被逼审代码
//   - 排除生成物（路径含 .gen./_generated/.pb./vendor/ 等）——自动生成代码刷 hash 无意义
//
// 与旧 computeDiffHash 的关键差异：用【文件当前内容】做指纹而非 git diff 输出——保证 commit 前后
// （untracked→tracked 切换）内容不变则指纹不变。review 时 untracked 文件若只记文件名，commit 后变 tracked
// （diff 含完整内容），两者维度不同会假阳性；统一读工作区当前内容后殊途同归，commit 只改 git 状态不改内容 → 同一 hash。
// tracked 变更用 `git diff --name-only <base>` 单树形式列文件（含 base..HEAD 已提交 + 工作区未提交），
// 让 commit-then-review 流（review 时工作区干净）能正确比对。
//
// baseCommit 不可达（amend/rebase 改写历史致 git 对象消失）→ 返回 err，调用方 fail-open。
// 纯文档/配置/生成物变更 → ("", false, nil) → 无需审。非 git 仓库 → 无需审。
func SourceChangesSince(root, baseCommit string) (hash string, hasChanges bool, err error) {
	if !isGitRepo(root) {
		return "", false, nil
	}
	base := baseCommit
	if base == "" {
		base = "HEAD"
	}
	// Base reachability: after amend/rebase, old commits may be unreachable and git diff/show
	// would fatal. Verify up front and return err so the caller fail-opens (instead of treating
	// git stderr as no-changes and wrongly passing).
	//
	// 基线可达性：amend/rebase 后旧 commit 可能不可达，git diff/show 会 fatal。提前 verify 返回 err，
	// 让调用方 fail-open（而非把 git stderr 当「无变更」误判放行）。
	if out, e := gitOut(root, "rev-parse", "--verify", base+"^{commit}"); e != nil || strings.TrimSpace(out) == "" {
		return "", false, fmt.Errorf("base commit %q not reachable: %w", base, e)
	}
	tracked, untracked := changedSourceFilesSince(root, base)
	files := append(tracked, untracked...)
	if len(files) == 0 {
		return "", false, nil
	}
	slices.Sort(files)
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s\n%s\n", f, fileContentForHash(root, base, f))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), true, nil
}

// changedSourceFilesSince returns the [source-code] files changed since baseCommit, split into
// tracked (modified/deleted, including base..HEAD committed + worktree uncommitted) and untracked
// (newly added). base=="" -> HEAD. Excludes .forge/, non-source extensions, and generated paths.
//
// changedSourceFilesSince 返回自 baseCommit 起变更的【源码】文件，分 tracked（修改/删除，含
// base..HEAD 已提交 + 工作区未提交）和 untracked（新增）。base=="" → HEAD。已排除 .forge/、
// 非源码扩展、生成物路径。
func changedSourceFilesSince(root, baseCommit string) (tracked, untracked []string) {
	base := baseCommit
	if base == "" {
		base = "HEAD"
	}
	if out, e := gitOut(root, "diff", "--name-only", base, "--", ".", ":(exclude).forge"); e == nil {
		for _, f := range nonEmptyLines(out) {
			if isSourceCode(f) {
				tracked = append(tracked, f)
			}
		}
	}
	if out, e := gitOut(root, "ls-files", "--others", "--exclude-standard", "--", ".", ":(exclude).forge"); e == nil {
		for _, f := range nonEmptyLines(out) {
			if isSourceCode(f) {
				untracked = append(untracked, f)
			}
		}
	}
	return tracked, untracked
}

// fileContentForHash returns the fingerprint content of a changed file: prefers current worktree
// contents (modified/added); if absent in worktree (deleted/renamed source), uses the base version
// as a deletion marker. Deletion is also a change and must be included in the fingerprint,
// otherwise deleting a source file would escape re-review. Shared by task and non-task paths.
//
// fileContentForHash 取变更文件的指纹内容：优先工作区当前内容（修改/新增）；工作区无（删除/重命名源）
// 用 base 版本内容作删除标记——删除也是变更，必须纳入指纹，否则删源码文件逃过复审。task/非 task 共用。
func fileContentForHash(root, base, path string) string {
	if data, err := os.ReadFile(filepath.Join(root, path)); err == nil {
		return string(data)
	}
	if out, e := gitOut(root, "show", base+":"+path); e == nil {
		return "[DELETED]\n" + out
	}
	return ""
}

// computeDiffHash computes the source-code change fingerprint of the worktree relative to HEAD.
// Degrades to SourceChangesSince(root, ""), the single source of truth: non-task stamps and
// task-mode snapshots share the same file filtering and hashing logic, avoiding drift.
//
// computeDiffHash 算当前工作区相对 HEAD 的源码变更指纹。退化为 SourceChangesSince(root, "")，
// 单一真相源——非 task 模式 stamp 与 task 模式快照共用同一套文件过滤与哈希逻辑，避免漂移。
func computeDiffHash(root string) (hash string, hasChanges bool, err error) {
	return SourceChangesSince(root, "")
}

// srcExts is the allowlist of source-code extensions under review. Docs (.md/.txt), configs
// (.yml/.json/.toml/.ini), data (.csv/.log), static assets (.png/.css) etc. are excluded:
// these changes do not trigger code review (false-trigger protection).
//
// srcExts 是受审查的源码扩展名白名单。文档(.md/.txt)/配置(.yml/.json/.toml/.ini)/
// 数据(.csv/.log)/静态资源(.png/.css)等不在内——这些变更不触发代码审查（误触发防护）。
var srcExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".py": true, ".rs": true, ".java": true, ".kt": true, ".kts": true, ".scala": true,
	".rb": true, ".php": true, ".c": true, ".h": true, ".hpp": true, ".cc": true, ".cxx": true,
	".cs": true, ".swift": true, ".m": true, ".mm": true, ".sh": true, ".bash": true, ".zsh": true,
	".ps1": true, ".sql": true, ".vue": true, ".svelte": true, ".dart": true, ".lua": true,
	".pl": true, ".r": true, ".jl": true, ".ex": true, ".exs": true, ".clj": true, ".cljs": true,
	".hs": true, ".ml": true, ".fs": true, ".nim": true, ".zig": true, ".v": true,
}

// genMarks identifies generated/third-party path fragments: even if the extension is a source
// extension, these are excluded (auto-generated code refreshing the hash is meaningless).
//
// genMarks 标识生成物/三方目录路径片段——即使扩展名是源码也排除（自动生成刷 hash 无意义）。
var genMarks = []string{".gen.", "_generated", ".pb.", "vendor/", "node_modules/", "third_party/"}

func isSourceCode(path string) bool {
	for _, mark := range genMarks {
		if strings.Contains(path, mark) {
			return false
		}
	}
	return srcExts[strings.ToLower(filepath.Ext(path))]
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func isGitRepo(root string) bool {
	out, err := gitOut(root, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

func currentBranch(root string) string {
	out, err := gitOut(root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// CurrentBranch exports currentBranch for callers that need the branch context of a
// non-task stamp (e.g. checklog audit detail in cmd `forge review pass`).
//
// CurrentBranch 导出 currentBranch，供需要非 task 戳分支上下文的调用方使用
//（如 `forge review pass` 命令的 checklog 审计 detail）。
func CurrentBranch(root string) string {
	return currentBranch(root)
}

func gitOut(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func stampPath(root string) string {
	branch := currentBranch(root)
	if branch == "" || branch == "HEAD" {
		branch = "default"
	}
	return filepath.Join(forgedata.DataDirFor(root), "stamps", taskcontext.SanitizeRef(branch)+".stamp")
}

func saveStamp(root string, s *Stamp) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(stampPath(root), data, 0o644)
}

func diffShort(h string) string {
	if len(h) >= 12 {
		return h[:12]
	}
	if h == "" {
		return "(none)"
	}
	return h
}
