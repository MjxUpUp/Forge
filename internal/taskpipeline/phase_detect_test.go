package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInferDesignPhases(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		expected []DesignPhase
	}{
		{
			name:     "requirement phase - PRD file",
			files:    []string{"docs/prd/feature.md"},
			expected: []DesignPhase{PhaseRequirement},
		},
		{
			name:     "api phase - openapi yaml",
			files:    []string{"api/openapi/spec.yaml"},
			expected: []DesignPhase{PhaseAPI},
		},
		{
			name:     "api phase - proto file",
			files:    []string{"proto/helloworld.proto"},
			expected: []DesignPhase{PhaseAPI},
		},
		{
			name:     "database phase - migration sql",
			files:    []string{"migrations/001_create_users.sql"},
			expected: []DesignPhase{PhaseDatabase},
		},
		{
			name:     "database phase - schema sql",
			files:    []string{"schema.sql"},
			expected: []DesignPhase{PhaseDatabase},
		},
		{
			name:     "frontend phase - tsx component",
			files:    []string{"components/Button.tsx"},
			expected: []DesignPhase{PhaseFrontend},
		},
		{
			name:     "frontend phase - vue file",
			files:    []string{"pages/Login.vue"},
			expected: []DesignPhase{PhaseFrontend},
		},
		{
			name:     "test phase - test file",
			files:    []string{"service_test.go"},
			expected: []DesignPhase{PhaseTest},
		},
		{
			name:     "backend phase - go service",
			files:    []string{"services/user.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		{
			name:     "backend phase - domain file",
			files:    []string{"domain/order.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		{
			name:     "multi-phase - full stack",
			files:    []string{"docs/prd/feature.md", "api/openapi/spec.yaml", "components/App.tsx"},
			expected: []DesignPhase{PhaseRequirement, PhaseAPI, PhaseFrontend},
		},
		{
			name:     "empty - no matching files",
			files:    []string{"README.md", "package.json"},
			expected: nil,
		},
		{
			name:     "empty - no files",
			files:    []string{},
			expected: nil,
		},
		// BUG-3 回归守卫：dirBase 含 "test" 子串（latest/testutil）不可误判为 PhaseTest。
		// 旧代码 strings.Contains(dirBase,"test") 把它们标成 test，污染 PhasePassRate。
		{
			name:     "BUG-3 regression - latest dir not test phase",
			files:    []string{"internal/latest/helper.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		{
			name:     "BUG-3 regression - testutil dir not test phase",
			files:    []string{"internal/testutil/setup.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		// 死代码清理后：components/ 下的 .ts（非 tsx）仍走 frontend（.tsx 已被首条 case 接走）。
		{
			name:     "frontend phase - components ts file",
			files:    []string{"components/util.ts"},
			expected: []DesignPhase{PhaseFrontend},
		},
		// F1 回归守卫：文件名含 "test_" 子串（latest_feature.go）不可误判为 PhaseTest。
		// 旧 Contains(base,"test_") 会匹配 "la**test_**feature.go"，污染 PhasePassRate。
		{
			name:     "F1 regression - test_ substring in filename not test phase",
			files:    []string{"internal/latest/latest_feature.go"},
			expected: []DesignPhase{PhaseBackend},
		},
		// HasPrefix 修复后：Python test_*.py 前缀仍正确匹配 PhaseTest（不能因修子串误判漏掉真测试）。
		{
			name:     "test phase - python test_ prefix file",
			files:    []string{"pkg/calc/test_calculator.py"},
			expected: []DesignPhase{PhaseTest},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferDesignPhases(tt.files)

			// Compare as sets (order-independent)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d phases, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			seen := make(map[DesignPhase]bool)
			for _, p := range result {
				seen[p] = true
			}
			for _, want := range tt.expected {
				if !seen[want] {
					t.Errorf("missing expected phase %s in result %v", want, result)
				}
			}
		})
	}
}

func TestAllDesignPhases(t *testing.T) {
	all := AllDesignPhases()
	if len(all) != 6 {
		t.Errorf("expected 6 design phases, got %d", len(all))
	}
}

// TestScanDesignArtifacts_BypassesGitignore 钉死 gitignore 盲区修复：docs/prd 被
// gitignore 时 git diff 看不到，scanDesignArtifacts 直接读文件系统应兜底返回它。
func TestScanDesignArtifacts_BypassesGitignore(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "docs/prd"), 0755)
	os.WriteFile(filepath.Join(dir, "docs/prd/feature.md"), []byte("# PRD\n"), 0644)
	os.MkdirAll(filepath.Join(dir, "api/openapi"), 0755)
	os.WriteFile(filepath.Join(dir, "api/openapi/spec.yaml"), []byte("openapi: 3.0\n"), 0644)
	// 非 design 目录 + 非设计 ext 不该扫。
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src/main.go"), []byte("package main\n"), 0644)
	// docs/ 下非 prd 子目录不该扫（只 docs/prd）。
	os.MkdirAll(filepath.Join(dir, "docs/notes"), 0755)
	os.WriteFile(filepath.Join(dir, "docs/notes/x.md"), []byte("notes\n"), 0644)

	got := scanDesignArtifacts(dir)
	wantSet := map[string]bool{
		"docs/prd/feature.md":   false,
		"api/openapi/spec.yaml": false,
	}
	for _, f := range got {
		if _, ok := wantSet[f]; ok {
			wantSet[f] = true
		}
	}
	for f, found := range wantSet {
		if !found {
			t.Errorf("scanDesignArtifacts 漏掉 %s（gitignore 兜底失效？got=%v）", f, got)
		}
	}
	// src/main.go 和 docs/notes/x.md 不该被扫到（范围限定）。
	for _, f := range got {
		if strings.Contains(f, "src/main.go") {
			t.Errorf("scanDesignArtifacts 不该扫 src/：%s", f)
		}
		if strings.Contains(f, "docs/notes") {
			t.Errorf("scanDesignArtifacts 不该扫 docs/notes（只 docs/prd）：%s", f)
		}
	}
}
