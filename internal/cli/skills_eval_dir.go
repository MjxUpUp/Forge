package cli

// skills_eval_dir.go — the --dir persistent flag for the eval command family.
//
// Registered on skillsCmd (same pattern as --canonical) so every eval subcommand
// (eval-gen / eval-cases / eval-record / eval-baseline / eval-report / battery)
// inherits it without per-command wiring. Resolution priority lives in
// skillseval.ResolveDir: this flag > FORGE_EVAL_DIR > ~/.forge/evals.
//
// Primary consumer: repo-level eval assets (evals/ in VCS) and CI — a checkout
// points --dir at the repo directory instead of the user-level store, which is
// what lets `battery --gate` run on a fresh runner without being vacuous.
//
// skills_eval_dir.go — eval 命令族的 --dir persistent flag。
//
// 注册在 skillsCmd 上（与 --canonical 同模式），全部 eval 子命令
// （eval-gen / eval-cases / eval-record / eval-baseline / eval-report / battery）
// 自动继承，无需逐命令接线。解析优先级在 skillseval.ResolveDir：
// 本 flag > FORGE_EVAL_DIR > ~/.forge/evals。
//
// 主要消费方：仓库级 eval 资产（进 VCS 的 evals/）与 CI——checkout 后把 --dir
// 指向仓库目录而非用户级存储，fresh runner 上的 `battery --gate` 才不是空转。

import (
	"github.com/MjxUpUp/Forge/internal/skillseval"
)

// skEvalDirFlag is the value of the skills --dir persistent flag (empty = default resolution).
//
// skEvalDirFlag 是 skills --dir persistent flag 的值（空 = 默认解析）。
var skEvalDirFlag string

// evalDataDir resolves the eval data dir honoring the --dir flag. Single entry for the
// eval command family (read-side consumers without flag context use skillseval.EvalDir).
//
// evalDataDir 按 --dir flag 解析 eval 数据目录。eval 命令族唯一入口
// （无 flag 上下文的读侧消费方用 skillseval.EvalDir）。
func evalDataDir() (string, error) {
	return skillseval.ResolveDir(skEvalDirFlag)
}

func init() {
	skillsCmd.PersistentFlags().StringVar(&skEvalDirFlag, "dir", "",
		"eval 数据目录（默认 ~/.forge/evals；FORGE_EVAL_DIR 可覆盖。仓库内 evals/ 或 CI 用）")
}
