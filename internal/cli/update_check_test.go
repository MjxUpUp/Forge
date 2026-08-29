package cli

// update_check.go 对应测试：负缓存（网络失败短 TTL 抑制）与启动路径查询短
// deadline。契约：
//   - 查询失败/超时 → 写一条 LatestVersion 为空、Channel 为本通道的负条目；
//   - 负条目在 updateCheckNegativeTTL 内视为「已检查」——不发网络请求（用
//     「网络恢复后也不重查」钉住）、不打印任何通知；
//   - 负条目过 TTL 后放行真实查询（正常通知、缓存被真实版本覆写）；
//   - 启动查询超 updateCheckQueryDeadline 即放弃（迟到的成功不阻塞 CLI）。
//
// update_check.go's corresponding tests: the negative cache (short-TTL
// suppression after a network failure) and the startup-path query deadline.
// Contract:
//   - query failure/timeout → a negative entry with empty LatestVersion and
//     this channel's tag is written;
//   - within updateCheckNegativeTTL a negative entry counts as "already
//     checked" — no network request (pinned by "even a recovered network is
//     not re-queried") and no notice;
//   - past the TTL the entry lets a real query through (normal notice, cache
//     overwritten with the real version);
//   - a startup query exceeding updateCheckQueryDeadline is abandoned (a late
//     success never holds the CLI hostage).

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// deadRegistryServer 返回一个「连接即被拒」的 registry URL：进程内 server 关闭后
// 端口不可达，连接立刻失败——比指向不存在的 IP 快且稳。
//
// deadRegistryServer returns a registry URL whose connection is refused
// outright: an in-process server closed leaves the port unreachable, failing
// the connect immediately — faster and more deterministic than a bogus IP.
func deadRegistryServer() string {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

// ageNegativeCache 把磁盘上的负缓存条目回溯到 TTL 之前（整条重写，CheckedAt 前移
// 31 分钟），用于验证过期后恢复查询。
//
// ageNegativeCache backdates the on-disk negative entry past its TTL (whole
// entry rewritten, CheckedAt moved 31 minutes back) to verify querying
// resumes after expiry.
func ageNegativeCache(t *testing.T, home string) {
	t.Helper()
	path := filepath.Join(home, updateCacheDir, updateCacheFile)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("负缓存文件须先存在: %v", err)
	}
	rewritten := `{"latest_version": "", "checked_at": "` +
		time.Now().Add(-31*time.Minute).UTC().Format(time.RFC3339) + `", "channel": "npm"}`
	if err := os.WriteFile(path, []byte(rewritten), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCheckForUpdateNegativeCacheSuppressesRetry 钉负缓存核心：失败后写空版本条目；
// TTL 内网络恢复也不重查（无通知）；条目过 TTL 后恢复查询并覆写缓存。
//
// TestCheckForUpdateNegativeCacheSuppressesRetry pins the negative cache core:
// a failure writes an empty-version entry; even a recovered network is not
// re-queried within the TTL (no notice); past the TTL querying resumes and the
// cache is overwritten.
func TestCheckForUpdateNegativeCacheSuppressesRetry(t *testing.T) {
	home := t.TempDir()
	setTestHome(t, home)
	forceChannel(t, installChannel{kind: channelNPM, pm: "npm"})

	// 1) 网络死亡 → 静默 + 负缓存落盘。
	//    Dead network → silence + negative entry on disk.
	t.Setenv("FORGE_NPM_REGISTRY", deadRegistryServer())
	cmd := &cobra.Command{Use: "status"}
	out := captureStderr(t, func() {
		checkForUpdate("1.39.1 (commit: abc, built: 2026-08-21)", cmd)
	})
	if out != "" {
		t.Fatalf("失败的检查必须静默，got %q", out)
	}
	cache, err := loadUpdateCache()
	if err != nil {
		t.Fatalf("负缓存必须落盘: %v", err)
	}
	if cache.LatestVersion != "" || cache.Channel != string(channelNPM) {
		t.Fatalf("负缓存条目应为 {空版本, npm}, got {%q, %q}", cache.LatestVersion, cache.Channel)
	}
	if cache.CheckedAt == "" {
		t.Fatal("负缓存必须记 CheckedAt=now")
	}

	// 2) 网络恢复 + 有新版本，但负缓存仍在 TTL 内 → 不得重查（无通知）。
	//    Network recovered + newer version exists, but the negative entry is
	//    still within TTL → no re-query (no notice).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"1.41.0"}`))
	}))
	defer srv.Close()
	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)
	out = captureStderr(t, func() {
		checkForUpdate("1.39.1 (commit: abc, built: 2026-08-21)", cmd)
	})
	if strings.Contains(out, "1.41.0") || strings.Contains(out, "可用") {
		t.Fatalf("TTL 内的负缓存必须抑制重查（不发网络请求、不通知），got %q", out)
	}

	// 3) 负条目过 TTL → 恢复查询：通知新版本，缓存被真实版本覆写。
	//    Negative entry past TTL → querying resumes: newer version notified,
	//    cache overwritten with the real version.
	ageNegativeCache(t, home)
	out = captureStderr(t, func() {
		checkForUpdate("1.39.1 (commit: abc, built: 2026-08-21)", cmd)
	})
	if !strings.Contains(out, "Forge 1.41.0 可用") {
		t.Fatalf("负缓存过期后应恢复查询并通知，got %q", out)
	}
	cache, err = loadUpdateCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache.LatestVersion != "1.41.0" {
		t.Fatalf("缓存应被真实版本覆写, got %q", cache.LatestVersion)
	}
}

// TestCheckForUpdateQueryDeadline 钉启动路径短 deadline：查询超过
// updateCheckQueryDeadline 即放弃（返回错误）并写负缓存；迟到的成功不改变结果。
//
// TestCheckForUpdateQueryDeadline pins the startup-path deadline: a query
// exceeding updateCheckQueryDeadline is abandoned (error) and the negative
// cache is written; a late success cannot change the outcome.
func TestCheckForUpdateQueryDeadline(t *testing.T) {
	setTestHome(t, t.TempDir())
	forceChannel(t, installChannel{kind: channelNPM, pm: "npm"})

	// 服务端比 deadline 慢得多：真实网络「慢而未死」的形态。
	// Server much slower than the deadline: the slow-but-alive network shape.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"1.41.0"}`))
	}))
	defer srv.Close()
	t.Setenv("FORGE_NPM_REGISTRY", srv.URL)

	origDeadline := updateCheckQueryDeadline
	updateCheckQueryDeadline = 100 * time.Millisecond
	t.Cleanup(func() { updateCheckQueryDeadline = origDeadline })

	start := time.Now()
	out := captureStderr(t, func() {
		checkForUpdate("1.39.1 (commit: abc, built: 2026-08-21)", &cobra.Command{Use: "status"})
	})
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("启动路径必须在短 deadline 内返回，took %v", elapsed)
	}
	if strings.Contains(out, "1.41.0") {
		t.Fatalf("超时的查询不得通知，got %q", out)
	}
	cache, err := loadUpdateCache()
	if err != nil {
		t.Fatalf("超时也必须写负缓存: %v", err)
	}
	if cache.LatestVersion != "" {
		t.Fatalf("超时路径写的是负条目（空版本）, got %q", cache.LatestVersion)
	}
}

// TestNegativeCacheShortTTLExpiry 钉 isExpired 的双 TTL：空版本条目 30 分钟内新鲜、
// 31 分钟过期；同时间的正条目仍按 24h 新鲜——负抑制不得外溢成正条目的提前过期。
//
// TestNegativeCacheShortTTLExpiry pins isExpired's dual TTL: an empty-version
// entry is fresh for 30 minutes and expires at 31; a positive entry of the
// same age stays fresh on the 24h clock — negative suppression must not leak
// into premature positive-entry expiry.
func TestNegativeCacheShortTTLExpiry(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	minutes31 := time.Now().Add(-31 * time.Minute).UTC().Format(time.RFC3339)
	cases := []struct {
		name    string
		cache   updateCache
		expired bool
	}{
		{"负条目·新鲜", updateCache{CheckedAt: now}, false},
		{"负条目·31分钟", updateCache{CheckedAt: minutes31}, true},
		{"正条目·31分钟仍按24h新鲜", updateCache{LatestVersion: "1.40.0", CheckedAt: minutes31}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cache.isExpired(); got != c.expired {
				t.Fatalf("isExpired() = %v, want %v", got, c.expired)
			}
		})
	}
}
