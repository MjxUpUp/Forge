// Package hazard implements the persistent marker for high-risk-command human-in-the-loop
// confirmation, backing the on-demand-guards automatic gate (hazard-guard hook + forge hazard confirm).
//
// Mechanism: the PreToolUse Bash hook hazard-guard detects a high-risk command -> blocks (exit 1)
// with additionalContext instructing the agent to obtain explicit user confirmation via the
// questioning/confirmation tool of whichever AI tool hosts it, then runs
// forge hazard confirm <command> to register a time-limited (5min) confirmation marker; the agent
// retries the original command; the hook sees the marker (same fingerprint + unexpired) and passes.
// Under the Forge hook model (only approve/block), this is the only viable form of human-in-the-loop:
// Forge cannot invoke each tool's private confirmation popup, so it relies on block + instruction +
// time-limited marker to close the loop.
//
// This package only handles computing/writing/querying the marker; the pattern matching for
// high-risk commands lives in the HazardGuardHook script in hooks/embed.go (BSD-safe case-glob,
// same style as bash-guard).
//
// Package hazard 实现「高危命令 human-in-the-loop 确认」的标记持久化，支撑
// on-demand-guards 自动挡（hazard-guard hook + forge hazard confirm）。
//
// 机制：PreToolUse Bash hook hazard-guard 检测到高危命令 → block（exit 1）+
// additionalContext 指引 agent 用所在 AI 工具的提问确认工具获得用户明确确认后，
// 运行 `forge hazard confirm 「<命令>」` 登记一个限时（5min）确认标记 → agent 重试
// 原命令 → hook 见标记（同指纹 + 未过期）放行。这是 Forge hook 模型（只有 approve/block）
// 下 human-in-the-loop 的唯一落地形态——Forge 调不起各工具私有的确认弹窗，靠 block +
// 指引 + 限时标记闭环。
//
// 本包只管标记的算/写/查；高危命令的模式匹配在 hooks/embed.go HazardGuardHook 脚本里
// （BSD 安全 case-glob，与 bash-guard 同风格）。
package hazard

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// ConfirmTTL is the validity window of a confirmation marker. Within the window, same-fingerprint
// retries are not re-blocked; outside the window, re-confirmation is required. This avoids a
// single confirmation permanently allowing a high-risk command (confirmation should be per-action,
// not carte blanche).
//
// ConfirmTTL 是一次确认标记的有效期。窗口内同指纹重试不重复 block；窗口外需重新
// 确认——避免一次确认永久放行某高危命令（确认应针对当次操作，不是 carte blanche）。
const ConfirmTTL = 5 * time.Minute

// expiryCheckSkew is the clock tolerance IsConfirmed allows when validating a marker's time
// fields: legitimate writes always have ExpiresAt == ConfirmedAt + ConfirmTTL exactly, so only
// cross-machine clock jitter (a marker file rsynced/shared between hosts) needs slack.
//
// expiryCheckSkew 是 IsConfirmed 校验标记时间字段时容忍的时钟偏差：正常写入恒有
// ExpiresAt == ConfirmedAt + ConfirmTTL，只有跨机器时钟抖动（标记文件在多主机间
// 同步/共享）需要这点余量。
const expiryCheckSkew = 5 * time.Second

// maxCommandStore is the truncation length for the command string stored at registration: it is
// only for audit/display; an overly long command would bloat DataDir/hazards/<fp>.json, and the
// full command text can be reverse-looked-up from logs via the fingerprint.
//
// maxCommandStore 是登记时存储的命令字符串截断长度——仅用于审计/展示，过长会撑大
// DataDir/hazards/<fp>.json，且命令全文可由指纹反查日志。
const maxCommandStore = 200

// Confirmation records a single human confirmation of a command, stored at
// DataDir/hazards/<fingerprint>.json.
//
// Confirmation 记录某条命令的一次人工确认，存 DataDir/hazards/<fingerprint>.json。
type Confirmation struct {
	Fingerprint string    `json:"fingerprint"`       // Fingerprint(命令) 的 sha256 hex
	Command     string    `json:"command,omitempty"` // 登记的命令（截断，审计用）
	ConfirmedAt time.Time `json:"confirmed_at"`
	ExpiresAt   time.Time `json:"expires_at"` // ConfirmedAt + ConfirmTTL
}

// Fingerprint returns a stable fingerprint (sha256 hex) of the command. Whitespace is normalized
// (consecutive whitespace collapsed to a single space, leading/trailing trimmed), so
// `rm  -rf /x` and `rm -rf /x` share a fingerprint. Whitespace jitter across agent retries should
// not require re-confirmation. Case is preserved (command case is significant). Same input always
// yields same output (consistent across hook/CLI calls).
//
// Fingerprint 返回命令的稳定指纹（sha256 hex）。空白归一化（连续空白折叠为单空格、
// 去首尾），故 `rm  -rf /x` 与 `rm -rf /x` 同指纹——agent 重试时空白抖动不该要求重新
// 确认。大小写保留（命令大小写有意义）。同输入必同输出（跨 hook/CLI 调用一致）。
func Fingerprint(cmd string) string {
	normalized := strings.Join(strings.Fields(cmd), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// Confirm registers one confirmation: compute the fingerprint, write the time-limited marker,
// return the fingerprint. Called by forge hazard confirm (without --fingerprint). Re-confirming
// the same command renews it (overwrite write, ExpiresAt refreshed).
//
// Concurrency: AtomicWrite is temp+rename, so multi-process same-fingerprint confirm is
// last-writer-wins; ExpiresAt values are all approximately now+5min and the window jitters by at
// most a few seconds, negligible for low-frequency human-triggered HITL; add flock if strict
// serialization is needed.
//
// Confirm 登记一次确认：算指纹 → 写限时标记 → 返回指纹。forge hazard confirm（无
// --fingerprint）调用。同一命令重复 confirm 续期（覆盖写，ExpiresAt 刷新）。
//
// 并发：AtomicWrite 是 temp+rename，多进程同指纹 confirm 是 last-writer-wins——
// ExpiresAt 都 ≈ now+5min，窗口最多抖动几秒，HITL 低频人工触发可忽略；需严格串行化加 flock。
func Confirm(p *forgedata.Project, cmd string) (string, error) {
	fp := Fingerprint(cmd)
	if err := writeConfirmation(p, fp, cmd); err != nil {
		return "", err
	}
	return fp, nil
}

// isValidFingerprint decides whether a fingerprint is a legal sha256 hex: exactly 64 characters,
// all hex digits ([0-9a-fA-F]; hex.DecodeString accepts both cases). Fingerprint() produces
// lowercase 64-hex, and the hook output matches. Case semantics are not this function's concern:
// the caller ConfirmByFingerprint first ToLowers to normalize, this function only checks whether
// it is a 64-character legal hex.
//
// Use: validation when confirm --fingerprint passes a value back. Agents (especially non-Claude
// transcribing models) regenerate 64-char hex token by token and frequently drop/mistype
// characters (2026-07 AgentWorld incident: three confirms produced e1/91(incomplete)/941 three
// endings; the first two wrote wrong filenames yet reported success, and the hook kept blocking
// because the real fingerprint was missing). Validation blocks this kind of distortion before
// registration rather than granting false success.
//
// isValidFingerprint 判定指纹是否为合法 sha256 hex：恰好 64 字符、全部是 hex 数字
// （[0-9a-fA-F]，hex.DecodeString 接受大小写）。Fingerprint() 产出小写 64 hex，hook
// 输出同款。大小写语义不归本函数管——调用方 ConfirmByFingerprint 先 ToLower 归一化，
// 本函数只判「是否 64 字符合法 hex」。
//
// 用途：confirm --fingerprint 回传时校验。agent（尤非 Claude 的转写型模型）回传 64 字符
// hex 时会逐 token 重新生成，常漏字符/错字符（2026-07 AgentWorld 事故：三次 confirm
// 抄出 e1/91(残缺)/941 三种结尾，前两次写入错文件名却报「✅ 已确认」，hook 用真指纹查
// 不到继续拦）。校验把这类失真挡在登记前，而非给虚假成功。
func isValidFingerprint(fp string) bool {
	if len(fp) != sha256.Size*2 { // sha256.Size=32 字节 → 64 hex 字符
		return false
	}
	_, err := hex.DecodeString(fp)
	return err == nil
}

// ValidateFingerprint accepts mixed-case input and validates length/charset; on illegal input it
// returns an invalid fingerprint error. ToLower is only used for internal validation to pass; the
// normalized value is NOT returned. The write side (ConfirmByFingerprint) must ToLower to lowercase
// itself. Exported so the cli layer can validate up front before findProjectRoot: format validation
// is pure input validation and does not need project context, avoiding the case where a missing
// .forge/ (e.g. CI fresh checkout) makes not-in-a-forge-project mask a fingerprint validation
// failure. An agent that mis-copies a fingerprint should be explicitly rejected, not given vague
// feedback due to unrelated project-location errors. The error message shares a source with
// ConfirmByFingerprint, ensuring consistent feedback across the two paths.
//
// ValidateFingerprint 接受大小写混合输入并校验长度/字符集，非法返回 invalid fingerprint
// 错误。ToLower 仅用于内部校验通过，不返回归一化值——落盘侧（ConfirmByFingerprint）须
// 自行 ToLower 小写。导出供 cli 层在 findProjectRoot 前做前置校验：格式校验是纯输入校验，
// 不需要项目上下文，避免无 .forge/（如 CI fresh checkout）时 not-in-a-forge-project 掩盖
// 指纹校验失败——agent 抄错指纹应被明确拒绝，不因无关的项目定位错误获得模糊反馈。错误
// 信息与 ConfirmByFingerprint 同源，保证两条路径反馈一致。
func ValidateFingerprint(fp string) error {
	lower := strings.ToLower(fp)
	if !isValidFingerprint(lower) {
		return fmt.Errorf(`invalid fingerprint (got %d chars, want 64-char sha256 hex): re-copy the fingerprint verbatim from the hazard-guard block message, or run "forge hazard confirm <command>" without --fingerprint to let forge compute it`, len(lower))
	}
	return nil
}

// ConfirmByFingerprint registers confirmation by the fingerprint the hook already computed,
// bypassing the Fingerprint(cmd) computation (avoids command-string copy distortion: agent-shell
// re-parsing would eat quotes). It first runs ValidateFingerprint to reject illegal formats, then
// ToLowers to lowercase before writing to disk. The hook queries with lowercase; on case-sensitive
// filesystems, an uppercase write would mismatch. Full background on normalization and the
// agent-miscopies-fingerprint incident is in the isValidFingerprint / ValidateFingerprint comments.
//
// ConfirmByFingerprint 按 hook 已算好的指纹登记确认，绕过 Fingerprint(cmd) 计算（避免
// 命令串复制失真：agent shell 重新解析会吃掉引号）。先 ValidateFingerprint 拒非法格式，
// 再 ToLower 小写落盘——hook 用小写查询，大小写敏感文件系统上大写落盘会失配。归一化与
// agent 抄错指纹事故的完整背景见 isValidFingerprint / ValidateFingerprint 注释。
func ConfirmByFingerprint(p *forgedata.Project, fp, cmd string) error {
	if err := ValidateFingerprint(fp); err != nil {
		return err
	}
	return writeConfirmation(p, strings.ToLower(fp), cmd)
}

// writeConfirmation constructs a Confirmation and persists it (AtomicWrite = temp+rename).
//
// writeConfirmation 构造 Confirmation 落盘（AtomicWrite = temp+rename）。
func writeConfirmation(p *forgedata.Project, fp, cmd string) error {
	now := time.Now()
	c := Confirmation{
		Fingerprint: fp,
		Command:     util.TruncateRunes(cmd, maxCommandStore),
		ConfirmedAt: now,
		ExpiresAt:   now.Add(ConfirmTTL),
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal confirmation: %w", err)
	}
	// Audit trail FIRST, marker second: the hook grants passage based on the on-disk
	// marker; if the event append failed after the marker was written, the
	// confirmation would be ACTIVE with zero EventConfirm records — exactly the
	// "every confirm is auditable" invariant break the audit exists for (and the
	// CLI would report failure while the marker is live, splitting state from
	// report). Event-first means a failed audit = no confirmation granted.
	//
	// 审计先行、标记后写：hook 依据磁盘标记放行；若先写标记后追加事件且事件
	// 失败，确认已生效却零 EventConfirm 记录——恰是这段审计要守的「每次确认
	// 必留痕」不变量被击穿（且 CLI 报失败而标记已活，状态与报告分裂）。
	// 事件先行 = 审计失败即不授确认。
	//
	// The forgery path note: an agent hand-writing the marker file (it has write
	// access to DataDir) still leaves no event — that remains for integrity signing
	// (feat/state-integrity-signing). This funnel covers forge-issued confirms.
	//
	// 伪造路径备注：agent 手写标记文件（对 DataDir 有写权限）依旧无事件——该面
	// 留给完整性签名任务（feat/state-integrity-signing）。本漏斗覆盖 forge 发出的
	// 确认。
	if err := AppendEvent(p, Event{Type: EventConfirm, Fingerprint: fp, Command: cmd}); err != nil {
		return fmt.Errorf("log confirm event: %w", err)
	}
	if err := util.AtomicWrite(p.HazardsConfirmPath(fp), data, 0o644); err != nil {
		return fmt.Errorf("write confirmation: %w", err)
	}
	return nil
}

// IsConfirmed queries whether a fingerprint has an unexpired confirmation. Missing/corrupt is
// treated as unconfirmed (next block triggers re-confirmation). The hook script invokes
// `forge hazard confirmed <fp>` and conveys the result via exit code.
//
// Read-side hardening (the agent has write access to DataDir, so a hand-forged marker file must
// not pass):
//   - fp is format-validated like the write side (ConfirmByFingerprint has ValidateFingerprint):
//     an illegal fp such as "../../tasks/x" would otherwise escape hazards/ via HazardsConfirmPath
//     and probe arbitrary .json files under DataDir. Illegal fp → unconfirmed.
//   - The file content's Fingerprint must equal fp: a forged file named after a real fingerprint
//     but with arbitrary content is rejected.
//   - Time fields are sanity-bounded: ConfirmedAt must not be in the future, and
//     ExpiresAt-ConfirmedAt must be within [0, ConfirmTTL+skew] — a hand-written
//     {"expires_at":"2999-..."} must not grant permanent release.
//
// IsConfirmed 查指纹是否有未过期确认。不存在/损坏视为未确认（下次拦了重新确认）。
// hook 脚本调 `forge hazard confirmed <fp>` 用 exit code 传达结果。
//
// 读侧加固（agent 对 DataDir 有写权限，手写伪造标记不得放行）：
//   - fp 与写侧同源做格式校验（写侧 ConfirmByFingerprint 有 ValidateFingerprint）：
//     非法 fp（如 "../../tasks/x"）否则会经 HazardsConfirmPath 逃逸出 hazards/，
//     探测 DataDir 下任意 .json。非法 fp 按未确认处理。
//   - 文件内容的 Fingerprint 必须等于 fp：文件名是真指纹、内容任意的伪造文件被拒。
//   - 时间字段合理性上界：ConfirmedAt 不得在未来，且 ExpiresAt-ConfirmedAt 必须在
//     [0, ConfirmTTL+skew] 内——手写 {"expires_at":"2999-..."} 不得永久放行。
func IsConfirmed(p *forgedata.Project, fp string) (bool, error) {
	// Normalize like the write side (ConfirmByFingerprint ToLowers before persisting), then
	// validate format; illegal input is treated as unconfirmed, never as a path.
	//
	// 先与写侧同款归一化（ConfirmByFingerprint 落盘前 ToLower），再校验格式；
	// 非法输入按未确认处理，绝不当路径用。
	fp = strings.ToLower(fp)
	if !isValidFingerprint(fp) {
		return false, nil
	}
	data, err := os.ReadFile(p.HazardsConfirmPath(fp))
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	var c Confirmation
	if err := json.Unmarshal(data, &c); err != nil {
		return false, nil // 损坏视为未确认，下次拦了重新确认
	}
	// Content fingerprint must match the queried one (file name is not proof of content).
	//
	// 内容指纹必须与查询指纹一致（文件名不是内容的证明）。
	if c.Fingerprint != fp {
		return false, nil
	}
	now := time.Now()
	// ConfirmedAt in the future (beyond clock skew) means a forged timestamp.
	//
	// ConfirmedAt 在未来（超出时钟偏差）即伪造时间戳。
	if c.ConfirmedAt.After(now.Add(expiryCheckSkew)) {
		return false, nil
	}
	// The validity window must not exceed ConfirmTTL (plus skew); negative windows are corrupt.
	//
	// 有效窗口不得超 ConfirmTTL（加偏差）；负窗口视为损坏。
	if d := c.ExpiresAt.Sub(c.ConfirmedAt); d < 0 || d > ConfirmTTL+expiryCheckSkew {
		return false, nil
	}
	return now.Before(c.ExpiresAt), nil
}

// ActiveConfirmations lists currently unexpired confirmations (for forge hazard status). Sorted
// ascending by ExpiresAt (earliest-expiring first). Also opportunistically cleans up expired files.
//
// ActiveConfirmations 列出当前未过期的确认（forge hazard status 用）。按 ExpiresAt
// 升序（最快过期的在前）。顺便清理已过期文件。
func ActiveConfirmations(p *forgedata.Project) ([]Confirmation, error) {
	dir := p.HazardsDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read hazards dir: %w", err)
	}
	now := time.Now()
	var out []Confirmation
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var c Confirmation
		if err := json.Unmarshal(data, &c); err != nil {
			continue
		}
		if now.Before(c.ExpiresAt) {
			out = append(out, c)
		} else {
			// Opportunistically clean up expired markers, preventing unbounded directory growth
			//
			// 顺带清理过期标记，避免目录无限增长
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	// Sort by ExpiresAt ascending (earliest-expiring first), consistent with the comment promise,
	// for predictable status output.
	//
	// 按 ExpiresAt 升序（最快过期在前），与注释承诺一致，status 输出可预测。
	slices.SortFunc(out, func(a, b Confirmation) int {
		return a.ExpiresAt.Compare(b.ExpiresAt)
	})
	return out, nil
}
