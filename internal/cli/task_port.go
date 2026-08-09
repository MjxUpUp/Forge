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

	bundle := taskBundle{
		SchemaVersion: taskBundleSchemaVersion,
		ExportedAt:    time.Now(),
		SourceProject: root,
		Task:          task,
		Checklog:      entries,
		Redacted:      redact,
	}
	data, err := json.MarshalIndent(bundle, ``, `  `)
	if err != nil {
		return fmt.Errorf(`序列化 Bundle 失败: %w`, err)
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
	fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已导出任务 %s 到 %s%s\n`, state.TaskRef, out, note)
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
	// source machine, never a local anchor. Done once, up front, so every strategy sees ghosted links.
	//
	// 所有传入的 session 链接在本机都成为幽灵：记录源机器谁参与过，永非本机锚点。一次性前置完成，
	// 使每种策略都看到已幽灵化的链接。
	for i := range bundle.Task.SessionLinks {
		bundle.Task.SessionLinks[i].Imported = true
	}

	// Imported review/acceptance/score signals are FOREIGN evidence — never trusted locally. Without
	// stripping, a hand-edited or buggy bundle carrying ReviewPassed=true + Acceptance Passed/
	// AcceptedHeadCommit=current-HEAD would satisfy the task-complete gate's hard prerequisites
	// without the review sub-agent / verify-acceptance ever running on THIS machine. History (gate
	// progression) is kept: it never satisfies task-complete (the complete gate itself isn't in
	// History until it passes here), and re-running an earlier gate is cheap + idempotent.
	//
	// 导入的 review/验收/评分信号是外来证据——本机绝不信任。不剥离则手改/有 bug 的 bundle 带上
	// ReviewPassed=true + 验收 Passed/AcceptedHeadCommit=当前 HEAD，就能满足 task-complete 门禁的硬
	// 前置，而本机从未跑过 review 子 agent / verify-acceptance。History（门禁进度）保留：它永不满足
	// task-complete（complete 门禁自身在通过前不在 History），且重跑早先门禁廉价且幂等。
	stripForeignGateSignals(bundle.Task)

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
		fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已覆盖导入任务 %s（--force，源自 %s）\n`, ref, originLabel(bundle.SourceProject))
	case exists && merge:
		// Merge: local task keeps its identity/definition; collaborative records (decisions/findings/
		// blockers/history/sessions/next-steps/artifacts) are unioned by ID/key. Incoming links are ghosts.
		mergeTaskState(existing, bundle.Task)
		if err := taskpipeline.SaveTaskState(root, existing); err != nil {
			return fmt.Errorf(`合并写入任务失败: %w`, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已合并导入任务 %s（--merge，源自 %s）\n`, ref, originLabel(bundle.SourceProject))
	default:
		// Fresh: no local task — write the bundled task (links already ghosted, gate signals stripped).
		if err := taskpipeline.SaveTaskState(root, bundle.Task); err != nil {
			return fmt.Errorf(`写入任务失败: %w`, err)
		}
		fmt.Fprintf(cmd.ErrOrStderr(), `✓ 已导入任务 %s（源自 %s）\n`, ref, originLabel(bundle.SourceProject))
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
				fmt.Fprintf(cmd.ErrOrStderr(), `⚠ 任务已导入，但回放 checklog 失败（不影响任务）: %v\n`, err)
			}
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(), `提示：导入不继承源机器的 review/验收/评分；完成前请在本机重跑门禁。接续用 forge task resume --ref %s\n`, ref)
	return nil
}

// stripForeignGateSignals clears imported review/acceptance/score signals so the local machine must
// re-run the gates to establish its own evidence (see runTaskImport). History is intentionally kept
// as provenance — it never satisfies the task-complete hard prerequisite.
//
// stripForeignGateSignals 清除导入的 review/验收/评分信号，使本机须重跑门禁建立自己的证据（见
// runTaskImport）。History 刻意保留为溯源——它永不满足 task-complete 的硬前置。
func stripForeignGateSignals(s *taskpipeline.TaskState) {
	s.ReviewPassed = false
	s.ReviewedHeadCommit = ``
	s.ReviewedChangeHash = ``
	s.Score = nil
	for i := range s.Acceptance {
		s.Acceptance[i].Passed = false
		s.Acceptance[i].AcceptedHeadCommit = ``
		s.Acceptance[i].Output = ``
	}
}

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
		s.SessionLinks[i].SessionID = redactedPlaceholder // 保留 Tool/Imported 以维持形状
	}
	// Commit SHAs — leak repo state.
	s.HeadCommit = ``
	s.ReviewedHeadCommit = ``
	s.ReviewedChangeHash = ``
	for i := range s.History {
		s.History[i].HeadCommit = ``
	}
	for i := range s.Acceptance {
		s.Acceptance[i].AcceptedHeadCommit = ``
		s.Acceptance[i].Output = ``
		s.Acceptance[i].Run = redactedPlaceholder // 命令含包路径
		s.Acceptance[i].Expected = redactedPlaceholder
	}
	// Assignment identity + free-text reasons.
	if s.Assignment != nil {
		s.Assignment.Agent = redactedPlaceholder
		s.Assignment.OfferedBy = redactedPlaceholder
		s.Assignment.LastQuestion = ``
		s.Assignment.FailReason = ``
		s.Assignment.CancelReason = ``
	}
	// Collaborative record content/evidence/code-paths.
	for i := range s.Decisions {
		s.Decisions[i].Content = redactedPlaceholder
		s.Decisions[i].Rationale = ``
		s.Decisions[i].Affects = nil
	}
	for i := range s.Findings {
		s.Findings[i].Content = redactedPlaceholder
		s.Findings[i].Evidence = ``
	}
	for i := range s.Blockers {
		s.Blockers[i].Content = redactedPlaceholder
		s.Blockers[i].Resolution = ``
	}
	for i := range s.Artifacts {
		s.Artifacts[i].Path = redactedPlaceholder
		s.Artifacts[i].Note = ``
	}
}

// mergeTaskState unions incoming collaborative records into local (mutates local). Local task
// identity/definition (Goal/Plan/Summary/Kind/OriginTool/Assignment/DependsOn/ParentTaskRef/gates)
// is authoritative and untouched — merge only grows the shared evidence/decision/history sets, so a
// re-merge is idempotent for the TASK STATE. (Checklog replay is idempotent separately:
// filterImportedChecklog drops entries already on disk before Append.) Incoming session links are
// already ghosted (Imported=true) by the caller.
//
// mergeTaskState 把传入的协作记录并集进 local（改 local）。本地任务身份/定义（Goal/Plan/Summary/
// Kind/OriginTool/Assignment/DependsOn/ParentTaskRef/门禁）为权威不动——合并只增长共享的
// 证据/决策/历史集合，故重复合并幂等。传入的 session 链接已由调用方幽灵化（Imported=true）。
func mergeTaskState(local, incoming *taskpipeline.TaskState) {
	// SessionLinks: append incoming ghost links whose sid isn't already present (any link, local or
	// ghost — a re-import of the same ghost must not duplicate).
	for _, in := range incoming.SessionLinks {
		if !local.HasAnySession(in.SessionID) {
			local.SessionLinks = append(local.SessionLinks, in)
		}
	}
	local.Decisions = unionDecisions(local.Decisions, incoming.Decisions)
	local.Findings = unionFindings(local.Findings, incoming.Findings)
	local.Blockers = unionBlockers(local.Blockers, incoming.Blockers)
	// History (gate results): keyed by Gate — keep local's result when both have one (authoritative,
	// never downgraded by a remote snapshot), else add the incoming gate the local task hasn't reached.
	local.History = unionGateHistory(local.History, incoming.History)
	local.NextSteps = unionStrings(local.NextSteps, incoming.NextSteps)
	local.Artifacts = unionArtifacts(local.Artifacts, incoming.Artifacts)
}

func unionDecisions(local, incoming []taskpipeline.Decision) []taskpipeline.Decision {
	// Forge always assigns unique IDs (newContinuityID); an empty ID = malformed bundle. Empty-ID
	// entries are appended as-is and never deduped (deduping them would collapse N distinct entries
	// into one, silently losing data). Only non-empty IDs participate in the seen-set.
	//
	// Forge 恒赋唯一 ID（newContinuityID）；空 ID = 畸形 bundle。空 ID 条目原样追加、永不参与去重
	// （去重会把 N 条不同条目压成一条，静默丢数据）。只有非空 ID 进 seen 集合。
	seen := map[string]bool{}
	for _, d := range local {
		if d.ID != `` {
			seen[d.ID] = true
		}
	}
	for _, d := range incoming {
		if d.ID != `` && seen[d.ID] {
			continue
		}
		if d.ID != `` {
			seen[d.ID] = true
		}
		local = append(local, d)
	}
	return local
}

func unionFindings(local, incoming []taskpipeline.Finding) []taskpipeline.Finding {
	seen := map[string]bool{}
	for _, f := range local {
		if f.ID != `` {
			seen[f.ID] = true
		}
	}
	for _, f := range incoming {
		if f.ID != `` && seen[f.ID] {
			continue
		}
		if f.ID != `` {
			seen[f.ID] = true
		}
		local = append(local, f)
	}
	return local
}

func unionBlockers(local, incoming []taskpipeline.Blocker) []taskpipeline.Blocker {
	seen := map[string]bool{}
	for _, b := range local {
		if b.ID != `` {
			seen[b.ID] = true
		}
	}
	for _, b := range incoming {
		if b.ID != `` && seen[b.ID] {
			continue
		}
		if b.ID != `` {
			seen[b.ID] = true
		}
		local = append(local, b)
	}
	return local
}

// unionGateHistory merges gate results keyed by Gate; local wins on conflict (authoritative), and an
// incoming gate absent locally is appended so a remote task further along contributes its gate
// progression without overriding what the local task already passed.
//
// unionGateHistory 按 Gate 合并门禁结果；冲突时 local 胜（权威），本地缺失的门禁追加进来，使进度更远
// 的远端 task 贡献其门禁进度而不覆盖本地已通过的。
func unionGateHistory(local, incoming []taskpipeline.TaskGateResult) []taskpipeline.TaskGateResult {
	seen := map[string]bool{}
	for _, h := range local {
		seen[h.Gate] = true
	}
	for _, h := range incoming {
		if !seen[h.Gate] {
			seen[h.Gate] = true
			local = append(local, h)
		}
	}
	return local
}

func unionStrings(local, incoming []string) []string {
	seen := map[string]bool{}
	for _, s := range local {
		seen[s] = true
	}
	for _, s := range incoming {
		if !seen[s] {
			seen[s] = true
			local = append(local, s)
		}
	}
	return local
}

func unionArtifacts(local, incoming []taskpipeline.Artifact) []taskpipeline.Artifact {
	seen := map[string]bool{}
	for _, a := range local {
		if a.Path != `` {
			seen[a.Path] = true
		}
	}
	for _, a := range incoming {
		if a.Path != `` && seen[a.Path] {
			continue
		}
		if a.Path != `` {
			seen[a.Path] = true
		}
		local = append(local, a)
	}
	return local
}
