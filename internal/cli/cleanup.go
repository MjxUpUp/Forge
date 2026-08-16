package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/protocol"
	"github.com/MjxUpUp/Forge/internal/util"
)

// cleanup.go — strip forge's project-level writes from existing projects after the
// user-level-assets refactor.
//
// cleanup.go —— user-level-assets 重构后，剥除存量项目里的项目级 forge 写入。
//
// Background: pre-refactor forge init/sync wrote `.forge/hooks/`, `.claude/` (settings/
// CLAUDE.md/skills), `AGENTS.md`, and agent-bridge files (`.codex/`, `.cursor/`,
// `.windsurf*`, `.github/instructions/`, `.clinerules/`, `.opencode/`) into the user's
// project — the "too invasive, accidentally committed" complaint. All of these now live
// at user level; autoSync runs this stripper whenever it fires (version change / force /
// dirty binding — not literally every command: an equal stamp short-circuits autoSync
// before this runs), so existing projects converge to zero-project-write by simply
// using forge.
//
// 背景：重构前 forge init/sync 往用户项目写 `.forge/hooks/`、`.claude/`（settings/
// CLAUDE.md/skills）、`AGENTS.md` 与各 agent bridge 文件（`.codex/`、`.cursor/`、
// `.windsurf*`、`.github/instructions/`、`.clinerules/`、`.opencode/`）——正是
// "侵入性太强、一不小心就提交"的投诉来源。这些现在全部在用户级；autoSync 触发时
// （版本变化/force/脏绑定——不是每条命令：stamp 相等时 autoSync 提前返回，轮不到
// 本清理）跑本清理，存量项目只要继续用 forge 即收敛为零项目写入。
//
// Boundaries:
//   - team mode (`forge init --project`, marker .forge/team-mode) is exempt — those
//     projects deliberately keep project-level assets for git sharing.
//   - .forge/protocol.yml is NOT blindly deleted: content equal to DefaultProtocol
//     (never user-edited) is migrated to the DataDir copy; a customized file stays
//     as the team-shared override layer.
//   - files with user content are never deleted — only forge-sourced entries/sections
//     are stripped (settings.local.json keeps its shell, CLAUDE.md/AGENTS.md keep
//     non-forge content, mixed hooks.json keeps user hooks).
//
// 边界：
//   - 团队模式（`forge init --project`，标记 .forge/team-mode）豁免——那些项目
//     刻意保留项目级资产做 git 共享。
//   - .forge/protocol.yml 不盲删：内容与 DefaultProtocol 相等（用户没改过）迁移到
//     DataDir 副本；改过的文件留作团队共享覆盖层。
//   - 含用户内容的文件绝不删——只剥 forge 来源的条目/段（settings.local.json 留壳、
//     CLAUDE.md/AGENTS.md 留非 forge 内容、混合 hooks.json 留用户 hook）。

// forge 指令段的标记对（与 skillgen 内未导出的常量同值；cleanup 属 cli 包，
// 本地持有以避免跨包导出仅为清理服务的符号）。
const (
	forgeSectionStartMarker = "<!-- FORGE:START -->"
	forgeSectionEndMarker   = "<!-- FORGE:END -->"
)

// teamModeMarker is the marker file written by `forge init --project`: projects in
// team mode deliberately keep project-level assets, so the stripper skips them.
//
// teamModeMarker 是 `forge init --project` 写入的标记文件：团队模式项目刻意
// 保留项目级资产，清理器跳过它们。
const teamModeMarker = "team-mode"

// stripProjectLevelForgeAssets removes forge's legacy project-level writes from dir.
// Idempotent; every step is a cheap stat/no-op when already converged. Called by
// autoSync when it fires (version change / force / dirty binding) and by forge init.
//
// stripProjectLevelForgeAssets 剥除 dir 里遗留的项目级 forge 写入。幂等；
// 收敛后每步都是廉价 stat/no-op。由 autoSync 触发时（版本变化/force/脏绑定）
// 与 forge init 调用。
func stripProjectLevelForgeAssets(dir string) {
	// Team mode is exempt.
	//
	// 团队模式豁免。
	if _, err := os.Stat(filepath.Join(dir, ".forge", teamModeMarker)); err == nil {
		return
	}

	// 1. .forge/: hooks copies + sync stamp are pure residue (runtime never read them;
	//    the copies now live in DataDir). protocol.yml is migrated when it carries no
	//    user edits.
	//
	// 1. .forge/：hooks 副本与 sync 戳是纯残留（运行时从不读它们；副本现在在
	//    DataDir）。protocol.yml 无用户改动时迁移。
	os.RemoveAll(filepath.Join(dir, ".forge", "hooks"))
	os.Remove(filepath.Join(dir, ".forge", ".sync-version"))
	migrateProjectProtocol(dir)
	removeDirIfEmpty(filepath.Join(dir, ".forge"))

	// 2. .claude/: settings hooks (keep the shell), forge-quality skill (moved to
	//    user level), CLAUDE.md forge section.
	//
	// 2. .claude/：settings hooks（留壳）、forge-quality skill（已迁用户级）、
	//    CLAUDE.md 的 forge 段。
	if _, err := hooks.StripForgeHooks(dir, true); err != nil {
		warnCleanup("strip .claude/settings.local.json hooks", err)
	}
	os.RemoveAll(filepath.Join(dir, ".claude", "skills", "forge-quality"))
	removeDirIfEmpty(filepath.Join(dir, ".claude", "skills"))
	stripMarkedSectionFile(filepath.Join(dir, ".claude", "CLAUDE.md"))
	removeDirIfEmpty(filepath.Join(dir, ".claude"))

	// 3. Project-root AGENTS.md forge section.
	//
	// 3. 项目根 AGENTS.md 的 forge 段。
	stripMarkedSectionFile(filepath.Join(dir, "AGENTS.md"))

	// 4. Agent bridge files.
	//
	// 4. 各 agent bridge 文件。
	stripHooksJSON(filepath.Join(dir, ".codex", "hooks.json"))
	removeDirIfEmpty(filepath.Join(dir, ".codex"))

	stripHooksJSON(filepath.Join(dir, ".cursor", "hooks.json"))
	os.Remove(filepath.Join(dir, ".cursor", "rules", "forge-quality.mdc"))
	removeDirIfEmpty(filepath.Join(dir, ".cursor", "rules"))
	removeDirIfEmpty(filepath.Join(dir, ".cursor"))

	stripHooksJSON(filepath.Join(dir, ".windsurf", "hooks.json"))
	removeDirIfEmpty(filepath.Join(dir, ".windsurf"))
	stripMarkedSectionFile(filepath.Join(dir, ".windsurfrules"))

	os.Remove(filepath.Join(dir, ".github", "instructions", "forge-quality.instructions.md"))
	removeDirIfEmpty(filepath.Join(dir, ".github", "instructions"))
	// .github itself is never removed — it is a standard user dir.
	//
	// .github 本身绝不删——它是用户的标准目录。

	os.Remove(filepath.Join(dir, ".clinerules", "forge-quality.md"))
	removeDirIfEmpty(filepath.Join(dir, ".clinerules"))

	os.Remove(filepath.Join(dir, ".opencode", "plugins", "forge.ts"))
	os.Remove(filepath.Join(dir, ".opencode", "forge.README.md"))
	removeDirIfEmpty(filepath.Join(dir, ".opencode", "plugins"))
	removeDirIfEmpty(filepath.Join(dir, ".opencode"))
}

// migrateProjectProtocol moves a project-level .forge/protocol.yml into the DataDir
// copy when it carries no user edits (semantically equal to DefaultProtocol). A
// customized protocol.yml is kept untouched — it is the team-shared override layer.
//
// migrateProjectProtocol 把项目级 .forge/protocol.yml 在无用户改动时（与
// DefaultProtocol 语义相等）迁入 DataDir 副本。改过的 protocol.yml 原样保留——
// 它是团队共享覆盖层。
func migrateProjectProtocol(dir string) {
	path := filepath.Join(dir, ".forge", "protocol.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Strict decode: a file with fields unknown to Protocol is treated as
	// user-modified and kept. A lenient unmarshal would silently drop those fields
	// on re-marshal, make a customized file look identical to the default, and get
	// it deleted — losing the user's edits without a trace.
	//
	// 严格解码：含 Protocol 未知字段的文件视为「用户改过」保留。宽松 unmarshal
	// 会在重 marshal 时静默丢掉这些字段，把改过的文件误判成与默认相等而删掉——
	// 用户的改动静默丢失。
	var p protocol.Protocol
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return // 解析失败或含未知字段——不碰（用户文件）
	}
	def := protocol.DefaultProtocol()
	defYAML, err := yaml.Marshal(def)
	if err != nil {
		return
	}
	// Semantic compare via re-marshal: field order/formatting of the on-disk file
	// must not matter.
	//
	// 经重 marshal 做语义比较：磁盘文件的字段顺序/格式不影响判定。
	gotYAML, err := yaml.Marshal(&p)
	if err != nil {
		return
	}
	if string(gotYAML) != string(defYAML) {
		return // 用户改过——保留为团队覆盖层
	}
	// No user edits: ensure the DataDir copy exists, then drop the project-level
	// file. The copy is written explicitly to the DataDir path (SaveDataDir).

	// The project file is removed ONLY after the copy is verified on disk: a
	// non-IsNotExist stat error (permission/IO) means "unverified", not "absent",
	// and deleting then would strand the project with no protocol at all.
	//
	// 无用户改动：确保 DataDir 副本存在，然后删项目级文件。副本显式写到 DataDir
	// 路径（SaveDataDir）——旧路由 protocol.Save 已删除，副本不再经项目级文件中转。
	// 项目文件仅在副本确认落盘后才删：非 IsNotExist 的 stat 错误（权限/IO）是
	// 「未验证」而非「不存在」，此时删除会让项目没有任何 protocol 可用。
	ddPath, err := protocol.DataDirPath(dir)
	if err != nil {
		warnCleanup("resolve DataDir protocol path", err)
		return
	}
	if _, err := os.Stat(ddPath); err != nil {
		if !os.IsNotExist(err) {
			return // 副本状态未验证——不删项目文件
		}
		if err := protocol.SaveDataDir(dir, def); err != nil {
			warnCleanup("migrate protocol.yml to DataDir", err)
			return
		}
	}
	os.Remove(path)
}

// stripMarkedSectionFile removes the FORGE:START/END marked section from a file.
// When nothing but whitespace remains, the file is deleted (it was forge-created);
// otherwise the stripped content is written back atomically.
//
// stripMarkedSectionFile 剥除文件里的 FORGE:START/END 标记段。只剩空白时删文件
// （它是 forge 创建的）；否则原子写回剥除后的内容。
func stripMarkedSectionFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	content := string(data)
	if !strings.Contains(content, forgeSectionStartMarker) {
		return
	}
	stripped := util.ReplaceMarkedSection(content, ``, forgeSectionStartMarker, forgeSectionEndMarker)
	if strings.TrimSpace(stripped) == `` {
		os.Remove(path)
		return
	}
	if err := util.AtomicWrite(path, []byte(stripped), 0644); err != nil {
		warnCleanup("strip marked section "+path, err)
	}
}

// stripHooksJSON removes forge-sourced hook entries from an agent hooks.json
// (codex/cursor/windsurf). Two shapes are handled:
//   - nested (claude/codex): hooks.<event>[] = {matcher, hooks:[{type,command}]}
//   - flat (cursor/windsurf): hooks.<event>[] = {command,matcher,timeout}
//
// User entries (commands not starting with "forge hook"/"forge gate") are preserved.
// When no hooks remain, the file is deleted if it held only forge content
// (hooks + optional version); otherwise the stripped object is written back.
//
// stripHooksJSON 剥除 agent hooks.json（codex/cursor/windsurf）里 forge 来源的
// hook 条目。处理两种形态：
//   - 嵌套（claude/codex）：hooks.<event>[] = {matcher, hooks:[{type,command}]}
//   - 扁平（cursor/windsurf）：hooks.<event>[] = {command,matcher,timeout}
//
// 用户条目（command 不以 "forge hook"/"forge gate" 开头）保留。无 hook 剩余时，
// 文件只含 forge 内容（hooks + 可选 version）则删文件，否则写回剥除后的对象。
func stripHooksJSON(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return // 非 JSON 或损坏——不碰用户文件
	}
	hooksObj, ok := cfg[`hooks`].(map[string]any)
	if !ok {
		return
	}
	changed := false
	for event, v := range hooksObj {
		arr, ok := v.([]any)
		if !ok {
			continue
		}
		kept := make([]any, 0, len(arr))
		for _, item := range arr {
			entry, ok := item.(map[string]any)
			if !ok {
				kept = append(kept, item)
				continue
			}
			if inner, ok := entry[`hooks`].([]any); ok {
				// Nested shape: filter the inner hook list.
				//
				// 嵌套形态：过滤内层 hook 列表。
				innerKept := make([]any, 0, len(inner))
				for _, h := range inner {
					he, ok := h.(map[string]any)
					if !ok {
						innerKept = append(innerKept, h)
						continue
					}
					if cmd, _ := he[`command`].(string); isForgeCmd(cmd) {
						changed = true
						continue
					}
					innerKept = append(innerKept, h)
				}
				if len(innerKept) > 0 {
					entry[`hooks`] = innerKept
					kept = append(kept, entry)
				} else {
					changed = true
				}
				continue
			}
			// Flat shape.
			//
			// 扁平形态。
			if cmd, _ := entry[`command`].(string); isForgeCmd(cmd) {
				changed = true
				continue
			}
			kept = append(kept, item)
		}
		if len(kept) == 0 {
			delete(hooksObj, event)
			changed = true
		} else {
			hooksObj[event] = kept
		}
	}
	if !changed {
		return
	}
	if len(hooksObj) == 0 {
		// Only forge content? (hooks + optional version) → delete the file.
		//
		// 只有 forge 内容？（hooks + 可选 version）→ 删文件。
		onlyForge := true
		for k := range cfg {
			if k != `hooks` && k != `version` {
				onlyForge = false
				break
			}
		}
		if onlyForge {
			os.Remove(path)
			return
		}
		delete(cfg, `hooks`)
	}
	out, err := json.MarshalIndent(cfg, ``, `  `)
	if err != nil {
		return
	}
	if err := util.AtomicWrite(path, append(out, '\n'), 0644); err != nil {
		warnCleanup("strip hooks.json "+path, err)
	}
}

// isForgeCmd reports whether a hook command is forge-sourced. Mirrors
// hooks.isForgeHookCommand (unexported there) for the JSON shapes this package strips.
//
// isForgeCmd 报告 hook command 是否 forge 来源。与 hooks.isForgeHookCommand
// （未导出）同判定，供本包剥除 JSON 形态用。
func isForgeCmd(cmd string) bool {
	return strings.HasPrefix(cmd, "forge hook ") ||
		strings.HasPrefix(cmd, "forge gate ") ||
		cmd == "forge hook" || cmd == "forge gate"
}

// removeDirIfEmpty removes a directory only when it has no entries left (forge's
// files were the last content). Errors (not empty / not exist) are ignored by design.
//
// removeDirIfEmpty 仅在目录无剩余条目时删除它（forge 的文件是最后的内容）。
// 错误（非空/不存在）按设计忽略。
func removeDirIfEmpty(path string) {
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) > 0 {
		return
	}
	os.Remove(path)
}

// warnCleanup reports a cleanup failure on stderr without failing the command —
// stripping legacy files is best-effort and must never block the user's actual command.
//
// warnCleanup 把清理失败报到 stderr 而不让命令失败——剥除遗留文件是 best-effort，
// 绝不能阻断用户实际要跑的命令。
func warnCleanup(what string, err error) {
	os.Stderr.WriteString("auto-sync warning: cleanup " + what + ": " + err.Error() + "\n")
}
