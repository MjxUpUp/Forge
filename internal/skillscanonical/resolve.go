// Package skillscanonical resolves forge's skill canonical library directory
// (canonical) — descended from cli, shared internally so packages share one
// parser and avoid copy drift.
//
// Priority: $FORGE_SKILLS_CANONICAL > built-in embed library (extracted to a
// persistent cache). (--canonical flag is a CLI-layer concept, stays in the
// cli package; when the flag hits, it short-circuits this package.)
//
// Package skillscanonical 解析 forge 的 skill 规范库目录（canonical）——从 cli 下沉，
// 供内部包共用同一份解析，避免复制漂移。
//
// 优先级：$FORGE_SKILLS_CANONICAL > 内置 embed 库（解压到持久缓存）。
// （--canonical flag 是 CLI 层概念，留在 cli 包，flag 命中时短路本包。）
package skillscanonical

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MjxUpUp/Forge/skills"
)

// EnvName is the environment variable name that overrides the built-in embed
// library.
//
// EnvName 是覆盖内置 embed 库的环境变量名。
const EnvName = "FORGE_SKILLS_CANONICAL"

// VersionFile is the version marker filename in the embed cache directory;
// its content = the forge version at extract time. Resolve compares it:
// marker missing or not equal to the current version (forge upgrade) →
// re-extract.
//
// VersionFile 是 embed 缓存目录的版本标记文件名，内容 = extract 时的 forge 版本。
// Resolve 比较它：标记缺失或不等于当前版本（forge 升级）→ re-extract。
const VersionFile = ".embedded-version"

// Resolve resolves the canonical directory and whether it is an external real
// source.
//
//	env override (FORGE_SKILLS_CANONICAL): returns (env path, true, nil)
//	otherwise embed fallback: returns (cache dir, false, nil)
//
// version is for embed cache version validation (refresh after upgrade).
// isExternal=false means it comes from the embed extraction cache.
//
// Resolve 解析 canonical 目录与是否外部真实源。
//
//	env 覆盖（FORGE_SKILLS_CANONICAL）：返回 (env路径, true, nil)
//	否则 embed fallback：返回 (缓存目录, false, nil)
//
// version 用于 embed 缓存版本校验（升级后刷新）。isExternal=false 表示来自 embed 解压缓存。
func Resolve(version string) (string, bool, error) {
	if env := os.Getenv(EnvName); env != "" {
		if _, err := os.Stat(env); err != nil {
			return "", false, fmt.Errorf("$%s 路径不存在: %s", EnvName, env)
		}
		return env, true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	cacheDir := filepath.Join(home, ".forge", "skills-cache", "embedded")
	if err := EnsureEmbeddedCache(cacheDir, version); err != nil {
		return "", false, fmt.Errorf("解压内置 skill 库失败: %w", err)
	}
	return cacheDir, false, nil
}

// EnsureEmbeddedCache ensures cacheDir is the embed snapshot for version.
// CONVENTIONS.md exists and version marker == version → reuse (do not touch
// the cache, avoid re-extracting 1.5M each time); otherwise (first run /
// upgrade / corrupted marker) → RemoveAll + ExtractTo + write a new version
// marker. Pure function (cacheDir/version parameterized) for testability,
// decoupled from global home/version.
//
// EnsureEmbeddedCache 保证 cacheDir 是 version 对应的 embed 快照。
// CONVENTIONS.md 存在且版本标记 == version → 复用(不动缓存，避免每次重解压 1.5M)；
// 否则(首次/升级/标记损坏) → RemoveAll + ExtractTo + 写新版本标记。
// 纯函数（cacheDir/version 参数化）便于测试，解耦全局 home/version。
func EnsureEmbeddedCache(cacheDir, version string) error {
	conv := filepath.Join(cacheDir, "CONVENTIONS.md")
	versionFile := filepath.Join(cacheDir, VersionFile)
	if _, statErr := os.Stat(conv); statErr == nil {
		if cached, rerr := os.ReadFile(versionFile); rerr == nil && string(cached) == version {
			return nil // 版本一致：复用缓存
		}
	}
	// cache missing or version mismatch (upgrade): full rebuild, ExtractTo
	// will MkdirAll cacheDir.
	//
	// 缓存缺失或版本不一致(升级)：整体重建，ExtractTo 会 MkdirAll cacheDir
	_ = os.RemoveAll(cacheDir)
	if err := skills.ExtractTo(cacheDir); err != nil {
		return err
	}
	return os.WriteFile(versionFile, []byte(version), 0644)
}
