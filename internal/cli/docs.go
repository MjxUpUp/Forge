package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MjxUpUp/Forge/internal/doclint"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/spf13/cobra"
)

var (
	docsLintBase string
	docsLintJSON bool
)

// exit code 契约与 skills validate 对齐：0=全部通过（或仅建议），
// 2=存在硬失败。
var docsLintCmd = &cobra.Command{
	Use:   "lint [paths...] [--base <rev>]",
	Short: "L1 文档 lint：禁令短语/必填章节/结论枚举/篇幅（exit code: 0=通过 2=硬失败）",
	Long: `forge docs lint 对 markdown 产物跑 L1 确定性检查（D1-D7）：
  - D1/D2 禁令短语与无证据整体结论（hard）
  - D3 围栏外复述 diff（advisory）
  - D4 通过性断言无证据标记（advisory）
  - D5-D7 按文件名命中的类型化规则（必填章节/结论枚举 hard、篇幅 advisory）

路径参数为文件或目录（目录递归收集 .md）；--base <rev> 改为 lint
该基线以来变更的 .md（已提交 + 工作区 + 未跟踪，与 doc gate 同一集合——
BLOCKED 文案承诺的「可复现」由此保证）。豁免：vendor/、node_modules/、
dist/、.git/、.zcode/、testdata/、归档目录、CHANGELOG、decisions.md、
文件头 forge-doc-lint: skip 标记。`,
	RunE: runDocsLint,
}

func runDocsLint(cmd *cobra.Command, args []string) error {
	files, err := collectLintTargets(args, docsLintBase)
	if err != nil {
		return err
	}

	type fileRes struct {
		File   string          `json:"file"`
		Issues []doclint.Issue `json:"issues"`
	}
	results := make([]fileRes, 0, len(files))
	hardCount, advisoryCount := 0, 0
	for _, f := range files {
		issues, lerr := doclint.LintFile(f)
		if lerr != nil {
			results = append(results, fileRes{File: f, Issues: []doclint.Issue{
				{Line: 0, Rule: "IO", Severity: doclint.Hard, Message: "读取失败: " + lerr.Error()},
			}})
			hardCount++
			continue
		}
		if issues == nil {
			issues = []doclint.Issue{}
		}
		for _, iss := range issues {
			if iss.Hard() {
				hardCount++
			} else {
				advisoryCount++
			}
		}
		results = append(results, fileRes{File: f, Issues: issues})
	}

	if docsLintJSON {
		out := struct {
			Total    int       `json:"total"`
			Hard     int       `json:"hard"`
			Advisory int       `json:"advisory"`
			Results  []fileRes `json:"results"`
		}{len(files), hardCount, advisoryCount, results}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		if len(files) == 0 {
			fmt.Println("无可 lint 的 markdown 目标（0 文件视为通过）")
		}
		for _, r := range results {
			if len(r.Issues) == 0 {
				fmt.Printf("  ✓ %s\n", r.File)
				continue
			}
			fmt.Printf("  ✗ %s\n", r.File)
			for _, iss := range r.Issues {
				sev := "硬"
				if !iss.Hard() {
					sev = "建议"
				}
				fmt.Printf("      - [%s][%s] L%d: %s\n", iss.Rule, sev, iss.Line, iss.Message)
			}
		}
		fmt.Printf("扫描 %d 文件 / 硬失败 %d / 建议 %d\n", len(files), hardCount, advisoryCount)
	}

	if hardCount > 0 {
		// 此前是 os.Exit(2)——它会绕过 cobra 链上所有 defer 清理，也绕过 Execute
		// 的 panic 恢复盘（os.Exit 不执行 defer）。返回 errHardExit 哨兵让这些安全
		// 网保持生效；Execute 映射为 exit 2（结论已在上方打印——不加多余 stderr 行）。
		// 退出码契约不变：hook 宿主只读 `forge hook` 子命令的退出码；`forge docs
		// lint` 的消费方（doc-review skill 笔记）保持 0=通过 / 2=硬失败。
		return errHardExit
	}
	return nil
}

// collectLintTargets 解析显式路径（文件或递归目录）；或带 --base 时取该
// 基线以来变更的 markdown（已提交 + 工作区）。显式参数优先于 --base。
func collectLintTargets(args []string, base string) ([]string, error) {
	if len(args) > 0 {
		var files []string
		for _, a := range args {
			info, err := os.Stat(a)
			if err != nil {
				return nil, fmt.Errorf("路径不可访问 %q: %w", a, err)
			}
			if !info.IsDir() {
				files = append(files, filepath.ToSlash(a))
				continue
			}
			err = filepath.WalkDir(a, func(path string, d os.DirEntry, werr error) error {
				if werr != nil {
					return werr
				}
				if d.IsDir() {
					return nil
				}
				if strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
					files = append(files, filepath.ToSlash(path))
				}
				return nil
			})
			if err != nil {
				return nil, err
			}
		}
		sort.Strings(files)
		return files, nil
	}

	if base != "" {
		root, err := findProjectRoot()
		if err != nil {
			return nil, err
		}
		// Shared with the doc gate (taskpipeline.ChangedMarkdownSince): the
		// gate's BLOCKED text promises "forge docs lint 可复现" — the CLI must
		// enumerate the identical set (committed + working tree + untracked,
		// deleted dropped, exemptions applied).
		//
		// 与 doc gate 共用（taskpipeline.ChangedMarkdownSince）：门禁 BLOCKED
		// 文案承诺「forge docs lint 可复现」——CLI 必须枚举同一集合
		// （已提交 + 工作区 + 未跟踪、剔除已删除、应用豁免）。
		files, err := taskpipeline.ChangedMarkdownSince(root, base)
		if err != nil {
			return nil, fmt.Errorf("--base 扫描失败: %w", err)
		}
		sort.Strings(files)
		return files, nil
	}

	return nil, fmt.Errorf("需要路径参数或 --base <rev>（如 forge docs lint docs/ 或 forge docs lint --base main）")
}

var docsCmd = &cobra.Command{
	Use:   "docs",
	Short: "文档产物可读性约束（L1 lint）",
}

func init() {
	docsLintCmd.Flags().StringVar(&docsLintBase, "base", "", "lint 该基线以来变更的 .md（已提交+工作区+未跟踪，与 doc gate 同集合），如 --base main")
	docsLintCmd.Flags().BoolVar(&docsLintJSON, "json", false, "JSON 输出")
	docsCmd.AddCommand(docsLintCmd)
	rootCmd.AddCommand(docsCmd)
}
