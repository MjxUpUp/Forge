package change

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// MaxSameCauseAttempts 是"同一个原因连续失败几次就停下交给人"的阈值。
//
// 这是回环契约的停止条件：没有它，循环会一直在同一个失败上重试（每次重试都
// 烧 token），而"重试"和"换一个思路"在日志里长得一模一样。达到阈值不是
// 失败判据，是请求判断——状态输出会明确说"停下"，继续与否由人决定。
const MaxSameCauseAttempts = 3

// Task 是 tasks.md 里的一条待办。
type Task struct {
	Index int
	Text  string
	Done  bool
	// Criteria 是这条任务声明的判据序号（`判据：acceptance #2` 里的 2）。
	// 空表示这条任务没写判据，无法单独 gate。
	Criteria []int
}

var (
	taskLineRE  = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s+(.*\S)\s*$`)
	criterionRE = regexp.MustCompile(`#(\d+)`)
)

// ParseTasks 解析 tasks.md 的勾选行。
//
// 只认 `- [ ]` / `- [x]`：说明性文字和标题不进列表，避免把文档里的示例当成
// 待办。判据序号只在"判据"标记之后提取（`判据：acceptance #1`），这样任务
// 标题里出现 #号 不会被误读。
func ParseTasks(raw []byte) []Task {
	var out []Task
	for _, line := range strings.Split(string(raw), "\n") {
		m := taskLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		t := Task{
			Index: len(out) + 1,
			Done:  strings.EqualFold(m[1], "x"),
			Text:  m[2],
		}
		if i := strings.LastIndex(t.Text, "判据"); i >= 0 {
			for _, n := range criterionRE.FindAllStringSubmatch(t.Text[i:], -1) {
				if v, err := strconv.Atoi(n[1]); err == nil {
					t.Criteria = appendUniqueInt(t.Criteria, v)
				}
			}
		}
		out = append(out, t)
	}
	return out
}

func appendUniqueInt(s []int, v int) []int {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// Attempt 是一次落盘的验证记录（evidence/<ts>）。
type Attempt struct {
	Dir          string
	StartedAt    string
	FinishedAt   string
	Passed       bool
	Sealed       bool
	Task         int
	Fast         bool
	Checks       []Check
	FirstFailure *Check
}

// Signature 是失败原因的指纹：第一条失败命令。
//
// 用它判断"是不是同一个原因"——两次失败的第一条命令相同，基本可以认定循环
// 在原地打转；命令不同说明尝试过别的路，那是进展，不该被阈值拦住。
func (a Attempt) Signature() string {
	if a.FirstFailure == nil {
		return ""
	}
	return a.FirstFailure.Command
}

// Attempts 从新到旧列出这个 change 的全部验证记录（含单任务 gate）。
func Attempts(c *Change) []Attempt {
	evDir := filepath.Join(c.Dir, EvidenceDirName)
	entries, err := os.ReadDir(evDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && evidenceNameRE.MatchString(e.Name()) &&
			exists(filepath.Join(evDir, e.Name(), SummaryFile)) {
			names = append(names, e.Name())
		}
	}
	sort.Slice(names, func(i, j int) bool { return lessEvidenceName(names[i], names[j]) })

	var out []Attempt
	for _, name := range names {
		dir := filepath.Join(evDir, name)
		sum, err := loadSummary(dir)
		if err != nil || sum.Change != c.ID {
			continue
		}
		a := Attempt{
			Dir:        dir,
			StartedAt:  sum.StartedAt,
			FinishedAt: sum.FinishedAt,
			Passed:     sum.Passed,
			Sealed:     sum.Sealed,
			Task:       sum.Task,
			Fast:       sum.Fast,
			Checks:     sum.Checks,
		}
		for i := range sum.Checks {
			if sum.Checks[i].Exit != 0 {
				a.FirstFailure = &sum.Checks[i]
				break
			}
		}
		out = append(out, a)
	}
	// 新的在前：状态输出先说最近一次发生了什么。
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// NodeState 是单条任务的状态。
type NodeState string

const (
	NodePending NodeState = "pending"
	NodePassed  NodeState = "passed"
	NodeFailed  NodeState = "failed"
	NodeBlocked NodeState = "blocked"
)

// NodeStatus 是一条任务的当前状态与它的最近一次证据。
type NodeStatus struct {
	Task         Task      `json:"task"`
	State        NodeState `json:"state"`
	LastError    string    `json:"last_error,omitempty"`
	LastEvidence string    `json:"last_evidence,omitempty"`
	Attempts     int       `json:"attempts"`
}

// Status 是"下一步做什么"的唯一来源。
//
// agent 不该自己挑下一步：它只看这里给出的 next_steps，逐条执行。判断标准
// 全部来自文件与证据，不来自对话记忆。
type Status struct {
	ID          string       `json:"id"`
	Stage       string       `json:"stage"`
	Fast        bool         `json:"fast"`
	Verdict     string       `json:"verdict"` // clean / action / blocked
	Blocked     string       `json:"blocked,omitempty"`
	NextSteps   []string     `json:"next_steps"`
	Nodes       []NodeStatus `json:"nodes,omitempty"`
	Attempts    int          `json:"attempts"`
	LastVerdict string       `json:"last_verdict,omitempty"` // sealed / failed / stale / none
}

// StatusOf 推导一个 change 的当前状态、判定与下一步。
func StatusOf(root, id string) (*Status, error) {
	c, _, err := Load(root, id)
	if err != nil {
		return nil, err
	}
	atts := Attempts(c)
	st := &Status{
		ID:       id,
		Stage:    string(c.Stage),
		Fast:     c.Fast,
		Verdict:  "action",
		Attempts: len(fullAttempts(atts)),
		Nodes:    nodeStatuses(c, atts),
	}
	switch {
	case c.Sealed:
		st.LastVerdict = "sealed"
	case c.Stale:
		st.LastVerdict = "stale"
	case c.Failed:
		st.LastVerdict = "failed"
	default:
		st.LastVerdict = "none"
	}

	// 整条 change 的停止条件：连续同因失败达到阈值。
	if sig, n := repeatedFailure(fullAttempts(atts)); n >= MaxSameCauseAttempts {
		st.Verdict = "blocked"
		st.Blocked = fmt.Sprintf("最近 %d 次验证都卡在同一条命令上：%s", n, truncate(sig, 120))
	}
	// 节点耗尽同样要停：某条任务反复卡在同一个判据上，继续在整条 verify 里
	// 重试只是换个地方重复烧钱。
	if st.Verdict != "blocked" {
		for _, n := range st.Nodes {
			if n.State == NodeBlocked {
				st.Verdict = "blocked"
				st.Blocked = fmt.Sprintf("任务 %d 连续 %d 次卡在同一个判据上：%s", n.Task.Index, MaxSameCauseAttempts, n.LastError)
				break
			}
		}
	}

	st.NextSteps = nextSteps(c, st)
	return st, nil
}

func fullAttempts(atts []Attempt) []Attempt {
	var out []Attempt
	for _, a := range atts {
		if a.Task == 0 {
			out = append(out, a)
		}
	}
	return out
}

// repeatedFailure 数"最新连续几次失败是同一个原因"。
func repeatedFailure(atts []Attempt) (string, int) {
	sig := ""
	n := 0
	for _, a := range atts {
		if a.Passed {
			break
		}
		s := a.Signature()
		if n == 0 {
			sig, n = s, 1
			continue
		}
		if s != sig {
			break
		}
		n++
	}
	return sig, n
}

func nodeStatuses(c *Change, atts []Attempt) []NodeStatus {
	raw, err := os.ReadFile(filepath.Join(c.Dir, FileTasks))
	if err != nil {
		return nil
	}
	tasks := ParseTasks(raw)
	out := make([]NodeStatus, 0, len(tasks))
	for _, t := range tasks {
		ns := NodeStatus{Task: t, State: NodePending}
		if t.Done {
			ns.State = NodePassed
		}
		var failed []Attempt
		for _, a := range atts {
			if a.Task != t.Index {
				continue
			}
			ns.Attempts++
			if ns.LastEvidence == "" {
				ns.LastEvidence = a.Dir
			}
			if a.Passed {
				if !t.Done {
					ns.State = NodePassed
				}
				break
			}
			failed = append(failed, a)
		}
		if len(failed) > 0 && !t.Done {
			ns.State = NodeFailed
			ns.LastError = failureBrief(failed[0])
			if sig, n := repeatedFailure(failed); n >= MaxSameCauseAttempts {
				ns.State = NodeBlocked
				ns.LastError = fmt.Sprintf("最近 %d 次卡在同一条命令：%s", n, truncate(sig, 120))
			}
		}
		out = append(out, ns)
	}
	return out
}

func failureBrief(a Attempt) string {
	if a.FirstFailure == nil {
		return "没有实跑的判据"
	}
	return truncate(a.FirstFailure.Command, 120)
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// nextSteps 给出"接下来该敲什么"。
//
// 顺序即优先级：产物不全先补产物，判据没跑过先跑判据，判据失效先重跑，
// 失败先看证据，全过了才谈归档。blocked 时不给出任何新命令——那是给人的。
//
// 参数 c 由调用方 Load 得到，避免重复读盘。
func nextSteps(c *Change, st *Status) []string {
	if st.Verdict == "blocked" {
		return []string{
			"停下：同一个原因已经失败 " + strconv.Itoa(MaxSameCauseAttempts) + " 次，继续重试只会重复烧钱。",
			"看最近一次证据：" + relTo(c.Dir, latestEvidenceDir(c)),
			"改判据或改实现后，由人重新跑步：「forge change verify " + c.ID + "」。",
		}
	}

	required := []string{FileProposal, FileDesign, FileTasks, FileAcceptance}
	if c.Fast {
		required = []string{FileProposal, FileAcceptance}
	}
	var missing []string
	for _, name := range required {
		raw, err := os.ReadFile(filepath.Join(c.Dir, name))
		if err != nil {
			missing = append(missing, name)
			continue
		}
		if strings.Contains(string(raw), scaffoldMarker) {
			missing = append(missing, name+"（还带着模板标记）")
		}
	}
	if len(missing) > 0 {
		return []string{
			"补齐产物：" + strings.Join(missing, "、"),
			"填完后删掉文件里的 " + scaffoldMarker + " 标记。",
		}
	}

	if !c.HasAcceptance {
		return []string{"补 " + FileAcceptance + "：至少要有一条能跑的判据。"}
	}
	switch {
	case c.Sealed:
		if c.Stale {
			return []string{"判据或工作区变过，证据已失效：forge change verify " + c.ID}
		}
		steps := []string{"归档：git mv " + relTo(filepath.Dir(c.Dir), c.Dir) + " spec/changes/archive/" + archiveNameSuggestion(c.ID)}
		for _, n := range st.Nodes {
			if n.State != NodePassed {
				steps = append(steps, "还有未过节点的记录："+nodeStep(c.ID, n))
			}
		}
		return steps
	case c.Stale:
		return []string{"判据或工作区变过，证据已失效：forge change verify " + c.ID}
	case c.Failed:
		return []string{
			"最近一次验证没通过，先读证据：" + relTo(c.Dir, latestEvidenceDir(c)),
			"修实现或修判据，然后重跑：forge change verify " + c.ID,
		}
	}

	steps := []string{"还没有验证记录：forge change verify " + c.ID}
	if len(st.Nodes) > 0 {
		steps = append(steps, "想逐节点推进就按 tasks.md 的判据跑：forge change gate "+c.ID+" --task <N>")
	}
	return steps
}

func nodeStep(id string, n NodeStatus) string {
	if n.Attempts == 0 {
		return "节点 " + strconv.Itoa(n.Task.Index) + " 还没跑过：forge change gate " + id + " --task " + strconv.Itoa(n.Task.Index)
	}
	return "节点 " + strconv.Itoa(n.Task.Index) + " 上次没通过（" + n.LastError + "）：forge change gate " + id + " --task " + strconv.Itoa(n.Task.Index)
}

func latestEvidenceDir(c *Change) string {
	atts := Attempts(c)
	if len(atts) == 0 {
		return filepath.Join(c.Dir, EvidenceDirName)
	}
	return atts[0].Dir
}

func archiveNameSuggestion(id string) string {
	return "YYYYMMDD-" + id
}

// WriteJSON 把状态写成 JSON（CLI --json 用）。
func (s *Status) WriteJSON(w interface{ Write([]byte) (int, error) }) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = w.Write(raw)
	return err
}
