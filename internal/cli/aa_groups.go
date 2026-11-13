package cli

// 命令族温和分组：命令路径全不变，仅让 `forge --help` 按职能分组展示。
//
// 为什么用 GroupID 而非归并路径：20 个顶层命令的路径已写进 README、CLAUDE.md、
// session-retrospective skill、MCP 文档与用户脚本——改路径要同步 5+ 处且破坏向后兼容。
// cobra 的 Command.Group 只改 help 展示，零迁移成本。
//
// 顺序敏感：cobra 在 AddCommand 时校验 "GroupID 已注册"（command.go:1208），未注册会 panic。
// 各命令在自身文件的 init() 里 rootCmd.AddCommand（按文件名字母序执行）；本文件名 aa_groups.go
// 排在所有命令文件之前，故本 init 最先执行——AddGroup 必先于任何 AddCommand。包级 var（各 xxxCmd）
// 在所有 init 之前初始化（Go 规范），故此处设 GroupID 时命令变量已构造完成。
//
// help/completion 是 cobra 自动生成的辅助命令，留默认（不分组，显示在末尾）。
import (
	"github.com/spf13/cobra"

	"github.com/MjxUpUp/Forge/internal/cliskills"
	"github.com/MjxUpUp/Forge/internal/clitask"
)

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: "lifecycle", Title: "项目生命周期"},
		&cobra.Group{ID: "pipeline", Title: "项目管道"},
		&cobra.Group{ID: "quality", Title: "任务质量"},
		&cobra.Group{ID: "governance", Title: "经验与治理"},
		&cobra.Group{ID: "integrate", Title: "集成与安全"},
	)

	// 项目生命周期：项目级低频管理
	initCmd.GroupID = "lifecycle"
	updateCmd.GroupID = "lifecycle"
	// Project Policy Layer P1: symmetric per-project takeover on/off (per-project
	// 接入管理的开关对).
	//
	offCmd.GroupID = "lifecycle"    // 按项目退出接管（对称命令面，见 docs/design/project-policy-layer.md）
	onCmd.GroupID = "lifecycle"     // 恢复接管（declined → managed 唯一通道）
	configCmd.GroupID = "lifecycle" // 用户级偏好（当前仅 compat 基线声明）
	policyCmd.GroupID = "lifecycle" // 接管策略快查/外来 harness 让位（P4）
	// 项目规范档案（conventions-profile 层 1 建档入口：init 扫描/show 查看；
	// 与 init 同族的项目级接入管理——先 init 进 forge，再 conventions 建档）。
	conventionsCmd.GroupID = "lifecycle"
	// One-click uninstall (npm binary + user-level assets).
	//
	// Legacy .forge runtime state → DataDir migration (upgrade path).
	//
	migrateCmd.GroupID = "lifecycle" // 旧 .forge runtime state → DataDir 迁移（升级路径）
	// Global project registry cleanup (counterpart to the explicit registration
	// done by `forge integrations enable` / `forge workspace add`; backticks guard
	// against Windows quote corruption).
	//
	registryCmd.GroupID = `lifecycle` // 全局项目注册表清理（integrations enable / workspace add 显式登记 的对应清理入口；反引号防 Windows 引号腐蚀）
	// 项目数据跨机器导出/导入/身份对齐（project-sync：与 init/migrate/registry 同族的项目级数据管理）。
	projectCmd.GroupID = `lifecycle`
	// 机器节点身份（node_id = 公钥指纹；node-identity：机器级，与 project 跨机族同族）。
	nodeCmd.GroupID = `lifecycle`
	// 多仓 workspace 清单（~/.forge/workspaces.json；项目级低频管理，与
	// registry/project 同族）。
	workspaceCmd.GroupID = `lifecycle`
	// worktree-per-task 生命周期管理（multi-task-concurrency L4：start --worktree /
	// finish / janitor；workspace 级低频管理，与 workspace/registry 同族）。
	clitask.WorktreeCmd.GroupID = `lifecycle`
	// Harness repo（multi-task-concurrency T6：git 化用户级台账；项目级低频管理，
	// 与 migrate/project 同族）。
	harnessCmd.GroupID = `lifecycle`

	// 项目管道：项目级状态（status 是主入口）
	statusCmd.GroupID = "pipeline"
	verifyCmd.GroupID = "pipeline"
	// 仓库内 change 生命周期（spec/changes 四份产物 + 可执行判据）：与 status/verify
	// 同属项目管道——它描述的是「这个仓库怎么交付」，不是任务级质量观测。
	changeCmd.GroupID = "pipeline"

	// 任务质量：任务管道 + 质量观测（trace/act/review/health 是看数据，看板会进一步聚合）
	clitask.Root.GroupID = "quality"
	traceCmd.GroupID = "quality"
	actCmd.GroupID = "quality"
	reviewCmd.GroupID = "quality"
	healthCmd.GroupID = "quality"
	dashboardCmd.GroupID = "quality"
	// 单命令引导（vNext P1）：任务质量链路的 pull 侧入口——与 task/review 同组。
	nextCmd.GroupID = "quality"
	// 文档产物可读性约束（L1 lint；doc gate 的执法在 task complete pre-flight）。
	docsCmd.GroupID = "quality"
	// Forge 自评测（双轨：端到端 profile×model × 治理层 golden/遥测/陷阱——
	// docs/design/forge-evaluation-system.md）。
	evalCmd.GroupID = "quality"
	// git 推送边界门禁（治理随 git 走——focus-batches §1c）。
	gateCmd.GroupID = "quality"
	// 可执行兼容工件（mechanism-hardening P1-1：七面快照与跨版本 diff）。
	compatCmd.GroupID = "quality"

	// dsh 插件生态工具（bridge verify 静态检查器，roadmap H2a）
	bridgeCmd.GroupID = "integrate"

	// skill 治理（experience/knowledge 经验闭环已移除）
	cliskills.Root.GroupID = "governance"
	// 执法健康报告与随机审计（vNext P2 审计层——S3* 独立通道，只读聚合）
	enforcementCmd.GroupID = "governance"

	// 集成与安全：agent 接口 + 拦截 + 内部 hook 分发 + 多 host plugin marketplace
	// 宿主接线（显式 opt-in：写用户级 settings/CLAUDE.md/skill 的唯一入口）。
	// 跨 agent 环境一致性审计（只读；多 host 接线 + 版本漂移）
	// 节点信任 store（TOFU + 团队档验签强制；node-identity §3）。
	trustCmd.GroupID = "integrate"
	// hook bash 算 DataDir 用（Hidden，不进 help 列表）
}
