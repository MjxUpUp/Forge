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

// appendKimiStaleAdvisory detects whether the kimi-installed forge plugin lags behind the
// running forge binary and, if so, appends a remediation hint to detail. It reuses the
// init-suggest advisory channel (exit 0, non-blocking): the caller wraps detail into the
// hook output's AdditionalContext / stdout. CAVEAT: init-suggest is SessionStart, and on
// kimi 0.35.0 SessionStart stdout is observation-only (dropped), so this advisory is
// silently inert on kimi (the function has a single production caller, which sits inside
// the agent+hook-name kimi guard; the drift is still checklogged regardless). See
// internal/agentbridge/kimi-hook-routing.md.
//
// Throttled to once per day via kimiStaleMarker. Returns detail unchanged whenever the
// check does not apply: non-stale, dev build, unreadable install info, or already nudged
// today.
//
// appendKimiStaleAdvisory 检测 kimi 已装 forge plugin 是否落后于运行中的 forge 二进制，
// 若落后则把修复提示追加到 detail。复用 init-suggest 的 advisory 通道（exit 0，非阻断）：
// 调用方把 detail 包进 hook 输出的 AdditionalContext / stdout。注意：init-suggest 是
// SessionStart，而 kimi 0.35.0 的 SessionStart stdout 是 observation-only（丢弃），故此
// advisory 在 kimi 上静默失效（本函数唯一生产调用方在 agent+hook 名双重 guard 内，
// kimi-only；漂移无论如何都记 checklog）。见 internal/agentbridge/kimi-hook-routing.md。
//
// 经 kimiStaleMarker 按日节流（每日最多一次）。当检测不适用时原样返回 detail：
// 未过期、dev 构建、读不到安装信息、或今日已提醒。
func appendKimiStaleAdvisory(detail, fullForgeVersion string) string {
	dataHome, err := forgedata.GlobalHome()
	if err != nil {
		return detail
	}
	return appendKimiStaleAdvisoryAt(detail, fullForgeVersion, time.Now(), dataHome)
}

// appendKimiStaleAdvisoryAt is the testable core: now and dataHome are injected so the
// throttle and comparison can be asserted without touching the real clock or home.
//
// installed and current are both bare versions (v prefix trimmed) before compareVersions;
// getCurrentVersion may leave a v prefix from `git describe`, so trim it here too.
//
// appendKimiStaleAdvisoryAt 是可测核心：now 与 dataHome 注入，使节流与比对可脱离
// 真实时钟与 home 断言。
//
// installed 与 current 在 compareVersions 前均为裸版本（已 trim v）；getCurrentVersion
// 可能从 `git describe` 留下 v 前缀，故此处一并 trim。
func appendKimiStaleAdvisoryAt(detail, fullForgeVersion string, now time.Time, dataHome string) string {
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
	return detail + "\n" + msg
}
