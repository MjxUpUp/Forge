package skillsdist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProfile(t *testing.T, forgeRoot, content string) {
	t.Helper()
	mustMk(t, os.MkdirAll(filepath.Join(forgeRoot, ".forge"), 0755))
	mustMk(t, os.WriteFile(ProfilePath(forgeRoot), []byte(content), 0644))
}

// TestLoadProfile_Missing: absent profile file = no profile = full distribution (the default path).
//
// TestLoadProfile_Missing：画像文件不存在 = 无画像 = 全量分发（默认路径）。
func TestLoadProfile_Missing(t *testing.T) {
	prof, err := LoadProfile(t.TempDir())
	mustMk(t, err)
	if prof != nil {
		t.Fatalf("无画像文件应返回 nil，got %v", prof)
	}
}

// TestLoadProfile_Parse: one name per line; # comments and blank lines skipped; duplicates deduped.
//
// TestLoadProfile_Parse：每行一个名；# 注释与空行跳过；重名去重。
func TestLoadProfile_Parse(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "# 项目画像\nalpha\n\n  beta  \nalpha\n# 尾注释\n")
	prof, err := LoadProfile(root)
	mustMk(t, err)
	want := []string{"alpha", "beta"}
	if len(prof) != len(want) {
		t.Fatalf("profile=%v want %v", prof, want)
	}
	for i, w := range want {
		if prof[i] != w {
			t.Fatalf("profile[%d]=%q want %q（full=%v）", i, prof[i], w, prof)
		}
	}
}

// TestLoadProfile_InvalidName: a malformed line is a hard error — silently ignoring the profile
// would distribute the full set while the user believes it is trimmed. Whitespace-bearing and
// separator-bearing names are rejected (IsValidSkillName deliberately loose, profile tightens:
// one canonical kebab name per line); non-ASCII but well-formed names fall through to the
// unknown→warning path instead (harmless if not in canonical).
//
// TestLoadProfile_InvalidName：非法行是硬错误——静默忽略画像会让用户以为已裁剪实则全量。
// 含空白/分隔符的名字拒绝（IsValidSkillName 刻意宽松，画像处收紧：每行一个规范 kebab 名）；
// 非_ascii 但格式完好的名字走 unknown→告警路径（不在 canonical 也无害）。
func TestLoadProfile_InvalidName(t *testing.T) {
	for _, bad := range []string{"has space", "has\ttab", "../evil", "a/b"} {
		root := t.TempDir()
		writeProfile(t, root, bad+"\n")
		if _, err := LoadProfile(root); err == nil {
			t.Errorf("非法 skill 名 %q 应报错", bad)
		}
	}
}

// TestLoadProfile_EmptyFileIsNotNil: a present file with only comments/blank lines is
// "profile active, allowlist empty" (non-nil empty slice) — NOT the absent-file nil.
// Downstream distinguishes via != nil; conflating the two would silently widen an
// all-commented debugging profile back to full distribution (review M1).
//
// TestLoadProfile_EmptyFileIsNotNil：文件存在但全注释/空行 = 「画像生效、白名单为空」
// （非 nil 空 slice）——不是「无画像」的 nil。下游用 != nil 区分；混同二者会让
// 调试期全注释画像被静默放大回全量分发（审查 M1）。
func TestLoadProfile_EmptyFileIsNotNil(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "# 全部注释掉调试中\n\n# alpha\n")
	prof, err := LoadProfile(root)
	mustMk(t, err)
	if prof == nil {
		t.Fatal("存在但为空的画像应返回非 nil 空 slice（≠无画像），got nil——下游 len>0 判定会退化为全量分发")
	}
	if len(prof) != 0 {
		t.Fatalf("全注释画像应解析为空，got %v", prof)
	}
}

// TestFilterByProfile: canonical order preserved; unknown entries collected for warnings.
//
// TestFilterByProfile：保持 canonical 顺序；未知条目收集为告警。
func TestFilterByProfile(t *testing.T) {
	all := []string{"alpha", "beta", "gamma"}
	kept, unknown := filterByProfile(all, []string{"gamma", "nope", "alpha"})
	if len(kept) != 2 || kept[0] != "alpha" || kept[1] != "gamma" {
		t.Fatalf("kept=%v want [alpha gamma]", kept)
	}
	if len(unknown) != 1 || unknown[0] != "nope" {
		t.Fatalf("unknown=%v want [nope]", unknown)
	}
	// empty profile = full set passthrough
	//
	// 空画像 = 全量直通
	kept, unknown = filterByProfile(all, nil)
	if len(kept) != 3 || len(unknown) != 0 {
		t.Fatalf("空画像应直通全量，kept=%v unknown=%v", kept, unknown)
	}
}

// TestInstall_ProfileTrims: profile restricts distribution to the allowlist; the excluded
// canonical skill is neither installed nor reported as failure; an unknown profile entry
// warns instead of erroring (durable config referencing removed skills is legitimate).
//
// TestInstall_ProfileTrims：画像把分发限定在白名单；被排除的 canonical skill 既不装也不计失败；
// 画像里的未知条目告警不报错（持久配置引用已移除的 skill 是合法状态）。
func TestInstall_ProfileTrims(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")
	writeCanonicalSkill(t, canonical, "gamma")
	projectDir := t.TempDir()
	opts := copyOpts(projectDir)
	opts.Profile = []string{"alpha", "gamma", "ghost"}

	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Total != 2 {
		t.Fatalf("total=%d want 2（画像白名单只含 alpha/gamma）", rep.Stats.Total)
	}
	if rep.Stats.Installed != 2 || rep.Stats.Failed != 0 {
		t.Fatalf("installed=%d failed=%d want 2/0", rep.Stats.Installed, rep.Stats.Failed)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "beta", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("beta 被画像排除，不应安装（err=%v）", err)
	}
	// ghost 不在 canonical → 告警不报错
	//
	// ghost not in canonical → warn, not error
	foundGhost := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "ghost") {
			foundGhost = true
		}
	}
	if !foundGhost {
		t.Fatalf("画像未知条目 ghost 应产生告警，warnings=%v", rep.Warnings)
	}
}

// TestInstall_ProfileExcludedPresentWarns: a skill already in the target but excluded by the
// profile must surface as a warning — install never deletes, yet the user must know the trim
// did not remove the stale copy.
//
// TestInstall_ProfileExcludedPresentWarns：已在目标但被画像排除的 skill 必须浮出为告警——
// install 绝不删除，但用户必须知道裁剪没有移走旧副本。
func TestInstall_ProfileExcludedPresentWarns(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")
	projectDir := t.TempDir()
	// 先全量装一次（beta 落进目标），再启用只含 alpha 的画像装第二次。
	//
	// full install first (beta lands in target), then install again with alpha-only profile.
	rep, err := Install(canonical, copyOpts(projectDir))
	mustMk(t, err)
	if rep.Stats.Installed != 2 {
		t.Fatalf("预置全量安装失败: installed=%d", rep.Stats.Installed)
	}
	opts := copyOpts(projectDir)
	opts.Profile = []string{"alpha"}
	rep, err = Install(canonical, opts)
	mustMk(t, err)
	foundBeta := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "beta") && strings.Contains(w, "画像") {
			foundBeta = true
		}
	}
	if !foundBeta {
		t.Fatalf("被排除但残留的 beta 应告警，warnings=%v", rep.Warnings)
	}
	// beta 内容保留未删（不销毁用户内容）
	//
	// beta content preserved (never destroy user content)
	if _, err := os.Stat(filepath.Join(projectDir, "beta", "SKILL.md")); err != nil {
		t.Fatalf("beta 应保留未删: %v", err)
	}
}

// TestInstall_EmptyProfileInstallsNothing: a non-nil empty profile (all lines commented)
// installs NOTHING (not everything) and says why — the silent full-distribution fallback
// is the exact trap review M1 flagged.
//
// TestInstall_EmptyProfileInstallsNothing：非 nil 空画像（全行注释）一个不装（而非
// 全量），且给出原因告警——静默回退全量正是审查 M1 点名的陷阱。
func TestInstall_EmptyProfileInstallsNothing(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")
	projectDir := t.TempDir()
	opts := copyOpts(projectDir)
	opts.Profile = []string{} // LoadProfile 对「存在但全注释」文件的返回形状

	rep, err := Install(canonical, opts)
	mustMk(t, err)
	if rep.Stats.Total != 0 || rep.Stats.Installed != 0 {
		t.Fatalf("空画像应一个不装，total=%d installed=%d", rep.Stats.Total, rep.Stats.Installed)
	}
	foundWhy := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "画像为空") {
			foundWhy = true
		}
	}
	if !foundWhy {
		t.Fatalf("空画像应有解释性告警，warnings=%v", rep.Warnings)
	}
}

// TestInstall_SkillFilterExcludedByProfileWarns: --skill names an in-canonical skill the
// profile excludes → the explicit request is dropped; a warning must say so, otherwise
// the silent Total=0 is unexplainable (review M2).
//
// TestInstall_SkillFilterExcludedByProfileWarns：--skill 点名 canonical 内但被画像
// 排除的 skill → 显式请求被丢弃；必须告警说明，否则 Total=0 无从解释（审查 M2）。
func TestInstall_SkillFilterExcludedByProfileWarns(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")
	projectDir := t.TempDir()
	opts := copyOpts(projectDir)
	opts.SkillFilter = []string{"alpha"}
	opts.Profile = []string{"beta"}

	rep, err := Install(canonical, opts)
	mustMk(t, err)
	// --skill 先收窄到 [alpha]，画像再剔除 alpha → 一个不装；告警让 Total=0 可解释。
	if rep.Stats.Total != 0 {
		t.Fatalf("total=%d want 0（--skill alpha 被画像剔除）", rep.Stats.Total)
	}
	if _, err := os.Stat(filepath.Join(projectDir, "beta", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("beta 不在 --skill 集合内，不应安装（err=%v）", err)
	}
	found := false
	for _, w := range rep.Warnings {
		if strings.Contains(w, "--skill alpha") && strings.Contains(w, "画像排除") {
			found = true
		}
	}
	if !found {
		t.Fatalf("--skill alpha 被画像剔除应有告警，warnings=%v", rep.Warnings)
	}
}

// TestDriftCheck_ProfileScopes: drift-check walks only profile skills in project scope —
// excluded canonical skills are not this project's distribution, reporting them is noise.
//
// TestDriftCheck_ProfileScopes：项目范围下 drift-check 只遍历画像内 skill——
// 被排除的 canonical skill 不归本项目分发管，报告它们是噪声。
func TestDriftCheck_ProfileScopes(t *testing.T) {
	canonical := t.TempDir()
	writeCanonicalSkill(t, canonical, "alpha")
	writeCanonicalSkill(t, canonical, "beta")
	projectDir := t.TempDir()
	opts := copyOpts(projectDir)
	opts.Profile = []string{"alpha"}

	rep, err := DriftCheck(canonical, opts)
	mustMk(t, err)
	if len(rep.Items) != 1 || rep.Items[0].Name != "alpha" {
		t.Fatalf("items=%v want 仅 alpha", rep.Items)
	}
	// alpha 未装 → missing=1
	if rep.Stats.Missing != 1 {
		t.Fatalf("missing=%d want 1", rep.Stats.Missing)
	}
}
