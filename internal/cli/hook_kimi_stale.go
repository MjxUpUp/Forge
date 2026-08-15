package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/agentbridge"
	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// kimiStaleMarker is the once-daily throttle marker under the forge data home. Its
// content is today's date (YYYY-MM-DD): if it already matches today, the stale advisory
// is suppressed for the rest of the day so every kimi session start does not re-nag.
//
// kimiStaleMarker 是 forge data home 下按日节流的 marker。内容为今日日期
// （YYYY-MM-DD）：若已与今日匹配，则本日剩余时间抑制 stale advisory，避免每个 kimi
// session 启动都重复唠叨。
const kimiStaleMarker = ".kimi-plugin-stale"

// kimiStaleRidesHook is the single routing predicate for the kimi plugin-stale advisory:
// it rides the resume-reinject hook (UserPromptSubmit) — the ONE stdout channel kimi
// 0.35.0 delivers to the model. Deliberately NOT init-suggest (the advisory's old
// SessionStart ride): that channel was triple-invisible in production (kimi drops
// SessionStart stdout, the noise gate drops the hook's PASS, nothing reached
// model/user/logs), and since SessionStart precedes the first UserPromptSubmit in every
// session, keeping both rides would let the inert append consume the once-daily marker
// before the visible channel fires. See internal/agentbridge/kimi-hook-routing.md.
//
// kimiStaleRidesHook 是 kimi plugin-stale advisory 的唯一路由谓词：搭载 resume-reinject
// hook（UserPromptSubmit）——kimi 0.35.0 唯一把 stdout 送达模型的通道。刻意不用
// init-suggest（advisory 旧的 SessionStart 通道）：该通道在生产三重不可见（kimi 丢
// SessionStart stdout、noise gate 丢该 hook 的 PASS、模型/用户/日志全无信号），且每个
// session 里 SessionStart 先于首个 UserPromptSubmit，两处都保留会让不可见追加先消耗
// 按日 marker、可见通道反而永不触发。见 internal/agentbridge/kimi-hook-routing.md。
func kimiStaleRidesHook(agent, hookName string) bool {
	return agent == "kimi" && hookName == "resume-reinject"
}

// prependKimiStaleAdvisory detects whether the kimi-installed forge plugin lags behind the
// running forge binary and, if so, prepends a remediation hint to detail. Its single
// production caller sits in runHook inside the agent+hook-name kimi guard and rides the
// resume-reinject hook (UserPromptSubmit) — the ONE stdout channel kimi 0.35.0 delivers
// to the model (next-prompt delivery). The advisory's previous ride, init-suggest
// (SessionStart), was triple-invisible in production (2026-08-15 audit): kimi drops
// SessionStart stdout, the checklog noise gate drops the hook's PASS, and the drift
// stayed silent to model/user/logs. The caller also records a kimi-plugin-stale warn
// checklog entry when this function fires, closing the log-visibility layer. See
// internal/agentbridge/kimi-hook-routing.md.
//
// PREPEND (code-review F2, 2026-08-15), not append: emitAgentOutput truncates detail to
// maxAdditionalContextLen (9500 runes) at the TAIL. A post-compaction resume-reinject
// handoff can approach that cap, and a tail-appended advisory would be silently cut off
// AFTER the once-daily marker was consumed and the checklog entry recorded — the exact
// "marker eaten, model never sees it" leak this channel fix exists to close. The head
// always survives truncation.
//
// Throttled to once per day via kimiStaleMarker. Returns detail unchanged whenever the
// check does not apply: non-stale, dev build, unreadable install info, or already nudged
// today.
//
// prependKimiStaleAdvisory 检测 kimi 已装 forge plugin 是否落后于运行中的 forge 二进制，
// 若落后则把修复提示**前置**到 detail。唯一生产调用方在 runHook 的 agent+hook 名 kimi
// 双重 guard 内，搭载 resume-reinject hook（UserPromptSubmit）——kimi 0.35.0 唯一把
// stdout 送达模型的通道（下一 prompt 送达）。旧通道 init-suggest（SessionStart）在
// 生产三重不可见（2026-08-15 审计）：kimi 丢 SessionStart stdout、checklog noise gate
// 丢该 hook 的 PASS、漂移对模型/用户/日志全程静默。本函数触发时调用方还会记一条
// kimi-plugin-stale warn checklog 条目，补上日志可见性层。见
// internal/agentbridge/kimi-hook-routing.md。
//
// 前置而非追加（code-review F2，2026-08-15）：emitAgentOutput 会在**尾部**把 detail
// 截到 maxAdditionalContextLen（9500 rune）。压缩后的 resume-reinject handoff 可逼近
// 该上限，尾接的 advisory 会在按日 marker 已消耗、checklog 条目已记录之后被静默截掉
// ——正是本次通道修复要关死的「marker 被吃、模型看不到」泄漏。头部永远存活于截断。
//
// 经 kimiStaleMarker 按日节流（每日最多一次）。当检测不适用时原样返回 detail：
// 未过期、dev 构建、读不到安装信息、或今日已提醒。
func prependKimiStaleAdvisory(detail, fullForgeVersion string) string {
	dataHome, err := forgedata.GlobalHome()
	if err != nil {
		return detail
	}
	return prependKimiStaleAdvisoryAt(detail, fullForgeVersion, time.Now(), dataHome)
}

// prependKimiStaleAdvisoryAt is the testable core: now and dataHome are injected so the
// throttle and comparison can be asserted without touching the real clock or home.
//
// installed and current are both bare versions (v prefix trimmed) before compareVersions;
// getCurrentVersion may leave a v prefix from `git describe`, so trim it here too.
//
// prependKimiStaleAdvisoryAt 是可测核心：now 与 dataHome 注入，使节流与比对可脱离
// 真实时钟与 home 断言。
//
// installed 与 current 在 compareVersions 前均为裸版本（已 trim v）；getCurrentVersion
// 可能从 `git describe` 留下 v 前缀，故此处一并 trim。
func prependKimiStaleAdvisoryAt(detail, fullForgeVersion string, now time.Time, dataHome string) string {
	installed, ok := agentbridge.KimiPluginStaleInfo()
	if !ok {
		return detail
	}
	current := strings.TrimPrefix(getCurrentVersion(fullForgeVersion), "v")
	if current == "dev" || current == "" {
		return detail
	}
	if compareVersions(installed, current) >= 0 {
		return detail
	}

	markerPath := filepath.Join(dataHome, kimiStaleMarker)
	today := now.Format("2006-01-02")
	if b, err := os.ReadFile(markerPath); err == nil && strings.TrimSpace(string(b)) == today {
		return detail
	}

	// Chinese advisory wrapped in a raw string (backticks) to avoid Windows input quote
	// corruption — ASCII " outer delimiters get converted to Chinese curly quotes and break
	// compilation (see memory windows-input-quote-corruption, and suggest.go for the norm).
	//
	// 中文 advisory 用 raw string（反引号）包裹，规避 Windows 输入引号腐蚀——ASCII " 外侧
	// 界定符被转中文弯引号会破坏编译（见 memory windows-input-quote-corruption，及
	// suggest.go 的规范）。
	msg := fmt.Sprintf(`[forge] 你的 kimi forge plugin 是 v%s，但当前 forge 二进制已是 v%s，plugin 落后了（缺 freeze-guard 等较新防护）。kimi-code 无插件自动更新能力，请在 kimi 对话里执行 /plugins remove forge，再 /plugins install https://github.com/MjxUpUp/Forge 更新。（每日最多提醒一次）`, installed, current)

	if err := os.MkdirAll(dataHome, 0755); err == nil {
		_ = os.WriteFile(markerPath, []byte(today), 0644)
	}

	if detail == "" {
		return msg
	}
	// Head placement (F2): emitAgentOutput truncates the TAIL at 9500 runes — the
	// advisory must sit before the (potentially long) handoff detail so it survives.
	//
	// 头部放置（F2）：emitAgentOutput 在 9500 rune 处截尾——advisory 必须放在（可能
	// 很长的）handoff detail 之前才能存活。
	return msg + "\n" + detail
}
