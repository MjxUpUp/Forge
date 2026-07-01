package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/health"
)

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
	// 点0：x=pad（最左），y=pad（score=100 顶）
	if pts[0].X != 20 || pts[0].Y != 20 {
		t.Errorf("pts[0] = (%v,%v), want (20,20)", pts[0].X, pts[0].Y)
	}
	// 点1：x=w-pad（最右），y=h-pad（score=0 底）
	if pts[1].X != 580 || pts[1].Y != 180 {
		t.Errorf("pts[1] = (%v,%v), want (580,180)", pts[1].X, pts[1].Y)
	}

	// 单点居中。
	one := scoreLine([]act.Conclusion{{Score: 50}}, 600, 200, 20)
	if len(one) != 1 || one[0].X != 300 { // pad + innerW/2 = 20+280
		t.Errorf("single point X = %v, want 310 (居中)", one[0].X)
	}

	if scoreLine(nil, 600, 200, 20) != nil {
		t.Errorf("scoreLine(nil) should return nil")
	}
}

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

// TestAggregate_Populated 用真实 act.Append 写盘，再聚合——验证整条 LoadAll→Summarize→Charts 链路。
func TestAggregate_Populated(t *testing.T) {
	root := t.TempDir()
	base := time.Unix(1700000000, 0)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(act.Append(root, &act.Conclusion{
		TaskRef: "feat/a", Score: 92, Grade: "A", Strength: "Strong",
		Deterministic: 5, AgentClaim: 1, CompletedAt: base,
	}))
	must(act.Append(root, &act.Conclusion{
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
	// 最近在前：feat/b（晚一小时）排首。
	if len(d.Tasks) != 2 || d.Tasks[0].TaskRef != "feat/b" {
		t.Errorf("Tasks order = %v, want feat/b first", taskRefs(d.Tasks))
	}
	// 折线按时序 2 点。
	if len(d.Charts.ScoreLine) != 2 {
		t.Errorf("ScoreLine len = %d, want 2", len(d.Charts.ScoreLine))
	}
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

// TestRenderPage 渲染输出含关键标记（标题、≥2 样本时的折线、最近任务行）。
// 用 2 条结论让 ScoreLine 长度 ≥2，polyline 才会 emit——单点不画线（见 SingleSample）。
func TestRenderPage(t *testing.T) {
	root := t.TempDir()
	base := time.Now()
	for _, c := range []act.Conclusion{
		{TaskRef: "feat/a", Score: 80, Grade: "B", Strength: "Strong", CompletedAt: base},
		{TaskRef: "feat/b", Score: 70, Grade: "C", Strength: "Weak", CompletedAt: base.Add(time.Hour)},
	} {
		if err := act.Append(root, &c); err != nil {
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

// TestRenderPage_SingleSample 仅 1 个任务时不画 polyline（SVG 需 ≥2 点才可见），
// 改为显示"仅 1 个样本"提示——防新用户看到孤立圆点以为渲染坏了。
func TestRenderPage_SingleSample(t *testing.T) {
	root := t.TempDir()
	if err := act.Append(root, &act.Conclusion{
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

// TestServe_HTTP 起 httptest server，验证 / 返回看板页、/api/data.json 返回合法 JSON。
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
	body := make([]byte, 8192)
	n, _ := resp.Body.Read(body)
	if !strings.Contains(string(body[:n]), "Forge 质量看板") {
		t.Errorf("GET / body missing title")
	}

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

func taskRefs(cs []act.Conclusion) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.TaskRef
	}
	return out
}

// TestServe_GracefulShutdown 起真实 Serve（临时端口 + 不开浏览器），ctx 取消后必须
// 及时返回 nil，不得永久阻塞（覆盖 Shutdown→errCh 兜底超时路径，防"需二次 Ctrl+C"回归）。
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
