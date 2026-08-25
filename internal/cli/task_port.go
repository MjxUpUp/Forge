package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
)

// task_port.go: cross-machine task handoff (design §11). A task lives in user-level DataDir, which
// does NOT travel with the repo — so handing a task to another machine/agent needs an explicit
// vehicle. `task export` serializes a task (optionally its checklog evidence + redacted) into a JSON
// Bundle; `task import` lands it locally with one of three conflict strategies (reject / overwrite /
// merge). Imported sessions become "ghost" links (SessionLink.Imported): provenance-only, never
// treated as local anchors by attach.
//
// task_port.go：跨机器任务交接（设计§11）。task 在用户级 DataDir，不随仓库走——把任务交给另一台
// 机器/agent 需要显式载体。task export 把任务（可选附带 checklog 证据 + 脱敏）序列化成 JSON Bundle；
// task import 用三种冲突策略之一（拒绝/覆盖/合并）落地到本地。导入的 session 成为「幽灵」链接
// （SessionLink.Imported）：仅溯源，attach 永不当本机锚点。

// taskBundleSchemaVersion is the Bundle format version; import refuses a bundle whose version is
// higher than this (forward-incompatibility guard) so a future format is never silently mis-parsed.
//
// taskBundleSchemaVersion 是 Bundle 格式版本；import 拒绝版本高于此值的 bundle（前向不兼容守卫），
// 使未来格式永不被静默误解析。
const taskBundleSchemaVersion = 1

// taskBundle is the JSON envelope for a cross-machine task handoff. SchemaVersion gates forward
// compatibility; SourceProject is a provenance label (the source machine's absolute project root);
// Redacted records whether --redact stripped identifying/evidence fields.
//
// taskBundle 是跨机器任务交接的 JSON 信封。SchemaVersion 门控前向兼容；SourceProject 是溯源标签
// （源机器的绝对项目根）；Redacted 记录 --redact 是否已抹除身份/证据字段。
type taskBundle struct {
	SchemaVersion int                     `json:"schema_version"`
	ExportedAt    time.Time               `json:"exported_at"`
	SourceProject string                  `json:"source_project,omitempty"`
	Task          *taskpipeline.TaskState `json:"task"`
	Checklog      []checklog.Entry        `json:"checklog,omitempty"` // 仅 --include-checklog 时填充
	Redacted      bool                    `json:"redacted,omitempty"` // --redact 是否已应用
}

var taskExportCmd = &cobra.Command{
	Use:   `export --ref <ref> [-o file] [--include-checklog] [--redact]`,
	Short: `把任务导出为跨机器 JSON Bundle（含状态/决策/历史；可选 checklog 证据/脱敏）`,
	Long: `forge task export 把一个任务序列化为自包含 JSON Bundle，供另一台机器/agent 用
forge task import 落地。task state 存于用户级 DataDir 不随仓库走，故跨机器交接需要本显式载体。
默认不含 checklog（--include-checklog 附带证据链）；--redact 抹除身份/证据字段（issue/agent/
commit/证据文本），适合把任务结构当模板或对外求助时分享而不泄露内部细节。
不传 -o 时输出到 stdout（便于管道）。`,
	RunE: runTaskExport,
}

var taskImportCmd = &cobra.Command{
	Use:   `import --file <bundle> [--force|--merge]`,
	Short: `从 JSON Bundle 导入任务到本地（三种冲突策略 + 幽灵 session 标记）`,
	Long: `forge task import 把 export 产出的 Bundle 落地到本地项目。导入的 session 链接一律标记为
幽灵（Imported=true）：仅溯源显示，不参与本机 attach。冲突策略：
  · 默认：同 ref 已存在则拒绝（安全，防覆盖）
  · --force：覆盖既有任务（先删后写）
  · --merge：保留本地任务定义，合并协作记录（Decisions/Findings/Blockers/History/SessionLinks/
    NextSteps/Artifacts 按 ID/键去重并集）
若 Bundle 带 checklog，条目追加到本地 checklog.jsonl（保留源时序，供 forge trace 重建时间线）。`,
	RunE: runTaskImport,
}

func init() {
	taskCmd.AddCommand(taskExportCmd)
	taskCmd.AddCommand(taskImportCmd)

	taskExportCmd.Flags().String(`ref`, ``, `要导出的任务引用（不传则用当前活跃任务）`)
	taskExportCmd.Flags().StringP(`output`, `o`, ``, `写入文件（不传则输出到 stdout）`)
	taskExportCmd.Flags().Bool(`include-checklog`, false, `附带该任务的 checklog 证据链`)
	taskExportCmd.Flags().Bool(`redact`, false, `脱敏：抹除 issue/agent/commit/证据等身份与证据字段`)

	taskImportCmd.Flags().String(`file`, ``, `要导入的 Bundle 文件路径（必填）`)
	taskImportCmd.Flags().Bool(`force`, false, `同 ref 已存在时覆盖（先删后写）`)
	taskImportCmd.Flags().Bool(`merge`, false, `同 ref 已存在时合并协作记录（按 ID 去重并集）`)
}

func runTaskExport(cmd *cobra.Command, args []string) error {
	state, root, err := loadTaskOrActive(cmd)
	if err != nil {
		return err
	}
	includeChecklog, _ := cmd.Flags().GetBool(`include-checklog`)
	redact, _ := cmd.Flags().GetBool(`redact`)

	// Deep-copy via JSON round-trip before redaction so the live in-memory state is never mutated
	// (defensive — LoadTaskState already returns a fresh struct, but the copy keeps the non-mutation
	// guarantee local and obvious, and survives any future pointer-sharing refactor).
	//
	// 脱敏前经 JSON 往返深拷贝，确保活的内存 state 绝不被改（防御性——LoadTaskState 已返回新结构，
	// 但拷贝使「不改原件」的保证就地且显式，并抗未来的指针共享重构）。
	task := state
	if redact {
		task, err = cloneTaskState(state)
		if err != nil {
			return fmt.Errorf(`脱敏前深拷贝任务失败: %w`, err)
		}
		redactTask(task)
	}

	var entries []checklog.Entry
	if includeChecklog {
		entries, err = checklog.LoadForTask(root, state.TaskRef)
		if err != nil {
			return fmt.Errorf(`读取 checklog 证据失败: %w`, err)
		}
	}
	// checklog Detail routinely embeds absolute paths / command output; SessionID/ToolName are
	// session+tool identity. Without this pass a --redact --include-checklog bundle leaks them
	// verbatim (redactTask only touches the Task body, never the entries). Preserves the evidence
	// SHAPE (Check/Passed/Level/Source/RecordedAt stay) so trace still shows which checks ran.
	//
	// checklog Detail 常嵌绝对路径/命令输出；SessionID/ToolName 是会话+工具身份。不处理则
	// --redact --include-checklog 的 bundle 原样外泄（redactTask 只动 Task 体，不碰 entries）。
	// 保留证据「形状」（Check/Passed/Level/Source/RecordedAt 不变）使 trace 仍显示跑了哪些检查。
	if redact {
		redactChecklogEntries(entries)
	}

	// SourceProject carries the source machine's project root. The absolute path leaks username /
	// customer names (/Users/jsmith/projects/secret-foo); under --redact keep only the last segment
	// so the envelope stays provenance-shaped without an identity leak.
	//
	// SourceProject 带源机器项目根。绝对路径泄露用户名/客户名（/Users/jsmith/projects/secret-foo）；
	// --redact 下仅留末段，信封保持来源形状而不泄露身份。
	sourceProject := root
	if redact {
		sourceProject = projectBaseName(root)
	}
	bundle := taskBundle{
		SchemaVersion: taskBundleSchemaVersion,
		ExportedAt:    time.Now(),
		SourceProject: sourceProject,
		Task:          task,
		Checklog:      entries,
		Redacted:      redact,
	}
	data, err := json.MarshalIndent(bundle, ``, `  `)
	if err != nil {
		return fmt.Errorf(`序列化 Bundle 失败: %w`, err)
	}

	// #8/#9（设计§11）：默认导出静默成功是跨机器交接盲区——checklog 不含则对方 forge task import 后
	// forge trace <ref> 重建不了时间线（门禁证据丢失），且导出侧此前零跨机器提示（只在导入侧有）。
	// 导出落盘前 warn 一次；--include-checklog 时用户已显式带证据，不再 warn。
	if !includeChecklog {
		fmt.Fprintf(cmd.ErrOrStderr(), `⚠ 跨机器导出默认不含 checklog 证据链——对方 import 后 forge trace %s 无法重建时间线；如需附带用 --include-checklog（敏感场景配 --redact）`, state.TaskRef)
		fmt.Fprintln(cmd.ErrOrStderr())
	}

	out, _ := cmd.Flags().GetString(`output`)
	if out == `` {
		fmt.Println(string(data))
		return nil
	}
	if err := util.AtomicWrite(out, data, 0644); err != nil {
		return fmt.Errorf(`写入 %s 失败: %w`, out, err)
	}
	note := ``
	if redact {
		note = `（已脱敏）`
	}
	if includeChecklog {
		note += fmt.Sprintf(` 含 %d 条 checklog`, len(entries))
	}
	fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已导出任务 %s 到 %s%s`, state.TaskRef, out, note)
	fmt.Fprintln(cmd.ErrOrStderr())
	return nil
}

func runTaskImport(cmd *cobra.Command, args []string) error {
	file, _ := cmd.Flags().GetString(`file`)
	if file == `` {
		return fmt.Errorf(`--file 必填（Bundle 文件路径；由 forge task export 产出）`)
	}
	force, _ := cmd.Flags().GetBool(`force`)
	merge, _ := cmd.Flags().GetBool(`merge`)
	if force && merge {
		return fmt.Errorf(`--force 与 --merge 互斥（覆盖 vs 合并，二选一）`)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf(`读取 Bundle %s 失败: %w`, file, err)
	}
	var bundle taskBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		return fmt.Errorf(`解析 Bundle 失败（确认是 forge task export 产出）: %w`, err)
	}
	if bundle.Task == nil || bundle.Task.TaskRef == `` {
		return fmt.Errorf(`Bundle 缺少 task 或 task_ref（文件损坏或非 Forge Bundle）`)
	}
	// == 0 (字段缺失/手改的畸形 bundle) 与 > current（未来格式）都不被接受：前者会让前向兼容守卫
	// 形同虚设（缺 schema_version 的文档被当 v1 解析），后者给出明确的升级提示。
	//
	// == 0 (missing/hand-edited malformed bundle) and > current (future format) are both refused: the
	// former would make the forward-compat guard useless (a doc with no schema_version parsed as v1),
	// the latter gives a clear upgrade message.
	if bundle.SchemaVersion == 0 || bundle.SchemaVersion > taskBundleSchemaVersion {
		return fmt.Errorf(`Bundle schema_version=%d 不被支持（本机支持 %d）；确认是 forge task export 产出，或升级 Forge 后再导入`, bundle.SchemaVersion, taskBundleSchemaVersion)
	}
	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	ref := bundle.Task.TaskRef

	// All incoming session links become ghosts on THIS machine: they record who participated on the
	// source machine, never a local anchor. The ghosting itself lives inside
	// StripForeignGateSignals (single source, right below) so the migrate path gets it too — every
	// strategy below sees ghosted links.
	//
	// 所有传入的 session 链接在本机都成为幽灵：记录源机器谁参与过，永非本机锚点。幽灵化本身在
	// StripForeignGateSignals 内完成（单一真相源，紧随其后），migrate 路径同样拿到——下方每种
	// 策略看到的都是已幽灵化的链接。

	// Imported review/acceptance/score signals are FOREIGN evidence — never trusted locally. Without
	// stripping, a hand-edited or buggy bundle carrying ReviewPassed=true + Acceptance Passed/
	// AcceptedHeadCommit=current-HEAD would satisfy the task-complete gate's hard prerequisites
	// without the review sub-agent / verify-acceptance ever running on THIS machine. Control-flow
	// fields are stripped too (2026-08-15 review): CompletedAt (disables every
	// CompletedAt==nil-guarded hard check), Overrides (silently disables four hard gates), and the
	// acceptance Run commands get the foreign marker (verify-acceptance demands --trust-foreign
	// before first execution). Single source of truth: taskpipeline.StripForeignGateSignals —
	// shared with the .forge migrate path so a repo-committed task file cannot take the other road
	// into the trusted DataDir.
	//
	// 导入的 review/验收/评分信号是外来证据——本机绝不信任。不剥离则手改/有 bug 的 bundle 带上
	// ReviewPassed=true + 验收 Passed/AcceptedHeadCommit=当前 HEAD，就能满足 task-complete 门禁的硬
	// 前置，而本机从未跑过 review 子 agent / verify-acceptance。控制流字段同样剥离（2026-08-15
	// 审查）：CompletedAt（会关掉所有 CompletedAt==nil 守卫的硬检查）、Overrides（静默关四个硬门禁）、
	// 验收 Run 命令打外来标记（verify-acceptance 首次执行前须 --trust-foreign）。单一真相源：
	// taskpipeline.StripForeignGateSignals——与 .forge migrate 路径共用，使 repo 提交的 task 文件
	// 无法从另一条路进入受信 DataDir。
	taskpipeline.StripForeignGateSignals(bundle.Task)

	// All state transitions take the per-task lock and reload INSIDE it (design §13): without the lock,
	// a concurrent resume auto-attach / task decide could save between our load and save, and this
	// import's write would silently clobber it — a lost-update on exactly the continuity data this
	// feature exists to preserve. The reload inside the lock discards the pre-lock snapshot so the
	// switch sees the latest on-disk state. SaveTaskState (not MutateTaskState) avoids double-locking.
	// DeleteTaskState only removes the .json (not the .lock), so the held lock survives a --force delete.
	//
	// 所有状态转换取 per-task 锁并在锁内重载（设计§13）：不加锁则并发 resume 自动锚定 / task decide
	// 可能在本进程 load 与 save 之间写入，本 import 的写会静默覆盖它——丢的恰是本特性要保的接续数据。
	// 锁内重载丢弃取锁前快照，使 switch 看到盘上最新。用 SaveTaskState（非 MutateTaskState）避免重复
	// 加锁。DeleteTaskState 只删 .json（不删 .lock），故 --force 删除后持锁仍有效。
	unlock, err := taskpipeline.LockTask(root, ref)
	if err != nil {
		return err
	}
	defer unlock()

	existing, loadErr := taskpipeline.LoadTaskState(root, ref)
	exists := loadErr == nil && existing != nil

	switch {
	case exists && !force && !merge:
		src := ``
		if bundle.SourceProject != `` {
			src = fmt.Sprintf(`（源自 %s）`, bundle.SourceProject)
		}
		return fmt.Errorf(`任务 %s 已存在%s；用 --force 覆盖或 --merge 合并协作记录`, ref, src)
	case exists && force:
		// Overwrite: drop the local task file entirely, then write the bundled task as-is.
		if err := taskpipeline.DeleteTaskState(root, ref); err != nil {
			return fmt.Errorf(`覆盖前删除既有任务失败: %w`, err)
		}
		if err := taskpipeline.SaveTaskState(root, bundle.Task); err != nil {
			return fmt.Errorf(`写入任务失败: %w`, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已覆盖导入任务 %s（--force，源自 %s）`, ref, originLabel(bundle.SourceProject))
		fmt.Fprintln(cmd.ErrOrStderr())
	case exists && merge:
		// Merge: local task keeps its identity/definition; collaborative records (decisions/findings/
		// blockers/history/sessions/next-steps/artifacts) are unioned by ID/key. Incoming links are ghosts.
		taskpipeline.MergeTaskState(existing, bundle.Task)
		if err := taskpipeline.SaveTaskState(root, existing); err != nil {
			return fmt.Errorf(`合并写入任务失败: %w`, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已合并导入任务 %s（--merge，源自 %s）`, ref, originLabel(bundle.SourceProject))
		fmt.Fprintln(cmd.ErrOrStderr())
	default:
		// Fresh: no local task — write the bundled task (links already ghosted, gate signals stripped).
		if err := taskpipeline.SaveTaskState(root, bundle.Task); err != nil {
			return fmt.Errorf(`写入任务失败: %w`, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已导入任务 %s（源自 %s）`, ref, originLabel(bundle.SourceProject))
		fmt.Fprintln(cmd.ErrOrStderr())
	}

	// Replay bundled checklog (if any) into the local evidence chain — preserves source timing so
	// `forge trace <ref>` reconstructs the cross-machine timeline. De-duped against the local checklog
	// first (filterImportedChecklog) so a repeated --merge import does not duplicate evidence lines.
	//
	// 回放 bundled checklog（若有）进本地证据链——保留源时序，使 forge trace <ref> 重建跨机器时间线。
	// 先对本地 checklog 去重（filterImportedChecklog），使重复 --merge import 不重复证据行。
	if len(bundle.Checklog) > 0 {
		toAppend := bundle.Checklog
		if fresh, derr := filterImportedChecklog(root, ref, bundle.Checklog); derr == nil {
			toAppend = fresh
		}
		// derr != nil → 读本地失败，best-effort 回落到全量追加（绝不在读故障时静默丢证据）。
		if len(toAppend) > 0 {
			if err := checklog.AppendEntries(root, toAppend); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), `⚠ 任务已导入，但回放 checklog 失败（不影响任务）: %v`, err)
				fmt.Fprintln(cmd.ErrOrStderr())
			}
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), `提示：导入不继承源机器的 review/验收/评分/完成状态/逃生舱；完成前请在本机重跑门禁。验收命令保留但标记为外来——首次 verify-acceptance 需人工审阅后加 --trust-foreign。接续用 forge task resume --ref %s`, ref)
	fmt.Fprintln(cmd.ErrOrStderr())
	return nil
}

// stripForeignGateSignals moved to taskpipeline.StripForeignGateSignals (trust.go) as the single
// source of truth shared with the .forge migrate path. See that function's godoc for the full
// field list and the 2026-08-15 control-flow bypass background.
//
// stripForeignGateSignals 已迁至 taskpipeline.StripForeignGateSignals（trust.go）作为与 .forge
// migrate 路径共用的单一真相源。完整字段清单与 2026-08-15 控制流绕过背景见该函数 godoc。

// filterImportedChecklog drops entries already present locally so a repeated --merge import does not
// duplicate evidence lines in `forge trace`. checklog lines carry no ID, so identity is the composite
// key RecordedAt+Check+Detail+SessionID+ToolName (stable across re-export of the same bundle; two
// genuinely distinct events differ in at least one). A read failure is non-fatal: the caller falls
// back to appending all entries — never silently drop evidence on a read glitch.
//
// filterImportedChecklog 丢弃本地已存在的条目，使重复 --merge import 不在 forge trace 里重复证据行。
// checklog 行无 ID，故身份取复合键 RecordedAt+Check+Detail+SessionID+ToolName（同一 bundle 重复导出
// 稳定；两条真正不同的事件至少差一项）。读失败非致命：调用方回落到全量追加——绝不在读故障时静默丢证据。
func filterImportedChecklog(root, taskRef string, incoming []checklog.Entry) ([]checklog.Entry, error) {
	var existing []checklog.Entry
	var err error
	if taskRef != `` {
		existing, err = checklog.LoadForTask(root, taskRef)
	} else {
		existing, err = checklog.LoadAll(root)
	}
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(existing)+len(incoming))
	for _, e := range existing {
		seen[checklogEntryKey(e)] = true
	}
	out := make([]checklog.Entry, 0, len(incoming))
	for _, e := range incoming {
		k := checklogEntryKey(e)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, e)
	}
	return out, nil
}

// checklogEntryKey is the stable identity of a checklog entry for import de-dup (see filterImportedChecklog).
//
// checklogEntryKey 是 checklog 条目用于 import 去重的稳定身份（见 filterImportedChecklog）。
func checklogEntryKey(e checklog.Entry) string {
	return fmt.Sprintf(`%d|%s|%s|%s|%s`, e.RecordedAt.UnixNano(), e.Check, e.Detail, e.SessionID, e.ToolName)
}

// originLabel renders the source-project root as a provenance label; empty → "外部".
//
// originLabel 把源项目根渲染成简短溯源标签；空 → 外部。
func originLabel(root string) string {
	if root == `` {
		return `外部`
	}
	return root
}

// cloneTaskState returns a deep copy of s via JSON round-trip. Used before redaction so the original
// is never mutated.
//
// cloneTaskState 经 JSON 往返返回 s 的深拷贝。用于脱敏前确保原件不被改。
func cloneTaskState(s *taskpipeline.TaskState) (*taskpipeline.TaskState, error) {
	data, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	var c taskpipeline.TaskState
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// redactedPlaceholder replaces identifying content while keeping the field non-empty so the SHAPE of
// the task (a decision exists, an assignment has an agent) stays visible in a shared bundle.
//
// redactedPlaceholder 替换身份内容但保持字段非空，使任务的形状（存在一条决策、分派有 agent）在分享的
// bundle 里仍可见。
const redactedPlaceholder = `[redacted]`

// redactTask strips identifying / evidence fields in place, keeping the task's SHAPE (status, gate
// progression, decision count) so a shared bundle stays useful as a template or for external help
// without leaking which issue / agent / commit / code-paths it touched. Structural enums (status,
// kind, gate IDs) are preserved.
//
// redactTask 就地抹除身份/证据字段，保留任务的形状（状态/门禁进度/决策数），使分享出去的 Bundle 仍可
// 当模板或对外求助，而不泄露碰过的 issue/agent/commit/代码路径。结构性枚举（状态/kind/门禁 ID）保留。
func redactTask(s *taskpipeline.TaskState) {
	// External issue provenance — clear entirely (tracker URL/ID identify the source project).
	s.ExternalOrigin = taskpipeline.ExternalOrigin{}
	// Free-text identity / code-path fields. Goal/Plan/Summary routinely embed file paths, endpoints,
	// customer names; PlanScope/Decisions.Affects are literal file globs; Acceptance.Run is a shell
	// command embedding package paths; Branch/ParentTaskRef/DependsOn carry feature/customer-shaped
	// names; NextSteps reference specifics; SessionID/SessionLinks[].SessionID are identity-adjacent.
	// Replace scalar text with the placeholder (keeps SHAPE: "has a goal/summary/agent"); clear lists.
	s.Goal = redactedPlaceholder
	s.Plan = redactedPlaceholder
	s.Summary = redactedPlaceholder
	s.PlanScope = nil
	s.Branch = ``
	s.ParentTaskRef = ``
	s.DependsOn = nil
	s.NextSteps = nil
	s.SessionID = ``
	for i := range s.SessionLinks {
		// SessionID 是身份，Tool 是发起该 session 的 agent 身份（claude-code/pi/codex…）——与 OriginTool
		// 同类，必须一并脱敏，否则「哪个 agent 干的」经 SessionLinks 残留。Imported 是 bool 形状标记（是否
		// 跨机器 import 带入），保留以维持结构。
		s.SessionLinks[i].SessionID = redactedPlaceholder
		s.SessionLinks[i].Tool = redactedPlaceholder
	}
	// Commit SHAs — leak repo state.
	s.HeadCommit = ``
	s.ReviewedHeadCommit = ``
	s.ReviewedChangeHash = ``
	for i := range s.History {
		s.History[i].HeadCommit = ``
	}
	// ReviewRounds carry the same per-round snapshot SHAs — same leak class as
	// ReviewedHeadCommit; keep the round COUNT (shape) but clear the hashes.
	//
	// ReviewRounds 带每轮快照 SHA——与 ReviewedHeadCommit 同类泄露；保留轮次数
	// （形状）但清掉哈希。
	for i := range s.ReviewRounds {
		s.ReviewRounds[i].HeadCommit = ``
		s.ReviewRounds[i].ChangeHash = ``
	}
	for i := range s.Acceptance {
		s.Acceptance[i].AcceptedHeadCommit = ``
		s.Acceptance[i].AcceptedBaseCommit = ``
		s.Acceptance[i].AcceptedChangeHash = ``
		s.Acceptance[i].Output = ``
		s.Acceptance[i].Run = redactedPlaceholder // 命令含包路径
		s.Acceptance[i].Expected = redactedPlaceholder
	}
	// Assignment identity + free-text reasons.
	if s.Assignment != nil {
		s.Assignment.Agent = redactedPlaceholder
		s.Assignment.OfferedBy = redactedPlaceholder
		s.Assignment.Role = redactedPlaceholder // 角色自由文本可带客户名（customer-acme-frontend）
		s.Assignment.LastQuestion = ``
		s.Assignment.FailReason = ``
		s.Assignment.CancelReason = ``
	}
	// Collaborative record content/evidence/code-paths. By/Source carry agent identity
	// ([pi]/[claude-code]/…) — same class as Assignment.Agent, redacted for consistency so the
	// "who" never survives even though the "what" (Content) is already replaced.
	//
	// 协作记录内容/证据/代码路径。By/Source 带 agent 身份（[pi]/[claude-code]/…）——与
	// Assignment.Agent 同类，一致脱敏，使「谁」绝不残留（尽管「什么」即 Content 已替换）。
	for i := range s.Decisions {
		s.Decisions[i].Content = redactedPlaceholder
		s.Decisions[i].By = redactedPlaceholder
		s.Decisions[i].Rationale = ``
		s.Decisions[i].Affects = nil
	}
	for i := range s.Findings {
		s.Findings[i].Content = redactedPlaceholder
		s.Findings[i].Source = redactedPlaceholder
		s.Findings[i].Evidence = ``
	}
	for i := range s.Blockers {
		s.Blockers[i].Content = redactedPlaceholder
		s.Blockers[i].By = redactedPlaceholder
		s.Blockers[i].Resolution = ``
	}
	for i := range s.Artifacts {
		s.Artifacts[i].Path = redactedPlaceholder
		s.Artifacts[i].Note = ``
	}
	// Originating tool is agent identity (pi/claude-code/opencode/codex/cursor…) — redact last so
	// every identity surface on TaskState is covered uniformly.
	//
	// 发起工具是 agent 身份（pi/claude-code/opencode/codex/cursor…）——最后脱敏，使 TaskState 上
	// 每个身份面被均匀覆盖。
	s.OriginTool = redactedPlaceholder
}

// projectBaseName returns the last path segment of a project root, dropping the leading
// directories that leak username/customer names (e.g. /Users/jsmith/projects/secret-foo →
// secret-foo, or E:\users\admin\Forge → Forge). Used only for the redacted bundle envelope so the
// absolute source path never leaves the machine. Rune literals (not double-quoted strings) keep
// this helper free of the ASCII-double-quote corruption on Windows edits.
//
// projectBaseName 返回项目根最后一段路径，丢掉前导目录（泄露用户名/客户名，如
// /Users/jsmith/projects/secret-foo → secret-foo，或 E:\users\admin\Forge → Forge）。仅用于脱敏
// bundle 信封，使绝对源路径不外泄。用 rune 字面量（非双引号字符串）避免 Windows 编辑时的双引号腐蚀。
func projectBaseName(p string) string {
	for len(p) > 0 && (p[len(p)-1] == '/' || p[len(p)-1] == '\\') {
		p = p[:len(p)-1]
	}
	name := p
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			name = p[i+1:]
			break
		}
	}
	// 盘根输入（"/" 修剪后为空、"C:\" 修剪后为 "C:"）无项目名可取——回退中性占位，避免信封名为空
	// 或泄露盘符。bare-drive 形如 len==2、第二字符 ':'、首字符为字母（用 rune 字面量避免双引号腐蚀）。
	if len(name) == 0 {
		return `redacted-project`
	}
	if len(name) == 2 && name[1] == ':' {
		c := name[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return `redacted-project`
		}
	}
	return name
}

// redactChecklogEntries scrubs identity/path-carrying fields of checklog entries in place so a
// --redact --include-checklog bundle does not leak absolute paths, command output, or session
// identity embedded in Detail/SessionID/ToolName. Structural fields (Check/Passed/Checked/Level/
// Source/RecordedAt/TaskRef) are preserved — they carry the evidence-chain SHAPE (which check ran,
// when, pass/fail, which subsystem) without identity. Detail becomes the placeholder so trace still
// shows "an entry existed here" rather than a silent gap.
//
// redactChecklogEntries 就地抹除 checklog 条目里携带身份/路径的字段，使 --redact --include-checklog
// 的 bundle 不外泄 Detail/SessionID/ToolName 里嵌入的绝对路径/命令输出/会话身份。结构性字段
// （Check/Passed/Checked/Level/Source/RecordedAt/TaskRef）保留——承载证据链「形状」（跑了哪个检查、
// 何时、通过与否、哪个子系统）而无身份。Detail 置占位符，使 trace 仍显示「此处曾有条目」而非空白。
func redactChecklogEntries(entries []checklog.Entry) {
	for i := range entries {
		entries[i].SessionID = redactedPlaceholder
		entries[i].Detail = redactedPlaceholder
		entries[i].ToolName = ``
	}
}
