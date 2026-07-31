package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/MjxUpUp/Forge/internal/skillsqa"
	"github.com/spf13/cobra"
)

var (
	skValSkill []string
	skValJSON  bool
)

// exit code contract: 0=all pass, 2=spec failure present.
//
// exit code 契约：0=全部通过，2=存在规范失败。
var skillsValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "R1-R11 规范校验（exit code: 0=通过 2=规范失败）",
	RunE:  runSkillsValidate,
}

func runSkillsValidate(cmd *cobra.Command, args []string) error {
	canonical, _, err := resolveCanonical()
	if err != nil {
		return err
	}
	names, err := skillsdist.ListSkills(canonical)
	if err != nil {
		return err
	}
	if len(skValSkill) > 0 {
		names, err = filterSkillNames(names, skValSkill)
		if err != nil {
			return err
		}
	}

	type res struct {
		Name       string   `json:"name"`
		Pass       bool     `json:"pass"`
		Issues     []string `json:"issues,omitempty"`
		Advisories []string `json:"advisories,omitempty"`
	}
	results := make([]res, 0, len(names))
	failCount := 0
	for _, n := range names {
		rep, qerr := skillsqa.AuditSkill(filepath.Join(canonical, n))
		if qerr != nil {
			results = append(results, res{Name: n, Pass: false, Issues: []string{"审查失败: " + qerr.Error()}})
			failCount++
			continue
		}
		results = append(results, res{Name: n, Pass: rep.Pass, Issues: rep.Issues, Advisories: rep.Advisories})
		if !rep.Pass {
			failCount++
		}
	}

	if skValJSON {
		out := struct {
			Canonical string `json:"canonical"`
			Total     int    `json:"total"`
			Failed    int    `json:"failed"`
			Results   []res  `json:"results"`
		}{canonical, len(names), failCount, results}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("canonical: %s  (%d skill)\n", canonical, len(names))
		for _, r := range results {
			mark := "✓"
			if !r.Pass {
				mark = "✗"
			}
			fmt.Printf("  %s %s\n", mark, r.Name)
			for _, iss := range r.Issues {
				fmt.Printf("      - %s\n", iss)
			}
			for _, a := range r.Advisories {
				fmt.Printf("      · %s [建议]\n", a)
			}
		}
		fmt.Printf("通过 %d / 失败 %d\n", len(names)-failCount, failCount)
	}

	if failCount > 0 {
		os.Exit(2)
	}
	return nil
}

// filterSkillNames filters skill names by whitelist (CLI layer, preserves order).
// Any requested name not present in the canonical library is an error — silently
// filtering a misspelled --skill down to an empty set would let `audit --gate`
// pass with 0 skills scanned, bypassing the CI security gate with a typo.
//
// filterSkillNames 按白名单过滤 skill 名（CLI 层，保持 order）。任何请求的名字
// 不在 canonical 库中直接报错——静默把拼错的 --skill 过滤成空集会让 `audit --gate`
// 扫 0 个 skill 通过，CI 安全门被拼写错误绕过。
func filterSkillNames(all, want []string) ([]string, error) {
	set := map[string]bool{}
	for _, a := range all {
		set[a] = true
	}
	wantSet := map[string]bool{}
	for _, w := range want {
		if !set[w] {
			return nil, fmt.Errorf("skill %q 不在 canonical 库中", w)
		}
		wantSet[w] = true
	}
	out := make([]string, 0, len(want))
	for _, a := range all {
		if wantSet[a] {
			out = append(out, a)
		}
	}
	return out, nil
}

func init() {
	skillsValidateCmd.Flags().StringSliceVar(&skValSkill, "skill", nil, "只校验指定 skill（可重复）")
	skillsValidateCmd.Flags().BoolVar(&skValJSON, "json", false, "JSON 输出")
	skillsCmd.AddCommand(skillsValidateCmd)
}
