// Package dashboard renders Forge quality governance data into the local read-only
// Pulse web panel. A single command `forge dashboard` aggregates ALL registered
// projects (global by default) and serves one page: the event stream (task-start /
// gates / skill triggers / conclusions), per-task scores with evidence chains, and
// skills aggregation.
//
// Design principles: read-only, local-only, stdlib-only.
//   - Aggregation reuses the pure-function reads from act / checklog / taskpipeline /
//     health via feed.go / skillsview.go; never re-parses jsonl.
//   - Bind to localhost; never expose externally.
//   - Zero third-party dependencies (net/http + embed); single binary, no extra weight.
//
// Package dashboard 把 Forge 质量治理数据渲染成本地只读的 Pulse web 面板。一条命令
// `forge dashboard` 聚合全部已登记项目（默认全局），只服务一个页面：事件流
// （task-start / gate / skill 触发 / 结论）、任务评分与证据链、skills 聚合。
//
// 设计原则：纯只读、纯本地、纯 stdlib。
//   - 聚合经 feed.go / skillsview.go 复用 act / checklog / taskpipeline / health 的
//     纯函数读路径，不重新解析 jsonl；
//   - 服务绑定 localhost，绝不对外暴露；
//   - 零第三方依赖（net/http + embed），单二进制不增重。
package dashboard

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// Options controls dashboard service startup behavior. Roots is the global scope
// (the CLI always fills it from the project registry); Root remains as the
// single-project fallback for tests and direct library use.
//
// Options 控制 dashboard 服务启动行为。Roots 是全局范围（CLI 一律从项目注册表
// 填入）；Root 保留为单项目回退，供测试与直接库调用使用。
type Options struct {
	Root        string   // 单项目根（Roots 为空时用）
	Roots       []string // 全局视图：多项目根（非空时优先于 Root）
	Port        int      // 监听端口；0 = 系统分配临时端口
	OpenBrowser bool     // 是否自动打开浏览器
}

// projectName returns the project name (the last two segments, parent/leaf) used for
// task attribution across the panel. Taking only the leaf would collide for same-
// named projects (~/work/app vs ~/personal/app); the last two segments eliminate most
// collisions. Volume-root projects are an exception: for E:\Forge the parent directory
// E:\ is a volume root and filepath.Base(E:\) returns \ (not empty, not .), which the
// old logic mis-joined into \/Forge; a volume root has no meaningful parent segment,
// so we fall back to the leaf Forge.
//
// projectName 取项目名（末两段「父目录/末段」），用于面板各处的任务归属。
// 仅取末段会在同名项目（~/work/app 与 ~/personal/app）撞名无法区分；末两段消除绝大多数碰撞。
// 盘根项目例外：E:\Forge 的父目录 E:\ 是盘根，filepath.Base(E:\) 返"\"（非""/非"."），
// 旧判据漏判拼出"\/Forge"；盘根无有意义的父段，回退只取末段"Forge"。
func projectName(root string) string {
	dir := filepath.Dir(root)
	if isVolumeRoot(dir) {
		return filepath.Base(root) // 盘根项目无父段，回退末段
	}
	parent := filepath.Base(dir)
	if parent == `` || parent == `.` {
		return filepath.Base(root) // 根/单层目录无父可拼，回退末段
	}
	return parent + `/` + filepath.Base(root)
}

// isVolumeRoot reports whether dir is a volume root (no meaningful parent segment, so
// projectName should fall back to the leaf). A Windows volume root looks like E:\
// (VolumeName + separator) and filepath.Base returns \ for it; the POSIX root is /.
//
// isVolumeRoot 判 dir 是否盘根（无有意义的父段，projectName 应回退末段）。
// Windows 盘根形如"E:\"（VolumeName + 分隔符），filepath.Base 对它返"\"；POSIX 根"/"。
func isVolumeRoot(dir string) bool {
	if vol := filepath.VolumeName(dir); vol != `` {
		rest := strings.TrimPrefix(dir, vol)
		rest = strings.TrimPrefix(rest, `\`)
		rest = strings.TrimPrefix(rest, `/`)
		return rest == ``
	}
	return dir == `/` || dir == `\`
}

// pulseAsset is the embedded path of the pulse panel page (the only page asset —
// the quality board index.html and continuity.html were removed when the panel
// converged to Pulse-only).
//
// pulseAsset 是 pulse 面板页面的嵌入路径（唯一页面资产——看板收敛到仅 Pulse 时
// 删除了质量看板 index.html 与接续看板 continuity.html）。
const pulseAsset = `assets/pulse.html`

// pulsePage holds the static pulse panel bytes, read once at init. The page is static
// and fetches /api/pulse/*.json client-side; it is NOT an html/template. A missing
// asset is a compile-time embed misconfiguration, so panic directly (Must semantics).
//
// pulsePage 保存静态 pulse 面板字节，启动时读一次。页面是静态的，由前端 fetch
// /api/pulse/*.json，不走 html/template。资产缺失属于编译期 embed 配置错误，
// 直接 panic（Must 语义）。
var pulsePage = mustReadAsset(pulseAsset)

// mustReadAsset reads an embedded asset or panics (mirrors template.Must semantics).
//
// mustReadAsset 读内嵌资产，失败即 panic（语义对齐 template.Must）。
func mustReadAsset(path string) []byte {
	b, err := assetsFS.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

// setSecureHeaders sets basic security headers. The dashboard is a localhost read-only
// page; this is defense in depth: X-Frame-Options prevents click-jacking, nosniff
// prevents MIME sniffing, CSP restricts origins (inline styles are required by the
// page and there is no JS at the middleware level, hence script-src 'none' — the
// pulse route overrides it for its inline script), and Referrer-Policy keeps local
// paths from leaking to outbound links.
//
// setSecureHeaders 加基础安全头。看板是 localhost 只读页，纵深防御：X-Frame-Options
// 防点击劫持、nosniff 防 MIME 嗅探、CSP 限源（内联 style 是页面所需、middleware 层
// 无 JS 故 script-src none——pulse 路由为内联脚本覆盖它）、Referrer-Policy 不泄露本机路径到外链。
func setSecureHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set(`X-Frame-Options`, `DENY`)
	h.Set(`X-Content-Type-Options`, `nosniff`)
	h.Set(`Referrer-Policy`, `no-referrer`)
	h.Set(`Content-Security-Policy`, `default-src 'self'; style-src 'unsafe-inline'; img-src 'self' data:; script-src 'none'`)
}

// securityHeaders wraps the headers into middleware to uniformly cover every route (incl. favicon).
//
// securityHeaders 包成 middleware，统一覆盖所有路由（含 favicon）。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecureHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// isLocalhostHost reports whether the Host header points at the local machine (port and
// IPv6 brackets stripped). Defends against DNS rebinding: an attacker resolves evil.com to
// 127.0.0.1 and the browser sends Host: evil.com to read the local dashboard — reject
// anything that is not localhost. An empty Host (some clients omit it) is allowed, to avoid
// breaking legitimate requests.
//
// isLocalhostHost 判 Host 头是否本机（去端口、去 IPv6 方括号）。防 DNS rebinding：
// 攻击者用 evil.com 解析到 127.0.0.1，浏览器带 Host: evil.com 读本地看板——非 localhost 拒。
// 空 Host（少数客户端不发）放行，避免误伤合法请求。
func isLocalhostHost(host string) bool {
	if len(host) == 0 {
		return true
	}
	h := host
	// net.SplitHostPort correctly strips the port from [::1]:8800 and host:port; for forms
	// without a port ([::1], ::1, localhost) it errors and we keep the original value, then
	// fall back to Trim to strip IPv6 brackets. The old version used LastIndex(':') to strip
	// the port and mis-cut on IPv6 ([::1] -> [::), mistaking a legal loopback for an external
	// domain — a regression caught by code-review-gate.
	//
	// net.SplitHostPort 正确剥 [::1]:8800 与 host:port 的端口；无端口形式（[::1]、::1、
	// localhost）报错则保留原值，再用 Trim 兜底剥 IPv6 方括号。旧版用 LastIndex（":"）去端口
	// 在 IPv6 上会切错（[::1] →"[::"），把合法回环误判为外域——code-review-gate 拦下的回归。
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}
	h = strings.Trim(h, `[]`)
	// Strip trailing dot (localhost. FQDN) and lowercase: per RFC the domain part of the
	// Host header is case-insensitive.
	//
	// 去尾点（localhost. FQDN）+ 小写：Host 头域名部分按 RFC 大小写不敏感。
	h = strings.ToLower(strings.TrimSuffix(h, `.`))
	switch h {
	case ``, `localhost`, `127.0.0.1`, `::1`:
		return true
	}
	return false
}

// localhostOnly is the second line of defense against DNS rebinding (the first is net.Listen
// binding only to localhost, but the browser can still reach it via rebinding). A non-local
// Host returns 403.
//
// localhostOnly 是 DNS rebinding 第二道防线（第一道是 net.Listen 只绑 localhost，但浏览器
// 经 rebinding 仍可达）。非本机 Host 返回 403。
func localhostOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalhostHost(r.Host) {
			http.Error(w, `forbidden`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// writeRendered buffers the full render output before touching the ResponseWriter. Writing
// directly would flush a 200 header on the first byte, so a template/encode failure mid-render
// would leave the client with truncated HTML/JSON and no way to even report the error. Render
// into a buffer first; only on success set headers and copy the body; on failure log the real
// error and answer with a neutral 500 (same style as the aggregate error handling above).
//
// writeRendered 先把渲染结果完整落进 buffer 再碰 ResponseWriter。直接写会在第一个字节
// 刷出 200 header，模板/编码中途失败时客户端只能拿到截断的 HTML/JSON 且无法报错。
// 先渲染进 buffer；成功才写 header+body；失败记真实 error 并回中性 500（与上方
// aggregate 错误处理风格一致）。
func writeRendered(w http.ResponseWriter, root, contentType, neutralMsg string, render func(io.Writer) error) {
	var buf bytes.Buffer
	if err := render(&buf); err != nil {
		log.Printf(`dashboard render %s: %v`, root, err)
		http.Error(w, neutralMsg, http.StatusInternalServerError)
		return
	}
	w.Header().Set(`Content-Type`, contentType)
	if _, err := w.Write(buf.Bytes()); err != nil {
		log.Printf(`dashboard write %s: %v`, root, err)
	}
}

// newMux builds the dashboard routes. Extracted so httptest can mount it directly
// (Serve handles listen + browser launch).
//
// newMux 构建看板路由。抽出便于 httptest 直接挂载（Serve 负责 listen+开浏览器）。
func newMux(opts Options) *http.ServeMux {
	mux := http.NewServeMux()
	// Pulse panel page at /: static HTML consuming the six /api/pulse/*.json endpoints
	// below. The single-file zero-dependency red line requires inline <script>, so this
	// route overrides the middleware's CSP (script-src 'none') with script-src
	// 'unsafe-inline'; no external origins are added, all other directives stay identical.
	//
	// Pulse 面板页挂 /：静态 HTML，消费下方六个 /api/pulse/*.json 端点。单文件零依赖红线
	// 要求内联 <script>，故本路由覆盖 middleware 的 CSP（script-src 'none'）放行
	// script-src 'unsafe-inline'；不放行任何外部源，其余指令原样。
	mux.HandleFunc(`/`, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != `/` {
			http.NotFound(w, r)
			return
		}
		w.Header().Set(`Content-Security-Policy`, `default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; img-src 'self' data:`)
		writeRendered(w, opts.Root, `text/html; charset=utf-8`, `渲染 pulse 面板失败`, func(out io.Writer) error {
			_, err := out.Write(pulsePage)
			return err
		})
	})
	// Legacy /pulse URL: the panel moved to / when the quality/continuity boards were
	// removed — permanent redirect keeps old links working.
	//
	// 旧 /pulse 地址：质量/接续看板下线后面板迁到 /——永久重定向保旧链接可用。
	mux.HandleFunc(`/pulse`, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, `/`, http.StatusPermanentRedirect)
	})
	// favicon: browsers request it automatically; return 204 to avoid console 404 noise
	// (the dashboard ships no icon asset).
	//
	// favicon：浏览器自动请求，给 204 避免 console 404 噪声（看板无需图标资源）。
	mux.HandleFunc(`/favicon.ico`, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	// Pulse panel JSON APIs (read-only event stream + skills aggregation). They live on the
	// same mux so they inherit the Host validation / security headers middleware unchanged.
	//
	// pulse 面板 JSON API（只读事件流 + skills 聚合）。挂在同一 mux 上，原样继承
	// Host 校验 / 安全头中间件。
	registerPulseRoutes(mux, opts)
	return mux
}

// Serve starts the local HTTP dashboard server and blocks until ctx is canceled (Ctrl+C).
// It binds to localhost; when the port is 0 the system picks a free port. If opening the
// browser fails, it just prints the URL and does not stop the service.
//
// Serve 启动本地 HTTP 看板服务，阻塞直至 ctx 取消（Ctrl+C）。绑定 localhost，
// 端口 0 时由系统分配临时端口。开浏览器失败仅打印 URL，不中断服务。
func Serve(ctx context.Context, opts Options) error {
	addr := fmt.Sprintf(`localhost:%d`, opts.Port)
	ln, err := net.Listen(`tcp`, addr)
	if err != nil {
		// On port-in-use, give an actionable hint rather than a raw OS message — the user
		// persona cares about whether 'someone who knows nothing can still use it'.
		//
		// 端口占用给可操作提示，而非裸 OS 文案——用户画像重视「什么都不懂的用户能用」。
		if isAddrInUse(err) {
			return fmt.Errorf(`端口 %d 已被占用——省略 --port 用系统临时端口，或 --port 指定一个空闲端口`, opts.Port)
		}
		return fmt.Errorf(`监听 %s 失败: %w`, addr, err)
	}
	url := `http://` + ln.Addr().String() + `/`

	if opts.OpenBrowser {
		if oerr := openBrowser(url); oerr != nil {
			// Non-fatal: print the URL so the user can open it manually.
			//
			// 非致命：打印 URL 让用户手动开。
			fmt.Fprintf(os.Stderr, "自动打开浏览器失败（%v），请手动访问：%s\n", oerr, url)
		}
	} else {
		fmt.Fprintf(os.Stderr, "看板地址：%s\n", url)
	}
	fmt.Fprintf(os.Stderr, "本地只读看板已启动，Ctrl+C 退出。\n")

	// Wrap the entire mux with security headers + Host validation: every response carries
	// the defensive headers, and non-local Hosts are rejected (DNS-rebinding defense).
	//
	// 安全头 + Host 校验包整条 mux：所有响应统一带防御头，非本机 Host 拒（防 DNS rebinding）。
	srv := &http.Server{Handler: localhostOnly(securityHeaders(newMux(opts)))}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
		// Once Shutdown closes the listener, Serve must return ErrServerClosed; 3s is the upper
		// bound for active connections to wind down. The inner select is a timeout fallback so
		// that in extreme cases where Serve does not return in time, Serve() does not block
		// forever (forcing a second Ctrl+C).
		//
		// Shutdown 关 listener 后 Serve 必返回 ErrServerClosed；3s 是等活跃连接收尾的上限。
		// 内层 select 兜底超时，防极端情况下 Serve 未及时返回导致 Serve() 永久阻塞（需二次 Ctrl+C）。
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(shutCtx)
		cancel()
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

// isAddrInUse cross-platform detects port-in-use. errors.Is(syscall.EADDRINUSE) is reliable
// on POSIX, but Windows net.Listen does not return that errno (its bind-failure message is
// 'Only one usage of each socket address...'), so we add a string fallback — that message
// format is a stable contract of the Go net package.
//
// isAddrInUse 跨平台判别端口占用。errors.Is(syscall.EADDRINUSE) 在 POSIX 上可靠，
// 但 Windows 的 net.Listen 不返回该 errno（bind 失败消息为「Only one usage of each
// socket address...」），故辅以字符串兜底——该消息格式是 Go net 包的稳定契约。
func isAddrInUse(err error) bool {
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "address already in use") || strings.Contains(msg, "Only one usage of each socket address")
}

// openBrowser opens the default browser across platforms. When url carries a query, the
// Windows start command needs a title placeholder (an empty title, preventing & in the url
// from being treated by cmd as a command separator). After Start, Wait runs asynchronously
// to reclaim the child process handle — start / open / xdg-open are thin wrappers that
// usually fork the browser process and exit immediately, and skipping Wait would leak the
// os.Process.
//
// openBrowser 跨平台打开默认浏览器。url 含 query 时 Windows 的 start 需要 title 占位
// （空标题，防 url 含 & 被 cmd 当命令分隔符）。Start 后异步 Wait 回收子进程句柄——
// start/open/xdg-open 多为派生浏览器进程后即退出的薄包装，不 Wait 会泄漏 os.Process。
func openBrowser(url string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		c = exec.Command(`cmd`, `/c`, `start`, ``, url)
	case "darwin":
		c = exec.Command(`open`, url)
	default:
		c = exec.Command(`xdg-open`, url)
	}
	if err := c.Start(); err != nil {
		return err
	}
	go func() { _ = c.Wait() }()
	return nil
}
