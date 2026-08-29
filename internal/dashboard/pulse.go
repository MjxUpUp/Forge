// pulse.go —— pulse 面板的 HTTP 层：七个只读 JSON 端点挂在现有 mux 上（与看板其余路由
// 共用 Host 校验 / 安全头中间件）。聚合全在 feed.go / skillsview.go；本文件只解析
// query 参数、组装载荷、沿用现有错误风格（记完整日志、回中性文案）。
package dashboard

import (
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
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/skillsfm"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
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

// pulseDimension 是一个评分维度及其配置权重。
type pulseDimension struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	Score  int     `json:"score"`
	Detail string  `json:"detail"`
}

// pulseEvidence 是评分的证据来源分布。
type pulseEvidence struct {
	Deterministic int     `json:"deterministic"`
	AgentClaim    int     `json:"agentClaim"`
	Ratio         float64 `json:"ratio"`
	Strength      string  `json:"strength,omitempty"`
}

// pulseScore 是 task.json 的 score 块（任务未评分且无结论可回填时为 nil）。
// FromConclusion 标记从最新 act 结论回填的降级块（存量任务的 TaskState.Score 从未落盘）
// ——该形态 dimensions 为空，前端如实标注而不是伪造维度条。
type pulseScore struct {
	Overall        float64          `json:"overall"`
	Grade          string           `json:"grade"`
	Dimensions     []pulseDimension `json:"dimensions"`
	CappedReason   string           `json:"cappedReason,omitempty"`
	Evidence       *pulseEvidence   `json:"evidence,omitempty"`
	FromConclusion bool             `json:"fromConclusion,omitempty"`
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
	Score      *pulseScore     `json:"score"`
	Acceptance pulseAcceptance `json:"acceptance"`
	// DocReview 是输出→回检循环的 L2 回检证据（doc gate）。任务无回检记录时为
	// nil——前端仅在存在时渲染该块，「未回检」绝不伪装成全零评分块。
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
// 任务级事件流（复用 AggregateFeed 的 TaskRef 过滤）、来自 TaskState.Score 的评分块
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

	res, err := AggregateFeed(opts, now, FeedQuery{Project: firstNonEmpty(pr.key, pr.name), TaskRef: state.TaskRef})
	if err != nil {
		return pulseTaskResponse{}, err
	}
	resp.Events = res.Events
	resp.Truncated = res.Truncated

	if state.Score != nil {
		resp.Score = toPulseScore(state.Score)
	}

	// 验收 + 证据强度：优先最新 act 结论（两者都有）；尚无结论时退化用现活的
	// Acceptance 切片。结论取自指纹门控缓存，不再现读 act.LoadAll。
	if d, err := sharedPulseCache.projectData(pr); err == nil {
		for i := len(d.conclusions) - 1; i >= 0; i-- {
			if d.conclusions[i].TaskRef != state.TaskRef {
				continue
			}
			resp.Acceptance = pulseAcceptance{Pass: d.conclusions[i].AcceptancePass, Total: d.conclusions[i].AcceptanceTotal}
			// 存量任务：结论在但 TaskState.Score 从未落盘（Score 字段后下沉；
			// cli/act_rebuild.go 处理同一形态）。回填降级评分块，避免详情页与自己的
			// conclusion 事件自相矛盾。
			if resp.Score == nil && d.conclusions[i].Score > 0 {
				resp.Score = &pulseScore{
					Overall:    d.conclusions[i].Score,
					Grade:      d.conclusions[i].Grade,
					Dimensions: []pulseDimension{},
					Evidence: &pulseEvidence{
						Deterministic: d.conclusions[i].Deterministic,
						AgentClaim:    d.conclusions[i].AgentClaim,
						Ratio:         d.conclusions[i].Ratio,
						Strength:      d.conclusions[i].Strength,
					},
					FromConclusion: true,
				}
			} else if resp.Score != nil && resp.Score.Evidence == nil &&
				d.conclusions[i].Deterministic+d.conclusions[i].AgentClaim > 0 {
				// Score 真实存在但 Evidence 为 nil（评分时零证据输入，buildEvidenceSummary
				// 合法返回 nil）：整块从结论回填，否则详情页评分块无证据链、下方
				// transcript 的 conclusion 事件却带 det/claim——自相矛盾。结论本身无证据
				// 数据时不回填（全零块是编造，保持 null 由前端如实显示"无证据"）。
				// 守卫只看 det+claim：Strength 不能当信号——BuildConclusion 永写
				// ec.Strength().String()，零证据结论的 Strength 是非空 "NoData"，
				// 判空恒假（曾有的 `Strength != ""` 析取项让本 skip 路径生产不可达）。
				resp.Score.Evidence = &pulseEvidence{
					Deterministic: d.conclusions[i].Deterministic,
					AgentClaim:    d.conclusions[i].AgentClaim,
					Ratio:         d.conclusions[i].Ratio,
					Strength:      d.conclusions[i].Strength,
				}
			} else if resp.Score != nil && resp.Score.Evidence != nil && resp.Score.Evidence.Strength == "" {
				resp.Score.Evidence.Strength = d.conclusions[i].Strength
			}
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

// toPulseScore 把 scoringtypes.ScoreResult 投影成线上形状，维度带上配置权重
// （取自 scoringtypes.DefaultWeights）。
func toPulseScore(s *scoringtypes.ScoreResult) *pulseScore {
	weights := scoringtypes.DefaultWeights()
	dims := make([]pulseDimension, 0, len(s.Dimensions))
	for _, d := range s.Dimensions {
		dims = append(dims, pulseDimension{
			Name:   string(d.Dimension),
			Weight: weights[string(d.Dimension)],
			Score:  d.Score,
			Detail: d.Detail,
		})
	}
	out := &pulseScore{
		Overall:      s.Overall,
		Grade:        s.Grade,
		Dimensions:   dims,
		CappedReason: s.CappedReason,
	}
	if s.Evidence != nil {
		out.Evidence = &pulseEvidence{
			Deterministic: s.Evidence.Deterministic,
			AgentClaim:    s.Evidence.AgentClaim,
			Ratio:         s.Evidence.Ratio,
		}
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// pulseProject 是 /api/pulse/projects.json 的一行。
type pulseProject struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	ActiveTasks int      `json:"activeTasks"`
	Zombies     int      `json:"zombies"`
	LastGrade   string   `json:"lastGrade,omitempty"`
	LastScore   *float64 `json:"lastScore"` // 无结论时 null
	// Sync 是机器本地的 git 同步绑定 + 最近操作戳（sync Phase 1）。未初始化（或
	// 文件不可读）时为 nil——omitempty 保持未绑定项目的线上结构不变。
	Sync *pulseSync `json:"sync,omitempty"`
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
				row.LastGrade = latest.Grade
				score := latest.Score
				row.LastScore = &score
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

// pulseStats 是 /api/pulse/stats.json 载荷。数值聚合用指针，「无数据」是 null，
// 绝不编造 0。
type pulseStats struct {
	Projects          int      `json:"projects"`
	ActiveTasks       int      `json:"activeTasks"`
	Zombies           int      `json:"zombies"`
	AvgScore          *float64 `json:"avgScore"`
	MedianScore       *float64 `json:"medianScore"`
	Trend             string   `json:"trend"`
	Alerts            int      `json:"alerts"`
	Nudges            int      `json:"nudges"` // 需回顾结论数·14天窗口（alerts = zombies + nudges 的拆解，前端分开展示；全量见 health.nudge_count）
	EvidenceBlindRate *float64 `json:"evidenceBlindRate"`
}

// aggregatePulseStats 跨范围合并结论，复用 health.SummarizeAt 算均分/中位/趋势/盲区率
// （与质量看板单一真相）；alerts = 僵尸任务数 + 窗口化回顾结论数（NudgeRecent，14 天
// ——见 health.NudgeRecentWindow）。源数据取自指纹门控缓存。
func aggregatePulseStats(opts Options, now time.Time) pulseStats {
	stats := pulseStats{Trend: "insufficient"}
	var cs []act.Conclusion
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
	}
	stats.Alerts = stats.Zombies
	if len(cs) == 0 {
		return stats
	}
	// 窗口化 summary（2026-08 告警疲劳校准）：面向告警的 Nudges 用 NudgeRecent
	//（14 天窗口）而非全量 NudgeCount——陈旧 nudge（session 早已关闭、历史上无 ack
	// 机制）不得把面板红灯永远挂着。全量计数仍留在 health/查询面供趋势分析。
	summary := health.SummarizeAt(cs, now)
	avg := summary.AvgScore
	median := summary.MedianScore
	blind := summary.BlindSpotRate
	stats.AvgScore = &avg
	stats.MedianScore = &median
	stats.EvidenceBlindRate = &blind
	if summary.Trend != "" {
		stats.Trend = summary.Trend
	}
	stats.Nudges = summary.NudgeRecent
	stats.Alerts += summary.NudgeRecent
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
