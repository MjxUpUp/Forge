package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/experience"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// --------------- Test: Experience list empty ---------------

func TestExperienceList_Empty(t *testing.T) {
	tmpDir, _ := forgedatatest.RealProject(t)

	stdout, _, code := runForge(t, tmpDir, "experience", "list")
	if code != 0 {
		t.Fatalf("forge experience list exit code %d, output: %s", code, stdout)
	}
	if !strings.Contains(stdout, "(none)") {
		t.Fatalf("expected '(none)' in output for empty lists, got: %s", stdout)
	}
}

// --------------- Test: Experience list --json ---------------

func TestExperienceList_JSON(t *testing.T) {
	tmpDir, proj := forgedatatest.RealProject(t)

	// Create a review file
	review := &experience.ReviewRequest{
		TaskRef: "PROJ-123",
		Score:   58,
		Grade:   "D",
		LowDimensions: []experience.LowDimension{
			{Dimension: scoringtypes.DimensionTesting, Score: 20, Detail: "No test file changes detected"},
			{Dimension: scoringtypes.DimensionAssertions, Score: 0, Detail: "Assertion weakening detected"},
		},
		Mandatory: true,
		Status:    experience.ReviewPending,
		CreatedAt: time.Date(2026, 6, 7, 10, 30, 0, 0, time.UTC),
	}
	if err := experience.SaveReview(proj, review); err != nil {
		t.Fatalf("failed to save review: %v", err)
	}

	// Create a proposal file
	proposal := &experience.ExperienceProposal{
		ID:           "exp-a1b2c3",
		SourceReview: "PROJ-123",
		Category:     "patterns",
		Title:        "禁止跳过错误处理",
		Description:  "不要忽略错误返回值",
		Patterns:     []string{"err\\s*!=\\s*nil\\s*\\{\\s*return"},
		Severity:     "error",
		Status:       experience.PropProposed,
		CreatedAt:    time.Date(2026, 6, 7, 10, 31, 0, 0, time.UTC),
	}
	if err := experience.SaveProposal(proj, proposal); err != nil {
		t.Fatalf("failed to save proposal: %v", err)
	}

	stdout, _, code := runForge(t, tmpDir, "experience", "list", "--json")
	if code != 0 {
		t.Fatalf("forge experience list --json exit code %d, output: %s", code, stdout)
	}

	// Verify valid JSON with expected structure
	var result struct {
		Reviews   []json.RawMessage `json:"reviews"`
		Proposals []json.RawMessage `json:"proposals"`
	}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\noutput: %s", err, stdout)
	}
	if len(result.Reviews) != 1 {
		t.Fatalf("expected 1 review, got %d", len(result.Reviews))
	}
	if len(result.Proposals) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(result.Proposals))
	}
}

// --------------- Test: Experience show ---------------

func TestExperienceShow(t *testing.T) {
	tmpDir, proj := forgedatatest.RealProject(t)

	review := &experience.ReviewRequest{
		TaskRef: "PROJ-456",
		Score:   45,
		Grade:   "F",
		LowDimensions: []experience.LowDimension{
			{Dimension: scoringtypes.DimensionTesting, Score: 10, Detail: "No tests"},
			{Dimension: scoringtypes.DimensionAssertions, Score: 0, Detail: "Weakened"},
		},
		Mandatory: true,
		Status:    experience.ReviewPending,
		CreatedAt: time.Date(2026, 6, 7, 14, 0, 0, 0, time.UTC),
	}
	if err := experience.SaveReview(proj, review); err != nil {
		t.Fatalf("failed to save review: %v", err)
	}

	stdout, _, code := runForge(t, tmpDir, "experience", "show", "PROJ-456")
	if code != 0 {
		t.Fatalf("forge experience show exit code %d, output: %s", code, stdout)
	}

	// Verify key fields appear in output
	if !strings.Contains(stdout, "PROJ-456") {
		t.Fatal("output missing task ref")
	}
	if !strings.Contains(stdout, "45") {
		t.Fatal("output missing score")
	}
	if !strings.Contains(stdout, "F") {
		t.Fatal("output missing grade")
	}
	if !strings.Contains(stdout, "pending") {
		t.Fatal("output missing status")
	}
	if !strings.Contains(stdout, "mandatory") {
		t.Fatal("output missing mandatory type")
	}
}

// --------------- Test: Experience show nonexistent ---------------

func TestExperienceShow_Nonexistent(t *testing.T) {
	tmpDir, _ := forgedatatest.RealProject(t)

	_, _, code := runForge(t, tmpDir, "experience", "show", "NONEXIST-999")
	if code == 0 {
		t.Fatal("expected non-zero exit for nonexistent review ref")
	}
}

// --------------- Test: Experience accept ---------------

func TestExperienceAccept(t *testing.T) {
	tmpDir, proj := forgedatatest.RealProject(t)

	// Create review and proposal
	review := &experience.ReviewRequest{
		TaskRef:   "PROJ-789",
		Score:     55,
		Grade:     "D",
		Mandatory: true,
		Status:    experience.ReviewPending,
		CreatedAt: time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC),
	}
	if err := experience.SaveReview(proj, review); err != nil {
		t.Fatalf("failed to save review: %v", err)
	}

	proposal := &experience.ExperienceProposal{
		ID:           "exp-accept1",
		SourceReview: "PROJ-789",
		Category:     "gotchas",
		Title:        "Always check errors",
		Description:  "Check all error return values",
		Patterns:     []string{"err.*=.*nil"},
		Severity:     "error",
		Status:       experience.PropProposed,
		CreatedAt:    time.Date(2026, 6, 7, 12, 1, 0, 0, time.UTC),
	}
	if err := experience.SaveProposal(proj, proposal); err != nil {
		t.Fatalf("failed to save proposal: %v", err)
	}

	// Accept the proposal — isolate BOTH HOME and USERPROFILE so the forge
	// subprocess writes the global knowledge store under tmpDir, not the real
	// home. On Windows os.UserHomeDir() reads USERPROFILE, not HOME — setting
	// only HOME leaked the accepted entry (exp-accept1 / PROJ-789 demo) into the
	// real ~/.forge/knowledge/index.json on every test run.
	//
	// home 必须与项目根（tmpDir）分离：projectroot.Find 会排除 home 这层的 .forge/
	// （那是全局状态目录，非项目根）。若把 home 设成 tmpDir，Find 会把 tmpDir 当 home
	// 排除 → forge 子进程找不到项目 .forge → accept 报 proposal not found。生产中
	// home 与项目根本就分离；这里把 home 放 tmpDir 子目录还原该不变量，既隔离全局
	// knowledge（写到 tmpDir/home/.forge/knowledge），又让 tmpDir/.forge 可被 Find 命中。
	homeDir := filepath.Join(tmpDir, "home")
	t.Setenv("HOME", homeDir)
	t.Setenv("USERPROFILE", homeDir)

	stdout, _, code := runForge(t, tmpDir, "experience", "accept", "exp-accept1")
	if code != 0 {
		t.Fatalf("forge experience accept exit code %d, output: %s", code, stdout)
	}

	// Verify proposal status changed to accepted
	loaded, err := experience.LoadProposal(proj, "exp-accept1")
	if err != nil {
		t.Fatalf("failed to load proposal after accept: %v", err)
	}
	if loaded.Status != experience.PropAccepted {
		t.Fatalf("expected proposal status 'accepted', got %q", loaded.Status)
	}
}

// --------------- Test: Experience reject ---------------

func TestExperienceReject(t *testing.T) {
	tmpDir, proj := forgedatatest.RealProject(t)

	proposal := &experience.ExperienceProposal{
		ID:           "exp-reject1",
		SourceReview: "PROJ-000",
		Category:     "patterns",
		Title:        "Bad pattern",
		Description:  "A rejected pattern",
		Patterns:     []string{"bad"},
		Severity:     "warning",
		Status:       experience.PropProposed,
		CreatedAt:    time.Date(2026, 6, 7, 13, 0, 0, 0, time.UTC),
	}
	if err := experience.SaveProposal(proj, proposal); err != nil {
		t.Fatalf("failed to save proposal: %v", err)
	}

	stdout, _, code := runForge(t, tmpDir, "experience", "reject", "exp-reject1")
	if code != 0 {
		t.Fatalf("forge experience reject exit code %d, output: %s", code, stdout)
	}

	// Verify proposal status changed to rejected
	loaded, err := experience.LoadProposal(proj, "exp-reject1")
	if err != nil {
		t.Fatalf("failed to load proposal after reject: %v", err)
	}
	if loaded.Status != experience.PropRejected {
		t.Fatalf("expected proposal status 'rejected', got %q", loaded.Status)
	}
}
