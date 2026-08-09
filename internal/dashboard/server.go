// Package dashboard renders Forge quality governance data (act conclusions / health
// project trends) into a local read-only web dashboard. A single command `forge dashboard`
// starts a local HTTP server and auto-opens the browser, turning project quality status
// from CLI text into a glanceable graphic — score trends, evidence blind-spot rate,
// recurring low-score dimensions.
//
// Design principles: read-only, local-only, stdlib-only.
//   - Reuse the pure-function aggregation from act.LoadAll + health.Summarize; never re-parse jsonl.
//   - Bind to localhost; never expose externally.
//   - Zero third-party dependencies (net/http + embed + html/template + hand-drawn SVG);
//     single binary, no extra weight.
//
// The dashboard is the visual home of the forge status / health / trace / act read-only
// observation commands: each aggregates .forge/ into text, and dashboard renders the same
// aggregation into graphics — single source of truth.
//
// Package dashboard 把 Forge 的质量治理数据（act 结论 / health 项目趋势）渲染成
// 本地只读 web 看板。一条命令 `forge dashboard` 起本地 HTTP 服务 + 自动开浏览器，
// 让「项目质量现状」从 CLI 文本变成一眼可读的图形——分数走势、证据盲区率、复发低分维度。
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
	"bytes"
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
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

// Options controls dashboard service startup behavior.
//
// Options 控制 dashboard 服务启动行为。
type Options struct {
	Root        string   // 单项目根（Roots 为空时用）
	Roots       []string // 全局视图：多项目根（非空时优先于 Root）
	Port        int      // 监听端口；0 = 系统分配临时端口
	OpenBrowser bool     // 是否自动打开浏览器
}

// aggregate picks single-project or global aggregation per Options:
// when Roots is non-empty, go global (cross-project merge).
//
// aggregate 按 Options 选择单项目或全局聚合：Roots 非空走全局（跨项目合并）。
func (o Options) aggregate(now time.Time) (Data, error) {
	if len(o.Roots) > 0 {
		return AggregateGlobal(o.Roots, now)
	}
	return Aggregate(o.Root, now)
}

// Data holds all aggregated data needed to render the dashboard. It reuses pure-function
// output from act/health; dashboard only reshapes it into a render-friendly form
// (including pre-computed SVG geometry — templates emit elements, never do arithmetic).
//
// Data 是看板渲染所需的全部聚合数据。复用 act/health 的纯函数产出，dashboard
// 只负责组装成渲染友好的形状（含 SVG 几何预算——模板不做算术，只 emit 元素）。
type Data struct {
	Summary      health.Summary
	Tasks        []TaskRow // 最近任务，最近在前，最多 20 条
	ActiveTask   string    // 单项目：DataDir/active-task-ref；全局：各项目 "项目:ref" 拼接
	Charts       Charts
	Now          time.Time
	IsGlobal     bool // 全局视图（聚合多项目）——模板据此切标题/项目列
	ProjectCount int  // 全局视图下的项目数（单项目 = 1）
}

// TaskRow is a single task conclusion with its project attribution. It embeds
// act.Conclusion so templates can still reach .TaskRef/.Score via Go field promotion;
// the global view additionally exposes .Project.
//
// TaskRow 是一条带项目归属的任务结论。内嵌 act.Conclusion 让模板仍能直接访问
// .TaskRef/.Score 等字段（Go 内嵌字段提升），全局视图额外暴露 .Project。
type TaskRow struct {
	act.Conclusion
	Project string
}

// Charts is the SVG geometry the template consumes directly
// (coordinates and ratios already pre-computed).
//
// Charts 是模板直接消费的 SVG 几何（坐标/占比已算好）。
type Charts struct {
	ScoreLine    []Point // 分数走势折线点（viewBox 坐标）
	GradeBars    []Bar
	StrengthBars []Bar
	LowDimBars   []Bar
}

// Point is a single point in the SVG coordinate system (viewBox units).
//
// Point 是 SVG 坐标系下的一个点（viewBox 单位）。
type Point struct {
	X float64
	Y float64
}

// Bar is one row of a bar chart: label, count, and width percentage (0-100)
// normalized against the maximum value.
//
// Bar 是一行柱状：标签、计数、按最大值归一化的宽度百分比（0-100）。
type Bar struct {
	Label    string
	Count    int
	WidthPct float64
}

// Aggregate reads and aggregates dashboard data from a project root. Pure read; reuses
// act/health. now is used for render timestamps.
//
// root is resolved to the user-level DataDir via forgedata.ProjectFor before reading act
// conclusions. When ProjectFor fails (non-git / not init), cs stays nil — empty data still
// renders fine (the TestAggregate_Empty path), so the dashboard never errors out in a
// non-forge directory.
//
// Aggregate 从项目根读取并聚合看板数据。纯读，复用 act/health。now 用于渲染时间戳。
//
// root 经 forgedata.ProjectFor 解析到用户级 DataDir 后再读 act 结论。ProjectFor 失败
// （非 git / 未 init）时 cs 保持 nil——空数据可正常渲染（TestAggregate_Empty 路径），
// 不让看板在非 forge 目录上报错。
func Aggregate(root string, now time.Time) (Data, error) {
	var cs []act.Conclusion
	if proj, err := forgedata.ProjectFor(root); err == nil {
		loaded, err := act.LoadAll(proj)
		if err != nil {
			return Data{}, err
		}
		cs = loaded
	}
	pn := projectName(root)
	rows := make([]TaskRow, len(cs))
	for i, c := range cs {
		rows[i] = TaskRow{Conclusion: c, Project: pn}
	}
	return buildData(rows, cs, readActiveTask(root), false, 1, now), nil
}

// AggregateGlobal aggregates across multiple project roots: it merges all conclusions
// (each carrying its project name) and runs Summarize over the whole merged slice.
// health.Summarize is a pure function over any []Conclusion, so cross-project needs no change.
// A single project failing to read is skipped (non-fatal); only projects with actual
// conclusions count toward ProjectCount (empty projects contribute no charts, and counting
// them would make the headline 'N projects' disagree with the table count).
// activeTask is composed as 'project: ref' for each project. Empty roots returns an empty
// Data (IsGlobal=true, ProjectCount=0, zero-value Summary).
//
// AggregateGlobal 跨多个项目根聚合：合并所有结论（各带项目名），Summarize 整个合并切片。
// health.Summarize 是吃任意 []Conclusion 的纯函数，跨项目零改动。单项目读失败跳过（不致命）；
// 仅实际有结论的项目计入 ProjectCount（空项目不贡献图表，计入会让头标「N 个项目」与表格项目数不符）。
// activeTask 拼成各项目「项目: ref」。roots 为空返回空数据 Data（IsGlobal=true，ProjectCount=0，Summary 零值）。
func AggregateGlobal(roots []string, now time.Time) (Data, error) {
	var allRows []TaskRow
	var allCs []act.Conclusion
	var actives []string
	valid := 0
	for _, r := range roots {
		proj, err := forgedata.ProjectFor(r)
		if err != nil {
			continue // 非 forge 项目（无 git/.forge）跳过——不致命
		}
		cs, err := act.LoadAll(proj)
		if err != nil {
			continue // 读失败（IO 级）不致命，跳过；注意文件不存在/行损坏 LoadAll 不报错，返空切片
		}
		pn := projectName(r)
		for _, c := range cs {
			allCs = append(allCs, c)
			allRows = append(allRows, TaskRow{Conclusion: c, Project: pn})
		}
		if len(cs) > 0 {
			valid++ // 仅实际有结论的项目计入头标项目数
		}
		if a := readActiveTask(r); a != "" {
			actives = append(actives, pn+`: `+a)
		}
	}
	return buildData(allRows, allCs, strings.Join(actives, `, `), true, valid, now), nil
}

// buildData is the shared assembly for Aggregate/AggregateGlobal: the conclusion slice
// feeds Summarize, rows takes the most recent 20, and charts use cs in time order
// (ScoreLine requires chronological input).
//
// buildData 是 Aggregate/AggregateGlobal 的共享组装：结论切片喂 Summarize，rows 取最近 20 条，
// charts 按时序用 cs（ScoreLine 需 chronological）。
func buildData(rows []TaskRow, cs []act.Conclusion, activeTask string, isGlobal bool, projectCount int, now time.Time) Data {
	// Sort the line by time: under the global view cs is a multi-project merge (appended in
	// roots order, not time order), so it must be stably sorted by CompletedAt before feeding
	// scoreLine (its X coordinates map index to evenly-spaced positions, i.e. index = time order).
	// For a single project cs is already chronological and stable sort does not change its order.
	// Summarize/recentRows are order-independent and unaffected.
	//
	// 折线按时间序：全局视图下 cs 是多项目合并（按 roots 顺序 append，非时间序），必须按
	// CompletedAt 稳定排序后再喂 scoreLine（其 X 坐标按索引等分映射，索引即时间序）。
	// 单项目 cs 本就 chronological，稳定排序不改变其顺序。Summarize/recentRows 顺序无关，不受影响。
	slices.SortStableFunc(cs, func(a, b act.Conclusion) int {
		return a.CompletedAt.Compare(b.CompletedAt)
	})
	summary := health.Summarize(cs)
	return Data{
		Summary:      summary,
		Tasks:        recentRows(rows),
		ActiveTask:   activeTask,
		IsGlobal:     isGlobal,
		ProjectCount: projectCount,
		Now:          now,
		Charts: Charts{
			// Line chart follows time order (cs is now chronological); bars use fixed
			// bucket order for readability.
			//
			// 折线按时序（cs 已 chronological），柱状按固定档位顺序保证可读。
			ScoreLine:    scoreLine(cs, lineW, lineH, linePad),
			GradeBars:    bars(summary.GradeDist, []string{`A`, `B`, `C`, `D`, `F`}),
			StrengthBars: bars(summary.StrengthDist, []string{`Strong`, `Weak`, `Unverified`, `NoData`}),
			LowDimBars:   lowDimBars(summary.LowDims),
		},
	}
}

// recentRows returns the top 20 in reverse order (most recent first),
// so a long list does not slow rendering down.
//
// recentRows 倒序（最近在前）取前 20 条，避免长表拖慢渲染。
func recentRows(rows []TaskRow) []TaskRow {
	recent := make([]TaskRow, len(rows))
	copy(recent, rows)
	slices.SortStableFunc(recent, func(a, b TaskRow) int {
		return b.CompletedAt.Compare(a.CompletedAt)
	})
	if len(recent) > 20 {
		recent = recent[:20]
	}
	return recent
}

// projectName returns the project name (the last two segments, parent/leaf) used for the
// task-attribution column in the global view. Taking only the leaf would collide for same-
// named projects (~/work/app vs ~/personal/app); the last two segments eliminate most collisions.
// Volume-root projects are an exception: for E:\Forge the parent directory E:\ is a volume root
// and filepath.Base(E:\) returns \ (not empty, not .), which the old logic mis-joined into
// \/Forge; a volume root has no meaningful parent segment, so we fall back to the leaf Forge.
//
// projectName 取项目名（末两段「父目录/末段」），用于全局视图的任务归属列。
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

// readActiveTask reads DataDir/active-task-ref (via taskpipeline.ReadActiveTaskRef,
// relocated to the user-level DataDir by refactor-data-home). Missing or erroring returns
// an empty string (non-fatal).
//
// readActiveTask 读 DataDir/active-task-ref（经 taskpipeline.ReadActiveTaskRef，
// 已随 refactor-data-home 迁到用户级 DataDir），缺失/出错返回空串（非致命）。
func readActiveTask(root string) string {
	return taskpipeline.ReadActiveTaskRef(root, "")
}

// Line-chart viewBox constants (kept in sync with the <svg viewBox> in index.html).
//
// 折线 viewBox 常量（与 index.html 的 <svg viewBox> 对齐）。
const (
	lineW   = 600.0
	lineH   = 200.0
	linePad = 20.0
)

// scoreLine maps conclusions' (time order, score) to viewBox coordinate line points.
// Pure function. Score 100 -> top (pad), 0 -> bottom (h-pad); a single point is centered.
// An empty slice returns nil.
//
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
		// Score is contractually 0-100, but scoring's overall is not clamped (it is a
		// weighted sum of dimensions), and jsonl may have been hand-edited — clamp
		// defensively to [0,100], otherwise an out-of-range score makes the line point
		// overflow the viewBox and get clipped (invisible).
		//
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

// bars renders map[label]count as bars in the given order, with widths normalized by the
// maximum count (0-100). Fixed order keeps grade/strength buckets readable instead of
// iterating the map in random order.
//
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

// lowDimBars turns recurring low-score dimensions (already frequency-sorted descending
// by health) into bars, with widths normalized by the highest frequency.
//
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

// funcMap provides the small arithmetic / formatting helpers the template needs
// (Go templates cannot do floating-point multiplication natively).
//
// funcMap 提供模板所需的小算术/格式化（Go template 原生不能做浮点乘法）。
var funcMap = template.FuncMap{
	// mul100: 0-1 ratio -> percentage value (paired with %% in the template).
	//
	// mul100：0-1 比率 → 百分数数值（与模板里的"%%"配合）。
	"mul100": func(v float64) float64 { return v * 100 },
	// trendLabel: health.Trend enum -> Chinese arrow.
	//
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

// assetFile is the embedded asset file path (ParseFS pattern). html/template's ParseFS
// registers files without {{define}} as templates named by their path, and the path prefix
// can drift across environments — so index.html explicitly uses {{define "page"}} to expose
// a stable name, and RenderPage locates it via page to avoid 'incomplete or undefined'
// template errors.
//
// assetFile 是嵌入资产文件路径（ParseFS 模式）。html/template 的 ParseFS 把无
// {{define}} 的文件注册成以路径为名的模板，路径前缀在不同环境下可能漂移，故
// index.html 内显式 {{define "page"}} 暴露稳定名，RenderPage 按"page"定位，
// 避免"incomplete or undefined"模板错误。
const assetFile = `assets/index.html`

// pageTmpl parses the embedded template once at process start. ParseFS failure means an
// asset is missing — a compile-time embed configuration error — so we panic directly via
// Must (mirroring skills/embed).
//
// pageTmpl 在进程启动时解析内嵌模板一次。ParseFS 失败 = 资产缺失，属于编译期
// embed 配置错误，用 Must 直接 panic（与 skills/embed 同构）。
var pageTmpl = template.Must(template.New(`dashboard`).Funcs(funcMap).ParseFS(assetsFS, assetFile))

// RenderPage renders aggregated data as HTML to w. Exported so the cli layer can dry-run / test it.
//
// RenderPage 把聚合数据渲染成 HTML 写入 w。导出便于 cli 层做 dry-run / 测试。
func RenderPage(w io.Writer, d Data) error {
	return pageTmpl.ExecuteTemplate(w, `page`, d)
}

// ============== continuity dashboard section ==============
// Task continuity dashboard: visualizes plan / decisions / blockers / participating tools
// for in-flight tasks. Complements the quality dashboard (index.html, reading finished
// conclusions via act.LoadAll) — the continuity dashboard reads TaskState (in-flight plus
// continuity fields) so you can see at a glance which tasks are running, where they are
// stuck, and who is participating.
// ============== end of continuity section ==============
//
// ============== 任务接续看板段 ==============
// 任务接续看板（continuity）：进行中任务的 plan/决策/阻塞/参与工具可视化。
// 与质量看板（index.html，读已完成结论 act.LoadAll）互补——接续看板读 TaskState
// （进行中 + 接续字段），让「哪些任务在跑、卡在哪、谁参与」一眼可见。
// ============== 任务接续看板段 结束 ==============

// continuityCard is the dashboard projection of a single task (shared by HTML and JSON;
// carries no sensitive fields like SessionID).
//
// continuityCard 是单个 task 的看板投影（HTML + JSON 共用，无 SessionID 等敏感字段）。
type continuityCard struct {
	TaskRef       string    `json:"task_ref"`
	Branch        string    `json:"branch"`
	Kind          string    `json:"kind"`
	Summary       string    `json:"summary,omitempty"`
	Goal          string    `json:"goal,omitempty"`
	OriginTool    string    `json:"origin_tool,omitempty"`
	SessionTools  []string  `json:"session_tools,omitempty"`
	IsComplete    bool      `json:"is_complete"`
	CurrentGate   string    `json:"current_gate,omitempty"`
	GatePassed    int       `json:"gate_passed"`
	GateTotal     int       `json:"gate_total"`
	OpenBlockers  int       `json:"open_blockers"`
	NextSteps     int       `json:"next_steps"`
	Decisions     int       `json:"decisions"`
	Findings      int       `json:"findings"`
	ParentTaskRef string    `json:"parent_task_ref,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	// IsZombie marks a delegation that has stalled (offered>7d / claimed>TTL /
	// input-required>7d / abandoned_count≥2) — rendered yellow on the board (design §12 标黄).
	// Computed in AggregateContinuity (which has root for the checklog-activity judgment) via the
	// shared taskpipeline.IsZombie, so the board, `task mine`, and `task health` never disagree.
	//
	// IsZombie 标记停滞的分派（offered>7d / claimed>TTL / input-required>7d /
	// abandoned_count≥2）——看板渲染黄色（设计 §12 标黄）。在 AggregateContinuity（持有 root 供
	// checklog 活动判断）经共享 taskpipeline.IsZombie 计算，使看板、task mine、task health 永不分歧。
	IsZombie     bool   `json:"is_zombie,omitempty"`
	ZombieReason string `json:"zombie_reason,omitempty"`
}

// ContinuityBoard is the continuity-dashboard payload: cards for every task in the project
// plus in-progress / completed counts.
//
// ContinuityBoard 是接续看板载荷：项目内所有 task 的卡片 + 进行中/已完成计数。
type ContinuityBoard struct {
	Project    string           `json:"project,omitempty"`
	Now        time.Time        `json:"now"`
	Cards      []continuityCard `json:"cards"`
	Incomplete int              `json:"incomplete"`
	Complete   int              `json:"complete"`
}

// AggregateContinuity reads all TaskStates from DataDir/tasks/ and projects them into
// dashboard cards. In-progress first; within the same state, sorted by start time descending
// — the most recent unfinished task lands on top, matching 'dashboard focuses on running work'.
// Empty root (global mode, no single project in focus) returns an empty board without error —
// the continuity dashboard focuses on a single project's in-flight work.
//
// AggregateContinuity 从 DataDir/tasks/ 读全部 TaskState 投影成看板卡片。进行中在前、
// 同状态按启动时间倒序——最近且未完成的任务排在最上，符合「看板聚焦在跑的工作」。
// root 为空（全局模式未聚焦单项目）返空 board，不报错——接续看板聚焦单项目进行中工作。
func AggregateContinuity(root string, now time.Time) (ContinuityBoard, error) {
	if root == "" {
		return ContinuityBoard{Now: now, Cards: []continuityCard{}}, nil
	}
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return ContinuityBoard{}, err
	}
	cards := make([]continuityCard, 0, len(states))
	incomplete, complete := 0, 0
	for _, s := range states {
		c := toContinuityCard(s)
		// Zombie annotation (design §12): root is available here (needed for the checklog-activity
		// judgment behind claimed>TTL / input-required>7d), so the board computes it at aggregation
		// time rather than in toContinuityCard (which only sees the state). Reuses the shared
		// taskpipeline.IsZombie — single truth across board / mine / health.
		//
		// 僵尸标注（设计 §12）：此处 root 可用（claimed>TTL / input-required>7d 背后的 checklog
		// 活动判断需要它），故看板在聚合时计算而非在只看 state 的 toContinuityCard 里算。复用共享的
		// taskpipeline.IsZombie——看板 / mine / health 单一真相。
		if zombie, reasons := taskpipeline.IsZombie(root, s, now); zombie {
			c.IsZombie = true
			c.ZombieReason = strings.Join(reasons, `, `)
		}
		if c.IsComplete {
			complete++
		} else {
			incomplete++
		}
		cards = append(cards, c)
	}
	slices.SortStableFunc(cards, func(a, b continuityCard) int {
		if a.IsComplete != b.IsComplete {
			if !a.IsComplete {
				return -1
			}
			return 1
		}
		return b.StartedAt.Compare(a.StartedAt)
	})
	return ContinuityBoard{
		Project:    projectName(root),
		Now:        now,
		Cards:      cards,
		Incomplete: incomplete,
		Complete:   complete,
	}, nil
}

func toContinuityCard(s *taskpipeline.TaskState) continuityCard {
	kind := s.Kind
	if kind == "" {
		kind = "code"
	}
	cur := s.CurrentGate
	if s.IsComplete() {
		cur = ""
	}
	gateTotal := len(taskpipeline.DefaultGates())
	if s.IsGeneric() {
		gateTotal = 0 // generic 不走门禁，看板不显示门禁进度（模板 {{if gt .GateTotal 0}} 会跳过整行）
	}
	return continuityCard{
		TaskRef:       s.TaskRef,
		Branch:        s.Branch,
		Kind:          kind,
		Summary:       s.Summary,
		Goal:          s.Goal,
		OriginTool:    s.OriginTool,
		SessionTools:  s.SessionTools(),
		IsComplete:    s.IsComplete(),
		CurrentGate:   cur,
		GatePassed:    len(s.CompletedGates()),
		GateTotal:     gateTotal,
		OpenBlockers:  len(s.OpenBlockers()),
		NextSteps:     len(s.NextSteps),
		Decisions:     len(s.Decisions),
		Findings:      len(s.Findings),
		ParentTaskRef: s.ParentTaskRef,
		StartedAt:     s.StartedAt,
	}
}

// continuityAsset is the embedded path of the continuity-dashboard template (same shape as assetFile).
//
// continuityAsset 是接续看板模板的嵌入路径（与 assetFile 同构）。
const continuityAsset = `assets/continuity.html`

// continuityTmpl is parsed once at process start. Reuses funcMap.
//
// continuityTmpl 进程启动时解析一次。复用 funcMap。
var continuityTmpl = template.Must(template.New(`continuity`).Funcs(funcMap).ParseFS(assetsFS, continuityAsset))

// RenderContinuityBoard renders the continuity-dashboard data as HTML. Exported for testing.
//
// RenderContinuityBoard 把接续看板数据渲染成 HTML。导出便于测试。
func RenderContinuityBoard(w io.Writer, b ContinuityBoard) error {
	return continuityTmpl.ExecuteTemplate(w, `continuity`, b)
}

// taskPublic is the external projection of a conclusion: SessionID is stripped. The HTML
// dashboard has no use for the session ID, and even though the JSON endpoint is bound to
// localhost only, we still do not serialize it — defense in depth (paired with the Host
// check to prevent DNS-rebinding reads).
//
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
	Project            string
}

// publicData is the /api/data.json payload: same shape as Data but Tasks is projected to
// taskPublic (no SessionID).
//
// publicData 是 /api/data.json 载荷：与 Data 同形但 Tasks 投影成 taskPublic（无 SessionID）。
type publicData struct {
	Summary      health.Summary
	Tasks        []taskPublic
	ActiveTask   string
	Now          time.Time
	IsGlobal     bool
	ProjectCount int
}

// toPublic projects Data into a JSON payload that does not carry SessionID.
//
// toPublic 投影 Data → 不含 SessionID 的 JSON 载荷。
func toPublic(d Data) publicData {
	tasks := make([]taskPublic, len(d.Tasks))
	for i, t := range d.Tasks {
		tasks[i] = taskPublic{
			TaskRef: t.TaskRef, Score: t.Score, Grade: t.Grade, Strength: t.Strength,
			Deterministic: t.Deterministic, AgentClaim: t.AgentClaim,
			CompletedAt: t.CompletedAt, RetrospectiveNudge: t.RetrospectiveNudge,
			Project: t.Project,
		}
	}
	return publicData{
		Summary: d.Summary, Tasks: tasks, ActiveTask: d.ActiveTask, Now: d.Now,
		IsGlobal: d.IsGlobal, ProjectCount: d.ProjectCount,
	}
}

// setSecureHeaders sets basic security headers. The dashboard is a localhost read-only
// page; this is defense in depth: X-Frame-Options prevents click-jacking, nosniff prevents
// MIME sniffing, CSP restricts origins (inline styles are required by the template and there
// is no JS, hence script-src 'none'), and Referrer-Policy keeps local paths from leaking to
// outbound links.
//
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
	mux.HandleFunc(`/`, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != `/` {
			http.NotFound(w, r)
			return
		}
		data, err := opts.aggregate(time.Now())
		if err != nil {
			// Log the full err (local stderr, with the path for triage) but respond with a
			// neutral message — do not leak the .forge path to the browser.
			//
			// 完整 err 记日志（本地 stderr，含路径便于排查），响应给中性文案——不向浏览器泄露 .forge 路径。
			log.Printf(`dashboard aggregate %s: %v`, opts.Root, err)
			http.Error(w, `聚合质量数据失败，请检查 .forge 数据完整性`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `text/html; charset=utf-8`, `渲染看板页面失败`, func(out io.Writer) error {
			return RenderPage(out, data)
		})
	})
	mux.HandleFunc(`/api/data.json`, func(w http.ResponseWriter, r *http.Request) {
		data, err := opts.aggregate(time.Now())
		if err != nil {
			log.Printf(`dashboard aggregate %s: %v`, opts.Root, err)
			http.Error(w, `聚合质量数据失败`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化看板数据失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(toPublic(data))
		})
	})
	// favicon: browsers request it automatically; return 204 to avoid console 404 noise
	// (the dashboard ships no icon asset).
	//
	// favicon：浏览器自动请求，给 204 避免 console 404 噪声（看板无需图标资源）。
	mux.HandleFunc(`/favicon.ico`, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	// Task continuity dashboard: visualizes plan / decisions / blockers / participating
	// tools for in-flight tasks. Complements the quality dashboard — the quality dashboard
	// shows the quality of finished conclusions, the continuity dashboard shows how far the
	// running task has progressed.
	//
	// 任务接续看板：进行中任务的 plan/决策/阻塞/参与工具可视化。与质量看板互补——
	// 质量看板看「已完成结论的质量」，接续看板看「在跑的任务接续到哪了」。
	mux.HandleFunc(`/continuity`, func(w http.ResponseWriter, r *http.Request) {
		board, err := AggregateContinuity(opts.Root, time.Now())
		if err != nil {
			log.Printf(`dashboard continuity %s: %v`, opts.Root, err)
			http.Error(w, `聚合接续数据失败，请检查任务接续数据完整性`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `text/html; charset=utf-8`, `渲染接续看板页面失败`, func(out io.Writer) error {
			return RenderContinuityBoard(out, board)
		})
	})
	mux.HandleFunc(`/api/continuity.json`, func(w http.ResponseWriter, r *http.Request) {
		board, err := AggregateContinuity(opts.Root, time.Now())
		if err != nil {
			log.Printf(`dashboard continuity %s: %v`, opts.Root, err)
			http.Error(w, `聚合接续数据失败`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化接续数据失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(board)
		})
	})
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
