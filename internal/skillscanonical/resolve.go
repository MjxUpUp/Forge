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
	skillsforge "github.com/MjxUpUp/Forge/skills-forge"
)

// EnvName is the environment variable name that overrides the built-in embed
// library.
//
// EnvName 是覆盖内置 embed 库的环境变量名。
const EnvName = "FORGE_SKILLS_CANONICAL"

// ForgeVersionFile is the version marker for the forge-native overlay
// (skills-forge/ → same cache dir, additive). Independent of VersionFile so a
// reused pre-migration neutral cache still triggers the overlay extract.
//
// ForgeVersionFile 是 forge 原生覆盖层（skills-forge/ → 同一缓存目录，增量）的
// 版本标记。与 VersionFile 独立——迁移前复用的旧中立缓存也会触发覆盖层解压。
const ForgeVersionFile = ".embedded-forge-version"

// VersionFile is the version marker filename in the embed cache directory;
// its content = the forge version at extract time. Resolve compares it:
// marker missing or not equal to the current version (forge upgrade) →
// re-extract.
//
// VersionFile 是 embed 缓存目录的版本标记文件名，内容 = extract 时的 forge 版本。
// Resolve 比较它：标记缺失或不等于当前版本（forge 升级）→ re-extract。
const VersionFile = ".embedded-version"

// EmbeddedCacheDir returns the embed extraction cache dir under the user home
// (path computation only, no disk touch). Exported so read-only consumers
// (e.g. the dashboard) resolve the same location as Resolve without copying
// the path logic — single truth source for the cache path.
//
// EmbeddedCacheDir 返回用户 home 下的 embed 解压缓存目录（只算路径，不碰盘）。
// 导出供只读消费方（如 dashboard）与 Resolve 解析同一位置，避免复制路径逻辑——
// 缓存路径的单一真相源。
func EmbeddedCacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge", "skills-cache", "embedded"), nil
}

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
	cacheDir, err := EmbeddedCacheDir()
	if err != nil {
		return "", false, err
	}
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
// Two layers since the 2026-08 zero-reverse-dependency migration: the neutral
// skills/ snapshot (this function's original lifecycle) + the forge-native
// overlay from skills-forge/ (ensureForgeOverlay, own marker, additive extract
// into the same dir). Merged layout on purpose: single-root consumers
// (ListSkills / skilltrigger.LoadAll / install / adapters) see the full set
// with zero changes.
//
// EnsureEmbeddedCache 保证 cacheDir 是 version 对应的 embed 快照。
// CONVENTIONS.md 存在且版本标记 == version → 复用(不动缓存，避免每次重解压 1.5M)；
// 否则(首次/升级/标记损坏) → RemoveAll + ExtractTo + 写新版本标记。
// 纯函数（cacheDir/version 参数化）便于测试，解耦全局 home/version。
//
// 2026-08 零反向依赖迁移后分两层：中立 skills/ 快照（本函数原有生命周期）+
// skills-forge/ 的 forge 原生覆盖层（ensureForgeOverlay，独立标记，增量解压进同一
// 目录）。刻意用合并布局：单根消费者（ListSkills / skilltrigger.LoadAll /
// install / adapters）零改动即可看到全量集。
func EnsureEmbeddedCache(cacheDir, version string) error {
	conv := filepath.Join(cacheDir, "CONVENTIONS.md")
	versionFile := filepath.Join(cacheDir, VersionFile)
	reusable := false
	if _, statErr := os.Stat(conv); statErr == nil {
		if cached, rerr := os.ReadFile(versionFile); rerr == nil && string(cached) == version {
			reusable = true // 版本一致：复用缓存
		}
	}
	if !reusable {
		// cache missing or version mismatch (upgrade): full rebuild, ExtractTo
		// will MkdirAll cacheDir. The version marker is removed FIRST — if the
		// RemoveAll below dies midway (Windows locked handle), the surviving
		// half-deleted cache must NOT look reusable: marker missing forces a
		// rebuild on the next run instead of a permanently broken cache that
		// passes the CONVENTIONS.md + marker check (review W1).
		//
		// 缓存缺失或版本不一致(升级)：整体重建，ExtractTo 会 MkdirAll cacheDir。
		// 版本标记先删——下方 RemoveAll 中途失败时（Windows 文件锁），幸存的半删除
		// 缓存绝不能看起来可复用：标记缺失强制下次重建，而不是一个通过
		// CONVENTIONS.md + 标记检查的永久损坏缓存（review W1）。
		_ = os.Remove(versionFile)
		if err := os.RemoveAll(cacheDir); err != nil {
			return fmt.Errorf("清理旧缓存 %s 失败: %w", cacheDir, err)
		}
		if err := skills.ExtractTo(cacheDir); err != nil {
			return err
		}
		if err := os.WriteFile(versionFile, []byte(version), 0644); err != nil {
			return err
		}
	}
	return ensureForgeOverlay(cacheDir, version)
}

// ensureForgeOverlay ensures the forge-native skills-forge/ overlay is extracted
// into cacheDir at version (own marker, independent of the neutral layer: a
// reused neutral cache from before the migration lacks the overlay — the marker
// mismatch triggers the additive extract without touching neutral content).
//
// ensureForgeOverlay 保证 skills-forge/ 的 forge 原生覆盖层已按 version 解压进
// cacheDir（独立标记，与中立层互不干扰：迁移前复用的旧中立缓存没有覆盖层——
// 标记不匹配即触发增量解压，不动中立内容）。
func ensureForgeOverlay(cacheDir, version string) error {
	marker := filepath.Join(cacheDir, ForgeVersionFile)
	if cached, rerr := os.ReadFile(marker); rerr == nil && string(cached) == version {
		return nil
	}
	if err := skillsforge.ExtractTo(cacheDir); err != nil {
		return fmt.Errorf("解压 forge 原生 skill 覆盖层失败: %w", err)
	}
	return os.WriteFile(marker, []byte(version), 0644)
}
