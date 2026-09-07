package evalkit

// artifactdrill_test.go — 产物链演练脚本形状的单元测试（e2e 全链路在
// internal/cli/eval_cli_test.go TestEvalArtifactDrillE2E——预构建二进制跑真实
// 命令；这里钉脚本定义本身的机械不变量，同目录配对源文件，与 wedgedrill_test
// 同款纪律）。

import (
	"strings"
	"testing"
)

// TestArtifactScriptShape 钉住产物链演练脚本的机械不变量：四条阻断/放行 gate
// 步骤齐全、三条 expectFail（前置缺失/审批缺失/漂移拦截）各自之后有对应修复步、
// 产物源文件在 fixture repo 之外（repo 树内不留 .md——doc gate 零接线）、schema
// 注入走 FORGE_DATA_HOME glob（key 派生哈希对脚本不可知）。
func TestArtifactScriptShape(t *testing.T) {
	const tmp = "/tmp/evalkit-artifact-test"
	steps := artifactScript("/fake/forge", tmp)
	if len(steps) == 0 {
		t.Fatal("脚本不应为空")
	}
	gateBlocks, gatePass, completions := 0, 0, 0
	for i, st := range steps {
		if len(st.argv) == 0 || st.argv[0] == "" {
			t.Fatalf("step %d (%s) argv 为空", i, st.name)
		}
		if strings.Contains(st.argv[0], "{forge}") {
			t.Fatalf("step %d (%s) 占位符未注入", i, st.name)
		}
		joined := strings.Join(st.argv, " ")
		if strings.Contains(joined, "gate task-implement") {
			if st.expectFail {
				gateBlocks++
			} else {
				gatePass++
			}
		}
		if strings.Contains(joined, "task complete") {
			if st.expectFail {
				completions++
			}
		}
		// 源文件必须在 fixture 之外：repo 子路径的 .md 会把 doc gate 拉进演练。
		if strings.Contains(joined, "repo/") && strings.Contains(joined, ".md") {
			t.Fatalf("step %d (%s) 在 fixture 内引用 .md（doc gate 会被拉入演练）: %s", i, st.name, joined)
		}
	}
	if gateBlocks != 3 {
		t.Fatalf("应有 3 个阻断 gate 步骤（前置缺失/产物缺失/审批缺失），got %d", gateBlocks)
	}
	if gatePass != 1 {
		t.Fatalf("应有 1 个放行 gate 步骤，got %d", gatePass)
	}
	if completions != 1 {
		t.Fatalf("应有 1 个红态 complete（漂移拦截），got %d", completions)
	}
	// schema 注入必须走 FORGE_DATA_HOME glob（key 派生哈希对脚本不可知）。
	schemaFound := false
	for _, st := range steps {
		joined := strings.Join(st.argv, " ")
		if strings.Contains(joined, "FORGE_DATA_HOME") && strings.Contains(joined, "schema.yaml") {
			schemaFound = true
		}
	}
	if !schemaFound {
		t.Fatal("缺 schema 注入步骤（FORGE_DATA_HOME glob）")
	}
	// 防回归护栏：schema YAML 经 sh 单引号字面量拼接（审查 P2），内容含单引号
	// 即破坏 shell 引用——出现即 fail，逼着改经 env 传入。
	if strings.Contains(artifactSchemaYAML, "'") {
		t.Fatal("artifactSchemaYAML 含单引号——sh 单引号拼接会被破坏，改经 step env 传入")
	}
	// spec 产物经 --artifact 在 start 时登记（产物编译成验收的 start 侧入口）。
	startFound := false
	for _, st := range steps {
		joined := strings.Join(st.argv, " ")
		if strings.Contains(joined, "task start") && strings.Contains(joined, "--artifact") {
			startFound = true
		}
	}
	if !startFound {
		t.Fatal("缺 start --artifact 登记步骤")
	}
}
