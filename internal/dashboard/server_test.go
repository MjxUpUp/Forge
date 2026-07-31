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

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/health"
)

// TestProjectName_VolumeRoot tests volume-root projects (e.g. E:\Forge) whose parent is the drive
// root E:\. On Windows filepath.Base(E:\) returns a single backslash (non-empty, non-dot); the old
// projectName logic mis-judged and concatenated \/Forge. The volume root itself has no meaningful
// parent segment, so it should fall back to only the last segment Forge. Volume-name semantics are
// Windows-only (VolumeName returns empty on POSIX); non-Windows skip.
//
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
	// Intra-drive non-root paths still concatenate the last two segments — confirms this isn't an
	// over-broad fix that retreats on any path containing a drive letter.
	//
	// 盘内非盘根路径仍拼末两段——确认不是凡带盘符都回退的过宽修复。
	if got := projectName(`D:\code\app`); got != `code/app` {
		t.Fatalf(`D:\code\app 应拼末两段 code/app，got %q`, got)
	}
}

// TestProjectName_NonVolumeRoot ensures non-volume-root paths still concatenate the last two
// segments — guards against isVolumeRoot mis-judging ordinary multi-level paths.
//
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

// TestScoreLine pins the line geometry: score 100 maps to the top, 0 to the bottom, and the two
// points evenly span the inner width.
//
// TestScoreLine 钉住折线几何：score 100→顶、0→底，双点均匀占满内宽。
func TestScoreLine(t *testing.T) {
	cs := []act.Conclusion{
		{Score: 100, CompletedAt: time.Unix(1000, 0)},
		{Score: 0, CompletedAt: time.Unix(2000, 0)},
	}
	pts := scoreLine(cs, 600, 200, 20)
	if len(pts) != 2 {
		t.Fatalf("scoreLine len = %d, want 2", len(pts))
	}
	// Point 0: x=pad (leftmost), y=pad (score=100 top).
	//
	// 点0：x=pad（最左），y=pad（score=100 顶）
	if pts[0].X != 20 || pts[0].Y != 20 {
		t.Errorf("pts[0] = (%v,%v), want (20,20)", pts[0].X, pts[0].Y)
	}
	// Point 1: x=w-pad (rightmost), y=h-pad (score=0 bottom).
	//
	// 点1：x=w-pad（最右），y=h-pad（score=0 底）
	if pts[1].X != 580 || pts[1].Y != 180 {
		t.Errorf("pts[1] = (%v,%v), want (580,180)", pts[1].X, pts[1].Y)
	}

	// Single point centered.
	//
	// 单点居中。
	one := scoreLine([]act.Conclusion{{Score: 50}}, 600, 200, 20)
	if len(one) != 1 || one[0].X != 300 { // pad + innerW/2 = 20+280
		t.Errorf("single point X = %v, want 310 (居中)", one[0].X)
	}

	if scoreLine(nil, 600, 200, 20) != nil {
		t.Errorf("scoreLine(nil) should return nil")
	}
}

// TestBars pins the bar normalization: the largest bucket fills width 100, others scale by ratio.
//
// TestBars 钉住柱状归一化：最大档满宽 100，其余按占比。
func TestBars(t *testing.T) {
	got := bars(map[string]int{`A`: 3, `B`: 1}, []string{`A`, `B`, `C`, `D`, `F`})
	if len(got) != 5 {
		t.Fatalf("bars len = %d, want 5", len(got))
	}
	if got[0].Label != `A` || got[0].Count != 3 || got[0].WidthPct != 100 {
		t.Errorf("bar A = %+v, want count 3 / pct 100", got[0])
	}
	if got[1].Count != 1 || got[1].WidthPct < 33.3 || got[1].WidthPct > 33.4 {
		t.Errorf("bar B pct = %v, want ~33.33", got[1].WidthPct)
	}
	if got[2].Count != 0 || got[2].WidthPct != 0 {
		t.Errorf("bar C (absent) should be count 0 / pct 0, got %+v", got[2])
	}
}

// TestLowDimBars converts health's pre-sorted dimension frequencies to bars, with the first bar full-width.
//
// TestLowDimBars 按 health 已降序的频次转柱，首项满宽。
func TestLowDimBars(t *testing.T) {
	got := lowDimBars([]health.DimFreq{{Dimension: `dim1`, Count: 2}, {Dimension: `dim2`, Count: 1}})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].WidthPct != 100 || got[1].WidthPct != 50 {
		t.Errorf("widths = %v/%v, want 100/50", got[0].WidthPct, got[1].WidthPct)
	}
	if lowDimBars(nil) != nil {
		t.Errorf("lowDimBars(nil) should return nil")
	}
}

// TestScoreLine_Clamp ensures out-of-range scores clamp into [0,100]; otherwise line points overflow
// the viewBox and get clipped.
//
// TestScoreLine_Clamp 越界分数必须夹到 [0,100]，否则折线点溢出 viewBox 被裁。
func TestScoreLine_Clamp(t *testing.T) {
	pts := scoreLine([]act.Conclusion{
		{Score: 150, CompletedAt: time.Unix(1000, 0)},
		{Score: -20, CompletedAt: time.Unix(2000, 0)},
	}, 600, 200, 20)
	if pts[0].Y != 20 { // 150 clamp→100→顶
		t.Errorf("score 150 clamp 后 Y = %v, want 20", pts[0].Y)
	}
	if pts[1].Y != 180 { // -20 clamp→0→底
		t.Errorf("score -20 clamp 后 Y = %v, want 180", pts[1].Y)
	}
}

// TestAggregate_Populated writes to disk via real act.Append, then aggregates — verifies the entire
// LoadAll→Summarize→Charts chain end-to-end.
//
// TestAggregate_Populated 用真实 act.Append 写盘，再聚合——验证整条 LoadAll→Summarize→Charts 链路。
func TestAggregate_Populated(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(act.Append(p, &act.Conclusion{
		TaskRef: "feat/a", Score: 92, Grade: "A", Strength: "Strong",
		Deterministic: 5, AgentClaim: 1, CompletedAt: base,
	}))
	must(act.Append(p, &act.Conclusion{
		TaskRef: "feat/b", Score: 55, Grade: "F", Strength: "Weak",
		Deterministic: 0, AgentClaim: 3, RetrospectiveNudge: true,
		CompletedAt: base.Add(time.Hour),
	}))

	d, err := Aggregate(root, base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary.TotalTasks != 2 {
		t.Fatalf("TotalTasks = %d, want 2", d.Summary.TotalTasks)
	}
	if d.Summary.AvgScore != 73.5 { // (92+55)/2
		t.Errorf("AvgScore = %v, want 73.5", d.Summary.AvgScore)
	}
	if d.Summary.BlindSpotCount != 1 { // Weak 一条
		t.Errorf("BlindSpotCount = %d, want 1", d.Summary.BlindSpotCount)
	}
	// Most-recent first: feat/b (one hour later) ranks first.
	//
	// 最近在前：feat/b（晚一小时）排首。
	if len(d.Tasks) != 2 || d.Tasks[0].TaskRef != "feat/b" {
		t.Errorf("Tasks order = %v, want feat/b first", taskRefs(d.Tasks))
	}
	// Two points in chronological order on the line chart.
	//
	// 折线按时序 2 点。
	if len(d.Charts.ScoreLine) != 2 {
		t.Errorf("ScoreLine len = %d, want 2", len(d.Charts.ScoreLine))
	}
	// Grade bars: A/F each appear once.
	//
	// 等级柱 A/F 各 1。
	barBy := func(bars []Bar, label string) int {
		for _, b := range bars {
			if b.Label == label {
				return b.Count
			}
		}
		return -1
	}
	if barBy(d.Charts.GradeBars, "A") != 1 || barBy(d.Charts.GradeBars, "F") != 1 {
		t.Errorf("GradeBars A/F counts wrong: %+v", d.Charts.GradeBars)
	}
}

// TestAggregate_Empty ensures an empty .forge does not crash and returns a renderable zero-value Data.
//
// TestAggregate_Empty 空 .forge 不崩，给出可渲染的零值 Data。
func TestAggregate_Empty(t *testing.T) {
	d, err := Aggregate(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary.TotalTasks != 0 {
		t.Errorf("TotalTasks = %d, want 0", d.Summary.TotalTasks)
	}
	if d.Charts.ScoreLine != nil {
		t.Errorf("ScoreLine should be nil on empty")
	}
}

// TestRenderPage verifies the render output contains key markers (title, line chart when ≥2 samples,
// and the most-recent task row).
// Two conclusions are used so ScoreLine length is ≥2 and polyline is emitted — single-point input
// does not draw a line (see SingleSample).
//
// TestRenderPage 渲染输出含关键标记（标题、≥2 样本时的折线、最近任务行）。
// 用 2 条结论让 ScoreLine 长度 ≥2，polyline 才会 emit——单点不画线（见 SingleSample）。
func TestRenderPage(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	base := time.Now()
	for _, c := range []act.Conclusion{
		{TaskRef: "feat/a", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: base},
		{TaskRef: "feat/b", Score: 70, Grade: "C", Strength: "Weak", CompletedAt: base.Add(time.Hour)},
	} {
		if err := act.Append(p, &c); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Aggregate(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := RenderPage(&buf, d); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"Forge 质量看板", "<polyline", "feat/a", "feat/b", "证据盲区率"} {
		if !strings.Contains(out, want) {
			t.Errorf("render output missing %q", want)
		}
	}
}

// TestRenderPage_SingleSample does not emit a polyline when there is only 1 task (SVG needs ≥2 points
// to be visible); instead it shows a"only 1 sample"hint — prevents new users from seeing an
// isolated dot and assuming the render is broken.
//
// TestRenderPage_SingleSample 仅 1 个任务时不画 polyline（SVG 需 ≥2 点才可见），
// 改为显示「仅 1 个样本」提示——防新用户看到孤立圆点以为渲染坏了。
func TestRenderPage_SingleSample(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/solo", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	d, err := Aggregate(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := RenderPage(&buf, d); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "<polyline") {
		t.Errorf("单样本不应 emit polyline（不可见）")
	}
	if !strings.Contains(out, "仅 1 个样本") {
		t.Errorf("单样本应显示提示文本")
	}
}

// TestRenderPage_EmptyState routes empty data through the empty-state branch and emits no polyline.
//
// TestRenderPage_EmptyState 空数据走空态分支，不出 polyline。
func TestRenderPage_EmptyState(t *testing.T) {
	d, err := Aggregate(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := RenderPage(&buf, d); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "尚无完成任务结论") {
		t.Errorf("empty state text missing")
	}
	if strings.Contains(out, "<polyline") {
		t.Errorf("empty state should not emit polyline")
	}
}

// TestServe_HTTP starts an httptest server and verifies / returns the dashboard page and
// /api/data.json returns valid JSON.
//
// TestServe_HTTP 起 httptest server，验证 / 返回看板页、/api/data.json 返回合法 JSON。
func TestServe_HTTP(t *testing.T) {
	mux := newMux(Options{Root: t.TempDir()})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Page endpoint.
	//
	// 页面端点
	resp, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("GET / status = %d, want 200", resp.StatusCode)
	}
	body := make([]byte, 8192)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "Forge 质量看板") {
		t.Errorf("GET / body missing title")
	}

	// JSON endpoint.
	//
	// JSON 端点
	resp2, err := http.Get(srv.URL + "/api/data.json")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", resp2.Header.Get("Content-Type"))
	}
	var d Data
	if err := json.NewDecoder(resp2.Body).Decode(&d); err != nil {
		t.Fatalf("decode /api/data.json: %v", err)
	}
	if d.Summary.TotalTasks != 0 {
		t.Errorf("JSON TotalTasks = %d, want 0 on empty", d.Summary.TotalTasks)
	}

	// Unmatched path → 404.
	//
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

func taskRefs(rows []TaskRow) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.TaskRef
	}
	return out
}

// TestServe_GracefulShutdown starts a real Serve (ephemeral port, browser not opened); after ctx is
// cancelled it must return nil promptly and never block forever (covers the Shutdown→errCh fallback
// timeout path, guarding against the"need a second Ctrl+C"regression).
//
// TestServe_GracefulShutdown 起真实 Serve（临时端口 + 不开浏览器），ctx 取消后必须
// 及时返回 nil，不得永久阻塞（覆盖 Shutdown→errCh 兜底超时路径，防「需二次 Ctrl+C」回归）。
func TestServe_GracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, Options{Root: t.TempDir(), Port: 0, OpenBrowser: false})
	}()

	// Give net.Listen a moment to start before sending cancel.
	//
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

// TestIsAddrInUse checks cross-platform port-in-use detection: both POSIX and Windows messages
// are recognised, and non-port errors are not mis-judged.
// On Windows errors.Is(syscall.EADDRINUSE) does not hold (verified by E2E), so string fallback is used.
//
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

// TestIsLocalhostHost pins Host validation: localhost / loopback / IPv6 / [::1] / empty are allowed;
// foreign domains and LAN addresses are rejected.
// This is the DNS-rebinding defence — port and IPv6 brackets are stripped before equality check.
//
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

// TestSecureHeaders requests / through the full middleware stack (Host validation + security headers
// + mux) and verifies defensive headers are in place.
//
// TestSecureHeaders 经完整 middleware 栈（Host 校验 + 安全头 + mux）请求 /，验证防御头就位。
func TestSecureHeaders(t *testing.T) {
	handler := localhostOnly(securityHeaders(newMux(Options{Root: t.TempDir()})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + `/`)
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
	// CSP includes script-src 'none' (the dashboard has no JS).
	//
	// CSP 含 script-src 'none'（看板无 JS）。
	csp := resp.Header.Get(`Content-Security-Policy`)
	if !strings.Contains(csp, `script-src 'none'`) {
		t.Errorf(`CSP 缺 script-src 'none': %q`, csp)
	}
}

// TestServe_JSONNoSessionID ensures /api/data.json never contains SessionID — even when the
// Conclusion does, the toPublic projection strips it. A SessionID value containing the substring
//"session"is injected into the conclusion to verify the JSON endpoint does not leak it.
//
// TestServe_JSONNoSessionID /api/data.json 必须不含 SessionID——即便 Conclusion 里有，
// toPublic 投影剥掉它。结论里塞个含"session"字样的 SessionID 值，验证 JSON 端点不泄露。
func TestServe_JSONNoSessionID(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	if err := act.Append(p, &act.Conclusion{
		TaskRef:   `feat/a`,
		SessionID: `secret-session-xyz`, // 值本身含 session 字样，泄露则测试红
		Score:     90, Grade: `A`, Strength: `Strong`, CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	handler := localhostOnly(securityHeaders(newMux(Options{Root: root})))
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + `/api/data.json`)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	// Check whether the test-injected SessionID value leaks — its absence in JSON proves taskPublic
	// stripped the SessionID field.
	// We do NOT use naive"session"/"sessionid"substring matching: the Project field may legitimately
	// contain that substring (project name / test name — e.g. this test's t.TempDir() directory name
	// contains"SessionID"), so substring matching would false-positive.
	//
	// 检查测试植入的 SessionID 值是否泄露——JSON 不含它即证明 taskPublic 剥掉了 SessionID 字段。
	// 不用朴素"session"/"sessionid"子串：Project 字段合理地可能含该子串（项目名/测试名，
	// 如本测试的 t.TempDir() 目录名就含"SessionID"），子串匹配会误报。
	if strings.Contains(string(body), `secret-session-xyz`) {
		t.Errorf(`JSON 端点泄露 SessionID 值: %s`, body)
	}
}

// TestServe_Favicon returns 204 for /favicon.ico, eliminating the console 404 noise from the
// browser's automatic request.
//
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

// TestAggregateGlobal_MergesProjects aggregates across two projects: conclusions are merged, each
// carries its project name, and Summary is computed cross-project.
//
// TestAggregateGlobal_MergesProjects 跨两项目聚合：结论合并、各带项目名、Summary 跨项目统计。
func TestAggregateGlobal_MergesProjects(t *testing.T) {
	rootA, pA := forgedatatest.RealProject(t)
	rootB, pB := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(act.Append(pA, &act.Conclusion{
		TaskRef: "feat/a1", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: base,
	}))
	must(act.Append(pB, &act.Conclusion{
		TaskRef: "feat/b1", Score: 60, Grade: "D", Strength: "Weak", CompletedAt: base.Add(time.Hour),
	}))

	d, err := AggregateGlobal([]string{rootA, rootB}, base.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsGlobal || d.ProjectCount != 2 {
		t.Fatalf("IsGlobal=%v ProjectCount=%d, want true/2", d.IsGlobal, d.ProjectCount)
	}
	if d.Summary.TotalTasks != 2 {
		t.Fatalf("TotalTasks=%d, want 2（跨项目合并）", d.Summary.TotalTasks)
	}
	if d.Summary.AvgScore != 70 { // (80+60)/2
		t.Errorf("AvgScore=%v, want 70", d.Summary.AvgScore)
	}
	// Each row carries a project name (the last two segments,"parent/last").
	//
	// 每条带项目名（末两段「父/末段」）。
	wantProj := func(root string) string {
		return filepath.Base(filepath.Dir(root)) + `/` + filepath.Base(root)
	}
	projOf := map[string]string{}
	for _, r := range d.Tasks {
		projOf[r.TaskRef] = r.Project
	}
	if projOf["feat/a1"] != wantProj(rootA) || projOf["feat/b1"] != wantProj(rootB) {
		t.Errorf("项目归属错: got %v, want a=%q b=%q", projOf, wantProj(rootA), wantProj(rootB))
	}
}

// TestAggregateGlobal_ScoreLineChronological ensures that after cross-project merge the line chart
// is sorted by time, not by roots order.
// Regression: rootA's task is later in time but earlier in roots — without sorting the line would be
// drawn as [A(late), B(early)], reverse-chronological, defeating the dashboard's core「global quality
// trend over time」purpose. Single-project tests cannot reach this bug because LoadAll is already
// chronological per project.
//
// TestAggregateGlobal_ScoreLineChronological 跨项目合并后折线必须按时间序，不能按 roots 顺序。
// 回归：rootA 任务时间更晚但排在 roots 前面——不排序会让折线按 [A(晚), B(早)] 画，时间倒序，
// 违背「全局质量随时间走势」的看板核心诉求。单项目测试因 LoadAll 本就 chronological 踩不到此 bug。
func TestAggregateGlobal_ScoreLineChronological(t *testing.T) {
	rootA, pA := forgedatatest.RealProject(t)
	rootB, pB := forgedatatest.RealProject(t)
	base := time.Unix(1700000000, 0)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	// rootA: later in time (base+2h), high score (90); rootB: earlier (base), low score (30).
	// roots=[rootA, rootB]: the append order is the reverse of time order, specifically aimed at the
	// unsorted bug.
	//
	// rootA：时间更晚（base+2h）、高分（90）；rootB：更早（base）、低分（30）。
	// roots=[rootA, rootB]：append 顺序与时间顺序相反，专门撞未排序的 bug。
	must(act.Append(pA, &act.Conclusion{
		TaskRef: "feat/a1", Score: 90, Grade: "A", Strength: "Strong", CompletedAt: base.Add(2 * time.Hour),
	}))
	must(act.Append(pB, &act.Conclusion{
		TaskRef: "feat/b1", Score: 30, Grade: "F", Strength: "Weak", CompletedAt: base,
	}))
	d, err := AggregateGlobal([]string{rootA, rootB}, base.Add(3*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	pts := d.Charts.ScoreLine
	if len(pts) != 2 {
		t.Fatalf("ScoreLine len = %d, want 2", len(pts))
	}
	// Expected time order: pts[0]=rootB (earlier, 30), pts[1]=rootA (later, 90).
	// If the bug is unfixed (roots order, no sort), pts[0]=rootA (90) and this assertion fails.
	// Y = pad + (1-score/100)*innerH = 20 + (1-score/100)*160. 30 → 132, 90 → 36.
	//
	// 期望时间序：pts[0]=rootB（更早，30 分），pts[1]=rootA（更晚，90 分）。
	// 若 bug 未修（按 roots 顺序不排序），pts[0]=rootA（90 分），此断言失败。
	// Y = pad + (1-score/100)*innerH = 20 + (1-score/100)*160。30 分→132，90 分→36。
	if pts[0].Y != 132 {
		t.Errorf("pts[0].Y = %v, want 132（更早的 30 分任务应在首位）", pts[0].Y)
	}
	if pts[1].Y != 36 {
		t.Errorf("pts[1].Y = %v, want 36（更晚的 90 分任务应在第二位）", pts[1].Y)
	}
}

// TestAggregateGlobal_SkipsBadRoot ensures a project with no conclusions (no DataDir/act) is not
// fatal — others aggregate as usual.
//
// TestAggregateGlobal_SkipsBadRoot 某项目无结论（无 DataDir/act）不致命，其余照常聚合。
func TestAggregateGlobal_SkipsBadRoot(t *testing.T) {
	good, pGood := forgedatatest.RealProject(t)
	bad := t.TempDir() // 无 act 结论
	if err := act.Append(pGood, &act.Conclusion{
		TaskRef: "feat/x", Score: 90, Grade: "A", Strength: "Strong", CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	d, err := AggregateGlobal([]string{good, bad}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if d.Summary.TotalTasks != 1 {
		t.Errorf("TotalTasks=%d, want 1（bad 跳过）", d.Summary.TotalTasks)
	}
	if d.ProjectCount != 1 {
		t.Errorf("ProjectCount=%d, want 1（bad 不计入有效项目数，避免头标与表格不符）", d.ProjectCount)
	}
}

// TestRenderPage_Global ensures the global-view render contains the"global"title, project count,
// and the project list header.
//
// TestRenderPage_Global 全局视图渲染含「全局」标题、项目计数、项目列表头。
func TestRenderPage_Global(t *testing.T) {
	rootA, pA := forgedatatest.RealProject(t)
	rootB, pB := forgedatatest.RealProject(t)
	if err := act.Append(pA, &act.Conclusion{
		TaskRef: "feat/a", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := act.Append(pB, &act.Conclusion{
		TaskRef: "feat/b", Score: 70, Grade: "C", Strength: "Weak", CompletedAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	d, err := AggregateGlobal([]string{rootA, rootB}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := RenderPage(&buf, d); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"全局质量看板", "2 个项目", "<th>项目</th>"} {
		if !strings.Contains(out, want) {
			t.Errorf("全局渲染缺 %q", want)
		}
	}
}

// TestRenderPage_SingleProjectNoProjectColumn ensures the single-project view has no project column
// (avoid redundancy).
//
// TestRenderPage_SingleProjectNoProjectColumn 单项目视图不应有项目列（避免冗余）。
func TestRenderPage_SingleProjectNoProjectColumn(t *testing.T) {
	root, p := forgedatatest.RealProject(t)
	if err := act.Append(p, &act.Conclusion{
		TaskRef: "feat/a", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	d, err := Aggregate(root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	if err := RenderPage(&buf, d); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "<th>项目</th>") {
		t.Errorf("单项目视图不应有项目列")
	}
	if strings.Contains(out, "全局质量看板") {
		t.Errorf("单项目视图不应显示全局标题")
	}
}

// TestWriteRendered_FailureGives500 pins the error-swallow fix: when the render function fails,
// the client must get a 500 with the neutral message — never a 200 with truncated content.
//
// TestWriteRendered_FailureGives500 钉死吞错修复：渲染函数失败时客户端必须拿到 500 +
// 中性文案——绝不能是 200 + 截断内容。
func TestWriteRendered_FailureGives500(t *testing.T) {
	rec := httptest.NewRecorder()
	writeRendered(rec, `/tmp/root`, `text/html; charset=utf-8`, `渲染看板页面失败`, func(out io.Writer) error {
		// Partial output before failing — must NOT leak into the response.
		//
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

// TestWriteRendered_SuccessWritesBody: successful render sets the content type and copies the
// full buffered body.
//
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
