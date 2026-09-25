package skillsdist

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/MjxUpUp/Forge/internal/skillsfm"
)

// profileFileName 是项目级分发画像文件名，落点 <project>/.forge/skills-profile。
// 每行一个 skill 名；`#` 起注释；空行跳过。文件不存在 = 无画像 = 全量分发（默认）。
// 文件存在 = 白名单：只有列出的 skill 分发到项目目标。
const profileFileName = "skills-profile"

// ProfilePath returns <forgeRoot>/.forge/skills-profile (forgeRoot = project root found by projectroot.Find).
//
// ProfilePath 返回 <forgeRoot>/.forge/skills-profile（forgeRoot = projectroot.Find
// 找到的项目根）。
func ProfilePath(forgeRoot string) string {
	return filepath.Join(forgeRoot, ".forge", profileFileName)
}

// LoadProfile reads the project distribution profile.
//
// LoadProfile 读取项目分发画像。文件不存在返回 (nil, nil)（常见无画像路径）。
// 文件存在但为空（全注释/全空行）返回非 nil 空 slice——「画像生效、白名单为空」
// ——下游必须用 `!= nil` 区分，绝不能用 `len() > 0`（空画像回退全量会让用户
// 调试画像时被静默反向放大到全量）。文件格式错误是硬错误——静默忽略画像会
// 分发出全量集合，而用户以为已裁剪，整个特性落空。非法 skill 名（空格/路径
// 分隔符/非 basename）同为硬错误，对齐 backupTarget 的路径注入防御。
func LoadProfile(forgeRoot string) ([]string, error) {
	data, err := os.ReadFile(ProfilePath(forgeRoot))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := []string{}
	seen := map[string]bool{}
	for i, line := range strings.Split(string(data), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") {
			continue
		}
		if !skillsfm.IsValidSkillName(name) || strings.ContainsAny(name, " \t") {
			return nil, fmt.Errorf("%s 第 %d 行非法 skill 名 %q（每行一个规范 skill 名）",
				ProfilePath(forgeRoot), i+1, name)
		}
		if !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
	}
	return out, nil
}

// filterByProfile 把 canonical 名单过滤到画像白名单（保持 canonical 顺序）。
// 与 --skill（filterNames）不同，画像里不在 canonical 的条目不是硬错误——画像是
// 持久配置，引用上游已移除的 skill 是合法状态；作为 `unknown` 返回给调用方以
// 告警呈现，不中止整个 install。
func filterByProfile(all, profile []string) (kept, unknown []string) {
	if profile == nil {
		return all, nil // 无画像 = 全量直通；空画像（非 nil）= 白名单为空 = 一个不留
	}
	set := make(map[string]bool, len(all))
	for _, a := range all {
		set[a] = true
	}
	profSet := make(map[string]bool, len(profile))
	for _, p := range profile {
		if !set[p] {
			unknown = append(unknown, p)
			continue
		}
		profSet[p] = true
	}
	for _, a := range all {
		if profSet[a] {
			kept = append(kept, a)
		}
	}
	slices.Sort(unknown)
	return kept, unknown
}
