package toolusage

import "testing"

func TestDetectSkills_SkillTool(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Skill", ToolInput: `{"skill": "forge-pipeline", "args": ""}`},
		{ToolName: "Skill", ToolInput: `{"skill": "code-review"}`},
	}
	hits := DetectSkills(calls)
	if len(hits) != 2 {
		t.Fatalf("expected 2 skill hits, got %d", len(hits))
	}
	if hits[0].SkillName != "forge-pipeline" {
		t.Errorf("expected forge-pipeline, got %s", hits[0].SkillName)
	}
	if hits[0].Source != "skill-tool" {
		t.Errorf("expected source=skill-tool, got %s", hits[0].Source)
	}
	if hits[1].SkillName != "code-review" {
		t.Errorf("expected code-review, got %s", hits[1].SkillName)
	}
}

func TestDetectSkills_ForgeCLI(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Bash", ToolInput: `{"command": "forge task start --ref test-feature"}`},
		{ToolName: "Bash", ToolInput: `{"command": "forge verify --regression"}`},
	}
	hits := DetectSkills(calls)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
	if hits[0].SkillName != "forge-pipeline" {
		t.Errorf("expected forge-pipeline, got %s", hits[0].SkillName)
	}
	if hits[0].Source != "forge-cli" {
		t.Errorf("expected source=forge-cli, got %s", hits[0].Source)
	}
	if hits[1].SkillName != "verify" {
		t.Errorf("expected verify, got %s", hits[1].SkillName)
	}
}

func TestDetectSkills_NoSkills(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Read", ToolInput: `{"file_path": "test.go"}`},
		{ToolName: "Edit", ToolInput: `{"file_path": "test.go"}`},
		{ToolName: "Bash", ToolInput: `{"command": "go test ./..."}`},
	}
	hits := DetectSkills(calls)
	if len(hits) != 0 {
		t.Errorf("expected 0 hits, got %d", len(hits))
	}
}

func TestDetectSkills_Mixed(t *testing.T) {
	calls := []ToolCall{
		{ToolName: "Skill", ToolInput: `{"skill": "forge-quality"}`},
		{ToolName: "Bash", ToolInput: `{"command": "forge task gate task-verify"}`},
		{ToolName: "Read", ToolInput: `{"file_path": "test.go"}`},
	}
	hits := DetectSkills(calls)
	if len(hits) != 2 {
		t.Fatalf("expected 2 hits, got %d", len(hits))
	}
}

func TestUniqueSkillNames(t *testing.T) {
	hits := []SkillHit{
		{SkillName: "forge-pipeline", Source: "skill-tool"},
		{SkillName: "forge-pipeline", Source: "forge-cli"},
		{SkillName: "verify", Source: "forge-cli"},
	}
	names := UniqueSkillNames(hits)
	if len(names) != 2 {
		t.Fatalf("expected 2 unique names, got %d", len(names))
	}
	if names[0] != "forge-pipeline" {
		t.Errorf("expected forge-pipeline, got %s", names[0])
	}
}

func TestSkillHitCount(t *testing.T) {
	hits := []SkillHit{
		{SkillName: "a"},
		{SkillName: "a"},
		{SkillName: "b"},
	}
	if SkillHitCount(hits) != 2 {
		t.Errorf("expected 2, got %d", SkillHitCount(hits))
	}
}

func TestParseSkillName_InvalidJSON(t *testing.T) {
	name := parseSkillName("not-json")
	if name != "" {
		t.Errorf("expected empty name for invalid JSON, got %s", name)
	}
}

func TestParseSkillName_NoSkillField(t *testing.T) {
	name := parseSkillName(`{"args": "test"}`)
	if name != "" {
		t.Errorf("expected empty name, got %s", name)
	}
}
