// hook_doclint.go — 输出可读性门禁的写时反馈（output-readability-gates-v2.md
// P1-C，G5）：PostToolUse Write|Edit 对被写的 .md 跑 doclint L1，命中以
// advisory 提示（带行号与修复方向），把违规可见性从 complete 的 doc gate
// 提前到落盘时刻——Vale「editor + CI 双反馈」模式的前半边，后半边（执法）
// 仍是 CheckDocGate，本 hook 永不阻断。
//
// 与 conventions-write 同一契约族：advisory 层 fail-open、静默路径不落章、
// 会话级状态放 $TMPDIR（短命、OS 清理）。去重按「文件 → 命中签名」：同一
// 文件同一组问题重复保存不重发（agent 连续多次 Edit 同一文档是常态）；命中
// 集合变化（修掉一些/冒出新的一些）即重发——信号是「变化」不是「出现过」。
package hookdispatch

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/doclint"
	"github.com/MjxUpUp/Forge/internal/hostcap"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
)

// docLintHookMaxIssues 单条 advisory 最多列出的命中行数——超出部分以计数收尾
// （注入文本与模型注意力争夺，清单是修复入口不是全量报告，完整列表
// `forge docs lint <file>` 可复现）。
const docLintHookMaxIssues = 5

// runDocLintHook 处理 PostToolUse Write|Edit。静默条件：无项目根、非 .md、
// 项目根外、豁免路径、零命中、命中签名与会话内上次相同。
func runDocLintHook(hookInput HookInput, root, version, agent string) error {
	if root == "" {
		return nil
	}
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			// 与 conventions-write 同纪律：方言异常静默吞掉会让 hook 在该宿主
			// 无声空转——stderr 一行，advisory 层不 fail。
			fmt.Fprintf(os.Stderr, "[doc-lint] warning: tool_input parse failed: %v\n", err)
		}
	}
	if fields.FilePath == "" && hostcap.IsPatchTool(hookInput.ToolName) {
		fields.FilePath = applyPatchFilePath(fields.Command)
		if fields.FilePath != "" && !filepath.IsAbs(fields.FilePath) {
			fields.FilePath = filepath.Join(root, fields.FilePath)
		}
	}
	if fields.FilePath == "" || !strings.EqualFold(filepath.Ext(fields.FilePath), ".md") {
		return nil
	}
	// 项目根外（$HOME、/tmp 下的写入）：无本仓库门禁可言，静默。
	if rel, err := filepath.Rel(root, fields.FilePath); err != nil || strings.HasPrefix(rel, "..") {
		return nil
	}
	issues, err := doclint.LintFile(fields.FilePath)
	if err != nil || len(issues) == 0 {
		return nil // 读取失败/豁免/skip 标记/零命中：静默（LintFile 自带三层豁免）
	}

	sig := docLintIssueSignature(issues)
	if docLintSignatureUnchanged(hookInput.SessionID, fields.FilePath, sig) {
		return nil
	}

	detail := renderDocLintAdvisory(fields.FilePath, issues)
	recordDocLintAdvisory(hookInput, root, version, agent, len(issues))
	if err := EmitAdvisoryRouted(agent, hookInput.HookEventName, "doc-lint", root, hookInput.SessionID, true, detail); err != nil {
		return err
	}
	docLintMarkSignature(hookInput.SessionID, fields.FilePath, sig)
	return nil
}

// renderDocLintAdvisory 渲染注入文本：头部给文件与计数 + 可复现命令，逐条
// 命中带 [规则|档位] 行号与消息，超出 docLintHookMaxIssues 以计数收尾。
func renderDocLintAdvisory(path string, issues []doclint.Issue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[doc-lint] %s 命中 %d 处 L1 规则——落盘前修掉可省一轮回检（`forge docs lint %s` 可复现）：\n",
		path, len(issues), path)
	for i, iss := range issues {
		if i >= docLintHookMaxIssues {
			fmt.Fprintf(&b, "  …等 %d 处（完整列表见上命令）\n", len(issues)-docLintHookMaxIssues)
			break
		}
		sev := "hard"
		if !iss.Hard() {
			sev = "建议"
		}
		fmt.Fprintf(&b, "  ⚠ [%s|%s] L%d %s\n", iss.Rule, sev, iss.Line, iss.Message)
	}
	b.WriteString("advisory 不阻塞——执法在 task-complete 的 doc gate。")
	return b.String()
}

// docLintIssueSignature 是命中集合的身份：规则|行号|消息 序列的 fnv64a。消息
// 计入身份——同一条命中换了措辞（新口头禅）也是 agent 该知道的变化；真正被
// 去重的是「同一文件同一组命中」的重复保存（同一内容反复落盘）。
func docLintIssueSignature(issues []doclint.Issue) string {
	h := fnv.New64a()
	for _, iss := range issues {
		fmt.Fprintf(h, "%s|%d|%s;", iss.Rule, iss.Line, iss.Message)
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// ---- session state helpers（$TMPDIR/forge-doclint/<sess>/files.json，与
// conventions-write 的 per-dir marker 同寿命类）----

type docLintSessionState struct {
	Files map[string]string `json:"files"`
}

func docLintStatePath(sessionID string) string {
	return filepath.Join(os.TempDir(), "forge-doclint", util.SanitizeSessionID(sessionID), "files.json")
}

func loadDocLintState(sessionID string) docLintSessionState {
	var state docLintSessionState
	if data, err := os.ReadFile(docLintStatePath(sessionID)); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

func docLintSignatureUnchanged(sessionID, path, sig string) bool {
	return loadDocLintState(sessionID).Files[path] == sig
}

func docLintMarkSignature(sessionID, path, sig string) {
	state := loadDocLintState(sessionID)
	if state.Files == nil {
		state.Files = map[string]string{}
	}
	state.Files[path] = sig
	_ = os.MkdirAll(filepath.Dir(docLintStatePath(sessionID)), 0755)
	_ = util.AtomicWrite(docLintStatePath(sessionID), mustJSONState(state), 0644)
}

// recordDocLintAdvisory 落观察条目并盖送达章（与 conventions record 同契约；
// 复用 CheckDocLint——gate 扫描与写时提示同属 doc-lint 检查面，漏斗共用）。
func recordDocLintAdvisory(hookInput HookInput, root, version, agent string, hitCount int) {
	attr := taskAttributionForSession(root, hookInput.SessionID)
	delivered, channel := AdvisoryEmissionChannel(agent, hookInput.HookEventName)
	entry := &checklog.Entry{
		Check:        taskpipeline.CheckNameDocLint,
		Passed:       false,
		Checked:      true,
		ToolName:     hookInput.ToolName,
		TaskRef:      attr.TaskRef,
		SessionID:    hookInput.SessionID,
		Detail:       fmt.Sprintf("write-time advisory: %d L1 hits", hitCount),
		Source:       checklog.EvidenceDeterministic,
		Level:        checklog.LevelAdvisory,
		Delivered:    &delivered,
		Channel:      channel,
		ForgeVersion: version,
		Meta:         map[string]string{"event": hookInput.HookEventName},
	}
	attr.stamp(entry)
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[doc-lint] warning: checklog record failed: %v\n", err)
	}
}
