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
	"time"
)

// MaxSameCauseAttempts 是"同一个原因连续失败几次就停下交给人"的阈值。
//
// 这是回环契约的停止条件：没有它，循环会一直在同一个失败上重试（每次重试都
// 烧 token），而"重试"和"换一个思路"在日志里长得一模一样。达到阈值不是
// 失败判据，是请求判断——状态输出会明确说"停下"，继续与否由人决定。
const MaxSameCauseAttempts = 3

// Task 是 tasks.md 里的一条待办。
type Task struct {
	Index int    `json:"index"`
	Text  string `json:"text"`
	// Done 是人在 tasks.md 里勾的进度，不产生证据，也不改变节点状态。
	Done bool `json:"done"`
	// Criteria 是这条任务声明的判据序号（`判据：acceptance #2` 里的 2）。
	// 空表示这条任务没写判据，无法单独 gate。
	Criteria []int `json:"criteria,omitempty"`
}

var (
	taskLineRE = regexp.MustCompile(`^\s*[-*]\s+\[([ xX])\]\s+(.*\S)\s*$`)
	// 判据必须是 `判据：acceptance #N`（允许中文冒号；多个序号用顿号/逗号/斜杠/「和」「与」「及」分隔）。
	// 早先只要出现"判据"就把后面所有 #N 都算上，`修改判据 #3 的文档` 会被误读。
	// 取「acceptance」到句末（或分号）的整段再提号：只认固定分隔符的话，
	// 「#1 或 #2」「#1 以及 #2」「（#2）」这类写法会静默丢号——gate 只跑
	// #1 却算通过，比直接报错更糟。
	criterionRE = regexp.MustCompile(`判据\s*[:：]\s*acceptance\s+([^；;\n]*)`)
	numberRE    = regexp.MustCompile(`#(\d+)`)
)

// ParseTasks 解析 tasks.md 的勾选行。
//
// 只认 `- [ ]` / `- [x]`：说明性文字和标题不进列表，避免把文档里的示例当成
// 待办。判据序号只在"判据"标记之后提取（`判据：acceptance #1`），这样任务
// 标题里出现 #号 不会被误读。
func ParseTasks(raw []byte) []Task {
	out, openAt := parseTaskLines(strings.Split(string(raw), "\n"))
	if openAt >= 0 {
		// 围栏没闭合：按原样会把后面所有任务当成示例吞掉，gate/status 会
		// 认为"没有任务"。围栏是给人看的，宁可把那一行去掉重扫一遍。
		lines := strings.Split(string(raw), "\n")
		lines[openAt] = ""
		out, _ = parseTaskLines(lines)
	}
	return out
}

// parseTaskLines 返回任务列表；若文件结束时仍停在围栏里，openAt 是那条
// 未闭合围栏的行号（否则 -1）。
func parseTaskLines(lines []string) (out []Task, openAt int) {
	openAt = -1
	var fenceChar byte
	fence := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if ch, width, bare := fenceRun(trimmed); width > 0 {
			switch {
			case fence == 0:
				// 收尾围栏后面不能再带语言名：```go 是示例内容，不是收尾。
				fenceChar, fence, openAt = ch, width, i
			case ch == fenceChar && bare && width >= fence:
				fenceChar, fence, openAt = 0, 0, -1
			}
			continue
		}
		if fence > 0 {
			continue
		}
		m := taskLineRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		t := Task{
			Index: len(out) + 1,
			Done:  strings.EqualFold(m[1], "x"),
			// 模板里任务本身带「1.」编号；保留原文会让界面打印成「1. 1. …」。
			Text: trimTaskNumber(m[2], len(out)+1),
		}
		for _, c := range criterionRE.FindAllStringSubmatch(t.Text, -1) {
			for _, n := range numberRE.FindAllStringSubmatch(c[1], -1) {
				if v, err := strconv.Atoi(n[1]); err == nil {
					t.Criteria = appendUniqueInt(t.Criteria, v)
				}
			}
		}
		out = append(out, t)
	}
	return out, openAt
}

// fenceRun 判断一行是不是围栏标记：连续 3 个以上 ` 或 ~。
// bare 表示标记之后没有别的字符——只有这种才是收尾围栏。
func fenceRun(trimmed string) (ch byte, width int, bare bool) {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	ch = trimmed[0]
	for width < len(trimmed) && trimmed[width] == ch {
		width++
	}
	if width < 3 {
		return 0, 0, false
	}
	return ch, width, strings.TrimSpace(trimmed[width:]) == ""
}

// trimTaskNumber 去掉任务正文开头与本条序号相同的编号（「1. 」「1、」「1)」）。
// 只认与 index 一致的数字，避免把「2.0 兼容」这类正文误伤。
func trimTaskNumber(text string, index int) string {
	prefix := strconv.Itoa(index)
	var rest string
	switch {
	case strings.HasPrefix(text, prefix+".") && !startsWithDigit(text[len(prefix)+1:]):
		rest = text[len(prefix)+1:]
	case strings.HasPrefix(text, prefix+"、"):
		rest = text[len(prefix)+len("、"):]
	case strings.HasPrefix(text, prefix+")"):
		rest = text[len(prefix)+1:]
	default:
		return text
	}
	return strings.TrimSpace(rest)
}

func startsWithDigit(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
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
	Passed       bool
	Sealed       bool
	Task         int
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
			Dir:    dir,
			Passed: sum.Passed,
			Sealed: sum.Sealed,
			Task:   sum.Task,
			Checks: sum.Checks,
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

// PriorAttempt 是一次没通过的验证记录（新→旧）。
//
// 它是"死路记忆"的读侧：返工的人先读这些命令和退出码，避免重试已经证伪的
// 路。只列失败的尝试——通过的记录不是死路，读它没有返工价值。
type PriorAttempt struct {
	// Evidence 是证据目录名（evidence/<ts>），失败命令的 stdout/stderr 就在其中。
	Evidence string `json:"evidence"`
	// Task 非零表示这是一次单节点 gate。
	Task    int    `json:"task,omitempty"`
	Command string `json:"failed_command,omitempty"`
	Exit    int    `json:"exit,omitempty"`
	Shell   string `json:"shell,omitempty"`
	// TimedOut 为真时 Exit 没有意义：命令是被超时终止的，不是自己退出的。
	TimedOut bool `json:"timed_out,omitempty"`
	// StderrFile/StdoutFile 是证据目录里的文件名（空表示该管道没有输出）。
	StderrFile string `json:"stderr_file,omitempty"`
	StdoutFile string `json:"stdout_file,omitempty"`
	// Reason 在没有可定位的失败命令时说明原因（证据残缺、或判据都过了却没封印）。
	Reason string `json:"reason,omitempty"`
}

// priorAttempts 从失败记录里取最近的几条（新→旧），最多 limit 条。
func priorAttempts(atts []Attempt, limit int) []PriorAttempt {
	var out []PriorAttempt
	for _, a := range atts {
		if a.Passed {
			continue
		}
		pa := PriorAttempt{
			Evidence: filepath.Base(a.Dir),
			Task:     a.Task,
		}
		if f := a.FirstFailure; f != nil {
			pa.Command, pa.Shell = f.Command, f.Shell
			if f.TimedOut {
				// 超时的退出码是杀人灭口留下的，不该当成命令自己的结果。
				pa.TimedOut = true
			} else {
				pa.Exit = f.Exit
			}
			pa.StdoutFile, pa.StderrFile = f.StdoutFile, f.StderrFile
		} else {
			pa.Reason = "没有可定位的失败命令（证据残缺，或判据都过了却没封印）"
		}
		out = append(out, pa)
		if len(out) == limit {
			break
		}
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
	// NodeDone 表示人勾了 [x] 但没有 gate 证据。勾选是进度记录，不是判据，
	// 所以它不能把任务变成 passed，也不能清掉 blocked。
	NodeDone NodeState = "done"
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
	ID      string `json:"id"`
	Stage   string `json:"stage"`
	Fast    bool   `json:"fast"`
	Verdict string `json:"verdict"` // clean / action / blocked
	Blocked string `json:"blocked,omitempty"`
	// Handoff 是给"决定继续的那个人"看的一句话。它不进 next_steps：循环只执行
	// next_steps，可执行的恢复命令放进那里就会被 agent 照跑。
	Handoff     string       `json:"handoff,omitempty"`
	NextSteps   []string     `json:"next_steps"`
	Nodes       []NodeStatus `json:"nodes,omitempty"`
	Attempts    int          `json:"attempts"`
	LastVerdict string       `json:"last_verdict,omitempty"` // sealed / failed / stale / none
	// PriorAttempts 是最近失败验证的摘要（新→旧）。循环停下或返工时先读它：
	// 同样的命令、同样的退出码，重跑一遍只是再烧一次 token。
	PriorAttempts []PriorAttempt `json:"prior_attempts,omitempty"`
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
	// 已封印的 change 不再需要死路记忆：封印是终态，历史失败只作记录。
	if !c.Sealed {
		st.PriorAttempts = priorAttempts(atts, 10)
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

	// 已封印是终态：整条判据都过了，节点上的历史失败只作记录，不再拦人。
	// 产物被删是例外——封印只保证判据跑过，不代表四份文件还在；缺文件时
	// 归档会把残缺的目录搬走，所以这里改回 action 让人先补齐。
	if c.Sealed {
		if missing := DocProblems(c.Dir, c.Fast); len(missing) > 0 {
			st.Verdict = "action"
			st.NextSteps = []string{
				"补齐被删掉的产物：" + strings.Join(missing, "、"),
				"补齐后再归档；封印本身仍然有效。",
			}
			return st, nil
		}
		st.Verdict = "clean"
		st.NextSteps = nextSteps(root, c, st)
		return st, nil
	}

	// 整条 change 的停止条件：连续同因失败达到阈值。
	if sig, n := repeatedFailure(fullAttempts(atts)); n >= MaxSameCauseAttempts {
		st.Verdict = "blocked"
		st.Blocked = fmt.Sprintf("最近 %d 次验证都卡在同一条命令上：%s", n, truncate(sig, 120))
		st.Handoff = fmt.Sprintf("确认要继续，就先改判据或改实现，再由人跑：forge change verify %s", id)
	}
	// 节点耗尽同样要停：某条任务反复卡在同一个判据上，继续在整条 verify 里
	// 重试只是换个地方重复烧钱。
	if st.Verdict != "blocked" {
		for _, n := range st.Nodes {
			if n.State == NodeBlocked {
				st.Verdict = "blocked"
				st.Blocked = fmt.Sprintf("任务 %d 连续 %d 次卡在同一个判据上：%s", n.Task.Index, MaxSameCauseAttempts, n.LastError)
				if n.Task.Done {
					// 勾选过的任务 gate 会拒绝重跑（见 gate.go 的前置检查）：
					// 让它去跑 gate 是死路。要么取消勾选，要么整条 verify。
					st.Handoff = fmt.Sprintf("任务 %d 已在 tasks.md 里勾选，gate 会拒绝重跑；先改判据或实现，再取消勾选或新起一条任务，或者直接由人跑：forge change verify %s", n.Task.Index, id)
				} else {
					st.Handoff = fmt.Sprintf("确认要继续，就改这一条的判据或实现，再由人跑：forge change gate %s --task %d", id, n.Task.Index)
				}
				break
			}
		}
	}

	st.NextSteps = nextSteps(root, c, st)
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
			ns.State = NodeDone
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
				ns.State = NodePassed
				break
			}
			failed = append(failed, a)
		}
		if len(failed) > 0 {
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
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// nextSteps 给出"接下来该敲什么"。
//
// 顺序即优先级：产物不全先补产物，判据没跑过先跑判据，判据失效先重跑，
// 失败先看证据，全过了才谈归档。blocked 时不给出任何新命令——那是给人的。
//
// 参数 c 由调用方 Load 得到，避免重复读盘。
func nextSteps(root string, c *Change, st *Status) []string {
	if st.Verdict == "blocked" {
		// 这里刻意不给命令：status 是循环唯一的下一步来源，写了什么 agent 就会
		// 照跑。恢复要人决定，命令放在 Handoff 里（CLI 单独一段输出）。
		return []string{
			"已停下：同一个原因连续失败 " + strconv.Itoa(MaxSameCauseAttempts) + " 次。",
			"证据：" + relTo(root, latestEvidenceDir(c)),
			"先读上面那条证据，再决定是改判据还是改实现；不要在循环里重试。",
		}
	}

	if missing := DocProblems(c.Dir, c.Fast); len(missing) > 0 {
		return []string{
			"补齐产物：" + strings.Join(missing, "、"),
			"填完后删掉" + scaffoldHint + "。",
		}
	}

	if !c.HasAcceptance {
		return []string{"补 " + FileAcceptance + "：至少要有一条能跑的判据。"}
	}
	switch {
	case c.Sealed:
		// 基准是 spec/changes 的上一层（仓库根），否则命令里的路径在哪个 cwd
		// 下都不对。归档只看整条 verify 的判据；未过节点只作记录，不拦归档。
		src := relTo(root, c.Dir)
		return []string{"归档：git mv " + src + " spec/changes/archive/" + archiveNameSuggestion(c.ID)}
	case c.Stale:
		return []string{"判据或工作区变过，证据已失效：forge change verify " + c.ID}
	case c.Failed:
		return []string{
			"最近一次验证没通过，先读证据：" + relTo(root, latestEvidenceDir(c)),
			"修实现或修判据，然后重跑：forge change verify " + c.ID,
		}
	}

	steps := []string{"还没有验证记录：forge change verify " + c.ID}
	if len(st.Nodes) > 0 {
		steps = append(steps, "想逐节点推进就按 tasks.md 的判据跑：forge change gate "+c.ID+" --task <N>")
	}
	return steps
}

func latestEvidenceDir(c *Change) string {
	atts := Attempts(c)
	if len(atts) == 0 {
		return filepath.Join(c.Dir, EvidenceDirName)
	}
	return atts[0].Dir
}

// archiveNameSuggestion 给出归档目录名。用今天的日期，命令可以直接复制执行；
// 占位的 YYYYMMDD 看起来像提醒，实际会让人敲出一条必错的命令。
func archiveNameSuggestion(id string) string {
	return time.Now().Format("20060102") + "-" + id
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
