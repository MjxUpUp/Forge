package cli

import (
	"encoding/json"
	"fmt"

	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

var skillsListJSON bool

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出 canonical skill 库中的 skill",
	RunE: func(cmd *cobra.Command, args []string) error {
		canonical, _, err := resolveCanonical()
		if err != nil {
			return err
		}
		names, err := skillsdist.ListSkills(canonical)
		if err != nil {
			return err
		}
		if skillsListJSON {
			type item struct {
				Name string `json:"name"`
			}
			items := make([]item, 0, len(names))
			for _, n := range names {
				items = append(items, item{Name: n})
			}
			out := struct {
				Canonical string `json:"canonical"`
				Count     int    `json:"count"`
				Skills    []item `json:"skills"`
			}{Canonical: canonical, Count: len(names), Skills: items}
			b, _ := json.MarshalIndent(out, "", "  ")
			fmt.Println(string(b))
			return nil
		}
		fmt.Printf("canonical: %s\n", canonical)
		fmt.Printf("共 %d 个 skill:\n", len(names))
		for _, n := range names {
			fmt.Printf("  %s\n", n)
		}
		return nil
	},
}

func init() {
	skillsListCmd.Flags().BoolVar(&skillsListJSON, "json", false, "JSON 输出")
	skillsCmd.AddCommand(skillsListCmd)
}
