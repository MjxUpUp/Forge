package cli

// eval.go — `forge eval` 命令族：Forge 自评测体系（docs/design/
// forge-evaluation-system.md）的 CLI 面。所有子命令都是观测/开发期工具：零新增
// 任务门禁；评测自身不合法（字典缺字段/卡不完整/指纹不符）时按 BLOCKED 契约
// 非零退出。命令树注册后自动进入 docsconsistency 反查。
//
// eval.go — the `forge eval` command family: CLI surface of Forge's
// self-evaluation system. Every subcommand is observability/dev-time tooling:
// zero new task gates; when the evaluation itself is illegitimate (incomplete
// dictionary/card, fingerprint mismatch) it exits non-zero under the BLOCKED
// contract. Registered commands are automatically covered by the
// docsconsistency tree audit.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/evalkit"
	"github.com/MjxUpUp/Forge/internal/skillseval"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var evalCmd = &cobra.Command{
	Use:   "eval",
	Short: "Forge 自评测（双轨：端到端 profile×model × 治理层 golden/遥测/陷阱）",
}

// recordEvalRow records one observation-class eval audit row on the cwd project.
//
// recordEvalRow 在 cwd 项目上记录一条观察类评测审计行。
func recordEvalRow(check checklog.CheckName, passed bool, detail string) {
	_ = checklog.Record(evalRepoRoot(), &checklog.Entry{
		Check: check, Passed: passed, Checked: true, Detail: detail,
	})
}

// evalAssetPath resolves an in-repo eval asset relative to cwd (the assets are
// committed at evals/forge/ — the command is meant to run inside the Forge repo).
//
// evalAssetPath 以 cwd 解析仓内评测资产（资产提交在 evals/forge/——本命令预期
// 在 Forge 仓库内运行）。
func evalAssetPath(rel string) string { return filepath.Join("evals", "forge", rel) }

// evalRepoRoot is the repo root for checklog audit rows (cwd).
//
// evalRepoRoot 是 checklog 审计行的仓库根（cwd）。
func evalRepoRoot() string { cwd, _ := os.Getwd(); return cwd }

func runEvalCard(cmd *cobra.Command, args []string) error {
	render, _ := cmd.Flags().GetBool("render")
	path := evalAssetPath("gates-card.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("BLOCKED: 读取披露卡失败（在 Forge 仓库根运行）: %v", err)
	}
	var card evalkit.GatesCard
	if err := yaml.Unmarshal(data, &card); err != nil {
		return fmt.Errorf("BLOCKED: 解析披露卡失败: %v", err)
	}
	if err := card.Validate(); err != nil {
		recordEvalRow(checklog.CheckEvalMetricsIncomplete, false, "gates-card: "+err.Error())
		return fmt.Errorf("BLOCKED: 披露卡不完整: %v", err)
	}
	if !render {
		fmt.Println("✅ gates-card — passed（五节校验通过；--render 查看全文）")
		return nil
	}
	out, err := card.RenderMarkdown()
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	fmt.Print(out)
	return nil
}

func runEvalDashboard(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	asJSON, _ := cmd.Flags().GetBool("json")
	dict, err := evalkit.LoadDictionary(evalAssetPath("metrics.yaml"))
	if err != nil {
		recordEvalRow(checklog.CheckEvalMetricsIncomplete, false, err.Error())
		return fmt.Errorf("BLOCKED: %v", err)
	}
	if dryRun {
		fmt.Printf("✅ metrics 字典 v%d：%d 条指标（C1-C7 roster 完整）\n", dict.Version, len(dict.Metrics))
		fmt.Println("✅ 数据源：checklog/toollog/registry 解析链就绪（--dry-run 不产出结论）")
		return nil
	}
	root := evalRepoRoot()
	rep, err := evalkit.Aggregate(root, dict, time.Now())
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	snapDir := filepath.Join(evalDir, "forge", "snapshots")
	if err := os.MkdirAll(snapDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	if err := util.AtomicWrite(filepath.Join(snapDir, fmt.Sprintf("snapshot-%s.json", rep.GeneratedAt.UTC().Format("20060102-150405"))), data, 0o644); err != nil {
		return err
	}
	if asJSON {
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("Forge 自评测仪表盘（%s；字典 v%d；checklog %d 条 / %d sessions）\n",
		rep.GeneratedAt.Format("2006-01-02 15:04"), dict.Version, rep.Entries, rep.Sessions)
	if rep.SignatureAudit.Forged > 0 {
		fmt.Printf("⚠ 伪造审计行 %d 条（本机可归属行验签失败）——C6 安全事件，立即溯源（他机行 %d / 历史无签行 %d 不在此列）\n",
			rep.SignatureAudit.Forged, rep.SignatureAudit.ForeignNode, rep.SignatureAudit.UnsignedLegacy)
	}
	for _, w := range rep.Weeks {
		fmt.Printf("  周 %s：blocked %d / advisory %d\n", w.WeekStart, w.Blocked, w.Advisory)
	}
	for _, r := range rep.Rates {
		if r.Insufficient {
			fmt.Printf("  %-22s INSUFFICIENT — %s\n", r.MetricID, r.Note)
			continue
		}
		if r.Denominator <= 1 {
			fmt.Printf("  %-22s %g\n", r.MetricID, r.Value)
			continue
		}
		fmt.Printf("  %-22s %.3f（95%% CI %.3f–%.3f，n=%d）\n", r.MetricID, r.Value, r.Lo, r.Hi, r.Denominator)
		if r.MisuseNote != "" {
			fmt.Printf("    ⚠ 误用注记：%s\n", r.MisuseNote)
		}
	}
	fmt.Printf("快照已写入 %s\n", snapDir)
	return nil
}

func runEvalGoldenRun(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	repeats, _ := cmd.Flags().GetInt("repeats")
	rewrite, _ := cmd.Flags().GetBool("rewrite-manifest")
	asJSON, _ := cmd.Flags().GetBool("json")
	if dir == "" {
		dir = evalAssetPath("golden")
	}
	cases, err := evalkit.LoadGoldenDir(dir)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	fp := evalkit.GoldenFingerprint(cases)
	if pinned := evalkit.LoadGoldenManifest(dir); pinned == "" {
		if err := evalkit.RewriteGoldenManifest(dir, cases); err != nil {
			return err
		}
		fmt.Println("已写入 golden manifest 基线（首跑钉指纹）")
	} else if pinned != fp {
		if !rewrite {
			return fmt.Errorf("BLOCKED: golden 指纹不符（期望 %s… 实际 %s…）——改样本凑数字被拒；确属轮换用 --rewrite-manifest", pinned[:12], fp[:12])
		}
		if err := evalkit.RewriteGoldenManifest(dir, cases); err != nil {
			return err
		}
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	rep, err := evalkit.RunGolden(dir, cases, evalkit.GoldenOptions{ForgeBin: bin, Repetitions: repeats})
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	path, err := evalkit.PersistGoldenReport(evalDir, evalRepoRoot(), rep)
	if err != nil {
		return err
	}
	if asJSON {
		data, _ := json.MarshalIndent(rep, "", "  ")
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("golden run：precision %d/%d（Wilson %.2f–%.2f） fpr %d/%d（%.2f–%.2f）\n",
		rep.Precision.Numerator, rep.Precision.Denominator, rep.Precision.Lo, rep.Precision.Hi,
		rep.FalsePositive.Numerator, rep.FalsePositive.Denominator, rep.FalsePositive.Lo, rep.FalsePositive.Hi)
	for _, f := range rep.Findings {
		fmt.Printf("  FINDING: %s\n", f)
	}
	for _, c := range rep.Cases {
		fmt.Printf("  %-38s %-8s → %s（重放一致率 %.2f）\n", c.ID, c.Kind, c.Outcome, c.Agreement)
	}
	fmt.Printf("报告：%s\n", path)
	return nil
}

func runEvalGoldenRotate(cmd *cobra.Command, args []string) error {
	maxCases, _ := cmd.Flags().GetInt("max-cases")
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	rec, path, err := evalkit.RotatePrivateGolden(evalDir, maxCases, evalRepoRoot())
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	fmt.Printf("轮换完成：kept %d retired %v invalid %d\n审计：%s\n", rec.Kept, rec.Retired, len(rec.Invalid), path)
	return nil
}

func runEvalGoldenPrivateInit(cmd *cobra.Command, args []string) error {
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	dir, err := evalkit.InitPrivateGolden(evalDir)
	if err != nil {
		return err
	}
	fmt.Printf("私有 golden 目录已建（0700，永不进 VCS）：%s\n", dir)
	return nil
}

func runEvalTraps(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		dir = evalAssetPath("traps")
	}
	traps, err := evalkit.LoadTrapDir(dir)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	rep, err := evalkit.RunTraps(traps, evalkit.GoldenOptions{ForgeBin: bin, Repetitions: 1})
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	path, err := evalkit.PersistTrapReport(evalDir, evalRepoRoot(), rep)
	if err != nil {
		return err
	}
	fmt.Printf("trap run：capture %d/%d\n", rep.CaptureRate.Numerator, rep.CaptureRate.Denominator)
	for _, t := range rep.Traps {
		status := "识破"
		if !t.Captured {
			status = "未识破"
		}
		fmt.Printf("  %-34s %-22s → %s\n", t.ID, t.Type, status)
	}
	for _, f := range rep.Findings {
		fmt.Printf("  FINDING: %s\n", f)
	}
	fmt.Printf("报告：%s\n", path)
	return nil
}

func runEvalJudgeAudit(cmd *cobra.Command, args []string) error {
	scoresPath, _ := cmd.Flags().GetString("scores")
	if scoresPath == "" {
		return fmt.Errorf("BLOCKED: 需要 --scores <file>（外部 rubric 评审轮次产出的分数 JSON）")
	}
	entries, err := evalkit.LoadJudgeScores(scoresPath)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	rep, err := evalkit.RunJudgeAudit(entries)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	path, err := evalkit.PersistJudgeAudit(evalDir, evalRepoRoot(), rep)
	if err != nil {
		return err
	}
	if rep.KappaValid {
		fmt.Printf("judge audit：κ=%.2f（阈值 %.2f）reliable=%v\n", rep.Kappa, evalkit.JudgeAuditKappaFloor, rep.JudgeReliable)
	} else {
		fmt.Println("judge audit：κ 无定义（样本不足或全同类别）")
	}
	for _, f := range rep.Findings {
		fmt.Printf("  FINDING: %s\n", f)
	}
	fmt.Printf("报告：%s\n", path)
	return nil
}

func runEvalResumeDrill(cmd *cobra.Command, args []string) error {
	dir, _ := cmd.Flags().GetString("dir")
	if dir == "" {
		dir = evalAssetPath("resume")
	}
	drills, err := evalkit.LoadResumeDrills(dir)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	results, err := evalkit.RunResumeDrills(drills, bin)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	fidelity, err := evalkit.ResumeFidelity(results)
	if err != nil {
		return err
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	path, err := evalkit.PersistResumeReport(evalDir, evalRepoRoot(), results, fidelity)
	if err != nil {
		return err
	}
	fmt.Printf("resume drills：%d/%d passed\n", fidelity.Numerator, fidelity.Denominator)
	for _, r := range results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL " + r.FailedAt
		}
		fmt.Printf("  %-30s %s\n", r.ID, status)
	}
	fmt.Printf("报告：%s\n", path)
	return nil
}

func runEvalRun(cmd *cobra.Command, args []string) error {
	manifestPath, _ := cmd.Flags().GetString("manifest")
	profile, _ := cmd.Flags().GetString("profile")
	model, _ := cmd.Flags().GetString("model")
	repeats, _ := cmd.Flags().GetInt("repeats")
	wallclock, _ := cmd.Flags().GetDuration("wallclock")
	forgeRef, _ := cmd.Flags().GetString("forge-ref")
	manifest, err := evalkit.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	spec := evalkit.RunSpec{
		Profile: evalkit.Profile(profile), Model: model, Benchmark: manifest.ID, Split: manifest.Split,
		Repeats: repeats, ForgeRef: forgeRef,
		Budget: evalkit.Budget{WallclockEach: wallclock},
	}
	if err := spec.Validate(); err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	runner, sandbox, degraded := evalkit.SelectRunner(manifest, os.Getenv(evalkit.SmokeManifestEnv) != "")
	if degraded {
		fmt.Println("⚠ sandbox 降级：docker 不可用——容器任务回退命令套件（scorecard 已标注 fallback-exec）")
	} else {
		fmt.Printf("sandbox=%s\n", sandbox)
	}
	sc, err := evalkit.RunBenchmark(cmd.Context(), spec, manifest, runner)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	path, err := evalkit.PersistScorecard(evalDir, evalRepoRoot(), sc)
	if err != nil {
		return err
	}
	fmt.Println("SCORECARD | " + sc.Header)
	fmt.Printf("pass@1 %.3f（n=%d）pass^k 曲线：%v\n", sc.Pass1.Value, sc.Pass1.Denominator, formatPassK(sc.PassKCurve))
	fmt.Printf("tokens %d cost $%.4f budget_cuts %d\n", sc.TotalTokens, sc.TotalCostUSD, sc.BudgetCuts)
	if sc.Note != "" {
		fmt.Println("注：" + sc.Note)
	}
	fmt.Printf("scorecard：%s\n", path)
	return nil
}

func formatPassK(curve []evalkit.PassKPoint) string {
	var parts []string
	for _, p := range curve {
		parts = append(parts, fmt.Sprintf("k=%d:%.2f", p.K, p.Value))
	}
	return strings.Join(parts, " ")
}

func runEvalDecompose(cmd *cobra.Command, args []string) error {
	manifestPath, _ := cmd.Flags().GetString("manifest")
	modelsFlag, _ := cmd.Flags().GetString("models")
	profilesFlag, _ := cmd.Flags().GetString("profiles")
	repeats, _ := cmd.Flags().GetInt("repeats")
	wallclock, _ := cmd.Flags().GetDuration("wallclock")
	manifest, err := evalkit.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	grid := evalkit.DecomposeGrid{Models: splitCSV(modelsFlag)}
	for _, p := range splitCSV(profilesFlag) {
		grid.Profiles = append(grid.Profiles, evalkit.Profile(p))
	}
	spec := evalkit.RunSpec{
		Model: "grid", Benchmark: manifest.ID, Split: manifest.Split, Repeats: repeats,
		ForgeRef: "decompose", Budget: evalkit.Budget{WallclockEach: wallclock},
	}
	runner, sandbox, degraded := evalkit.SelectRunner(manifest, os.Getenv(evalkit.SmokeManifestEnv) != "")
	if degraded {
		fmt.Println("⚠ sandbox 降级：docker 不可用——回退命令套件（报告已标注）")
	} else {
		fmt.Printf("sandbox=%s\n", sandbox)
	}
	rep, err := evalkit.RunDecompose(cmd.Context(), grid, spec, manifest, runner)
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	path, err := evalkit.PersistDecomposeReport(evalDir, evalRepoRoot(), rep)
	if err != nil {
		return err
	}
	fmt.Print(rep.RenderDecomposeMarkdown())
	fmt.Printf("报告：%s\n", path)
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runEvalAuditVerify(cmd *cobra.Command, args []string) error {
	sum, err := evalkit.VerifyAuditRows(evalRepoRoot())
	if err != nil {
		return fmt.Errorf("BLOCKED: %v", err)
	}
	fmt.Printf("审计行验签：valid %d / unsigned-legacy %d / foreign-node %d / forged %d\n",
		sum.Valid, sum.UnsignedLegacy, sum.ForeignNode, sum.Forged)
	if sum.Forged > 0 {
		recordEvalRow(checklog.CheckEvalJudgeWeak, false,
			fmt.Sprintf(`伪造审计行 %d 条（本机可归属行验签失败）`, sum.Forged))
		return fmt.Errorf("BLOCKED: 伪造审计行 %d 条——本机可归属行验签失败，审计时间线已被污染，立即溯源（他机行需公钥注册表，v1 边界）", sum.Forged)
	}
	return nil
}

func runEvalReport(cmd *cobra.Command, args []string) error {
	quarter, _ := cmd.Flags().GetString("quarter")
	if quarter == "" {
		now := time.Now().UTC()
		quarter = fmt.Sprintf("%d-Q%d", now.Year(), (int(now.Month())-1)/3+1)
	}
	evalDir, err := skillseval.EvalDir()
	if err != nil {
		return err
	}
	md, missing, err := evalkit.BuildQuarterlyReport(evalDir, quarter, time.Now())
	if err != nil {
		return err
	}
	fmt.Print(md)
	if len(missing) > 0 {
		fmt.Printf("（%d 项证据缺失，已如实标注）\n", len(missing))
	}
	return nil
}

func init() {
	cardCmd := &cobra.Command{
		Use:   "card [--render]",
		Short: "治理披露卡：Forge 占层/hook/门禁/逃生舱/已知盲区（缺节 BLOCKED）",
		RunE:  runEvalCard,
	}
	cardCmd.Flags().Bool("render", false, "渲染 Markdown 全文")
	evalCmd.AddCommand(cardCmd)

	dash := &cobra.Command{
		Use:   "dashboard [--dry-run] [--json]",
		Short: "Track B 遥测仪表盘（C4/C7：override/escape/wait/off_churn + Wilson 区间 + 误用注记）",
		RunE:  runEvalDashboard,
	}
	dash.Flags().Bool("dry-run", false, "只校验字典与数据源连通，不产出结论")
	dash.Flags().Bool("json", false, "输出 JSON")
	evalCmd.AddCommand(dash)

	goldenRun := &cobra.Command{
		Use:   "run [--dir <dir>] [--repeats N] [--rewrite-manifest] [--json]",
		Short: "门禁 golden 重放：precision/recall + 确定性（指纹不符拒绝启动）",
		RunE:  runEvalGoldenRun,
	}
	goldenRun.Flags().String("dir", "", "golden 用例目录（默认 evals/forge/golden）")
	goldenRun.Flags().Int("repeats", 3, "每用例重放次数（确定性判定）")
	goldenRun.Flags().Bool("rewrite-manifest", false, "轮换后重钉指纹")
	goldenRun.Flags().Bool("json", false, "输出 JSON")

	golden := &cobra.Command{Use: "golden", Short: "golden 标注集（precision/recall 基线）"}
	goldenPrivateInit := &cobra.Command{
		Use:   "private-init",
		Short: "建私有 golden 目录（0700，永不进 VCS）",
		RunE:  runEvalGoldenPrivateInit,
	}
	goldenRotate := &cobra.Command{
		Use:   "rotate [--max-cases N]",
		Short: "私有 golden 季度轮换（oracle 复验 + 最老优先淘汰 + 审计行）",
		RunE:  runEvalGoldenRotate,
	}
	goldenRotate.Flags().Int("max-cases", 30, "轮换后保留上限")
	golden.AddCommand(goldenRun, goldenPrivateInit, goldenRotate)
	evalCmd.AddCommand(golden)

	traps := &cobra.Command{
		Use:   "run [--dir <dir>]",
		Short: "对抗陷阱重放（测试削弱/伪造审计证据/虚假完成）",
		RunE:  runEvalTraps,
	}
	traps.Flags().String("dir", "", "陷阱用例目录（默认 evals/forge/traps）")
	trapsCmd := &cobra.Command{Use: "traps", Short: "对抗陷阱（C2）"}
	trapsCmd.AddCommand(traps)
	evalCmd.AddCommand(trapsCmd)

	judge := &cobra.Command{
		Use:   "judge-audit --scores <file>",
		Short: "判分器受审：重放方差 + 人工一致率 κ（<0.6 降级 ADVISORY）",
		RunE:  runEvalJudgeAudit,
	}
	judge.Flags().String("scores", "", "分数 JSON（外部 rubric 评审产出）")
	evalCmd.AddCommand(judge)

	resume := &cobra.Command{
		Use:   "resume-drill [--dir <dir>]",
		Short: "接续演练：脚本化断点续做（C3，仅回归对比）",
		RunE:  runEvalResumeDrill,
	}
	resume.Flags().String("dir", "", "演练目录（默认 evals/forge/resume）")
	evalCmd.AddCommand(resume)

	runCmd := &cobra.Command{
		Use:   "run --manifest <file> --profile <p> --model <m>",
		Short: "Track A 基准运行（四元组 scorecard；真实执行需 FORGE_EVAL_SMOKE）",
		RunE:  runEvalRun,
	}
	runCmd.Flags().String("manifest", "", "基准 manifest YAML")
	runCmd.Flags().String("profile", "full", "off|gates-only|full")
	runCmd.Flags().String("model", "", "被测模型标识")
	runCmd.Flags().Int("repeats", 2, "每任务重复次数")
	runCmd.Flags().Duration("wallclock", 10*time.Second, "单任务墙钟预算")
	runCmd.Flags().String("forge-ref", "self", "forge 版本标识（四元组之一）")
	evalCmd.AddCommand(runCmd)

	decompose := &cobra.Command{
		Use:   "decompose --manifest <file> --models <a,b> [--profiles full,off]",
		Short: "方差分解：HV̄/MV̄ + 翻转数 + η²_p + 三档差值（季度大体检）",
		RunE:  runEvalDecompose,
	}
	decompose.Flags().String("manifest", "", "基准 manifest YAML")
	decompose.Flags().String("models", "", "逗号分隔模型列表（≥2）")
	decompose.Flags().String("profiles", "off,gates-only,full", "逗号分隔 profile（≥2）")
	decompose.Flags().Int("repeats", 2, "每格重复次数")
	decompose.Flags().Duration("wallclock", 10*time.Second, "单任务墙钟预算")
	evalCmd.AddCommand(decompose)

	auditVerify := &cobra.Command{
		Use:   "audit-verify",
		Short: "审计行验签：伪造审计行 >0 即 BLOCKED（签名/验签闭环的读侧）",
		RunE:  runEvalAuditVerify,
	}
	evalCmd.AddCommand(auditVerify)

	report := &cobra.Command{
		Use:   "report [--quarter 2026-Q3]",
		Short: "季度自评测报告（只汇编已落盘证据，缺失如实标注）",
		RunE:  runEvalReport,
	}
	report.Flags().String("quarter", "", "季度标签（默认当前季）")
	evalCmd.AddCommand(report)

	rootCmd.AddCommand(evalCmd)
}
