package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/registry"
)

// ScenarioResult holds the result of a single E2E scenario run.
//
// ScenarioResult 持有单次 E2E scenario 运行的结果。
type ScenarioResult struct {
	Name     string
	Passed   bool
	Output   string
	Duration time.Duration
}

// ---------- scenario 实现 ----------
//
// 所有场景断言 user-level-assets 重构之后的现实：`forge init` 默认零项目写入
// （不写项目级 .forge/ 标记），hooks 以参考副本形式落在用户级 DataDir/hooks/
// （运行时执行嵌入内容），settings/SKILL.md/CLAUDE.md 在用户级（~/.claude/...），
// autoSync 剥除遗留项目级 forge 资产（.forge/hooks、settings.local hooks、
// .claude/skills/forge-quality）而非刷新它们。重构前的场景断言旧的项目级布局，
// 在 2026-08-18 Nightly 上转红——这里的重写钉死新契约而非残留。
//
// 每个场景用隔离的 HOME + FORGE_DATA_HOME 跑 forge（scenarioEnv）：用户级写入
// 绝不碰真实用户配置，且用户级资产路径变得可断言。

// scenarioEnv 携带一个场景的隔离 HOME / FORGE_DATA_HOME 及给 forge 子进程用的
// 预建环境切片。
type scenarioEnv struct {
	home     string   // 隔离 HOME（预建 .claude，用户级 claude 资产可生成、可断言）
	dataHome string   // 隔离 FORGE_DATA_HOME（registry + DataDir）
	env      []string // os.Environ() + HOME + FORGE_DATA_HOME
}

// newScenarioEnv 建隔离环境。预建 ~/.claude：用户级 quality SKILL.md 生成器在
// agent 配置 home 不存在时 no-op（探测自毒防护），该目录必须先存在，资产才可断言。
func newScenarioEnv() (*scenarioEnv, error) {
	home, err := os.MkdirTemp("", "forge-verify-home-*")
	if err != nil {
		return nil, err
	}
	dataHome, err := os.MkdirTemp("", "forge-verify-data-*")
	if err != nil {
		os.RemoveAll(home)
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		os.RemoveAll(home)
		os.RemoveAll(dataHome)
		return nil, err
	}
	return &scenarioEnv{
		home:     home,
		dataHome: dataHome,
		env: append(os.Environ(),
			"HOME="+home,
			"FORGE_DATA_HOME="+dataHome,
		),
	}, nil
}

// cleanup 删除隔离目录。
func (e *scenarioEnv) cleanup() {
	os.RemoveAll(e.home)
	os.RemoveAll(e.dataHome)
}

// dataDir 经 `forge data-dir` 解析项目 DataDir（只取 stdout——autoSync 告警走
// stderr，不得污染解析出的路径）。
func (e *scenarioEnv) dataDir(forgeBin, dir string) (string, error) {
	cmd := exec.Command(forgeBin, "data-dir")
	cmd.Dir = dir
	cmd.Env = e.env
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// runScenarioFreshInstall 验证零项目写入默认下从空目录的干净 init：无项目级
// .forge/，registry + DataDir 在用户级，hook 参考副本在 DataDir/hooks/，用户级
// claude settings + forge-quality skill 由 autoSync 生成。
func runScenarioFreshInstall(forgeBin string) ScenarioResult {
	start := time.Now()
	var outputLines []string

	senv, err := newScenarioEnv()
	if err != nil {
		return failResult("fresh-install", err.Error(), start)
	}
	defer senv.cleanup()

	// 创建 temp dir 含 git + go 项目
	dir, err := os.MkdirTemp("", "forge-verify-fresh-*")
	if err != nil {
		return failResult("fresh-install", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	if output, err := verifyRunGit(dir, "init"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("git init failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.email", "test@example.com"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("git config user.email failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.name", "Test"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("git config user.name failed: %v\n%s", err, output), start)
	}

	// 写最小 go 项目
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// 跑 forge init
	if output, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "init"); err != nil {
		return failResult("fresh-install", fmt.Sprintf("forge init failed: %v\n%s", err, output), start)
	}

	// 零项目写入：init 不得创建项目级 .forge/。
	if verifyFileExists(dir, ".forge") {
		outputLines = append(outputLines, ".forge should NOT exist (zero-project-write default)")
	}

	// 隔离用户级的注册表登记了该项目。断言走结构化：json.Unmarshal 进
	// registry.File 后逐条路径等值比较（filepath.Clean + Windows 下 EqualFold）。
	// 旧的原始子串匹配 `"dir"` 在 Windows 上恒红——JSON 会转义反斜杠，字面
	// needle C:\...\dir 在编码后的字节里永不出现。存储路径还可能是 symlink
	// 解析后的物理形态（macOS /var → /private/var：登记路径经过 root 解析），
	// 故字面与解析形态任一命中即算。
	regData, err := os.ReadFile(filepath.Join(senv.dataHome, "projects.json"))
	if err != nil {
		outputLines = append(outputLines, fmt.Sprintf("projects.json missing: %v", err))
	} else if !scenarioRegistryHasPath(regData, dir) {
		outputLines = append(outputLines, fmt.Sprintf("projects.json should contain %s (entries: %s)", dir, regData))
	}

	// 跑 forge status（触发 autoSync：DataDir hook 副本 + 用户级资产）。
	if output, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "status"); err != nil {
		outputLines = append(outputLines, fmt.Sprintf("forge status failed: %v\n%s", err, output))
	} else if !strings.Contains(output, "Project:") {
		outputLines = append(outputLines, "forge status output missing 'Project:'")
	}

	// DataDir hook 参考副本存在（运行时执行嵌入内容；副本是检视面）。
	dataDir, err := senv.dataDir(forgeBin, dir)
	if err != nil {
		outputLines = append(outputLines, fmt.Sprintf("forge data-dir failed: %v", err))
	} else {
		for _, hook := range []string{"auto-compile.sh", "assertion-check.sh", "task-verify.sh"} {
			if !verifyFileExists(dataDir, filepath.Join("hooks", hook)) {
				outputLines = append(outputLines, fmt.Sprintf("missing DataDir hook copy: %s", hook))
			}
		}
	}

	// 用户级 claude 资产（隔离 HOME）。
	for _, f := range []string{
		filepath.Join(".claude", "settings.json"),
		filepath.Join(".claude", "skills", "forge-quality", "SKILL.md"),
	} {
		if !verifyFileExists(senv.home, f) {
			outputLines = append(outputLines, fmt.Sprintf("missing user-level asset: %s", f))
		}
	}

	if len(outputLines) > 0 {
		return failResult("fresh-install", strings.Join(outputLines, "\n"), start)
	}
	return ScenarioResult{Name: "fresh-install", Passed: true, Duration: time.Since(start)}
}

// runScenarioMasterReminder 验证 task-verify hook 在 master 上改码时告警。
func runScenarioMasterReminder(forgeBin string) ScenarioResult {
	start := time.Now()

	senv, err := newScenarioEnv()
	if err != nil {
		return failResult("master-reminder", err.Error(), start)
	}
	defer senv.cleanup()

	dir, err := os.MkdirTemp("", "forge-verify-master-*")
	if err != nil {
		return failResult("master-reminder", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// 准备：git init + go 项目 + forge init
	if output, err := verifyRunGit(dir, "init"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git init failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.email", "test@example.com"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git config user.email failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.name", "Test"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git config user.name failed: %v\n%s", err, output), start)
	}
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")
	if _, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "init"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("forge init failed: %v", err), start)
	}

	// 全部 commit，然后创建 feature branch
	if output, err := verifyRunGit(dir, "add", "."); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git add failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "-c", "commit.gpgsign=false", "commit", "-m", "initial"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git commit failed: %v\n%s", err, output), start)
	}

	// 探测实际默认分支（master/main 取决于环境的 init.defaultBranch）——
	// 硬编码 checkout master 在 main 默认的环境静默失败，场景会在错误分支上"通过"。
	branchOut, err := verifyRunGit(dir, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return failResult("master-reminder", fmt.Sprintf("git symbolic-ref HEAD failed: %v\n%s", err, branchOut), start)
	}
	defaultBranch := strings.TrimSpace(branchOut)
	if defaultBranch == "" {
		return failResult("master-reminder", "git symbolic-ref HEAD returned empty branch name", start)
	}

	if output, err := verifyRunGit(dir, "checkout", "-b", "feature/EXP-1-test"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git checkout -b failed: %v\n%s", err, output), start)
	}

	// 启动 task、过门禁、complete
	if _, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "task", "start", "--ref", "EXP-1", "--title", "test experience"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("task start failed: %v", err), start)
	}
	if err := passAllVerifyGates(forgeBin, dir, senv.env, "EXP-1"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("pass gates failed: %v", err), start)
	}
	if _, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "task", "complete", "--ref", "EXP-1"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("task complete failed: %v", err), start)
	}

	// 切回默认分支
	if output, err := verifyRunGit(dir, "checkout", defaultBranch); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git checkout %s failed: %v\n%s", defaultBranch, err, output), start)
	}

	// 创建源码文件、commit，再修改
	writeVerifyFile(dir, "foo.go", "package main\n\nfunc Foo() int { return 42 }\n")
	if output, err := verifyRunGit(dir, "add", "foo.go"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git add foo.go failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "-c", "commit.gpgsign=false", "commit", "-m", "add foo.go"); err != nil {
		return failResult("master-reminder", fmt.Sprintf("git commit foo.go failed: %v\n%s", err, output), start)
	}
	writeVerifyFile(dir, "foo.go", "package main\n\nfunc Foo() int { return 99 }\n")

	// 从 DataDir 参考副本跑 task-verify hook（user-level-assets 后项目级
	// .forge/hooks/ 不再存在；运行时执行的嵌入内容与副本同源）。
	dataDir, err := senv.dataDir(forgeBin, dir)
	if err != nil {
		return failResult("master-reminder", fmt.Sprintf("forge data-dir failed: %v", err), start)
	}
	hookPath := filepath.Join(dataDir, "hooks", "task-verify.sh")
	if !verifyFileExists(dataDir, filepath.Join("hooks", "task-verify.sh")) {
		return failResult("master-reminder", fmt.Sprintf("missing DataDir hook copy: %s", hookPath), start)
	}
	binDir := filepath.Dir(forgeBin)
	cmd := exec.Command("bash", hookPath)
	cmd.Dir = dir
	cmd.Env = append(senv.env, "PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	output, _ := cmd.CombinedOutput()
	outStr := string(output)

	if !strings.Contains(outStr, "without active task") {
		return failResult("master-reminder", fmt.Sprintf("hook should warn 'without active task', got: %q", outStr), start)
	}

	return ScenarioResult{Name: "master-reminder", Passed: true, Duration: time.Since(start)}
}

// runScenarioUpgradeV040 验证从 v0.4.0 类状态的升级：遗留项目级 .forge/
// （pipeline.yml/state.json/自定义 protocol/旧 hook）被 autoSync 收敛——死文件与
// 项目级 hook 副本剥除、用户自定义 protocol.yml 保留、DataDir hook 副本与用户级
// skill 生成。
func runScenarioUpgradeV040(forgeBin string) ScenarioResult {
	start := time.Now()

	senv, err := newScenarioEnv()
	if err != nil {
		return failResult("upgrade-v040", err.Error(), start)
	}
	defer senv.cleanup()

	dir, err := os.MkdirTemp("", "forge-verify-v040-*")
	if err != nil {
		return failResult("upgrade-v040", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// 准备
	if output, err := verifyRunGit(dir, "init"); err != nil {
		return failResult("upgrade-v040", fmt.Sprintf("git init failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.email", "test@example.com"); err != nil {
		return failResult("upgrade-v040", fmt.Sprintf("git config user.email failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.name", "Test"); err != nil {
		return failResult("upgrade-v040", fmt.Sprintf("git config user.name failed: %v\n%s", err, output), start)
	}
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// 创建 v0.4.0 风格的 .forge/ 结构
	for _, d := range []string{".forge/hooks", ".forge/tasks", ".forge/gates"} {
		os.MkdirAll(filepath.Join(dir, d), 0755)
	}

	writeVerifyFile(dir, ".forge/pipeline.yml", `version: "2.0"
project: "old-project"
mode: medium

pipeline:
  gates:
    - id: gate-4-implement
      name: "Code Implementation"
      enabled: true
      depends_on: []
      hooks:
        - auto-compile.sh
        - assertion-check.sh
`)

	writeVerifyFile(dir, ".forge/state.json", `{
  "pipeline_version": "2.0",
  "mode": "medium",
  "current_gate": "",
  "started_at": "2025-01-01T00:00:00Z",
  "history": [],
  "overrides": [],
  "last_sync_version": "v0.4.0"
}`)

	// 用户自定义的 protocol.yml 含 scoring 配置
	writeVerifyFile(dir, ".forge/protocol.yml", `version: "1.0"
standards:
  - id: my-custom-standard
    name: "My Custom Standard"
    description: "A custom standard from v0.4.0"
    severity: error
    enabled: true
session_rules:
  - id: my-custom-rule
    trigger: always
    instruction: "Always follow my custom rule"
    mandatory: true
scoring:
  weights:
    process: 0.3
    testing: 0.2
    code-quality: 0.2
    assertions: 0.1
    scope: 0.1
    efficiency: 0.1
  thresholds:
    A: 90
    B: 80
    C: 70
    D: 60
    F: 0
`)

	// 旧 hook
	writeVerifyFile(dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old-auto-compile\n")
	writeVerifyFile(dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old-assertion-check\n")

	// 跑 forge status 触发 auto-sync
	if output, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "status"); err != nil {
		return failResult("upgrade-v040", fmt.Sprintf("forge status failed: %v\n%s", err, output), start)
	}

	var failures []string

	// 遗留残留被剥除：死管道文件 + 项目级 hook 副本（参考副本现在在
	// DataDir/hooks/，运行时执行嵌入内容）。
	for _, gone := range []string{
		".forge/pipeline.yml",
		".forge/state.json",
		".forge/hooks",
	} {
		if verifyFileExists(dir, gone) {
			failures = append(failures, fmt.Sprintf("legacy residue should be stripped: %s", gone))
		}
	}

	// 校验 protocol.yml 未被覆盖
	protoContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "protocol.yml"))
	if !strings.Contains(string(protoContent), "my-custom-standard") {
		failures = append(failures, "protocol.yml should still contain user's custom standard")
	}

	// DataDir hook 参考副本存在。
	dataDir, err := senv.dataDir(forgeBin, dir)
	if err != nil {
		failures = append(failures, fmt.Sprintf("forge data-dir failed: %v", err))
	} else {
		for _, hook := range []string{"auto-compile.sh", "assertion-check.sh", "task-verify.sh"} {
			if !verifyFileExists(dataDir, filepath.Join("hooks", hook)) {
				failures = append(failures, fmt.Sprintf("missing DataDir hook copy: %s", hook))
			}
		}
	}

	// 用户级 quality SKILL.md 已生成（隔离 HOME）。
	if !verifyFileExists(senv.home, filepath.Join(".claude", "skills", "forge-quality", "SKILL.md")) {
		failures = append(failures, "user-level forge-quality SKILL.md should be generated")
	}

	if len(failures) > 0 {
		return failResult("upgrade-v040", strings.Join(failures, "\n"), start)
	}
	return ScenarioResult{Name: "upgrade-v040", Passed: true, Duration: time.Since(start)}
}

// runScenarioUpgradeV030 验证从 v0.3.0 类状态升级时 protocol 保留。
func runScenarioUpgradeV030(forgeBin string) ScenarioResult {
	start := time.Now()

	senv, err := newScenarioEnv()
	if err != nil {
		return failResult("upgrade-v030", err.Error(), start)
	}
	defer senv.cleanup()

	dir, err := os.MkdirTemp("", "forge-verify-v030-*")
	if err != nil {
		return failResult("upgrade-v030", err.Error(), start)
	}
	defer os.RemoveAll(dir)

	// 准备
	if output, err := verifyRunGit(dir, "init"); err != nil {
		return failResult("upgrade-v030", fmt.Sprintf("git init failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.email", "test@example.com"); err != nil {
		return failResult("upgrade-v030", fmt.Sprintf("git config user.email failed: %v\n%s", err, output), start)
	}
	if output, err := verifyRunGit(dir, "config", "user.name", "Test"); err != nil {
		return failResult("upgrade-v030", fmt.Sprintf("git config user.name failed: %v\n%s", err, output), start)
	}
	writeVerifyFile(dir, "go.mod", "module example.com/test\n\ngo 1.24\n")
	writeVerifyFile(dir, "main.go", "package main\n\nfunc main() {}\n")

	// 创建 v0.3.0 类状态
	for _, d := range []string{".forge/hooks", ".forge/tasks", ".forge/gates"} {
		os.MkdirAll(filepath.Join(dir, d), 0755)
	}

	writeVerifyFile(dir, ".forge/pipeline.yml", `version: "2.0"
project: "old-project"
mode: medium

pipeline:
  gates:
    - id: gate-4-implement
      name: "Code Implementation"
      enabled: true
      depends_on: []
`)

	writeVerifyFile(dir, ".forge/state.json", `{
  "pipeline_version": "2.0",
  "mode": "medium",
  "current_gate": "",
  "started_at": "2025-01-01T00:00:00Z",
  "history": [],
  "overrides": [],
  "last_sync_version": "v0.3.0"
}`)

	// 用户 protocol 含自定义 standards
	writeVerifyFile(dir, ".forge/protocol.yml", `version: "1.0"
standards:
  - id: no-console-log
    name: "No console.log"
    description: "Production code must not contain console.log statements"
    severity: error
    enabled: true
  - id: require-error-handling
    name: "Error handling required"
    description: "All public functions must handle errors"
    severity: warning
    enabled: true
session_rules:
  - id: review-before-merge
    trigger: always
    instruction: "Always review code before merge"
    mandatory: true
scoring:
  weights:
    process: 0.25
    testing: 0.25
    code-quality: 0.20
    assertions: 0.15
    scope: 0.10
    efficiency: 0.05
  thresholds:
    A: 90
    B: 80
    C: 70
    D: 60
    F: 0
`)

	// 旧 hook
	writeVerifyFile(dir, ".forge/hooks/auto-compile.sh", "#!/bin/bash\necho old\n")
	writeVerifyFile(dir, ".forge/hooks/assertion-check.sh", "#!/bin/bash\necho old\n")

	// 跑 forge status 触发 auto-sync
	if output, err := verifyRunForgeEnv(forgeBin, dir, senv.env, "status"); err != nil {
		return failResult("upgrade-v030", fmt.Sprintf("forge status failed: %v\n%s", err, output), start)
	}

	var failures []string

	// 校验 protocol.yml 仍含用户自定义 standards
	protoContent, _ := os.ReadFile(filepath.Join(dir, ".forge", "protocol.yml"))
	for _, needle := range []string{"no-console-log", "require-error-handling", "review-before-merge"} {
		if !strings.Contains(string(protoContent), needle) {
			failures = append(failures, fmt.Sprintf("protocol.yml should still contain '%s'", needle))
		}
	}

	// 遗留项目级 hook 副本与死管道文件被剥除（参考副本现在在 DataDir/hooks/）。
	for _, gone := range []string{".forge/hooks", ".forge/pipeline.yml", ".forge/state.json"} {
		if verifyFileExists(dir, gone) {
			failures = append(failures, fmt.Sprintf("legacy residue should be stripped: %s", gone))
		}
	}

	// DataDir hook 参考副本存在（与 v040 同一 autoSync 路径）。
	dataDir, err := senv.dataDir(forgeBin, dir)
	if err != nil {
		failures = append(failures, fmt.Sprintf("forge data-dir failed: %v", err))
	} else {
		for _, hook := range []string{"auto-compile.sh", "task-verify.sh"} {
			if !verifyFileExists(dataDir, filepath.Join("hooks", hook)) {
				failures = append(failures, fmt.Sprintf("missing DataDir hook copy: %s", hook))
			}
		}
	}

	// 用户级 claude settings + quality SKILL.md 存在（隔离 HOME）。
	if !verifyFileExists(senv.home, filepath.Join(".claude", "settings.json")) {
		failures = append(failures, "user-level settings.json should exist after auto-sync")
	}
	if !verifyFileExists(senv.home, filepath.Join(".claude", "skills", "forge-quality", "SKILL.md")) {
		failures = append(failures, "user-level forge-quality SKILL.md should exist after auto-sync")
	}

	if len(failures) > 0 {
		return failResult("upgrade-v030", strings.Join(failures, "\n"), start)
	}
	return ScenarioResult{Name: "upgrade-v030", Passed: true, Duration: time.Since(start)}
}

// ---------- 辅助函数 ----------

// scenarioRegistryHasPath 判断 projects.json 字节（forge 子进程所写）是否登记了
// want——或其 symlink 解析后的物理形态。结构化比较，取代旧的原始子串匹配
// （后者在 Windows 恒红：JSON 转义反斜杠，字面 needle 在编码字节里永不出现）：
// unmarshal 进 registry.File（条目列表与遗留字符串列表两种形态都接受）后按值
// 比较路径——filepath.Clean 消词法形态差异，Windows 大小写不敏感比较
// （EqualFold，对齐 registry.pathKey 语义）。
func scenarioRegistryHasPath(regData []byte, want string) bool {
	var f registry.File
	if err := json.Unmarshal(regData, &f); err != nil {
		return false
	}
	candidates := []string{want}
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		candidates = append(candidates, resolved)
	}
	samePath := func(a, b string) bool {
		a, b = filepath.Clean(a), filepath.Clean(b)
		if runtime.GOOS == "windows" {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	for _, e := range f.Projects {
		for _, c := range candidates {
			if samePath(e.Path, c) {
				return true
			}
		}
	}
	return false
}

func failResult(name, output string, start time.Time) ScenarioResult {
	return ScenarioResult{Name: name, Passed: false, Output: output, Duration: time.Since(start)}
}

// verifyRunForgeEnv 以显式环境执行 forge 命令（场景隔离：HOME +
// FORGE_DATA_HOME），返回合并输出与错误。
func verifyRunForgeEnv(forgeBin, dir string, env []string, args ...string) (string, error) {
	cmd := exec.Command(forgeBin, args...)
	cmd.Dir = dir
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// verifyRunGit 在指定目录执行 git 命令。
func verifyRunGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// writeVerifyFile 在 dir 内写入内容到文件。
func writeVerifyFile(dir, name, content string) {
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}

// verifyFileExists 检查文件或目录是否存在。
func verifyFileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// passAllVerifyGates 为给定 task ref 通过全部 3 个 task gate（v0.17：从 5 个精简）。
func passAllVerifyGates(forgeBin, dir string, env []string, ref string) error {
	// 经子进程 env 关闭 work-activity 门禁（不用 os.Setenv：senv.env 在场景开始
	// 就从 os.Environ() 快照，之后的进程级 Setenv 永远到不了显式 cmd.Env 的
	// forge 子进程——code-review 发现）。FORGE_GATE_MIN_INTERVAL 生产代码无人
	// 读取（只有测试 set），刻意不再设置。
	env = append(append([]string{}, env...), "FORGE_WORK_ACTIVITY=disable")

	// commit 一个真实文件变更让 task-implement 的代码变更检查通过：它按文件名
	// diff HeadCommit..HEAD，--allow-empty 空 commit（零文件 diff）会判
	// 「no code changes detected」——2026-08-18 Nightly 的红。变更必须在跑
	// 门禁前落在 task 的 feature 分支上。
	writeVerifyFile(dir, "verify_change.go", "package main\n\n// VerifyChange exists so task-implement sees a real code change.\nfunc VerifyChange() int { return 1 }\n")
	if out, err := verifyRunGit(dir, "add", "verify_change.go"); err != nil {
		return fmt.Errorf("git add verify_change.go failed: %v\n%s", err, out)
	}
	if out, err := verifyRunGit(dir, "-c", "commit.gpgsign=false", "commit", "-m", "verify: real change for task-implement"); err != nil {
		return fmt.Errorf("git commit (verify change) failed: %v\n%s", err, out)
	}

	for _, g := range []string{"task-implement", "task-verify"} {
		out, err := verifyRunForgeEnv(forgeBin, dir, env, "task", "gate", g, "--ref", ref)
		if err != nil {
			return fmt.Errorf("gate %s failed: %v\n%s", g, err, out)
		}
	}

	// task-complete 有 code-review-gate 硬前置（ReviewPassed + reviewed-HEAD
	// 快照）：在变更 commit 之后、complete 门禁之前记录 review pass，与真实
	// 工作流一致。
	if out, err := verifyRunForgeEnv(forgeBin, dir, env, "review", "pass"); err != nil {
		return fmt.Errorf("review pass failed: %v\n%s", err, out)
	}
	if out, err := verifyRunForgeEnv(forgeBin, dir, env, "task", "gate", "task-complete", "--ref", ref); err != nil {
		return fmt.Errorf("gate task-complete failed: %v\n%s", err, out)
	}
	return nil
}
