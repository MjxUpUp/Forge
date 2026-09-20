package taskpipeline

// independence.go — delivery-hardening 价-1b：审查独立性归因。会话取证
// （sess_cbe4047c）实证：review pass 的 stamp 可由跨会话/无人来源写入、
// doc-review 分数可由生产者自录（9 分被拦后自己录 92 分过关）——「产出者不能
// 自检」只在 skill 文本里，代码零校验。本文件给独立性一个机械判据：
// **盖章 session ∈ 生产者 session 集 = 自审**。生产者集来自 toollog（本任务
// 的 Write/Edit 调用归属会话）。自审不禁止（同宿主子代理共用 session id 的
// 形态下会误伤），但被打上 self-review 标记：落独立 WARN 披露行（self-review）、
// report 审查行显示「含自审轮」、doc-gate 不消费自录分数——披露定价，不造墙。

import (
	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"strings"
)

// ProducerSessions returns the set of sessions that authored changes for the
// task (Write/Edit tool calls, plus Bash calls carrying write-file patterns —
// review P1-5 hardening: `cat > f`, heredocs, `sed -i` bypass Write/Edit
// attribution while toollog already records the Bash command text). Empty set
// = no producer telemetry (host didn't wire tool-track) — independence checks
// degrade to "unknown", fail-open.
//
// ProducerSessions 返回任务的改动作者会话集（toollog 里归属本任务的
// Write/Edit 调用 + 含写文件模式的 Bash 调用——审查 P1-5 加固：`cat > f`、
// heredoc、`sed -i` 绕过 Write/Edit 归因，而 toollog 已记录 Bash 命令文本，
// 把写形态的 Bash 行计入生产者集是低成本补口）。空集 = 无生产者遥测（宿主
// 未接 tool-track）——独立性判定降级 unknown，fail-open。
func ProducerSessions(root, taskRef string) map[string]bool {
	producers := map[string]bool{}
	calls, err := toolusage.LoadForTaskAll(root, taskRef)
	if err != nil {
		return producers
	}
	for _, c := range calls {
		isProducer := c.ToolName == "Write" || c.ToolName == "Edit" ||
			(c.ToolName == "Bash" && bashWritesFiles(c.ToolInput))
		if isProducer && c.SessionID != "" {
			producers[c.SessionID] = true
		}
	}
	return producers
}

// bashWritesFiles 报告 Bash 命令文本是否携带【写文件】模式（复审 P1-5 收紧：
// `>` 误伤 `2>&1`/`>&` 只读重定向、`dd ` 误伤 `git add `——误报会把独立
// reviewer 判成生产者，而 SelfReview 在 docgate 是硬拒路径）。匹配面：
// >>/<</sed -i/tee/truncate/词边界 dd 直命中；裸 `>` 经"剥只读形态后测残留"
// 处理（见函数尾块）——覆盖写计入、2>&1 合并不计。
func bashWritesFiles(input string) bool {
	for _, pat := range []string{">>", "<<", "sed -i", "tee ", "truncate "} {
		if strings.Contains(input, pat) {
			return true
		}
	}
	// 词边界 dd（dd——"git add " 不命中）。
	for _, f := range strings.Fields(input) {
		if f == "dd" {
			return true
		}
	}
	// 覆盖写 `>`：剥掉只读形态（2>&1 / 2> / >& / &> / >>）后仍残留 > 即写
	//（二轮复审收尾：`cat > f` 是取证主形态必须命中；裸扫 `>` 会把 go test
	// 2>&1 误判成生产者——误报方向落在 docgate 硬拒路径）。
	out := input
	for _, ro := range []string{"2>&1", "2>", ">&", "&>", ">>"} {
		out = strings.ReplaceAll(out, ro, " ")
	}
	return strings.Contains(out, ">")
}

// ReviewIndependence reports whether the stamping session is independent of
// the producers. ok=false only when producer telemetry EXISTS and the stamper
// is in it (deterministic self-review detection); unknown telemetry → ok=true
// with Known=false (fail-open, disclosed).
//
// ReviewIndependence 报告盖章会话是否独立于生产者集。仅当生产者遥测存在且
// 盖章者在其中时 ok=false（确定性的自审检出）；遥测缺失 → ok=true 且
// Known=false（fail-open，如实标注）。
func ReviewIndependence(root, taskRef, stamperSession string) (ok, known bool) {
	producers := ProducerSessions(root, taskRef)
	if len(producers) == 0 {
		return true, false
	}
	if stamperSession == "" {
		// 盖章侧无会话归属（非 hook 路径）——无法归因，按未知放行（披露在行上）。
		return true, false
	}
	return !producers[stamperSession], true
}

// RecordSelfReviewRow 落自审披露行（review-pass 的伴随行；不改变 pass 事实，
// 只把「同会话既写又审」变成可审计事件）。
func RecordSelfReviewRow(root, taskRef, sessionID, kind string) {
	recordAudit(root, &checklog.Entry{
		Check:   CheckNameSelfReview,
		Passed:  true, // 披露事件，非检查结论
		Checked: true,
		Level:   checklog.LevelWarn,
		TaskRef: taskRef,
		Detail:  "self-review: 盖章会话与改动生产者相同（" + kind + "）——独立性缺失被披露而非拒绝；验收方在 report 可见",
		Meta:    map[string]string{"kind": kind, "self_review": "true"},
	})
}

// CheckNameSelfReview 是自审披露行名（taskpipeline 字面量，观察类）。
const CheckNameSelfReview checklog.CheckName = "self-review"

// HasSelfReviewRow reports whether the task has a self-review disclosure row
// （report 消费：审查行渲染「自审」标记）。
func HasSelfReviewRow(root, taskRef, kind string) bool {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Check == CheckNameSelfReview && e.Meta["kind"] == kind {
			return true
		}
	}
	return false
}
