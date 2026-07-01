// Package dashboard 把 Forge 的质量治理数据（act 结论 / health 项目趋势）渲染成
// 本地只读 web 看板。一条命令 `forge dashboard` 起本地 HTTP 服务 + 自动开浏览器，
// 让"项目质量现状"从 CLI 文本变成一眼可读的图形——分数走势、证据盲区率、复发低分维度。
//
// 设计原则：纯只读、纯本地、纯 stdlib。
//   - 复用 act.LoadAll + health.Summarize 的纯函数聚合，不重新解析 jsonl；
//   - 服务绑定 localhost，绝不对外暴露；
//   - 零第三方依赖（net/http + embed + html/template + 手绘 SVG），单二进制不增重。
//
// 看板是 forge status / health / trace / act 这一组只读观测命令的可视化 home：
// 它们各自把 .forge/ 聚合成文本，dashboard 把同一份聚合渲染成图形，数据源单一真相。
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/health"
)

// Options 控制 dashboard 服务启动行为。
type Options struct {
	Root        string // 项目根（.forge 所在）
	Port        int    // 监听端口；0 = 系统分配临时端口
	OpenBrowser bool   // 是否自动打开浏览器
}

// Data 是看板渲染所需的全部聚合数据。复用 act/health 的纯函数产出，dashboard
// 只负责组装成渲染友好的形状（含 SVG 几何预算——模板不做算术，只 emit 元素）。
type Data struct {
	Summary    health.Summary
	Tasks      []act.Conclusion // 最近任务，最近在前，最多 20 条
	ActiveTask string           // .forge/active-task-ref 内容（可能空）
	Charts     Charts
	Now        time.Time
}

// Charts 是模板直接消费的 SVG 几何（坐标/占比已算好）。
type Charts struct {
	ScoreLine    []Point // 分数走势折线点（viewBox 坐标）
	GradeBars    []Bar
	StrengthBars []Bar
	LowDimBars   []Bar
}

// Point 是 SVG 坐标系下的一个点（viewBox 单位）。
type Point struct {
	X float64
	Y float64
}

// Bar 是一行柱状：标签、计数、按最大值归一化的宽度百分比（0-100）。
type Bar struct {
	Label    string
	Count    int
	WidthPct float64
}

// Aggregate 从项目根读取并聚合看板数据。纯读，复用 act/health。now 用于渲染时间戳。
func Aggregate(root string, now time.Time) (Data, error) {
	cs, err := act.LoadAll(root)
	if err != nil {
		return Data{}, err
	}
	summary := health.Summarize(cs)

	// 最近任务倒序（最近在前），上限 20 条，避免长表拖慢渲染。
	recent := make([]act.Conclusion, len(cs))
	copy(recent, cs)
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].CompletedAt.After(recent[j].CompletedAt)
	})
	if len(recent) > 20 {
		recent = recent[:20]
	}

	return Data{
		Summary:    summary,
		Tasks:      recent,
		ActiveTask: readActiveTask(root),
		Now:        now,
		Charts: Charts{
			// 折线按时序（cs 已 chronological），柱状按固定档位顺序保证可读。
			ScoreLine:    scoreLine(cs, lineW, lineH, linePad),
			GradeBars:    bars(summary.GradeDist, []string{`A`, `B`, `C`, `D`, `F`}),
			StrengthBars: bars(summary.StrengthDist, []string{`Strong`, `Weak`, `Unverified`, `NoData`}),
			LowDimBars:   lowDimBars(summary.LowDims),
		},
	}, nil
}

// readActiveTask 读 .forge/active-task-ref，缺失/出错返回空串（非致命）。
func readActiveTask(root string) string {
	b, err := os.ReadFile(filepath.Join(root, `.forge`, `active-task-ref`))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// 折线 viewBox 常量（与 index.html 的 <svg viewBox> 对齐）。
const (
	lineW   = 600.0
	lineH   = 200.0
	linePad = 20.0
)

// scoreLine 把结论的（时间序, 分数）映射到 viewBox 坐标的折线点。纯函数。
// score 100→顶(pad)，0→底(h-pad)；单点居中。空切片返回 nil。
func scoreLine(cs []act.Conclusion, w, h, pad float64) []Point {
	if len(cs) == 0 {
		return nil
	}
	n := len(cs)
	innerW := w - 2*pad
	innerH := h - 2*pad
	pts := make([]Point, 0, n)
	for i, c := range cs {
		var x float64
		if n == 1 {
			x = pad + innerW/2
		} else {
			x = pad + float64(i)/float64(n-1)*innerW
		}
		// Score 约定 0-100，但 scoring 的 overall 不 clamp（维度加权和），且 jsonl 可能
		// 手动编辑——防御性夹到 [0,100]，否则越界分数让折线点溢出 viewBox 被裁（不可见）。
		s := c.Score
		if s < 0 {
			s = 0
		} else if s > 100 {
			s = 100
		}
		y := pad + (1-s/100)*innerH
		pts = append(pts, Point{X: x, Y: y})
	}
	return pts
}

// bars 把 map[label]count 按给定顺序渲染成柱，宽度按最大计数归一化（0-100）。
// 固定顺序保证等级/强度档位始终可读，而非按 map 随机迭代。
func bars(dist map[string]int, order []string) []Bar {
	maxN := 0
	for _, k := range order {
		if dist[k] > maxN {
			maxN = dist[k]
		}
	}
	out := make([]Bar, 0, len(order))
	for _, k := range order {
		n := dist[k]
		var pct float64
		if maxN > 0 {
			pct = float64(n) / float64(maxN) * 100
		}
		out = append(out, Bar{Label: k, Count: n, WidthPct: pct})
	}
	return out
}

// lowDimBars 把复发低分维度（health 已按频次降序）转成柱，宽度按最高频归一化。
func lowDimBars(dims []health.DimFreq) []Bar {
	if len(dims) == 0 {
		return nil
	}
	maxN := dims[0].Count // 已降序，首项最大
	out := make([]Bar, 0, len(dims))
	for _, d := range dims {
		var pct float64
		if maxN > 0 {
			pct = float64(d.Count) / float64(maxN) * 100
		}
		out = append(out, Bar{Label: d.Dimension, Count: d.Count, WidthPct: pct})
	}
	return out
}

// funcMap 提供模板所需的小算术/格式化（Go template 原生不能做浮点乘法）。
var funcMap = template.FuncMap{
	// mul100：0-1 比率 → 百分数数值（与模板里的 "%%" 配合）。
	"mul100": func(v float64) float64 { return v * 100 },
	// trendLabel：health.Trend 枚举 → 中文箭头。
	"trendLabel": func(t string) string {
		switch t {
		case `improving`:
			return `↑ 改善`
		case `regressing`:
			return `↓ 回退`
		case `stable`:
			return `→ 稳定`
		default:
			return `样本不足`
		}
	},
}

// assetFile 是嵌入资产文件路径（ParseFS 模式）。html/template 的 ParseFS 把无
// {{define}} 的文件注册成以路径为名的模板，路径前缀在不同环境下可能漂移，故
// index.html 内显式 {{define "page"}} 暴露稳定名，RenderPage 按 "page" 定位，
// 避免 "incomplete or undefined" 模板错误。
const assetFile = `assets/index.html`

// pageTmpl 在进程启动时解析内嵌模板一次。ParseFS 失败 = 资产缺失，属于编译期
// embed 配置错误，用 Must 直接 panic（与 skills/embed 同构）。
var pageTmpl = template.Must(template.New(`dashboard`).Funcs(funcMap).ParseFS(assetsFS, assetFile))

// RenderPage 把聚合数据渲染成 HTML 写入 w。导出便于 cli 层做 dry-run / 测试。
func RenderPage(w io.Writer, d Data) error {
	return pageTmpl.ExecuteTemplate(w, `page`, d)
}

// taskPublic 是结论的对外投影：剥掉 SessionID。HTML 看板用不到会话 ID，JSON 端点虽
// 只绑 localhost，也不把它序列化出去——纵深防御（配合 Host 校验防 DNS rebinding 读取）。
type taskPublic struct {
	TaskRef            string
	Score              float64
	Grade              string
	Strength           string
	Deterministic      int
	AgentClaim         int
	CompletedAt        time.Time
	RetrospectiveNudge bool
}

// publicData 是 /api/data.json 载荷：与 Data 同形但 Tasks 投影成 taskPublic（无 SessionID）。
type publicData struct {
	Summary    health.Summary
	Tasks      []taskPublic
	ActiveTask string
	Now        time.Time
}

// toPublic 投影 Data → 不含 SessionID 的 JSON 载荷。
func toPublic(d Data) publicData {
	tasks := make([]taskPublic, len(d.Tasks))
	for i, t := range d.Tasks {
		tasks[i] = taskPublic{
			TaskRef: t.TaskRef, Score: t.Score, Grade: t.Grade, Strength: t.Strength,
			Deterministic: t.Deterministic, AgentClaim: t.AgentClaim,
			CompletedAt: t.CompletedAt, RetrospectiveNudge: t.RetrospectiveNudge,
		}
	}
	return publicData{Summary: d.Summary, Tasks: tasks, ActiveTask: d.ActiveTask, Now: d.Now}
}

// setSecureHeaders 加基础安全头。看板是 localhost 只读页，纵深防御：X-Frame-Options
// 防点击劫持、nosniff 防 MIME 嗅探、CSP 限源（内联 style 是模板所需、无 JS 故
// script-src none）、Referrer-Policy 不泄露本机路径到外链。
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
	h := host
	if idx := strings.LastIndex(h, `:`); idx != -1 {
		h = h[:idx]
	}
	h = strings.Trim(h, `[]`)
	if len(h) == 0 {
		return true
	}
	switch h {
	case `localhost`, `127.0.0.1`, `::1`:
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

// newMux 构建看板路由。抽出便于 httptest 直接挂载（Serve 负责 listen+开浏览器）。
func newMux(opts Options) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(`/`, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != `/` {
			http.NotFound(w, r)
			return
		}
		data, err := Aggregate(opts.Root, time.Now())
		if err != nil {
			// 完整 err 记日志（本地 stderr，含路径便于排查），响应给中性文案——不向浏览器泄露 .forge 路径。
			log.Printf(`dashboard aggregate %s: %v`, opts.Root, err)
			http.Error(w, `聚合质量数据失败，请检查 .forge 数据完整性`, http.StatusInternalServerError)
			return
		}
		w.Header().Set(`Content-Type`, `text/html; charset=utf-8`)
		_ = RenderPage(w, data)
	})
	mux.HandleFunc(`/api/data.json`, func(w http.ResponseWriter, r *http.Request) {
		data, err := Aggregate(opts.Root, time.Now())
		if err != nil {
			log.Printf(`dashboard aggregate %s: %v`, opts.Root, err)
			http.Error(w, `聚合质量数据失败`, http.StatusInternalServerError)
			return
		}
		w.Header().Set(`Content-Type`, `application/json`)
		_ = json.NewEncoder(w).Encode(toPublic(data))
	})
	// favicon：浏览器自动请求，给 204 避免 console 404 噪声（看板无需图标资源）。
	mux.HandleFunc(`/favicon.ico`, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// Serve 启动本地 HTTP 看板服务，阻塞直至 ctx 取消（Ctrl+C）。绑定 localhost，
// 端口 0 时由系统分配临时端口。开浏览器失败仅打印 URL，不中断服务。
func Serve(ctx context.Context, opts Options) error {
	addr := fmt.Sprintf(`localhost:%d`, opts.Port)
	ln, err := net.Listen(`tcp`, addr)
	if err != nil {
		// 端口占用给可操作提示，而非裸 OS 文案——用户画像重视"什么都不懂的用户能用"。
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
// 但 Windows 的 net.Listen 不返回该 errno（bind 失败消息为 "Only one usage of each
// socket address..."），故辅以字符串兜底——该消息格式是 Go net 包的稳定契约。
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
