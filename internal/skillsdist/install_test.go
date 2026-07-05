package skillsdist

import (
	"os"
	"path/filepath"
	"testing"
)

func mustMk(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

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

	// copy-in-sync：拷贝 SKILL.md 内容一致
	mustMk(t, os.MkdirAll(dst, 0755))
	data, _ := os.ReadFile(canonSkillMD)
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), data, 0644))
	if got := detectState(skillDir, dst); got != StateCopyInSync {
		t.Fatalf("copy-in-sync: got %s", got)
	}

	// drift：改目标 SKILL.md
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("---\nname: my-skill\ndescription: drifted\n---\n\nother\n"), 0644))
	if got := detectState(skillDir, dst); got != StateDrift {
		t.Fatalf("drift: got %s", got)
	}
}

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
	writeCanonicalSkill(t, canonical, "forge-pipeline")
	projectDir := t.TempDir()
	rep, err := Install(canonical, copyOpts(projectDir))
	mustMk(t, err)
	if rep.Stats.Installed != 0 {
		t.Fatalf("reserved name 不应安装，installed=%d", rep.Stats.Installed)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "forge-pipeline")); err == nil {
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

func TestRoutes_Match(t *testing.T) {
	routes := []Route{{Match: []string{"feishu", "lark"}, Skill: "lark-router", Reason: "飞书路由"}}
	if got := MatchRoute(routes, "看看 my.feishu.cn 文档"); got != "lark-router" {
		t.Fatalf("feishu 命中: got %q", got)
	}
	if got := MatchRoute(routes, "nothing here"); got != "" {
		t.Fatalf("无命中应返回空串: got %q", got)
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
	// 独立副本断言（Suggest#5）：备份后再改原 dst，备份内容不应跟随变化（证明是 copy 非 link，真正留底）。
	mustMk(t, os.WriteFile(filepath.Join(dst, "SKILL.md"), []byte("changed after backup"), 0644))
	after, _ := os.ReadFile(filepath.Join(got, "SKILL.md"))
	if string(after) != "user custom" {
		t.Fatalf("备份应是独立副本，改原 dst 后备份却跟随变化: %q", string(after))
	}
}

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

// TestBackupTarget_SkipsMissing：不存在/断链无内容，不备份（cursor SkillsHub 断链场景）。
func TestBackupTarget_SkipsMissing(t *testing.T) {
	got, err := backupTarget(filepath.Join(t.TempDir(), "nope"), t.TempDir(), "claude", "my-skill")
	mustMk(t, err)
	if got != "" {
		t.Fatalf("不存在目标不应备份，got %s", got)
	}
}

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

// TestTargetDirs_AllExpandsCodexCopilot：TargetAll 必须展开含 codex 和 copilot。
// 守护 target=all 不会漏掉新加的 codex/copilot 目标——否则用户 --target all 分发会静默漏掉这两个工具，
// skills 只装到 claude/cursor/pi，loop engineering 多 agent 分发失效。
func TestTargetDirs_AllExpandsCodexCopilot(t *testing.T) {
	dirs, err := TargetDirs([]Target{TargetAll}, true, "")
	mustMk(t, err)
	for _, want := range []string{"claude", "cursor", "codex", "copilot"} {
		if _, ok := dirs[want]; !ok {
			t.Errorf("target=all 应展开含 %q，实际 keys=%v（codex/copilot 漏装会让多 agent 分发静默失效）", want, dirs)
		}
	}
	if len(dirs) != 4 {
		t.Fatalf("target=all 应展开 4 个目标，got %d: %v", len(dirs), dirs)
	}
}

// TestTargetDir_CodexCopilotPath：codex/copilot 全局目录路径正确。
// Codex CLI 读 ~/.codex/skills（2025-12 起官方），Copilot 个人 skill 读 ~/.copilot/skills（GitHub Docs）。
// 路径写错会导致分发到错误位置，工具识别不到 skill。
func TestTargetDir_CodexCopilotPath(t *testing.T) {
	home := "/tmp/fake-home"
	cases := map[string]string{
		"codex":   filepath.Join(home, ".codex", "skills"),
		"copilot": filepath.Join(home, ".copilot", "skills"),
		"claude":  filepath.Join(home, ".claude", "skills"),
		"cursor":  filepath.Join(home, ".cursor", "skills"),
	}
	for name, want := range cases {
		if got := targetDir(name, true, home, ""); got != want {
			t.Errorf("targetDir(%q)=%q want %q", name, got, want)
		}
	}
	// 未知 target 返回空串（不 panic、不误写到默认位置）
	if got := targetDir("unknown-tool", true, home, ""); got != "" {
		t.Errorf("未知 target 应返回空串，got %q", got)
	}
}
