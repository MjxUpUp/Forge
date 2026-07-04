// Package review 实现"代码审查通过"的持久化标记与门禁决策，支撑两条触发路径：
//   - task 流程内：ReviewPassed 字段存 DataDir/tasks/<ref>.json（taskpipeline 管）
//   - 非 task 流程：diff hash stamp 存 DataDir/stamps/<branch>.stamp（本包管）
//
// 两者服务于同一目标——让 code-review-gate 从"靠人手动喊"变成"门禁/hook 自动挡"。
// 本包管非 task 模式的 stamp，并导出 SourceChangesSince 供 taskpipeline 在 task 模式算
// "审查后代码是否变更"的快照（task-complete 门禁据此强制复审）。循环依赖不存在：review 只
// 依赖 taskcontext+util，不 import taskpipeline；taskpipeline 单向 import review。
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/util"
)

// MaxReviewRounds 是 Stop hook 反复 block 同一 diff 的兜底上限。超过后 advisory
// 放行——防 agent 不调 forge review pass 导致的 Stop 死循环（task-verify 历史教训：
// Stop block 会触发 retry-loop，见 hooks/embed.go TaskVerifyHook 注释）。
const MaxReviewRounds = 3

// Decision 是 Evaluate 对非 task 模式当前 diff 的审查决策。
type Decision int

const (
	DecisionPass         Decision = iota // 无需审：已审过这版 diff，或无变更/非 git
	DecisionNeedReview                   // 需审：有未审变更，Stop hook 应 block
	DecisionPassAdvisory                 // 兜底放行：撞 MaxReviewRounds，advisory 提醒
)

// Stamp 记录某分支当前 diff 的审查状态，存 DataDir/stamps/<branch>.stamp。
type Stamp struct {
	DiffHash   string    `json:"diff_hash"`             // 审查范围（git diff）的 sha256；空=无变更
	Reviewed   bool      `json:"reviewed"`              // 该 diff_hash 是否已通过 code-review-gate
	BlockCount int       `json:"block_count"`           // 该 diff_hash 被 Stop hook block 的次数
	ReviewedAt time.Time `json:"reviewed_at,omitempty"` // 最近一次 forge review pass 时间
	Branch     string    `json:"branch,omitempty"`
}

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

	stamp, _ := LoadStamp(root)

	// 同一 diff 已审过 → 放行
	if stamp.DiffHash == hash && stamp.Reviewed {
		return DecisionPass, "当前 diff 已通过 code-review-gate", nil
	}

	// 撞 max rounds 兜底放行（防 Stop 死循环）
	if stamp.DiffHash == hash && stamp.BlockCount >= MaxReviewRounds {
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
	return DecisionNeedReview,
		fmt.Sprintf("检测到未审查的代码变更（block %d/%d）", next.BlockCount, MaxReviewRounds), nil
}

// MarkPassed 标记当前 diff 已通过审查（forge review pass 调用）。
// 算当前 hash 写 reviewed stamp，重置 block_count。
func MarkPassed(root string) error {
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
	}
	return saveStamp(root, stamp)
}

// LoadStamp 读取当前分支的审查 stamp；不存在/损坏返回空 Stamp（非 error）。
func LoadStamp(root string) (*Stamp, error) {
	data, err := os.ReadFile(stampPath(root))
	if err != nil {
		return &Stamp{}, nil
	}
	var s Stamp
	if err := json.Unmarshal(data, &s); err != nil {
		return &Stamp{}, nil // 损坏视为空，下次重审
	}
	return &s, nil
}

// CurrentState 返回人读的当前审查状态（forge review status 用）。
func CurrentState(root string) (string, error) {
	hash, hasChanges, err := computeDiffHash(root)
	if err != nil {
		return "", err
	}
	stamp, _ := LoadStamp(root)
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

	switch {
	case !hasChanges:
		b.WriteString("\n→ 无未提交变更，无需审查\n")
	case stamp.DiffHash == hash && stamp.Reviewed:
		b.WriteString("\n→ 当前 diff 已通过审查\n")
	default:
		b.WriteString("\n→ 当前 diff 未审查：加载 code-review-gate 派只读子 agent 审查，通过后 forge review pass\n")
	}
	return b.String(), nil
}

// --- internals ---

// computeDiffHash 算当前工作区相对 HEAD 的【源码】变更指纹（sha256），用于判断"这版代码审过没"。
// 误触发防护（2026-06-27）：审查范围**只统计源码文件**——
//   - 排除 .forge/（否则写 stamp 改 diff → hash 变 → 死循环）
//   - 排除非源码扩展（.md/.txt/.yml/.json/.toml 等文档与配置）——改 README 不该被逼审代码
//   - 排除生成物（路径含 .gen./_generated/.pb./vendor/ 等）——自动生成代码刷 hash 无意义
//
// 纯文档/配置/生成物变更 → changedSourceFiles 空 → ("", false, nil) → 无需审。
// 非 git 仓库同样 → 无需审。
// SourceChangesSince 算"自 baseCommit 起到当前工作区"的【源码】变化指纹（sha256）。
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
	// 基线可达性：amend/rebase 后旧 commit 可能不可达，git diff/show 会 fatal。提前 verify 返回 err，
	// 让调用方 fail-open（而非把 git stderr 当"无变更"误判放行）。
	if out, e := gitOut(root, "rev-parse", "--verify", base+"^{commit}"); e != nil || strings.TrimSpace(out) == "" {
		return "", false, fmt.Errorf("base commit %q not reachable: %w", base, e)
	}
	tracked, untracked := changedSourceFilesSince(root, base)
	files := append(tracked, untracked...)
	if len(files) == 0 {
		return "", false, nil
	}
	sort.Strings(files)
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "%s\n%s\n", f, fileContentForHash(root, base, f))
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:]), true, nil
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
