// pulse.go —— pulse 面板的 HTTP 层：七个只读 JSON 端点挂在现有 mux 上（与看板其余路由
// 共用 Host 校验 / 安全头中间件）。聚合全在 feed.go / skillsview.go；本文件只解析
// query 参数、组装载荷、沿用现有错误风格（记完整日志、回中性文案）。
package dashboard

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/MjxUpUp/Forge/internal/act"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/health"
	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// errInvalidSkillName 报告被拒的 skill 名 query 参数（路径遍历防护）。
func errInvalidSkillName(name string) error {
	return fmt.Errorf("invalid skill name %q", name)
}

// registerPulseRoutes 把七个 pulse JSON 端点挂到 mux。从 newMux 抽出，理由同 newMux
// 本身（httptest 直接挂载）。
func registerPulseRoutes(mux *http.ServeMux, opts Options) {
	mux.HandleFunc(`/api/pulse/feed.json`, func(w http.ResponseWriter, r *http.Request) {
		q := FeedQuery{Project: r.URL.Query().Get("project")}
		if s := r.URL.Query().Get("since"); s != "" {
			since, err := time.Parse(time.RFC3339, s)
			if err != nil {
				http.Error(w, `since 需 RFC3339 格式`, http.StatusBadRequest)
				return
			}
			q.Since = since
		}
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				q.Limit = n
			}
		}
		res, err := AggregateFeed(opts, time.Now(), q)
		if err != nil {
			log.Printf(`dashboard pulse feed %s: %v`, opts.Root, err)
			http.Error(w, `归并事件流失败`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化事件流失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(map[string]any{
				"generatedAt": time.Now(),
				"events":      res.Events,
				// truncated 让客户端识别「事件被 limit 截掉」：增量轮询截断 = 有事件
				// 永久丢失，客户端须全量重拉；初始加载截断 = 更早事件不可达，如实标注。
				"truncated": res.Truncated,
			})
		})
	})

	mux.HandleFunc(`/api/pulse/task.json`, func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Query().Get("ref")
		if ref == "" {
			http.Error(w, `缺 ref 参数`, http.StatusBadRequest)
			return
		}
		pr, ok := resolvePulseTaskRoot(opts, r.URL.Query().Get("project"))
		if !ok {
			http.Error(w, `项目未在范围内`, http.StatusNotFound)
			return
		}
		state, err := taskpipeline.LoadTaskState(pr.root, ref)
		if err != nil {
			http.Error(w, `task 不存在`, http.StatusNotFound)
			return
		}
		resp, err := buildPulseTask(opts, pr, state, time.Now())
		if err != nil {
			log.Printf(`dashboard pulse task %s ref=%s: %v`, opts.Root, ref, err)
			http.Error(w, `归并任务事件失败`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化任务详情失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(resp)
		})
	})

	mux.HandleFunc(`/api/pulse/projects.json`, func(w http.ResponseWriter, r *http.Request) {
		rows := aggregatePulseProjects(opts, time.Now())
		writeRendered(w, opts.Root, `application/json`, `序列化项目列表失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(rows)
		})
	})

	mux.HandleFunc(`/api/pulse/stats.json`, func(w http.ResponseWriter, r *http.Request) {
		stats := aggregatePulseStats(opts, time.Now())
		writeRendered(w, opts.Root, `application/json`, `序列化统计失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(stats)
		})
	})

	mux.HandleFunc(`/api/pulse/skills.json`, func(w http.ResponseWriter, r *http.Request) {
		ov, err := AggregateSkills(opts, pulseCanonicalDir(), pulseEvalDir())
		if err != nil {
			log.Printf(`dashboard pulse skills %s: %v`, opts.Root, err)
			http.Error(w, `聚合 skills 数据失败`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化 skills 总览失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(ov)
		})
	})

	mux.HandleFunc(`/api/pulse/skill.json`, func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, `缺 name 参数`, http.StatusBadRequest)
			return
		}
		if !skillsfm.IsValidSkillName(name) {
			http.Error(w, `非法 skill 名`, http.StatusBadRequest)
			return
		}
		view, err := LoadSkillDetail(pulseCanonicalDir(), pulseEvalDir(), name)
		if err != nil {
			http.Error(w, `非法 skill 名`, http.StatusBadRequest)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化 skill 详情失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(view)
		})
	})

	// quality.json：触发质量 tab 的单一端点。该 tab 此前要拉 skills.json 再逐 skill 拉
	// skill.json（N+1 请求，且客户端永久缓存不过期）；本端点在服务端一次聚合各 skill
	// 的 triggerQuality + compare（便宜——LoadSkillDetail 走指纹门控缓存）。
	mux.HandleFunc(`/api/pulse/quality.json`, func(w http.ResponseWriter, r *http.Request) {
		views, err := AggregateQuality(opts, pulseCanonicalDir(), pulseEvalDir())
		if err != nil {
			log.Printf(`dashboard pulse quality %s: %v`, opts.Root, err)
			http.Error(w, `聚合触发质量数据失败`, http.StatusInternalServerError)
			return
		}
		writeRendered(w, opts.Root, `application/json`, `序列化触发质量失败`, func(out io.Writer) error {
			return json.NewEncoder(out).Encode(views)
		})
	})
}

// resolvePulseTaskRoot 选定 task.json 查询的目标项目：给了 project 过滤（key 或名）按
// 过滤选，否则取范围内唯一项目。多项目在scope而未给过滤是歧义——不静默取第一个，
// ok=false 让 handler 回 404。
func resolvePulseTaskRoot(opts Options, projectFilter string) (pulseRoot, bool) {
	roots := resolvePulseRoots(opts)
	if projectFilter != "" {
		for _, pr := range roots {
			if pr.matches(projectFilter) {
				return pr, true
			}
		}
		return pulseRoot{}, false
	}
	if len(roots) == 1 {
		return roots[0], true
	}
	return pulseRoot{}, false
}

// pulseGateProgress 是任务的 gate x/y。
type pulseGateProgress struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// pulseTaskState 是 task.json 的 state 块（无 SessionID——绝不向面板序列化 session 标识）。
type pulseTaskState struct {
	CurrentGate  string            `json:"currentGate"`
	StartedAt    time.Time         `json:"startedAt"`
	OriginTool   string            `json:"originTool,omitempty"`
	Zombie       bool              `json:"zombie"`
	GateProgress pulseGateProgress `json:"gateProgress"`
	// Lease 是跨机器认领的投影（task-lease，sync-convergence §4）：谁持有、请求时点
	// 是否仍有效、何时过期。多机器前的任务为 nil（从未认领）——与 FeedEvent.Node
	// 同一条 omitempty 纪律。
	Lease *pulseLease `json:"lease,omitempty"`
}

// pulseLease 是 taskpipeline.Lease 的 state 块投影。Active 派生自「过期即自由」的
// 唯一规则（Lease.ActiveAt）——此处不另造判定。
type pulseLease struct {
	HolderNode string    `json:"holderNode"`
	Active     bool      `json:"active"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Fencing    int64     `json:"fencing"`
	TTLSec     int64     `json:"ttlSec"`
}

// pulseEvidence 是完成证据的来源分布。
type pulseEvidence struct {
	Deterministic int     `json:"deterministic"`
	AgentClaim    int     `json:"agentClaim"`
	Ratio         float64 `json:"ratio"`
	Strength      string  `json:"strength,omitempty"`
}

// pulseFacts 是 task.json 的完成事实块：判据通过、先失败证明、评审轮次、逃生次数
// 与证据来源分布。2.3 起没有 overall/grade/维度分——要量化就数事实。
// FromConclusion 标记该块来自最新 act 结论（而非盘上的 TaskEvidence），前端如实标注。
type pulseFacts struct {
	Criteria       int            `json:"criteria"`
	CriteriaPassed int            `json:"criteriaPassed"`
	RedFirstTrials int            `json:"redFirstTrials,omitempty"`
	RedFirstClosed int            `json:"redFirstClosed,omitempty"`
	ReviewRounds   int            `json:"reviewRounds,omitempty"`
	Escapes        int            `json:"escapes,omitempty"`
	Evidence       *pulseEvidence `json:"evidence,omitempty"`
	FromConclusion bool           `json:"fromConclusion,omitempty"`
}

// pulseAcceptance 是 verify-acceptance 的 x/y。
type pulseAcceptance struct {
	Pass  int `json:"pass"`
	Total int `json:"total"`
}

// pulseTaskResponse 是 /api/pulse/task.json 载荷。Truncated 对齐 feed.json 契约：
// 任务事件流共用 AggregateFeed 的默认上限，长任务的 transcript 会被截断——不带该
// 标记详情页会静默冒充完整序列。
type pulseTaskResponse struct {
	TaskRef    string          `json:"taskRef"`
	Project    string          `json:"project"`
	State      pulseTaskState  `json:"state"`
	Events     []FeedEvent     `json:"events"`
	Truncated  bool            `json:"truncated"` // events 被默认上限截断（最早事件不可达）
	Facts      *pulseFacts     `json:"facts,omitempty"`
	Acceptance pulseAcceptance `json:"acceptance"`
	// DocReview 是输出→回检循环的 L2 回检证据（doc gate）。任务无回检记录时为
	// nil——前端仅在存在时渲染该块，「未回检」绝不伪装成全零事实块。
	DocReview *pulseDocReview `json:"docReview,omitempty"`
}

// pulseDocReview 是 taskpipeline.DocReview 的面板投影：最新判定 + rubric 分 + 轮次，
// 外加累计轮数（跨轮收敛趋势是回检循环的可观测信号）。
type pulseDocReview struct {
	Passed      bool `json:"passed"`
	RubricScore int  `json:"rubricScore"`
	Round       int  `json:"round"`
	RoundsTotal int  `json:"roundsTotal"` // 含历史轮次的累计轮数（≥ Round，钳制见填充处）
	// ReviewedAt 用指针：time.Time 的 omitempty 对零值无效（仍序列化 0001-01-01），
	// 零值评审时刻须序列化为缺席而非假日期。
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
}

// buildPulseTask 组装 task.json 载荷：状态投影（僵尸经共享 taskpipeline.IsZombie 计算）、
// 任务级事件流（复用 AggregateFeed 的 TaskRef 过滤）、来自 TaskState.Evidence 的事实块
// （存量任务从最新 act 结论回填），以及 join 自该结论的验收 + 证据强度。事件流归并
// 失败返回 error 不吞没——handler 转成 500，与 feed.json 同语义。
func buildPulseTask(opts Options, pr pulseRoot, state *taskpipeline.TaskState, now time.Time) (pulseTaskResponse, error) {
	resp := pulseTaskResponse{
		TaskRef: state.TaskRef,
		Project: pr.name,
		Events:  []FeedEvent{},
	}

	currentGate := state.CurrentGate
	if state.IsComplete() {
		currentGate = ""
	}
	gateTotal := 0
	if !state.IsGeneric() {
		gateTotal = len(taskpipeline.DefaultGates())
	}
	zombie, _ := taskpipeline.IsZombie(pr.root, state, now)
	resp.State = pulseTaskState{
		CurrentGate: currentGate,
		StartedAt:   state.StartedAt,
		OriginTool:  state.OriginTool,
		Zombie:      zombie,
		GateProgress: pulseGateProgress{
			Passed: len(state.CompletedGates()),
			Total:  gateTotal,
		},
	}
	if state.Lease != nil {
		resp.State.Lease = &pulseLease{
			HolderNode: state.Lease.HolderNode,
			Active:     state.Lease.ActiveAt(now),
			ExpiresAt:  state.Lease.ExpiresAt(),
			Fencing:    state.Lease.Fencing,
			TTLSec:     state.Lease.TTLSec,
		}
	}

	res, err := AggregateFeed(opts, now, FeedQuery{Project: cmp.Or(pr.key, pr.name), TaskRef: state.TaskRef})
	if err != nil {
		return pulseTaskResponse{}, err
	}
	resp.Events = res.Events
	resp.Truncated = res.Truncated

	// 完成事实 + 验收：优先最新 act 结论（事实清单与证据强度都在结论里，含 TaskState
	// 从未落盘事实块的存量任务）；尚无结论时退化用盘上的 TaskEvidence / 现活
	// Acceptance 切片。结论取自指纹门控缓存，不再现读 act.LoadAll。
	if d, err := sharedPulseCache.projectData(pr); err == nil {
		for i := len(d.conclusions) - 1; i >= 0; i-- {
			c := &d.conclusions[i]
			if c.TaskRef != state.TaskRef {
				continue
			}
			resp.Acceptance = pulseAcceptance{Pass: c.AcceptancePass, Total: c.AcceptanceTotal}
			resp.Facts = factsFromConclusion(c)
			break
		}
	}
	if resp.Acceptance.Total == 0 && len(state.Acceptance) > 0 {
		pass := 0
		for _, a := range state.Acceptance {
			if a.Passed {
				pass++
			}
		}
		resp.Acceptance = pulseAcceptance{Pass: pass, Total: len(state.Acceptance)}
	}
	if resp.Facts == nil {
		ev := state.Evidence
		if ev == nil {
			ev = taskpipeline.BuildTaskEvidence(pr.root, state)
		}
		resp.Facts = factsFromEvidence(ev)
	}

	// ("第 5 轮 · 累计 2 轮").
	//
	// Doc gate 证据：最新判定 + 累计轮数（历史轮 + 当前轮）。DocReview 为 nil →
	// 块缺席，绝不编造全零块。RoundsTotal 钳制到 ≥ Round：显式 --round 跳号（合法
	// 覆盖）或历史截断否则会让面板自相矛盾（「第 5 轮 · 累计 2 轮」）。
	if state.DocReview != nil {
		roundsTotal := len(state.DocReviewHistory) + 1
		if state.DocReview.Round > roundsTotal {
			roundsTotal = state.DocReview.Round
		}
		dr := &pulseDocReview{
			Passed:      state.DocReview.Passed,
			RubricScore: state.DocReview.RubricScore,
			Round:       state.DocReview.Round,
			RoundsTotal: roundsTotal,
		}
		if !state.DocReview.ReviewedAt.IsZero() {
			at := state.DocReview.ReviewedAt
			dr.ReviewedAt = &at
		}
		resp.DocReview = dr
	}
	return resp, nil
}

// factsFromEvidence 把盘上的事实清单投影成线上块；nil 事实清单返回 nil（前端如实
// 显示「无事实」，绝不编造全零块）。
func factsFromEvidence(ev *tasktypes.TaskEvidence) *pulseFacts {
	if ev == nil {
		return nil
	}
	return &pulseFacts{
		Criteria:       ev.Criteria,
		CriteriaPassed: ev.CriteriaPassed,
		RedFirstTrials: ev.RedFirstTrials,
		RedFirstClosed: ev.RedFirstClosed,
		ReviewRounds:   ev.ReviewRounds,
		Escapes:        ev.Escapes,
	}
}

// factsFromConclusion 从最新 act 结论投影完成事实；证据强度只在结论里，且只在
// 确有证据条目时回填（零证据结论不编造证据块）。
func factsFromConclusion(c *act.Conclusion) *pulseFacts {
	if c == nil {
		return nil
	}
	f := &pulseFacts{
		Criteria:       c.AcceptanceTotal,
		CriteriaPassed: c.AcceptancePass,
		RedFirstTrials: c.RedFirstTrials,
		RedFirstClosed: c.RedFirstClosed,
		ReviewRounds:   c.ReviewRounds,
		Escapes:        c.Escapes,
		FromConclusion: true,
	}
	if c.Deterministic+c.AgentClaim > 0 {
		f.Evidence = &pulseEvidence{
			Deterministic: c.Deterministic,
			AgentClaim:    c.AgentClaim,
			Ratio:         c.Ratio,
			Strength:      c.Strength,
		}
	}
	return f
}

// pulseProject 是 /api/pulse/projects.json 的一行。
type pulseProject struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	ActiveTasks int    `json:"activeTasks"`
	Zombies     int    `json:"zombies"`
	// LastConclusion 是最近一条结论的事实摘要（判据/证据强度/完成时间）；无结论时 nil。
	LastConclusion *pulseLastConclusion `json:"lastConclusion,omitempty"`
	// Sync 是机器本地的 git 同步绑定 + 最近操作戳（sync Phase 1）。未初始化（或
	// 文件不可读）时为 nil——omitempty 保持未绑定项目的线上结构不变。
	Sync *pulseSync `json:"sync,omitempty"`
}

// pulseLastConclusion 是项目行上的最近完成事实：判据通过数、证据强度、完成时间。
type pulseLastConclusion struct {
	Criteria       int       `json:"criteria"`
	CriteriaPassed int       `json:"criteriaPassed"`
	Strength       string    `json:"strength"`
	At             time.Time `json:"at"`
}

// pulseSync 是 DataDir/sync-remote.json 的面板投影（机器本地，永不随 bundle 旅行）。
type pulseSync struct {
	Remote     string `json:"remote"`
	NodeID     string `json:"nodeId"`
	LastPushAt string `json:"lastPushAt,omitempty"`
	LastPullAt string `json:"lastPullAt,omitempty"`
}

// loadPulseSync 每请求直读 sync-remote.json，不走指纹缓存：文件极小、轮询 30 秒
// 一次，且它刻意不在缓存指纹集里——把它加进去是用缓存失效复杂度换近乎零收益。
// 未绑定或不可读 → nil（CLI 的 `sync status` 会响亮暴露损坏；面板省略该块而非
// 编造一块）。
func loadPulseSync(root string) *pulseSync {
	var file struct {
		Remote     string `json:"remote"`
		NodeID     string `json:"node_id"`
		LastPushAt string `json:"last_push_at"`
		LastPullAt string `json:"last_pull_at"`
	}
	raw, err := os.ReadFile(filepath.Join(forgedata.DataDirFor(root), `sync-remote.json`))
	if err != nil {
		return nil
	}
	if err := json.Unmarshal(raw, &file); err != nil || file.Remote == `` {
		return nil
	}
	return &pulseSync{
		Remote: file.Remote, NodeID: file.NodeID,
		LastPushAt: file.LastPushAt, LastPullAt: file.LastPullAt,
	}
}

// aggregatePulseProjects 列出范围内每个项目的活跃/僵尸计数与最新结论。单项目读失败
// 降级为该项目的零值行（不致命跳过——一个坏项目不应让整面板空白）。源数据取自
// 指纹门控缓存。
func aggregatePulseProjects(opts Options, now time.Time) []pulseProject {
	rows := []pulseProject{}
	for _, pr := range resolvePulseRoots(opts) {
		row := pulseProject{Key: pr.key, Name: pr.name}
		d, err := sharedPulseCache.projectData(pr)
		if err == nil {
			for _, s := range d.states {
				if !s.IsComplete() {
					row.ActiveTasks++
					if zombie, _ := taskpipeline.IsZombie(pr.root, s, now); zombie {
						row.Zombies++
					}
				}
			}
			if latest := latestConclusion(d.conclusions); latest != nil {
				row.LastConclusion = &pulseLastConclusion{
					Criteria:       latest.AcceptanceTotal,
					CriteriaPassed: latest.AcceptancePass,
					Strength:       latest.Strength,
					At:             latest.CompletedAt,
				}
			}
		}
		// 同步绑定独立于缓存错误分支——缓存读失败不该藏起绑定信息（反之亦然）。
		row.Sync = loadPulseSync(pr.root)
		rows = append(rows, row)
	}
	return rows
}

// latestConclusion 返回最近一条结论，无则 nil（结论自 act.LoadAll 起即按时间序）。
func latestConclusion(cs []act.Conclusion) *act.Conclusion {
	if len(cs) == 0 {
		return nil
	}
	return &cs[len(cs)-1]
}

// pulseStats 是 /api/pulse/stats.json 载荷。无数据用 nil 指针/null，绝不编造 0。
// 2.3 起只有事实：判据通过、先失败证明、评审轮次、逃生次数、证据强度分布——
// 没有均分/中位/档位分布/低维直方图。
type pulseStats struct {
	Projects          int      `json:"projects"`
	ActiveTasks       int      `json:"activeTasks"`
	Zombies           int      `json:"zombies"`
	Alerts            int      `json:"alerts"`
	Nudges            int      `json:"nudges"`           // 窗口内 nudge 总量·14天（透明拆解分量；全量见 health.nudge_count）
	NudgesActionable  int      `json:"nudgesActionable"` // 告警分量：窗口内非降档 nudge（alerts = zombies + 此值）
	EvidenceBlindRate *float64 `json:"evidenceBlindRate"`

	// —— 完成事实 + 强度分布。仅在存在结论时填充；omitempty 保持无数据时的线上
	// 结构不变。——
	Conclusions    int            `json:"conclusions,omitempty"`
	StrengthDist   map[string]int `json:"strengthDist,omitempty"` // Strong/Weak/Unverified/NoData → 数
	CappedWeak     int            `json:"cappedWeak,omitempty"`   // 逃生舱封顶 Weak（带 det 证据——是逃生代价，不是盲区）
	Criteria       int            `json:"criteria,omitempty"`
	CriteriaPassed int            `json:"criteriaPassed,omitempty"`
	RedFirstTrials int            `json:"redFirstTrials,omitempty"`
	RedFirstClosed int            `json:"redFirstClosed,omitempty"`
	ReviewRounds   int            `json:"reviewRounds,omitempty"`
	Escapes        int            `json:"escapes,omitempty"`
}

// aggregatePulseStats 跨范围合并结论，复用 health.SummarizeAtAcked 得盲区率/强度分布
// 与事实合计（与质量看板单一真相）；alerts = 僵尸任务数 + 窗口化【可行动】结论数
// （NudgeActionable，14 天——见 health.NudgeRecentWindow）。降档 Weak（逃生税已收）
// 不进告警、只留 Nudges 拆解（2026-09 告警疲劳分级）。源数据取自指纹门控缓存。
func aggregatePulseStats(opts Options, now time.Time) pulseStats {
	stats := pulseStats{}
	var cs []act.Conclusion
	var disps []act.Disposition
	for _, pr := range resolvePulseRoots(opts) {
		stats.Projects++
		d, err := sharedPulseCache.projectData(pr)
		if err != nil {
			continue
		}
		for _, s := range d.states {
			if s.IsComplete() {
				continue
			}
			stats.ActiveTasks++
			if zombie, _ := taskpipeline.IsZombie(pr.root, s, now); zombie {
				stats.Zombies++
			}
		}
		cs = append(cs, d.conclusions...)
		disps = append(disps, d.dispositions...)
	}
	stats.Alerts = stats.Zombies
	if len(cs) == 0 {
		return stats
	}
	// 窗口化 summary（2026-08 告警疲劳校准）：面向告警的 Nudges 用 NudgeRecent
	//（14 天窗口）而非全量 NudgeCount；2026-09 分级只加 NudgeActionable；证据式 ack
	// 让已 retro-done 的 nudge 退出告警（匹配器单一真相源在 act.AckMatcher）。
	summary := health.SummarizeAtAcked(cs, now, act.AckMatcher(disps))
	blind := summary.BlindSpotRate
	stats.EvidenceBlindRate = &blind
	stats.Nudges = summary.NudgeRecent
	stats.NudgesActionable = summary.NudgeActionable
	stats.Alerts += summary.NudgeActionable
	// 事实与强度分布纯投影，零新增 IO。仅在存在结论时到达此处——无结论走上方提前
	// return，字段缺席而非编造的空。
	stats.Conclusions = summary.TotalTasks
	stats.StrengthDist = summary.StrengthDist
	stats.CappedWeak = summary.CappedWeakCount
	stats.Criteria = summary.Criteria
	stats.CriteriaPassed = summary.CriteriaPassed
	stats.RedFirstTrials = summary.RedFirstTrials
	stats.RedFirstClosed = summary.RedFirstClosed
	stats.ReviewRounds = summary.ReviewRounds
	stats.Escapes = summary.Escapes
	return stats
}

// pulseCanonicalDir 只读地解析 canonical skill 目录：FORGE_SKILLS_CANONICAL 覆盖，或
// 已解压的 embed 缓存（路径取自 skillscanonical.EmbeddedCacheDir——单一真相源）。
// 刻意不调 skillscanonical.Resolve——首次使用它会把内置库解压落盘，违反看板只读
// 红线。"" = canonical 不可用（总览降级为观测集）。
func pulseCanonicalDir() string {
	if env := os.Getenv(skillscanonical.EnvName); env != "" {
		if info, err := os.Stat(env); err == nil && info.IsDir() {
			return env
		}
		return ""
	}
	cache, err := skillscanonical.EmbeddedCacheDir()
	if err != nil {
		return ""
	}
	if info, err := os.Stat(cache); err == nil && info.IsDir() {
		return cache
	}
	return ""
}

// pulseEvalDir 解析 skillseval eval 目录（默认 ~/.forge/evals——解析链与一次性旧路径
// 迁移见 skillseval/dir.go）。解析失败降级为 "" = 无 eval 数据。
func pulseEvalDir() string {
	dir, err := skillseval.EvalDir()
	if err != nil {
		return ""
	}
	return dir
}
