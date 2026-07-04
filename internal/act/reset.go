package act

import (
	"os"

	"github.com/MjxUpUp/Forge/internal/forgedata"
)

// ResetForRebuild 备份现有 conclusions.jsonl 到 .bak（如有）并清空原位，供 act rebuild
// 全量重建。无现有文件返空备份路径（合法状态——旧项目本就没有，不报错）。os.Rename 既
// 备份又清空原位（删旧建新），Append 随后会在原位重建文件。
func ResetForRebuild(p *forgedata.Project) (string, error) {
	path := p.ActConclusionsPath()
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	backup := path + ".bak"
	if err := os.Rename(path, backup); err != nil {
		return "", err
	}
	return backup, nil
}
