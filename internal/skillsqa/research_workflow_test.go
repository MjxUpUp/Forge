package skillsqa

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// research_workflow_test.go — research-workflow 可信度升级（2026-10-15 复盘）的守卫：
// 交付前 lint 脚本对每类陷阱的检出、SKILL.md 对新 references 的接线、产出质量 golden 集
// 的结构。lint 脚本是分发给用户的 skill 资产，这里用临时 run_dir 实跑它。

const researchSkillDir = "../../skills/research-workflow"

// validRun returns the files of a run_dir that passes research-lint with zero errors and zero warnings.
//
// validRun 返回一个能以 0 ERROR 0 WARN 通过 research-lint 的 run_dir 文件集。
func validRun() map[string]string {
	return map[string]string{
		"brief.md":     "# Brief\n- 原问题（用户原文，逐字）：门禁逻辑可见会不会让作弊率上升\n- 子问题：Q1 有无实证\n",
		"status.json":  `{"topic":"gate","mode":"locked","stage":"delivered","degradations":[],"searches":2,"fetches":1}`,
		"map.md":       "# map\n",
		"scan_anti.md": "# anti\n",
		"dive_01.md":   "## Dimension 01\n",
		"verify.md":    "# verify\n",
		"insight.md":   "# insight\n",
		"search_log.md": "| worker | 时间 | 工具 | 查询 | 纳入 | 理由 |\n|---|---|---|---|---|---|\n" +
			"| d1 | 10:00 | search | metr reward hacking | 纳入 | 一手 |\n" +
			"| d1 | 10:01 | search | impossiblebench | 纳入 | 一手 |\n" +
			"| d1 | 10:02 | fetch | https://example.org/a | 纳入 | 原文 |\n",
		"claims.md": "| ID | 断言 | 限定 | 引用 | 原文 quote | URL | 核验 |\n|---|---|---|---|---|---|---|\n" +
			"| C1 | 作弊率从 92% 降到 1% | 仅 Conflicting-LiveCodeBench，prompt D | [^1^] | \"from 92% to 1%\" | https://example.org/a | 支持 |\n",
		"report.md": "# 结论\n\nQ1：有实证，但只是相关，原文用了 perhaps。[^1^]\n\n" +
			"[^1^]: https://example.org/a — Example Lab, 2026-03-01\n",
	}
}

func writeRun(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func runResearchLint(t *testing.T, dir string) (string, int) {
	t.Helper()
	script, err := filepath.Abs(filepath.Join(researchSkillDir, "scripts", "research-lint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", script, filepath.ToSlash(dir))
	cmd.Env = append(os.Environ(), "LC_ALL=en_US.UTF-8")
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("run research-lint: %v\n%s", err, out)
	}
	return string(out), code
}

// TestResearchLintScript pins that research-lint passes a compliant run and flags each trap class with its rule id.
//
// TestResearchLintScript 钉住 research-lint 放行合规 run，并对每类陷阱报出对应规则编号。
func TestResearchLintScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("research-lint 面向 macOS/Linux 的 bash 3.2+；Git Bash 的路径改写与 awk 多字节行为未在 Windows 上验证")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}

	out, code := runResearchLint(t, writeRun(t, validRun()))
	if code != 0 || !strings.Contains(out, "0 error(s), 0 warning(s)") {
		t.Fatalf("compliant run must pass cleanly, exit=%d:\n%s", code, out)
	}

	cases := []struct {
		name     string
		mutate   func(map[string]string)
		wantCode int
		wantRule string
	}{
		{"missing brief", func(f map[string]string) { delete(f, "brief.md") }, 1, "[E2]"},
		{"brief without original question", func(f map[string]string) { f["brief.md"] = "# Brief\n- 子问题：Q1\n" }, 1, "[E2]"},
		{"status missing stage", func(f map[string]string) {
			f["status.json"] = `{"topic":"gate","mode":"locked","degradations":[]}`
		}, 1, "[E3]"},
		{"delivered without claims", func(f map[string]string) { delete(f, "claims.md") }, 1, "[E4]"},
		{"report written before delivery still checks files", func(f map[string]string) {
			f["status.json"] = `{"topic":"gate","mode":"locked","stage":"report","degradations":[],"searches":2,"fetches":1}`
			delete(f, "verify.md")
		}, 1, "[E4]"},
		{"no dive files", func(f map[string]string) { delete(f, "dive_01.md") }, 1, "[E4]"},
		{"invalid mode", func(f map[string]string) {
			f["status.json"] = `{"topic":"gate","mode":"bogus","stage":"delivered","degradations":[],"searches":2,"fetches":1}`
		}, 1, "[E3]"},
		{"file-only run needs file_analysis", func(f map[string]string) {
			f["status.json"] = `{"topic":"gate","mode":"file-only","stage":"delivered","degradations":[],"searches":2,"fetches":1}`
		}, 1, "[E4]"},
		{"delivered without anti-frame scan", func(f map[string]string) { delete(f, "scan_anti.md") }, 1, "[E4]"},
		{"citation without definition", func(f map[string]string) {
			f["report.md"] = "# 结论\n\n见 [^2^]。\n"
		}, 1, "[E5]"},
		{"definition without URL", func(f map[string]string) {
			f["report.md"] = "# 结论\n\n见 [^1^]。\n\n[^1^]: Example Lab 内部报告, 2026-03-01\n"
		}, 1, "[E5]"},
		{"definition without date", func(f map[string]string) {
			f["report.md"] = "# 结论\n\n见 [^1^]。\n\n[^1^]: https://example.org/a — Example Lab\n"
		}, 1, "[E5]"},
		{"internal label leak", func(f map[string]string) {
			f["report.md"] = "# 结论\n\n本次按锁定模式完成，见 dive_01。[^1^]\n\n[^1^]: https://example.org/a — Example Lab, 2026-03-01\n"
		}, 1, "[E6]"},
		{"claim without verdict", func(f map[string]string) {
			f["claims.md"] = strings.Replace(f["claims.md"], "| 支持 |", "|  |", 1)
		}, 1, "[E7]"},
		{"claim verdict not in the allowed set", func(f map[string]string) {
			f["claims.md"] = strings.Replace(f["claims.md"], "| 支持 |", "| 未支持 |", 1)
		}, 1, "[E7]"},
		{"claim number not in quote", func(f map[string]string) {
			f["claims.md"] = strings.Replace(f["claims.md"], "作弊率从 92% 降到 1%", "作弊率反升 43%，从 92% 降到 1%", 1)
		}, 0, "[W4]"},
		{"unsupported claim kept", func(f map[string]string) {
			f["claims.md"] = strings.Replace(f["claims.md"], "| 支持 |", "| 不支持 |", 1)
		}, 0, "[W2]"},
		{"inflated self-reported searches", func(f map[string]string) {
			f["status.json"] = `{"topic":"gate","mode":"locked","stage":"delivered","degradations":[],"searches":170,"fetches":0}`
		}, 0, "[W1]"},
	}
	for _, tc := range cases {
		files := validRun()
		tc.mutate(files)
		out, code := runResearchLint(t, writeRun(t, files))
		if code != tc.wantCode || !strings.Contains(out, tc.wantRule) {
			t.Errorf("%s: want exit %d with %s, got exit %d:\n%s", tc.name, tc.wantCode, tc.wantRule, code, out)
		}
	}
}

// TestResearchLintNoFalsePositives pins inputs that must pass cleanly: a file-only run citing files without dates, an escaped pipe inside a claim quote, an ordinary English "workers", and years or two-digit numbers in a claim.
//
// TestResearchLintNoFalsePositives 钉住必须干净通过的输入：文件模式引用文件（无日期）、claims
// quote 里的转义竖线、正文里正常的英文 workers、断言里的年份与两位数。另钉缺目录与未知 flag 的退出码。
func TestResearchLintNoFalsePositives(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("research-lint 面向 macOS/Linux 的 bash 3.2+")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	fileOnly := validRun()
	fileOnly["status.json"] = `{"topic":"gate","mode":"file-only","stage":"delivered","degradations":[],"searches":2,"fetches":1}`
	fileOnly["file_analysis.md"] = "# files\n"
	delete(fileOnly, "map.md")
	delete(fileOnly, "scan_anti.md")
	fileOnly["report.md"] = "# 结论\n\nQ1：gig workers 的收入在 2025 年下降 12 个点。[^1^]\n\n[^1^]: [File: survey.pdf, Section: 3.2]\n"
	fileOnly["claims.md"] = "| ID | 断言 | 限定 | 引用 | 原文 quote | URL | 核验 |\n|---|---|---|---|---|---|---|\n" +
		"| C1 | 2025 年收入下降 12 个点，比例 3.5% | 仅美国样本 | [^1^] | \"income fell 3.5% \\| 12 points\" | File: survey.pdf | 支持 |\n"
	if out, code := runResearchLint(t, writeRun(t, fileOnly)); code != 0 || !strings.Contains(out, "0 error(s), 0 warning(s)") {
		t.Fatalf("clean file-only run must pass without findings, exit=%d:\n%s", code, out)
	}

	if out, code := runResearchLint(t, filepath.Join(t.TempDir(), "missing")); code != 1 || !strings.Contains(out, "[E1]") {
		t.Fatalf("missing run_dir must exit 1 with E1, got %d:\n%s", code, out)
	}
	script, _ := filepath.Abs(filepath.Join(researchSkillDir, "scripts", "research-lint.sh"))
	cmd := exec.Command("bash", script, "--bogus", t.TempDir())
	if err := cmd.Run(); err == nil {
		t.Fatal("unknown flag must be rejected")
	} else if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 {
		t.Fatalf("unknown flag must exit 2, got %v", err)
	}
}

// TestResearchWorkflowCredibilityWiring pins that SKILL.md routes to the credibility protocol, the Phase L reference and the lint script, and that every referenced file exists.
//
// TestResearchWorkflowCredibilityWiring 钉住 SKILL.md 指向可信度协议、Phase L reference 与
// lint 脚本，且被指向的文件都存在；搜索量下限不再作为出口条件。
func TestResearchWorkflowCredibilityWiring(t *testing.T) {
	body, err := os.ReadFile(filepath.Join(researchSkillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(body)
	for _, ref := range []string{
		"references/credibility-protocol.md",
		"references/phase-l.md",
		"scripts/research-lint.sh",
	} {
		if !strings.Contains(skill, ref) {
			t.Errorf("SKILL.md must point to %s", ref)
		}
		if _, err := os.Stat(filepath.Join(researchSkillDir, ref)); err != nil {
			t.Errorf("%s referenced but missing: %v", ref, err)
		}
	}
	for _, must := range []string{"brief.md", "claims.md", "status.json", "来源族"} {
		if !strings.Contains(skill, must) {
			t.Errorf("SKILL.md must mention %s", must)
		}
	}
	for _, gone := range []string{"≥110 searches", "≥60 searches", "## Phase L — 轻量事实核查（≤20"} {
		if strings.Contains(skill, gone) {
			t.Errorf("SKILL.md still contains retired text %q", gone)
		}
	}
	proto, err := os.ReadFile(filepath.Join(researchSkillDir, "references", "credibility-protocol.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"研究契约", "来源族", "限定条件", "反证", "承重断言", "本地适用性", "status.json", "停止规则"} {
		if !strings.Contains(string(proto), section) {
			t.Errorf("credibility-protocol.md missing section %q", section)
		}
	}
}

// TestResearchWorkflowOutputCases pins the golden output-quality set: at least six cases, unique ids, each with a prompt, a trap class and expectations, covering the six trap classes from the 2026-10 review.
//
// TestResearchWorkflowOutputCases 钉住产出质量 golden 集：≥6 条、id 唯一、每条有题面、
// 陷阱类别与期望，覆盖 2026-10 复盘的六类陷阱。
func TestResearchWorkflowOutputCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(researchSkillDir, "evals", "output-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Metrics []string `json:"metrics"`
		Cases   []struct {
			ID     string   `json:"id"`
			Trap   string   `json:"trap"`
			Prompt string   `json:"prompt"`
			Expect []string `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &set); err != nil {
		t.Fatalf("output-cases.json: %v", err)
	}
	if len(set.Cases) < 6 || len(set.Metrics) == 0 {
		t.Fatalf("want >=6 cases and metrics, got %d cases, %d metrics", len(set.Cases), len(set.Metrics))
	}
	seen := map[string]bool{}
	traps := map[string]bool{}
	for _, c := range set.Cases {
		if c.ID == "" || seen[c.ID] || strings.TrimSpace(c.Prompt) == "" || len(c.Expect) == 0 {
			t.Errorf("case %q: need unique id, prompt and expectations", c.ID)
		}
		seen[c.ID] = true
		traps[c.Trap] = true
	}
	for _, trap := range []string{"hedge", "co-author", "same-origin", "stale", "local-applicability", "adjacent-question"} {
		if !traps[trap] {
			t.Errorf("golden set missing trap class %q", trap)
		}
	}
}
