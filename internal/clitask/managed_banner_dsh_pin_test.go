package clitask

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestManagedBannerMatchesDshPluginMarker pins the cross-language contract: the forge-dsh plugin waits for this banner (MANAGED_BANNER in plugins/forge-dsh/lib/tools.js) before telling the user "Forge is active". If the banner's leading marker is renamed here, the plugin's welcome line silently stops appearing — so the two literals are pinned together.
//
// TestManagedBannerMatchesDshPluginMarker 钉住跨语言契约：forge-dsh 插件等到该横幅
// （tools.js 的 MANAGED_BANNER）才向用户宣告「Forge 已接管」。这里改了横幅起始标记而
// 插件没跟，欢迎语会静默不再出现——两处字面量在此一并钉死。
func TestManagedBannerMatchesDshPluginMarker(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "plugins", "forge-dsh", "lib", "tools.js"))
	if err != nil {
		t.Fatalf("read plugin tools.js: %v", err)
	}
	m := regexp.MustCompile(`export const MANAGED_BANNER = "([^"]+)"`).FindSubmatch(src)
	if m == nil {
		t.Fatal(`plugins/forge-dsh/lib/tools.js 缺 export const MANAGED_BANNER = "..."——欢迎语的接管判据丢失`)
	}
	if marker := string(m[1]); !strings.HasPrefix(managedSessionBanner, marker) {
		t.Fatalf("managedSessionBanner %q 不以插件的 MANAGED_BANNER %q 开头——forge-dsh 欢迎语将静默失效，两处须同步改", managedSessionBanner, marker)
	}
}
