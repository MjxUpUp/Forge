package clitask

import (
	"fmt"
	"time"

	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

// task_questioning.go — oracle-pipeline L2 机器出题三命令（mutation/fuzz/
// edgecheck）。「写码快但交付质量待研究」的正面回答：bad case 不再靠实现者
// 自觉出题——变异体逐个注入、fuzz 批量生成、edge 清单机械枚举，全部 forge
// 亲自执行并落 deterministic 证据。

var taskMutationCmd = &cobra.Command{
	Use:   "mutation [--sample N]",
	Short: "mutation 抽样：对任务改动注入变异体并实跑包测试（杀不死=假绿，bad case 机器出题）",
	Long: `mutation 抽样（oracle-pipeline L2a）：扫描任务改动的 Go 源文件的算子位点
（比较翻转/逻辑翻转），确定性抽样后逐个原地变异、实跑所在包 go test、
立即还原。杀不死变异体的测试是装饰品——断言弱化/空断言无论覆盖率多高
都查不出；每个变异体就是一道注入的 bad case。

判定（go 工具退出码契约）：测试失败=杀灭；构建失败=无效体（编译器抓住的
不算测试的功）；通过=存活（stderr 逐位点点名）。证据：一条 mutation-sampling
checklog 行（deterministic，计入证据强度）。原地变异带 sidecar 备份护栏
（<file>.forge-mutbak）——还原失败指路备份文件（勿用 git checkout：会连带
销毁任务未提交改动）；进程被杀时备份残留，下次起跑检测到即拒绝执行。
advisory 不阻断。--sample 控制抽样数（默认 6）；单变异体超时
FORGE_MUTATION_TIMEOUT（默认 3m）。`,
	RunE: runTaskMutation,
}

var taskFuzzCmd = &cobra.Command{
	Use:   "fuzz [--sec N] [--max N]",
	Short: "fuzz 实跑：发现任务改动包的 Fuzz 目标并按预算执行（Go 原生引擎，机器批量出题）",
	Long: `fuzz 实跑（oracle-pipeline L2b）：扫描任务改动文件所在目录的 _test.go，
发现 FuzzXxx 目标后按预算逐个执行 go test -fuzz。edge case 清单再全也是
人枚举的——fuzz 是无记忆的对抗者。失败目标的崩溃输入由 go 写进 testdata/
（证据即语料，修完 go test 直接回归）。证据：一条 fuzz-run checklog 行
（deterministic）。--sec 每目标预算（默认 30s）；--max 最多跑几个目标
（默认 3，其余记 skipped）。advisory 不阻断。`,
	RunE: runTaskFuzz,
}

var taskEdgecheckCmd = &cobra.Command{
	Use:   "edgecheck",
	Short: "edge case 清单：任务改动的每个导出函数 × 五维（基数/值域/时序/环境/故障）机械枚举",
	Long: `edge case 清单（oracle-pipeline L2c）：把「出 edge case」从灵感变流水线——
任务改动的每个导出函数按五个维度（基数/值域/时序/环境/故障）生成可勾选
的清单骨架，落 <DataDir>/edgecases/<ref>.md。机器出骨架、验收前人或 agent
逐项填断言——spec 评审与测试计划的直接输入。确定性生成（文件序×声明序）。`,
	RunE: runTaskEdgecheck,
}

func init() {
	Root.AddCommand(taskMutationCmd)
	Root.AddCommand(taskFuzzCmd)
	Root.AddCommand(taskEdgecheckCmd)
	taskMutationCmd.Flags().Int("sample", 6, "抽样变异体数量（确定性均匀取样）")
	taskFuzzCmd.Flags().Int("sec", 30, "每目标 fuzz 预算（秒）")
	taskFuzzCmd.Flags().Int("max", 3, "最多实跑的目标数（其余记 skipped）")
}

func runTaskMutation(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	sample, _ := cmd.Flags().GetInt("sample")
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return fmt.Errorf("failed to load task state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("no active task——mutation 抽样作用于当前任务的改动集（forge task start / --ref 族命令不适用：抽样结果要落进任务证据链）")
	}
	res, checked, err := taskpipeline.RunMutationSampling(root, state, sample)
	if err != nil {
		return err
	}
	if !checked {
		fmt.Println("任务改动集中无 Go 非测试源文件或无可变异位点——抽样空转（Checked=false，不落证据行）")
		return nil
	}
	fmt.Print(taskpipeline.FormatMutationSummary(res))
	if res.Survived > 0 {
		fmt.Println("→ 按存活位点补断言后重跑本命令；证据已落 checklog（advisory 不阻断 complete）")
		return nil
	}
	fmt.Println("✅ 全部杀灭——考卷对注入 bad case 的防御有效")
	return nil
}

func runTaskFuzz(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	sec, _ := cmd.Flags().GetInt("sec")
	maxN, _ := cmd.Flags().GetInt("max")
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return fmt.Errorf("failed to load task state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("no active task——fuzz 作用于当前任务改动包的 Fuzz 目标（证据要落进任务链）")
	}
	targets := taskpipeline.DiscoverFuzzTargets(root, state)
	if len(targets) == 0 {
		fmt.Println("任务改动目录中未发现 Fuzz 目标（FuzzXxx in _test.go）——Checked=false。可以先补一个 fuzz 函数把不变式交给机器出题")
		return nil
	}
	fmt.Printf("发现 %d 个 Fuzz 目标，预算 %ds/目标，实跑前 %d 个：\n", len(targets), sec, maxN)
	for _, t := range targets {
		fmt.Printf("  - %s.%s（%s）\n", t.Pkg, t.Func, t.File)
	}
	res, err := taskpipeline.RunFuzz(root, state, targets, time.Duration(sec)*time.Second, maxN)
	if err != nil {
		return err
	}
	for _, f := range res.Findings {
		fmt.Printf("  ❌ %s\n", f)
	}
	if len(res.Failed) == 0 {
		fmt.Printf("✅ 预算内零发现（run %d / skip %d）——证据已落 checklog\n", res.Run, len(res.Skipped))
		return nil
	}
	fmt.Println("→ 修完以 go test <pkg> 回归（崩溃输入在 testdata/）；证据已落 checklog（advisory）")
	return nil
}

func runTaskEdgecheck(cmd *cobra.Command, args []string) error {
	root, err := projectroot.Find()
	if err != nil {
		return err
	}
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return fmt.Errorf("failed to load task state: %w", err)
	}
	if state == nil {
		return fmt.Errorf("no active task——edgecheck 作用于当前任务的改动集")
	}
	art, ok := taskpipeline.GenerateEdgeChecklist(root, state)
	if !ok {
		fmt.Println("任务改动集中无可枚举的导出函数（非 Go 或无导出）——清单空转")
		return nil
	}
	fmt.Printf("edge case 清单已生成：%s（改动文件 %d / 导出函数 %d / 条目 %d）\n", art.Path, len(art.Changed), art.Funcs, art.Items)
	fmt.Println("→ 验收前逐项填断言/勾选——五维：基数/值域/时序/环境/故障")
	return nil
}
