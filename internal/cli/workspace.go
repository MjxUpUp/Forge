package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectroot"
	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
	"github.com/spf13/cobra"
)

// forge workspace 管理用户级多仓清单（~/.forge/workspaces.json——见 workspace
// 包）：一组共同交付的项目 key 的具名分组。成员引用 KEY（路径会漂移，key
// 不会）；存的路径仅是展示缓存，漂移由 `forge workspace doctor` 检出。
// 字符串全用反引号 raw string（Windows 输入引号腐蚀——与 registry.go 同约定）。

func init() {
	rootCmd.AddCommand(workspaceCmd)
	workspaceCmd.AddCommand(workspaceCreateCmd)
	workspaceCmd.AddCommand(workspaceAddCmd)
	workspaceCmd.AddCommand(workspaceRemoveCmd)
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceStatusCmd)
	workspaceCmd.AddCommand(workspaceDoctorCmd)

	workspaceAddCmd.Flags().String(`path`, ``, `要加入的 repo 路径（默认当前项目）`)
	workspaceRemoveCmd.Flags().String(`path`, ``, `要移除的 repo 路径（默认当前项目）`)
	// --key 是 drift 逃生口：repo 已删除/搬走的成员无法再从 --path 推导 key
	//（Key 推导需要存活的 .git），按存储 key 移除是清掉 doctor 标出的僵尸
	// 成员的唯一途径。
	workspaceRemoveCmd.Flags().String(`key`, ``, `按项目 key 移除（成员路径已失效时用；doctor 报 not-registered/path-missing 后走这里）`)
	workspaceListCmd.Flags().Bool(`json`, false, `JSON 格式输出`)
	workspaceDoctorCmd.Flags().Bool(`json`, false, `JSON 格式输出`)
}

var workspaceCmd = &cobra.Command{
	Use:   `workspace`,
	Short: `管理多 repo workspace（~/.forge/workspaces.json）`,
	Long: `forge workspace 管理多 repo workspace 清单 ~/.forge/workspaces.json。

workspace 是一组共同交付的 forge 项目的逻辑分组（如 app + 后端 + infra 仓）。
成员按项目 key 引用（路径只是展示缓存，漂移由 doctor 检出）；同一 repo 允许
属于多个 workspace（重叠由 doctor 标 advisory）。多仓 workspace 成员的任务
在 task-verify 须声明跨仓影响（forge task impact），默认 advisory，protocol
cross_repo_impact: required 可升级为阻断。

子命令：
  create  创建空 workspace
  add     把 repo（默认当前项目）加入 workspace
  remove  从 workspace 移除 repo
  list    列出全部 workspace 及成员
  status  聚合各成员仓的活跃任务
  doctor  检出 drift（未注册成员/路径漂移/一 key 多组/空 workspace/跨仓依赖环）`,
}

var workspaceCreateCmd = &cobra.Command{
	Use:   `create <name>`,
	Short: `创建空 workspace`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceCreate,
}

var workspaceAddCmd = &cobra.Command{
	Use:   `add <name>`,
	Short: `把 repo（默认当前项目）加入 workspace`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceAdd,
}

var workspaceRemoveCmd = &cobra.Command{
	Use:   `remove <name>`,
	Short: `从 workspace 移除 repo（默认当前项目）`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceRemove,
}

var workspaceListCmd = &cobra.Command{
	Use:   `list`,
	Short: `列出全部 workspace 及成员`,
	Args:  cobra.NoArgs,
	RunE:  runWorkspaceList,
}

var workspaceStatusCmd = &cobra.Command{
	Use:   `status <name>`,
	Short: `聚合 workspace 各成员仓的活跃任务`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceStatus,
}

var workspaceDoctorCmd = &cobra.Command{
	Use:   `doctor`,
	Short: `检出 workspace 清单的 drift 与跨仓依赖环（advisory，不阻断）`,
	Args:  cobra.NoArgs,
	RunE:  runWorkspaceDoctor,
}

func runWorkspaceCreate(cmd *cobra.Command, args []string) error {
	f, err := workspace.LoadForWrite()
	if err != nil {
		return err
	}
	if err := f.Create(args[0]); err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), `✅ 已创建 workspace %s——用 forge workspace add %s 加入成员 repo`, args[0], args[0])
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// resolveRepoKeyPath 把 --path flag（默认：当前项目）解析为 (key, absPath)。
// key 推导与 registry.Add 同款：git 仓走 forgedata.Key，否则回落 PathKey——
// 存入的身份必须与注册表、task-verify 门禁算出的同一个。
func resolveRepoKeyPath(cmd *cobra.Command) (key, absPath string, err error) {
	p, _ := cmd.Flags().GetString(`path`)
	if p == `` {
		if p, err = projectroot.Find(); err != nil {
			return ``, ``, err
		}
	} else {
		if p, err = filepath.Abs(p); err != nil {
			return ``, ``, err
		}
		p = filepath.Clean(p)
		if _, serr := os.Stat(p); serr != nil {
			return ``, ``, fmt.Errorf("路径 %s 不可读: %w", p, serr)
		}
	}
	key, kerr := forgedata.Key(p)
	if kerr != nil {
		key = forgedata.PathKey(p)
	}
	// 展示缓存存仓根而非可能更深的 --path：Key() 向上归仓、registry 存的也是
	// 仓根——缓存子目录会让 doctor 对刚 add 的成员立刻报 path-mismatch。
	if gitRoot := forgedata.FindGitRoot(p); gitRoot != `` {
		p = gitRoot
	}
	return key, p, nil
}

func runWorkspaceAdd(cmd *cobra.Command, args []string) error {
	key, path, err := resolveRepoKeyPath(cmd)
	if err != nil {
		return err
	}
	f, err := workspace.LoadForWrite()
	if err != nil {
		return err
	}
	if err := f.AddRepo(args[0], workspace.RepoRef{Key: key, Path: path}); err != nil {
		return err
	}
	if err := f.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), `✅ 已把 %s（key %s）加入 workspace %s`, path, key, args[0])
	fmt.Fprintln(cmd.OutOrStdout())
	// 成员提示：未注册 repo 的任务/pulse 对全局 store 不可见；init 之前 doctor
	// 会一直标该 key。仅 advisory——add 绝不能静默改注册表（副作用纪律）。
	if _, ok := registry.IsMember(path); !ok {
		fmt.Fprintf(cmd.ErrOrStderr(), "提示：%s 未在全局注册表（到该目录跑 forge init 登记），workspace doctor 会持续标出\n", path)
	}
	return nil
}

func runWorkspaceRemove(cmd *cobra.Command, args []string) error {
	key, _ := cmd.Flags().GetString(`key`)
	if key == `` {
		var err error
		key, _, err = resolveRepoKeyPath(cmd)
		if err != nil {
			return err
		}
	}
	f, err := workspace.LoadForWrite()
	if err != nil {
		return err
	}
	removed, err := f.RemoveRepo(args[0], key)
	if err != nil {
		return err
	}
	if !removed {
		return fmt.Errorf("key %s 不是 workspace %s 的成员（forge workspace list 查看成员；路径已失效的成员用 --key 移除）", key, args[0])
	}
	if err := f.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), `✅ 已从 workspace %s 移除 key %s`, args[0], key)
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

func runWorkspaceList(cmd *cobra.Command, args []string) error {
	f, err := workspace.Load()
	if err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool(`json`)
	if asJSON {
		ws := f.Workspaces
		if ws == nil {
			ws = []workspace.Workspace{}
		}
		out, _ := json.MarshalIndent(map[string]any{`workspaces`: ws}, ``, `  `)
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if len(f.Workspaces) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), `尚无 workspace——forge workspace create <name> 创建`)
		return nil
	}
	for _, w := range f.Workspaces {
		fmt.Fprintf(cmd.OutOrStdout(), "▶ %s（%d 个 repo，创建于 %s）\n", w.Name, len(w.Repos), w.CreatedAt.Format("2006-01-02"))
		for _, r := range w.Repos {
			fmt.Fprintf(cmd.OutOrStdout(), `    %s  %s`, r.Key, r.Path)
			fmt.Fprintln(cmd.OutOrStdout())
		}
	}
	return nil
}

// runWorkspaceStatus 是读侧聚合：成员任务按 KEY 扫描（RootDir(key)/tasks——
// 成员路径 add 后可能漂移，key 绝不），再过滤出活跃任务。只读：单个成员
// 坏了警告跳过，绝不让整视图空白（runTaskMineAllProjects 同款模式）。
func runWorkspaceStatus(cmd *cobra.Command, args []string) error {
	f, err := workspace.Load()
	if err != nil {
		return err
	}
	w := f.Find(args[0])
	if w == nil {
		return fmt.Errorf("workspace %q not found", args[0])
	}
	if len(w.Repos) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "workspace %s 为空（forge workspace add %s 加入成员）\n", w.Name, w.Name)
		return nil
	}
	out := cmd.OutOrStdout()
	total := 0
	for _, r := range w.Repos {
		fmt.Fprintf(out, "▶ %s  %s\n", r.Key, r.Path)
		// 拼路径前先守卫：GlobalHome 不可解析时 RootDir 返回 ""（Join("","tasks")
		// 会扫进程 cwd！），畸形 key 绝不许把扫描引出数据 home。
		if !forgedata.ValidKeyFormat(r.Key) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: 成员 key %s 格式非法（跳过）\n", r.Key)
			continue
		}
		dir := forgedata.RootDir(r.Key)
		if dir == `` {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: 解析 %s 的数据目录失败（跳过）\n", r.Key)
			continue
		}
		states, err := taskpipeline.ListTaskStatesInDir(filepath.Join(dir, `tasks`))
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: 扫描 %s 的任务失败（跳过）: %v\n", r.Key, err)
			continue
		}
		active := 0
		for _, s := range states {
			if s.CompletedAt != nil {
				continue
			}
			active++
			total++
			gate := s.NextGate()
			if gate == `` {
				gate = `gates 全过（待 complete）`
			}
			fmt.Fprintf(out, `    %-30s gate:%s  branch:%s  %s`, s.TaskRef, gate, s.Branch, s.Summary)
			fmt.Fprintln(out)
		}
		if active == 0 {
			fmt.Fprintln(out, `    （无活跃任务）`)
		}
	}
	fmt.Fprintf(out, `共 %d 个活跃任务（%d 个成员仓）`, total, len(w.Repos))
	fmt.Fprintln(out)
	return nil
}

func runWorkspaceDoctor(cmd *cobra.Command, args []string) error {
	f, err := workspace.Load()
	if err != nil {
		return err
	}
	findings := f.Doctor(registry.List())
	// 跨仓依赖环（Option B）：在此检出而非 workspace.Doctor——workspace 包不能 import
	// taskpipeline（taskpipeline 已 import workspace），故建图 + DFS 留在 CLI 层，
	// 把 dep-cycle finding 追加进 Doctor 的 drift 清单（与其余同为 advisory；--json
	// 同样携带）。
	findings = append(findings, depCycleFindings(f, collectWorkspaceTasks(f, cmd.ErrOrStderr()))...)
	asJSON, _ := cmd.Flags().GetBool(`json`)
	if asJSON {
		if findings == nil {
			findings = []workspace.Finding{}
		}
		out, _ := json.MarshalIndent(map[string]any{`findings`: findings}, ``, `  `)
		fmt.Fprintln(cmd.OutOrStdout(), string(out))
		return nil
	}
	if len(findings) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), `✅ workspace 清单健康，无 drift`)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), `发现 %d 项 drift（全部 advisory 不阻断）:`, len(findings))
	fmt.Fprintln(cmd.OutOrStdout())
	for _, fd := range findings {
		fmt.Fprintf(cmd.OutOrStdout(), `  ⚠ [%s] %s`, fd.Kind, fd.Detail)
		fmt.Fprintln(cmd.OutOrStdout())
	}
	return nil
}
