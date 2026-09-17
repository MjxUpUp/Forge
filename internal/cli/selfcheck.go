package cli

// selfcheck.go — discipline-first-gates P2：门禁的镜像自检命令。`forge selfcheck
// pairing|scope` 对当前活跃任务跑与 verify 门禁**同一代码路径**的计算（taskpipeline
// 侧导出），落痕 selfcheck 条目——后续 verify 失败在条目在场时归 confirmation
// （agent 已持有事实而未行动）。守纪律 = 任意时刻秒级可自检，摩擦最低。

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

var selfcheckCmd = &cobra.Command{
	Use:   "selfcheck",
	Short: "门禁镜像自检：与 task-verify 同一计算路径的秒级自检",
	Long: `forge selfcheck — 对当前活跃任务跑门禁的镜像计算。

pairing 镜像 test-coverage 门禁（测试配对），scope 镜像 scope-drift advisory。
计算与门禁逐字同源；每次运行落 selfcheck 条目——其后失败的 verify 门禁将归
confirmation（已知未行动）而非 discovery（第一防线缺位）。
发现项不阻断（exit 1 供脚本感知），修复指引随输出给出。`,
}

var selfcheckPairingCmd = &cobra.Command{
	Use:   "pairing",
	Short: "测试配对自检（镜像 test-coverage 门禁）",
	RunE:  runSelfcheckPairing,
}

var selfcheckScopeCmd = &cobra.Command{
	Use:   "scope",
	Short: "PlanScope 漂移自检（镜像 scope-drift advisory）",
	RunE:  runSelfcheckScope,
}

func init() {
	selfcheckCmd.AddCommand(selfcheckPairingCmd, selfcheckScopeCmd)
	// 命令树分组契约（aa_groups）：顶层命令必须归组，否则 --help 游离且
	// AddCommand panic（TestCommandGroups_AllTopLevelGrouped 守卫）。selfcheck
	// 是质量自检入口，归 quality 组（与 task 门禁树同组）。
	selfcheckCmd.GroupID = "quality"
	rootCmd.AddCommand(selfcheckCmd)
}

// activeStateForSelfcheck 解析当前活跃任务；无任务即报错——selfcheck 预演的是
// 「任务的」门禁，无任务就没有可预演的对象。
func activeStateForSelfcheck() (string, *taskpipeline.TaskState, error) {
	root, err := findProjectRoot()
	if err != nil {
		return "", nil, err
	}
	state, err := taskpipeline.ActiveTaskState(root, taskpipeline.CurrentSessionID())
	if err != nil {
		return "", nil, err
	}
	if state == nil {
		return root, nil, errors.New("无活跃任务——selfcheck 预演的是任务的门禁（forge task start 后再跑）")
	}
	return root, state, nil
}

// recordSelfcheckEntry 落 selfcheck 条目（deterministic 自检观察；落盘失败只警
// 告不阻断——与 taskpipeline.recordAudit 同款纪律，审计自身绝不拖垮命令）。
func recordSelfcheckEntry(root, ref string, check checklog.CheckName, passed bool, detail string, meta map[string]string) {
	entry := &checklog.Entry{
		Check:   check,
		Passed:  passed,
		Checked: true,
		TaskRef: ref,
		Detail:  detail,
		Source:  checklog.EvidenceDeterministic,
		Level:   checklog.LevelAdvisory,
		Meta:    meta,
	}
	if err := checklog.Record(root, entry); err != nil {
		fmt.Fprintf(os.Stderr, "[selfcheck] warning: checklog record failed: %v\n", err)
	}
}

func runSelfcheckPairing(cmd *cobra.Command, args []string) error {
	root, state, err := activeStateForSelfcheck()
	if err != nil {
		return err
	}
	// 镜像诚实义务（守护监督 P2-3）：escape 下门禁豁免本检查、selfcheck 照报
	// 事实——不标注口径差会反向教出「逃生=全干净」。
	if taskpipeline.TestCoverageEscapeActive(state) {
		fmt.Println("⚠ escape active（override/FORGE_TEST_COVERAGE）：门禁已豁免本检查——以下输出是事实，不是门禁预演")
	}
	missing, total := taskpipeline.SelfcheckPairing(root, state)
	metaList := missing
	if len(metaList) > 8 {
		metaList = metaList[:8]
	}
	recordSelfcheckEntry(root, state.TaskRef, checklog.CheckSelfcheckPairing, len(missing) == 0,
		fmt.Sprintf("selfcheck pairing: %d missing / %d changed source files", len(missing), total),
		map[string]string{
			"missing_files": fmt.Sprintf("%d", len(missing)),
			"missing_list":  strings.Join(metaList, ","),
		})
	if len(missing) == 0 {
		fmt.Printf("✅ 配对干净：%d/%d 个改动源码文件均有配对测试\n", total, total)
		return nil
	}
	fmt.Printf("⚠ %d/%d 个改动源码文件无配对测试：\n", len(missing), total)
	for _, f := range missing {
		fmt.Printf("  - %s\n", f)
	}
	fmt.Println("下一步：为上述文件写配对测试（同目录惯例 foo.go ↔ foo_test.go）；入口/生成物/纯类型文件白名单豁免。")
	return errors.New("selfcheck pairing：发现未配对源码（事实陈述供 exit 1 感知，非门禁阻断）")
}

func runSelfcheckScope(cmd *cobra.Command, args []string) error {
	root, state, err := activeStateForSelfcheck()
	if err != nil {
		return err
	}
	if len(state.PlanScope) == 0 {
		fmt.Println("ℹ 未声明 PlanScope——无可镜像的 scope 声明（forge task scope add 声明后可自检）")
		return nil
	}
	drift := taskpipeline.SelfcheckScope(root, state)
	metaList := drift
	if len(metaList) > 8 {
		metaList = metaList[:8]
	}
	recordSelfcheckEntry(root, state.TaskRef, checklog.CheckSelfcheckScope, len(drift) == 0,
		fmt.Sprintf("selfcheck scope: %d drifted / scope %s", len(drift), strings.Join(state.PlanScope, ",")),
		map[string]string{
			"drift":      fmt.Sprintf("%d", len(drift)),
			"drift_files": strings.Join(metaList, ","),
		})
	if len(drift) == 0 {
		fmt.Printf("✅ scope 干净：改动全部落在 PlanScope 声明内（%s）\n", strings.Join(state.PlanScope, ", "))
		return nil
	}
	fmt.Printf("⚠ %d 个改动源码文件超出 PlanScope 声明：\n", len(drift))
	for _, f := range drift {
		fmt.Printf("  - %s\n", f)
	}
	fmt.Println("下一步：forge task scope add <glob> 收编实改，或把改动收回声明范围。")
	return errors.New("selfcheck scope：发现 scope 漂移（事实陈述供 exit 1 感知，非门禁阻断）")
}
