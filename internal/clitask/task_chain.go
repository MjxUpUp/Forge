package clitask

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/artifactchain"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_chain.go —— 产物链 CLI（artifact-chain-workflow.md §3）：`forge task
// artifact` 单命令、动作全走互斥 flags（命令面冻结预算：净增 1 命令）。
// 写入经 taskpipeline.WriteArtifact（文件拥有内容，状态持哈希引用，I5）；
// 审批签内容哈希（漂移即作废）；提取把产物验收标准编译进既有验收管道。
func init() {
	Root.AddCommand(taskArtifactCmd)
	taskStartCmd.Flags().StringArray("artifact", nil, `开工即登记产物（可重复 --artifact "<stage>=<path>"）：读文件内容落 specs 目录并折哈希引用进任务（spec-kit 式「先写 spec 再开工」路径）`)
}

var taskArtifactCmd = &cobra.Command{
	Use:   "artifact --set <stage> --file <path> | --list | --verify | --approve <stage> | --extract",
	Short: "产物链操作（L6 契约层：登记/查看/漂移校验/人工审批/验收提取）",
	RunE:  runTaskArtifact,
}

func runTaskArtifact(cmd *cobra.Command, args []string) error {
	setStage, _ := cmd.Flags().GetString("set")
	list, _ := cmd.Flags().GetBool("list")
	verify, _ := cmd.Flags().GetBool("verify")
	approveStage, _ := cmd.Flags().GetString("approve")
	extract, _ := cmd.Flags().GetBool("extract")
	actions := 0
	for _, on := range []bool{setStage != "", list, verify, approveStage != "", extract} {
		if on {
			actions++
		}
	}
	if actions != 1 {
		return fmt.Errorf("恰好一个动作：--set <stage> --file <path> | --list | --verify | --approve <stage> | --extract")
	}
	switch {
	case setStage != "":
		file, _ := cmd.Flags().GetString("file")
		useStdin, _ := cmd.Flags().GetBool("stdin")
		return runArtifactSet(setStage, file, useStdin)
	case list:
		asJSON, _ := cmd.Flags().GetBool("json")
		return runArtifactList(cmd, asJSON)
	case verify:
		return runArtifactVerify()
	case approveStage != "":
		by, _ := cmd.Flags().GetString("by")
		return runArtifactApprove(approveStage, by)
	default:
		return runArtifactExtract()
	}
}

// activeTask mirrors mutateActiveTask's resolution but returns the state for
// read-side decisions before mutation.
func activeTask(root string) (*taskpipeline.TaskState, error) {
	st, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, fmt.Errorf("无活跃任务——产物命令作用于当前任务（forge task start / forge next）")
	}
	return st, nil
}

// runArtifactSet 登记一个 stage 产物：读文件 → WriteArtifact 落 specs → 折哈希
// 引用进 state。重登记同一 stage = 内容更新：旧审批随之作废（哈希不再匹配的
// 审批不是审批——§5 事实语义在写入侧同样成立）。
func runArtifactSet(stage, file string, useStdin bool) error {
	if useStdin == (file != "") {
		return fmt.Errorf("--file <path> 与 --stdin 二选一")
	}
	var content string
	if useStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("读取 stdin 失败: %w", err)
		}
		content = string(data)
	} else {
		data, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("读取 %q 失败: %w", file, err)
		}
		content = string(data)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("产物内容为空——拒绝登记空产物")
	}
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	st, err := activeTask(root)
	if err != nil {
		return err
	}
	if _, known := chain.StageByName(stage); !known {
		fmt.Fprintf(os.Stderr, "ℹ️ stage %q 不在链声明中（schema: %s）——按链外产物处理（漂移仅留痕不执法）\n", stage, artifactchain.SchemaPath(root))
	}
	requires := append([]string(nil), chainStageRequires(chain, stage)...)
	var ref taskpipeline.ArtifactRef
	if err := taskpipeline.MutateTaskState(root, st.TaskRef, func(s *taskpipeline.TaskState) error {
		aref, aerr := taskpipeline.WriteArtifact(root, st.TaskRef, stage, content)
		if aerr != nil {
			return aerr
		}
		ref = aref
		if s.SpecArtifacts == nil {
			s.SpecArtifacts = map[string]taskpipeline.ArtifactRef{}
		}
		s.SpecArtifacts[stage] = aref
		// 重登记 = 内容更新 → 旧审批作废（若审批哈希与新内容不一致）。
		if apr, has := s.ArtifactApprovals[stage]; has && apr.Hash != aref.Hash {
			delete(s.ArtifactApprovals, stage)
			fmt.Fprintln(os.Stderr, "ℹ️ 内容已变，该 stage 旧审批已作废（human 档需 forge task artifact --approve 重批）")
		}
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("产物已登记：%s → %s (hash %s)\n", stage, ref.Path, ref.Hash)
	// 链序提示（advisory 语义，不拦——执法在 gate）：前置缺失给一次性提醒。
	var missingReq []string
	for _, r := range requires {
		if _, has := st.SpecArtifacts[r]; !has {
			missingReq = append(missingReq, r)
		}
	}
	if len(missingReq) > 0 {
		mode := string(chainStageMode(chain, stage))
		fmt.Fprintf(os.Stderr, "⚠️ 前置产物未登记: %s（gate 处 %s 档将阻断）\n", strings.Join(missingReq, ", "), mode)
	}
	return nil
}

func chainStageRequires(chain *artifactchain.Chain, stage string) []string {
	if s, ok := chain.StageByName(stage); ok {
		return s.Requires
	}
	return nil
}

func chainStageMode(chain *artifactchain.Chain, stage string) artifactchain.Mode {
	if s, ok := chain.StageByName(stage); ok {
		return s.Mode
	}
	return artifactchain.ModeAdvisory
}

// runArtifactList renders the chain + artifact state table (text or JSON).
func runArtifactList(cmd *cobra.Command, asJSON bool) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	st, err := activeTask(root)
	if err != nil {
		return err
	}
	type row struct {
		Stage    string `json:"stage"`
		Mode     string `json:"mode"`
		Produces string `json:"produces"`
		Requires string `json:"requires,omitempty"`
		Ref      string `json:"ref,omitempty"`
		Hash     string `json:"hash,omitempty"`
		Drifted  bool   `json:"drifted"`
		Approved bool   `json:"approved"`
		Missing  bool   `json:"missing"`
	}
	rows := make([]row, 0, len(chain.Stages))
	for _, s := range chain.Stages {
		r := row{Stage: s.Name, Mode: string(s.Mode), Produces: s.Produces, Requires: strings.Join(s.Requires, ",")}
		ref, has := st.SpecArtifacts[s.Name]
		if !has {
			r.Missing = true
		} else {
			r.Ref, r.Hash = ref.Path, ref.Hash
			r.Drifted = taskpipeline.ArtifactCurrentHash(root, ref) != ref.Hash
			_, r.Approved = st.ArtifactApprovals[s.Name]
		}
		rows = append(rows, r)
	}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	}
	fmt.Printf("任务 %s 的产物链（schema: %s）：\n", st.TaskRef, artifactchain.SchemaPath(root))
	for _, r := range rows {
		status := "✓"
		switch {
		case r.Missing:
			status = "缺失"
		case r.Drifted:
			status = "漂移✗"
		}
		line := fmt.Sprintf("  %-12s %-8s %-14s %s", r.Stage, r.Mode, r.Produces, status)
		if r.Hash != "" {
			line += fmt.Sprintf(" (%s)", r.Hash)
		}
		if r.Approved {
			line += " [已审批]"
		}
		if r.Requires != "" {
			line += fmt.Sprintf(" 前置: %s", r.Requires)
		}
		fmt.Println(line)
	}
	return nil
}

// runArtifactVerify re-verifies every registered ref (drift rows land in checklog).
func runArtifactVerify() error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	st, err := activeTask(root)
	if err != nil {
		return err
	}
	drifted := taskpipeline.VerifyArtifactsReport(root, st)
	if len(drifted) == 0 {
		fmt.Printf("全部 %d 个产物引用校验通过（无漂移）\n", len(st.SpecArtifacts))
		return nil
	}
	fmt.Printf("发现 %d 处漂移：\n", len(drifted))
	for _, d := range drifted {
		fmt.Println("  - " + d)
	}
	fmt.Println("重登记: forge task artifact --set <stage> --file <path>；漂移在 hard/human 档将阻断 complete")
	return nil
}

// runArtifactApprove records the human-tier approval: the approval signs the
// CURRENT registered content hash (fact) — drifted or unregistered artifacts
// refuse approval.
func runArtifactApprove(stage, by string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	s, known := chain.StageByName(stage)
	if !known {
		return fmt.Errorf("stage %q 不在链声明中（%s）——审批只对链内 stage 有意义", stage, artifactchain.SchemaPath(root))
	}
	if s.Mode != artifactchain.ModeHuman {
		return fmt.Errorf("stage %q 档位是 %s（审批只在 human 档有意义；改档请编辑 schema.yaml）", stage, s.Mode)
	}
	st, err := activeTask(root)
	if err != nil {
		return err
	}
	if by == "" {
		by = taskpipeline.CurrentSessionID()
	}
	if by == "" {
		return fmt.Errorf("审批人缺失：--by <审批人标识>（审批必须可归因到人）")
	}
	var approved taskpipeline.ArtifactApproval
	if err := taskpipeline.MutateTaskState(root, st.TaskRef, func(ts *taskpipeline.TaskState) error {
		ref, has := ts.SpecArtifacts[stage]
		if !has {
			return fmt.Errorf("stage %q 产物未登记——先 forge task artifact --set %s --file <path>", stage, stage)
		}
		cur := taskpipeline.ArtifactCurrentHash(root, ref)
		if cur == "" || cur != ref.Hash {
			return fmt.Errorf("stage %q 引用已漂移（登记后文件被改）——先重登记再审批", stage)
		}
		approved = taskpipeline.ArtifactApproval{By: by, At: time.Now(), Hash: cur}
		if ts.ArtifactApprovals == nil {
			ts.ArtifactApprovals = map[string]taskpipeline.ArtifactApproval{}
		}
		ts.ArtifactApprovals[stage] = approved
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("已审批 %s（by %s，hash %s）——内容再改即作废\n", stage, approved.By, approved.Hash)
	return nil
}

// runArtifactExtract compiles acceptance criteria out of registered artifacts
// (chain order) into the task's acceptance set via the same MergeAcceptance
// dedup as --plan-file.
func runArtifactExtract() error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	chain, warns := artifactchain.Load(root)
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, w)
	}
	st, err := activeTask(root)
	if err != nil {
		return err
	}
	// 链序提取：上游产物先提（显式 --accept 仍优先，MergeAcceptance 按 Run 去重）。
	var stages []string
	for _, s := range chain.Stages {
		if _, has := st.SpecArtifacts[s.Name]; has {
			stages = append(stages, s.Name)
		}
	}
	// 链外登记的产物也提取（排序保证确定性）。
	var extra []string
	for name := range st.SpecArtifacts {
		if _, inChain := chain.StageByName(name); !inChain {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	stages = append(stages, extra...)
	added := 0
	var perStage []string
	if err := taskpipeline.MutateTaskState(root, st.TaskRef, func(s *taskpipeline.TaskState) error {
		for _, name := range stages {
			ref := s.SpecArtifacts[name]
			extracted, xerr := taskpipeline.ParseAcceptanceFromArtifactFile(taskpipeline.ArtifactAbsPath(root, ref))
			if xerr != nil {
				fmt.Fprintf(os.Stderr, "⚠️ %s 产物不可读（%v）——跳过\n", name, xerr)
				continue
			}
			if len(extracted) == 0 {
				continue
			}
			before := len(s.Acceptance)
			s.Acceptance = taskpipeline.MergeAcceptance(s.Acceptance, extracted)
			if n := len(s.Acceptance) - before; n > 0 {
				perStage = append(perStage, fmt.Sprintf("%s+%d", name, n))
				added += n
			}
		}
		return nil
	}); err != nil {
		return err
	}
	if added == 0 {
		fmt.Println("未提取到新验收标准（显式 --accept 已覆盖，或产物无 accept:/Run:/```accept 形态）")
		return nil
	}
	fmt.Printf("已从产物提取 %d 条验收标准（%s）——forge task verify-acceptance 实跑回扣\n", added, strings.Join(perStage, ", "))
	return nil
}
