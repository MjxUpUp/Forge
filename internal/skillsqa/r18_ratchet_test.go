package skillsqa

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// TestR18_Grandfathered_Exact — R18 存量豁免表的棘轮守卫（双向卡死，跑在仓库源
// skills/ 上）：表内每一条都必须仍被需要（对应 skill 存在操作性行为命中），表外
// 任何非 requires_forge skill 都不得有命中。三个失败方向各有明确指引——
//   - 表外 skill 有命中：新违例。要么清理内容（去 forge 化），要么它本就是
//     forge 原生 skill（补 `metadata.requires_forge: "true"`）；不允许为它加豁免。
//   - 表内 skill 无命中：死条目。清理完成后忘了从 R18Grandfathered 移除。
//   - 表内名字不在库里：skill 已更名/删除，条目随之作废。
//
// 该测试保证「只减不增」不是口头纪律：加条目必须同时改这张表并过 code review，
// 而任何新增 forge 引用在不改表时立刻红。
//
// TestR18_Grandfathered_Exact — the ratchet guard for the R18 legacy exemption
// table (pinned in both directions, runs against the repo-source skills/):
// every table entry must still be needed (its skill still has operational
// hits), and no non-requires_forge skill outside the table may have hits.
// Each failure direction carries its remedy —
//   - hit outside the table: a new violation. Either clean the content
//     (de-forge it) or, if it genuinely documents forge itself, add
//     `metadata.requires_forge: "true"`; adding a grandfather entry is not
//     the remedy.
//   - table entry without hits: dead entry, cleanup done but the
//     R18Grandfathered removal was forgotten.
//   - table name not in the library: renamed/removed skill, stale entry.
//
// This test turns "shrink-only" from prose into mechanics: growing the table
// requires editing it in the open, and any new forge reference goes red
// immediately without a table edit.
func TestR18_Grandfathered_Exact(t *testing.T) {
	root := filepath.Join("..", "..", "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("仓库源 skills/ 不可读（%v）——棘轮守卫仅在源码仓内生效", err)
	}

	matched := map[string]bool{} // 有命中且非 requires_forge 的 skill 名
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sd := filepath.Join(root, e.Name())
		data, rerr := os.ReadFile(filepath.Join(sd, "SKILL.md"))
		if rerr != nil {
			continue // 无 SKILL.md 的目录（fact-research/web-search-bridge 遗骸）不可安装，不参与
		}
		fm := skillsfm.Parse(data)
		if v, ok := fm.Metadata["requires_forge"]; ok && strings.Trim(strings.TrimSpace(v), `"`) == "true" {
			continue // forge 原生 skill：独立豁免通道，不入棘轮
		}
		if len(ScanForgeRefs(sd, fm)) > 0 {
			matched[filepath.Base(sd)] = true
		}
	}

	for name := range matched {
		if !R18Grandfathered[name] {
			t.Errorf("skill %q 存在 forge 反向依赖命中但不在 R18Grandfathered——新违例：清理内容去 forge 化（forge 原生 skill 则补 requires_forge 标记），不允许加豁免", name)
		}
	}
	for name := range R18Grandfathered {
		if !matched[name] {
			t.Errorf("R18Grandfathered 死条目 %q——该 skill 已无操作性命中（或已更名/删除），请移除", name)
		}
	}

	if len(matched) != len(R18Grandfathered) {
		var got, want []string
		for n := range matched {
			got = append(got, n)
		}
		for n := range R18Grandfathered {
			want = append(want, n)
		}
		sort.Strings(got)
		sort.Strings(want)
		t.Errorf("豁免表与实际命中集合不相等: got=%v want=%v", got, want)
	}
}

// TestSkillsForge_AllMarked — forge 原生专区（仓库源 skills-forge/）的准入守卫：
// 每个 skill 必须带 `metadata.requires_forge: "true"`。专区的存在意义就是收纳
// 「描述 forge 自身机制」的 skill——出现未标记的 skill = 中立 skill 放错位置
// （应进 skills/ 并过 R18 零反向依赖校验）。2026-08 迁移起效。
//
// TestSkillsForge_AllMarked — admission guard for the forge-native zone
// (repo-source skills-forge/): every skill must carry
// `metadata.requires_forge: "true"`. The zone exists precisely to host skills
// that document forge itself — an unmarked skill there is a neutral skill
// misplaced (it belongs in skills/ under the R18 zero-reverse-dependency
// check). In force since the 2026-08 migration.
func TestSkillsForge_AllMarked(t *testing.T) {
	root := filepath.Join("..", "..", "skills-forge")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("仓库源 skills-forge/ 不可读（%v）——专区守卫仅在源码仓内生效", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sd := filepath.Join(root, e.Name())
		data, rerr := os.ReadFile(filepath.Join(sd, "SKILL.md"))
		if rerr != nil {
			continue
		}
		fm := skillsfm.Parse(data)
		if v, ok := fm.Metadata["requires_forge"]; !ok || strings.Trim(strings.TrimSpace(v), `"`) != "true" {
			t.Errorf("skills-forge/%s 未声明 metadata.requires_forge: true——专区只收 forge 原生 skill；中立方法论 skill 应放 skills/（过 R18 校验）", e.Name())
		}
	}
}

// TestSkills_NoneMarked — TestSkillsForge_AllMarked 的镜像守卫：中立树 skills/
// 不得声明 `metadata.requires_forge: "true"`。该标记是 R18 的最高豁免——R18 升为
// 硬校验后（2026-08），在中立树头文件加这一行即整体跳过校验且无任何测试变红
// （review M2：与 R18Grandfathered 被棘轮双向卡死的保护强度不对称）。forge 原生
// 内容的家是 skills-forge/，中立树出现该标记 = 豁免通道被滥用。
//
// TestSkills_NoneMarked — the mirror guard of TestSkillsForge_AllMarked: the
// neutral skills/ tree must NOT declare `metadata.requires_forge: "true"`.
// That marker is R18's top-tier exemption — after R18 became a hard gate
// (2026-08), adding this one frontmatter line in the neutral tree skips the
// check entirely with no test going red (review M2: asymmetric protection
// vs. the ratchet-pinned R18Grandfathered). Forge-native content belongs in
// skills-forge/; the marker appearing in the neutral tree = abused escape hatch.
func TestSkills_NoneMarked(t *testing.T) {
	root := filepath.Join("..", "..", "skills")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("仓库源 skills/ 不可读（%v）——镜像守卫仅在源码仓内生效", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sd := filepath.Join(root, e.Name())
		data, rerr := os.ReadFile(filepath.Join(sd, "SKILL.md"))
		if rerr != nil {
			continue
		}
		fm := skillsfm.Parse(data)
		if v, ok := fm.Metadata["requires_forge"]; ok && strings.Trim(strings.TrimSpace(v), `"`) == "true" {
			t.Errorf("skills/%s 声明了 metadata.requires_forge: true——中立树零豁免；forge 原生 skill 应住 skills-forge/（CONVENTIONS §13）", e.Name())
		}
	}
}
