package taskpipeline

import (
	"path/filepath"
	"strings"
)

// DesignPhase 标识任务涉及的设计阶段。
type DesignPhase string

const (
	PhaseRequirement DesignPhase = "requirement" // 需求设计（PRD / 需求文档）
	PhaseAPI         DesignPhase = "api"          // API 设计（OpenAPI / proto / 接口定义）
	PhaseDatabase    DesignPhase = "database"     // 数据库设计（migrations / schema）
	PhaseFrontend    DesignPhase = "frontend"     // 前端设计（组件/页面/路由）
	PhaseBackend     DesignPhase = "backend"      // 后端设计（services / domain / 业务逻辑）
	PhaseTest        DesignPhase = "test-design"  // 测试用例设计（test 文件）
)

// AllDesignPhases 返回全部设计阶段。
func AllDesignPhases() []DesignPhase {
	return []DesignPhase{
		PhaseRequirement, PhaseAPI, PhaseDatabase,
		PhaseFrontend, PhaseBackend, PhaseTest,
	}
}

// inferDesignPhases 按文件路径推断任务涉及的设计阶段。
// 零摩擦：不要求用户声明，自动根据改动文件路径判断。
// 无匹配时返回空列表（不阻塞）。
func inferDesignPhases(changedFiles []string) []DesignPhase {
	phases := make(map[DesignPhase]bool)

	for _, f := range changedFiles {
		slash := filepath.ToSlash(f)
		ext := strings.ToLower(filepath.Ext(f))
		base := filepath.Base(f)
		dir := filepath.Dir(f)
		dirBase := filepath.Base(dir)

		switch {
		// 需求设计：docs/prd/*.md 含"验收/Out of Scope"
		case strings.Contains(slash, "docs/prd/") && ext == ".md":
			phases[PhaseRequirement] = true

		// API 设计：*.{yaml,yml} 匹配 openapi/asyncapi/proto / *.{proto,grpc}
		case ext == ".yaml" || ext == ".yml":
			if strings.Contains(slash, "openapi") ||
				strings.Contains(slash, "asyncapi") ||
				strings.Contains(slash, "proto") ||
				strings.Contains(slash, "api/") ||
				strings.Contains(dirBase, "api") ||
				strings.Contains(base, "openapi") ||
				strings.Contains(base, "swagger") ||
				strings.Contains(base, "proto") ||
				strings.Contains(base, "grpc") {
				phases[PhaseAPI] = true
			}
		case ext == ".proto" || ext == ".grpc":
			phases[PhaseAPI] = true

		// 数据库设计：migrations/*.sql / schema.*
		case ext == ".sql" && (strings.Contains(slash, "migrations/") ||
			strings.Contains(base, "schema") ||
			strings.Contains(slash, "migration")):
			phases[PhaseDatabase] = true

		// 前端设计：*.{tsx,jsx,vue} / components/*
		case ext == ".tsx" || ext == ".jsx" || ext == ".vue":
			phases[PhaseFrontend] = true
		case strings.Contains(slash, "components/") && (ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx"):
			phases[PhaseFrontend] = true

		// 测试设计：*_test.* / *.test.* / test_*
		case strings.Contains(base, "_test.") ||
			strings.Contains(base, ".test.") ||
			strings.Contains(base, "test_") ||
			strings.Contains(dirBase, "test"):
			phases[PhaseTest] = true

		// 后端设计：services/*/ / domain/ / *.{go,rs,java}
		case strings.Contains(slash, "services/") ||
			strings.Contains(slash, "domain/") ||
			strings.Contains(slash, "internal/"):
			phases[PhaseBackend] = true
		case (ext == ".go" || ext == ".rs" || ext == ".java") &&
			!strings.Contains(slash, "components/") &&
			!strings.Contains(slash, "test") &&
			!strings.Contains(slash, "migrations/") &&
			!strings.Contains(slash, "openapi") &&
			!strings.Contains(slash, "docs/prd/"):
			phases[PhaseBackend] = true
		}
	}

	// 转为有序切片（保持确定性）
	var result []DesignPhase
	for _, p := range AllDesignPhases() {
		if phases[p] {
			result = append(result, p)
		}
	}
	return result
}
