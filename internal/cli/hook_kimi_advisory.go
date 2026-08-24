// kimi advisory pending-queue: kimi 0.35.0 delivers hook stdout to the model
// ONLY on UserPromptSubmit (wire.jsonl-verified, internal/agentbridge/kimi-hook-routing.md),
// and reads ANY PreToolUse stdout as a deny (blocks the tool call, edit never
// lands). So an advisory (passed=true, non-empty detail) fired on
// PreToolUse/PostToolUse/Stop/SessionStart has NO safe immediate channel: print
// it and the edit is denied with an "allowed"-worded reason (the 2026-08 P0
// promotion's self-contradiction), stay silent and the signal evaporates
// (production checklog: 100% of kimi/no-channel advisories lost — 41
// skill-trigger + 1 test-nudge in two weeks). This file is the third option:
// queue the advisory per-project (advisories-pending.jsonl under the forge
// DataDir) and drain the queue as ONE batched injection on the next
// UserPromptSubmit — the one channel kimi carries into model context.
//
// Blocking results (passed=false: read-before-edit, hazard-guard, freeze-guard)
// never touch this file — they are designed denies and still ride exit 2.
// Other hosts never reach the queue either: emitAdvisoryRouted (below) is the
// single entry point for every hook emission, and it delegates to
// emitAgentOutput unless agent=="kimi" AND passed — the kimi/allow gate lives
// INSIDE that router, not at the call sites.
//
// kimi advisory pending-queue：kimi 0.35.0 只在 UserPromptSubmit 把 hook stdout
// 送达模型（wire.jsonl 实证，见 internal/agentbridge/kimi-hook-routing.md），且把
// PreToolUse 上**任何** stdout 当 deny（阻断工具调用、编辑不落盘）。故
// PreToolUse/PostToolUse/Stop/SessionStart 上产生的 advisory（passed=true、
// detail 非空）没有安全的即时通道：打印则编辑被「allowed」文案的 deny 拦下
// （2026-08 P0 提升的自相矛盾），静默则信号蒸发（生产 checklog：kimi/no-channel
// advisory 100% 丢失——两周 41 条 skill-trigger + 1 条 test-nudge）。本文件给出
// 第三条路：advisory 按项目入队（forge DataDir 下 advisories-pending.jsonl），
// 下次 UserPromptSubmit 时把队列攒成**一条**注入——kimi 唯一送进模型上下文的
// 通道。
//
// 阻断结果（passed=false：read-before-edit、hazard-guard、freeze-guard）永不经过
// 本文件——它们是设计内的 deny，仍走 exit 2。其他宿主也不会到达队列：
// emitAdvisoryRouted（见下）是所有 hook 输出的唯一入口，除非 agent=="kimi" 且
// passed，否则一律委派 emitAgentOutput——kimi/allow 判定在路由器**内部**，
// 而非各调用处。
package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/util"
)

const (
	// kimiAdvisoryQueueFile is the per-project pending queue under the forge
	// DataDir (~/.forge/projects/<key>/).
	//
	// kimiAdvisoryQueueFile 是 forge DataDir（~/.forge/projects/<key>/）下的
	// per-project pending 队列文件名。
	kimiAdvisoryQueueFile = "advisories-pending.jsonl"

	// kimiAdvisoryDrainCap bounds one batched injection to the NEWEST few
	// advisories — a UserPromptSubmit injection competes with the user's own
	// prompt for attention, and anything older is likely already acted on or
	// stale (the hook that fired it re-fires if the condition persists).
	//
	// kimiAdvisoryDrainCap 把单次攒发限制在**最近**几条——UserPromptSubmit 注入
	// 要与用户自己的 prompt 争夺注意力，更旧的大概率已被处理或过期（条件仍在
	// 时产生它的 hook 会再触发）。
	kimiAdvisoryDrainCap = 5

	// kimiAdvisoryMaxTextLen caps one queued entry so a pathological detail
	// (a full compile log) cannot bloat the queue or the batched injection.
	//
	// kimiAdvisoryMaxTextLen 给单条入队文本设上限，防止病态 detail（整段编译
	// 日志）撑爆队列或攒发注入。
	kimiAdvisoryMaxTextLen = 2000

	// kimiAdvisoryMaxQueueBytes is the self-reset threshold: a session that
	// never submits a prompt (fully autonomous run) would otherwise grow the
	// queue forever. Past the threshold the file starts over — every entry in
	// it was by definition never drained, so the newest window is what matters.
	//
	// kimiAdvisoryMaxQueueBytes 是自重置阈值：从不提交 prompt 的会话（全自动
	// 运行）否则会让队列无限增长。超过阈值即重来——其中条目按定义都未被
	// drain 过，只有最近的窗口有价值。
	kimiAdvisoryMaxQueueBytes = 256 * 1024
)

// kimiAdvisoryEntry is one queued advisory line (JSONL).
//
// kimiAdvisoryEntry 是一条入队 advisory（JSONL 行）。
type kimiAdvisoryEntry struct {
	TS      string `json:"ts"`
	Hook    string `json:"hook"`
	Event   string `json:"event"`
	Session string `json:"session,omitempty"`
	Text    string `json:"text"`
}

// emitAdvisoryRouted is the single advisory-output router every hook emission
// call site uses in place of a bare emitAgentOutput. For agent!="kimi" or a
// block (passed=false) it IS emitAgentOutput — byte-identical behavior. For
// kimi allow-path output it applies the queue contract:
//   - UserPromptSubmit (kimi's one delivered channel): drain the pending queue
//     and PREPEND the batch to the hook's own detail (head placement — the
//     emitter truncates the TAIL at maxAdditionalContextLen; same F2 rationale
//     as prependKimiStaleAdvisory).
//   - every other event: enqueue the detail and stay SILENT (exit 0, no
//     stdout) — stdout on kimi PreToolUse is read as a deny, and on
//     PostToolUse/Stop/SessionStart it is dropped before reaching the model.
//
// emitAdvisoryRouted 是所有 hook 输出口径处替代裸 emitAgentOutput 的唯一
// advisory 输出路由器。agent!="kimi" 或阻断（passed=false）时它**就是**
// emitAgentOutput——逐字节行为一致。kimi 的 allow 路径输出走队列契约：
//   - UserPromptSubmit（kimi 唯一送达通道）：drain pending 队列并**前置**到
//     该 hook 自己的 detail（头部放置——emitter 在 maxAdditionalContextLen 截
//     **尾部**；与 prependKimiStaleAdvisory 同 F2 理由）。
//   - 其余事件：detail 入队并保持**静默**（exit 0、无 stdout）——kimi
//     PreToolUse 的 stdout 会被当 deny，PostToolUse/Stop/SessionStart 的
//     stdout 到不了模型。
func emitAdvisoryRouted(agent, eventName, hookName, root, sessionID string, passed bool, detail string) error {
	if agent != "kimi" || !passed {
		return emitAgentOutput(agent, eventName, hookName, passed, detail)
	}
	if eventName == "UserPromptSubmit" {
		batch := drainKimiAdvisories(root, sessionID)
		if batch != "" {
			if detail != "" {
				detail = batch + "\n" + detail
			} else {
				detail = batch
			}
		}
		return emitAgentOutput(agent, eventName, hookName, passed, detail)
	}
	if strings.TrimSpace(detail) != "" {
		enqueueKimiAdvisory(root, sessionID, hookName, eventName, detail)
	}
	return nil
}

// kimiAdvisoryQueueChannel is the checklog Channel label stamped on advisory
// records whose emission rode the pending queue instead of a live channel
// (kimi, any event whose allow-path channel does not deliver). Delivered stays
// false — nothing has reached the model YET — but the label lets the usage
// funnel distinguish "queued, drains on the next UserPromptSubmit" from
// "produced and lost forever" (kimi/no-channel, the 2026-08 false-prosperity
// class this queue fixes).
//
// kimiAdvisoryQueueChannel 是落在「输出经 pending 队列而非活通道」的 advisory
// 记录上的 checklog Channel 标签（kimi、allow 通道不可送达的事件）。Delivered
// 保持 false——尚无任何内容到达模型——但该标签让 usage 漏斗能区分「已入队、
// 下次 UserPromptSubmit 攒发」与「产生即永久丢失」（kimi/no-channel，本队列
// 要修的 2026-08 虚假繁荣类）。
const kimiAdvisoryQueueChannel = "kimi/advisory-queue"

// advisoryEmissionChannel returns the (delivered, channel) stamp for an
// advisory emission that went through emitAdvisoryRouted: identical to
// contextChannelDelivered except on kimi's non-delivered events, where the
// emission is queued for the UserPromptSubmit drain and the channel says so.
// Record sites that emit via emitAdvisoryRouted MUST stamp through this (not
// contextChannelDelivered) or the funnel keeps filing queued advisories under
// "lost forever".
//
// advisoryEmissionChannel 返回经 emitAdvisoryRouted 输出的 advisory 的
// （delivered, channel）章：与 contextChannelDelivered 一致，唯一例外是 kimi
// 不可送达事件——输出在那里入队待 UserPromptSubmit 攒发，channel 如实标注。
// 经 emitAdvisoryRouted 输出的记录点必须经本函数盖章（而非
// contextChannelDelivered），否则漏斗会继续把入队的 advisory 计入「永久丢失」。
func advisoryEmissionChannel(agent, eventName string) (bool, string) {
	delivered, channel := contextChannelDelivered(agent, eventName)
	if agent == "kimi" && !delivered {
		channel = kimiAdvisoryQueueChannel
	}
	return delivered, channel
}

// kimiAdvisoryQueuePath resolves the per-project queue file ("" when root is
// empty — global hooks have no project DataDir; their kimi advisories keep the
// old documented-inert behavior rather than writing into a hash-of-nothing
// bucket).
//
// kimiAdvisoryQueuePath 解析 per-project 队列文件路径（root 为空时返回
// ""——global hook 没有项目 DataDir，其 kimi advisory 维持既有的「已知失效」
// 行为，而不是写进一个空哈希桶）。
func kimiAdvisoryQueuePath(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(forgedata.DataDirFor(root), kimiAdvisoryQueueFile)
}

// enqueueKimiAdvisory appends one advisory to the per-project queue.
// Best-effort, fail-open: the advisory layer must never take the hook down
// (same discipline as the checklog record sites).
//
// enqueueKimiAdvisory 往 per-project 队列追加一条 advisory。尽力而为、
// fail-open：advisory 层绝不允许拖垮 hook（与 checklog 记录点同一纪律）。
func enqueueKimiAdvisory(root, sessionID, hookName, eventName, detail string) {
	path := kimiAdvisoryQueuePath(root)
	if path == "" {
		return
	}
	if st, err := os.Stat(path); err == nil && st.Size() > kimiAdvisoryMaxQueueBytes {
		_ = os.Remove(path) // 自重置：只保留最近的窗口（见常量注释）
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	entry := kimiAdvisoryEntry{
		TS:      time.Now().UTC().Format(time.RFC3339),
		Hook:    hookName,
		Event:   eventName,
		Session: util.SanitizeSessionID(sessionID),
		Text:    truncate(strings.TrimSpace(detail), kimiAdvisoryMaxTextLen),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// drainKimiAdvisories consumes the per-project queue and renders the batched
// injection ("" when there is nothing new for this session). Contract:
//   - dedupe by exact text (a throttled hook re-firing the same advisory must
//     not repeat it in one batch);
//   - a text already injected to THIS session is never injected again (the
//     once-per-session delivered set lives in $TMPDIR, session-keyed, same
//     lifespan class as the skill-trigger noise markers and the reads log);
//   - at most kimiAdvisoryDrainCap entries, in chronological order (the queue
//     is append-ordered; the tail is the newest window);
//   - the queue file is consumed even when everything in it was already
//     delivered — the entries belong to whoever drains first. Consumption is
//     ATOMIC: rename-then-read (not read-then-remove), so an enqueue racing
//     the drain re-creates the original path via O_CREATE and its entries
//     survive for the next drain instead of being silently deleted.
//
// drainKimiAdvisories 消费 per-project 队列并渲染攒发注入（本会话无新内容时
// 返回 ""）。契约：
//   - 按文本精确去重（被节流的 hook 重复触发同一 advisory 不得在一批里重复）；
//   - 已注入过**本会话**的文本绝不再次注入（每会话一次的 delivered 集合放
//     $TMPDIR、按会话键控，与 skill-trigger 噪声标记、reads log 同寿命类）；
//   - 最多 kimiAdvisoryDrainCap 条，按时间顺序（队列按追加顺序排列，尾部即
//     最新窗口）；
//   - 即使队列内容全部已投递过也照常消费队列文件——条目归先 drain 到的会话。
//     消费是**原子**的：先 rename 再读（而非先读再删），与 drain 竞争的 enqueue
//     会经 O_CREATE 重建原路径，其条目留待下次 drain，而不是被静默删掉。
func drainKimiAdvisories(root, sessionID string) string {
	path := kimiAdvisoryQueuePath(root)
	if path == "" {
		return ""
	}
	// 原子认领：先 rename 成 drain 副本，再读再删。先读后删的窗口里，并发的
	// enqueue（O_APPEND 到原路径）会随 remove 一起消失；rename 之后 append 会
	// 经 O_CREATE 落回**新**的原路径文件，条目不丢。
	drainPath := fmt.Sprintf("%s.drain.%d", path, os.Getpid())
	if err := os.Rename(path, drainPath); err != nil {
		return "" // 队列不存在（或刚被另一个 drain 认领）：无内容可攒
	}
	defer os.Remove(drainPath) // 消费即删（契约见上）
	data, err := os.ReadFile(drainPath)
	if err != nil {
		return ""
	}

	delivered := readKimiDeliveredSet(sessionID)
	var texts []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry kimiAdvisoryEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Text == "" {
			continue // 坏行/空行：advisory 层 fail-open，跳过不阻塞攒发
		}
		key := kimiAdvisoryTextKey(entry.Text)
		if seen[key] || delivered[key] {
			continue
		}
		seen[key] = true
		texts = append(texts, entry.Text)
	}
	if len(texts) == 0 {
		return ""
	}
	// Newest window: the queue is append-ordered, so the tail is the newest.
	//
	// 取最新窗口：队列按追加顺序排列，尾部即最新。
	if len(texts) > kimiAdvisoryDrainCap {
		texts = texts[len(texts)-kimiAdvisoryDrainCap:]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[forge] %d hook advisory(ies) could not be delivered at fire time "+
		"(kimi only carries UserPromptSubmit output into context; PreToolUse stdout would block the tool call). "+
		"Batched here once per session, newest last:\n", len(texts))
	for i, text := range texts {
		fmt.Fprintf(&b, "%d. %s\n", i+1, text)
	}
	appendKimiDeliveredSet(sessionID, texts)
	return strings.TrimRight(b.String(), "\n")
}

// kimiAdvisoryTextKey is the dedupe/delivered-set identity of one advisory:
// fnv64a of the exact text (same hash family as projectTagFor).
//
// kimiAdvisoryTextKey 是一条 advisory 的去重/delivered 集合身份：精确文本的
// fnv64a（与 projectTagFor 同一哈希族）。
func kimiAdvisoryTextKey(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return strconv.FormatUint(h.Sum64(), 16)
}

// kimiDeliveredSetPath is the per-session delivered-set file in $TMPDIR —
// ephemeral session state, NOT project data (it must not survive into the
// DataDir's permanent runtime home; a new session re-delivers advisories whose
// conditions still hold).
//
// kimiDeliveredSetPath 是 $TMPDIR 下按会话键控的 delivered 集合文件——临时
// 会话态，**不是**项目数据（不得落进 DataDir 这个永久运行态 home；新会话对
// 条件仍成立的 advisory 会重新投递）。
func kimiDeliveredSetPath(sessionID string) string {
	return filepath.Join(os.TempDir(), "forge-kimi-advisories", util.SanitizeSessionID(sessionID)+".delivered")
}

func readKimiDeliveredSet(sessionID string) map[string]bool {
	set := map[string]bool{}
	f, err := os.Open(kimiDeliveredSetPath(sessionID))
	if err != nil {
		return set
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			set[line] = true
		}
	}
	return set
}

func appendKimiDeliveredSet(sessionID string, texts []string) {
	path := kimiDeliveredSetPath(sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	for _, text := range texts {
		_, _ = f.WriteString(kimiAdvisoryTextKey(text) + "\n")
	}
}
