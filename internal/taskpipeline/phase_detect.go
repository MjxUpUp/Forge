package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
)

// DesignPhase 标识任务涉及的设计阶段。
type DesignPhase string

const (
	PhaseRequirement DesignPhase = "requirement" // 需求设计（PRD / 需求文档）
	PhaseAPI         DesignPhase = "api"         // API 设计（OpenAPI / proto / 接口定义）
	PhaseDatabase    DesignPhase = "database"    // 数据库设计（migrations / schema）
	PhaseFrontend    DesignPhase = "frontend"    // 前端设计（组件/页面/路由）
	PhaseBackend     DesignPhase = "backend"     // 后端设计（services / domain / 业务逻辑）
	PhaseTest        DesignPhase = "test-design" // 测试用例设计（test 文件）
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
		// 仅 .ts/.js：.tsx/.jsx 已被上一条 case（*.{tsx,jsx,vue}）接走，写这里永不可达。
		case strings.Contains(slash, "components/") && (ext == ".ts" || ext == ".js"):
			phases[PhaseFrontend] = true

		// 测试设计：*_test.* / *.test.*（后缀中缀，Contains 安全）；test_*.py 前缀
		// （Python）用 HasPrefix——旧 Contains 会误匹配 latest_feature.go（"la**test_**..."）。
		case strings.Contains(base, "_test.") ||
			strings.Contains(base, ".test.") ||
			strings.HasPrefix(base, "test_") ||
			dirBase == "test" || dirBase == "tests" || dirBase == "__tests__":
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

// designPhasesEqual 比较两个 DesignPhase 切片是否相等（顺序敏感——inferDesignPhases
// 按 AllDesignPhases 固定顺序输出，故同输入必同顺序）。用于 task-verify gate 判断
// 推断结果是否变化，避免每次 verify 无谓写盘。
func designPhasesEqual(a, b []DesignPhase) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// scanDesignArtifacts 扫描 working tree 的已知设计产物目录，返回存在的设计文件
// 路径（相对 root，正斜杠规范化）。补 taskChangedFiles 的 gitignore 盲区：docs/ 等
// 常被全局或项目 gitignore，git diff 三源都 --exclude-standard 看不到，致
// PhaseRequirement 等设计阶段推不出（回路断在第一环）。这里直接读文件系统，不依赖
// git——phase 推断的目的是"加载对应 checklist 审查"，项目存在设计产物即应覆盖（即便
// 本次未改，审查代码时对照 PRD/API 设计也合理）。只扫顶层，不深递归，避免 migrations
// 历史几百个文件拖慢 task-verify。
func scanDesignArtifacts(root string) []string {
	designDirs := []struct {
		dir  string
		exts []string
	}{
		{"docs/prd", []string{".md"}},
		{"api/openapi", []string{".yaml", ".yml"}},
		{"openapi", []string{".yaml", ".yml"}},
		{"proto", []string{".proto"}},
		{"migrations", []string{".sql"}},
	}
	var files []string
	for _, d := range designDirs {
		entries, err := os.ReadDir(filepath.Join(root, d.dir))
		if err != nil {
			continue // 目录不存在/不可读——正常，多数项目没全套设计目录
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			for _, want := range d.exts {
				if ext == want {
					files = append(files, filepath.ToSlash(filepath.Join(d.dir, e.Name())))
					break
				}
			}
		}
	}
	return files
}
