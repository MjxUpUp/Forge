package clitask

import (
	"fmt"
	"strings"

	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// behaviorSourceExts 是「行为类改动」的判据：改到这些扩展名就要求登记先失败证明。
// 文档、配置、脚本（.sh/.md/.yml）不算——它们没有可先失败的单元行为。
var behaviorSourceExts = []string{
	".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".py", ".rs", ".java", ".kt", ".kts",
	".c", ".h", ".cc", ".cpp", ".cxx", ".hpp", ".cs", ".rb", ".php", ".swift", ".scala", ".lua",
	".dart", ".m", ".mm", ".vue", ".svelte", ".ex", ".exs", ".erl", ".hs", ".clj", ".zig",
}

// CheckRedFirstDeclared 核对「先失败证明是否已声明」：行为类改动（changed 里含源码
// 扩展名）却一条 --accept-red 都没登记时阻断完成。
//
// CheckRedFirst 只能核对「登记了但没闭合」，管不了「根本没登记」——3.0 里
// --accept-red 是可选项，实测 20 个归档 change 只登记过 1 次，红先绿从机制退化成
// 自觉。豁免必须显式（task start --red-first-waived "<理由>"），理由进状态供评审看。
//
// CheckRedFirstDeclared gates the *declaration* of red-first evidence: a
// behavior-changing edit set with no --accept-red at all is blocked. The waiver
// is explicit and reasoned, never implicit.
func CheckRedFirstDeclared(s *tasktypes.TaskState, changed []string) error {
	if s == nil || strings.TrimSpace(s.RedFirstWaived) != "" || len(s.RedAttempts) > 0 {
		return nil
	}
	for _, f := range changed {
		for _, ext := range behaviorSourceExts {
			if strings.HasSuffix(f, ext) {
				return fmt.Errorf("先失败证明未登记：本次改动含源码（%s），但开工时没有登记任何 --accept-red 红判据——红先绿机制没有输入。恢复路径：换成能先失败的行为判据重开任务（forge task abort 后 forge task start --accept-red \"<命令>\"）；确实无可失败之物时显式豁免并写理由：forge task start --red-first-waived \"<理由>\"", f)
			}
		}
	}
	return nil
}
