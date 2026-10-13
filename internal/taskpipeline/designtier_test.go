package taskpipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// TestDesignTier 钉住 W4 分档路由：硬触发器推导、标准变更豁免、跨模块信号、声明与推导
// 取较高者、带理由下调、protocol.yml 覆盖、go.mod patch 升级豁免、事后升档。
func TestDesignTier(t *testing.T) {
	builtin := tierConfig{rules: defaultTierRules, standard: defaultStandardChanges}

	cases := []struct {
		name        string
		files       []string
		wantTier    string
		wantTrigger string // 子串；"" = 不应有触发
	}{
		{"纯文档是标准变更", []string{"README.md", "docs/a.md", "db/migrations/README.md"}, tasktypes.DesignTierNone, ""},
		{"单模块改动直接改", []string{"internal/x/a.go", "internal/x/a_test.go"}, tasktypes.DesignTierNone, ""},
		{"迁移目录触发完整方案", []string{"db/migrations/0003_add.go"}, tasktypes.DesignTierFull, "schema-migration"},
		{"sql 文件触发完整方案", []string{"schema/users.sql"}, tasktypes.DesignTierFull, "schema-migration"},
		{"proto 触发完整方案", []string{"proto/v1/user.proto"}, tasktypes.DesignTierFull, "api-contract"},
		{"鉴权目录触发完整方案", []string{"internal/auth/token.go"}, tasktypes.DesignTierFull, "auth"},
		{"author.go 不是鉴权", []string{"internal/x/author.go"}, tasktypes.DesignTierNone, ""},
		{"依赖文件触发小方案", []string{"go.mod"}, tasktypes.DesignTierMini, "dependency"},
		{"跨 3 个目录触发小方案", []string{"a/x.go", "b/y.go", "c/z.go"}, tasktypes.DesignTierMini, "cross-module"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tier, triggers := deriveTier(tc.files, builtin)
			if tier != tc.wantTier {
				t.Errorf("tier = %q, want %q (triggers %v)", tier, tc.wantTier, triggers)
			}
			joined := strings.Join(triggers, "|")
			if tc.wantTrigger == "" && len(triggers) > 0 {
				t.Errorf("不应有触发: %v", triggers)
			}
			if tc.wantTrigger != "" && !strings.Contains(joined, tc.wantTrigger) {
				t.Errorf("triggers %v 应含 %q", triggers, tc.wantTrigger)
			}
		})
	}

	t.Run("声明与推导取较高者，下调须带理由", func(t *testing.T) {
		if d := DecideDesignTier("", "", "mini", nil); d.Final != "mini" {
			t.Errorf("未声明取推导: %+v", d)
		}
		if d := DecideDesignTier("full", "", "mini", nil); d.Final != "full" {
			t.Errorf("声明更高取声明: %+v", d)
		}
		if d := DecideDesignTier("none", "", "full", nil); d.Final != "full" || d.Reason != "" {
			t.Errorf("无理由下调应取推导: %+v", d)
		}
		if d := DecideDesignTier("none", "只改测试夹具里的 sql", "full", nil); d.Final != "none" || d.Reason == "" {
			t.Errorf("带理由下调应生效并留理由: %+v", d)
		}
		if err := ValidateDesignTier("huge"); err == nil {
			t.Error("非法档位应报错")
		}
		if out := FormatDesignTierAdvice(DecideDesignTier("none", "", "full", []string{"auth: internal/auth/a.go"})); !strings.Contains(out, "完整方案") || !strings.Contains(out, "--tier-reason") {
			t.Errorf("提示应点名档位产物与下调出口: %s", out)
		}
	})

	t.Run("protocol.yml 追加与同名覆盖", func(t *testing.T) {
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		root := t.TempDir()
		proto := protocol.DefaultProtocol()
		proto.DesignTiers = &protocol.DesignTiersConfig{
			Rules: []protocol.DesignTierRule{
				{Name: "billing", Tier: "full", Paths: []string{"seg:billing"}},
				{Name: "dependency", Tier: "full", Paths: []string{"go.mod"}},
			},
			StandardChanges: []string{"*.golden"},
		}
		if err := protocol.SaveDataDir(root, proto); err != nil {
			t.Fatal(err)
		}
		if tier, _ := DeriveDesignTier(root, []string{"internal/billing/pay.go"}); tier != "full" {
			t.Errorf("追加规则应生效, got %q", tier)
		}
		if tier, _ := DeriveDesignTier(root, []string{"go.mod"}); tier != "full" {
			t.Errorf("同名规则应覆盖内置, got %q", tier)
		}
		if tier, trig := DeriveDesignTier(root, []string{"internal/auth/x.golden"}); tier != "none" {
			t.Errorf("追加标准变更应豁免, got %q %v", tier, trig)
		}
	})

	t.Run("go.mod 只升 patch 是标准变更，新增依赖不是", func(t *testing.T) {
		root := t.TempDir()
		git := func(args ...string) {
			t.Helper()
			cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
			cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v %s", args, err, out)
			}
		}
		mod := func(body string) {
			if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		git("init", "-q")
		mod("module m\n\nrequire (\n\tgithub.com/a/b v1.2.3\n)\n")
		git("add", ".")
		git("commit", "-qm", "base")
		base := gitOut(root, "rev-parse", "HEAD")
		mod("module m\n\nrequire (\n\tgithub.com/a/b v1.2.9\n)\n")
		if !goModPatchOnly(root, base) {
			t.Error("patch 升级应判为标准变更")
		}
		mod("module m\n\nrequire (\n\tgithub.com/a/b v1.3.0\n)\n")
		if goModPatchOnly(root, base) {
			t.Error("minor 升级不是 patch-only")
		}
		mod("module m\n\nrequire (\n\tgithub.com/a/b v1.2.3\n\tgithub.com/c/d v0.1.0\n)\n")
		if goModPatchOnly(root, base) {
			t.Error("新增依赖不是 patch-only")
		}
	})

	t.Run("事后升档：diff 要求更高档且无 design 产物时落 escalated 行", func(t *testing.T) {
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		root := t.TempDir()
		st := &TaskState{TaskRef: "feat/esc", DesignTier: &tasktypes.DesignTierDecision{Declared: "none", Final: "none"}}
		CheckDesignTierEscalation(root, st, []string{"internal/auth/token.go"})
		entries, _ := checklog.LoadForTask(root, st.TaskRef)
		var meta map[string]string
		for _, e := range entries {
			if e.Check == CheckNameDesignTier {
				meta = e.Meta
			}
		}
		if meta["escalated"] != "true" || meta["derived"] != "full" || meta["phase"] != "complete" {
			t.Fatalf("应落事后升档行: %v", meta)
		}

		st2 := &TaskState{TaskRef: "feat/esc2", DesignTier: &tasktypes.DesignTierDecision{Final: "none"},
			SpecArtifacts: map[string]ArtifactRef{"design": {Path: "x"}}}
		CheckDesignTierEscalation(root, st2, []string{"internal/auth/token.go"})
		entries, _ = checklog.LoadForTask(root, st2.TaskRef)
		if len(entries) != 0 {
			t.Fatalf("已登记 design 产物时不应升档: %+v", entries)
		}
	})

	t.Run("implement 首轮判定并落 design-tier 行", func(t *testing.T) {
		t.Setenv("FORGE_DATA_HOME", t.TempDir())
		root := t.TempDir()
		st := &TaskState{TaskRef: "feat/impl", PlanScope: []string{"proto/v1/user.proto"}}
		RecordImplementDesignTier(root, st, nil)
		if st.DesignTier == nil || st.DesignTier.Final != "full" {
			t.Fatalf("应判 full: %+v", st.DesignTier)
		}
		if !findCheckEntry(t, root, st.TaskRef, CheckNameDesignTier) {
			t.Fatal("应落 design-tier 行")
		}
	})
}

// TestPlanConversionByTier 钉住 W0.2：按档位统计任务数、跳过（要求方案却无 design/plan）
// 与否决（带理由下调）；无档位判定的任务不进切片。
func TestPlanConversionByTier(t *testing.T) {
	states := []*TaskState{
		{DesignTier: &tasktypes.DesignTierDecision{Final: "mini"}},
		{DesignTier: &tasktypes.DesignTierDecision{Final: "mini"}, Plan: "p"},
		{DesignTier: &tasktypes.DesignTierDecision{Final: "full"}, SpecArtifacts: map[string]ArtifactRef{"design": {Path: "d"}}},
		{DesignTier: &tasktypes.DesignTierDecision{Final: "none", Declared: "none", Derived: "full", Reason: "测试夹具"}},
		{},
	}
	got := ComputePlanConversion(states).ByTier
	want := map[string]TierStats{
		"mini": {Tasks: 2, Skipped: 1},
		"full": {Tasks: 1},
		"none": {Tasks: 1, Overridden: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ByTier = %+v, want %+v", got, want)
	}
}

// TestDesignTierReviewFixes 钉住审查轮修复：公共接口只认仓库顶层 api/pkg/sdk；docs
// 目录下的源码不再被当作标准变更；伪版本升级不是 patch-only。
func TestDesignTierReviewFixes(t *testing.T) {
	builtin := tierConfig{rules: defaultTierRules, standard: defaultStandardChanges}
	if tier, _ := deriveTier([]string{"internal/api/handler.go"}, builtin); tier != "none" {
		t.Errorf("internal/api 不是公共接口, got %q", tier)
	}
	if tier, _ := deriveTier([]string{"api/v1/user.go"}, builtin); tier != "full" {
		t.Errorf("顶层 api/ 应触发完整方案, got %q", tier)
	}
	if tier, _ := deriveTier([]string{"docs/tool/gen.go", "a/x.go", "b/y.go"}, builtin); tier != "mini" {
		t.Errorf("docs 下的源码应计入跨模块, got %q", tier)
	}
	if tier, trig := deriveTier([]string{"requirements.txt"}, builtin); tier != "mini" {
		t.Errorf("requirements.txt 是依赖文件，应触发小方案, got %q %v", tier, trig)
	}
	if goModVersionLineRe.MatchString("\tgithub.com/a/b v0.0.0-20240101000000-abcdef123456") {
		t.Error("伪版本不应被当作可比较的 patch 版本")
	}
	if !goModVersionLineRe.MatchString("\tgithub.com/a/b v1.2.3 // indirect") {
		t.Error("带 // indirect 的正式版本应可解析")
	}
}
