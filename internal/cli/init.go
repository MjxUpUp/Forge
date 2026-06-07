package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Harness/forge/internal/hooks"
	"github.com/Harness/forge/internal/pipeline"
	"github.com/Harness/forge/internal/protocol"
	"github.com/Harness/forge/internal/skillgen"
	"github.com/Harness/forge/internal/snapshot"
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().String("mode", "", "管道模式: small, medium, large")
	initCmd.Flags().Bool("fresh", false, "跳过项目快照推断，从第一个门禁开始")
}

var initCmd = &cobra.Command{
	Use:   "init [--mode small|medium|large]",
	Short: "初始化项目管道（创建 .forge/ 目录结构 + Skill + Hooks）",
	Long: `forge init 为当前项目创建 .forge/ 目录，
包含 pipeline.yml v2、state.json、hooks/ 和 Claude Code Skill。

模式：small（原型/Demo）、medium（个人正式项目）、large（企业级项目）。`,
	RunE: runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	mode, _ := cmd.Flags().GetString("mode")
	if mode == "" {
		mode = detectMode(dir)
	}

	// Create .forge/ directory structure
	forgeDir := filepath.Join(dir, ".forge")
	dirs := []string{
		forgeDir,
		filepath.Join(forgeDir, "gates"),
		filepath.Join(forgeDir, "hooks"),
		filepath.Join(forgeDir, "tasks"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", d, err)
		}
	}

	projectName := filepath.Base(dir)

	// Write pipeline.yml v2
	pipelineContent := getPipelineTemplate(mode, projectName)
	if err := os.WriteFile(filepath.Join(forgeDir, "pipeline.yml"), []byte(pipelineContent), 0644); err != nil {
		return fmt.Errorf("failed to write pipeline.yml: %w", err)
	}

	// Write state.json
	state := pipeline.State{
		PipelineVersion: "2.0",
		Mode:            mode,
		StartedAt:       time.Now(),
	}

	// Project snapshot inference: detect existing project state and
	// auto-skip gates that appear to be already completed.
	var inferredGates []snapshot.InferredGate
	fresh, _ := cmd.Flags().GetBool("fresh")
	if !fresh {
		snap, err := snapshot.Take(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: project snapshot failed: %v\n", err)
		} else if snap != nil {
			// Load the pipeline we just wrote to get gate structure
			if p, err := pipeline.Load(dir); err == nil {
				inferredGates = snapshot.InferCompletedGates(snap, p)

				// Record overrides for inferred gates
				for _, ig := range inferredGates {
					state.AddOverride(ig.GateID, "auto-detected: "+ig.Reason)
				}

				// Record snapshot data for transparency
				inferredIDs := make([]string, len(inferredGates))
				for i, ig := range inferredGates {
					inferredIDs[i] = ig.GateID
				}
				state.Snapshot = &pipeline.SnapshotData{
					TakenAt:       snap.TakenAt,
					InferredGates: inferredIDs,
				}
			}
		}
	}

	stateJSON, err := jsonMarshal(state)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(forgeDir, "state.json"), stateJSON, 0644); err != nil {
		return fmt.Errorf("failed to write state.json: %w", err)
	}

	// Copy hook templates
	if err := hooks.WriteHookTemplates(forgeDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to copy hooks: %v\n", err)
	}

	// Generate .claude/settings.local.json
	if err := hooks.GenerateSettings(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate .claude/settings.local.json: %v\n", err)
	}

	// Generate Claude Code Skill
	var inferredIDs []string
	for _, ig := range inferredGates {
		inferredIDs = append(inferredIDs, ig.GateID)
	}
	p, err := pipeline.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load pipeline for skill generation: %v\n", err)
	} else if err := skillgen.GenerateSkill(dir, p, inferredIDs); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate skill: %v\n", err)
	}

	// Write quality protocol
	proto := protocol.DefaultProtocol(mode)
	if err := protocol.Save(dir, proto); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write protocol.yml: %v\n", err)
	}

	// Generate quality skill
	if p != nil {
		if err := skillgen.GenerateQualitySkill(dir, proto, p); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to generate quality skill: %v\n", err)
		}
	}

	// Generate .claude/CLAUDE.md with quality protocol reference
	if err := skillgen.GenerateClaudeMD(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to generate CLAUDE.md: %v\n", err)
	}

	fmt.Printf("Forge pipeline initialized (mode: %s)\n", mode)
	fmt.Println()
	fmt.Println("Created:")
	fmt.Printf("  .forge/pipeline.yml              — 管道定义 (v2)\n")
	fmt.Printf("  .forge/state.json                — 管道状态机\n")
	fmt.Printf("  .forge/protocol.yml              — 质量协议\n")
	fmt.Printf("  .forge/hooks/                    — 门禁 Hook 脚本\n")
	fmt.Printf("  .forge/tasks/                    — 任务状态目录\n")
	fmt.Printf("  .claude/settings.local.json      — Claude Code 集成\n")
	fmt.Printf("  .claude/CLAUDE.md                — 质量协议引用\n")
	fmt.Printf("  .claude/skills/forge-pipeline/    — 管道编排 Skill\n")
	fmt.Printf("  .claude/skills/forge-quality/     — 质量协议 Skill\n")

	// Show snapshot inference results
	if len(inferredGates) > 0 {
		// Show detected signals
		snap, _ := snapshot.Take(dir)
		if snap != nil {
			fmt.Println()
			fmt.Println("Detected project signals:")
			fmt.Println(snapshot.FormatSignals(&snap.Signals))
		}

		fmt.Println()
		fmt.Println("Inferred completed gates:")
		fmt.Println(snapshot.FormatInferred(inferredGates))

		// Find starting gate
		if p, err := pipeline.Load(dir); err == nil {
			completed := state.CompletedGates()
			// Also mark overridden gates as completed for the purpose of finding next gate
			for _, ig := range inferredGates {
				completed[ig.GateID] = true
			}
			nextGate := p.NextReadyGate(completed)
			fmt.Println()
			if nextGate != "" {
				if gate, err := p.GetGate(nextGate); err == nil {
					fmt.Printf("Pipeline starts from: %s (%s)\n", nextGate, gate.Name)
				} else {
					fmt.Printf("Pipeline starts from: %s\n", nextGate)
				}
			} else {
				fmt.Println("All gates inferred as completed — pipeline is fully done.")
			}
		}
	}

	fmt.Println()
	fmt.Println()
	fmt.Println("Next step: open Claude Code in this project and describe what you want to build.")
	fmt.Println("Claude Code will read the Forge skill and drive the pipeline automatically.")
	fmt.Println()
	fmt.Println("Manual commands:")
	fmt.Println("  forge status    — see all gate IDs and their status")
	fmt.Println("  forge validate  — check pipeline.yml configuration")
	fmt.Println()
	if len(inferredGates) > 0 {
		fmt.Println("Use --fresh to reinitialize without project detection.")
	}
	return nil
}

func detectMode(dir string) string {
	indicators := []struct {
		path string
		mode string
	}{
		{"go.mod", "medium"},
		{"Cargo.toml", "medium"},
		{"package.json", "medium"},
		{"Makefile", "medium"},
		{"README.md", "small"},
	}
	for _, ind := range indicators {
		if _, err := os.Stat(filepath.Join(dir, ind.path)); err == nil {
			return ind.mode
		}
	}
	return "small"
}

func getPipelineTemplate(mode, project string) string {
	switch strings.ToLower(mode) {
	case "small":
		return fmt.Sprintf(smallTemplate, project)
	case "large":
		return fmt.Sprintf(largeTemplate, project)
	default:
		return fmt.Sprintf(mediumTemplate, project)
	}
}

// --- Pipeline YAML v2 Templates ---

const smallTemplate = `version: "2.0"
project: "%s"
mode: small

pipeline:
  gates:
    - id: gate-4-implement
      name: "代码实现"
      enabled: true
      depends_on: []
      prompt: |
        根据以下要求编写代码并确保编译通过：
        {{.UserInput}}

        要求：
        - 每完成一个文件，确保编译通过
        - 编写对应的单元测试
        - 不允许跳过测试或弱化断言
      hooks:
        - auto-compile.sh
        - assertion-check.sh
      artifacts:
        inputs: []
        outputs:
          - compile.log
          - test-results.json
      checks:
        - name: "编译通过"
          type: file_not_contains
          params:
            file: compile.log
            keyword: ERROR
        - name: "单元测试通过"
          type: json_equals
          params:
            file: test-results.json
            field: failed
            value: 0
      on_failure: abort

    - id: gate-8-release
      name: "发布"
      enabled: true
      depends_on: [gate-4-implement]
      prompt: |
        准备发布。检查以下内容并生成发布清单：
        - 所有测试通过
        - CHANGELOG.md 已更新
        - 版本号符合 semver
      artifacts:
        inputs:
          - gate:gate-4-implement/test-results.json
        outputs:
          - checklist.md
      checks:
        - name: "前序门禁通过"
          type: all_gates_passed
        - name: "CHANGELOG 更新"
          type: file_exists
          params:
            file: CHANGELOG.md
            in: project_root
      on_failure: abort
`

const mediumTemplate = `version: "2.0"
project: "%s"
mode: medium

pipeline:
  gates:
    - id: gate-1-prd
      name: "需求定义"
      enabled: true
      depends_on: []
      prompt: |
        根据以下需求编写 PRD 文档：
        {{.UserInput}}

        输出要求：
        1. prd.md — 需求文档（含功能列表、验收条件、Out of Scope）
        2. acceptance-criteria.json — 验收条件（JSON 数组）
      artifacts:
        inputs: [user_idea]
        outputs:
          - prd.md
          - acceptance-criteria.json
      checks:
        - name: "明确不做什么"
          type: file_contains
          params:
            file: prd.md
            keyword: "Out of Scope"
        - name: "验收条件存在"
          type: file_exists
          params:
            file: acceptance-criteria.json
      on_failure: abort
      auto_publish_feishu: true

    - id: gate-3-plan
      name: "实现计划"
      enabled: true
      depends_on: [gate-1-prd]
      prompt: |
        根据以下 PRD 和验收条件，制定实现计划：
        PRD: {{.GateArtifacts "gate-1-prd" "prd.md"}}
        验收条件: {{.GateArtifacts "gate-1-prd" "acceptance-criteria.json"}}

        输出要求：
        1. plan.md — 实现计划（含任务拆解、依赖关系）
        2. tasks.json — 任务列表（JSON 数组，每个任务含 id、title、acceptance_criteria）
      artifacts:
        inputs:
          - gate:gate-1-prd/prd.md
          - gate:gate-1-prd/acceptance-criteria.json
        outputs:
          - plan.md
          - tasks.json
      checks:
        - name: "计划文件存在"
          type: file_exists
          params:
            file: plan.md
        - name: "任务列表存在"
          type: file_exists
          params:
            file: tasks.json
      on_failure: abort

    - id: gate-4-implement
      name: "代码实现"
      enabled: true
      depends_on: [gate-3-plan]
      prompt: |
        根据以下实现计划编写代码：
        计划: {{.GateArtifacts "gate-3-plan" "plan.md"}}
        任务: {{.GateArtifacts "gate-3-plan" "tasks.json"}}

        要求：
        - 每完成一个文件，确保编译通过
        - 编写对应的单元测试
        - 不允许跳过测试或弱化断言
      hooks:
        - auto-compile.sh
        - assertion-check.sh
        - experience-check.sh
      artifacts:
        inputs:
          - gate:gate-3-plan/plan.md
          - gate:gate-3-plan/tasks.json
        outputs:
          - compile.log
          - test-results.json
      checks:
        - name: "编译通过"
          type: file_not_contains
          params:
            file: compile.log
            keyword: ERROR
        - name: "单元测试通过"
          type: json_equals
          params:
            file: test-results.json
            field: failed
            value: 0
        - name: "无断言弱化"
          type: custom_script
          params:
            script: assertion-check.sh
        - name: "无已知经验违规"
          type: knowledge_check
      on_failure: abort

    - id: gate-5-test
      name: "测试验证"
      enabled: true
      depends_on: [gate-4-implement]
      prompt: |
        验证测试覆盖情况：
        验收条件: {{.GateArtifacts "gate-1-prd" "acceptance-criteria.json"}}
        测试结果: {{.GateArtifacts "gate-4-implement" "test-results.json"}}

        输出要求：
        1. test-plan.md — 测试计划
        2. coverage.json — 覆盖率数据
      artifacts:
        inputs:
          - gate:gate-1-prd/acceptance-criteria.json
          - gate:gate-4-implement/test-results.json
        outputs:
          - test-plan.md
          - coverage.json
      checks:
        - name: "测试计划存在"
          type: file_exists
          params:
            file: test-plan.md
        - name: "覆盖率数据存在"
          type: file_exists
          params:
            file: coverage.json
      on_failure: abort

    - id: gate-6-acceptance
      name: "项目验收"
      enabled: true
      depends_on: [gate-5-test]
      prompt: |
        进行项目验收，对比 PRD 和实际实现：
        PRD: {{.GateArtifacts "gate-1-prd" "prd.md"}}
        验收条件: {{.GateArtifacts "gate-1-prd" "acceptance-criteria.json"}}

        输出要求：
        1. report.md — 验收报告
        2. gap-analysis.json — 缺口分析（JSON，含 covered 和 uncovered 列表）
      artifacts:
        inputs:
          - gate:gate-1-prd/prd.md
          - gate:gate-1-prd/acceptance-criteria.json
          - gate:gate-5-test/coverage.json
        outputs:
          - report.md
          - gap-analysis.json
      checks:
        - name: "PRD 功能全覆盖"
          type: json_equals
          params:
            file: gap-analysis.json
            field: uncovered_count
            value: 0
        - name: "README 更新"
          type: file_exists
          params:
            file: README.md
            in: project_root
      on_failure: abort
      requires_human_approval: true

    - id: gate-8-release
      name: "发布"
      enabled: true
      depends_on: [gate-6-acceptance]
      prompt: |
        准备发布。检查以下内容并生成发布清单：
        验收报告: {{.GateArtifacts "gate-6-acceptance" "report.md"}}

        输出要求：
        1. checklist.md — 发布清单
      artifacts:
        inputs:
          - gate:gate-6-acceptance/report.md
        outputs:
          - checklist.md
      checks:
        - name: "前序门禁通过"
          type: all_gates_passed
        - name: "CHANGELOG 更新"
          type: file_exists
          params:
            file: CHANGELOG.md
            in: project_root
      on_failure: abort
`

const largeTemplate = `version: "2.0"
project: "%s"
mode: large

pipeline:
  gates:
    - id: gate-0-research
      name: "立项调研"
      enabled: true
      depends_on: []
      prompt: |
        对以下创意进行立项调研：
        {{.UserInput}}

        输出要求：
        1. report.md — 调研报告（含竞品分析、技术可行性）
        2. competitors.json — 竞品列表（JSON 数组，每个元素含 name/url/description）
      timeout: 600
      artifacts:
        inputs: [user_idea]
        outputs:
          - report.md
          - competitors.json
      checks:
        - name: "竞品分析完整性"
          type: json_array_min_count
          params:
            file: competitors.json
            min_count: 3
        - name: "技术可行性"
          type: file_contains
          params:
            file: report.md
            keyword: "技术可行性"
      on_failure: abort
      auto_publish_feishu: true

    - id: gate-1-prd
      name: "需求定义"
      enabled: true
      depends_on: [gate-0-research]
      prompt: |
        根据调研结果编写 PRD：
        调研报告: {{.GateArtifacts "gate-0-research" "report.md"}}
        竞品分析: {{.GateArtifacts "gate-0-research" "competitors.json"}}

        输出要求：
        1. prd.md — 需求文档（含功能列表、验收条件、异常场景、Out of Scope）
        2. acceptance-criteria.json — 验收条件（JSON 数组）
      artifacts:
        inputs:
          - gate:gate-0-research/report.md
          - gate:gate-0-research/competitors.json
        outputs:
          - prd.md
          - acceptance-criteria.json
      checks:
        - name: "明确不做什么"
          type: file_contains
          params:
            file: prd.md
            keyword: "Out of Scope"
        - name: "异常场景覆盖"
          type: file_contains
          params:
            file: prd.md
            keyword: "异常"
        - name: "验收条件存在"
          type: file_exists
          params:
            file: acceptance-criteria.json
      on_failure: abort
      auto_publish_feishu: true

    - id: gate-2-design
      name: "技术方案"
      enabled: true
      depends_on: [gate-1-prd]
      prompt: |
        根据以下 PRD 设计技术方案：
        PRD: {{.GateArtifacts "gate-1-prd" "prd.md"}}

        输出要求：
        1. design.md — 技术设计文档（含架构图、接口定义、数据模型）
        2. adr/ — 架构决策记录目录
      artifacts:
        inputs:
          - gate:gate-1-prd/prd.md
        outputs:
          - design.md
      checks:
        - name: "设计文档存在"
          type: file_exists
          params:
            file: design.md
      on_failure: abort

    - id: gate-3-plan
      name: "实现计划"
      enabled: true
      depends_on: [gate-1-prd, gate-2-design]
      prompt: |
        制定实现计划：
        PRD: {{.GateArtifacts "gate-1-prd" "prd.md"}}
        技术方案: {{.GateArtifacts "gate-2-design" "design.md"}}

        输出要求：
        1. plan.md — 实现计划
        2. tasks.json — 任务列表
      artifacts:
        inputs:
          - gate:gate-1-prd/prd.md
          - gate:gate-2-design/design.md
        outputs:
          - plan.md
          - tasks.json
      checks:
        - name: "计划文件存在"
          type: file_exists
          params:
            file: plan.md
        - name: "任务列表存在"
          type: file_exists
          params:
            file: tasks.json
      on_failure: abort

    - id: gate-4-implement
      name: "代码实现"
      enabled: true
      depends_on: [gate-3-plan]
      prompt: |
        根据实现计划编写代码：
        计划: {{.GateArtifacts "gate-3-plan" "plan.md"}}
        任务: {{.GateArtifacts "gate-3-plan" "tasks.json"}}

        要求：
        - 每完成一个文件，确保编译通过
        - 编写对应的单元测试
        - 不允许跳过测试或弱化断言
      hooks:
        - auto-compile.sh
        - assertion-check.sh
        - experience-check.sh
      artifacts:
        inputs:
          - gate:gate-3-plan/plan.md
          - gate:gate-3-plan/tasks.json
        outputs:
          - compile.log
          - test-results.json
      checks:
        - name: "编译通过"
          type: file_not_contains
          params:
            file: compile.log
            keyword: ERROR
        - name: "单元测试通过"
          type: json_equals
          params:
            file: test-results.json
            field: failed
            value: 0
        - name: "无断言弱化"
          type: custom_script
          params:
            script: assertion-check.sh
        - name: "无已知经验违规"
          type: knowledge_check
      on_failure: abort

    - id: gate-5-test
      name: "测试验证"
      enabled: true
      depends_on: [gate-4-implement]
      prompt: |
        验证测试覆盖情况并制定 E2E 测试计划：
        验收条件: {{.GateArtifacts "gate-1-prd" "acceptance-criteria.json"}}
        测试结果: {{.GateArtifacts "gate-4-implement" "test-results.json"}}

        输出要求：
        1. test-plan.md — 测试计划（含 E2E 核心路径）
        2. coverage.json — 覆盖率数据
      artifacts:
        inputs:
          - gate:gate-1-prd/acceptance-criteria.json
          - gate:gate-4-implement/test-results.json
        outputs:
          - test-plan.md
          - coverage.json
      checks:
        - name: "测试计划存在"
          type: file_exists
          params:
            file: test-plan.md
        - name: "覆盖率数据存在"
          type: file_exists
          params:
            file: coverage.json
      on_failure: abort

    - id: gate-6-acceptance
      name: "项目验收"
      enabled: true
      depends_on: [gate-5-test]
      prompt: |
        进行项目验收：
        PRD: {{.GateArtifacts "gate-1-prd" "prd.md"}}
        技术方案: {{.GateArtifacts "gate-2-design" "design.md"}}
        验收条件: {{.GateArtifacts "gate-1-prd" "acceptance-criteria.json"}}

        输出要求：
        1. report.md — 验收报告
        2. gap-analysis.json — 缺口分析
      artifacts:
        inputs:
          - gate:gate-1-prd/prd.md
          - gate:gate-1-prd/acceptance-criteria.json
          - gate:gate-2-design/design.md
          - gate:gate-5-test/coverage.json
        outputs:
          - report.md
          - gap-analysis.json
      checks:
        - name: "PRD 功能全覆盖"
          type: json_equals
          params:
            file: gap-analysis.json
            field: uncovered_count
            value: 0
        - name: "README 更新"
          type: file_exists
          params:
            file: README.md
            in: project_root
      on_failure: abort
      requires_human_approval: true

    - id: gate-7-archive
      name: "经验归档"
      enabled: true
      depends_on: [gate-6-acceptance]
      prompt: |
        归档本次开发经验：
        缺口分析: {{.GateArtifacts "gate-6-acceptance" "gap-analysis.json"}}

        输出要求：
        1. lessons.md — 经验总结（至少 3 条）
        2. cross-project.json — 可跨项目复用的经验条目
      artifacts:
        inputs:
          - gate:gate-6-acceptance/gap-analysis.json
        outputs:
          - lessons.md
          - cross-project.json
      checks:
        - name: "经验总结存在"
          type: file_exists
          params:
            file: lessons.md
      on_failure: warn

    - id: gate-8-release
      name: "发布"
      enabled: true
      depends_on: [gate-6-acceptance, gate-7-archive]
      prompt: |
        准备发布：
        验收报告: {{.GateArtifacts "gate-6-acceptance" "report.md"}}

        输出要求：
        1. checklist.md — 发布清单
      artifacts:
        inputs:
          - gate:gate-6-acceptance/report.md
        outputs:
          - checklist.md
      checks:
        - name: "前序门禁通过"
          type: all_gates_passed
        - name: "CHANGELOG 更新"
          type: file_exists
          params:
            file: CHANGELOG.md
            in: project_root
      on_failure: abort
`
