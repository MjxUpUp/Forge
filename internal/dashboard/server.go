// Package dashboard renders Forge quality governance data into the local read-only Pulse web panel.
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

// Options controls dashboard service startup behavior.
//
// Options 控制 dashboard 服务启动行为。Roots 是全局范围（CLI 一律从项目注册表
// 填入）；Root 保留为单项目回退，供测试与直接库调用使用。
type Options struct {
	Root        string   // 单项目根（Roots 为空时用）
	Roots       []string // 全局视图：多项目根（非空时优先于 Root）
	Port        int      // 监听端口；0 = 系统分配临时端口
	OpenBrowser bool     // 是否自动打开浏览器
}

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

// pulseAsset 是 pulse 面板页面的嵌入路径（唯一页面资产——看板收敛到仅 Pulse 时
// 删除了质量看板 index.html 与接续看板 continuity.html）。
const pulseAsset = `assets/pulse.html`

// pulsePage 保存静态 pulse 面板字节，启动时读一次。页面是静态的，由前端 fetch
// /api/pulse/*.json，不走 html/template。资产缺失属于编译期 embed 配置错误，
// 直接 panic（Must 语义）。
var pulsePage = mustReadAsset(pulseAsset)

// mustReadAsset 读内嵌资产，失败即 panic（语义对齐 template.Must）。
func mustReadAsset(path string) []byte {
	b, err := assetsFS.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return b
}

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

// securityHeaders 包成 middleware，统一覆盖所有路由（含 favicon）。
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setSecureHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// isLocalhostHost 判 Host 头是否本机（去端口、去 IPv6 方括号）。防 DNS rebinding：
// 攻击者用 evil.com 解析到 127.0.0.1，浏览器带 Host: evil.com 读本地看板——非 localhost 拒。
// 空 Host（少数客户端不发）放行，避免误伤合法请求。
func isLocalhostHost(host string) bool {
	if len(host) == 0 {
		return true
	}
	h := host
	// net.SplitHostPort 正确剥 [::1]:8800 与 host:port 的端口；无端口形式（[::1]、::1、
	// localhost）报错则保留原值，再用 Trim 兜底剥 IPv6 方括号。旧版用 LastIndex（":"）去端口
	// 在 IPv6 上会切错（[::1] →"[::"），把合法回环误判为外域——code-review-gate 拦下的回归。
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		h = parsed
	}
	h = strings.Trim(h, `[]`)
	// 去尾点（localhost. FQDN）+ 小写：Host 头域名部分按 RFC 大小写不敏感。
	h = strings.ToLower(strings.TrimSuffix(h, `.`))
	switch h {
	case ``, `localhost`, `127.0.0.1`, `::1`:
		return true
	}
	return false
}

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

// newMux 构建看板路由。抽出便于 httptest 直接挂载（Serve 负责 listen+开浏览器）。
func newMux(opts Options) *http.ServeMux {
	mux := http.NewServeMux()
	// Pulse 面板页挂 /：静态 HTML，消费下方七个 /api/pulse/*.json 端点。单文件零依赖红线
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
	// 旧 /pulse 地址：质量/接续看板下线后面板迁到 /——永久重定向保旧链接可用。
	mux.HandleFunc(`/pulse`, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, `/`, http.StatusPermanentRedirect)
	})
	// favicon：浏览器自动请求，给 204 避免 console 404 噪声（看板无需图标资源）。
	mux.HandleFunc(`/favicon.ico`, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	// pulse 面板 JSON API（只读事件流 + skills 聚合）。挂在同一 mux 上，原样继承
	// Host 校验 / 安全头中间件。
	registerPulseRoutes(mux, opts)
	return mux
}

// Serve 启动本地 HTTP 看板服务，阻塞直至 ctx 取消（Ctrl+C）。绑定 localhost，
// 端口 0 时由系统分配临时端口。开浏览器失败仅打印 URL，不中断服务。
func Serve(ctx context.Context, opts Options) error {
	addr := fmt.Sprintf(`localhost:%d`, opts.Port)
	ln, err := net.Listen(`tcp`, addr)
	if err != nil {
		// 端口占用给可操作提示，而非裸 OS 文案——用户画像重视「什么都不懂的用户能用」。
		if isAddrInUse(err) {
			return fmt.Errorf(`端口 %d 已被占用——省略 --port 用系统临时端口，或 --port 指定一个空闲端口`, opts.Port)
		}
		return fmt.Errorf(`监听 %s 失败: %w`, addr, err)
	}
	url := `http://` + ln.Addr().String() + `/`

	if opts.OpenBrowser {
		if oerr := openBrowser(url); oerr != nil {
			// 非致命：打印 URL 让用户手动开。
			fmt.Fprintf(os.Stderr, "自动打开浏览器失败（%v），请手动访问：%s\n", oerr, url)
		}
	} else {
		fmt.Fprintf(os.Stderr, "看板地址：%s\n", url)
	}
	fmt.Fprintf(os.Stderr, "本地只读看板已启动，Ctrl+C 退出。\n")

	// 安全头 + Host 校验包整条 mux：所有响应统一带防御头，非本机 Host 拒（防 DNS rebinding）。
	srv := &http.Server{Handler: localhostOnly(securityHeaders(newMux(opts)))}
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case <-ctx.Done():
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
