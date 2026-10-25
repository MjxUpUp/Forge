package hooks

// infra.go — hook 基础设施失败的分类、去重与自愈判定（docs/plans/hook-silent-failure-visibility-2026-10.md）。
//
// 门禁裁决（block / allow）与基础设施失败（hook 跑不起来、跑崩、settings 与二进制
// 错位）分开处理：前者是 forge 的判定，后者宿主只会当成「非阻塞错误」逐次刷屏。
// 本文件给 forge 进程内能自报的那一端提供三样东西——
//   - 失败分类（InfraFailure）与带修复办法的提示文案（FormatInfraNotice）；
//   - 同会话同因只提示一次的去重（ClaimInfraNotice，状态在 $TMPDIR）；
//   - settings 引用了二进制不认识的 hook 时的发现与自愈（UserSettingsUnknownHooks /
//     HealUnknownUserHooks）。
// 进程内无法自报的失败（node 缺失、forge 不在 hook PATH、126）由 forge doctor 承担，
// 见 internal/doctor/hookruntime.go。
//
// infra.go — classification, dedup and self-heal for hook infrastructure
// failures. Gate verdicts (block/allow) are kept apart from infra failures (a
// hook that cannot start, crashes, or a settings/binary roster mismatch); the
// host only ever surfaces the latter as a per-call "non-blocking error". This
// file serves the side that CAN self-report inside the forge process: failure
// classification + fix-bearing notice text, once-per-session dedup (state in
// $TMPDIR), and discovery/self-heal of settings that reference hooks the
// binary does not know. Failures that cannot self-report (node missing, forge
// off the hook PATH, 126) are carried by forge doctor.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// InfraCause names why a hook invocation failed for infrastructure reasons.
//
// InfraCause 是 hook 调用因基础设施原因失败的原因名。
type InfraCause string

const (
	// InfraUnknownHook: settings wires a hook name this binary does not know (version skew).
	//
	// InfraUnknownHook：settings 接了本二进制不认识的 hook 名（版本错位）。
	InfraUnknownHook InfraCause = "unknown-hook"
	// InfraPanic: the forge process panicked while running a hook.
	//
	// InfraPanic：forge 进程在跑 hook 时 panic。
	InfraPanic InfraCause = "panic"
	// InfraHookError: a hook returned a non-block error (stdin read, script spawn, ...).
	//
	// InfraHookError：hook 返回了非阻断错误（stdin 读取、脚本拉起……）。
	InfraHookError InfraCause = "hook-error"
)

const (
	// infraDedupEnv disables once-per-session dedup (escape hatch): every failure
	// surfaces again as exit 1 + the raw error, the pre-fix behaviour.
	//
	// infraDedupEnv 关闭同会话去重（逃生口）：每次失败都重新 exit 1 + 原始错误，
	// 即修复前的行为。
	infraDedupEnv = "FORGE_HOOK_INFRA_DEDUP"
	// InfraStateDirName is the per-user dedup state dir under os.TempDir().
	//
	// InfraStateDirName 是 os.TempDir() 下的去重状态目录名。
	InfraStateDirName = "forge-hook-infra"
	// infraStateMaxAge bounds how long a session dir lives before opportunistic pruning.
	//
	// infraStateMaxAge 限定会话目录被顺手清理前的寿命。
	infraStateMaxAge = 7 * 24 * time.Hour
	// infraMarkerTTL: a dedup marker older than this is stale (PID reuse across
	// sessions, very long sessions) and is re-claimed so the notice shows again.
	//
	// infraMarkerTTL：早于此的去重标记视为陈旧（跨会话 PID 复用、超长会话），重新
	// 认领使提示再次出现。
	infraMarkerTTL = 12 * time.Hour
	// infraSubjectMax truncates the subject (error first line) in dedup keys and notices.
	//
	// infraSubjectMax 截断去重键与提示里的 subject（报错首行）。
	infraSubjectMax = 120
)

// InfraFailure is one infrastructure failure: why (Cause), about what (Subject —
// the hook name or the first line of the error), and the raw Detail.
//
// InfraFailure 是一次基础设施失败：为什么（Cause）、关于什么（Subject——hook 名或
// 报错首行）、原始 Detail。
type InfraFailure struct {
	Cause   InfraCause
	Subject string
	Detail  string
}

// Key is the dedup identity: same cause + same subject = same reason.
//
// Key 是去重身份：同 cause + 同 subject = 同一原因。
func (f InfraFailure) Key() string {
	return string(f.Cause) + ":" + util.TruncateRunes(strings.TrimSpace(f.Subject), infraSubjectMax)
}

// InfraDedupEnabled reports whether once-per-session dedup is on (default yes;
// FORGE_HOOK_INFRA_DEDUP=0 is the escape hatch — callers land a checklog row).
//
// InfraDedupEnabled 报告同会话去重是否开启（默认开；FORGE_HOOK_INFRA_DEDUP=0 是
// 逃生口——调用方负责落 checklog 行）。
func InfraDedupEnabled() bool {
	return strings.TrimSpace(os.Getenv(infraDedupEnv)) != "0"
}

// InfraDedupEnvName exposes the escape-hatch env name for audit rows and notices.
//
// InfraDedupEnvName 暴露逃生口 env 名，供审计行与提示文案引用。
func InfraDedupEnvName() string { return infraDedupEnv }

// InfraFix returns the repair advice for a failure — the part a bare "exit 1"
// never carried.
//
// InfraFix 返回某失败的修复办法——光秃秃的 exit 1 从不携带的那部分。
func InfraFix(f InfraFailure) string {
	switch f.Cause {
	case InfraUnknownHook:
		return "settings 引用了本 forge 二进制不认识的 hook（二进制与 settings 版本错位）。运行 forge sync --force 按当前二进制重生成接线；仍报错说明 PATH 上有旧版 forge，用 forge doctor 查多副本与版本漂移，升级到与接线一致的版本。"
	case InfraPanic:
		return "forge 在执行 hook 时 panic（已 fail-open，不影响放行）。运行 forge doctor 查二进制版本与多副本；升级 forge 后仍复现请带上本行报错反馈。"
	default:
		return "hook 执行出错（已 fail-open，不影响放行）。运行 forge doctor 查 hook 运行环境（PATH 上的 forge、node）；升级 forge 后仍复现请带上本行报错反馈。"
	}
}

// FormatInfraNotice renders the one-time notice: what failed, that it does not
// block, how to fix, and how to turn dedup off.
//
// FormatInfraNotice 渲染一次性提示：什么失败了、不阻断、怎么修、怎么关去重。
func FormatInfraNotice(f InfraFailure) string {
	detail := util.TruncateRunes(strings.TrimSpace(f.Detail), 300)
	if detail == "" {
		detail = f.Key()
	}
	return fmt.Sprintf("[forge] hook 基础设施故障（%s，已放行、不阻断；本会话同因不再提示）: %s\n修复: %s\n（%s=0 恢复逐次报错）",
		f.Cause, detail, InfraFix(f), infraDedupEnv)
}

// infraStateRoot is the dedup state root: session-scoped short-lived state lives
// under $TMPDIR (OS-cleaned), never the GlobalHome.
//
// infraStateRoot 是去重状态根：会话级短期状态放 $TMPDIR（系统清理），绝不进
// GlobalHome。
func infraStateRoot() string {
	// 带 uid：共享 /tmp（多用户 Linux，TMPDIR=/tmp）下不同用户各用各的根，避免他人
	// 预建同名目录劫持/误导本用户的去重状态与清理。
	//
	// uid-qualified: on a shared /tmp (multi-user Linux, TMPDIR=/tmp) each user gets
	// their own root, so another user's pre-created same-name dir cannot hijack or
	// mislead this user's dedup state and pruning.
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d", InfraStateDirName, os.Getuid()))
}

// InfraSessionBucket maps a (sanitized) session id to its dedup bucket; an empty
// id falls back to the parent process (the host), the best session proxy left.
//
// InfraSessionBucket 把（已归一的）session id 映射为去重桶；id 为空时回落父进程
// （宿主），这是剩下最好的会话代理。
func InfraSessionBucket(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return "ppid-" + strconv.Itoa(os.Getppid())
	}
	return util.SanitizeSessionID(sessionID)
}

// ClaimInfraNotice atomically claims the right to show f's notice in this
// session: the first caller for a (session, key) wins (true), later callers get
// false. O_EXCL marker creation is the atomic step — concurrent hook spawns race
// safely. State errors resolve to true: when dedup cannot be recorded, showing
// again beats swallowing the failure.
//
// ClaimInfraNotice 原子地认领「本会话展示 f 的提示」的权利：同一 (会话, key)
// 第一个调用者拿到 true，之后拿到 false。O_EXCL 创建 marker 是原子步骤——并发 hook
// 拉起安全竞争。状态出错时返回 true：记不下去重时，重复展示好过吞掉失败。
func ClaimInfraNotice(sessionID string, f InfraFailure) bool {
	root := infraStateRoot()
	dir := filepath.Join(root, InfraSessionBucket(sessionID))
	// 根必须是本人创建的真实目录：符号链接（他人预置的劫持）按「状态不可用」处理——
	// 宁可多提示，也不在别处读写/清理。
	if info, err := os.Lstat(root); err == nil && !info.IsDir() {
		return true
	}
	if _, err := os.Stat(dir); err != nil {
		pruneInfraState(root)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return true
	}
	marker := filepath.Join(dir, hex.EncodeToString(sumKey(f)))
	file, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if !os.IsExist(err) {
			return true
		}
		// PID 复用 / 久驻会话：超过 infraMarkerTTL 的旧标记视为陈旧，重新认领。
		if info, serr := os.Stat(marker); serr == nil && time.Since(info.ModTime()) > infraMarkerTTL {
			_ = os.Remove(marker)
			if file, err = os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600); err == nil {
				_, _ = file.WriteString(f.Key() + "\n")
				_ = file.Close()
				return true
			}
		}
		return false
	}
	_, _ = file.WriteString(f.Key() + "\n")
	_ = file.Close()
	return true
}

// sumKey is the marker file name source: a short hash of the failure key.
//
// sumKey 是 marker 文件名的来源：失败键的短哈希。
func sumKey(f InfraFailure) []byte {
	sum := sha256.Sum256([]byte(f.Key()))
	return sum[:8]
}

// pruneInfraState drops session dirs older than infraStateMaxAge (bounded scan —
// at most 200 entries — so a pathological state dir cannot slow a hook spawn).
//
// pruneInfraState 清掉超过 infraStateMaxAge 的会话目录（有界扫描——至多 200 项——
// 病态状态目录拖不慢 hook 拉起）。
func pruneInfraState(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	if len(entries) > 200 {
		entries = entries[:200]
	}
	cutoff := time.Now().Add(-infraStateMaxAge)
	for _, e := range entries {
		info, err := e.Info()
		if err != nil || !info.IsDir() || info.ModTime().After(cutoff) || !infraBucketName(e.Name()) {
			continue
		}
		_ = os.RemoveAll(filepath.Join(root, e.Name()))
	}
}

// infraBucketName reports whether a dir name looks like one of our buckets
// (a sanitized session id or ppid-<n>) — pruning never touches anything else.
//
// infraBucketName 报告目录名是否像我们自己的桶（归一后的 session id 或 ppid-<n>）
// ——清理绝不碰其他任何东西。
func infraBucketName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// knownHookNames is the hook-name roster this binary can run: every name in
// ForgeHookSpec plus the batch entry point. ForgeHookSpec is the single source
// of truth for wiring; TestHookWiring_EveryNameResolvable (hookdispatch) pins
// that each spec name resolves at runtime, so spec-membership is the faithful
// proxy for "this binary knows the hook".
//
// knownHookNames 是本二进制能跑的 hook 名册：ForgeHookSpec 的全部名字加 batch
// 入口。ForgeHookSpec 是接线的单一真相源；TestHookWiring_EveryNameResolvable
// （hookdispatch）钉住每个 spec 名在运行时可解析，所以「在 spec 内」就是
// 「本二进制认识该 hook」的可靠代理。
func knownHookNames() map[string]bool {
	known := map[string]bool{"batch": true}
	for _, matchers := range ForgeHookSpec() {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				known[hookNameOf(h.Command)] = true
			}
		}
	}
	return known
}

// hookNameOf extracts the hook name from a `forge hook <name> ...` command.
//
// hookNameOf 从 `forge hook <name> ...` 命令里取 hook 名。
func hookNameOf(cmd string) string {
	fields := strings.Fields(strings.TrimPrefix(cmd, "forge hook "))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// UnknownWiredHooks lists the forge hook names wired in the settings file at
// path that this binary does not know (sorted, deduplicated). A missing or
// unparsable file yields nil — discovery is advisory and must never fail a run.
//
// UnknownWiredHooks 列出 path 处 settings 文件里接了、但本二进制不认识的 forge
// hook 名（排序、去重）。文件缺失或解析失败返回 nil——发现是 advisory，绝不能让
// 一次运行失败。
func UnknownWiredHooks(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil
	}
	known := knownHookNames()
	seen := map[string]bool{}
	var unknown []string
	for _, matchers := range cfg.Hooks {
		for _, m := range matchers {
			for _, h := range m.Hooks {
				if !strings.HasPrefix(h.Command, "forge hook ") {
					continue
				}
				name := hookNameOf(h.Command)
				if name == "" || known[name] || seen[name] {
					continue
				}
				seen[name] = true
				unknown = append(unknown, name)
			}
		}
	}
	slices.Sort(unknown)
	return unknown
}

// UserSettingsUnknownHooks is UnknownWiredHooks over the user-level Claude
// settings.json; empty when Claude home cannot be resolved.
//
// UserSettingsUnknownHooks 是对用户级 Claude settings.json 的 UnknownWiredHooks；
// Claude home 解析不出时为空。
func UserSettingsUnknownHooks() []string {
	home := ClaudeHome()
	if home == "" {
		return nil
	}
	return UnknownWiredHooks(filepath.Join(home, "settings.json"))
}

// UserSettingsNeedHeal reports whether autoSync must force a sync to heal version
// skew: the user-level settings wire hooks this binary does not know AND heal is
// possible (no forge plugin installed). With the plugin installed, autoSync's
// deferred dedupe already strips every user-level forge hook (unknown ones
// included), so no forced sync is needed — and gating on possibility keeps the
// fast no-op path intact there instead of forcing a full sync on every command.
//
// UserSettingsNeedHeal 报告 autoSync 是否须强制同步以自愈版本错位：用户级 settings
// 接了本二进制不认识的 hook，且自愈可行（未安装 forge 插件）。已装插件时 autoSync 的
// defer dedupe 会整体剥除用户级 forge hook（含未知条目），无需强制同步——以「可行」
// 为门也让该场景保持快速 no-op 路径，而不是每条命令都做一次全量同步。
func UserSettingsNeedHeal() bool {
	return len(UserSettingsUnknownHooks()) > 0 && !IsClaudePluginInstalled()
}

// HealUnknownUserHooks regenerates the user-level settings when they wire hooks
// this binary does not know, and returns the names it healed. GenerateUserSettings
// strips every forge-origin entry (user entries are preserved) and re-appends this
// binary's wiring, so the stale entries disappear and a second call is a no-op.
// Skipped when the forge plugin is installed (its hooks come from the plugin
// payload, not user settings — same gate as autoSync).
//
// HealUnknownUserHooks 在用户级 settings 接了本二进制不认识的 hook 时重生成
// settings，返回被自愈的名字。GenerateUserSettings 剥掉全部 forge 来源条目（用户
// 条目保留）再追加本二进制的接线，所以陈旧条目消失、再调一次是 no-op。forge 插件
// 已安装时跳过（其 hook 来自插件载荷而非用户 settings——与 autoSync 同门）。
func HealUnknownUserHooks() ([]string, error) {
	unknown := UserSettingsUnknownHooks()
	if len(unknown) == 0 || IsClaudePluginInstalled() {
		return nil, nil
	}
	if err := GenerateUserSettings(); err != nil {
		return nil, fmt.Errorf("heal unknown hooks %v: %w", unknown, err)
	}
	return unknown, nil
}
