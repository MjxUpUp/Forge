package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
)

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
		case isTestPhasePath(base, dirBase):
			phases[PhaseTest] = true

		// 后端设计：services/*/ / domain/ / *.{go,rs,java}
		case strings.Contains(slash, "services/") ||
			strings.Contains(slash, "domain/") ||
			strings.Contains(slash, "internal/"):
			phases[PhaseBackend] = true
		case (ext == ".go" || ext == ".rs" || ext == ".java") &&
			!strings.Contains(slash, "components/") &&
			// segment/base 级测试排除——与上方测试设计 case 同一判定。裸
			// strings.Contains(slash, "test") 会误排 latest_feature.go、contest/、
			// testutil/（测试判定里已修掉的同款陷阱，backend 排除侧仍有）。
			!isTestPhasePath(base, dirBase) &&
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

// isTestPhasePath 按 segment/base 级匹配判定路径是否为测试产物（非全路径子串）：
// 后缀/中缀 _test. / .test.、Python test_* 前缀、或 test/tests/__tests__ 目录。
// 测试设计 case 与 backend 兜底的测试排除共用此判定，保证两处永不漂移——此处用
// 子串匹配会误命中 latest_feature.go、contest/、testutil/。
func isTestPhasePath(base, dirBase string) bool {
	return strings.Contains(base, "_test.") ||
		strings.Contains(base, ".test.") ||
		strings.HasPrefix(base, "test_") ||
		dirBase == "test" || dirBase == "tests" || dirBase == "__tests__"
}

// designPhasesEqual 比较两个 DesignPhase 切片是否相等（顺序敏感——inferDesignPhases
// 按 allDesignPhases 固定顺序输出，故同输入必同顺序）。用于 task-verify gate 判断
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
