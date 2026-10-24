// Package review implements the persistent marker and gate decision for
// code-review-passed, supporting two trigger paths: - within task flow:
// ReviewPassed field stored in DataDir/tasks/<ref>.json (managed by
// taskpipeline) - non-task flow: diff hash stamp stored in
// DataDir/stamps/<branch>.stamp (managed by this package).
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
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/attribution"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/util"
)

// MaxReviewRounds 是 Stop hook 反复 block 同一 diff 的兜底上限。超过后 advisory
// 放行——防 agent 不调 forge review pass 导致的 Stop 死循环（task-verify 历史教训：
// Stop block 会触发 retry-loop，见 hooks/embed.go TaskVerifyHook 注释）。
const MaxReviewRounds = 3

// Decision is the review decision produced by Evaluate for the non-task current
// diff.
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

// Evaluate is the atomic decision entry for the Stop hook (non-task mode):
// compute the current diff hash, compare against the stamp, decide
// pass/need-review/fallback, and on need-review increment block_count and
// persist it.
//
// Evaluate 是 Stop hook（非 task 模式）的原子决策入口：算当前 diff hash →
// 对比 stamp → 决定放行/需审/兜底，并在需审时累加 block_count 持久化。
// 返回 (决策, 人读原因)。
func Evaluate(root string) (Decision, string, error) {
	return EvaluateFor(root, "")
}

// EvaluateFor is the session-scoped Stop decision: the fingerprint only carries
// the source changes this session can be attributed, so a Stop in a shared
// checkout no longer blocks on (nor asks to review) another live session's work.
//
// The exclusion set is the attribution layer's provably-foreign paths: changed
// paths whose last writer is another session inside the liveness window AND that
// this session has never touched itself (View.Foreign). The second half matters —
// last-writer-wins says who wrote last, not whose changes the file still holds.
// Everything else stays in the fingerprint — including ORPHANS the ledger cannot
// explain, because "I can't attribute it" must keep failing safe towards review
// (multi-task-concurrency §8: expose, never silently drop).
//
// sid == "" keeps the historical whole-tree behaviour (no subject, no exclusion),
// so every existing caller and test is unchanged; attributions disabled
// (FORGE_ATTRIBUTION=0) or a broken ledger degrades the same way.
//
// EvaluateFor 是会话作用域的 Stop 判定：指纹只装「能归因到本会话」的源码变更——
// 共享检出里的 Stop 不再因为别的活跃会话的改动而阻断，也不再要求本会话去审查它。
// 排除集是归属层可证明的外来路径（last-writer 是他人 + 活跃窗口内）；其余一切留在
// 指纹里，包括台账解释不了的无主路径（解释不了 ⇒ 继续按需审处理，fail-safe）。
// sid 为空退回整树历史行为；归属层关闭/台账损坏同样退化为整树。
func EvaluateFor(root, sid string) (Decision, string, error) {
	// 归属参与判定的前提是「有主体 + 归属层开着」。台账读取要解析本机全部事件，
	// 所以只在确实可能有外来路径时才读（clean 工作区由下面的空指纹早退兜住）。
	var foreign []string
	if sid != "" && attribution.Enabled() {
		foreign = foreignForSession(root, sid)
	}
	exclude := make(map[string]bool, len(foreign))
	for _, p := range foreign {
		exclude[p] = true
	}
	scopeHash, wholeHash, included, err := scopeAndWholeFingerprint(root, "", exclude)
	if err != nil {
		return DecisionPass, "", err
	}
	if scopeHash == "" && wholeHash == "" {
		return DecisionPass, "无未提交变更，无需审查", nil
	}
	if len(included) == 0 {
		return DecisionPassAdvisory, fmt.Sprintf("本会话无可归因的源码变更（另有 %d 个文件属于其他活跃会话，已按归属排除）——不阻断、不消耗审查次数", len(foreign)), nil
	}
	hash := scopeHash

	// 已审判定接受「整树戳」或「作用域戳」：`forge review pass` 作为独立 CLI 进程可能拿不到
	// 会话 id（dsh 只在 hook 子进程注入身份），于是盖的是整树戳；整树审查是作用域审查的超集，
	// 两者都按内容寻址，命中任一都意味着「这些字节审过了」。
	reviewed := func(s *Stamp) bool {
		if !s.Reviewed {
			return false
		}
		return s.DiffHash == hash || (wholeHash != "" && s.DiffHash == wholeHash)
	}

	stamp := loadStamp(root, hash)

	// 同一 diff 已审过 → 放行
	if reviewed(stamp) {
		return DecisionPass, "当前 diff 已通过 code-review-gate", nil
	}

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
	if wholeHash != "" && wholeHash != hash {
		if other, ok := knownReviewed(root, wholeHash); ok {
			branch := other.Branch
			if branch == "" {
				branch = "其他分支"
			}
			return DecisionPass, fmt.Sprintf("当前 diff 已在分支 %s 通过 code-review-gate（整树审查，内容一致）", branch), nil
		}
	}

	// 撞 max rounds 兜底放行（防 Stop 死循环）。整树戳与作用域戳都算同一轮次：
	// 它们描述的是同一批未提交字节，只是范围不同。
	if (stamp.DiffHash == hash || (wholeHash != "" && stamp.DiffHash == wholeHash)) && stamp.BlockCount >= MaxReviewRounds {
		return DecisionPassAdvisory,
			fmt.Sprintf("已达 max review rounds (%d)，advisory 放行——请人工确认未审变更", MaxReviewRounds), nil
	}

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
	reason := fmt.Sprintf("检测到未审查的代码变更（block %d/%d）", next.BlockCount, MaxReviewRounds)
	if len(foreign) > 0 {
		reason += fmt.Sprintf("；范围含本会话 %d 个文件，已排除其他活跃会话的 %d 个", len(included), len(foreign))
	}
	// 归属层被关掉时，判定退化成整树——把这件事说出来，否则用户以为门禁已经按会话
	// 收窄过，实际上它又回到了「任何人的改动都算你的」。信号必须是看得见的。
	if sid != "" && !attribution.Enabled() {
		reason += "；归属层不可用（FORGE_ATTRIBUTION=0），本次按整树判定"
	}
	return DecisionNeedReview, reason, nil
}

// MarkPassedWithNote marks the current diff as having passed review (`forge
// review pass [--note]`) — computes the diff hash, writes the reviewed stamp
// with the optional reviewer conclusion text, and resets block_count.
//
// MarkPassedWithNote 在 MarkPassed 之上把可选审查结论文本
// （`forge review pass --note`）持久化进 stamp。
func MarkPassedWithNote(root, note string) error {
	return MarkPassedFor(root, "", note)
}

// MarkPassedFor marks the *same scope EvaluateFor will judge* as reviewed. It must
// mirror EvaluateFor's fingerprint exactly: a session that reviewed its own change
// and then stamps the whole tree would never satisfy the scoped gate (block →
// pass → block again), so the two entry points share one scope computation.
//
// MarkPassedFor 盖章的范围必须与 EvaluateFor 判定的范围完全一致：若盖章用整树指纹
// 而门禁用会话指纹，复审通过的会话下次 Stop 仍会被拦（盖章-放行死循环）。两者共用
// 同一份作用域计算。
func MarkPassedFor(root, sid, note string) error {
	foreign := foreignForSession(root, sid)
	exclude := make(map[string]bool, len(foreign))
	for _, p := range foreign {
		exclude[p] = true
	}
	hash, _, err := sourceChangesSinceExcluded(root, "", exclude)
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

// loadStamp 读取给定 diff hash 的审查 stamp：先内容寻址 dh-<hash>.stamp（主路径），
// 后旧 <branch>.stamp（迁移前 stamp 的读兼容）。不存在/损坏返回空 Stamp。设计上没有
// error 返回：stamp 只是提示，所有失败模式（不存在/不可读/损坏）都降级为「空 stamp →
// 重审」，这是安全方向。非 IsNotExist 的读失败（权限/IO）记 log 保持可观测，不再伪装
// 成「还没有 stamp」。
func loadStamp(root, hash string) *Stamp {
	for _, path := range []string{stampContentPath(root, hash), stampPath(root)} {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("[review] stamp read failed (%v) — treating as empty stamp, re-review required", err)
			}
			continue
		}
		var s Stamp
		if err := json.Unmarshal(data, &s); err != nil {
			continue // 损坏视为空，试下一路径/重审
		}
		return &s
	}
	return &Stamp{}
}

// knownReviewed 报告给定 diff hash 是否在【任意】分支 stamp 上被标过已审（不限当前分支）。
// 这让审查通过跨「只改 scope」的迁移可移植（见 Evaluate）：别处审过的同一内容照样认。返回
// 命中 stamp 供调用方点名来源分支。失败（无 stamps 目录/不可读/损坏）降级为「未找到」——
// 安全方向，重审。
//
// 扫描策略（feat/checklog-janitor）：扫描前先由 pruneStamps 清扫陈旧 dh-*.stamp（每个新
// diff 的 Stop-block 都会留下一枚；不清扫则目录与这次全量扫都无界增长）。内容寻址 stamp
// 改按【文件名】匹配——dh-<hash>.stamp 的名即 hash（saveStamp 用 s.DiffHash 推导路径），
// 名字不匹配的 dh-*.stamp 直接跳过不读，扫描从 O(N) 次文件读降到至多一次。仅 legacy
// <branch>.stamp（hash 在内容里）仍读内容。名字命中的那一枚 dh- 文件仍要读出来确认
// Reviewed：Evaluate 的 block 路径把 Reviewed=false 的计数戳写在同一内容寻址路径上，
// 若只按文件名命中，一个仅被 block 过的 diff 会冒充已审——假放行。（名/内容错配不可能
// 由 saveStamp 产生；只有手工篡改才会。）
func knownReviewed(root, hash string) (*Stamp, bool) {
	dir := filepath.Join(forgedata.DataDirFor(root), "stamps")
	pruneStamps(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	// 目标 hash 对应的内容寻址文件名（SanitizeRef 对十六进制 hash 是 no-op，但为与
	// stampContentPath/saveStamp 完全对齐仍照做一遍）。
	wantName := "dh-" + taskcontext.SanitizeRef(hash) + ".stamp"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".stamp") {
			continue
		}
		if strings.HasPrefix(e.Name(), "dh-") {
			// 内容寻址：名即 hash。不匹配的文件一律不读；仅读名字命中的那一枚
			// （至多一次）以确认 Reviewed（block 戳同路径——见函数注释）。
			if e.Name() != wantName {
				continue
			}
		}
		// 名字命中的 dh- 文件与所有 legacy <branch>.stamp 走到这里做内容校验
		//（legacy 的名字里没有 hash）。
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

// pruneStamps 尽力清扫超过 FORGE_STAMP_RETENTION_DAYS（默认 30；≤0 禁用）的陈旧内容
// 寻址 stamp（dh-*.stamp）：每个新 diff 的 Stop-block 都持久化一枚 dh-<hash>.stamp，
// diff 一直在换，却没有任何路径删它们——stamps 目录无界增长，knownReviewed 每次
// Evaluate 白付一次全量扫。龄取文件 mtime：util.AtomicWrite（临时文件+rename）在写入
// 时刻设定它，且 retention 窗口未被动过的戳标记的是早已被取代的 diff——它的跨分支
// 可移植保证随之过期（30 天前的工作区状态被要求重审是 retention 的本意取舍，不是
// 回归）。legacy <branch>.stamp 不清扫：只做读兼容、每分支至多一枚——本就有界。
// 失败（glob/stat/remove）静默跳过：清理是查找的副作用，绝不是其结果。
func pruneStamps(dir string) {
	days := util.RetentionDays("FORGE_STAMP_RETENTION_DAYS", 30)
	if days <= 0 {
		return
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	matches, err := filepath.Glob(filepath.Join(dir, "dh-*.stamp"))
	if err != nil {
		return
	}
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Before(cutoff) {
			continue // 并发删除/未过期 → 跳过
		}
		_ = os.Remove(path)
	}
}

// CurrentState returns a human-readable view of the current review state (for
// forge review status).
//
// CurrentState 返回人读的当前审查状态（forge review status 用）。
func CurrentState(root string) (string, error) {
	hash, hasChanges, err := computeDiffHash(root)
	if err != nil {
		return "", err
	}
	stamp := loadStamp(root, hash)
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

// --- 内部实现 ---

// SourceChangesSince computes the source-code change fingerprint (sha256) from baseCommit
// to the current worktree.
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
	return SourceChangesSinceExcluded(root, baseCommit, nil)
}

// SourceChangesSinceExcluded is SourceChangesSince with an attribution-exclusion
// set (multi-task-concurrency §6, T3): paths the caller has proven foreign
// (other incomplete tasks' sessions, via taskpipeline.ForeignAttributedPaths)
// leave the fingerprint.
//
// SourceChangesSinceExcluded 是带归属排除集的 SourceChangesSince
// （multi-task-concurrency §6，T3）：调用方已证明外来（其他未完成任务的会话，经
// taskpipeline.ForeignAttributedPaths）的路径离开指纹——review 的范围是本任务的变更，
// 不是共享整树的噪音。按契约无主路径不可在此排除：解释不了的变更必须被审
// （fail-safe）。nil map = 全树（L3 之前的行为；非 task 模式传 nil）。
func SourceChangesSinceExcluded(root, baseCommit string, exclude map[string]bool) (hash string, hasChanges bool, err error) {
	hash, files, err := sourceChangesSinceExcluded(root, baseCommit, exclude)
	return hash, len(files) > 0, err
}

// sourceChangesSinceExcluded is the list-returning form of
// SourceChangesSinceExcluded: the caller that has to name what it reviewed (and
// what it excluded) gets the surviving paths, not just a hash.
//
// sourceChangesSinceExcluded 是 SourceChangesSinceExcluded 的列表版：需要点名「审了
// 哪些、排除了哪些」的调用方拿到留在指纹里的路径，而不只是一个 hash。无变更时返回
// 空 files（hash 为空）。
func sourceChangesSinceExcluded(root, baseCommit string, exclude map[string]bool) (hash string, files []string, err error) {
	if !isGitRepo(root) {
		return "", nil, nil
	}
	base := baseCommit
	if base == "" {
		base = "HEAD"
	}
	// 基线可达性：amend/rebase 后旧 commit 可能不可达，git diff/show 会 fatal。提前 verify 返回 err，
	// 让调用方 fail-open（而非把 git stderr 当「无变更」误判放行）。
	if out, e := gitOut(root, "rev-parse", "--verify", base+"^{commit}"); e != nil || strings.TrimSpace(out) == "" {
		return "", nil, fmt.Errorf("base commit %q not reachable: %w", base, e)
	}
	tracked, untracked := changedSourceFilesSince(root, base)
	files = append(tracked, untracked...)
	if len(exclude) > 0 {
		kept := files[:0]
		for _, f := range files {
			if !exclude[f] {
				kept = append(kept, f)
			}
		}
		files = kept
	}
	if len(files) == 0 {
		return "", nil, nil
	}
	slices.Sort(files)
	return hashSourceFiles(root, base, files), files, nil
}

// hashSourceFiles is the content-addressed fingerprint of a file set: sorted
// (path, current-content) pairs, sha256. Sorted so the order git reports changes
// in never moves the hash.
//
// hashSourceFiles 是文件集的内容寻址指纹：按（路径 + 当前内容）排序后 sha256，
// 与 git 报告顺序无关。
func hashSourceFiles(root, base string, files []string) string {
	slices.Sort(files)
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s\n%s\n", f, fileContentForHash(root, base, f))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// scopeAndWholeFingerprint computes both fingerprints in one git pass: the
// whole-tree hash (every changed source file) and the scoped hash (after removing
// exclude). The caller needs the pair because `forge review pass` may legitimately
// stamp either — it runs as a CLI process whose session id can be empty on hosts
// that inject identity only into hook subprocesses (dsh). A whole-tree review is a
// superset of a scoped review, and both are content-addressed, so accepting either
// can never pass unreviewed code.
//
// scopeAndWholeFingerprint 一次 git 调用同时算出整树指纹与作用域指纹（去掉 exclude
// 后）。调用方需要这一对，因为 `forge review pass` 作为 CLI 进程可能拿不到会话 id
// （dsh 只在 hook 子进程注入身份），于是盖的是整树戳；整树审查是作用域审查的超集，
// 且两者都按内容寻址，接受任一命中都不会放行未审代码。
func scopeAndWholeFingerprint(root, baseCommit string, exclude map[string]bool) (scopeHash, wholeHash string, included []string, err error) {
	if !isGitRepo(root) {
		return "", "", nil, nil
	}
	base := baseCommit
	if base == "" {
		base = "HEAD"
	}
	if out, e := gitOut(root, "rev-parse", "--verify", base+"^{commit}"); e != nil || strings.TrimSpace(out) == "" {
		return "", "", nil, fmt.Errorf("base commit %q not reachable: %w", base, e)
	}
	tracked, untracked := changedSourceFilesSince(root, base)
	all := append(append([]string{}, tracked...), untracked...)
	if len(all) == 0 {
		return "", "", nil, nil
	}
	wholeHash = hashSourceFiles(root, base, all)
	included = slices.Clone(all)
	if len(exclude) > 0 {
		kept := included[:0]
		for _, f := range included {
			if !exclude[f] {
				kept = append(kept, f)
			}
		}
		included = kept
	}
	if len(included) > 0 {
		scopeHash = hashSourceFiles(root, base, included)
	}
	return scopeHash, wholeHash, included, nil
}

// foreignWindowEnv overrides the liveness window (minutes) in which another
// session's write still counts as its live work.
//
// foreignWindowEnv 可覆盖「其他会话的写入仍算其在飞工作」的时间窗（分钟）。
const foreignWindowEnv = "FORGE_REVIEW_FOREIGN_WINDOW_MIN"

// foreignForSession returns the changed source paths provably owned by other live
// sessions. Every degradation (no subject, attribution disabled, empty or broken
// ledger, non-git) returns nil ⇒ whole-tree behaviour: when in doubt review more,
// never less.
//
// foreignForSession 返回可证明属于其他活跃会话的变更路径。所有退化路径（无主体、
// 归属层关闭、台账空/损坏、非 git）都返回 nil ⇒ 整树行为：存疑时宁可多审。
func foreignForSession(root, sid string) []string {
	if sid == "" || !attribution.Enabled() {
		return nil
	}
	return attribution.Reconcile(root).Foreign(sid, foreignWindow())
}

// foreignWindow resolves the liveness window. Only a positive value is accepted:
// 0 would mean "no time limit", i.e. a write from hours ago would still count as
// another session's live work and get excluded — the one degradation direction
// this gate must never take (under-review). Invalid, non-positive and unparsable
// values all fall back to the default.
//
// foreignWindow 解析时效窗口。只接受正数：0 表示「不限时」，几小时前的写入仍会被当成
// 他人的在飞工作而排除——正是本门禁绝不能走的漏审方向。非法/非正/无法解析一律回落默认。
func foreignWindow() time.Duration {
	raw := strings.TrimSpace(os.Getenv(foreignWindowEnv))
	if raw == "" {
		return attribution.DefaultForeignWindow
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return attribution.DefaultForeignWindow
	}
	return time.Duration(n) * time.Minute
}

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

// computeDiffHash 算当前工作区相对 HEAD 的源码变更指纹。退化为 SourceChangesSince(root, "")，
// 单一真相源——非 task 模式 stamp 与 task 模式快照共用同一套文件过滤与哈希逻辑，避免漂移。
func computeDiffHash(root string) (hash string, hasChanges bool, err error) {
	return SourceChangesSince(root, "")
}

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

// CurrentBranch exports currentBranch for callers that need the branch context
// of a non-task stamp (e.g. checklog audit detail in cmd `forge review pass`).
//
// CurrentBranch 导出 currentBranch，供需要非 task 戳分支上下文的调用方使用
// （如 `forge review pass` 命令的 checklog 审计 detail）。
func CurrentBranch(root string) string {
	return currentBranch(root)
}

// gitOut runs git in root with a fixed global config: core.quotepath=off. Without
// it git C-quotes non-ASCII paths ("\344\270\255\346\226\207.go"), while the
// attribution ledger stores raw UTF-8 — the two sets then never intersect (foreign
// paths are never excluded) and `git show base:<quoted path>` fails, so content
// edits to such files leave the fingerprint unchanged (fail-open). One flag keeps
// every git-derived path in the same encoding as the ledger.
//
// gitOut 在 root 下执行 git，并固定 core.quotepath=off：否则 git 会把非 ASCII 路径
// 转义成 "\344\270\255..."，而归属台账存的是原生 UTF-8——两侧永不相交（外来路径永不
// 命中排除集），且 `git show <quoted>` 取不到内容，导致中文文件的**内容**改动不进指纹
// （fail-open）。一个 flag 让所有 git 路径与台账同编码。
func gitOut(root string, args ...string) (string, error) {
	base := []string{"-c", "core.quotepath=off", "-C", root}
	cmd := exec.Command("git", append(base, args...)...)
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

// stampContentPath 是内容寻址的 stamp 落点（multi-task-concurrency 设计 §5）：
// stamps/dh-<diffhash>.stamp。diff hash 本就是审查通过的语义键——knownReviewed 早已按
// hash 跨分支匹配——按内容寻址让主查找精确化，并把 stamp 从 branch/worktree 碰撞里解
// 放出来（同分支两个 worktree 此前共享一枚 <branch>.stamp，里面的 hash 是最后保存的
// 那个 worktree 的）。旧 <branch>.stamp 路径保留只读兼容。
func stampContentPath(root, diffHash string) string {
	return filepath.Join(forgedata.DataDirFor(root), "stamps", "dh-"+taskcontext.SanitizeRef(diffHash)+".stamp")
}

func saveStamp(root string, s *Stamp) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// 内容寻址写入；旧分支路径仅作迁移前 stamp 的读兼容回落（无 DiffHash 的 stamp 无内容
	// 可寻址——防御式，所有调用方保存前都先算 hash）。
	path := stampPath(root)
	if s.DiffHash != "" {
		path = stampContentPath(root, s.DiffHash)
	}
	return util.AtomicWrite(path, data, 0o644)
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
