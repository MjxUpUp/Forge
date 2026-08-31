package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/registry"
)

// gcFixture 是 seedGcFixture 的布置结果：隔离 GlobalHome 下的一个已登记项目
// 数据目录 + 三个孤儿（空 / 过期非空 / 新鲜非空）。
type gcFixture struct {
	home        string
	projectsDir string
	regKey      string
	regDir      string
	emptyDir    string
	staleDir    string
	freshDir    string
}

// seedGcFixture 在逐测试隔离的 GlobalHome（本测试自设 FORGE_DATA_HOME——包级
// TestMain 的共享 home 里，先跑的其他测试会留下额外孤儿目录，让 dry-run 汇总
// 计数断言顺序依赖）下布置 gc 场景。过期孤儿的最新文件 mtime 拨回 20 天前；
// 新鲜孤儿保持当前 mtime。
func seedGcFixture(t *testing.T) *gcFixture {
	t.Helper()
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	home, err := forgedata.GlobalHome()
	if err != nil {
		t.Fatalf("GlobalHome: %v", err)
	}
	projectsDir := filepath.Join(home, "projects")
	for _, d := range []string{"o-empty", "o-stale", "o-fresh"} {
		if err := os.MkdirAll(filepath.Join(projectsDir, d), 0755); err != nil {
			t.Fatalf("MkdirAll %s: %v", d, err)
		}
	}
	// 已登记项目：真实临时路径 + registry.Add，其 key 数据目录必须永不被碰。
	regPath := t.TempDir()
	if err := registry.Add(regPath); err != nil {
		t.Fatalf("registry.Add: %v", err)
	}
	keys := registry.Keys()
	if len(keys) == 0 {
		t.Fatal("registry.Keys 应含刚登记的项目")
	}
	regKey := keys[0]
	regDir := filepath.Join(projectsDir, regKey)
	if err := os.MkdirAll(filepath.Join(regDir, "tasks"), 0755); err != nil {
		t.Fatalf("MkdirAll regDir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(regDir, "tasks", "keep.json"), []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed regDir file: %v", err)
	}

	// 过期孤儿：内容 mtime 拨回 20 天前。
	staleDir := filepath.Join(projectsDir, "o-stale")
	old := time.Now().Add(-20 * 24 * time.Hour)
	if err := os.MkdirAll(filepath.Join(staleDir, "tasks"), 0755); err != nil {
		t.Fatalf("seed staleDir: %v", err)
	}
	p := filepath.Join(staleDir, "tasks", "feat-x.json")
	if err := os.WriteFile(p, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed staleDir file: %v", err)
	}
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// 新鲜孤儿：默认 mtime = now。
	freshDir := filepath.Join(projectsDir, "o-fresh")
	if err := os.WriteFile(filepath.Join(freshDir, "checklog.jsonl"), []byte("{}\n"), 0644); err != nil {
		t.Fatalf("seed freshDir: %v", err)
	}
	return &gcFixture{
		home:        home,
		projectsDir: projectsDir,
		regKey:      regKey,
		regDir:      regDir,
		emptyDir:    filepath.Join(projectsDir, "o-empty"),
		staleDir:    staleDir,
		freshDir:    freshDir,
	}
}

// runRegistryGcCmd 以给定 flags 执行 gc 命令并返回输出。
func runRegistryGcCmd(t *testing.T, prune bool) string {
	t.Helper()
	var buf bytes.Buffer
	registryGcCmd.SetOut(&buf)
	if err := registryGcCmd.Flags().Set("prune", boolStr(prune)); err != nil {
		t.Fatalf("set prune flag: %v", err)
	}
	defer func() { _ = registryGcCmd.Flags().Set("prune", "false") }()
	if err := registryGcCmd.RunE(registryGcCmd, []string{}); err != nil {
		t.Fatalf("gc(prune=%v): %v", prune, err)
	}
	return buf.String()
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// TestRegistryGc_DryRunNoTouch dry-run（默认）只报告不改动：三个孤儿都被点名，
// 但目录原样保留。
func TestRegistryGc_DryRunNoTouch(t *testing.T) {
	fx := seedGcFixture(t)
	out := runRegistryGcCmd(t, false)
	for _, key := range []string{"o-empty", "o-stale", "o-fresh"} {
		if !strings.Contains(out, key) {
			t.Errorf("dry-run 输出应点名孤儿 %s，实得 %q", key, out)
		}
	}
	if !strings.Contains(out, "演练") {
		t.Errorf("dry-run 输出应标明这是演练模式，实得 %q", out)
	}
	// P2 回归（2026-08-31 review）：dry-run 汇总行必须报“计划处置数”而非恒 0——
	// fixture 里恰有一个空孤儿（计划删除）与一个过期孤儿（计划移入备份）。
	if !strings.Contains(out, "删除空目录 1，移入备份 1") {
		t.Errorf("dry-run 汇总应报告计划数（删除 1/备份 1），实得 %q", out)
	}
	for _, d := range []string{fx.regDir, fx.emptyDir, fx.staleDir, fx.freshDir} {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("dry-run 不得改动任何目录，%s 却消失了: %v", d, err)
		}
	}
}

// TestRegistryGc_PruneSemantics --prune 语义四条：空树删除、过期非空移入备份、
// 新鲜非空保留、已登记项目永不触碰；二次执行幂等。
func TestRegistryGc_PruneSemantics(t *testing.T) {
	fx := seedGcFixture(t)
	out := runRegistryGcCmd(t, true)

	// 空树：删除。
	if _, err := os.Stat(fx.emptyDir); !os.IsNotExist(err) {
		t.Errorf("空孤儿目录应被删除，stat 得 %v", err)
	}
	// 过期非空：移入备份，原位消失，备份里有内容。
	if _, err := os.Stat(fx.staleDir); !os.IsNotExist(err) {
		t.Errorf("过期孤儿应移入备份（原位消失），stat 得 %v", err)
	}
	backupRoot := filepath.Join(fx.home, "backups")
	matches, _ := filepath.Glob(filepath.Join(backupRoot, "gc-*", "o-stale", "tasks", "feat-x.json"))
	if len(matches) == 0 {
		t.Errorf("备份中应存在 o-stale/tasks/feat-x.json（backupRoot=%s），输出:\n%s", backupRoot, out)
	}
	// 新鲜非空：保留并被报告为跳过。
	if _, err := os.Stat(fx.freshDir); err != nil {
		t.Errorf("新鲜孤儿（14d 内有活动）应保留，stat 得 %v", err)
	}
	if !strings.Contains(out, "o-fresh") {
		t.Errorf("新鲜孤儿应被报告为跳过，实得 %q", out)
	}
	// 已登记：永不被碰、不出现在孤儿清单。
	if _, err := os.Stat(filepath.Join(fx.regDir, "tasks", "keep.json")); err != nil {
		t.Errorf("已登记项目的数据目录不得被 gc 触碰: %v", err)
	}
	if strings.Contains(out, fx.regKey) {
		t.Errorf("已登记项目 %s 不应出现在孤儿清单里", fx.regKey)
	}

	// 幂等：再跑一次无错误，新鲜孤儿仍在。
	runRegistryGcCmd(t, true)
	if _, err := os.Stat(fx.freshDir); err != nil {
		t.Errorf("二次执行后新鲜孤儿应仍在，stat 得 %v", err)
	}
}
