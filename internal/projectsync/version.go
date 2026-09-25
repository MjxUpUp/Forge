package projectsync

// version.go — bundle 版本偏移感知（mechanism-hardening P0-1，对侧兼容）。
// Manifest.ForgeVersion 导出侧早已填充；缺的是导入侧感知——旧二进制反序列化
// 新 bundle 时 Go json 静默丢弃未知字段，随后 re-export 会无声裁剪这些字段
//（前向兼容只在字节透传时成立，而 import→merge→re-export 走类型化结构）。
// 语义取 K8s 版本偏移窗口：窗口外**警告不硬拒**——账本幂等、重复 pull 免费的
// 同步体验不受影响，但把"本机正在裁剪较新字段"从无声变有声。

import (
	"fmt"

	"github.com/MjxUpUp/Forge/internal/util"
)

// VersionSkew 返回导入侧版本偏移警示（空 = 无需警示）。
// local = 本机 forge 版本（rootCmd.Version 清洗后）；bundle = manifest.ForgeVersion。
// 仅前向偏移（bundle 比 local 新）警示：本机是旧节点、是裁剪风险方；后向偏移
// （旧 bundle 导入新 forge）由 legacy 字段兜底与惰性重推导覆盖，无须拦。
func VersionSkew(local, bundle string) string {
	if bundle == "" || local == "" {
		return "" // 早期 bundle 未携带版本——无从比较，静默（fail-open，观察类语义）
	}
	if util.CompareVersions(bundle, local) > 0 {
		return fmt.Sprintf("bundle 由较新 forge 导出（bundle=%s > 本机=%s）——本机 re-export 会静默裁剪较新字段（旧版本反序列化丢弃未知键）；建议先升级本机 forge 再做转发放", bundle, local)
	}
	return ""
}
