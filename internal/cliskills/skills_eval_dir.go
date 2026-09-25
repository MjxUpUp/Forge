package cliskills

// skills_eval_dir.go — eval 命令族的 --dir persistent flag。
//
// 注册在 Root 上（与 --canonical 同模式），全部 eval 子命令
// （eval-gen / eval-cases / eval-record / eval-baseline / eval-report / battery）
// 自动继承，无需逐命令接线。解析优先级在 skillseval.ResolveDir：
// 本 flag > FORGE_EVAL_DIR > ~/.forge/evals。
//
// 主要消费方：仓库级 eval 资产（进 VCS 的 evals/）与 CI——checkout 后把 --dir
// 指向仓库目录而非用户级存储，fresh runner 上的 `battery --gate` 才不是空转。

import (
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// skEvalDirFlag 是 skills --dir persistent flag 的值（空 = 默认解析）。
var skEvalDirFlag string

// evalDataDir 按 --dir flag 解析 eval 数据目录。eval 命令族唯一入口
// （无 flag 上下文的读侧消费方用 skillseval.EvalDir）。
func evalDataDir() (string, error) {
	return skillseval.ResolveDir(skEvalDirFlag)
}

func init() {
	Root.PersistentFlags().StringVar(&skEvalDirFlag, "dir", "",
		"eval 数据目录（默认 ~/.forge/evals；FORGE_EVAL_DIR 可覆盖。仓库内 evals/ 或 CI 用）")
}
