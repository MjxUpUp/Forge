package skillsfm

import "strings"

// IsValidSkillName 校验 skill 名是否为可安全拼入文件路径的规范 basename：
// 非空、非 . / ..、不含路径分隔符。所有把用户输入（--skill flag、外部
// canonical 来源）的 skill 名拼进文件路径的入口（install backup、eval
// 数据文件、decisions.md）都必须先过此校验，防路径遍历写出目录外。
// 单一真相源：skillsdist.isSafeName 的历史实现已收敛到这里。
func IsValidSkillName(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	return true
}
