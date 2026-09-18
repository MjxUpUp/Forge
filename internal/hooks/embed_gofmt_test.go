package hooks

// embed_gofmt_test.go —— auto-compile hook 的 gofmt 事中检查守卫（发布流程
// 排查 P2-1：5 个未格式化文件穿过开发期全链，直到 make premerge 才被拦——
// 格式检查只在最后两层，动作发生当下没有）。hook 已在每次源码 Write/Edit 的
// PostToolUse 触发，加单文件 gofmt -l 是毫秒级 advisory：把拦截点从发版前移到
// 敲代码的那一刻。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runAutoCompileScript 在 dir 里以 env 跑 AutoCompileHook 脚本本体，返回合并输出。
// 测试自带 gofmt（Go 工具链），脚本侧 command -v gofmt 守卫对非 Go 环境零影响。
func runAutoCompileScript(t *testing.T, dir, sessionID, filePath string) string {
	t.Helper()
	script := filepath.Join(t.TempDir(), "auto-compile.sh")
	if err := os.WriteFile(script, []byte(AutoCompileHook), 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, dir)
	cmd.Env = append(os.Environ(),
		"FORGE_FILE_PATH="+filePath,
		"FORGE_SESSION_ID="+sessionID,
		"FORGE_DATA_DIR="+filepath.Join(dir, ".forge-data"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("auto-compile hook must never fail (advisory), got %v:\n%s", err, out)
	}
	return string(out)
}

// touchSourceMarker 造 source-touched marker 打开 research-mode 抑制的对立分支。
func touchSourceMarker(t *testing.T, dir, sessionID string) {
	t.Helper()
	markerDir := filepath.Join(dir, ".forge-data", "markers")
	if err := os.MkdirAll(markerDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(markerDir, "forge-source-touched-"+sessionID), []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestAutoCompileHook_GofmtAdvisoryOnUnformatted pins the write-time format check:
// editing an unformatted .go file must surface a gofmt advisory naming the fix
// command; a formatted file must stay silent about format (no noise). Non-Go
// files never trigger it (stack-agnostic contract preserved).
//
// TestAutoCompileHook_GofmtAdvisoryOnUnformatted 钉住写入时刻的格式检查：编辑未
// 格格式的 .go 文件必须给出 gofmt 提醒（点名修复命令）；已格式化文件不得有格式
// 噪声；非 Go 文件永不触发（技术栈无关契约保持）。
func TestAutoCompileHook_GofmtAdvisoryOnUnformatted(t *testing.T) {
	dir := t.TempDir()
	const sid = "sess-gofmt-hook"

	unformatted := filepath.Join(dir, "ugly.go")
	// gofmt 会重排的对齐：struct 字段未对齐。
	if err := os.WriteFile(unformatted, []byte("package p\n\ntype S struct {\n\tA int\n\tBB string\n}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	touchSourceMarker(t, dir, sid)
	out := runAutoCompileScript(t, dir, sid, unformatted)
	if !strings.Contains(out, "gofmt") {
		t.Errorf("unformatted .go write must surface a gofmt advisory, got:\n%s", out)
	}

	formatted := filepath.Join(dir, "clean.go")
	if err := os.WriteFile(formatted, []byte("package p\n\nfunc F() int { return 1 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out = runAutoCompileScript(t, dir, sid, formatted)
	if strings.Contains(out, "gofmt") {
		t.Errorf("formatted file must not emit format noise, got:\n%s", out)
	}

	nonGo := filepath.Join(dir, "mod.ts")
	if err := os.WriteFile(nonGo, []byte("const x=1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	out = runAutoCompileScript(t, dir, sid, nonGo)
	if strings.Contains(out, "gofmt") {
		t.Errorf("non-Go file must never trigger gofmt (stack-agnostic), got:\n%s", out)
	}
}
