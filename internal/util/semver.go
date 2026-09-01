package util

import (
	"strconv"
	"strings"
)

// semver.go —— semver 风格版本比较（自 cli/update.go 迁入，2026-09 普查 A2-2：
// hook 分发包的 kimi-stale 探测需要它，cli 的 update 域同样需要——纯函数下沉
// 单一源，两侧不再互拷）。

// GetCurrentVersion extracts the bare version from a full version string
// (SetVersion 形态: "X.Y.Z (commit: ..., built: ...)"); "dev" passes through.
//
// GetCurrentVersion 从 SetVersion 设置的完整 version 串（如 X.Y.Z (commit: ..., built: ...)）
// 中提取裸版本号（如 0.11.1）。dev 原样返回。
func GetCurrentVersion(fullVersion string) string {
	if fullVersion == "dev" {
		return "dev"
	}
	// 在第一个空格/括号前提取 version
	idx := strings.IndexByte(fullVersion, ' ')
	if idx > 0 {
		return fullVersion[:idx]
	}
	return fullVersion
}

// CompareVersions compares two semver-style version strings ("0.11.1" vs "0.12.0").
// Returns 1/0/-1 for a>/=/b. Equal numeric cores tie-break by semver §11: release
// beats pre-release (0.12.0 > 0.12.0-beta.1 — otherwise beta users never receive
// the release); two pre-releases compare by dot segments (numeric numerically,
// numeric < alphabetic, alphabetic by ASCII; fewer segments sorts lower on prefix
// equality).
//
// CompareVersions 比较两个 semver 风格的 version 串（如 0.11.1 对 0.12.0）。
// 返回：a > b 返 1，a == b 返 0，a < b 返 -1。数字核心相等时按 semver §11
// tie-break pre-release：正式版高于 pre-release（0.12.0 > 0.12.0-beta.1——否则
// beta 用户永远收不到正式版）；两个 pre-release 按 . 分段比较（数字段按数值、
// 数字段 < 字母段、字母段按 ASCII；前缀全同时段数少者小）。
func CompareVersions(a, b string) int {
	aCore, aPre := splitPreRelease(a)
	bCore, bPre := splitPreRelease(b)

	if c := compareVersionCores(aCore, bCore); c != 0 {
		return c
	}
	if aPre == bPre {
		return 0
	}
	if aPre == "" {
		return 1 // release > pre-release (semver §11)
	}
	if bPre == "" {
		return -1
	}
	return comparePreReleases(aPre, bPre)
}

// splitPreRelease 把 version 串拆成数字核心与 pre-release 后缀（无则 ""）。
func splitPreRelease(v string) (core, pre string) {
	if idx := strings.IndexByte(v, '-'); idx > 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

// compareVersionCores 比较点分数字核心。
func compareVersionCores(aCore, bCore string) int {
	aParts := strings.Split(aCore, ".")
	bParts := strings.Split(bCore, ".")

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		av := 0
		bv := 0
		if i < len(aParts) {
			av = parseVersionPart(aParts[i])
		}
		if i < len(bParts) {
			bv = parseVersionPart(bParts[i])
		}
		if av != bv {
			if av > bv {
				return 1
			}
			return -1
		}
	}
	return 0
}

// comparePreReleases 按 semver §11.4 比较两个 pre-release 后缀：. 分段，
// 数字段按数值且低于字母段，字母段按 ASCII，前缀段全等时段数少者低。
func comparePreReleases(aPre, bPre string) int {
	aSegs := strings.Split(aPre, ".")
	bSegs := strings.Split(bPre, ".")

	maxLen := len(aSegs)
	if len(bSegs) > maxLen {
		maxLen = len(bSegs)
	}

	for i := 0; i < maxLen; i++ {
		if i >= len(aSegs) {
			return -1 // a ran out of identifiers: a < b
		}
		if i >= len(bSegs) {
			return 1
		}
		aNum, aIsNum := numericIdentifier(aSegs[i])
		bNum, bIsNum := numericIdentifier(bSegs[i])
		switch {
		case aIsNum && bIsNum:
			if aNum != bNum {
				if aNum > bNum {
					return 1
				}
				return -1
			}
		case aIsNum: // numeric < alphanumeric
			return -1
		case bIsNum:
			return 1
		default:
			if c := strings.Compare(aSegs[i], bSegs[i]); c != 0 {
				if c > 0 {
					return 1
				}
				return -1
			}
		}
	}
	return 0
}

// numericIdentifier 判断 pre-release 分段是否纯数字，是则返回其数值。
func numericIdentifier(s string) (int, bool) {
	if s == "" || s[0] == '+' || s[0] == '-' {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseVersionPart(s string) int {
	// 剥离非数字前缀/后缀（如 rc1 错误地算成 1，直接返 0）
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			break
		}
	}
	return n
}
