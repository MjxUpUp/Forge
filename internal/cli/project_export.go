package cli

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/projectsync"
	"github.com/spf13/cobra"
)

// project_export.go — `forge project export`: pack the project's portable records
// into a checksummed tar.gz bundle. The file list comes from projectsync's allowlist
// (default-deny); sensitive stores are opt-in via --include.
//
// project_export.go —— `forge project export`：把项目可移植记录打包成带校验和的
// tar.gz bundle。文件清单来自 projectsync 的 allowlist（默认拒绝）；敏感 store
// 经 --include 显式选入。

func init() {
	projectExportCmd.Flags().String(`out`, ``, `输出文件（默认 ./forge-project-<项目名>-<时间戳>.tar.gz）`)
	projectExportCmd.Flags().StringSlice(`include`, nil, `额外包含敏感 store：quarantine / hazards（逗号分隔）`)
}

var projectExportCmd = &cobra.Command{
	Use:   `export [--out <file>] [--include quarantine,hazards]`,
	Short: `把项目数据打包为跨机器 bundle（默认排除敏感与机器本地文件）`,
	Long: `forge project export —— 打包项目记录为可搬运 bundle。

默认包含（allowlist，默认拒绝其余一切）：tasks/*.json、checklog*.jsonl、
toollog*.jsonl、sessions.jsonl、sessions/*.json、act/conclusions.jsonl、
stamps/*（除 hook-deploy）、protocol.yml。

默认排除：quarantine/（被隔离源码全文）、hazards/（完整命令行，可能含
token）、freeze/（机器本地绝对路径）、会话锚与 sentinel、hooks/、
.sync-version、.rekey-backup-*/、导入账本。quarantine/hazards 用 --include
显式选入。

提示：toollog 与 checklog 含本机绝对路径与编辑片段摘要（自用双机同步场景可
接受；对外分享请用 forge task export --redact）。`,
	RunE: runProjectExport,
}

func runProjectExport(cmd *cobra.Command, args []string) error {
	outPath, _ := cmd.Flags().GetString(`out`)
	include, _ := cmd.Flags().GetStringSlice(`include`)
	out := cmd.OutOrStdout()

	root, err := findProjectRoot()
	if err != nil {
		return err
	}
	dataDir := forgedata.DataDirFor(root)
	if _, serr := os.Stat(dataDir); serr != nil {
		return fmt.Errorf(`项目数据目录不存在（%s）——先运行 forge init 或产生一些项目记录`, dataDir)
	}

	origin, err := exportOrigin(root)
	if err != nil {
		return err
	}

	if outPath == `` {
		outPath = filepath.Join(`.`, fmt.Sprintf(`forge-project-%s-%s.tar.gz`,
			filepath.Base(root), time.Now().Format(`20060102-150405`)))
	}
	if !strings.HasSuffix(outPath, `.tar.gz`) && !strings.HasSuffix(outPath, `.tgz`) {
		outPath += `.tar.gz`
	}
	absOut, aerr := filepath.Abs(outPath)
	if aerr != nil {
		return aerr
	}

	// 临时文件 + rename：打包中途失败不留半个 bundle 被误导入（bundle 无签名，
	// 一个「看似完整」的截断包比没有包更危险）。
	tmp, terr := os.CreateTemp(filepath.Dir(absOut), `.forge-export-*.tmp`)
	if terr != nil {
		return fmt.Errorf(`创建临时文件失败: %w`, terr)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // rename 成功后 Remove 报错无害

	manifest, perr := projectsync.Pack(projectsync.PackInput{
		DataDir:      dataDir,
		Extra:        include,
		Origin:       origin,
		ForgeVersion: cleanVersion(cmd.Root().Version),
		Now:          time.Now(),
	}, tmp)
	closeErr := tmp.Close()
	if perr != nil {
		return fmt.Errorf(`打包失败: %w`, perr)
	}
	if closeErr != nil {
		return closeErr
	}
	if rerr := os.Rename(tmpName, absOut); rerr != nil {
		return fmt.Errorf(`落盘 %s 失败: %w`, absOut, rerr)
	}

	fmt.Fprintf(out, `✅ 已导出 %d 个文件到 %s\n`, len(manifest.Files), absOut)
	fmt.Fprintf(out, `bundle_id=%s  key=%s（key_mode=%s）\n`, manifest.BundleID, origin.Key, origin.KeyMode)
	if len(include) > 0 {
		fmt.Fprintf(out, `⚠ 已包含敏感 store：%s\n`, strings.Join(include, `,`))
	}
	fmt.Fprintf(out, `对端导入：forge project import %s\n`, absOut)
	if origin.KeyMode == `path` {
		fmt.Fprintf(out, `提示：本机仍是路径身份（key 随机器路径变化）——两台机器各跑一次 forge project adopt 后同步免 key 重映射\n`)
	}
	return nil
}

// exportOrigin builds the manifest provenance block for this machine.
//
// exportOrigin 构造本机的 manifest 溯源块。
func exportOrigin(root string) (projectsync.Origin, error) {
	key, err := forgedata.Key(root)
	if err != nil {
		return projectsync.Origin{}, err
	}
	origin := projectsync.Origin{Key: key, KeyMode: `path`}

	// ID 体系：合法 ID 文件存在即 id 模式（与 Key() 的优先级判定一致）。
	if gitDir, gerr := forgedata.ResolvedGitDir(root); gerr == nil {
		if id, ierr := forgedata.ReadProjectID(filepath.Dir(gitDir)); ierr == nil {
			origin.KeyMode = `id`
			origin.ProjectID = id
		}
	}

	origin.Root = root
	if host, herr := os.Hostname(); herr == nil {
		origin.Hostname = host
	}
	if u, uerr := user.Current(); uerr == nil {
		origin.User = u.Username
	}
	return origin, nil
}

// cleanVersion strips the ldflags decoration ("1.36.0 (commit: .., built: ..)")
// down to the bare version for the manifest.
//
// cleanVersion 把 ldflags 装饰（"1.36.0 (commit: .., built: ..)"）剥成裸版本号
// 进 manifest。
func cleanVersion(v string) string {
	if i := strings.IndexByte(v, ' '); i > 0 {
		return v[:i]
	}
	return v
}
