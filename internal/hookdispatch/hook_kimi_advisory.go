// kimi advisory pending-queue: kimi 0.35.0 delivers hook stdout to the model
// ONLY on UserPromptSubmit (wire.jsonl-verified,
// internal/agentbridge/kimi-hook-routing.md), and reads ANY PreToolUse stdout as
// a deny (blocks the tool call, edit never lands).
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
package hookdispatch

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
	// kimiAdvisoryQueueFile 是 forge DataDir（~/.forge/projects/<key>/）下的
	// per-project pending 队列文件名。
	kimiAdvisoryQueueFile = "advisories-pending.jsonl"

	// kimiAdvisoryDrainCap 把单次攒发限制在**最近**几条——UserPromptSubmit 注入
	// 要与用户自己的 prompt 争夺注意力，更旧的大概率已被处理或过期（条件仍在
	// 时产生它的 hook 会再触发）。
	kimiAdvisoryDrainCap = 5

	// kimiAdvisoryMaxTextLen 给单条入队文本设上限，防止病态 detail（整段编译
	// 日志）撑爆队列或攒发注入。
	kimiAdvisoryMaxTextLen = 2000

	// kimiAdvisoryMaxQueueBytes 是自重置阈值：从不提交 prompt 的会话（全自动
	// 运行）否则会让队列无限增长。超过阈值即重来——其中条目按定义都未被
	// drain 过，只有最近的窗口有价值。
	kimiAdvisoryMaxQueueBytes = 256 * 1024
)

// kimiAdvisoryEntry 是一条入队 advisory（JSONL 行）。
type kimiAdvisoryEntry struct {
	TS      string `json:"ts"`
	Hook    string `json:"hook"`
	Event   string `json:"event"`
	Session string `json:"session,omitempty"`
	Text    string `json:"text"`
}

// emitAdvisoryRouted 是所有 hook 输出口径处替代裸 emitAgentOutput 的唯一
// advisory 输出路由器。agent!="kimi" 或阻断（passed=false）时它**就是**
// emitAgentOutput——逐字节行为一致。kimi 的 allow 路径输出走队列契约：
//   - UserPromptSubmit（kimi 唯一送达通道）：drain pending 队列并**前置**到
//     该 hook 自己的 detail（头部放置——emitter 在 maxAdditionalContextLen 截
//     **尾部**；与 prependKimiStaleAdvisory 同 F2 理由）。
//   - 其余事件：detail 入队并保持**静默**（exit 0、无 stdout）——kimi
//     PreToolUse 的 stdout 会被当 deny，PostToolUse/Stop/SessionStart 的
//     stdout 到不了模型。
func EmitAdvisoryRouted(agent, eventName, hookName, root, sessionID string, passed bool, detail string) error {
	if agent != "kimi" || !passed {
		return EmitAgentOutput(agent, eventName, hookName, passed, detail)
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
		return EmitAgentOutput(agent, eventName, hookName, passed, detail)
	}
	if strings.TrimSpace(detail) != "" {
		enqueueKimiAdvisory(root, sessionID, hookName, eventName, detail)
	}
	return nil
}

// kimiAdvisoryQueueChannel 是落在「输出经 pending 队列而非活通道」的 advisory
// 记录上的 checklog Channel 标签（kimi、allow 通道不可送达的事件）。Delivered
// 保持 false——尚无任何内容到达模型——但该标签让 usage 漏斗能区分「已入队、
// 下次 UserPromptSubmit 攒发」与「产生即永久丢失」（kimi/no-channel，本队列
// 要修的 2026-08 虚假繁荣类）。
const kimiAdvisoryQueueChannel = "kimi/advisory-queue"

// AdvisoryEmissionChannel 返回经 emitAdvisoryRouted 输出的 advisory 的
// （delivered, channel）章：与 contextChannelDelivered 一致，唯一例外是 kimi
// 不可送达事件——输出在那里入队待 UserPromptSubmit 攒发，channel 如实标注。
// 经 emitAdvisoryRouted 输出的记录点必须经本函数盖章（而非
// contextChannelDelivered），否则漏斗会继续把入队的 advisory 计入「永久丢失」。
func AdvisoryEmissionChannel(agent, eventName string) (bool, string) {
	delivered, channel := contextChannelDelivered(agent, eventName)
	if agent == "kimi" && !delivered {
		channel = kimiAdvisoryQueueChannel
	}
	return delivered, channel
}

// kimiAdvisoryQueuePath 解析 per-project 队列文件路径（root 为空时返回
// ""——global hook 没有项目 DataDir，其 kimi advisory 维持既有的「已知失效」
// 行为，而不是写进一个空哈希桶）。
func kimiAdvisoryQueuePath(root string) string {
	if root == "" {
		return ""
	}
	return filepath.Join(forgedata.DataDirFor(root), kimiAdvisoryQueueFile)
}

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
		Text:    util.TruncateRunes(strings.TrimSpace(detail), kimiAdvisoryMaxTextLen),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	_, _ = f.Write(append(data, '\n'))
}

// drainKimiAdvisories 消费 per-project 队列并渲染攒发注入（本会话无新内容时
// 返回 ""）。契约：
//   - 按文本精确去重（被节流的 hook 重复触发同一 advisory 不得在一批里重复）；
//   - 已注入过**本会话**的文本绝不再次注入（每会话一次的 delivered 集合放
//     $TMPDIR、按会话键控，与 skill-trigger 噪声标记、reads log 同寿命类）；
//     无 id 会话（空 sessionID）豁免此记忆——共享 "session" 桶为何绝不写入见
//     readKimiDeliveredSet；
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

	delivered := readKimiDeliveredSet(root, sessionID)
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
	appendKimiDeliveredSet(root, sessionID, texts)
	return strings.TrimRight(b.String(), "\n")
}

// kimiAdvisoryTextKey 是一条 advisory 的去重/delivered 集合身份：精确文本的
// fnv64a（与 projectTagFor 同一哈希族）。
func kimiAdvisoryTextKey(text string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return strconv.FormatUint(h.Sum64(), 16)
}

// kimiDeliveredSetPath 是 $TMPDIR 下按（project, session）键控的 delivered 集合
// 文件——临时会话态，**不是**项目数据（不得落进 DataDir 这个永久运行态 home；
// 新会话对条件仍成立的 advisory 会重新投递）。project tag 使同一静态模板 advisory
// 每个项目可投递一次（否则跨项目 cd 的会话在第二个项目被静音），镜像
// readsFilePath 的 project 桶。
func kimiDeliveredSetPath(root, sessionID string) string {
	return filepath.Join(os.TempDir(), "forge-kimi-advisories", ProjectTagFor(root)+"-"+util.SanitizeSessionID(sessionID)+".delivered")
}

// readKimiDeliveredSet 读取每会话一次的 delivered 集合。空 sessionID 直接禁用
// 记忆：SanitizeSessionID 会把 ""（及全脏字符 id）折叠成字面量 "session"，于是
// 全机所有无 id 会话共享**同一个** "...-session.delivered" 文件——那里的一次写入
// 将永久抑制该 advisory 文本在后续所有无 id 会话里的投递（全机级、无界的静音器
// ）。把 "" 视为「无历史」意味着这类会话每次都重新投递——身份未知时的诚实默认。
func readKimiDeliveredSet(root, sessionID string) map[string]bool {
	if sessionID == "" {
		return map[string]bool{}
	}
	set := map[string]bool{}
	f, err := os.Open(kimiDeliveredSetPath(root, sessionID))
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

// appendKimiDeliveredSet 把文本记入该会话的 delivered 集合。空 sessionID 完全
// 不写（理由同 readKimiDeliveredSet：写进共享的 "session" 桶会把该文本对全机
// 无 id 会话静音；不写则每次 drain 都重新投递）。
func appendKimiDeliveredSet(root, sessionID string, texts []string) {
	if sessionID == "" {
		return
	}
	path := kimiDeliveredSetPath(root, sessionID)
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
