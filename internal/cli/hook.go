package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/toolusage"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// projectTagFor returns a stable hex tag for a given project root. By hashing the canonical
// (absolute, cleaned) path, the tag stays invariant across path case, drive letter format, and symlinks —
// whereas $PWD cksum also depends on the host's cksum format (GNU vs BSD). The hook reads it via
// the FORGE_PROJECT_TAG env var to isolate state per project.
//
// projectTagFor 为给定 project root 返回稳定的 hex tag。通过对 canonical
// （绝对、clean 后的）路径做哈希，使 tag 在路径大小写、盘符格式、symlink 之间保持
// 不变——而 $PWD cksum 还依赖宿主的 cksum 格式（GNU vs BSD）。hook 通过
// FORGE_PROJECT_TAG env var 读取它来按 project 隔离状态。
func projectTagFor(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	h := fnv.New64a()
	h.Write([]byte(filepath.Clean(abs)))
	return strconv.FormatUint(h.Sum64(), 16)
}

// findGitRoot walks up from dir to the nearest ancestor directory that contains a .git entry (a directory for normal repos,
// a file for worktree/submodule). Returns "" if none. It mirrors the bash root-finding of the init-suggest hook so that
// the tag computed on the Go side aligns with the ROOT the script operates on: same .git predicate, same stop-at-root
// behavior. Cross-platform safe: on a Windows drive root (E:\) filepath.Dir returns itself, a natural stop condition,
// so the loop does not spin on the drive root.
//
// findGitRoot 从 dir 向上遍历到最近的含 .git 条目的祖先目录（普通仓库是目录，
// worktree/submodule 是文件）。无则返回 ""。与 init-suggest hook 的 bash
// root-finding 保持一致，使 Go 侧算出的 tag 与脚本作用的 ROOT 对齐：相同的
// .git 判定、相同的遇 root 停止行为。跨平台安全：Windows 盘根（E:\）的
// filepath.Dir 返回自身，正是天然的终止条件，故循环不会在盘根上空转。
//
// Known cost: on UNC/SMB network shares each os.Stat may block for about 1s (network probe timeout),
// adding about 1-3s to SessionStart for users whose cwd is on a network mount. In practice rare — accepted
// rather than adding a stat timeout (which would complicate the POSIX-correct upward walk for a rare case).
//
// 已知开销：在 UNC/SMB 网络共享上每次 os.Stat 可能阻塞约 1s（网络探测超时），
// 给 cwd 位于网络挂载的用户的 SessionStart 增加约 1-3s。实际罕见——选择接受，
// 而不是加 stat 超时（会为一个罕见场景复杂化 POSIX-correct 的向上遍历）。
func findGitRoot(dir string) string {
	d := filepath.Clean(dir)
	for {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "" // 已到文件系统根（Unix 的 / 或 Windows 盘根）
		}
		d = parent
	}
}

// suggestTagFor returns the init-suggest marker tag for a directory, keyed by its git root,
// so no matter which subdir the agent runs `forge suggest decline` from, the same project is tagged only
// once. This guards the decline contract: previously keyed by cwd, declining from a subdir would write a different tag
// than the hook read at the project root, silently making decline a no-op. Non-git directories fall back to
// projectTagFor(dir) (still a stable per-dir tag). Shared by the init-suggest hook
// (FORGE_CWD_TAG) and `forge suggest` — both must produce the same tag for the same project.
//
// suggestTagFor 返回某目录的 init-suggest marker tag，按其 git root 作 key，
// 这样无论 agent 从哪个 subdir 执行 `forge suggest decline`，同一 project 只会被
// tag 一次。这守护 decline 契约：此前按 cwd 作 key，从 subdir decline 会写出与
// hook 在 project root 读到的不同的 tag，使 decline 静默 no-op。非 git 目录回退到
// projectTagFor(dir)（仍是稳定的 per-dir tag）。由 init-suggest hook
// （FORGE_CWD_TAG）和 `forge suggest` 共用——两者对同一 project 必须产出相同的
// tag。
func suggestTagFor(dir string) string {
	if root := findGitRoot(dir); root != "" {
		return projectTagFor(root)
	}
	return projectTagFor(dir)
}

// HookInput represents the JSON that Claude Code sends to a hook via stdin.
//
// HookInput 表示 Claude Code 通过 stdin 发给 hook 的 JSON。
type HookInput struct {
	SessionID     string          `json:"session_id"`
	HookEventName string          `json:"hook_event_name"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
	ToolOutput    json.RawMessage `json:"tool_output,omitempty"`
}

// toolInputFields holds the fields extracted from the tool_input JSON.
//
// toolInputFields 持有从 tool_input JSON 抽取的字段。
type toolInputFields struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
	Command  string `json:"command"` // Bash 的 tool_input.command
}

// HookOutput represents the structured JSON that Claude Code expects to receive on stdout.
// Field semantics: see the Claude Code hook documentation.
//
// HookOutput 表示 Claude Code 期望在 stdout 收到的结构化 JSON。
// 字段语义参见 Claude Code hook 文档。
type HookOutput struct {
	Decision           string              `json:"decision"`
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookSpecificOutput holds fields that steer Claude Code behavior.
//
// HookSpecificOutput 含控制 Claude Code 行为的字段。
type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}

// maxAdditionalContextLen is the upper bound of Claude Code additionalContext (10,000 chars).
// Here we use 9,500 to leave room for the JSON envelope.
//
// maxAdditionalContextLen 是 Claude Code additionalContext 的上限（10,000 字符）。
// 这里取 9,500 给 JSON envelope 留余量。
const maxAdditionalContextLen = 9500

// maxChecklogDetail is the truncation cap for a checklog entry detail.
//
// maxChecklogDetail 是 checklog entry detail 的截断上限。
const maxChecklogDetail = 500

// maxEnvValueLen is the maximum length of an env var value passed to the bash script,
// used to prevent memory issues.
//
// maxEnvValueLen 是传给 bash 脚本的 env var value 的最大长度，
// 用于防止内存问题。
const maxEnvValueLen = 100000

var hookCmd = &cobra.Command{
	Use:    "hook <name>",
	Short:  "Run an embedded hook script by name",
	Long:   "Executes the named hook script embedded in the forge binary. Extracts fields from Claude Code's stdin JSON into env vars, runs the script, and wraps its plain-text output into structured JSON.",
	Args:   cobra.ExactArgs(1),
	Hidden: true,
	RunE:   runHook,
}

// hookAgent specifies the non-Claude-Code stdin dialect to normalize. Each agent's translator sets it via the cross-platform
// `--agent` flag when the hook stdin differs from the Claude Code shape (Windsurf, Copilot). opencode/pi construct Claude-shape
// stdin in TS and do not set this variable. FORGE_HOOK_AGENT is the fallback for translators already wired via env
// (and for TS code that sets the env).
//
// hookAgent 指定要 normalize 的非 Claude Code stdin 方言。由各 agent 的 translator
// 在 hook stdin 与 Claude Code 形状不同时（Windsurf、Copilot）通过跨平台
// `--agent` flag 设置。opencode/pi 在 TS 里构造 Claude-shape stdin，不设此变量。
// FORGE_HOOK_AGENT 是已通过 env 接线的 translator（以及设 env 的 TS 代码）的兜底。
var hookAgent string

func init() {
	hookCmd.Flags().StringVar(&hookAgent, "agent", "", "agent whose stdin dialect to normalize (windsurf|copilot)")
	rootCmd.AddCommand(hookCmd)
}

// resolveHookAgent decides which agent's stdin dialect to normalize. The --agent flag
// (set by translators, cross-platform — Windows cmd cannot parse ENV=val cmd) takes precedence;
// FORGE_HOOK_AGENT is the fallback for callers wired via env (and for TS extensions that set the env before
// spawning forge). Returning an empty string means Claude-Code-shape stdin, no normalization needed —
// this is the default behavior for claude-code/codex/cursor and for opencode/pi (which construct Claude stdin in TS).
//
// resolveHookAgent 决定要 normalize 哪个 agent 的 stdin 方言。--agent flag
// （由 translator 设置，跨平台——Windows cmd 无法解析 ENV=val cmd）优先；
// FORGE_HOOK_AGENT 是改走 env 接线的调用方（以及在 spawn forge 前设 env 的
// TS 扩展）的兜底。返回空串表示 Claude-Code-shape stdin、无需 normalize——
// 这是 claude-code/codex/cursor 以及 opencode/pi（在 TS 里构造 Claude stdin）
// 的默认行为。
func resolveHookAgent(flagVal, envVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return envVal
}

// isGlobalHook decides whether a hook runs independently of a forge project. A global hook scans
// $HOME-level state (skill-scan -> ~/.claude/skills) or cwd-level state
// (init-suggest -> detects whether cwd is a forge candidate; mcp-scan -> scans the project-level
// .mcp.json), all of which are relevant in any project — so when findProjectRoot fails
// (non-forge project) runHook must not silently skip them. init-suggest and mcp-scan
// must run inside non-forge projects: init-suggest is exactly where it discovers forge-candidate
// projects, and mcp-scan catches malicious .mcp.json in user-cloned projects (which may never run forge init).
// Project-scoped hooks (task-guard, file-sentinel, etc.) keep their original
// allow-and-exit behavior.
//
// isGlobalHook 判断某 hook 是否独立于 forge project 运行。Global hook 扫描
// $HOME 级别状态（skill-scan → ~/.claude/skills）或 cwd 级别状态
// （init-suggest → 检测 cwd 是否为 forge candidate；mcp-scan → 扫描项目级
// .mcp.json），这些在任何 project 都相关——所以 findProjectRoot 失败时
// （非 forge project）runHook 不可静默跳过它们。init-suggest 与 mcp-scan
// 必须在非 forge project 中运行：init-suggest 正是在那里发现 forge-candidate
// 项目，mcp-scan 则捕获用户 clone 的项目（可能永远不跑 forge init）里的
// 恶意 .mcp.json。Project-scoped hook（task-guard、file-sentinel 等）保持原有
// allow-and-exit 行为。
func isGlobalHook(name string) bool {
	return name == "skill-scan" || name == "init-suggest" || name == "mcp-scan"
}

func runHook(cmd *cobra.Command, args []string) error {
	name := args[0]
	content, ok := hooks.EmbeddedContent(name)
	if !ok {
		return fmt.Errorf("unknown hook: %s", name)
	}

	// Not in a forge project — output allow and exit silently.
	// Global hook (skill-scan scans $HOME/.claude/skills) is relevant in any project,
	// so it must run even without a forge project root.
	//
	// 不在 forge project 中——输出 allow 并静默退出。
	// Global hook（skill-scan 扫 $HOME/.claude/skills）在任何 project 都相关，
	// 故即便没有 forge project root 也要运行。
	root, err := findProjectRoot()
	if err != nil {
		if !isGlobalHook(name) {
			outputAllow("")
			return nil
		}
		root = "" // global hook：无需 project root；shCmd.Dir="" 回退到 cwd
	}

	// 1. Read Claude Code's stdin JSON.
	//
	// 1. 读取 Claude Code 的 stdin JSON。
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		stdinData = []byte{}
	}

	var hookInput HookInput
	if len(stdinData) > 0 {
		if err := json.Unmarshal(stdinData, &hookInput); err != nil {
			// Log the parse failure for diagnosis, but continue with empty input.
			//
			// 记录解析失败以便诊断，但仍以空输入继续。
			fmt.Fprintf(os.Stderr, "[forge] warning: hook stdin JSON parse failed: %v\n", err)
		}
	}

	// 1b. Normalize the stdin of non-Claude-Code agents. Windsurf/Copilot use a different
	// hook stdin schema (Windsurf: {agent_action_name, trajectory_id,
	// tool_info}); without this step forge would extract empty file_path/command and blocking hooks
	// (task-guard/bash-guard) would fail open. The `--agent` flag (cross-platform, set by the translator)
	// selects the dialect; FORGE_HOOK_AGENT is the fallback. opencode/pi are code-based and directly
	// construct Claude stdin in TS, so no normalizer is needed here.
	//
	// 1b. normalize 非 Claude Code agent 的 stdin。Windsurf/Copilot 使用不同的
	// hook stdin schema（Windsurf: {agent_action_name, trajectory_id,
	// tool_info}）；不做这步，forge 会抽出空的 file_path/command，拦截类 hook
	// （task-guard/bash-guard）会 fail open。`--agent` flag（跨平台，由 translator
	// 设置）选择方言；FORGE_HOOK_AGENT 是兜底。opencode/pi 是 code-based，直接
	// 在 TS 里构造 Claude stdin，故此处无需 normalizer。
	agent := resolveHookAgent(hookAgent, os.Getenv("FORGE_HOOK_AGENT"))
	if agent != "" {
		normalizeAgentStdin(agent, stdinData, &hookInput)
	}

	// 2. Extract tool_input fields on the Go side (reliable JSON parsing).
	//
	// 2. 在 Go 侧抽取 tool_input 字段（可靠的 JSON 解析）。
	var fields toolInputFields
	if len(hookInput.ToolInput) > 0 {
		if err := json.Unmarshal(hookInput.ToolInput, &fields); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: tool_input parse failed: %v\n", err)
		}
	}

	// 2b. Detect the active task as context for the task-guard hook.
	// Scope the lookup by the Claude Code session id from stdin so concurrent sessions each
	// resolve their own active task (rather than racing on whichever wrote the global file last).
	//
	// 2b. 检测 active task，作为 task-guard hook 的上下文。
	// 按来自 stdin 的 Claude Code session id 限定查找范围，使并发 session 各自
	// 解析到自己的 active task（而非看哪个最后写入全局文件）。
	var activeTaskRef string
	var activeTaskGate string
	// Scheme 5: the per-task work-activity override lives in state.json, which the bash
	// PreToolUse hook cannot read. Surface it to the upper layer here so read-before-edit
	// (scheme 2 shift-left) respects the per-task disable just like the work-activity gate —
	// an escape must work end-to-end or it is not an escape (fake hard gate backfire: the gate passes
	// but PreToolUse still rejects the edit).
	//
	// 方案5：per-task 的 work-activity override 存在 state.json 中，bash
	// PreToolUse hook 读不到。这里把它显式 surface 给上层，使 read-before-edit
	// （方案2 shift-left）和 work-activity gate 一样尊重 per-task 的 disable——
	// escape 必须端到端生效，否则算不上 escape（fake hard gate 反噬：gate 放行
	// 但 PreToolUse 仍拒绝 edit）。
	var workActivityOverride string
	if active, err := taskpipeline.ActiveTaskState(root, util.SanitizeSessionID(hookInput.SessionID)); err == nil && active != nil {
		activeTaskRef = active.TaskRef
		activeTaskGate = active.CurrentGate
		if active.Overrides.WorkActivity == "disable" {
			workActivityOverride = "disable"
		}
	}

	// 3. Write the embedded script to a temp file.
	//
	// 3. 把 embedded script 写入临时文件。
	tmpFile, err := os.CreateTemp("", "forge-hook-*.sh")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write script: %w", err)
	}
	tmpFile.Close()
	// No chmod needed — bash reads the file as an argument and does not execute it directly.
	//
	// 无需 chmod——bash 把文件作为参数读入，并不直接执行它。

	// 4. Run the script with the extracted fields as env vars.
	//
	// 4. 用抽出的字段作为 env var 执行该 script。
	bash, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("bash not found in PATH: %w", err)
	}

	shCmd := exec.Command(bash, tmpPath)
	shCmd.Dir = root
	cwd, _ := os.Getwd() // 真实 cwd，给 init-suggest global hook 用（FORGE_CWD / FORGE_CWD_TAG）
	shCmd.Env = append(os.Environ(),
		"FORGE_FILE_PATH="+sanitizeForShell(toRelPath(root, fields.FilePath)),
		"FORGE_CONTENT="+sanitizeForShell(fields.Content),
		"FORGE_COMMAND="+sanitizeForShell(fields.Command),
		"FORGE_TOOL_NAME="+sanitizeForShell(hookInput.ToolName),
		"FORGE_TASK_REF="+sanitizeForShell(activeTaskRef),
		"FORGE_TASK_GATE="+sanitizeForShell(activeTaskGate),
		"FORGE_SESSION_ID="+sanitizeForShell(hookInput.SessionID),
		// Scheme 2 shift-left: absolute path of this session's reads log. The Go dispatcher
		// (tool-track) appends each Read's repo-relative path here; the PreToolUse
		// read-before-edit hook greps it to intercept Edit-without-Read. Passed as an absolute path
		// (not reconstructed in bash) so that Windows (os.TempDir = Windows AppData temp)
		// and Unix resolve the temp dir consistently — avoiding $TMPDIR vs /tmp divergence that would make the hook
		// silently never match.
		//
		// 方案2 shift-left：本 session reads log 的绝对路径。Go 分发器
		// （tool-track）把每次 Read 的 repo-relative 路径追加到这里；PreToolUse
		// read-before-edit hook grep 它来拦截 Edit-without-Read。以绝对路径传递
		// （不在 bash 里重建），让 Windows（os.TempDir = Windows AppData temp）
		// 与 Unix 在 temp dir 上一致解析——避免 $TMPDIR 与 /tmp 不一致导致 hook
		// 静默永远命中不到。
		"FORGE_READS_FILE="+readsFilePath(root, hookInput.SessionID),
		// Stable project tag (fnv hash of the canonical project root) so the hook can
		// bucket per-project state by it, not relying on $PWD/cksum — the latter is unstable across path case, drive letters,
		// and BSD/GNU cksum formats. For global hooks (init-suggest/skill-scan) root is empty, so this hashes the real cwd —
		// init-suggest must never depend on it (non-forge projects have no forge root); use FORGE_CWD_TAG below instead.
		//
		// 稳定的 project tag（canonical project root 的 fnv 哈希），让 hook
		// 据此为 per-project 状态分桶，不依赖 $PWD/cksum——后者在路径大小写、盘符、
		// BSD/GNU cksum 格式之间都不稳定。对 global hook（init-suggest/
		// skill-scan）来说 root="" ，于是这里哈希的是真实 cwd——init-suggest
		// 绝不能依赖它（非 forge project 没有 forge root）；改用下面的 FORGE_CWD_TAG。
		"FORGE_PROJECT_TAG="+projectTagFor(root),
		// The cwd and its git-root-keyed tag, for init-suggest (a global hook) to use:
		// the hook finds the git root from FORGE_CWD, then writes a per-project marker keyed by FORGE_CWD_TAG.
		// Keyed by git root (via suggestTagFor), not cwd, so no matter which subdir runs
		// `forge suggest decline`, the tag written matches what the hook reads at the project root —
		// guarding the decline contract.
		//
		// cwd 及其按 git root 作 key 的 tag，给 init-suggest（global hook）用：
		// hook 从 FORGE_CWD 找 git root，再按 FORGE_CWD_TAG 写 per-project marker。
		// 以 git root 作 key（经 suggestTagFor），不是 cwd，所以从任何 subdir
		// 跑 `forge suggest decline` 写出的 tag 与 hook 在 project root 读到的
		// 一致——守护 decline 契约。
		"FORGE_CWD="+cwd,
		"FORGE_CWD_TAG="+suggestTagFor(cwd),
	)
	// Scheme 5: expose the active task's per-task work-activity override as
	// the FORGE_WORK_ACTIVITY env for the hook to check. Forced only when disable —
	// when the override is empty, leave the existing os.Environ() value untouched to preserve a user's global FORGE_WORK_ACTIVITY,
	// and to avoid falsely reporting the escape hatch as open on non-escaping tasks.
	//
	// 方案5：把 active task 的 per-task work-activity override 以
	// FORGE_WORK_ACTIVITY env 暴露给 hook 检查。仅在 disable 时强制写入——
	// override 为空时不碰 os.Environ() 的现有值，保留用户全局 FORGE_WORK_ACTIVITY，
	// 也避免给未 escape 的 task 误报逃生舱已开。
	if workActivityOverride == "disable" {
		shCmd.Env = append(shCmd.Env, "FORGE_WORK_ACTIVITY=disable")
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	shCmd.Stdout = &stdoutBuf
	shCmd.Stderr = &stderrBuf

	exitErr := shCmd.Run()

	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())
	passed := exitErr == nil

	// 5. Parse the script output into the structured JSON that Claude Code expects.
	// The script outputs plain text: PASS [detail] or FAIL [reason].
	// Here we wrap it into the Claude Code hook protocol JSON format.
	//
	// 5. 把 script 输出解析成 Claude Code 需要的结构化 JSON。
	// Script 输出纯文本：PASS [detail] 或 FAIL [reason]。
	// 这里把它包成 Claude Code hook protocol JSON 格式。
	eventName := hookInput.HookEventName
	var output HookOutput
	if passed {
		detail := extractDetail(stdout, "PASS")
		output = HookOutput{Decision: "approve"}
		if detail != "" {
			output.HookSpecificOutput = &HookSpecificOutput{
				HookEventName:     eventName,
				AdditionalContext: truncate(detail, maxAdditionalContextLen),
			}
		}
	} else {
		detail := stdout
		if detail == "" {
			detail = stderr
		}
		output = HookOutput{
			Decision: "block",
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:     eventName,
				AdditionalContext: truncate(detail, maxAdditionalContextLen),
			},
		}
	}

	// 6. Record into checklog (noise-gated).
	//
	// 6. 记入 checklog（noise-gated）。
	checkName := checklog.CheckName(name)
	logDetail := firstNonEmpty(stderr, stdout, "completed")

	// Reuse the task ref detected earlier for audit traceability.
	//
	// 复用前面检测到的 task ref，便于审计追溯。
	taskRef := activeTaskRef

	// On block (e.g. task-guard) clear tool_name to avoid producing ghost activity records.
	// A blocked Write should not inflate the WorkActivity count.
	//
	// 被拦截时（如 task-guard）清空 tool_name，避免产生 ghost activity 记录。
	// 被拦截的 Write 不应膨胀 WorkActivity 计数。
	recordedToolName := hookInput.ToolName
	if !passed {
		recordedToolName = ""
	}

	// Noise gate (axis A of checklog layered governance): scoring reads only the
	// LATEST entry per check (task.go scoreTask's LatestByCheckForSession), so writing PASS on every
	// tool call is pure audit noise — measured 15946 lines of checklog, 100% PASS, zero FAIL.
	// Only record FAIL (block/warn signal traceability and diagnostics that are actually needed) plus the
	// PASS of scoring-dependent checks (assertion-check/auto-compile) — their
	// LatestByCheck feeds CompilePassed/AssertionPassed. Non-scoring PASS is dropped,
	// cutting about 86% of the checklog volume. See shouldRecordCheck.
	//
	// Noise gate（checklog 分层治理的 axis A）：scoring 只读每个 check 的
	// LATEST 条目（task.go scoreTask 的 LatestByCheckForSession），所以每次
	// tool call 都写 PASS 纯属审计噪声——实测 15946 行 checklog 中 100% 是
	// PASS、零 FAIL。仅记录 FAIL（block/warn 信号追溯和诊断真正需要的）以及
	// scoring 依赖的 check（assertion-check/auto-compile）的 PASS——它们的
	// LatestByCheck 会喂给 CompilePassed/AssertionPassed。Non-scoring PASS 丢弃，
	// 削减约 86% 的 checklog 体积。参见 shouldRecordCheck。
	if shouldRecordCheck(checkName, passed) {
		if err := checklog.Record(root, &checklog.Entry{
			Check:     checkName,
			Passed:    passed,
			Checked:   true,
			ToolName:  recordedToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
			Detail:    truncate(logDetail, maxChecklogDetail),
		}); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: checklog record failed: %v\n", err)
		}
	}

	// 6b. Record tool usage for activity-ratio detection. auto-compile records Write/Edit;
	// tool-track records Read|Skill|Agent (matcher lives in ForgeHookSpec), giving the
	// read-before-edit gate (task-verify) Read data — otherwise that gate would always fail on any task
	// with an edit (644b142 deleted the original Read recorder).
	// tool_input population: auto-compile (Edit/Write) records file_path/content; tool-track's Skill/Agent
	// records the skill name/subagent_type (scheme C: lets toollog audits see which quality skill the agent loaded and what kind of
	// subagent it dispatched — root cause of zero quality-skill fires in advisory context is traceable). Read still omits tool_input
	// (frequent; gate only needs tool_name+timestamp, keep toollog lean).
	//
	// 6b. 记录 tool usage 用于 activity-ratio 检测。auto-compile 记 Write/Edit；
	// tool-track 记 Read|Skill|Agent（matcher 在 ForgeHookSpec 中），让
	// read-before-edit gate（task-verify）有 Read 数据——否则该 gate 在任何带
	// edit 的 task 上恒失败（644b142 删过原来的 Read recorder）。
	// tool_input 填充：auto-compile（Edit/Write）记 file_path/content；tool-track 的 Skill/Agent
	// 记 skill 名/subagent_type（方案 C：让 toollog 审计能看到 agent 加载了哪个质量 skill、派了
	// 哪类子 agent——advisory 语境下质量 skill 0 触发的根因可追溯）。Read 仍省略 tool_input
	// （频繁，gate 只需 tool_name+timestamp，保持 toollog lean）。
	if name == "auto-compile" || name == "tool-track" {
		call := &toolusage.ToolCall{
			ToolName:  hookInput.ToolName,
			TaskRef:   taskRef,
			SessionID: util.SanitizeSessionID(hookInput.SessionID),
		}
		if name == "auto-compile" || (name == "tool-track" && (hookInput.ToolName == "Skill" || hookInput.ToolName == "Agent")) {
			raw := string(hookInput.ToolInput)
			call.ToolInput = toolusage.TruncateInput(raw)
			call.InputLen = len(raw)
			call.EstTokens = toolusage.EstimateTokens(raw)
		}
		if err := toolusage.Record(root, call); err != nil {
			fmt.Fprintf(os.Stderr, "[forge] warning: toollog record failed: %v\n", err)
		}
		// Scheme 2 shift-left: append this Read's file_path to the per-session reads log,
		// so the PreToolUse read-before-edit hook can intercept Edit-without-Read at Edit time.
		// toollog does not record Read's file_path (kept lean); this is a dedicated side channel.
		// PostToolUse fires after the Read completes, so this round's Read is recorded before the subsequent Edit —
		// the Edit's PreToolUse hook can then see the path. Only tool-track
		// (Read) records paths; auto-compile (Edit/Write) does not.
		//
		// 方案2 shift-left：把本次 Read 的 file_path 追加到 per-session reads log，
		// 让 PreToolUse read-before-edit hook 能在 Edit 时拦截 Edit-without-Read。
		// toollog 不记 Read 的 file_path（保持 lean）；这是一条专用 side-channel。
		// PostToolUse 在 Read 完成之后触发，所以本回合的 Read 会先于随后的 Edit
		// 被记录——Edit 的 PreToolUse hook 就能看到该路径。只有 tool-track
		// （Read）记路径；auto-compile（Edit/Write）不记。
		if name == "tool-track" && hookInput.ToolName == "Read" && fields.FilePath != "" {
			rel := toRelPath(root, fields.FilePath)
			if rel != "" && rel != "." {
				appendSessionRead(readsFilePath(root, hookInput.SessionID), rel)
			}
		}
	}

	// 7. Output structured JSON to Claude Code.
	//
	// 7. 向 Claude Code 输出结构化 JSON。
	outputJSON, err := json.Marshal(output)
	if err != nil {
		// Should not happen — HookOutput only contains strings.
		//
		// 不应发生——HookOutput 只含字符串。
		fmt.Fprintf(os.Stderr, "[forge] error: failed to marshal hook output: %v\n", err)
		fmt.Println(`{"decision":"approve"}`)
	} else {
		fmt.Println(string(outputJSON))
	}

	if !passed {
		return fmt.Errorf("hook %s failed", name)
	}
	return nil
}

// readsFilePath returns the absolute path of this session's reads log — the PreToolUse
// read-before-edit hook (scheme 2 shift-left) greps it to intercept Edit-without-Read via this
// on-disk side channel. Per-session (keyed by sanitized session id), ephemeral
// ($TMPDIR). Persisted to disk rather than carried in context so it SURVIVES compaction within a session:
// a Read before compaction still counts toward an Edit after it, eliminating the biggest false-positive source of context-based checks.
//
// readsFilePath 返回本 session 的 reads log 绝对路径——PreToolUse
// read-before-edit hook（方案2 shift-left）grep 它来拦截 Edit-without-Read 的
// 磁盘 side-channel。Per-session（按 sanitized session id 作 key）、ephemeral
// （$TMPDIR）。落盘而非存于 context，是为了在 session 内 SURVIVES compaction：
// compact 之前的 Read 仍计入之后的 Edit，消除基于 context 检查的最大假阳性来源。
func readsFilePath(root, sessionID string) string {
	// projectTagFor(root) buckets the reads log by project: $TMPDIR is shared across projects, and naming by session id alone
	// would let project A's reads log be read by project B under short/reused session ids (e.g. test sid-*) —
	// the read-before-edit hook would then falsely conclude an Edit had been Read (false-positive pass). The project tag is fnv hex
	// (filename-safe), sourced identically with FORGE_PROJECT_TAG.
	//
	// projectTagFor(root) 把 reads log 按 project 分桶：$TMPDIR 跨项目共享，仅按 session id
	// 命名会在短/复用 session id（如测试 sid-*）下让 A 项目的 reads log 被 B 项目读到——
	// read-before-edit hook 会误判 Edit 已 Read 过（假阳性放行）。project tag 是 fnv hex
	// （文件名安全），与 FORGE_PROJECT_TAG 同源。
	return filepath.Join(os.TempDir(), "forge-session-reads-"+projectTagFor(root)+"-"+readsFileKey(sessionID)+".log")
}

// readsFileKey collapses a session id into a filename-safe token. SanitizeSessionID
// preserves readability but may still contain characters that file systems treat specially on some platforms; any character
// outside [A-Za-z0-9._-] is collapsed to '_' so the temp file name is always safe and the original id is not leaked into $TMPDIR.
//
// readsFileKey 把 session id 收敛为 filename-safe 的 token。SanitizeSessionID
// 保留可读性，但仍可能含某些平台上被文件系统特殊对待的字符；将 [A-Za-z0-9._-]
// 之外的字符一律折叠为 '_'，使临时文件名始终安全，且不把原始 id 泄漏到 $TMPDIR。
func readsFileKey(sessionID string) string {
	s := util.SanitizeSessionID(sessionID)
	if s == "" {
		return "default"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "default"
	}
	return b.String()
}

// appendSessionRead appends a repo-relative Read path to the per-session reads log.
// Best-effort (advisory side channel): a write failure only means the read-before-edit hook
// will not see this Read — it must never cause the tool call to fail.
//
// appendSessionRead 把 repo-relative 的 Read 路径追加到 per-session reads log。
// Best-effort（advisory side-channel）：写入失败仅意味着 read-before-edit hook
// 看不到这次 Read——绝不能让 tool call 因此失败。
func appendSessionRead(path, relPath string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(relPath + "\n")
}

// sanitizeForShell sanitizes a string into a form safe for use as a shell env var. Prevents
// shell injection when user-controlled content reaches a bash script via an env var.
//
// sanitizeForShell 把字符串净化为可安全用于 shell env var 的形式。防止
// user-controlled 内容经 env var 传入 bash 脚本时发生 shell injection。
//
// Strategy:
//   - Truncate to maxEnvValueLen to prevent memory exhaustion
//   - Replace NULL bytes and control characters (except tab, newline, carriage return)
//   - Unicode-safe validation (reject invalid UTF-8)
//   - No quoting or escaping — callers must use export VAR=$value themselves and double-quote the value
//
// 策略：
//   - 截断到 maxEnvValueLen，防内存耗尽
//   - 替换 NULL 字节和控制字符（tab、newline、carriage return 除外）
//   - Unicode-safe 校验（拒绝非法 UTF-8）
//   - 不做引号或转义——调用方须自行用 export VAR=$value 并给 value 加双引号
//
// Note: this is a defense-in-depth measure. The hook script itself should also validate input before use.
//
// 注意：这是 defense-in-depth 措施。hook 脚本自身在使用前也应校验输入。
func sanitizeForShell(value string) string {
	if value == "" {
		return ""
	}

	// Truncate to prevent memory issues.
	//
	// 截断以防内存问题
	if len(value) > maxEnvValueLen {
		// Truncate at a UTF-8 boundary.
		//
		// 在 UTF-8 边界处截断
		for offset := maxEnvValueLen - 10; offset < maxEnvValueLen; offset++ {
			if offset >= len(value) {
				break
			}
			if utf8.RuneStart(value[offset]) {
				value = value[:offset]
				break
			}
		}
	}

	// Validate UTF-8 and remove control characters.
	//
	// 校验 UTF-8 并移除控制字符
	var result strings.Builder
	result.Grow(len(value))

	for _, r := range value {
		// Check UTF-8 validity.
		//
		// 检查 UTF-8 合法性
		if r == utf8.RuneError {
			// Skip invalid runes.
			//
			// 跳过非法 rune
			continue
		}

		// Remove NULL bytes and most control characters.
		// Allow: tab (0x09), newline (0x0A), carriage return (0x0D).
		// Block: NULL (0x00) and other control characters (0x01-0x08, 0x0B-0x0C, 0x0E-0x1F).
		//
		// 移除 NULL 字节和大多数控制字符
		// 放行：tab (0x09)、newline (0x0A)、carriage return (0x0D)
		// 拦截：NULL (0x00) 及其他控制字符 (0x01-0x08、0x0B-0x0C、0x0E-0x1F)
		if r == 0 {
			// Replace NULL with a space.
			//
			// NULL 替换为空格
			result.WriteRune(' ')
			continue
		}
		if r < 0x20 && r != 0x09 && r != 0x0A && r != 0x0D {
			// Skip other control characters.
			//
			// 跳过其他控制字符
			continue
		}

		result.WriteRune(r)
	}

	return result.String()
}

// extractDetail parses output of PASS/WARN/FAIL with optional detail. Returns the
// detail part after the keyword; if the output does not start with a known prefix, returns the full output.
//
// extractDetail 解析 PASS/WARN/FAIL 加可选 detail 的输出。返回关键字之后的
// detail 部分；若不以已知前缀开头，则返回完整输出。
func extractDetail(stdout, prefix string) string {
	if stdout == "" {
		return ""
	}
	for _, p := range []string{prefix, "WARN"} {
		after, ok := strings.CutPrefix(stdout, p)
		if ok {
			return strings.TrimSpace(after)
		}
	}
	return stdout
}

func outputAllow(msg string) {
	out := HookOutput{Decision: "approve"}
	if msg != "" {
		out.HookSpecificOutput = &HookSpecificOutput{AdditionalContext: msg}
	}
	data, _ := json.Marshal(out)
	fmt.Println(string(data))
}

// shouldRecordCheck decides whether a hook result is worth writing a checklog entry. It is the
// noise gate for checklog's dual responsibility (scoring input + audit traceability): scoring reads only the
// latest entry per check name (LatestByCheckForSession), so writing PASS on every call is redundant. Any
// FAIL returns true (block/warn signal traceability and diagnostics need it); PASS returns true only for
// scoring-dependent checks.
//
// shouldRecordCheck 判断一次 hook 结果是否值得写 checklog 条目。它是 checklog
// 双重职责（scoring 输入 + 审计追溯）的 noise gate：scoring 只读每个 check name
// 的最新条目（LatestByCheckForSession），所以每次调用都写 PASS 是冗余的。任何
// FAIL 都返回 true（block/warn 信号追溯和诊断需要），PASS 仅在 scoring 依赖的
// check 上返回 true。
func shouldRecordCheck(name checklog.CheckName, passed bool) bool {
	if !passed {
		return true
	}
	return isScoringCheck(name)
}

// isScoringCheck decides whether a hook check's PASS will be consumed by task scoring.
// scoreTask (task.go) reads LatestByCheckForSession for these checks to populate
// CompilePassed/AssertionPassed; their PASS must be written to the log so scoring sees
// checked & passed. Other checks' PASS is dropped by the noise gate (only FAIL is recorded). Note:
// test-coverage scoring reads a separate test-coverage-gate entry written by taskpipeline at task-verify
// (not this hook path), so test-coverage-check does not need to write PASS here.
//
// isScoringCheck 判断某 hook check 的 PASS 是否会被 task scoring 消费。
// scoreTask（task.go）对这些 check 读 LatestByCheckForSession 来填
// CompilePassed/AssertionPassed；它们的 PASS 必须写入 log，scoring 才能看到
// checked & passed。其他 check 的 PASS 被 noise gate 丢弃（只记 FAIL）。注意：
// test-coverage scoring 读的是 taskpipeline 在 task-verify 写的另一条
// test-coverage-gate 条目（不是这条 hook 路径），故 test-coverage-check 在此
// 无需写 PASS。
func isScoringCheck(name checklog.CheckName) bool {
	switch name {
	case checklog.CheckAssertion, checklog.CheckAutoCompile:
		return true
	}
	return false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// toRelPath converts an absolute file path to a project-root-relative, slash-separated
// relative path. This way, patterns like .forge/* in shell scripts match correctly regardless of OS path format.
// On failure, returns the input unchanged.
// toRelPath returns the path of absPath relative to root, slash-separated. Both inputs are symlink-resolved first:
// on macOS, paths like t.TempDir() directories arrive via symlinks
// (/var/folders/... -> /private/var/folders/...), while findProjectRoot's
// os.Getwd() returns the physical form and tool_input's file_path arrives in symlink form.
// Without resolving both sides, filepath.Rel would cross the link boundary and produce ../../... paths that no longer
// match the hook glob patterns (.forge/*, .claude/settings*) — this is the root cause of task-guard uniquely
// failing to block .forge/state.json writes on macOS.
//
// toRelPath 把绝对文件路径转换为以 project root 为基准、用正斜杠分隔的相对
// 路径。这样 shell 脚本里的 .forge/* 等模式才能无视 OS 路径格式正确匹配。
// 转换失败时原样返回。
// toRelPath 返回 absPath 相对 root 的路径、用正斜杠分隔。两个入参都先做
// symlink 解析：在 macOS 上，类似 t.TempDir() 目录的路径会经由 symlink 到达
// （/var/folders/... → /private/var/folders/...），而 findProjectRoot 用的
// os.Getwd() 返回 physical 形式，tool_input 的 file_path 却以 symlink 形式
// 到达。不先解析两侧，filepath.Rel 会跨 link 边界产出 ../../... 路径，不再
// 匹配 hook glob 模式（.forge/*、.claude/settings*）——这是 task-guard 在
// macOS 上独有地拦不住 .forge/state.json 写入的根因。
func toRelPath(root, absPath string) string {
	if root == "" || absPath == "" {
		return absPath
	}
	root = resolveSymlinks(root)
	absPath = resolveSymlinks(absPath)
	rel, err := filepath.Rel(root, absPath)
	if err != nil {
		return filepath.ToSlash(absPath)
	}
	return filepath.ToSlash(rel)
}

// resolveSymlinks resolves symlinks on path. If path does not yet exist (e.g. a PreToolUse
// Write target before the file is created), it resolves the longest existing ancestor directory and appends the base name back,
// so a not-yet-existing file still gets the physical prefix on macOS. When no segment of path can be resolved,
// returns path unchanged, preserving the original fallback behavior on symlink-free systems.
//
// resolveSymlinks 对 path 求值 symlink。若 path 尚不存在（例如 PreToolUse
// Write 目标在文件创建之前），则解析最长已存在的父目录再补回 base name，使
// 尚未存在的文件在 macOS 上仍能拿到 physical 前缀。当 path 上没有任何可解析
// 段时原样返回，保留无 symlink 系统上原有的 fallback 行为。
func resolveSymlinks(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	dir, base := filepath.Split(path)
	if dir == "" || dir == path {
		return path
	}
	resolvedDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return path
	}
	return filepath.Join(resolvedDir, base)
}
