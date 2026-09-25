package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// versionedForgeCache 按版本记忆 buildVersionedForge 的产物：两个 e2e 测试共用 9.9.9
// 二进制，而一次 go build 要数秒——每次重建纯属浪费。首个调用方构建；并发调用方可能
// 重复构建一次，无害（两个二进制都有效；最后一次 Store 生效）。
var versionedForgeCache sync.Map // version string → bin path string

// buildVersionedForge 用 -ldflags 构建带固定版本的 forge 二进制。共享的 forgeBin 是
// 普通 go build（version "dev"），而 prependKimiStaleAdvisory 对 dev 构建抑制 advisory
// ——stale 路径只有带版本的二进制才能走通。结果按版本缓存于包运行期（见
// versionedForgeCache），且放进 TestMain 持有的 forgeBuildDir——不能放 t.TempDir()：
// 缓存比创建它的测试活得久，测试 cleanup 会让后续缓存调用方拿到悬空路径。
func buildVersionedForge(t *testing.T, version string) string {
	t.Helper()
	if v, ok := versionedForgeCache.Load(version); ok {
		return v.(string)
	}
	bin := filepath.Join(forgeBuildDir, "forge-versioned-"+version+exeSuffix())
	cmd := exec.Command("go", "build", "-ldflags", fmt.Sprintf("-X main.version=%s", version), "-o", bin, "./cmd/forge/")
	cmd.Dir = repoRoot()
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build versioned forge: %v\n%s", err, out)
	}
	versionedForgeCache.Store(version, bin)
	return bin
}

// exeSuffix mirrors upgrade_test.go's windows handling for binary names.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// kimiHook 以 kimi-home 隔离（伪造 plugins/installed.json）+ 指定 stdin 运行带版本的
// forge 二进制的 `forge hook <name> --agent kimi`——即真实 kimi 0.35 会话产生的子进程
// 形态。返回 (stdout, stderr, err)。PATH 前置版本化 bin 目录，使 resume-reinject thin
// wrapper 里的 `exec forge task resume --reinject` 解析到同一个带版本二进制。
func kimiHook(t *testing.T, bin, dir, kimiHome, hookName, stdinJSON string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(bin, "hook", hookName, "--agent", "kimi")
	cmd.Dir = dir
	cmd.Stdin = strings.NewReader(stdinJSON)
	tmp := t.TempDir()
	// TMP/TEMP 与 TMPDIR 并设：Windows 的 os.TempDir 读 TMP 再 TEMP、不读 TMPDIR——
	// 只设 TMPDIR 会让子进程在 Windows 上解析到真实用户 temp。
	cmd.Env = append(os.Environ(),
		"KIMI_CODE_HOME="+kimiHome,
		"TMPDIR="+tmp,
		"TMP="+tmp,
		"TEMP="+tmp,
		"PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// writeKimiInstalledJSON 植入伪造的 kimi plugins/installed.json，把 forge plugin 锁在
// 指定 tag——即 KimiPluginStaleInfo 读取的形态。
func writeKimiInstalledJSON(t *testing.T, kimiHome, tag string) {
	t.Helper()
	dir := filepath.Join(kimiHome, "plugins")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"plugins":[{"id":"forge","name":"forge","enabled":true,"github":{"ref":{"kind":"tag","value":%q}}}]}`, tag)
	if err := os.WriteFile(filepath.Join(dir, "installed.json"), []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestHook_KimiStaleAdvisory_RidesUserPromptSubmit pins the 2026-08-15 staleness
// channel fix end-to-end: the kimi plugin-stale advisory must ride
// resume-reinject (UserPromptSubmit.
//
// TestHook_KimiStaleAdvisory_RidesUserPromptSubmit 端到端钉死 2026-08-15 的 staleness
// 通道修复：kimi plugin 过期 advisory 必须搭载 resume-reinject（UserPromptSubmit——
// kimi 0.35.0 唯一把 stdout 送达模型的通道），而非 init-suggest（SessionStart——stdout
// 被 kimi 丢弃；旧通道三重不可见：plugin 漂移期间模型/用户/日志全静默）。两次调用的
// 顺序承重：init-suggest 先跑，必须既不打印 advisory 也不消耗按日 marker——否则不可见
// 的 SessionStart 通道会在可见通道触发前吃掉节流额度（正是「两处都保留」的重新接线会
// 复活的 bug 模式）。同时钉死第三层可见性：advisory 触发时记一条 kimi-plugin-stale
// warn checklog 条目。
func TestHook_KimiStaleAdvisory_RidesUserPromptSubmit(t *testing.T) {
	dir := freshProject(t)
	// e2e TestMain 可能与其他针对 forgeBin 的测试并行；本测试只用自己的带版本二进制。
	bin := buildVersionedForge(t, "9.9.9")
	kimiHome := t.TempDir()
	writeKimiInstalledJSON(t, kimiHome, "v1.30.0") // plugin lags binary 9.9.9 → stale

	upsStdin := `{"session_id":"sess-kimi-stale-1","hook_event_name":"UserPromptSubmit","cwd":` + jsonStr(dir) + `,"prompt":[{"type":"text","text":"继续"}]}`
	ssStdin := `{"session_id":"sess-kimi-stale-1","hook_event_name":"SessionStart","cwd":` + jsonStr(dir) + `}`

	// 1. init-suggest（SessionStart）先跑：advisory 的旧通道。它不得打印 stale
	// advisory——SessionStart stdout 被 kimi 丢弃，且此处追加还会在可见通道触发前
	// 消耗掉按日 marker。
	ssOut, _, err := kimiHook(t, bin, dir, kimiHome, "init-suggest", ssStdin)
	if err != nil {
		t.Fatalf("forge hook init-suggest --agent kimi: %v\n%s", err, ssOut)
	}
	if strings.Contains(ssOut, "/plugins install") {
		t.Errorf("init-suggest (SessionStart) must NOT carry the stale advisory anymore (channel moved to resume-reinject/UserPromptSubmit):\n%s", ssOut)
	}

	// 2. resume-reinject（UserPromptSubmit）：advisory 必须出现在 stdout——纯文本、
	// exit 0，即 kimi 下一 prompt 送达模型的通道。
	upsOut, upsErr, err := kimiHook(t, bin, dir, kimiHome, "resume-reinject", upsStdin)
	if err != nil {
		t.Fatalf("forge hook resume-reinject --agent kimi: %v\n%s", err, upsErr)
	}
	if !strings.Contains(upsOut, "plugin 落后") || !strings.Contains(upsOut, "/plugins install") {
		t.Errorf("resume-reinject stdout missing stale advisory (UserPromptSubmit is the one model-visible channel on kimi):\n%s", upsOut)
	}
	if strings.Contains(upsOut, `"decision"`) {
		t.Errorf("advisory ride must stay non-blocking allow output (no decision JSON):\n%s", upsOut)
	}

	// 3. 第三层可见性：触发的 advisory 必须在 DataDir checklog 留下 kimi-plugin-stale
	// warn 条目（否则 noise gate 丢掉该 hook 的 PASS；logDetail 是脚本原始 stdout，
	// 本就不含这里前置的 advisory）。
	dataDir := forgedata.DataDirFor(dir)
	logBody, rerr := os.ReadFile(filepath.Join(dataDir, "checklog.jsonl"))
	if rerr != nil {
		t.Fatalf("checklog not readable at DataDir/checklog.jsonl: %v", rerr)
	}
	if !strings.Contains(string(logBody), `"check":"kimi-plugin-stale"`) {
		t.Errorf("checklog missing kimi-plugin-stale entry:\n%s", logBody)
	}
	if !strings.Contains(string(logBody), `"level":"warn"`) {
		t.Errorf("kimi-plugin-stale entry must carry level warn (escape-hatch pattern: warn rides Level, Passed stays neutral):\n%s", logBody)
	}

	// 4. 按日节流：同 session 第二次 UserPromptSubmit 必须静默（marker 已被首次触发消耗）。
	ups2, _, err := kimiHook(t, bin, dir, kimiHome, "resume-reinject", upsStdin)
	if err != nil {
		t.Fatalf("second resume-reinject: %v\n%s", err, ups2)
	}
	if strings.Contains(ups2, "plugin 落后") {
		t.Errorf("once-daily throttle violated: advisory re-fired on the same day:\n%s", ups2)
	}
}

// jsonStr quotes s as a JSON string literal (small local helper to avoid importing
// encoding/json for one-liners).
func jsonStr(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// TestHook_KimiStaleAdvisory_SilentWhenCurrent guards the false-positive
// direction: when the installed plugin tag matches the binary version, the
// UserPromptSubmit ride must stay silent (no advisory noise on healthy installs)
// and no warn checklog entry may appear.
//
// TestHook_KimiStaleAdvisory_SilentWhenCurrent 守护假阳性方向：装着的 plugin tag 与
// 二进制版本一致时，UserPromptSubmit 通道必须静默（健康安装不出 advisory 噪声），
// 也不得出现 warn checklog 条目。
func TestHook_KimiStaleAdvisory_SilentWhenCurrent(t *testing.T) {
	dir := freshProject(t)
	bin := buildVersionedForge(t, "9.9.9")
	kimiHome := t.TempDir()
	writeKimiInstalledJSON(t, kimiHome, "v9.9.9") // matches binary → not stale

	upsStdin := `{"session_id":"sess-kimi-current","hook_event_name":"UserPromptSubmit","cwd":` + jsonStr(dir) + `,"prompt":[{"type":"text","text":"继续"}]}`
	out, stderr, err := kimiHook(t, bin, dir, kimiHome, "resume-reinject", upsStdin)
	if err != nil {
		t.Fatalf("forge hook resume-reinject --agent kimi: %v\n%s", err, stderr)
	}
	if strings.Contains(out, "plugin 落后") || strings.Contains(out, "/plugins install") {
		t.Errorf("advisory fired on a current install (false positive):\n%s", out)
	}

	dataDir := forgedata.DataDirFor(dir)
	if body, rerr := os.ReadFile(filepath.Join(dataDir, "checklog.jsonl")); rerr == nil {
		if strings.Contains(string(body), `"check":"kimi-plugin-stale"`) {
			t.Errorf("kimi-plugin-stale entry recorded on a current install:\n%s", body)
		}
	}
}
