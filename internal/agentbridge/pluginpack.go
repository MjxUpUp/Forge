package agentbridge

// Plugin pack 生成：让 forge 通过各 agent 的 plugin marketplace 一键分发。采用多 host
// 插件市场的通用模式：薄 manifest + 共享内容，单仓即 marketplace。
//
// 生成结构（写入 spec.RepoDir）：
//
//	.claude-plugin/marketplace.json   claude+copilot 官方文档确认扫描此目录；codex
//	                                  (OpenAI 未明确路径)按兼容性假设——README 指引 codex
//	                                  用户额外跑 forge init --agents codex，故即使 entry
//	                                  对 codex 无效，安装路径仍可达
//	.cursor-plugin/marketplace.json   cursor 独立（只扫自己的 .cursor-plugin/）
//	plugins/<PluginName>/
//	  .claude-plugin/plugin.json      claude plugin manifest：hooks 字段 = ForgeHookSpec，
//	                                  让 `claude plugin install <name>` 直接获得与 forge init
//	                                  字节相同的 gate 接线（单一真相源）
//	  skills/<skill>/...              内嵌 canonical skill 库，每 skill 一个目录——claude
//	                                  plugin 的 skills 布局（plugin 根下 skills/ 目录，按约定
//	                                  加载，无需 manifest 字段），plugin 安装即随 hooks 带走
//	                                  整个 skill 库；来源与其他路径共用的同一 go:embed（单一
//	                                  真相源；防止分发出一个没有 skills 的 plugin）
//	  reasonix-plugin.json            reasonix NATIVE plugin manifest（apiVersion reasonix.io/plugin/v1）：
//	                                  hooks 字段 = buildReasonixHooks 扁平 {match,command}，让
//	                                  `reasonix plugin install <name>` 获得相同的 gate 接线。
//	                                  reasonix 的 Claude 兼容不解析 .claude-plugin/plugin.json 的
//	                                  hooks，故此 native manifest 必需；两者并存时 reasonix 优先
//	                                  native（互不污染）。
//	  hooks.json                      copilot plugin hooks，位于 plugin 根（copilot 文档化的
//	                                  "每个 plugin 自己的 hooks.json" 位置——非 hooks/hooks.json，
//	                                  那会让 Claude Code 与 plugin.json 的 hooks 字段双跑）：
//	                                  {"version":1,"hooks":{PascalCase event}}，扁平
//	                                  {type,command,matcher,timeoutSec} 条目带
//	                                  `--agent copilot`（Wave 2c；见 copilot_hooks.go）
//	  README.md                       每 host 一段安装命令
//
// 关键设计：source 用 ./plugins/<PluginName> 子目录而非仓库根 —— forge 是 Go 工具仓
// （internal/cmd/...），须把插件配置隔离到子目录，避免整个源码树被当插件拉取。
//
// 省略 version 字段：claude marketplace 用 git commit SHA 驱动每次 commit 自动更新
// （claude plugin 文档确认省略 version → SHA），forge v1.0 迭代期合适，且简化 generator
// （无 version 常量 drift）、golden test 更稳。
//
// owner 字段：claude marketplace schema 把 owner 标为 REQUIRED（marketplaces 文档
// "Marketplace schema → Required fields"）。故 GeneratePluginPack 在 OwnerName 空时
// 报错，DefaultPluginPack 预填 forge 的 owner（MjxUpUp）。
//
// 覆盖范围：marketplace 模型的工具（claude/cursor；codex/copilot 复用 claude marketplace）。
// opencode 走各自项目级/包级生成器（opencode.go 的 forge.ts、pi 的 pi install），
// 不在 marketplace 模型内。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/MjxUpUp/Forge/internal/hooks"
	"github.com/MjxUpUp/Forge/internal/util"
	"github.com/MjxUpUp/Forge/skills"
	skillsforge "github.com/MjxUpUp/Forge/skills-forge"
)

// DefaultPluginDescription 是 plugin/marketplace 描述的单一真相，被 DefaultPluginPack
// 与 CLI flag 默认值共用（避免 DefaultPluginPack("").Description 这种为取字段造空 spec
// 的反模式）。
const DefaultPluginDescription = "Forge loop-engineering quality gates: task-tracked source changes, assertion guards, file-sentinel quarantine, and review-gated completion for AI coding agents."

// PluginPackSpec configures the generated plugin pack.
//
// PluginPackSpec 配置生成的 plugin pack。OwnerName 是 required（claude marketplace schema），
// RepoSlug/OwnerEmail 用于品牌化 marketplace manifest 与 README 安装命令。
type PluginPackSpec struct {
	// Repo root: marketplaces + plugins/ are written into this dir.
	//
	// 仓库根：marketplaces + plugins/ 写入此目录
	RepoDir string // 仓库根：marketplaces + plugins/ 写入此目录
	// github owner/repo for install commands, e.g. MjxUpUp/Forge.
	//
	// github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	RepoSlug string // github owner/repo，用于安装命令，如 "MjxUpUp/Forge"
	// Marketplace identifier, e.g. forge.
	//
	// marketplace 标识，如 "forge"
	MarketplaceName string // marketplace 标识，如 "forge"
	// Plugin identifier, e.g. forge.
	//
	// plugin 标识，如 "forge"
	PluginName  string // plugin 标识，如 "forge"
	Description string
	// required (schema); the name of the marketplace owner + plugin author.
	//
	// required（schema）；marketplace owner + plugin author 的 name
	OwnerName string // required（schema）；marketplace owner + plugin author 的 name
	// optional; the email of the marketplace owner + plugin author.
	//
	// optional；marketplace owner + plugin author 的 email
	OwnerEmail string // optional；marketplace owner + plugin author 的 email
}

// DefaultPluginPack returns a spec pre-filled with forge defaults (owner=MjxUpUp
// satisfies schema required).
//
// DefaultPluginPack 返回填好 forge 默认值的 spec（含 owner=MjxUpUp 满足 schema required）。
// 调用方可覆盖 OwnerName/OwnerEmail/RepoSlug 来品牌化。
func DefaultPluginPack(repoDir string) PluginPackSpec {
	return PluginPackSpec{
		RepoDir:         repoDir,
		RepoSlug:        "MjxUpUp/Forge",
		MarketplaceName: "forge",
		PluginName:      "forge",
		Description:     DefaultPluginDescription,
		OwnerName:       "MjxUpUp",
	}
}

// GeneratePluginPack writes a multi-host plugin pack under spec.RepoDir (file
// layout shown in the file-header comment).
//
// GeneratePluginPack 在 spec.RepoDir 下写多 host plugin pack（文件布局见文件头注释）。
// OwnerName 空时报错（claude marketplace schema required）；幂等：重跑就地覆盖。
func GeneratePluginPack(spec PluginPackSpec) error {
	if spec.OwnerName == "" {
		return fmt.Errorf("plugin pack: OwnerName is required (claude marketplace schema marks owner as required); use DefaultPluginPack for the defaults")
	}
	if spec.MarketplaceName == "" || spec.PluginName == "" {
		return fmt.Errorf("plugin pack: MarketplaceName and PluginName are required")
	}

	// 2 份 marketplace。claude+copilot 官方文档确认扫 .claude-plugin/；cursor 扫
	// .cursor-plugin/。codex 路径 OpenAI 未明确，按兼容性假设（见文件头注释）。
	if err := writeMarketplace(spec, filepath.Join(spec.RepoDir, ".claude-plugin")); err != nil {
		return err
	}
	if err := writeMarketplace(spec, filepath.Join(spec.RepoDir, ".cursor-plugin")); err != nil {
		return err
	}

	pluginDir := filepath.Join(spec.RepoDir, "plugins", spec.PluginName)
	if err := writeClaudePluginManifest(spec, pluginDir); err != nil {
		return err
	}
	if err := writeReasonixPluginManifest(spec, pluginDir); err != nil {
		return err
	}
	if err := writeCopilotHooksManifest(pluginDir); err != nil {
		return err
	}
	if err := writePluginSkills(pluginDir); err != nil {
		return err
	}
	if err := writePluginReadme(spec, pluginDir); err != nil {
		return err
	}
	return nil
}

// ownerMap 构建 owner/author 对象。name 总在（GeneratePluginPack 已校验非空），email 可选。
func ownerMap(spec PluginPackSpec) map[string]string {
	m := map[string]string{"name": spec.OwnerName}
	if spec.OwnerEmail != "" {
		m["email"] = spec.OwnerEmail
	}
	return m
}

// writeMarketplace 写一份 marketplace.json（claude 与 cursor 各一份，格式相同，仅目录不同）。
// 结构遵循 claude marketplace schema：{name, description, owner, plugins:[{name, description, source, author}]}。
// source 跟随 PluginName（非硬编码），省略 version（git SHA 驱动自动更新）。
func writeMarketplace(spec PluginPackSpec, dir string) error {
	// name 必有，email 可选——复用一次填 owner 与 author
	owner := ownerMap(spec) // name 必有，email 可选——复用一次填 owner 与 author
	entry := map[string]any{
		"name":        spec.PluginName,
		"description": spec.Description,
		"source":      "./plugins/" + spec.PluginName,
		"author":      owner,
	}
	mp := map[string]any{
		"name":        spec.MarketplaceName,
		"description": "Forge plugin marketplace",
		"owner":       owner,
		"plugins":     []map[string]any{entry},
	}
	return writeJSONIndent(filepath.Join(dir, "marketplace.json"), mp)
}

// writeClaudePluginManifest 写 plugins/<name>/.claude-plugin/plugin.json。hooks 字段是
// hooks.ForgeHookSpec() 返回的同一个对象（也是 GenerateUserSettings 写到 user-level
// settings.json "hooks" key 下的那个），故 `claude plugin install <name>` 得到的 gate 接线
// 与 `forge init` 字节一致——单一真相源。TestPluginPack_HooksMirrorSettings 守卫此相等性。
func writeClaudePluginManifest(spec PluginPackSpec, pluginDir string) error {
	manifest := map[string]any{
		"name":        spec.PluginName,
		"description": spec.Description,
		"hooks":       hooks.ForgeHookSpec(),
	}
	return writeJSONIndent(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), manifest)
}

// writeReasonixPluginManifest 写 plugins/<name>/reasonix-plugin.json——reasonix 的 NATIVE
// plugin manifest（apiVersion reasonix.io/plugin/v1）。reasonix 的 Claude 兼容不解析
// .claude-plugin/plugin.json 的 hooks 字段（实测被判 "no Reasonix-compatible capabilities"、
// kinds 全 0），故 reasonix 需在 claude manifest 旁加一份 native manifest。两者并存时
// reasonix 优先 native（已确认：manifestKind "reasonix"、compatibility full、
// mappedCapabilities ["hooks"]），故两份 manifest 共处同一 plugin 目录——claude 读
// .claude-plugin/plugin.json，reasonix 读 reasonix-plugin.json，互不污染。native schema 是
// 每 event 扁平 {match, command}（非 claude 的嵌套 {matcher, hooks:[{type,command}]}——
// reasonix 拒绝 matcher/type/嵌套 hooks 字段），正是 buildReasonixHooks 为 settings.json
// 产出的形态。复用它使 `reasonix plugin install` 的 gate 接线与 `forge init --agents
// reasonix` 一致——单一真相源。TestPluginPack_ReasonixManifestHooksMirror 守卫此点。
//
// version 是静态展示串（"1.0.0"）：reasonix 要求该字段（native manifest 结构体把它建模为
// 非 omitempty），而 pack 生成器不接收 version 输入（claude 省略 version 走 SHA 自动更新，
// 但 reasonix 的 native 格式要求 version 在场）。它是 plugin 展示元数据，与 forge 自身发布
// 版本解耦。
func writeReasonixPluginManifest(spec PluginPackSpec, pluginDir string) error {
	manifest := map[string]any{
		"apiVersion":  "reasonix.io/plugin/v1",
		"name":        spec.PluginName,
		"version":     "1.0.0",
		"description": spec.Description,
		"hooks":       buildReasonixHooks()["hooks"],
	}
	return writeJSONIndent(filepath.Join(pluginDir, "reasonix-plugin.json"), manifest)
}

// writePluginSkills 把内嵌 canonical skill 库展开到 plugins/<name>/skills/——claude plugin
// 的 skills 布局（plugin 根下 skills/ 目录，每 skill 一个目录，按约定加载，无需 manifest
// 字段）。缺了这一步，`claude plugin install` 只接线 gate、不带任何 skill：用户仍需手动
// `forge skills install --global`（正是 plugin 用户反馈的缺口）。来源与其他分发路径共用
// 同一 go:embed（skills.FS + skillsforge.FS）——单一真相源。2026-08 零反向依赖迁移
// 后插件分发两棵树：中立 skills/ + forge 原生 skills-forge/（合并写进同一
// plugins/<name>/skills/，forge 自己的插件当然带 forge 原生 skill）。
//
// 收敛性：先 RemoveAll 整个 skills 树，canonical 库里删掉的 skill 不会以陈旧条目残留
// 在 committed pack 里（regen 必须收敛，而非只增不减）。根级文件（CONVENTIONS.md、*.go）
// 是库元数据，不是 skill；无 SKILL.md 的孤儿目录跳过（非可加载 skill）。
func writePluginSkills(pluginDir string) error {
	skillsDir := filepath.Join(pluginDir, "skills")
	if err := os.RemoveAll(skillsDir); err != nil {
		return fmt.Errorf("remove stale plugin skills: %w", err)
	}
	shipped := 0
	for _, lib := range []fs.FS{skills.FS, skillsforge.FS} {
		n, err := writeSkillsFrom(lib, skillsDir)
		if err != nil {
			return err
		}
		shipped += n
	}
	// 空库 = 分发回退（embed FS 缺失/被清空）——拒绝静默分发无 skills 的 plugin；
	// 零数量同样破坏 claude plugin 的 skills 契约。
	if shipped == 0 {
		return fmt.Errorf("plugin pack: embedded skill library resolved 0 skills — refusing to ship a skills-less plugin (distribution regression)")
	}
	return nil
}

// writeSkillsFrom 把 lib 里每个可加载 skill（含 SKILL.md 的顶层目录）写进
// skillsDir，返回分发数。中立树与 forge 原生树共用——两棵树语义一致。
func writeSkillsFrom(lib fs.FS, skillsDir string) (int, error) {
	entries, err := fs.ReadDir(lib, ".")
	if err != nil {
		return 0, fmt.Errorf("read embedded skill library: %w", err)
	}
	shipped := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue // root files (CONVENTIONS.md, *.go) are library metadata, not skills
		}
		if _, serr := fs.Stat(lib, path.Join(e.Name(), "SKILL.md")); serr != nil {
			// 只有真正的 NotExist 才表示"无 SKILL.md 的孤儿目录——非可加载 skill"；
			// 其他 Stat 错误必须上报，不能静默少发一个 skill。
			if !errors.Is(serr, fs.ErrNotExist) {
				return 0, fmt.Errorf("stat skill %s: %w", e.Name(), serr)
			}
			continue
		}
		werr := fs.WalkDir(lib, e.Name(), func(p string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return werr
			}
			target := filepath.Join(skillsDir, filepath.FromSlash(p))
			if d.IsDir() {
				return os.MkdirAll(target, 0755)
			}
			// 跳过 .go：embed 只排除含 build 指令的 .go；无指令的测试产物会随 embed 混入
			//（与 skills.ExtractTo 同款跳过）。
			if filepath.Ext(p) == ".go" {
				return nil
			}
			data, rerr := fs.ReadFile(lib, p)
			if rerr != nil {
				return rerr
			}
			return util.AtomicWrite(target, data, 0644)
		})
		if werr != nil {
			// 上面的 RemoveAll 已清掉 committed 树——walk 中途失败会留下残缺。显式给出
			// 恢复动作，让操作者知道须重跑到成功，而不是分发一个被削过的包。
			return 0, fmt.Errorf("write skill %s: %w (committed skills tree at %s is now PARTIAL — re-run `forge plugin pack` until it succeeds before committing)", e.Name(), werr, skillsDir)
		}
		shipped++
	}
	return shipped, nil
}

func writePluginReadme(spec PluginPackSpec, pluginDir string) error {
	slug := spec.RepoSlug
	if slug == "" {
		slug = "MjxUpUp/Forge"
	}
	return util.AtomicWrite(filepath.Join(pluginDir, "README.md"), []byte(pluginReadme(slug)), 0644)
}

// writeJSONIndent 以 2-space 缩进写 JSON 到 path（自动建父目录）。所有 plugin pack 文件
// 走此 helper，保证格式一致（golden test 依赖此缩进）。
func writeJSONIndent(path string, v any) error {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	return util.AtomicWrite(path, append(data, '\n'), 0644)
}
