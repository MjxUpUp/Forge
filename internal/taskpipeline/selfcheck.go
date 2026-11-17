package taskpipeline

// selfcheck.go — discipline-first-gates P2/P3：selfcheck 镜像命令的计算与查询层、
// 产物链的开工前预期探针。`forge selfcheck pairing|scope`（internal/cli/selfcheck.go）
// 对当前任务的改动集跑与 verify 门禁**同一代码路径**的纯计算——agent 能在任何时刻
// 秒级自检，落痕的 selfcheck 条目把后续失败门禁的 outcome 从 discovery（第一防线
// 缺位）升为 confirmation（已知未行动）。守纪律由此成为摩擦最小的路径。

import (
	"github.com/MjxUpUp/Forge/internal/checklog"
)

// selfcheckRanForTask 报告 task 内是否跑过 pairing 自检（任意结果——跑过即持有
// 事实）。confirmation 判定的第二输入（第一是已送达 test-nudge）：
// 自检发现 missing 而未修、或自检后代码再漂移导致 verify 失败，都是纪律债
// 兑现的如实形态。读失败按未跑处理（保守方向：不虚增 confirmation）。
func selfcheckRanForTask(root, taskRef string) bool {
	entries, err := checklog.LoadForTask(root, taskRef)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.Check == checklog.CheckSelfcheckPairing {
			return true
		}
	}
	return false
}
