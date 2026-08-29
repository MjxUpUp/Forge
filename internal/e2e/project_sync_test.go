package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// project_sync_test.go —— 双机往返 e2e（project-sync）：同一仓库的两个 clone 在
// 不同路径、两个隔离 FORGE_DATA_HOME、完整二进制驱动 adopt → export → import
// 双向同步，断言收敛（同 key、任务可见、决策同步、事件不重复）。
//
// 「机器」切换即 t.Setenv(FORGE_DATA_HOME, ...)——forge() helper 把进程 env 继承
// 进子进程。

// copyTree 复制目录树（普通文件 + 目录；跳过 symlink）——「把仓库 clone 到另一台
// 机器路径」的替身，跨平台。
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		if !d.Type().IsRegular() {
			return nil // symlink 等跳过（git 对象链接/平台差异）
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0644)
	})
	if err != nil {
		t.Fatalf(`复制 %s → %s 失败: %v`, src, dst, err)
	}
}

// dataDirKey 从 `forge data-dir` 输出提取 key（basename）。
func dataDirKey(t *testing.T, dir string) string {
	t.Helper()
	out := forge(t, dir, `data-dir`)
	out = strings.TrimSpace(out)
	// 输出可能多行（打印路径 + 提示），取第一行非空路径。绝对路径判定用
	// filepath.IsAbs 而非前缀分隔符——Windows 路径以盘符（C:\）开头，
	// 不以 filepath.Separator 开头。
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if filepath.IsAbs(line) || strings.HasSuffix(line, "projects") {
			return filepath.Base(strings.TrimRight(line, string(filepath.Separator)))
		}
	}
	t.Fatalf(`无法从 data-dir 输出解析 key: %q`, out)
	return ``
}

// TestProjectSyncDualMachineRoundTrip：标准双机流程。
//
// 机器 A：真 git repo → forge init → 启动任务 + 跑一次门禁（checklog 证据）→
// adopt（ID 诞生，数据迁到 ID key）。
// 机器 B：目录树复制到不同路径（「clone」），独立空 FORGE_DATA_HOME → init →
// adopt（复制来的同 ID → 同 key）。
// A export、B import（同 key ⇒ 受信）：任务与证据在 B 可见。
// B 记一条决策、反向 export、A import：决策在 A 收敛。
// 同 bundle 在 B 重导入被账本跳过。
func TestProjectSyncDualMachineRoundTrip(t *testing.T) {
	homeA := t.TempDir()
	homeB := t.TempDir()
	repoA := t.TempDir()
	repoB := t.TempDir()

	// ---- 机器 A：init repo + 任务 + 证据 + adopt ----
	gitInit(t, repoA)
	t.Setenv(`FORGE_DATA_HOME`, homeA)
	forge(t, repoA, `init`)
	forge(t, repoA, `task`, `start`, `--ref`, `feat/e2e-sync`, `--title`, `e2e 双机同步`)
	// 门禁跑一次即留 checklog 证据（BLOCKED/FAIL 也记 checklog —— trace 的原料）
	if _, err := forgeErr(t, repoA, `task`, `gate`, `task-implement`, `--ref`, `feat/e2e-sync`); err == nil {
		t.Fatal(`未实现任何代码的 task-implement 门禁不应通过（e2e 前置假设失效）`)
	}
	forge(t, repoA, `project`, `adopt`)

	idPath := filepath.Join(repoA, `.forge-project-id`)
	if _, err := os.ReadFile(idPath); err != nil {
		t.Fatalf(`adopt 应在主根写 ID 文件: %v`, err)
	}
	keyA := dataDirKey(t, repoA)
	if keyA == `` {
		t.Fatal(`A 侧 key 为空`)
	}

	// ---- 机器 B：另一路径「clone」+ 对齐 ----
	copyTree(t, repoA, repoB)
	t.Setenv(`FORGE_DATA_HOME`, homeB)
	forge(t, repoB, `init`)
	// B 已带 ID 文件（树复制），adopt 幂等打印现状——身份即刻对齐
	out := forge(t, repoB, `project`, `adopt`)
	if !strings.Contains(out, `已启用项目 ID`) {
		t.Fatalf(`B 侧 adopt 应识别复制来的 ID: %s`, out)
	}
	keyB := dataDirKey(t, repoB)
	if keyA != keyB {
		t.Fatalf(`双机 key 应相等（ID 身份）: A=%s B=%s`, keyA, keyB)
	}

	// ---- A export → B import（同 key ⇒ 受信）----
	t.Setenv(`FORGE_DATA_HOME`, homeA)
	bundle := filepath.Join(t.TempDir(), `sync.tar.gz`)
	exportOut := forge(t, repoA, `project`, `export`, `--out`, bundle)
	if !strings.Contains(exportOut, `已导出`) {
		t.Fatalf(`export 输出异常: %s`, exportOut)
	}

	t.Setenv(`FORGE_DATA_HOME`, homeB)
	importOut := forge(t, repoB, `project`, `import`, bundle)
	if !strings.Contains(importOut, `受信`) {
		t.Fatalf(`同 key import 应判定受信: %s`, importOut)
	}
	listOut := forge(t, repoB, `task`, `list`)
	if !strings.Contains(listOut, `feat/e2e-sync`) {
		t.Fatalf(`B 侧 task list 应见任务: %s`, listOut)
	}
	// trace 时间线：B 侧能重建 A 的 checklog 证据（B 自己从未跑过任何 hook——
	// 事件只能来自 bundle；auto-compile 是 A 侧 hook 在门禁运行期记录的检查名）
	traceOut := forge(t, repoB, `trace`, `feat/e2e-sync`)
	if !strings.Contains(traceOut, `auto-compile`) || strings.Contains(traceOut, `0 events`) {
		t.Fatalf(`B 侧 trace 应含 A 的 checklog 证据: %s`, traceOut)
	}

	// ---- B 反向：记决策 → export → A import 收敛 ----
	forge(t, repoB, `task`, `decide`, `--ref`, `feat/e2e-sync`, `--content`, `dual-machine decision from B`, `--by`, `claude-code`)
	bundle2 := filepath.Join(t.TempDir(), `sync-back.tar.gz`)
	forge(t, repoB, `project`, `export`, `--out`, bundle2)

	t.Setenv(`FORGE_DATA_HOME`, homeA)
	forge(t, repoA, `project`, `import`, bundle2)
	resumeOut := forge(t, repoA, `task`, `resume`, `--ref`, `feat/e2e-sync`, `--json`)
	if !strings.Contains(resumeOut, `dual-machine decision from B`) {
		t.Fatalf(`A 侧应收敛 B 的决策: %s`, resumeOut)
	}

	// ---- 幂等：同 bundle 重复导入被账本跳过 ----
	t.Setenv(`FORGE_DATA_HOME`, homeB)
	reimport := forge(t, repoB, `project`, `import`, bundle)
	if !strings.Contains(reimport, `已导入过`) {
		t.Fatalf(`重复导入应被账本跳过: %s`, reimport)
	}
}

// gitInit 建一个真 git repo 并打一个空 commit（forge task start 需要可解析的
// HEAD/分支）。
func gitInit(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command(`git`, args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf(`git %s: %v`+"\n"+`%s`, strings.Join(args, ` `), err, out)
		}
	}
	run(`init`, `-q`)
	run(`-c`, `user.email=e2e@forge.test`, `-c`, `user.name=e2e`, `commit`, `--allow-empty`, `-q`, `-m`, `init`)
}
