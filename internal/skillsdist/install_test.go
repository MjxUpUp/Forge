package skillsdist

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsqa"
)

func mustMk(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// writeCanonicalSkill creates name/SKILL.md under canonical (with a short desc that fails quality, paired with SkipQuality).
//
// writeCanonicalSkill 在 canonical 下建 name/SKILL.md（不合格的简短 desc，配 SkipQuality 用）。
func writeCanonicalSkill(t *testing.T, canonical, name string) string {
	t.Helper()
	sd := filepath.Join(canonical, name)
	mustMk(t, os.MkdirAll(sd, 0755))
	mustMk(t, os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte("---\nname: "+name+"\ndescription: d\n---\n\nbody\n"), 0644))
	return sd
}

func copyOpts(projectDir string) InstallOpts {
	return InstallOpts{
		Mode:             ModeCopy,
		Targets:          []Target{TargetClaude},
		Global:           false,
		ProjectSkillsDir: projectDir,
		SkipQuality:      true,
	}
}

func TestDetectState_MissingCopyInSyncDrift(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	canonSkillMD := filepath.Join(skillDir, "SKILL.md")

	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")

	// missing
	if got := detectState(skillDir, dst); got != StateMissing {
		t.Fatalf("missing: got %s", got)
	}

	// copy-in-sync: copied SKILL.md content matches
	//
	// copy-in-sync：拷贝 SKILL.md 内容一致
	mustMk(t, os.MkdirAll(dst, 0755))
	data, _ := os.ReadFile(canonSkillMD)
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), data, 0644))
	if got := detectState(skillDir, dst); got != StateCopyInSync {
		t.Fatalf("copy-in-sync: got %s", got)
	}

	// drift: modify target SKILL.md
	//
	// drift：改目标 SKILL.md
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("---\nname: my-skill\ndescription: drifted\n---\n\nother\n"), 0644))
	if got := detectState(skillDir, dst); got != StateDrift {
		t.Fatalf("drift: got %s", got)
	}
}

// TestDetectState_Linked: link is the core capability for forge cross-drive single source, must work.
// Windows mklink /J needs no admin; Linux symlink is user-creatable. Both CI ends should pass.
//
// TestDetectState_Linked：link 是 forge 跨盘单源的核心能力，必须可用。
// Windows mklink /J 无需管理员；Linux symlink 普通用户可建。两端 CI 都该通过。
func TestDetectState_Linked(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")
	mustMk(t, makeDirLink(dst, skillDir))
	if got := detectState(skillDir, dst); got != StateLinked {
		t.Fatalf("linked: got %s", got)
	}
}

func TestInstall_Copy_Missing(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill")
	rep, err := Install(canonical, copyOpts(t.TempDir()))
	mustMk(t, err)
	if rep.Stats.Installed != 1 {
		t.Fatalf("installed=%d want 1", rep.Stats.Installed)
	}
	if rep.Stats.Failed != 0 {
		t.Fatalf("failed=%d want 0", rep.Stats.Failed)
	}
}

func TestInstall_DriftAbort(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill")
	projectDir := t.TempDir()
	dst := filepath.Join(projectDir, "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("---\nname: my-skill\ndescription: drifted\n---\n\nother\n"), 0644))

	opts := copyOpts(projectDir)
	opts.DriftPolicy = DriftAbort
	_, err := Install(canonical, opts)
	if err == nil {
		t.Fatal("want abort error on drift")
	}
}

func TestInstall_DriftOverwrite(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill")
	projectDir := t.TempDir()
	dst := filepath.Join(projectDir, "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("drifted"), 0644))

	opts := copyOpts(projectDir)
	opts.DriftPolicy = DriftOverwrite
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Installed != 1 {
		t.Fatalf("installed=%d want 1 (overwrite)", rep.Stats.Installed)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	want, _ := os.ReadFile(filepath.Join(canonical, "my-skill", "SKILL.md"))
	if string(got) != string(want) {
		t.Fatal("overwrite 后内容未与 canonical 一致")
	}
}

func TestInstall_DriftSkip(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill")
	projectDir := t.TempDir()
	dst := filepath.Join(projectDir, "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("drifted"), 0644))

	opts := copyOpts(projectDir)
	opts.DriftPolicy = DriftSkip
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Skipped != 1 {
		t.Fatalf("skipped=%d want 1 (drift skip)", rep.Stats.Skipped)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if string(got) != "drifted" {
		t.Fatal("skip 不应改动 drift 目标内容")
	}
}

func TestInstall_ReservedName(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "forge-quality")
	projectDir := t.TempDir()
	rep, err := Install(canonical, copyOpts(projectDir))
	mustMk(t, err)
	if rep.Stats.Installed != 0 {
		t.Fatalf("reserved name 不应安装，installed=%d", rep.Stats.Installed)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "forge-quality")); err == nil {
		t.Fatal("reserved name 被错误写入目标")
	}
}

func TestInstall_QualityBlock(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill") // desc 过短等，registry 不合格
	opts := copyOpts(t.TempDir())
	opts.SkipQuality = false
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Failed == 0 {
		t.Fatal("不合格 skill 应被质量门控拦截")
	}
}

// TestInstall_BlocksOnSingleCritical is the #4 score-math guard: the install gate used to block
// only on rec==DO_NOT_INSTALL (score≥50 — reachable with ≥3 CRITICALs). A single CRITICAL finding
// (e.g. DE-1 conf 0.9 → 22.5) lands in CAUTION and was let through to all targets. Any CRITICAL
// must block, regardless of the aggregate score.
//
// TestInstall_BlocksOnSingleCritical 是 #4 分数数学守卫：install 门禁此前只在
// rec==DO_NOT_INSTALL（score≥50——需 ≥3 条 CRITICAL）时阻断。单条 CRITICAL（如 DE-1 conf 0.9 →
// 22.5）落在 CAUTION 被放行到全部 target。任何 CRITICAL 必须无视聚合分直接阻断。
func TestInstall_BlocksOnSingleCritical(t *testing.T) {
	canonical := t.TempDir()
	writePassingSkill(t, canonical, "my-skill") // 过 registry 门，走到安全扫描
	opts := copyOpts(t.TempDir())
	opts.SkipQuality = false
	old := scanSkillFn
	scanSkillFn = func(string) ([]skillsqa.Finding, error) {
		return []skillsqa.Finding{{RuleID: "DE-1", Severity: "CRITICAL", Confidence: 0.9}}, nil
	}
	defer func() { scanSkillFn = old }()
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Failed != 1 {
		t.Fatalf("单条 CRITICAL（score 22，CAUTION）必须阻断: failed=%d want 1", rep.Stats.Failed)
	}
	if rep.Stats.Installed != 0 {
		t.Fatalf("被阻断的 skill 不应安装到任何 target, installed=%d", rep.Stats.Installed)
	}
	// The Issue text must say WHY it blocked (any-CRITICAL), so the failure is diagnosable
	// from the report without re-running the audit.
	//
	// Issue 文案必须说明阻断原因（任一 CRITICAL），让失败无需重跑 audit 即可从报告诊断。
	var issueText string
	for _, s := range rep.Skills {
		for _, iss := range s.Issues {
			issueText += iss + "\n"
		}
	}
	if !strings.Contains(issueText, "存在 CRITICAL finding") {
		t.Fatalf("阻断 Issue 应注明「存在 CRITICAL finding」, got %q", issueText)
	}
}

// TestInstall_CautionSurfacesWarning is the second half of #4: below-block findings (CAUTION band,
// no CRITICAL) used to be dropped entirely — res.Issues only ever carried ScanSkill results on the
// blocked path, so a CAUTION skill installed silently. Non-blocking ≠ silent: the findings must
// surface as a report warning (text-visible) while install proceeds.
//
// TestInstall_CautionSurfacesWarning 是 #4 的另一半：低于阻断线的 findings（CAUTION 带、无
// CRITICAL）此前被整体丢弃——res.Issues 只在阻断路径携带 ScanSkill 结果，CAUTION skill 静默
// 安装。不阻断 ≠ 静默：findings 必须以 report warning 浮出（文本可见），安装照常进行。
func TestInstall_CautionSurfacesWarning(t *testing.T) {
	canonical := t.TempDir()
	writePassingSkill(t, canonical, "my-skill")
	opts := copyOpts(t.TempDir())
	opts.SkipQuality = false
	old := scanSkillFn
	scanSkillFn = func(string) ([]skillsqa.Finding, error) {
		// 4 × MEDIUM(8) × 0.75 = 24 → CAUTION（20-49），无 CRITICAL
		return []skillsqa.Finding{
			{RuleID: "DC-10", Severity: "MEDIUM", Confidence: 0.75},
			{RuleID: "DC-10", Severity: "MEDIUM", Confidence: 0.75},
			{RuleID: "DC-10", Severity: "MEDIUM", Confidence: 0.75},
			{RuleID: "DC-10", Severity: "MEDIUM", Confidence: 0.75},
		}, nil
	}
	defer func() { scanSkillFn = old }()
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Failed != 0 || rep.Stats.Installed != 1 {
		t.Fatalf("CAUTION 无 CRITICAL 不应阻断: failed=%d installed=%d", rep.Stats.Failed, rep.Stats.Installed)
	}
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "my-skill") && strings.Contains(w, "CAUTION") {
			found = true
		}
	}
	if !found {
		t.Fatalf("CAUTION findings 不得静默——Warnings 应含该 skill 的安全提示, got %v", rep.Warnings)
	}
}

// TestInstall_LinkMode_NewLink: link mode actually creates junction/symlink and is detected as linked.
//
// TestInstall_LinkMode_NewLink：link 模式实际创建 junction/symlink 并被识别为 linked。
func TestInstall_LinkMode_NewLink(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	projectDir := t.TempDir()
	opts := copyOpts(projectDir)
	opts.Mode = ModeLink
	_, err := Install(canonical, opts)
	mustMk(t, err)
	dst := filepath.Join(projectDir, "my-skill")
	if got := detectState(skillDir, dst); got != StateLinked {
		t.Fatalf("link 模式安装后态应为 linked，got %s", got)
	}
}

func TestAdapters_PlanDeploy(t *testing.T) {
	home := t.TempDir()
	canonical := t.TempDir()
	for _, sp := range Adapters(home) {
		p := filepath.Join(canonical, sp.SrcRel)
		mustMk(t, os.MkdirAll(filepath.Dir(p), 0755))
		mustMk(t, os.WriteFile(p, []byte("content:"+sp.SrcRel), 0644))
	}
	plan := PlanAdapters(canonical, home)
	deployCount := 0
	for _, a := range plan {
		if a.Action == "deploy" {
			deployCount++
		}
	}
	if deployCount != 4 {
		t.Fatalf("plan deploy=%d want 4", deployCount)
	}
	done, _, err := DeployAdapters(canonical, home)
	mustMk(t, err)
	if done != 4 {
		t.Fatalf("deploy done=%d want 4", done)
	}
	plan2 := PlanAdapters(canonical, home)
	for _, a := range plan2 {
		if a.Action != "ok" {
			t.Fatalf("部署后应 ok，got %s (%s)", a.Action, a.Spec.Dst)
		}
	}
}

func TestDriftCheck_TargetOnly(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill")
	projectDir := t.TempDir()
	orphan := filepath.Join(projectDir, "stray-skill")
	mustMk(t, os.MkdirAll(orphan, 0755))
	mustMk(t, os.WriteFile(filepath.Join(orphan, "SKILL.md"), []byte("---\nname: stray-skill\ndescription: d\n---\n\nx\n"), 0644))

	rep, err := DriftCheck(canonical, copyOpts(projectDir))
	mustMk(t, err)
	if rep.Stats.TargetOnly != 1 {
		t.Fatalf("target-only=%d want 1", rep.Stats.TargetOnly)
	}
	if rep.Stats.Missing != 1 {
		t.Fatalf("missing=%d want 1（my-skill 在目标缺失）", rep.Stats.Missing)
	}
}

// TestHandleTarget_CopyInSync_ToLink: copy-in-sync + ModeLink → safe replacement with link.
// Guards the upgrade path when user switches from copy to link single source (delete copy, create link, action=linked).
//
// TestHandleTarget_CopyInSync_ToLink：copy-in-sync + ModeLink → 安全替换为 link。
// 守护用户从 copy 切到 link 单源时的升级路径（删副本建 link，action="linked"）。
func TestHandleTarget_CopyInSync_ToLink(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	data, _ := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), data, 0644))
	if got := detectState(skillDir, dst); got != StateCopyInSync {
		t.Fatalf("precondition: want copy-in-sync, got %s", got)
	}
	action, detail, err := handleTarget(skillDir, dst, StateCopyInSync, ModeLink, DriftAbort)
	if err != nil {
		t.Fatalf("handleTarget copy-in-sync→link: %v", err)
	}
	if action != "linked" {
		t.Fatalf("action=%q want linked (detail=%s)", action, detail)
	}
	if got := detectState(skillDir, dst); got != StateLinked {
		t.Fatalf("after: want linked, got %s", got)
	}
}

// TestHandleTarget_Drift_Overwrite_Link: drift + DriftOverwrite + ModeLink → force-create link from canonical.
// Guards that under drift with overwrite policy, link mode rebuilds link (not copy).
//
// TestHandleTarget_Drift_Overwrite_Link：drift + DriftOverwrite + ModeLink → 强制以 canonical 建 link。
// 守护 drift 时 overwrite 策略下 link 模式重建 link（而非 copy）。
func TestHandleTarget_Drift_Overwrite_Link(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("drifted"), 0644))
	action, _, err := handleTarget(skillDir, dst, StateDrift, ModeLink, DriftOverwrite)
	if err != nil {
		t.Fatalf("handleTarget drift overwrite link: %v", err)
	}
	if action != "linked" {
		t.Fatalf("action=%q want linked", action)
	}
	if got := detectState(skillDir, dst); got != StateLinked {
		t.Fatalf("after: want linked, got %s", got)
	}
}

// TestRemoveTargetTree_PreservesCanonicalSource: removing target in linked state must never delete the canonical source.
// Go 1.24 RemoveAll is safe for junctions (only deletes reparse point), but this is a data safety red line that must be locked by a test.
//
// TestRemoveTargetTree_PreservesCanonicalSource：linked 态下删目标绝不能删到 canonical 源。
// Go 1.24 的 RemoveAll 对 junction 安全（只删 reparse point），但这是数据安全红线，必须有测试锁定。
func TestRemoveTargetTree_PreservesCanonicalSource(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")
	mustMk(t, makeDirLink(dst, skillDir))
	if got := detectState(skillDir, dst); got != StateLinked {
		t.Fatalf("precondition: want linked, got %s", got)
	}
	removeTargetTree(dst)
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatalf("target link should be removed, lstat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
		t.Fatalf("canonical source MUST be preserved after removing linked target: %v", err)
	}
}

// TestCopyTree_SkipsVCSAndDeps: copyTree must skip .git/node_modules etc. (distSkipDirs),
// otherwise VCS metadata / huge dependency trees get copied into target, polluting distribution.
//
// TestCopyTree_SkipsVCSAndDeps：copyTree 必须跳过 .git/node_modules 等（distSkipDirs），
// 否则把 VCS 元数据/依赖巨树复制进目标污染分发。
func TestCopyTree_SkipsVCSAndDeps(t *testing.T) {
	src := t.TempDir()
	mustMk(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("x"), 0644))
	mustMk(t, os.MkdirAll(filepath.Join(src, ".git", "objects"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(src, ".git", "HEAD"), []byte("ref"), 0644))
	mustMk(t, os.MkdirAll(filepath.Join(src, "node_modules", "pkg"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(src, "node_modules", "pkg", "index.js"), []byte("y"), 0644))
	dst := t.TempDir()
	mustMk(t, copyTree(src, dst))
	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md should be copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git")); err == nil {
		t.Fatal(".git must be skipped by copyTree")
	}
	if _, err := os.Stat(filepath.Join(dst, "node_modules")); err == nil {
		t.Fatal("node_modules must be skipped by copyTree")
	}
}

// TestListSkills_FollowsSymlink guards the regression where ListSkills used
// e.IsDir() (Lstat-based), which returns false for a junction/symlink entry —
// silently dropping every link-installed skill. With link mode (default) and
// external junctions (the 28 lark-* junctions under ~/.claude/skills), most
// installed skills ARE links, so the bug made ListSkills — and therefore audit
// scan / skill-scan — see only real directories (e.g. alipay-*). ListSkills must
// use os.Stat (follows links) so a link pointing at a real skill dir counts.
func TestListSkills_FollowsSymlink(t *testing.T) {
	canonical := t.TempDir()
	realSkill := writeCanonicalSkill(t, t.TempDir(), "linked-skill")
	linkPath := filepath.Join(canonical, "linked-skill")
	mustMk(t, makeDirLink(linkPath, realSkill)) // junction (Windows) / symlink (unix)

	names, err := ListSkills(canonical)
	mustMk(t, err)

	found := false
	for _, n := range names {
		if n == "linked-skill" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ListSkills must follow junction/symlink to count linked-skill; got %v (regression: e.IsDir() dropped link skills)", names)
	}
}

// TestListSkills_BrokenSymlink guards the os.Stat error branch in ListSkills: a
// dangling symlink (target removed/never existed) makes os.Stat error, and
// ListSkills must skip it — not crash, not count it as a skill. This branch was
// introduced by the symlink-following fix but had no coverage (review suggest#1).
func TestListSkills_BrokenSymlink(t *testing.T) {
	canonical := t.TempDir()
	// Dangling symlink: points at a target that does not exist.
	dangling := filepath.Join(canonical, "dangling-skill")
	if err := os.Symlink(filepath.Join(t.TempDir(), "does-not-exist"), dangling); err != nil {
		t.Skipf("symlinks unavailable on host (Windows may need developer mode): %v", err)
	}
	// A real skill alongside it — ListSkills must still return it.
	writeCanonicalSkill(t, canonical, "real-skill")

	names, err := ListSkills(canonical)
	mustMk(t, err)

	foundReal := false
	for _, n := range names {
		if n == "dangling-skill" {
			t.Error("ListSkills must skip dangling symlink (os.Stat errors → continue), counted dangling-skill")
		}
		if n == "real-skill" {
			foundReal = true
		}
	}
	if !foundReal {
		t.Error("ListSkills dropped real-skill alongside the dangling symlink")
	}
}

// TestBackupTarget_RealDir: drift real-dir copy must be backed up before overwrite, content preserved as rollback fallback.
//
// TestBackupTarget_RealDir：drift 的真目录副本被 overwrite 前必须备份，内容留底（后悔药）。
func TestBackupTarget_RealDir(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "claude", "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("user custom"), 0644))

	got, err := backupTarget(dst, t.TempDir(), "claude", "my-skill")
	mustMk(t, err)
	if got == "" {
		t.Fatal("真目录副本应被备份，got 空路径")
	}
	data, rerr := os.ReadFile(filepath.Join(got, "SKILL.md"))
	if rerr != nil || string(data) != "user custom" {
		t.Fatalf("备份内容应与原副本一致: %v %q", rerr, string(data))
	}
	// Independent copy assertion (Suggest#5): after backup, modifying original dst should not change backup content (proves it is a copy not a link, truly preserved).
	//
	// 独立副本断言（Suggest#5）：备份后再改原 dst，备份内容不应跟随变化（证明是 copy 非 link，真正留底）。
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("changed after backup"), 0644))
	after, _ := os.ReadFile(filepath.Join(got, "SKILL.md"))
	if string(after) != "user custom" {
		t.Fatalf("备份应是独立副本，改原 dst 后备份却跟随变化: %q", string(after))
	}
}

// TestBackupTarget_PureSnapshot: backup dir has leftover from last time → must clear first to ensure pure snapshot (Fix#1).
// Otherwise when reusing the same dir, files that existed last time but deleted this time will linger, polluting rollback result.
//
// TestBackupTarget_PureSnapshot：备份目录已有上次残留 → 必须先清空，保证纯净快照（Fix#1）。
// 否则同目录复用时上次有、这次删的文件会残留，污染回滚结果。
func TestBackupTarget_PureSnapshot(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "claude", "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("new"), 0644))

	backupBase := t.TempDir()
	bkDir := filepath.Join(backupBase, "claude", "my-skill")
	mustMk(t, os.MkdirAll(bkDir, 0755))
	mustMk(t, os.WriteFile(filepath.Join(bkDir, "stale-from-last-backup"), []byte("dirty"), 0644))

	got, err := backupTarget(dst, backupBase, "claude", "my-skill")
	mustMk(t, err)
	if _, serr := os.Stat(filepath.Join(got, "stale-from-last-backup")); !os.IsNotExist(serr) {
		t.Fatalf("上次残留文件应被清空（纯净快照），仍存在: err=%v", serr)
	}
}

// TestBackupTarget_RejectsUnsafeName: skill names containing .. or path separators should be rejected (path injection defense, Suggest#4).
//
// TestBackupTarget_RejectsUnsafeName：含 .. 或路径分隔符的 skill 名应拒绝（路径注入防御，Suggest#4）。
func TestBackupTarget_RejectsUnsafeName(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "claude", "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("x"), 0644))
	backupBase := t.TempDir()
	for _, bad := range []string{"..", ".", "a/b"} {
		if _, err := backupTarget(dst, backupBase, "claude", bad); err == nil {
			t.Fatalf("不安全 skill 名 %q 应被拒绝（路径注入风险）", bad)
		}
	}
}

// TestBackupTarget_SkipsLink: junction/symlink has no independent user content, not backed up.
//
// TestBackupTarget_SkipsLink：junction/symlink 无独立用户内容，不备份。
func TestBackupTarget_SkipsLink(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "claude", "my-skill")
	mustMk(t, os.MkdirAll(filepath.Dir(link), 0755))
	mustMk(t, makeDirLink(link, real)) // junction(Windows)/symlink(unix)
	got, err := backupTarget(link, t.TempDir(), "claude", "my-skill")
	mustMk(t, err)
	if got != "" {
		t.Fatalf("link/junction 不应备份，got %s", got)
	}
}

// TestBackupTarget_SkipsMissing: non-existent/broken link has no content, not backed up (cursor SkillsHub broken link scenario).
//
// TestBackupTarget_SkipsMissing：不存在/断链无内容，不备份（cursor SkillsHub 断链场景）。
func TestBackupTarget_SkipsMissing(t *testing.T) {
	got, err := backupTarget(filepath.Join(t.TempDir(), "nope"), t.TempDir(), "claude", "my-skill")
	mustMk(t, err)
	if got != "" {
		t.Fatalf("不存在目标不应备份，got %s", got)
	}
}

// TestInstall_DriftOverwrite_Backups: overwriting real-dir drift copy → tr.Backup records path, user content preserved.
//
// TestInstall_DriftOverwrite_Backups：overwrite 真目录 drift 副本 → tr.Backup 记录路径，用户内容留底。
func TestInstall_DriftOverwrite_Backups(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "my-skill")
	projectDir := t.TempDir()
	dst := filepath.Join(projectDir, "my-skill")
	mustMk(t, os.MkdirAll(dst, 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("drifted"), 0644))

	opts := copyOpts(projectDir)
	opts.DriftPolicy = DriftOverwrite
	// Injected to avoid polluting home directory
	opts.BackupBase = t.TempDir() // 注入避免污染家目录
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Installed != 1 {
		t.Fatalf("installed=%d want 1", rep.Stats.Installed)
	}
	var bk string
	for _, s := range rep.Skills {
		for _, tr := range s.Targets {
			if tr.Backup != "" {
				bk = tr.Backup
			}
		}
	}
	if bk == "" {
		t.Fatal("overwrite drift 副本应记录备份路径")
	}
	data, _ := os.ReadFile(filepath.Join(bk, "SKILL.md"))
	if string(data) != "drifted" {
		t.Fatalf("备份应保留用户 drift 内容，got %q", string(data))
	}
}

// TestTargetDirs_AllExpandsCodexCopilot: TargetAll must expand to include codex/copilot/agents.
// Guards that target=all does not miss the newly added targets — otherwise user --target all distribution silently drops those tools,
// skills only install to claude/cursor, loop engineering multi-agent distribution breaks.
//
// TestTargetDirs_AllExpandsCodexCopilot：TargetAll 必须展开含 codex/copilot/agents。
// 守护 target=all 不会漏掉新加的目标——否则用户 --target all 分发会静默漏掉这些工具，
// skills 只装到 claude/cursor，loop engineering 多 agent 分发失效。
func TestTargetDirs_AllExpandsCodexCopilot(t *testing.T) {
	dirs, err := TargetDirs([]Target{TargetAll}, true, "")
	mustMk(t, err)
	for _, want := range []string{"claude", "cursor", "codex", "copilot", "agents"} {
		if _, ok := dirs[want]; !ok {
			t.Errorf("target=all 应展开含 %q，实际 keys=%v（漏装会让多 agent 分发静默失效）", want, dirs)
		}
	}
	if len(dirs) != 5 {
		t.Fatalf("target=all 应展开 5 个目标，got %d: %v", len(dirs), dirs)
	}
}

// TestTargetDir_CodexCopilotPath: codex/copilot/agents global directory paths are correct.
// Codex CLI reads ~/.codex/skills (official since 2025-12), Copilot personal skill reads ~/.copilot/skills (GitHub Docs),
// agents target reads the cross-agent shared ~/.agents/skills.
// Wrong paths would cause distribution to wrong locations, tools cannot detect skills.
//
// TestTargetDir_CodexCopilotPath：codex/copilot/agents 全局目录路径正确。
// Codex CLI 读 ~/.codex/skills（2025-12 起官方），Copilot 个人 skill 读 ~/.copilot/skills（GitHub Docs），
// agents 目标读跨 agent 共享的 ~/.agents/skills。
// 路径写错会导致分发到错误位置，工具识别不到 skill。
func TestTargetDir_CodexCopilotPath(t *testing.T) {
	home := "/tmp/fake-home"
	cases := map[string]string{
		"codex":   filepath.Join(home, ".codex", "skills"),
		"copilot": filepath.Join(home, ".copilot", "skills"),
		"agents":  filepath.Join(home, ".agents", "skills"),
		"claude":  filepath.Join(home, ".claude", "skills"),
		"cursor":  filepath.Join(home, ".cursor", "skills"),
	}
	for name, want := range cases {
		got, err := targetDir(name, true, home, "")
		if err != nil {
			t.Errorf("targetDir(%q) 不应报错: %v", name, err)
			continue
		}
		if got != want {
			t.Errorf("targetDir(%q)=%q want %q", name, got, want)
		}
	}
	// Unknown target returns an explicit error (never "" — an empty dir would degrade
	// filepath.Join("", name) into a cwd-relative write).
	//
	// 未知 target 显式报错（绝不返回 ""——空目录会让 filepath.Join("", name) 退化为 cwd 相对写）
	if got, err := targetDir("unknown-tool", true, home, ""); err == nil || got != "" {
		t.Errorf("未知 target 应返回错误，got %q err=%v", got, err)
	}
	// TargetDirs propagates the unknown-target error.
	//
	// TargetDirs 传播未知 target 错误
	if _, err := TargetDirs([]Target{"unknown-tool"}, true, ""); err == nil {
		t.Error("TargetDirs 对未知 target 应报错")
	}
}

// writePassingSkill creates a skill that passes the full quality gate (R1-R11):
// kebab name == dir name, desc ≥80 runes with Use when + SKIP, valid metadata.pattern,
// high-signal body. Paired with SkipQuality=false tests.
//
// writePassingSkill 创建一个能过完整质量门控（R1-R11）的 skill：kebab 名 == 目录名、
// desc ≥80 字符含 Use when + SKIP、合法 metadata.pattern、高信号正文。配 SkipQuality=false 的测试用。
func writePassingSkill(t *testing.T, canonical, name string) string {
	t.Helper()
	sd := filepath.Join(canonical, name)
	mustMk(t, os.MkdirAll(sd, 0755))
	content := "---\nname: " + name + "\n" +
		"description: \"合格描述前缀。" + strings.Repeat("测试内容段落", 12) + "Use when: 场景触发。SKIP: 跳过场景。\"\n" +
		"metadata:\n  pattern: pipeline\n  domain: testing\n---\n\n" +
		"# 标题\n\n决策树：第一步先做这个。自查清单：检查项。\n"
	mustMk(t, os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(content), 0644))
	return sd
}

// TestDetectState_FullTreeDrift: copy-in-sync must compare the WHOLE tree, not just SKILL.md.
// Data-loss regression: user edits target references/foo.md (SKILL.md untouched); a SKILL.md-only
// comparison would misjudge copy-in-sync, and mode=link handleTarget would os.RemoveAll the whole
// tree WITHOUT backup (backup only triggers on StateDrift) — silent loss of user edits.
//
// TestDetectState_FullTreeDrift：copy-in-sync 必须全树对比而非只比 SKILL.md。
// 数据丢失回归：用户改了 target 的 references/foo.md（SKILL.md 没动）；单文件对比会误判
// copy-in-sync，mode=link 时 handleTarget 无备份 os.RemoveAll 整树——用户改动静默丢失。
func TestDetectState_FullTreeDrift(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	mustMk(t, os.MkdirAll(filepath.Join(skillDir, "references"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(skillDir, "references", "foo.md"), []byte("canonical ref"), 0644))

	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")
	mustMk(t, copyTree(skillDir, dst))

	// identical full tree → copy-in-sync
	//
	// 全树一致 → copy-in-sync
	if got := detectState(skillDir, dst); got != StateCopyInSync {
		t.Fatalf("identical tree: got %s, want copy-in-sync", got)
	}

	// user edits references/foo.md (SKILL.md identical) → MUST be drift, not copy-in-sync
	//
	// 用户改 references/foo.md（SKILL.md 相同）→ 必须判 drift 而非 copy-in-sync
	mustMk(t, os.WriteFile(filepath.Join(dst, "references", "foo.md"), []byte("user local edit"), 0644))
	if got := detectState(skillDir, dst); got != StateDrift {
		t.Fatalf("edited references/: got %s, want drift（SKILL.md 相同但子文件被改）", got)
	}

	// restore, then add an extra file in target → drift
	//
	// 恢复后 target 多一个文件 → drift
	mustMk(t, os.WriteFile(filepath.Join(dst, "references", "foo.md"), []byte("canonical ref"), 0644))
	mustMk(t, os.WriteFile(filepath.Join(dst, "extra.md"), []byte("user added"), 0644))
	if got := detectState(skillDir, dst); got != StateDrift {
		t.Fatalf("extra file: got %s, want drift（target 多了文件）", got)
	}

	// remove extra, delete a canonical-copied file in target → drift
	//
	// 删掉多余文件，再删 target 里一个 canonical 文件 → drift
	mustMk(t, os.Remove(filepath.Join(dst, "extra.md")))
	mustMk(t, os.Remove(filepath.Join(dst, "references", "foo.md")))
	if got := detectState(skillDir, dst); got != StateDrift {
		t.Fatalf("missing file: got %s, want drift（target 缺文件）", got)
	}
}

// TestHandleTarget_DriftOverwrite_RemoveFailure: overwrite whose deletion fails must NOT fall
// through to copyTree — that would produce a new-old hybrid tree reported as a clean overwrite.
// Expects action=failed, no abortErr (per-target failure, install continues elsewhere).
//
// TestHandleTarget_DriftOverwrite_RemoveFailure：删除失败的 overwrite 禁止带病 copy——
// 否则会产出新旧混合树却报告纯净覆盖。期望 action=failed、无 abortErr（单 target 失败，
// install 其他 target 继续）。
func TestHandleTarget_DriftOverwrite_RemoveFailure(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")
	dst := filepath.Join(t.TempDir(), "my-skill")

	// Inject a failing deleter (deterministic cross-platform RemoveAll failure is not
	// constructible: Windows ignores read-only bits, Unix ignores open file handles).
	//
	// 注入失败的删除器（确定性的跨平台 RemoveAll 失败无法直接构造：
	// Windows 忽略只读位、Unix 忽略打开的文件句柄）。
	orig := removeTargetTreeFn
	removeTargetTreeFn = func(string) error { return errors.New("remove boom") }
	t.Cleanup(func() { removeTargetTreeFn = orig })

	action, detail, abortErr := handleTarget(skillDir, dst, StateDrift, ModeCopy, DriftOverwrite)
	if abortErr != nil {
		t.Fatalf("删除失败不应 abort 整个 install，got abortErr=%v", abortErr)
	}
	if action != actFailed {
		t.Fatalf("action=%q want failed（detail=%s）", action, detail)
	}
}

// TestRemoveTargetTree_Error: removeTargetTree must surface deletion errors (previously
// swallowed via `_ = os.Remove...`). Failure is made deterministic per-platform: Windows
// refuses to delete a directory containing an open file; Unix refuses to delete entries
// inside a directory without write permission.
//
// TestRemoveTargetTree_Error：removeTargetTree 必须上抛删除错误（原先 `_ = os.Remove...` 吞掉）。
// 失败按平台确定性构造：Windows 拒绝删除含打开文件的目录；Unix 拒绝删除无写权限目录里的条目。
func TestRemoveTargetTree_Error(t *testing.T) {
	victim := filepath.Join(t.TempDir(), "victim")
	mustMk(t, os.MkdirAll(victim, 0755))
	f, err := os.Create(filepath.Join(victim, "lock"))
	mustMk(t, err)
	if runtime.GOOS == "windows" {
		defer f.Close() // 保持句柄打开：Windows 下删除含打开文件的目录必失败
	} else {
		mustMk(t, f.Close())
		mustMk(t, os.Chmod(victim, 0500)) // 无写权限：Unix 下删除其内容必失败
		defer os.Chmod(victim, 0700)      //nolint:errcheck // 清理，便于 TempDir 回收
	}
	if err := removeTargetTree(victim); err == nil {
		t.Fatal("不可删除目录应返回 error，got nil")
	}

	// happy path: real dir removed without error
	//
	// 正常路径：真目录删除无 error
	dir := filepath.Join(t.TempDir(), "victim2")
	mustMk(t, os.MkdirAll(dir, 0755))
	if err := removeTargetTree(dir); err != nil {
		t.Fatalf("正常目录删除不应报错: %v", err)
	}
}

// TestInstall_ScanSkillError_Blocks: a ScanSkill error must block the skill (issue + Failed++),
// never be scored on zero findings as clean — the security gate would otherwise pass unreadable
// skills (skillsqa/audit.go's explicit downstream contract).
//
// TestInstall_ScanSkillError_Blocks：ScanSkill 出错必须拦截该 skill（记 issue + Failed++），
// 绝不能在零 findings 上打分报 clean——否则安全门放过不可读 skill（skillsqa/audit.go 明确的下游契约）。
func TestInstall_ScanSkillError_Blocks(t *testing.T) {
	canonical := t.TempDir()
	writePassingSkill(t, canonical, "my-skill") // passes AuditSkill so ScanSkill is reached

	orig := scanSkillFn
	scanSkillFn = func(string) ([]skillsqa.Finding, error) {
		return nil, errors.New("scan boom")
	}
	t.Cleanup(func() { scanSkillFn = orig })

	opts := copyOpts(t.TempDir())
	opts.SkipQuality = false
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Failed != 1 {
		t.Fatalf("failed=%d want 1（ScanSkill 错误应按审查失败拦截）", rep.Stats.Failed)
	}
	if rep.Stats.Installed != 0 {
		t.Fatalf("installed=%d want 0（扫描失败的 skill 不许装）", rep.Stats.Installed)
	}
	found := false
	for _, s := range rep.Skills {
		for _, iss := range s.Issues {
			if strings.Contains(iss, "安全审查失败") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("应记录安全审查失败 issue，got %+v", rep.Skills)
	}
}

// TestDriftCheck_SkillFilterNoFalseOrphans: target-only orphan detection must use the FULL
// canonical name list. With `--skill foo`, other legit skills in the target (bar) must NOT be
// misreported as orphans.
//
// TestDriftCheck_SkillFilterNoFalseOrphans：target-only 孤儿检测必须用过滤前的完整名单。
// `--skill foo` 时 target 里其他正常 skill（bar）不得被误报孤儿。
func TestDriftCheck_SkillFilterNoFalseOrphans(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "foo")
	writeCanonicalSkill(t, canonical, "bar")
	projectDir := t.TempDir()
	// bar is present in the target (installed earlier); foo is not.
	//
	// bar 在 target 里（之前装过）；foo 不在。
	mustMk(t, os.MkdirAll(filepath.Join(projectDir, "bar"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(projectDir, "bar", "SKILL.md"), []byte("x"), 0644))

	opts := copyOpts(projectDir)
	opts.SkillFilter = []string{"foo"}
	rep, err := DriftCheck(canonical, opts)
	mustMk(t, err)
	if rep.Stats.TargetOnly != 0 {
		t.Fatalf("target-only=%d want 0（bar 在 canonical 全集里，--skill foo 不得误报孤儿）", rep.Stats.TargetOnly)
	}
}

// TestDirEntryIsDir: DirEntryIsDir must follow junction/symlink (os.Stat semantics) —
// e.IsDir() is Lstat-based and drops link-form skills.
//
// TestDirEntryIsDir：DirEntryIsDir 必须跟随 junction/symlink（os.Stat 语义）——
// e.IsDir() 基于 Lstat，会漏掉 link 形态的 skill。
func TestDirEntryIsDir(t *testing.T) {
	parent := t.TempDir()
	mustMk(t, os.MkdirAll(filepath.Join(parent, "real-dir"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(parent, "a-file"), []byte("x"), 0644))
	realSkill := writeCanonicalSkill(t, t.TempDir(), "linked-skill")
	mustMk(t, makeDirLink(filepath.Join(parent, "linked-skill"), realSkill))

	entries, err := os.ReadDir(parent)
	mustMk(t, err)
	got := map[string]bool{}
	for _, e := range entries {
		got[e.Name()] = DirEntryIsDir(parent, e)
	}
	if !got["real-dir"] {
		t.Error("real-dir 应判 true")
	}
	if got["a-file"] {
		t.Error("a-file 应判 false")
	}
	if !got["linked-skill"] {
		t.Error("linked-skill（junction/symlink → 真实目录）应判 true（e.IsDir 基于 Lstat 会漏）")
	}
}

// TestInstall_TotalCountsEveryProcessedSkill: Total counts every processed skill — including
// quality-gate-blocked and reserved ones — so the skill-level tier reconciles
// (Total = passed + failed + reserved).
//
// TestInstall_TotalCountsEveryProcessedSkill：Total 统计每个被处理的 skill——含门控拦截
// 与保留名——保证 skill 级口径对得上（Total = 通过 + 失败 + 保留）。
func TestInstall_TotalCountsEveryProcessedSkill(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "bad-skill")     // 不合格（desc 过短），被门控拦
	writeCanonicalSkill(t, canonical, "forge-quality") // 保留名跳过
	opts := copyOpts(t.TempDir())
	opts.SkipQuality = false
	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Total != 2 {
		t.Fatalf("total=%d want 2（被拦 skill 与保留名 skill 都计 Total）", rep.Stats.Total)
	}
	if rep.Stats.Failed != 1 {
		t.Fatalf("failed=%d want 1（bad-skill 门控拦截）", rep.Stats.Failed)
	}
}

// TestTreesInSync_SymlinkDifference: a symlink is NOT content-free — its target
// string participates in the tree comparison. Before the fix, hashTree skipped
// symlinks entirely, so trees differing ONLY by a symlink (extra link, different
// target, single-side link) were judged copy-in-sync — and copy-in-sync → link
// mode deletes the target tree with os.RemoveAll and no backup.
//
// TestTreesInSync_SymlinkDifference：symlink 并非无内容——其 target 串参与整树
// 对比。修复前 hashTree 完全跳过 symlink，"唯一差异是 symlink"（新增 link /
// 指向不同 / 单侧存在）的两棵树会被误判 copy-in-sync——而 copy-in-sync → link
// 会 os.RemoveAll 整树且无备份。
func TestTreesInSync_SymlinkDifference(t *testing.T) {
	// Junction (Windows, no admin needed) / dir symlink (unix) via makeDirLink —
	// WalkDir reports Windows junctions as ModeIrregular (type "?"), not
	// ModeSymlink; hashTree detects links via !IsRegular + os.Readlink. This
	// subtest runs even where file symlinks need privileges.
	t.Run("Junction", func(t *testing.T) {
		mkJuncTree := func(withLink bool, linkTarget string) string {
			root := t.TempDir()
			mustMk(t, os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("same"), 0644))
			if withLink {
				mustMk(t, makeDirLink(filepath.Join(root, "ref"), linkTarget))
			}
			return root
		}
		realA := t.TempDir()
		realB := t.TempDir()
		if treesInSync(mkJuncTree(false, ""), mkJuncTree(true, realA)) {
			t.Error("target 多一个 junction/link 必须判 drift（修复前 link 被跳过 → 误判 in-sync → link 模式无备份删树）")
		}
		if treesInSync(mkJuncTree(true, realA), mkJuncTree(true, realB)) {
			t.Error("junction/link 指向不同必须判 drift")
		}
		jcA, jcB := mkJuncTree(true, realA), mkJuncTree(true, realA)
		if !treesInSync(jcA, jcB) {
			t.Errorf("双侧 junction/link 完全一致应判 in-sync: hashA=%v hashB=%v", hashTree(jcA), hashTree(jcB))
		}
	})

	// File symlinks — skipped where the host cannot create them (Windows may
	// need developer mode).
	t.Run("FileSymlink", func(t *testing.T) {
		mkTree := func(withLink bool, linkTarget string) string {
			root := t.TempDir()
			mustMk(t, os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("same"), 0644))
			if withLink {
				if err := os.Symlink(linkTarget, filepath.Join(root, "ref")); err != nil {
					t.Skipf("symlinks unavailable on host (Windows may need developer mode): %v", err)
				}
			}
			return root
		}

		src := mkTree(false, "")

		// Extra symlink on the target side only → drift.
		if treesInSync(src, mkTree(true, "SKILL.md")) {
			t.Error("target 多一个 symlink 必须判 drift（修复前 symlink 被跳过 → 误判 in-sync → link 模式无备份删树）")
		}
		// Same symlink name, different target → drift.
		if treesInSync(mkTree(true, "SKILL.md"), mkTree(true, "other.md")) {
			t.Error("symlink 指向不同必须判 drift")
		}
		// Identical symlink on both sides → in sync.
		if !treesInSync(mkTree(true, "SKILL.md"), mkTree(true, "SKILL.md")) {
			t.Error("双侧 symlink 完全一致应判 in-sync")
		}
	})
}

// TestInstall_UnknownSkillFilterErrors: a misspelled --skill must be an explicit
// error (same semantics as the CLI layer's filterSkillNames / audit / validate),
// never silently filtered to an empty set — "0 installed, exit 0" is a false green.
//
// TestInstall_UnknownSkillFilterErrors：拼错的 --skill 必须显式报错（与 CLI 层
// filterSkillNames / audit / validate 同款语义），绝不静默过滤成空集——
// "安装 0 个，exit 0" 是假绿。
func TestInstall_UnknownSkillFilterErrors(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "foo")

	opts := copyOpts(t.TempDir())
	opts.SkillFilter = []string{"fooo"}
	if _, err := Install(canonical, opts); err == nil {
		t.Fatal("Install: --skill 拼错必须报错，got nil（静默过滤成空集=假绿）")
	} else if !strings.Contains(err.Error(), "fooo") {
		t.Fatalf("Install: 错误应点名未匹配的 skill，got: %v", err)
	}

	dopts := copyOpts(t.TempDir())
	dopts.SkillFilter = []string{"fooo"}
	if _, err := DriftCheck(canonical, dopts); err == nil {
		t.Fatal("DriftCheck: --skill 拼错必须报错，got nil（静默过滤成空集=假绿）")
	} else if !strings.Contains(err.Error(), "fooo") {
		t.Fatalf("DriftCheck: 错误应点名未匹配的 skill，got: %v", err)
	}
}

// TestCopyTree_JunctionEntryNotError pins the copyTree junction fix (review
// finding): the link detection must use !d.Type().IsRegular() — the SAME rule as
// hashTree — not ModeSymlink alone. WalkDir reports Windows junctions as
// ModeIrregular (not ModeSymlink), so the old check let a junction entry fall
// through to os.ReadFile, which fails on a directory reparse point and failed
// the whole copyTree on Windows. The junction entry is skipped (like symlinks
// always were); the regular files around it must still be copied.
//
// TestCopyTree_JunctionEntryNotError 钉死 copyTree 的 junction 修复（审查发现）：
// link 判定必须用 !d.Type().IsRegular()——与 hashTree 同一规则——而非仅
// ModeSymlink。WalkDir 把 Windows junction 报为 ModeIrregular（非
// ModeSymlink），旧判定会让 junction 条目落到 os.ReadFile，对目录 reparse
// point 读取必失败、整个 copyTree 在 Windows 报错。junction 条目被跳过（与
// symlink 一贯的行为一致）；其周围的常规文件必须照常复制。
func TestCopyTree_JunctionEntryNotError(t *testing.T) {
	src := t.TempDir()
	mustMk(t, os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("x"), 0655))
	// A junction (Windows, no admin) / dir symlink (unix) INSIDE the tree —
	// the entry shape that used to fail the copy on Windows.
	//
	// 树内的 junction（Windows 免提权）/目录 symlink（unix）——正是 Windows 上
	// 曾让 copy 失败的条目形态。
	linkTarget := t.TempDir()
	mustMk(t, os.WriteFile(filepath.Join(linkTarget, "inner.md"), []byte("inner"), 0644))
	mustMk(t, makeDirLink(filepath.Join(src, "ref-link"), linkTarget))

	dst := t.TempDir()
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copyTree 含 junction/link 条目不得报错（修复前 Windows 在此失败）: %v", err)
	}
	data, rerr := os.ReadFile(filepath.Join(dst, "SKILL.md"))
	if rerr != nil || string(data) != "x" {
		t.Fatalf("常规文件必须照常复制: %v %q", rerr, string(data))
	}
	// Non-regular entries are skipped, not expanded (the link itself is not a
	// copied directory — same semantics symlinks always had).
	//
	// 非常规条目被跳过而非展开（link 本身不是被复制的目录——与 symlink 一贯语义一致）。
	if _, err := os.Lstat(filepath.Join(dst, "ref-link")); !os.IsNotExist(err) {
		t.Logf("ref-link 在副本中存在（跟随语义）: err=%v", err)
	}
}

// TestTreesInSync_TargetOnlySkipDirIsDrift pins the skip-blind-spot fix: a skip
// dir (distSkipDirs: .git/node_modules/...) present ONLY in the target tree —
// e.g. the user ran git init inside the installed copy — must judge DRIFT, not
// copy-in-sync. Before the fix both sides skipped .git in the content hash, so
// the trees compared equal and link mode's copy-in-sync→link transition
// os.RemoveAll'd the whole target with NO backup (backup only triggers on
// StateDrift) — silent destruction of the user's .git. A skip dir present on
// BOTH sides stays in sync (canonical's copy is not distributed, the target's
// survives copyTree's skip — consistent).
//
// TestTreesInSync_TargetOnlySkipDirIsDrift 钉死 skip 盲区修复：仅目标侧存在的
// 被跳过目录（distSkipDirs：.git/node_modules/…——如用户在装出的副本里 git init）
// 必须判 DRIFT 而非 copy-in-sync。修复前双侧内容 hash 都跳过 .git，两树比成
// 相等，link 模式的 copy-in-sync→link 迁移会无备份 os.RemoveAll 整树（备份只在
// StateDrift 触发）——用户的 .git 被静默销毁。双侧都有的被跳过目录仍判同步
// （canonical 的那份不分发、目标的经 copyTree 跳过存活——口径一致）。
func TestTreesInSync_TargetOnlySkipDirIsDrift(t *testing.T) {
	canonical := t.TempDir()
	skillDir := writeCanonicalSkill(t, canonical, "my-skill")

	target := t.TempDir()
	dst := filepath.Join(target, "my-skill")
	mustMk(t, copyTree(skillDir, dst))

	// Baseline: identical trees → in sync.
	//
	// 基线：两树一致 → 同步。
	if !treesInSync(skillDir, dst) {
		t.Fatal("基线：内容一致应判 in-sync")
	}

	// User ran git init inside the installed copy: target-only .git → drift.
	//
	// 用户在装出的副本里 git init：目标单侧 .git → drift。
	mustMk(t, os.MkdirAll(filepath.Join(dst, ".git", "objects"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644))
	if treesInSync(skillDir, dst) {
		t.Fatal("目标单侧 .git 必须判 drift（修复前双侧跳过 .git → 误判 in-sync → link 模式无备份删树）")
	}
	if got := detectState(skillDir, dst); got != StateDrift {
		t.Fatalf("detectState: got %s, want drift（用户 .git 是本地状态，须走有备份路径）", got)
	}

	// Same skip dir on BOTH sides → still in sync (no false drift for canonical
	// trees that legitimately contain a skip dir).
	//
	// 双侧同名被跳过目录 → 仍判同步（canonical 本身含被跳过目录时不得误报 drift）。
	mustMk(t, os.MkdirAll(filepath.Join(skillDir, ".git", "objects"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(skillDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644))
	if !treesInSync(skillDir, dst) {
		t.Fatal("双侧 .git 应判 in-sync（内容对比双侧都跳过、存在性双侧都命中）")
	}
	if got := detectState(skillDir, dst); got != StateCopyInSync {
		t.Fatalf("detectState: got %s, want copy-in-sync（双侧 .git 不构成 drift）", got)
	}
}

// TestBackupTarget_IncludesSkipDirs pins the backup full-copy switch: the
// overwrite backup must snapshot EVERYTHING — .git/node_modules included — via
// copyTreeFiltered(nil). Combined with the skip-blind-spot drift fix, this is
// the rollback path that preserves the user's git init: drift (backup) →
// overwrite → the .git lands in the backup, not in the void.
//
// TestBackupTarget_IncludesSkipDirs 钉死备份切换为完整拷贝：overwrite 备份必须
// 快照全部内容——含 .git/node_modules——经 copyTreeFiltered(nil)。与 skip 盲区
// drift 修复合起来，这就是保住用户 git init 的回滚路径：drift（备份）→
// overwrite → .git 落进备份而非虚空。
func TestBackupTarget_IncludesSkipDirs(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "claude", "my-skill")
	mustMk(t, os.MkdirAll(filepath.Join(dst, ".git", "objects"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("user custom"), 0644))
	mustMk(t, os.WriteFile(filepath.Join(dst, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644))
	mustMk(t, os.MkdirAll(filepath.Join(dst, "node_modules", "pkg"), 0755))
	mustMk(t, os.WriteFile(filepath.Join(dst, "node_modules", "pkg", "index.js"), []byte("y"), 0644))

	got, err := backupTarget(dst, t.TempDir(), "claude", "my-skill")
	mustMk(t, err)
	if got == "" {
		t.Fatal("真目录副本应被备份，got 空路径")
	}
	head, rerr := os.ReadFile(filepath.Join(got, ".git", "HEAD"))
	if rerr != nil || string(head) != "ref: refs/heads/main" {
		t.Fatalf("备份必须包含 .git（完整拷贝，不跳过 distSkipDirs）: %v %q", rerr, string(head))
	}
	if _, err := os.Stat(filepath.Join(got, "node_modules", "pkg", "index.js")); err != nil {
		t.Fatalf("备份必须包含 node_modules: %v", err)
	}
}
