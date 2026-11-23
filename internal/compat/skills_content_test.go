package compat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillsContentDirs 守住"删了 skills 脚手架包，但技能内容目录仍然可用"这条线：
// core-only 之后 forge 不再挂载 skills 命令，技能内容只在 skills/ 与 skills-forge/
// 里按路径被读取（taskpipeline 的 skill-decisions 门禁、compat 的载荷面）。
// 目录在、SKILL.md 在、frontmatter 的 name 与目录同名、description 非空——缺一项
// 就等于技能静默消失：没有命令报错，也没有测试发现。
func TestSkillsContentDirs(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, dir := range []string{"skills", "skills-forge"} {
		base := filepath.Join(root, dir)
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		dirs := 0
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dirs++
			skillPkg := dir + "/" + e.Name()
			p := filepath.Join(base, e.Name(), "SKILL.md")
			b, rerr := os.ReadFile(p)
			if rerr != nil {
				t.Errorf("%s: 缺 SKILL.md（技能内容面断档）：%v", skillPkg, rerr)
				continue
			}
			name, description := frontmatterField(string(b), "name"), frontmatterField(string(b), "description")
			if name == "" {
				t.Errorf("%s: frontmatter 缺 name", skillPkg)
			} else if name != e.Name() {
				t.Errorf("%s: frontmatter name = %q，必须与目录同名", skillPkg, name)
			}
			if strings.TrimSpace(description) == "" {
				t.Errorf("%s: frontmatter 缺 description（路由靠它，空等于技能不可发现）", skillPkg)
			}
		}
		if dirs == 0 {
			t.Fatalf("%s 下没有任何技能目录——内容面被删空了？", dir)
		}
	}
}

// frontmatterField 从 --- 围起来的头部里取一个标量字段（去掉包裹的引号）。
func frontmatterField(src, key string) string {
	lines := strings.Split(src, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "---" {
			return ""
		}
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		v := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		return v
	}
	return ""
}
