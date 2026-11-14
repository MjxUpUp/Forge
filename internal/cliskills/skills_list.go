package cliskills

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/MjxUpUp/Forge/internal/skillscanonical"
	"github.com/MjxUpUp/Forge/internal/skillsdist"
	"github.com/spf13/cobra"
)

var skillsListJSON bool

var skillsListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出 canonical skill 库中的 skill",
	RunE: func(cmd *cobra.Command, args []string) error {
		// 没有显式 canonical 源时直接从内置 embed 库读，不解压、不落盘
		// （阶段 D 收尾：唯一会往磁盘写的只剩需要真实路径的子命令，
		// 且它们解压到系统临时目录，不再碰用户 home）。
		explicit := skillsCanonicalFlag != "" || os.Getenv(skillscanonical.EnvName) != ""
		canonical := "(内置 embed 库，未落盘)"
		var names []string
		var err error
		if explicit {
			canonical, _, err = ResolveCanonical()
			if err != nil {
				return err
			}
			names, err = skillsdist.ListSkills(canonical)
		} else {
			names, err = skillsdist.ListEmbeddedSkills()
		}
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
	Root.AddCommand(skillsListCmd)
}
