package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// TestEmbedSplitCoverage pins the P7 split contract (2026-09 census): every
// roster entry (embeddedHooks) has its script constant declared in exactly one
// embed_*.go file — no hook lost, no duplicate across the split files.
//
// TestEmbedSplitCoverage 钉住 P7 拆分契约（2026-09 普查）：名册（embeddedHooks）
// 的每个条目，其脚本常量在且仅在一个 embed_*.go 文件中声明——拆分不丢 hook、
// 不跨文件重复。
func TestEmbedSplitCoverage(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Skip("runtime.Caller unavailable")
	}
	dir := filepath.Dir(thisFile)

	names := make([]string, 0, len(embeddedHooks))
	for name := range embeddedHooks {
		names = append(names, name)
	}

	declRe := regexp.MustCompile(`^const ([A-Z]\w*Hook) = `)
	fileDecls := map[string][]string{} // const 名 → 出现的文件
	bases, err := filepath.Glob(filepath.Join(dir, "embed*.go"))
	if err != nil {
		t.Skipf("glob unavailable: %v", err)
	}
	for _, path := range bases {
		base := filepath.Base(path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("源码文件不可读（无源码环境）: %v", err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			if m := declRe.FindStringSubmatch(line); m != nil {
				fileDecls[m[1]] = append(fileDecls[m[1]], base)
			}
		}
	}

	kebabToPascal := func(kebab string) string {
		parts := strings.Split(kebab, "-")
		for i, p := range parts {
			if p != "" {
				parts[i] = strings.ToUpper(p[:1]) + p[1:]
			}
		}
		return strings.Join(parts, "")
	}

	constsByPascal := map[string]string{} // PascalCase 常量名 → 名册 kebab 名
	for _, name := range names {
		constsByPascal[kebabToPascal(name)+"Hook"] = name
	}

	for _, name := range names {
		constName := kebabToPascal(name) + "Hook"
		locs := fileDecls[constName]
		switch {
		case len(locs) == 0:
			t.Errorf("名册条目 %q 的常量 %s 未在任何 embed_*.go 声明——拆分丢失", name, constName)
		case len(locs) > 1:
			// 同包重复 const 声明编译期即报错——此分支实为不可达的防御性断言。
			t.Errorf("常量 %s 跨文件重复声明: %v", constName, locs)
		}
	}

	// 反向：分文件里的每个声明都对应名册条目（防分文件私货）。
	for constName, locs := range fileDecls {
		if _, ok := constsByPascal[constName]; !ok {
			t.Errorf("%s 声明了名册之外的 %s", locs, constName)
		}
	}
}
