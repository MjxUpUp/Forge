package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/registry"
	"github.com/spf13/cobra"
)

// registry_audit.go — `forge registry audit`: render registry.Audit() findings.
// Read-only advisory surface for identity drift (project-sync). Lives next to
// rekey because its findings ARE rekey/adopt work items.
//
// registry_audit.go —— `forge registry audit`：渲染 registry.Audit() 发现。
// 身份漂移（project-sync）的只读 advisory 面。与 rekey 同组，因为它的发现
// 正是 rekey/adopt 的工作项。

func init() {
	registryCmd.AddCommand(registryAuditCmd)
	registryAuditCmd.Flags().Bool(`json`, false, `输出 JSON（机器可读）`)
}

var registryAuditCmd = &cobra.Command{
	Use:   `audit [--json]`,
	Short: `审计注册表与数据目录一致性（key 漂移/孤儿目录/ID 冲突/非法 ID）`,
	Long: `forge registry audit —— 只读一致性审计。

四类发现（全部 advisory，不改动任何东西）：
  key-drift       注册表 key ≠ 当前派生 key 且旧 key 数据目录有数据
                  （项目 adopt 了 ID 或项目被移动）→ forge project adopt 迁移
  orphan-datadir  projects/<key>/ 有实质载荷但注册表无对应条目
  id-collision    两个不同注册路径派生同一 key
                  （同仓库两 clone 属预期；不同项目共享 .forge-project-id
                  是复制粘贴事故 → forge project adopt --regenerate）
  invalid-id      .forge-project-id 存在但格式非法
                  （身份静默回落路径 hash；Key() fail-open 的唯一暴露面）`,
	RunE: func(cmd *cobra.Command, args []string) error {
		asJSON, _ := cmd.Flags().GetBool(`json`)
		out := cmd.OutOrStdout()
		findings := registry.Audit()
		if asJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent(``, `  `)
			return enc.Encode(findings)
		}
		if len(findings) == 0 {
			fmt.Fprintln(out, `✓ 无发现（注册表与数据目录一致）`)
			return nil
		}
		for _, f := range findings {
			loc := f.Key
			if f.Path != `` {
				loc = f.Path
			}
			fmt.Fprintf(out, `⚠ [%s] %s\n    %s\n`, f.Kind, loc, f.Detail)
		}
		return nil
	},
}
