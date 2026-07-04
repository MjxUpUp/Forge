// Package hazard 实现"高危命令 human-in-the-loop 确认"的标记持久化，支撑
// on-demand-guards 自动挡（hazard-guard hook + forge hazard confirm）。
//
// 机制：PreToolUse Bash hook hazard-guard 检测到高危命令 → block（exit 1）+
// additionalContext 指引 agent 用所在 AI 工具的提问确认工具获得用户明确确认后，
// 运行 `forge hazard confirm "<命令>"` 登记一个限时（5min）确认标记 → agent 重试
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
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

// ConfirmTTL 是一次确认标记的有效期。窗口内同指纹重试不重复 block；窗口外需重新
// 确认——避免一次确认永久放行某高危命令（确认应针对当次操作，不是 carte blanche）。
const ConfirmTTL = 5 * time.Minute

// maxCommandStore 是登记时存储的命令字符串截断长度——仅用于审计/展示，过长会撑大
// .forge/hazards/<fp>.json，且命令全文可由指纹反查日志。
const maxCommandStore = 200

// Confirmation 记录某条命令的一次人工确认，存 .forge/hazards/<fingerprint>.json。
type Confirmation struct {
	Fingerprint string    `json:"fingerprint"`       // Fingerprint(命令) 的 sha256 hex
	Command     string    `json:"command,omitempty"` // 登记的命令（截断，审计用）
	ConfirmedAt time.Time `json:"confirmed_at"`
	ExpiresAt   time.Time `json:"expires_at"` // ConfirmedAt + ConfirmTTL
}

// Fingerprint 返回命令的稳定指纹（sha256 hex）。空白归一化（连续空白折叠为单空格、
// 去首尾），故 `rm  -rf /x` 与 `rm -rf /x` 同指纹——agent 重试时空白抖动不该要求重新
// 确认。大小写保留（命令大小写有意义）。同输入必同输出（跨 hook/CLI 调用一致）。
func Fingerprint(cmd string) string {
	normalized := strings.Join(strings.Fields(cmd), " ")
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

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

// isValidFingerprint 判定指纹是否为合法 sha256 hex：恰好 64 字符、全部是 hex 数字
// （[0-9a-fA-F]，hex.DecodeString 接受大小写）。Fingerprint() 产出小写 64 hex，hook
// 输出同款。大小写语义不归本函数管——调用方 ConfirmByFingerprint 先 ToLower 归一化，
// 本函数只判"是否 64 字符合法 hex"。
//
// 用途：confirm --fingerprint 回传时校验。agent（尤非 Claude 的转写型模型）回传 64 字符
// hex 时会逐 token 重新生成，常漏字符/错字符（2026-07 AgentWorld 事故：三次 confirm
// 抄出 e1/91(残缺)/941 三种结尾，前两次写入错文件名却报"✅ 已确认"，hook 用真指纹查
// 不到继续拦）。校验把这类失真挡在登记前，而非给虚假成功。
func isValidFingerprint(fp string) bool {
	if len(fp) != sha256.Size*2 { // sha256.Size=32 字节 → 64 hex 字符
		return false
	}
	_, err := hex.DecodeString(fp)
	return err == nil
}

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

// writeConfirmation 构造 Confirmation 落盘（AtomicWrite = temp+rename）。
func writeConfirmation(p *forgedata.Project, fp, cmd string) error {
	now := time.Now()
	c := Confirmation{
		Fingerprint: fp,
		Command:     truncate(cmd, maxCommandStore),
		ConfirmedAt: now,
		ExpiresAt:   now.Add(ConfirmTTL),
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal confirmation: %w", err)
	}
	if err := util.AtomicWrite(p.HazardsConfirmPath(fp), data, 0o644); err != nil {
		return fmt.Errorf("write confirmation: %w", err)
	}
	return nil
}

// IsConfirmed 查指纹是否有未过期确认。不存在/损坏视为未确认（下次拦了重新确认）。
// hook 脚本调 `forge hazard confirmed <fp>` 用 exit code 传达结果。
func IsConfirmed(p *forgedata.Project, fp string) (bool, error) {
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
	return time.Now().Before(c.ExpiresAt), nil
}

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
			// 顺带清理过期标记，避免目录无限增长
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	// 按 ExpiresAt 升序（最快过期在前），与注释承诺一致，status 输出可预测。
	sort.Slice(out, func(i, j int) bool {
		return out[i].ExpiresAt.Before(out[j].ExpiresAt)
	})
	return out, nil
}

func truncate(s string, n int) string {
	// 按 rune（字符）而非字节切片：中文命令每字 3 字节，字节切片会在字符中间切断产生
	// 无效 UTF-8，json.Marshal 会替换为 U+FFFD 导致审计日志/Confirmation 乱码。
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
