package evalkit

// artifactdrill_test.go — 产物链演练脚本形状的单元测试（e2e 全链路在
// internal/cli/eval_cli_test.go TestEvalArtifactDrillE2E——预构建二进制跑真实
// 命令；这里钉脚本定义本身的机械不变量，同目录配对源文件，与 wedgedrill_test
// 同款纪律）。

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestArtifactScriptFixtureCommandsExecute 真实执行脚本里改写过\\n转义的三条
// sh 命令（fixture 红/绿、tamper），断言落盘内容——转义写法改坏时此处红，而非
// 等到 CI 深处的 drill 全链才红。
func TestArtifactScriptFixtureCommandsExecute(t *testing.T) {
	steps := artifactScript("/fake/forge", t.TempDir())
	runSH := func(t *testing.T, st wedgeStep, dir string, env map[string]string) {
		t.Helper()
		// st.argv 形如 ["sh", "-c", "<script>"]——剥掉前两个元素后整体透传。
		args := append([]string{"sh", "-c"}, st.argv[2:]...)
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		for _, e := range os.Environ() {
			// 宿主的 FORGE_DATA_HOME 会遮蔽测试注入的同名键（getenv 取首个）——滤掉。
			if strings.HasPrefix(e, "FORGE_DATA_HOME=") {
				continue
			}
			cmd.Env = append(cmd.Env, e)
		}
		cmd.Env = append(cmd.Env, st.env...)
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("sh 执行失败: %v\n%s", err, out)
		}
	}

	dir := t.TempDir()
	// fixture 步含 git add/commit——先建仓（身份由步内联 -c 提供，与真演练一致）。
	if out, err := exec.Command("git", "init", "-q", dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	var fixture, green, tamper *wedgeStep
	for i := range steps {
		switch steps[i].name {
		case "fixture":
			fixture = &steps[i]
		case "fixture(绿)":
			green = &steps[i]
		case "tamper(手改产物)":
			tamper = &steps[i]
		}
	}
	if fixture == nil || green == nil || tamper == nil {
		t.Fatal("三条 sh 步骤缺失")
	}

	// 红态 fixture：check 脚本如实失败 + 三个产物源文件就位。
	runSH(t, *fixture, dir, map[string]string{"SRC": filepath.Join(dir, "src")})
	if _, err := os.Stat(filepath.Join(dir, "src", "spec.txt")); err != nil {
		t.Fatalf("spec.txt 未落盘: %v", err)
	}
	spec, _ := os.ReadFile(filepath.Join(dir, "src", "spec.txt"))
	if !strings.Contains(string(spec), "accept: sh main_check.sh :: ALL-GOOD") {
		t.Fatalf("spec.txt 缺 accept 行: %s", spec)
	}
	// 红态断言必须在 fixture 目录跑（缺 Dir 会跑在测试进程 cwd、对缺失文件恒失败
	// ——复审指出的死断言），并断言输出正是红态文案。
	red := exec.Command("sh", "main_check.sh")
	red.Dir = dir
	redOut, redErr := red.Output()
	if redErr == nil || !strings.Contains(string(redOut), "NOT-READY") {
		t.Fatalf("红态 check 脚本必须失败且输出 NOT-READY: out=%q err=%v", redOut, redErr)
	}
	// 绿态 fixture：转义改写后 check 脚本必须输出 ALL-GOOD（\\n 在 sh printf 展开为换行）。
	runSH(t, *green, dir, nil)
	check := exec.Command("sh", "main_check.sh")
	check.Dir = dir
	out, err := check.Output()
	if err != nil || string(out) != "ALL-GOOD\n" {
		t.Fatalf("绿态 check 输出不符: %q err=%v", out, err)
	}
	// tamper：对 glob 文件追加内容（无匹配时必须非零，不静默成功）。
	home := t.TempDir()
	art := filepath.Join(home, "projects", "k", "specs", "test-artifact-drill", "design.md")
	if err := os.MkdirAll(filepath.Dir(art), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(art, []byte("# design\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runSH(t, *tamper, dir, map[string]string{"FORGE_DATA_HOME": home})
	tampered, err := os.ReadFile(art)
	if err != nil || !strings.Contains(string(tampered), "tampered") {
		t.Fatalf("tamper 应经 glob 追加到产物: %q err=%v", tampered, err)
	}
}
