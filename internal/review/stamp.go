// Package review 实现"代码审查通过"的持久化标记与门禁决策，支撑两条触发路径：
//   - task 流程内：ReviewPassed 字段存 .forge/tasks/<ref>.json（taskpipeline 管）
//   - 非 task 流程：diff hash stamp 存 .forge/stamps/<branch>.stamp（本包管）
//
// 两者服务于同一目标——让 code-review-gate 从"靠人手动喊"变成"门禁/hook 自动挡"。
// 本包只管非 task 模式的 stamp；task 模式的 ReviewPassed 读写留在 taskpipeline 包内
// （避免 task-complete 门禁 import review → review import taskpipeline 的循环依赖）。
package review

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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

// Stamp 记录某分支当前 diff 的审查状态，存 .forge/stamps/<branch>.stamp。
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
func computeDiffHash(root string) (hash string, hasChanges bool, err error) {
	tracked, untracked := changedSourceFiles(root)
	if len(tracked) == 0 && len(untracked) == 0 {
		return "", false, nil
	}
	var parts []string
	// tracked 源码变更内容（staged + unstaged）
	if len(tracked) > 0 {
		args := append([]string{"diff", "HEAD", "--"}, tracked...)
		if out, e := gitOut(root, args...); e == nil {
			if s := strings.TrimSpace(out); s != "" {
				parts = append(parts, s)
			}
		}
	}
	// untracked 源码新文件（仅文件名；内容变化不纳入，但首次出现即触发审查）
	if len(untracked) > 0 {
		parts = append(parts, "---UNTRACKED---", strings.Join(untracked, "\n"))
	}
	if len(parts) == 0 {
		return "", false, nil
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:]), true, nil
}

// changedSourceFiles 返回当前相对 HEAD 变更的【源码】文件，分 tracked（修改/删除）和
// untracked（新增）。已排除 .forge/、非源码扩展、生成物路径。
func changedSourceFiles(root string) (tracked, untracked []string) {
	if !isGitRepo(root) {
		return nil, nil
	}
	if out, e := gitOut(root, "diff", "--name-only", "HEAD", "--", ".", ":(exclude).forge"); e == nil {
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
	return filepath.Join(root, ".forge", "stamps", taskcontext.SanitizeRef(branch)+".stamp")
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
