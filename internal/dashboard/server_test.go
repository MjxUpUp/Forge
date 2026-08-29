package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestProjectName_VolumeRoot 盘根项目（如 E:\Forge）的父目录是盘根 E:\，filepath.Base(E:\)
// 在 Windows 返回单反斜杠（非空、非 .），旧 projectName 漏判会拼出 \/Forge。盘根本身无有意义的
// 父段，应回退只取末段 Forge。卷名语义仅 Windows 有（VolumeName 在 POSIX 返空），非 Windows 跳过。
func TestProjectName_VolumeRoot(t *testing.T) {
	if runtime.GOOS != `windows` {
		t.Skip(`盘根卷名语义仅 Windows 有`)
	}
	if got := projectName(`E:\Forge`); got != `Forge` {
		t.Fatalf(`盘根项目 E:\Forge 应回退末段 Forge，got %q（旧逻辑拼 \/Forge）`, got)
	}
	// 盘内非盘根路径仍拼末两段——确认不是凡带盘符都回退的过宽修复。
	if got := projectName(`D:\code\app`); got != `code/app` {
		t.Fatalf(`D:\code\app 应拼末两段 code/app，got %q`, got)
	}
}

// TestProjectName_NonVolumeRoot 非盘根路径仍拼末两段——防 isVolumeRoot 误判普通多级路径。
func TestProjectName_NonVolumeRoot(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, `myapp`)
	got := projectName(root)
	want := filepath.Base(parent) + `/` + `myapp`
	if got != want {
		t.Fatalf(`非盘根 %q 应拼末两段 %q，got %q`, root, want, got)
	}
}

// TestServe_HTTP 起 httptest server，验证 / 返回 pulse 面板页、旧 /pulse 地址重定向到 /、
// 未匹配路径 404。
func TestServe_HTTP(t *testing.T) {
	mux := newMux(Options{Root: t.TempDir()})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 页面端点
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Forge", "Pulse", "/api/pulse/feed.json"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("GET / body missing %q", want)
		}
	}

	// 旧 /pulse 重定向到 /（http.Get 自动跟随；断言最终落点）。
	respPulse, err := http.Get(srv.URL + "/pulse")
	if err != nil {
		t.Fatal(err)
	}
	defer respPulse.Body.Close()
	if respPulse.StatusCode != 200 || respPulse.Request.URL.Path != `/` {
		t.Errorf("GET /pulse → %s status %d, want redirect landing / 200", respPulse.Request.URL.Path, respPulse.StatusCode)
	}

	// 未匹配路径 → 404
	resp3, err := http.Get(srv.URL + "/nope")
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != 404 {
		t.Errorf("GET /nope status = %d, want 404", resp3.StatusCode)
	}
}

// TestServe_GracefulShutdown 起真实 Serve（临时端口 + 不开浏览器），ctx 取消后必须
// 及时返回 nil，不得永久阻塞（覆盖 Shutdown→errCh 兜底超时路径，防「需二次 Ctrl+C」回归）。
func TestServe_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Root: t.TempDir(), Port: 0, OpenBrowser: false})
	}()

	// 给 net.Listen 一点时间起监听，再发取消。
	time.Sleep(150 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve 返回 %v，ctx 取消应返回 nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve 在 ctx 取消后 3s 未返回——Shutdown→errCh 路径永久阻塞")
	}
}

// TestIsAddrInUse 跨平台端口占用判别：POSIX 与 Windows 消息都识别，非占用错误不误判。
// Windows 上 errors.Is(syscall.EADDRINUSE) 不成立（E2E 实测），靠字符串兜底。
func TestIsAddrInUse(t *testing.T) {
	if !isAddrInUse(errors.New("listen tcp 127.0.0.1:8799: bind: address already in use")) {
		t.Error("POSIX address-already-in-use 未识别")
	}
	if !isAddrInUse(errors.New("listen tcp 127.0.0.1:8799: bind: Only one usage of each socket address (protocol/network address/port) is normally permitted.")) {
		t.Error("Windows 端口占用消息未识别")
	}
	if isAddrInUse(errors.New("permission denied")) {
		t.Error("非端口占用错误不应识别为占用")
	}
}

// TestIsLocalhostHost 钉住 Host 校验：localhost/回环/IPv6/[::1]/空 放行，外域/局域网拒。
// 这是 DNS rebinding 防线——去端口、去 IPv6 方括号后判等。
func TestIsLocalhostHost(t *testing.T) {
	cases := []struct {
		host string
		want bool
	}{
		{`localhost`, true},
		{`localhost:8800`, true},
		{`127.0.0.1`, true},
		{`127.0.0.1:8800`, true},
		{`[::1]:8800`, true},
		{`[::1]`, true},      // IPv6 回环无端口（旧 LastIndex 误拒，BLOCK-1 回归）
		{`::1`, true},        // 裸 IPv6 回环
		{`localhost.`, true}, // 尾点 FQDN（搜索域补点）
		{`LOCALHOST`, true},  // Host 头大小写不敏感
		{``, true},           // 空 Host 放行（避免误伤不发 Host 的客户端）
		{`evil.com`, false},
		{`evil.com:8800`, false},
		{`192.168.1.1:8800`, false},
	}
	for _, c := range cases {
		if got := isLocalhostHost(c.host); got != c.want {
			t.Errorf(`isLocalhostHost(%q) = %v, want %v`, c.host, got, c.want)
		}
	}
}

// TestSecureHeaders 经完整 middleware 栈（Host 校验 + 安全头 + mux）请求，验证防御头就位。
// JSON API 带 middleware CSP（script-src 'none'——API 响应无 JS）；/ 处的 pulse 页为内联
// 脚本覆盖为 script-src 'unsafe-inline'，且仍不放行任何外部源。
func TestSecureHeaders(t *testing.T) {
	handler := localhostOnly(securityHeaders(newMux(Options{Root: t.TempDir()})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + `/favicon.ico`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for _, c := range []struct{ k, v string }{
		{`X-Frame-Options`, `DENY`},
		{`X-Content-Type-Options`, `nosniff`},
		{`Referrer-Policy`, `no-referrer`},
	} {
		if got := resp.Header.Get(c.k); got != c.v {
			t.Errorf(`header %s = %q, want %q`, c.k, got, c.v)
		}
	}
	// 非页面路由保持严格 CSP（script-src 'none'）。
	if csp := resp.Header.Get(`Content-Security-Policy`); !strings.Contains(csp, `script-src 'none'`) {
		t.Errorf(`CSP 缺 script-src 'none': %q`, csp)
	}

	respPage, err := http.Get(srv.URL + `/`)
	if err != nil {
		t.Fatal(err)
	}
	defer respPage.Body.Close()
	csp := respPage.Header.Get(`Content-Security-Policy`)
	if !strings.Contains(csp, `script-src 'unsafe-inline'`) {
		t.Errorf(`pulse 页 CSP 应放行内联脚本，got %q`, csp)
	}
	if strings.Contains(csp, `http:`) || strings.Contains(csp, `https:`) {
		t.Errorf(`CSP 不得放行外部源，got %q`, csp)
	}
}

// TestServe_Favicon /favicon.ico 返回 204，消除浏览器自动请求的 console 404 噪声。
func TestServe_Favicon(t *testing.T) {
	handler := localhostOnly(securityHeaders(newMux(Options{Root: t.TempDir()})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + `/favicon.ico`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf(`favicon status = %d, want 204`, resp.StatusCode)
	}
}

// TestWriteRendered_FailureGives500 钉死吞错修复：渲染函数失败时客户端必须拿到 500 +
// 中性文案——绝不能是 200 + 截断内容。
func TestWriteRendered_FailureGives500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRendered(rec, `/tmp/root`, `text/html; charset=utf-8`, `渲染看板页面失败`, func(out io.Writer) error {
		// 失败前的半截输出——不得漏进响应。
		_, _ = io.WriteString(out, `<html>truncated`)
		return errors.New(`template exec failed`)
	})
	res := rec.Result()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf(`render 失败应 500，got %d`, res.StatusCode)
	}
	body, _ := io.ReadAll(res.Body)
	if strings.Contains(string(body), `truncated`) {
		t.Errorf(`半截渲染输出不得泄露给客户端，body=%q`, string(body))
	}
	if !strings.Contains(string(body), `渲染看板页面失败`) {
		t.Errorf(`应回中性文案，body=%q`, string(body))
	}
}

// TestWriteRendered_SuccessWritesBody：渲染成功时设置 Content-Type 并完整写出 buffer 内容。
func TestWriteRendered_SuccessWritesBody(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRendered(rec, `/tmp/root`, `application/json`, `序列化失败`, func(out io.Writer) error {
		return json.NewEncoder(out).Encode(map[string]int{`a`: 1})
	})
	res := rec.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf(`渲染成功应 200，got %d`, res.StatusCode)
	}
	if ct := res.Header.Get(`Content-Type`); ct != `application/json` {
		t.Errorf(`Content-Type 应 application/json，got %q`, ct)
	}
	body, _ := io.ReadAll(res.Body)
	if !strings.Contains(string(body), `"a":1`) {
		t.Errorf(`body 应含完整渲染输出，got %q`, string(body))
	}
}
