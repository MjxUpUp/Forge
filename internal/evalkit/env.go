package evalkit

// env.go — drill 子进程环境构造（chore/evalkit-subproc-env-dedup，2026-09-09）。
// 裸 append(os.Environ(), "HOME="+tmp, ...) 在宿主已带同名键时产生重复键：
// Go 子进程 getenv 取首个、shell 取末个，解析分叉——宿主键可能静默遮蔽注入值。
// 键唯一性契约见 env_test.go（TestDrillEnvSingleOwnerKeys）。

import (
	"os"
	"strings"
)

// drillEnvKeyPrefixes 是 drill 注入并独占的键——构造子进程 env 前先滤除基 env
// 中的同名残留（同 artifactdrill_test.go runSH 的既有过滤模式）。
var drillEnvKeyPrefixes = []string{"HOME=", "FORGE_DATA_HOME="}

// drillEnv 返回 drill 子进程 env：os.Environ() 为基，HOME/FORGE_DATA_HOME 滤除后
// 以注入值替换——两键各恰一条，消费者无论首键/末键优先均解析到注入值。
func drillEnv(tmp, dataHome string) []string {
	base := os.Environ()
	env := make([]string, 0, len(base)+2)
	for _, e := range base {
		dup := false
		for _, p := range drillEnvKeyPrefixes {
			if strings.HasPrefix(e, p) {
				dup = true
				break
			}
		}
		if !dup {
			env = append(env, e)
		}
	}
	return append(env, "HOME="+tmp, "FORGE_DATA_HOME="+dataHome)
}
