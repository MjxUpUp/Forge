package taskpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskcontext"
	"github.com/MjxUpUp/Forge/internal/util"
)

// specs.go implements the L6 artifact-contract layer (multi-task-concurrency design §9):
// narrative artifacts (proposal/spec/design/plan) and review attempts live as FILES in
// the harness repo's per-project specs directory, while TaskState holds only verifiable
// REFERENCES (path + content hash) — invariant I5: one owning medium (the file), the
// other side holds hash references. Attempts are write-once: prior review failures are
// preserved under attempts/round-NNN/ and fed back to the next round
//（LoopSpec priorAttempts），never deleted; the rollback closure never touches TaskState
//（持久锚豁免，LoopSpec state.md 语义）。
//
// AcceptanceCriterion stays authoritative for gates — tasks.md-style checkboxes are a
// human view, never a completion signal（防弱类型退化）。
//
// specs.go 实现 L6 产物契约层（multi-task-concurrency 设计 §9）：叙事产物
//（proposal/spec/design/plan）与 review 尝试以【文件】形态落在 harness repo 的每项目
// specs 目录，TaskState 只持可验证【引用】（路径 + 内容哈希）——不变式 I5：拥有介质
// 唯一（文件），另一侧持哈希引用。attempts 一次写入：历次审查失败保存在
// attempts/round-NNN/ 并回灌给下一轮（LoopSpec priorAttempts），永不删除；回滚闭包
// 永不触碰 TaskState（持久锚豁免，LoopSpec state.md 语义）。
//
// AcceptanceCriterion 对门禁保持权威——tasks.md 式复选框是人读视图，绝不是完成信号
//（防弱类型退化）。

// ArtifactRef is TaskState's verifiable pointer to a spec file (I5): Path is
// DataDir-relative (portable across machines — the project key maps to different
// absolute DataDirs), Hash is the content sha256 (first 16 hex). A mismatch means the
// file was hand-edited after the reference was taken — surface as drift, never silent.
//
// ArtifactRef 是 TaskState 指向 spec 文件的可验证指针（I5）：Path 相对 DataDir
//（跨机可移植——project key 在不同机器映射到不同绝对 DataDir），Hash 是内容 sha256
// 前 16 hex。失配 = 引用建立后文件被手改——按漂移上浮，绝不静默。
type ArtifactRef struct {
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SpecsDir is the task's artifact directory inside the project DataDir (physically inside
// the harness repo's tracked set once T6 initialized it).
//
// SpecsDir 是任务在项目 DataDir 内的产物目录（T6 init 后物理上处于 harness repo 的
// tracked 集合内）。
func SpecsDir(root, taskRef string) string {
	return filepath.Join(forgedata.DataDirFor(root), "specs", taskcontext.SanitizeRef(taskRef))
}

// artifactFrontmatter renders the identity header every spec file carries: the reference
// triangle task-ref ↔ stage ↔ hash is verifiable from the file itself.
//
// artifactFrontmatter 渲染每个 spec 文件携带的身份头：task-ref ↔ stage ↔ hash 的引用
// 三角形从文件自身即可校验。
func artifactFrontmatter(taskRef, stage string, updatedAt time.Time) string {
	return fmt.Sprintf("---\nforge-task-ref: %s\nforge-stage: %s\nupdated-at: %s\n---\n\n",
		taskRef, stage, updatedAt.Format(time.RFC3339))
}

// WriteArtifact writes a stage artifact for the task and returns its ArtifactRef. It does
// NOT mutate TaskState — callers fold the ref in before their own SaveTaskState（写文件
// 与改状态分离，锁语义由调用方持有）。
//
// WriteArtifact 写任务的阶段产物并返回其 ArtifactRef。它不改动 TaskState——调用方在
// 自己的 SaveTaskState 前把引用折进去（写文件与改状态分离，锁语义由调用方持有）。
func WriteArtifact(root, taskRef, stage, content string) (ArtifactRef, error) {
	dir := SpecsDir(root, taskRef)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ArtifactRef{}, err
	}
	now := time.Now()
	body := artifactFrontmatter(taskRef, stage, now) + content
	sum := sha256.Sum256([]byte(body))
	rel := filepath.ToSlash(filepath.Join("specs", taskcontext.SanitizeRef(taskRef), stage+".md"))
	abs := filepath.Join(forgedata.DataDirFor(root), filepath.FromSlash(rel))
	if err := util.AtomicWrite(abs, []byte(body), 0o644); err != nil {
		return ArtifactRef{}, err
	}
	return ArtifactRef{Path: rel, Hash: hex.EncodeToString(sum[:])[:16], UpdatedAt: now}, nil
}

// VerifyArtifact re-hashes the referenced file and reports whether it still matches the
// ref — the drift detector (I5's 反 desync：哈希失配即人工改动，触发重新确认).
//
// VerifyArtifact 重算引用文件的哈希并报告是否仍匹配——漂移探测器（I5 的反 desync：
// 哈希失配即人工改动，触发重新确认）。
func VerifyArtifact(root string, ref ArtifactRef) bool {
	if ref.Path == "" || ref.Hash == "" {
		return false
	}
	data, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(root), filepath.FromSlash(ref.Path)))
	if err != nil {
		return false
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])[:16] == ref.Hash
}

// attemptVerdict is the machine-readable half of an archived attempt round.
//
// attemptVerdict 是归档尝试轮次的机器可读半边。
type attemptVerdict struct {
	Round     int       `json:"round"`
	TaskRef   string    `json:"task_ref"`
	Verdict   string    `json:"verdict"` // "fail"——归档只发生在失败轮
	ArchivedAt time.Time `json:"archived_at"`
	Findings  int       `json:"findings"`
}

// ArchiveAttempt preserves one failed review round under specs/<ref>/attempts/round-NNN/
//（findings.md 人类可读 + verdict.json 机器可读）。Write-once by construction — the
// round number is derived from the caller's round, and re-archiving the same round
// REFUSES rather than overwrites（失败上下文永不覆盖，LoopSpec "move, never delete"）。
// Never touches TaskState（回滚闭包的持久锚豁免）。
//
// ArchiveAttempt 把一个失败的审查轮次保存在 specs/<ref>/attempts/round-NNN/
//（findings.md 人类可读 + verdict.json 机器可读）。构造上一次写入——轮号由调用方
// 的 round 推导，重复归档同轮【拒绝】而非覆盖（失败上下文永不覆盖，LoopSpec
// "move, never delete"）。绝不触碰 TaskState（回滚闭包的持久锚豁免）。
func ArchiveAttempt(root, taskRef string, round int, findings []string) error {
	if round <= 0 {
		return fmt.Errorf("attempt round 必须 >= 1")
	}
	roundDir := filepath.Join(SpecsDir(root, taskRef), "attempts", fmt.Sprintf("round-%03d", round))
	if _, err := os.Stat(filepath.Join(roundDir, "verdict.json")); err == nil {
		return fmt.Errorf("attempt round-%03d 已归档（一次写入，永不覆盖）", round)
	}
	if err := os.MkdirAll(roundDir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Round %03d 审查失败归档（task %s）\n\n", round, taskRef)
	if len(findings) == 0 {
		b.WriteString("（无结构化 findings——见当轮 transcript/checklog）\n")
	}
	for i, f := range findings {
		fmt.Fprintf(&b, "%d. %s\n", i+1, f)
	}
	b.WriteString("\n> 回灌契约：下一轮修复前先读本目录历史轮次，不重复踩已指出的坑。\n")
	if err := util.AtomicWrite(filepath.Join(roundDir, "findings.md"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	v, _ := json.MarshalIndent(attemptVerdict{
		Round: round, TaskRef: taskRef, Verdict: "fail",
		ArchivedAt: time.Now(), Findings: len(findings),
	}, "", "  ")
	return util.AtomicWrite(filepath.Join(roundDir, "verdict.json"), v, 0o644)
}

// PriorAttemptsSummary renders the last N archived rounds' findings as bounded input for
// the next attempt（LoopSpec priorAttempts：带着「为什么被拒」重做，不重复踩坑）。
// Empty string when no attempts exist. Char-capped（注入面纪律：回灌内容按数据渲染，
// 长度有界）。
//
// PriorAttemptsSummary 渲染最近 N 个归档轮次的 findings，作为下一次尝试的有界输入
//（LoopSpec priorAttempts：带着「为什么被拒」重做，不重复踩坑）。无尝试时为空串。
// 字符封顶（注入面纪律：回灌内容按数据渲染，长度有界）。
func PriorAttemptsSummary(root, taskRef string, lastN int, charCap int) string {
	base := filepath.Join(SpecsDir(root, taskRef), "attempts")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	var rounds []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "round-") {
			rounds = append(rounds, e.Name())
		}
	}
	if len(rounds) == 0 {
		return ""
	}
	sort.Strings(rounds)
	if len(rounds) > lastN {
		rounds = rounds[len(rounds)-lastN:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Prior attempts（最近 %d 轮失败归档，勿重复踩坑）:\n", len(rounds))
	for _, r := range rounds {
		data, err := os.ReadFile(filepath.Join(base, r, "findings.md"))
		if err != nil {
			continue
		}
		text := string(data)
		if b.Len()+len(text) > charCap {
			text = text[:max(0, charCap-b.Len())] + "\n…（截断）"
		}
		b.WriteString(text)
	}
	return b.String()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
