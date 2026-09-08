package evalkit

// artifactdrill.go — 产物链行为级演练（artifact-chain-workflow.md 落地收尾）：
// 脚本化重放产物链全链——登记（start --artifact + 验收自动提取）→ hard 档前置
// 阻断 → human 档产物/审批阻断 → 审批放行 → 提取的验收实跑 → 手改产物 complete
// 被漂移 pre-flight 拦截 → 重登记重审批 → complete。与 wedge-drill 同构：隔离
// 环境（HOME/FORGE_DATA_HOME 在临时目录，宿主零接线）、逐步机械判定（退出码 +
// stdout 子串 + expectFail 红态）、失败短路。用途：发布冒烟（release workflow
// 的 drill 门禁位）与每日行为回归（nightly）。
//
// artifactdrill.go — artifact-chain behavioral drill: scripted replay of the
// full artifact chain (register at start with acceptance extraction → hard-tier
// prereq block → human-tier artifact/approval blocks → approval passes the gate
// → extracted acceptance real-runs → tampered artifact blocks complete via the
// drift pre-flight → re-register/re-approve → complete). Same shape as the
// wedge drill: isolated env, per-step mechanical verdicts, fail-short.

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// artifactSchemaYAML 是演练注入的链声明：proposal(advisory) → spec(hard, 前置
// proposal) → design(human, 前置 spec) → plan(advisory)——四档里取两档阻断位，
// 正好覆盖「链序前置」与「人工审批」两条事实执法路径。
const artifactSchemaYAML = `version: 1
stages:
  - name: proposal
  - name: spec
    mode: hard
    requires: [proposal]
  - name: design
    mode: human
    requires: [spec]
  - name: plan
`

// artifactScript 构建产物链演练脚本。产物源文件放 tmp/src（fixture repo 之外）——
// 链产物经 WriteArtifact 落 DataDir specs，repo 树内不留 .md（doc gate 零接线，
// 演练只测产物链本身）。
func artifactScript(forgeBin, tmp string) []wedgeStep {
	src := filepath.Join(tmp, "src")
	dataGlob := `"$FORGE_DATA_HOME"/projects`
	return []wedgeStep{
		{name: "git init", argv: []string{"git", "init", "-q"}},
		{name: "forge init", argv: []string{forgeBin, "init"}},
		{name: "fixture", env: []string{"SRC=" + src}, argv: []string{"sh", "-c",
			"printf '#!/bin/sh\\necho NOT-READY\\nexit 1\\n' > main_check.sh && printf 'v1\\n' > code.txt && mkdir -p \"$SRC\" && printf '# spec\\n\\n- accept: sh main_check.sh :: ALL-GOOD\\n' > \"$SRC/spec.txt\" && printf '# design\\n\\nfmt output.\\n' > \"$SRC/design.txt\" && printf '# proposal\\n\\nwhy + what.\\n' > \"$SRC/proposal.txt\" && git add main_check.sh code.txt && git -c user.name=drill -c user.email=drill@localhost -c commit.gpgsign=false commit -qm init"}},
		{name: "start --artifact(登记+提取)", argv: []string{forgeBin, "task", "start", "--ref", "test/artifact-drill", "--branch",
			"--artifact", "spec=" + src + "/spec.txt"}, expectContains: []string{"main_check.sh :: ALL-GOOD"}},
		{name: "schema(注入 hard/human 档)", argv: []string{"sh", "-c",
			`set -- ` + dataGlob + `/*/; [ -d "$1" ] || { echo "no project dir under FORGE_DATA_HOME"; exit 1; }; for d in "$@"; do mkdir -p "$d/schemas"; printf '%s' '` + artifactSchemaYAML + `' > "$d/schemas/schema.yaml"; done`}},
		{name: "codechange", argv: []string{"sh", "-c", "echo v2 > code.txt"}},
		{name: "gate(阻断·hard 前置缺失)", argv: []string{forgeBin, "task", "gate", "task-implement"},
			expectFail: true, expectContains: []string{"proposal"}},
		{name: "set proposal", argv: []string{forgeBin, "task", "artifact", "--set", "proposal", "--file", src + "/proposal.txt"}},
		{name: "gate(阻断·human 产物缺失)", argv: []string{forgeBin, "task", "gate", "task-implement"},
			expectFail: true, expectContains: []string{"design（human）: 产物未登记"}},
		{name: "set design", argv: []string{forgeBin, "task", "artifact", "--set", "design", "--file", src + "/design.txt"}},
		{name: "gate(阻断·审批缺失)", argv: []string{forgeBin, "task", "gate", "task-implement"},
			expectFail: true, expectContains: []string{"人工审批缺失"}},
		{name: "approve design", argv: []string{forgeBin, "task", "artifact", "--approve", "design", "--by", "drill-reviewer"},
			expectContains: []string{"已审批 design"}},
		{name: "gate(链齐放行)", argv: []string{forgeBin, "task", "gate", "task-implement"},
			expectContains: []string{"— passed"}},
		{name: "fixture(绿)", argv: []string{"sh", "-c", "printf '#!/bin/sh\\necho ALL-GOOD\\n' > main_check.sh"}},
		{name: "verify(提取的验收实跑)", argv: []string{forgeBin, "task", "verify-acceptance"},
			expectContains: []string{"ALL-GOOD"}},
		{name: "gate task-verify", argv: []string{forgeBin, "task", "gate", "task-verify"}},
		{name: "review pass", argv: []string{forgeBin, "review", "pass"}},
		{name: "gate task-complete", argv: []string{forgeBin, "task", "gate", "task-complete"}},
		{name: "tamper(手改产物)", argv: []string{"sh", "-c",
			"for f in " + dataGlob + "/*/specs/*/design.md; do printf '\\ntampered\\n' >> \"$f\"; done"}},
		{name: "complete(阻断·漂移)", argv: []string{forgeBin, "task", "complete"},
			expectFail: true, expectContains: []string{"漂移"}},
		{name: "re-register design", argv: []string{forgeBin, "task", "artifact", "--set", "design", "--file", src + "/design.txt"}},
		{name: "re-approve design", argv: []string{forgeBin, "task", "artifact", "--approve", "design", "--by", "drill-reviewer"}},
		{name: "complete(修复后放行)", argv: []string{forgeBin, "task", "complete"},
			expectContains: []string{"completed!"}},
	}
}

// RunArtifactDrill replays the artifact-chain script in an isolated environment.
// Mechanical verdicts only — a drill pass is a fact, not a judgment.
//
// RunArtifactDrill 在隔离环境重放产物链脚本。机械判定——演练通过是一个事实。
func RunArtifactDrill(forgeBin string) (WedgeDrillResult, error) {
	if forgeBin == "" {
		return WedgeDrillResult{}, fmt.Errorf("evalkit: 产物链演练需要真实 forge 二进制")
	}
	tmp, err := os.MkdirTemp("", "evalkit-artifact-")
	if err != nil {
		return WedgeDrillResult{}, err
	}
	defer os.RemoveAll(tmp)
	return runDrill(tmp, artifactScript(forgeBin, tmp))
}

// PersistArtifactReport writes the artifact drill report and the audit row.
//
// PersistArtifactReport 写产物链演练报告与审计行。
func PersistArtifactReport(evalDir string, repoRoot string, res WedgeDrillResult) (string, error) {
	dir := evalDataDir(evalDir)
	payload := map[string]any{"generated_at": time.Now().UTC(), "passed": res.Passed, "total": res.Total, "steps": res.Steps}
	data, err := jsonMarshal(payload)
	if err != nil {
		return "", err
	}
	path := filepathJoin(dir, fmt.Sprintf("artifact-drill-%s.json", time.Now().UTC().Format("20060102-150405")))
	if err := atomicWriteFile(path, data); err != nil {
		return "", err
	}
	_ = checklog.Record(repoRoot, &checklog.Entry{
		Check:   checklog.CheckArtifactDrill,
		Passed:  res.Passed,
		Checked: true,
		Detail:  fmt.Sprintf(`artifact drill: %d steps, total %s`, len(res.Steps), res.Total),
	})
	return path, nil
}
