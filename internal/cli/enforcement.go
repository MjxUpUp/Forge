package cli

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// enforcement.go —— `forge enforcement`：执法健康报告与随机审计（vNext P2，设计
// M7 审计层 + M8 协议自身指标）。
//
// 依据（2026-08-31 跨学科调研）：S3* 独立审计（绕开常规汇报链、不采信自我报告）
// 与"无灾≠安全"复盘是六学科共同指向的缺口；essential variables（无视率/门禁通过
// 率/升档数）作为协议自身的超稳定性输入——越界即应触发对规则本身的审查（双环）。
// 本命令只读聚合三路数据：checklog（advisory/blocked 判定）、markers/（无视计数
// 与测试编辑计数）、wild/（野外申报），可 join 已完成任务做抽样审计（--sample）。
func init() {
	rootCmd.AddCommand(enforcementCmd)
	enforcementCmd.Flags().Int("sample", 0, "随机抽 N 个已完成任务 join 其会话执法遥测（随机审计）")
	enforcementCmd.Flags().Bool("json", false, "JSON 格式输出")
}

var enforcementCmd = &cobra.Command{
	Use:   "enforcement",
	Short: "执法健康报告：无视/升档/阻断计数 + 双环与降档信号（只读）",
	Long: `Enforcement health report over checklog + session markers + wild declarations.

聚合 task-guard 的 advisory/blocked 计数、无视升档会话、测试编辑计数、野外申报，
并给出两个治理信号：双环（升档会话超阈——审查规则本身而非继续加码执法）与降格
（提升规则长期零阻断零升档——zombie rule 复审）。--sample N 随机抽已完成任务，
join 其 session 的 ignores/wild 遥测——随机化使被审计方不可预演（抗俘获）。`,
	RunE: runEnforcement,
}

// enforcementReport 是报告契约（--json 直出）。字段全部可从三路数据推导，无新增
// 持久状态——文件系统即真相源。
type enforcementReport struct {
	TaskGuard struct {
		Advisory int `json:"advisory"` // checklog：task-guard advisory 级
		Blocked  int `json:"blocked"`  // checklog：task-guard blocked 级
	} `json:"task_guard"`
	EscalatedSessions int         `json:"escalated_sessions"` // ignores≥2 的会话数（无视升档）
	MaxIgnores        int         `json:"max_ignores"`        // 单会话无视计数峰值
	WindowViolations  int         `json:"window_violations"`  // 缓冲窗口超时未补救的会话数（违规记录）
	TestEditSessions  int         `json:"test_edit_sessions"` // _test.go 无任务编辑的会话数
	TestEditTotal     int         `json:"test_edit_total"`
	WildDeclarations  int         `json:"wild_declarations"`
	TasksCompleted    int         `json:"tasks_completed"`
	DoubleLoop        []string    `json:"double_loop"` // 双环触发：审查规则本身
	DemotionReview    []string    `json:"demotion_review"`
	Samples           []taskAudit `json:"samples,omitempty"`
}

// taskAudit 是一次随机审计的 join 结果：任务 × 其会话的执法遥测。
type taskAudit struct {
	TaskRef      string  `json:"task_ref"`
	SessionID    string  `json:"session_id"`
	Ignores      int     `json:"ignores"`    // 该会话的无视计数（0=未触发或未留痕）
	WildCount    int     `json:"wild_count"` // 该会话的野外申报数
	Advisories   int     `json:"advisories"` // 该任务的 task-guard advisory 条数
	Blocked      int     `json:"blocked"`    // 该任务的 blocked 条数
	ScoreOverall float64 `json:"score_overall,omitempty"`
	Verdict      string  `json:"verdict"` // 采样结论一句话
}

func runEnforcement(cmd *cobra.Command, args []string) error {
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	dataDir := forgedata.DataDirFor(root)
	rep := buildEnforcementReport(root, dataDir)

	if n, _ := cmd.Flags().GetInt("sample"); n > 0 {
		rep.Samples = sampleCompletedTasks(root, dataDir, n)
	}

	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(rep)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "task-guard：advisory %d · blocked %d\n", rep.TaskGuard.Advisory, rep.TaskGuard.Blocked)
	fmt.Fprintf(w, "无视升档会话 %d（峰值 %d 次）· 无任务测试编辑 %d 次/%d 会话 · 野外申报 %d\n",
		rep.EscalatedSessions, rep.MaxIgnores, rep.TestEditTotal, rep.TestEditSessions, rep.WildDeclarations)
	fmt.Fprintf(w, "已完成任务 %d\n", rep.TasksCompleted)
	if len(rep.DoubleLoop) > 0 {
		fmt.Fprintf(w, "双环触发（审查规则本身，勿继续加码执法）：%s\n", strings.Join(rep.DoubleLoop, "、"))
	}
	if len(rep.DemotionReview) > 0 {
		fmt.Fprintf(w, "降格复审（提升规则零阻断零升档——zombie rule）：%s\n", strings.Join(rep.DemotionReview, "、"))
	}
	for _, s := range rep.Samples {
		fmt.Fprintf(w, "· %s（session %s）：ignores=%d wild=%d advisory=%d blocked=%d — %s\n",
			s.TaskRef, shortSession(s.SessionID), s.Ignores, s.WildCount, s.Advisories, s.Blocked, s.Verdict)
	}
	if rep.TaskGuard.Advisory == 0 && rep.TaskGuard.Blocked == 0 && rep.WildDeclarations == 0 {
		fmt.Fprintln(w, "（无执法事件记录——干净或未启用）")
	}
	fmt.Fprintln(w, "（口径：会话计数为近 7 天滚动窗口（markers 定期清扫）；checklog 计数含归档全量）")
	return nil
}

// buildEnforcementReport 聚合三路数据。双环阈值：任一会话 ignores≥2 即触发（升档
// 文案已发而仍继续——规则或流程有一方需要被审视）；降格条件：规则处于提升位
// （存在 promotion 配置的 task-guard）且 blocked==0 且无升档会话——"备而从未用"
// 要么环境极干净（可接受）要么规则已过时，均值得人看一眼。
func buildEnforcementReport(root, dataDir string) enforcementReport {
	var rep enforcementReport
	entries, lerr := checklog.LoadAllAll(root)
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: checklog 读取不全（审计口径可能低估）: %v\n", lerr)
	}
	for _, e := range entries {
		if string(e.Check) != "task-guard" {
			continue
		}
		switch e.EffectiveLevel() {
		case checklog.LevelAdvisory:
			rep.TaskGuard.Advisory++
		case checklog.LevelBlocked:
			rep.TaskGuard.Blocked++
		}
	}
	// 数据形态实证（2026-08-31 dogfood，本项目真实 checklog 零 task-guard 行）：
	// shouldRecordCheck 只记脚本 FAIL 与 scoring 检查的 PASS——task-guard 的
	// advisory 结果（脚本 exit 0 + WARN）从不进 checklog，生产上 Advisory 计数
	// 恒 0。advisory 遥测的第一来源是 markers 的无视/测试编辑计数器（下方），
	// 双环信号也只依赖 markers；Advisory 字段保留是为 FAIL 提升路径（hook.go 记
	// Passed=true + Level=advisory 的那类）与未来日志策略变化留的直读位。

	ignores := readMarkerCounts(dataDir, "forge-taskguard-ignores-")
	for _, n := range ignores {
		if n > rep.MaxIgnores {
			rep.MaxIgnores = n
		}
		if n >= 2 {
			rep.EscalatedSessions++
		}
	}
	rep.WindowViolations = len(readMarkerCounts(dataDir, "forge-taskguard-violation-"))
	tests := readMarkerCounts(dataDir, "forge-test-edits-")
	for _, n := range tests {
		rep.TestEditTotal += n
		if n > 0 {
			rep.TestEditSessions++
		}
	}
	rep.WildDeclarations = countWildAll(dataDir)

	if states, err := taskpipeline.ListTaskStates(root); err != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: 任务状态读取失败（TasksCompleted 口径可能低估）: %v\n", err)
	} else {
		for _, st := range states {
			if st.CompletedAt != nil {
				rep.TasksCompleted++
			}
		}
	}

	if rep.EscalatedSessions > 0 {
		rep.DoubleLoop = append(rep.DoubleLoop,
			fmt.Sprintf("task-guard：%d 个会话已进入升档档位（≥2 次无视）——按双环审查规则本身（文案/门槛/出口是否合理），而非继续加码", rep.EscalatedSessions))
	}
	// 降格信号只在存在 task-guard 遥测时列（F2：干净/未启用项目恒触发是噪声，且
	// 与"无执法事件"行同屏矛盾）；措辞不虚断"提升位"——promotion 与否属 hostcap
	// 配置，本报告只陈述可观测事实（有遥测而零阻断零升档）。
	hasTelemetry := rep.TaskGuard.Advisory > 0 || rep.TaskGuard.Blocked > 0 || len(ignores) > 0
	if hasTelemetry && rep.TaskGuard.Blocked == 0 && rep.EscalatedSessions == 0 {
		rep.DemotionReview = append(rep.DemotionReview,
			"task-guard：有遥测但零阻断零升档——zombie rule 复审（确认规则仍需要，或降格回 advisory）")
	}
	return rep
}

// sampleCompletedTasks 随机抽 n 个已完成任务，join 其 SessionID 的 ignores/wild 与
// TaskRef 的 checklog 条数。随机化是抗俘获要点：不可预演的审计才可信。
func sampleCompletedTasks(root, dataDir string, n int) []taskAudit {
	states, err := taskpipeline.ListTaskStates(root)
	if err != nil {
		return nil
	}
	var done []*taskpipeline.TaskState
	for _, st := range states {
		if st.CompletedAt != nil {
			done = append(done, st)
		}
	}
	if len(done) == 0 {
		return nil
	}
	rand.Shuffle(len(done), func(i, j int) { done[i], done[j] = done[j], done[i] })
	if n > len(done) {
		n = len(done)
	}
	ignores := readMarkerCounts(dataDir, "forge-taskguard-ignores-")
	wild := countWildBySession(dataDir)
	entries, lerr := checklog.LoadAllAll(root)
	if lerr != nil {
		fmt.Fprintf(os.Stderr, "[forge] warning: checklog 读取不全（审计口径可能低估）: %v\n", lerr)
	}
	var out []taskAudit
	for _, st := range done[:n] {
		a := taskAudit{TaskRef: st.TaskRef, SessionID: st.SessionID}
		// 空 SessionID 的任务（legacy/未注入宿主）不做 join（F3）：匿名计数器与
		// 匿名 wild 申报不可归属，挂到任何任务头上都是误报——遥测保持 0 并注明。
		if st.SessionID != "" {
			a.Ignores = ignores[st.SessionID]
			a.WildCount = wild[st.SessionID]
		}
		if st.Score != nil {
			a.ScoreOverall = st.Score.Overall
		}
		for _, e := range entries {
			if e.TaskRef != st.TaskRef || string(e.Check) != "task-guard" {
				continue
			}
			switch e.EffectiveLevel() {
			case checklog.LevelAdvisory:
				a.Advisories++
			case checklog.LevelBlocked:
				a.Blocked++
			}
		}
		a.Verdict = sampleVerdict(a)
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].TaskRef < out[j].TaskRef })
	return out
}

// sampleVerdict 给采样一条人话结论：无视计数>0 而任务照常完成 = "advisory 被无视
// 但成功"样本——正是"无灾≠安全"强制复盘的目标人群。
func sampleVerdict(a taskAudit) string {
	switch {
	case a.Ignores >= 2 && a.Blocked == 0:
		return "无视升档仍完成且从未被阻断——无灾≠安全：强制复盘该任务的产出"
	case a.Ignores > 0:
		return "有无视记录——抽查产出是否真在任务覆盖内"
	case a.WildCount > 0:
		return "有野外申报——核对申报说明与实际改动相符"
	default:
		return "遥测干净"
	}
}

// readMarkerCounts 扫 markers/ 下带前缀的计数文件，返回 session→计数。
func readMarkerCounts(dataDir, prefix string) map[string]int {
	out := map[string]int{}
	entries, err := os.ReadDir(filepath.Join(dataDir, "markers"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dataDir, "markers", name))
		if err != nil {
			continue
		}
		n := 0
		fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &n)
		out[strings.TrimPrefix(name, prefix)] = n
	}
	return out
}

// countWildAll / countWildBySession 读野外申报（会话维度聚合供采样 join）。
func countWildAll(dataDir string) int { return len(countWildBySessionLines(dataDir, false)) }
func countWildBySession(dataDir string) map[string]int {
	m := map[string]int{}
	for _, s := range countWildBySessionLines(dataDir, true) {
		m[s]++
	}
	return m
}

func countWildBySessionLines(dataDir string, withSession bool) []string {
	data, err := os.ReadFile(filepath.Join(dataDir, "wild", "declarations.jsonl"))
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		var e wildDeclaration
		if json.Unmarshal([]byte(line), &e) != nil {
			continue // 残行容错（并发追加撕裂）——审计消费侧不炸
		}
		if withSession {
			out = append(out, e.Session)
		} else {
			out = append(out, line)
		}
	}
	return out
}

func shortSession(sid string) string {
	if len(sid) > 12 {
		return sid[:12] + "…"
	}
	if sid == "" {
		return "(无会话身份)"
	}
	return sid
}
