package change

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidID(t *testing.T) {
	valid := []string{"a", "abc", "x1", "repo-declared-spec", strings.Repeat("a", 64)}
	for _, id := range valid {
		if err := ValidID(id); err != nil {
			t.Errorf("ValidID(%q) 期望通过，got %v", id, err)
		}
	}
	invalid := []string{"", "-a", "a-", "A", "a_b", "a/b", "a b", "../x", strings.Repeat("a", 65)}
	for _, id := range invalid {
		if err := ValidID(id); err == nil {
			t.Errorf("ValidID(%q) 期望拒绝，got nil", id)
		}
	}
}

func TestInit_CreatesContract(t *testing.T) {
	root := t.TempDir()
	created, err := Init(root, "mini")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if len(created) == 0 {
		t.Fatal("Init 应报告新建的文件")
	}

	raw, err := os.ReadFile(filepath.Join(root, "spec", "forge.yaml"))
	if err != nil {
		t.Fatalf("读契约: %v", err)
	}
	for _, want := range []string{`version: "1"`, `profile: "mini"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("契约缺 %q：\n%s", want, raw)
		}
	}
	if _, err := os.Stat(ChangesDir(root)); err != nil {
		t.Errorf("应创建 spec/changes/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Errorf("应写缺省 AGENTS.md: %v", err)
	}

	// 幂等性：已存在就不覆盖（契约是人的决策文件，不能被静默重刷）。
	if _, err := Init(root, "full"); err == nil {
		t.Fatal("第二次 Init 应报已存在")
	}
}

func TestInit_KeepsExistingAgentsFile(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# 本仓库自己的规则\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(root, "mini"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	raw, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "# 本仓库自己的规则\n" {
		t.Errorf("不应覆盖已有 AGENTS.md，got %q", raw)
	}
}

func TestInit_RejectsUnknownProfile(t *testing.T) {
	if _, err := Init(t.TempDir(), "huge"); err == nil {
		t.Fatal("未知 profile 应报错")
	}
}

func TestNew_RequiresContract(t *testing.T) {
	_, err := New(t.TempDir(), "demo", "")
	if !errors.Is(err, ErrNoSpec) {
		t.Fatalf("期望 ErrNoSpec，got %v", err)
	}
}

func TestNew_CreatesSkeleton(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo-change", "示例标题")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, name := range []string{FileProposal, FileDesign, FileTasks, FileAcceptance} {
		path := filepath.Join(dir, name)
		raw, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("缺产物 %s: %v", name, rerr)
		}
		if name == FileProposal {
			for _, want := range []string{"示例标题", "`demo-change`"} {
				if !strings.Contains(string(raw), want) {
					t.Errorf("proposal 缺 %q", want)
				}
			}
		}
	}

	// 重复创建与非法 id 都必须拒绝：change 是人的决策落点，不能被静默覆盖。
	if _, err := New(root, "demo-change", "again"); err == nil {
		t.Error("重复 New 应报错")
	}
	if _, err := New(root, "Bad ID", ""); err == nil {
		t.Error("非法 id 应报错")
	}
}

func TestList_DerivesStageFromFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	mk := func(id string, files ...string) {
		dir := ChangeDir(root, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte("# x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("aaa-empty")
	mk("bbb-proposal", FileProposal)
	mk("ccc-design", FileProposal, FileDesign)
	mk("ddd-tasks", FileProposal, FileDesign, FileTasks)

	// 归档目录只贡献一项，且带日期前缀要去掉。
	archive := filepath.Join(ChangesDir(root), ArchiveDirName, "20260101-old-change", FileProposal)
	if err := os.MkdirAll(filepath.Dir(archive), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(archive, []byte("# old\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	items, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	got := map[string]Change{}
	for _, c := range items {
		got[c.ID] = c
	}
	wantStage := map[string]Stage{
		"aaa-empty":    StageEmpty,
		"bbb-proposal": StageProposal,
		"ccc-design":   StageDesign,
		"ddd-tasks":    StageTasks,
		"old-change":   StageArchived,
	}
	for id, want := range wantStage {
		c, ok := got[id]
		if !ok {
			t.Errorf("List 缺 %s", id)
			continue
		}
		if c.Stage != want {
			t.Errorf("%s: stage=%s，期望 %s", id, c.Stage, want)
		}
	}
	if !got["old-change"].Archive {
		t.Error("archive/ 下的 change 应标 Archive")
	}
	if items[0].Archive {
		t.Error("归档项应排在活动 change 之后")
	}
}

func TestLoad_StaleWhenAcceptanceEdited(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "")
	if err != nil {
		t.Fatal(err)
	}
	accPath := filepath.Join(dir, FileAcceptance)
	raw := []byte("```bash\ntrue\n```\n")
	if err := os.WriteFile(accPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeSealedSummary(t, dir, raw)

	c, _, err := Load(root, "demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.Sealed || c.Stage != StageVerified {
		t.Fatalf("期望 Sealed/verified，got sealed=%v stage=%s", c.Sealed, c.Stage)
	}

	// 改判据：旧证据自动作废——不需要谁去"更新状态"。
	if err := os.WriteFile(accPath, []byte("```bash\ntrue && true\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, _, err = Load(root, "demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Sealed {
		t.Error("判据改过后不应仍然 Sealed")
	}
	if !c.Stale {
		t.Error("应标记 Stale")
	}
	if c.Stage == StageVerified {
		t.Error("Stale 后阶段不应还是 verified")
	}
}

func TestLoad_MissingChange(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(root, "nope"); err == nil {
		t.Fatal("不存在的 change 应报错")
	}
}

// writeSealedSummary 造一份与 acceptance.md 对应的通过证据（不实跑命令）。
func writeSealedSummary(t *testing.T, changeDir string, acceptance []byte) {
	t.Helper()
	ev := filepath.Join(changeDir, EvidenceDirName, "20260101T000000Z")
	if err := os.MkdirAll(ev, 0o755); err != nil {
		t.Fatal(err)
	}
	sum := Summary{
		Change:           filepath.Base(changeDir),
		Passed:           true,
		Sealed:           true,
		AcceptanceSHA256: hashBytes(acceptance),
		Checks:           []Check{{Index: 1, Command: "true", Exit: 0}},
	}
	raw, err := json.Marshal(sum)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ev, SummaryFile), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

// 中途被打断的 verify 会留下只有目录、没有 summary.json 的残迹。
// 它不构成证据：既不能顶替真证据，也不该让人以为自己跑过。
func TestList_IgnoresIncompleteEvidence(t *testing.T) {
	root := t.TempDir()
	if _, err := Init(root, "mini"); err != nil {
		t.Fatal(err)
	}
	dir, err := New(root, "demo", "demo")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, EvidenceDirName, "20260101T000000Z"), 0o755); err != nil {
		t.Fatal(err)
	}
	list, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("期望 1 个 change，got %d", len(list))
	}
	if list[0].EvidenceCount != 0 {
		t.Errorf("没有摘要的目录不算证据，got %d", list[0].EvidenceCount)
	}
	if list[0].Sealed {
		t.Error("残迹不能算封印")
	}
}
