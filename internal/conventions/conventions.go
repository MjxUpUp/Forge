// Package conventions implements the project conventions profile: mechanical
// discovery of a target repository's DECLARED coding conventions, a fingerprint
// for staleness detection, and renderers for the two injection hooks (session
// digest on SessionStart/PostCompact, write-time pointers on PreToolUse
// Write|Edit).
//
// Package conventions 实现项目规范档案：机械发现目标仓库**已声明**的编码规范、
// 过期判定的 fingerprint，以及两个注入 hook 的渲染器（SessionStart/PostCompact
// 的会话摘要、PreToolUse Write|Edit 的写入时刻指针）。
//
// 本包存在的原因（2026-08-28 dogfood 发现）：forge 在每个事件上注入全局纪律，
// 却对目标仓库自身的规范零感知——`forge init` 只**写** CLAUDE.md/AGENTS.md、
// 从不**读**项目已声明的那些，导致新接入项目里产出的代码漂离该库惯例
// （「forge 接入全新项目后代码不符合规范」的机制缺口）。本包补上这条数据流：
// 发现 → 建档 → 注入；fingerprint 让过期档案提示重扫，而不是静默注入旧规则。
//
// 业界对齐（2026-08 调研）：AGENTS.md 是收敛的跨工具交换格式（60k+ 项目，
// Codex/Cursor/Copilot coding agent 均读）——检测以它为首要来源。claude 宿主
// 自读 CLAUDE.md，摘要对其部分冗余——档案在 claude 上的价值是它不自动加载的
// 部分（CONVENTIONS.md、copilot instructions、lint 命令、过期状态），在非
// claude 宿主上是整份摘要；宿主间价值差异被接受（advisory 层，每会话一小块）。
package conventions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/MjxUpUp/Forge/internal/util"
)

// ProfileVersion is the schema version of Profile.
//
// ProfileVersion 是 Profile 的 schema 版本。JSON 形状破坏性变更时递增，
// LoadProfile 拒读旧文件（而非误解析）。
const ProfileVersion = 1

// SummaryLineBudget caps the session-digest injection at 15 non-empty lines — the always-on layer must stay minimal.
//
// SummaryLineBudget 把会话摘要注入限制在 15 个非空行——always-on 层必须
// 极小（Cursor/Anthropic 共识：「臃肿的常驻规则会淹没真实指令」；auto-memory
// 的每项目 200 行上限是同一哲学的宽松界）。细节由写入时刻 hook 携带，
// 摘要只负责定向。
const SummaryLineBudget = 15

// ExemplarMax is how many sibling exemplar files the write-time injection names.
//
// ExemplarMax 是写入时刻注入列出的同目录范例文件数上限。三个来自
// 「仓内相似示例改善风格一致性」证据线的甜点位（再多只是稀释注意力，
// 不增加模式覆盖）。
const ExemplarMax = 3

// SourceFile is one fingerprinted convention source: a repo-relative path plus a short content hash.
//
// SourceFile 是一个计入指纹的规范来源：仓库相对路径 + 短内容哈希。Bytes
// 供渲染器输出「规范文件很大、选择性阅读」的提示。
type SourceFile struct {
	Path  string `json:"path"`
	SHA   string `json:"sha"`
	Bytes int64  `json:"bytes"`
}

// Profile is the machine-scanned conventions profile stored at <DataDir>/conventions/profile.json.
//
// Profile 是存于 <DataDir>/conventions/profile.json 的机械扫描档案。人/agent
// 可编辑的摘要单独存 summary.md——`forge conventions init` 刷新元数据时不会
// 覆盖人工提炼的内容。
type Profile struct {
	Version      int          `json:"version"`
	Stack        string       `json:"stack"` // go/rust/node/python/java/dotnet；空=未识别
	LintCmd      string       `json:"lint"`
	TestCmd      string       `json:"test"`
	BuildCmd     string       `json:"build"`
	Instructions []SourceFile `json:"instructions"`  // 已声明的规范文件（AGENTS.md 一族）
	StyleConfigs []SourceFile `json:"style_configs"` // lint/format 配置
	CursorRules  int          `json:"cursor_rules"`  // .cursor/rules/*.mdc 计数（只计数不入指纹）
	Fingerprint  string       `json:"fingerprint"`
	Updated      string       `json:"updated"` // RFC3339
}

// instructionFiles 是已声明规范的检测清单，按优先级排序（AGENTS.md 居首：
// 收敛的跨工具交换格式）。刻意排除 .claude/CLAUDE.md 与 CLAUDE.local.md：
// forge 自己的 team-mode init 会把 forge 质量协议写进 .claude/CLAUDE.md——
// 把它哈希进来等于把 forge 自己的文本当「项目规范」回声注入。
var instructionFiles = []string{
	"AGENTS.md",
	"CLAUDE.md",
	".github/copilot-instructions.md",
	"GEMINI.md",
	"CONVENTIONS.md",
}

// styleConfigFiles 是 lint/format 配置检测清单。它们计入指纹（规则变更必须
// 标记档案过期）并列进摘要（agent 应跑 linter，而不是猜规则）。pyproject.toml
// 随行——实践中 Python 的 lint 配置（ruff/black）就住在里面。
var styleConfigFiles = []string{
	".golangci.yml", ".golangci.yaml", ".golangci.json", ".golangci.toml",
	".eslintrc", ".eslintrc.js", ".eslintrc.cjs", ".eslintrc.json", ".eslintrc.yml", ".eslintrc.yaml",
	"eslint.config.js", "eslint.config.mjs", "eslint.config.cjs",
	".prettierrc", ".prettierrc.json", ".prettierrc.yml", ".prettierrc.yaml", ".prettierrc.js", "prettier.config.js",
	"biome.json", "biome.jsonc",
	"rustfmt.toml", ".rustfmt.toml", "clippy.toml",
	"ruff.toml", ".ruff.toml", "pyproject.toml",
	".editorconfig",
	"checkstyle.xml",
}

// stackMarkers 把工具链标记文件映射到 stack 标签。首个命中生效；标记文件本身
// 计入指纹——stack 变化（新增 go.mod 等）会翻转过期状态。
var stackMarkers = []struct {
	File  string
	Stack string
}{
	{"go.mod", "go"},
	{"Cargo.toml", "rust"},
	{"package.json", "node"},
	{"pyproject.toml", "python"},
	{"requirements.txt", "python"},
	{"pom.xml", "java"},
	{"build.gradle", "java"},
	{"build.gradle.kts", "java"},
}

// nodePackageJSON 是取 script 命令所需的最小 package.json 投影。
type nodePackageJSON struct {
	Scripts map[string]string `json:"scripts"`
}

// LoadProfile 用 (nil, nil) 表示「无档案」——调用方无需 errors.Is 管道即可
// 区分「没建过档」与「档案损坏」（真错误），Stale 对 nil 档案直接放行，
// 未采纳本层的项目保持静默。

// DetectStack returns the repo's stack label ("" when unrecognized).
//
// DetectStack 返回仓库的 stack 标签（未识别为空串）。后缀探测（.sln/.csproj）
// 仅在标记文件全未命中时进行——它们需要目录扫描而非 stat。
func DetectStack(root string) string {
	for _, m := range stackMarkers {
		if fileExists(filepath.Join(root, m.File)) {
			return m.Stack
		}
	}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			n := strings.ToLower(e.Name())
			if strings.HasSuffix(n, ".sln") || strings.HasSuffix(n, ".csproj") {
				return "dotnet"
			}
		}
	}
	return ""
}

// stackMarkerFile 返回首个存在的 stack 标记文件（仓库相对路径；无则空串）。
// 指纹输入的一部分。
func stackMarkerFile(root string) string {
	for _, m := range stackMarkers {
		if fileExists(filepath.Join(root, m.File)) {
			return m.File
		}
	}
	return ""
}

// stackCommands 按 stack 推导惯用 lint/test/build 命令。node 优先仓库自己的
// package.json scripts 而非通用回落——起了 lint script 的仓库已经声明了它的规范。
func stackCommands(root, stack string) (lint, test, build string) {
	switch stack {
	case "go":
		lint = "go vet ./..."
		for _, c := range []string{".golangci.yml", ".golangci.yaml", ".golangci.json", ".golangci.toml"} {
			if fileExists(filepath.Join(root, c)) {
				lint = "golangci-lint run"
				break
			}
		}
		return lint, "go test ./...", "go build ./..."
	case "rust":
		return "cargo clippy -- -D warnings", "cargo test", "cargo build"
	case "node":
		return nodeCommands(root)
	case "python":
		lint = "ruff check ."
		if !fileExists(filepath.Join(root, "ruff.toml")) && !fileExists(filepath.Join(root, ".ruff.toml")) {
			lint = "flake8"
		}
		return lint, "pytest", ""
	case "java":
		if fileExists(filepath.Join(root, "build.gradle")) || fileExists(filepath.Join(root, "build.gradle.kts")) {
			return "", "gradle test", "gradle build"
		}
		return "", "mvn test", "mvn package"
	case "dotnet":
		return "dotnet format --verify-no-changes", "dotnet test", "dotnet build"
	}
	return "", "", ""
}

// nodeCommands 读 package.json scripts；script 在场即胜过通用回落，
// 档案陈述的是**本仓库**实际跑的东西。
func nodeCommands(root string) (lint, test, build string) {
	lint, test, build = "npx eslint .", "", "npm run build"
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return lint, test, build
	}
	var pkg nodePackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return lint, test, build
	}
	if cmd, ok := pkg.Scripts["lint"]; ok && cmd != "" {
		lint = "npm run lint"
	}
	if _, ok := pkg.Scripts["test"]; ok {
		test = "npm test"
	}
	if cmd, ok := pkg.Scripts["build"]; ok && cmd != "" {
		build = "npm run build"
	}
	return lint, test, build
}

// Scan mechanically profiles root: instruction files, style configs, stack commands, cursor rule count, and the fingerprint over all of it.
//
// Scan 机械剖析 root：规范文件、style 配置、stack 命令、cursor 规则计数，
// 以及对以上全部的指纹。刻意纯机械（P0）——LLM 提取层在其上增补 summary.md；
// 两者混做会让 `init` 不确定、不可测。
func Scan(root string) (*Profile, error) {
	p := &Profile{
		Version: ProfileVersion,
		Stack:   DetectStack(root),
		Updated: time.Now().Format(time.RFC3339),
	}
	for _, rel := range instructionFiles {
		if sf, ok := hashInstructionFile(root, rel); ok {
			p.Instructions = append(p.Instructions, sf)
		}
	}
	for _, rel := range styleConfigFiles {
		if sf, ok := hashFile(root, rel); ok {
			p.StyleConfigs = append(p.StyleConfigs, sf)
		}
	}
	if entries, err := os.ReadDir(filepath.Join(root, ".cursor", "rules")); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".mdc") {
				p.CursorRules++
			}
		}
	}
	p.LintCmd, p.TestCmd, p.BuildCmd = stackCommands(root, p.Stack)
	p.Fingerprint = Fingerprint(root)
	return p, nil
}

// Fingerprint hashes the CURRENT tree's convention inputs — instruction files, style configs, the stack marker — straight from the detection tables, NOT from the profile's stored lists.
//
// Fingerprint 对**当前**树的规范输入重算——规范文件、style 配置、stack 标记
// ——直接走检测表，而非档案里存的清单：重哈希存量条目既看不到内容编辑
// （存的就是旧 SHA）也看不到扫描后新增的文件。两个检测盲区均由
// TestScan_FlipsStaleOnSourceChange 抓出。
func Fingerprint(root string) string {
	var files []SourceFile
	for _, rel := range instructionFiles {
		if sf, ok := hashInstructionFile(root, rel); ok {
			files = append(files, sf)
		}
	}
	for _, rel := range styleConfigFiles {
		if sf, ok := hashFile(root, rel); ok {
			files = append(files, sf)
		}
	}
	if marker := stackMarkerFile(root); marker != "" {
		if sf, ok := hashFile(root, marker); ok {
			files = append(files, sf)
		}
	}
	return fingerprintOf(files)
}

// fingerprintOf 把排序后的 "path sha" 行折叠为单个十六进制摘要。排序让摘要
// 与检测顺序无关；路径与内容一起哈希，文件改名同样翻转过期状态。
func fingerprintOf(files []SourceFile) string {
	lines := make([]string, 0, len(files))
	for _, f := range files {
		if f.Path != "" && f.SHA != "" {
			lines = append(lines, f.Path+" "+f.SHA)
		}
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])[:16]
}

// Stale reports whether the tree drifted from the profile.
//
// Stale 报告树是否已偏离档案。按当前树重新检测（扫描后新增的文件也算），
// 而非只重哈希当初扫到的清单。
func Stale(root string, p *Profile) bool {
	if p == nil {
		return false
	}
	return Fingerprint(root) != p.Fingerprint
}

// ---- Storage (under <DataDir>/conventions/) ----

// Dir returns the profile directory under a project DataDir.
//
// Dir 返回项目 DataDir 下的档案目录。保持函数（而非 Project 方法），
// 让 forgedata 对 conventions 无感知——依赖只从 conventions/hook 侧指向
// forgedata。
func Dir(dataDir string) string { return filepath.Join(dataDir, "conventions") }

// ProfilePath returns <DataDir>/conventions/profile.json.
func ProfilePath(dataDir string) string { return filepath.Join(Dir(dataDir), "profile.json") }

// SummaryPath returns <DataDir>/conventions/summary.md.
func SummaryPath(dataDir string) string { return filepath.Join(Dir(dataDir), "summary.md") }

// SaveProfile atomically writes the profile JSON.
//
// SaveProfile 原子写档案 JSON。
func SaveProfile(dataDir string, p *Profile) error {
	if err := os.MkdirAll(Dir(dataDir), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(ProfilePath(dataDir), append(data, '\n'), 0644)
}

// LoadProfile reads the profile; (nil, nil) when absent, an error when present but unreadable/corrupt.
//
// LoadProfile 读档案；不存在返 (nil, nil)，存在但读不出/损坏返错误
// （调用方把损坏当「重建」输入，不当静默放行）。
func LoadProfile(dataDir string) (*Profile, error) {
	data, err := os.ReadFile(ProfilePath(dataDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var p Profile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	if p.Version != ProfileVersion {
		return nil, fmt.Errorf("conventions: profile version %d unsupported (want %d)", p.Version, ProfileVersion)
	}
	return &p, nil
}

// LoadSummary reads summary.md ("" when absent).
//
// LoadSummary 读 summary.md（不存在为空串）。
func LoadSummary(dataDir string) string {
	data, err := os.ReadFile(SummaryPath(dataDir))
	if err != nil {
		return ""
	}
	return string(data)
}

// SaveSummary atomically writes summary.md.
//
// SaveSummary 原子写 summary.md。
func SaveSummary(dataDir, content string) error {
	if err := os.MkdirAll(Dir(dataDir), 0755); err != nil {
		return err
	}
	return util.AtomicWrite(SummaryPath(dataDir), []byte(content), 0644)
}

// SummaryExists reports whether a digest has been written (init or agent enrichment).
//
// SummaryExists 报告摘要是否已写过（init 或 agent 增补）——区分「已建档但
// 摘要为空」与「无档案」。
func SummaryExists(dataDir string) bool {
	_, err := os.Stat(SummaryPath(dataDir))
	return err == nil
}

// ---- Renderers (factual phrasing: state facts, no imperatives — same
// discipline as failure-track/test-nudge, imperatives read as injected
// instructions) ----

// GenerateSummary renders the initial digest scaffold: what was detected plus an empty extraction section for the agent to enrich.
//
// GenerateSummary 渲染初始摘要骨架：检测结果 + 留给 agent 增补的空提取节。
// 提取要点节是 P1 的挂点——`forge conventions init` 打印增补指引，hook 注入
// 这里最终的内容。
func GenerateSummary(p *Profile) string {
	var b strings.Builder
	b.WriteString("# conventions digest（forge conventions init 生成；提取要点由 agent 代码考古后补全，整份保持 ≤15 行）\n")
	var toolchain []string
	for _, kv := range []struct{ k, v string }{
		{"stack", p.Stack}, {"lint", p.LintCmd}, {"test", p.TestCmd}, {"build", p.BuildCmd},
	} {
		if kv.v != "" {
			toolchain = append(toolchain, kv.k+": "+kv.v)
		}
	}
	if len(toolchain) > 0 {
		fmt.Fprintf(&b, "- %s\n", strings.Join(toolchain, " · "))
	}
	if names := Paths(p.Instructions); len(names) > 0 {
		fmt.Fprintf(&b, "- 规范声明文件（写码前先读）: %s\n", strings.Join(names, ", "))
	}
	if names := Paths(p.StyleConfigs); len(names) > 0 {
		fmt.Fprintf(&b, "- lint/format 配置: %s\n", strings.Join(names, ", "))
	}
	if p.CursorRules > 0 {
		fmt.Fprintf(&b, "- cursor rules: %d 个（.cursor/rules/*.mdc）\n", p.CursorRules)
	}
	b.WriteString("\n## 提取要点（agent 代码考古后逐条补全：命名惯例 / 错误处理模式 / 目录结构 / import 与注释风格）\n")
	b.WriteString("- （待提取）\n")
	return b.String()
}

// SessionInject renders the SessionStart/PostCompact digest block.
//
// SessionInject 渲染 SessionStart/PostCompact 的摘要块。stale 追加重扫行——
// 过期摘要比没有更糟（它自信地陈述旧规则），过期状态必须在注入内容里可见。
func SessionInject(p *Profile, summary string, stale bool) string {
	if p == nil {
		return ""
	}
	body := strings.TrimSpace(summary)
	if body == "" {
		// 摘要被删而档案仍在：回落到生成版，让本层降级为仅元数据而非静默。
		body = strings.TrimSpace(GenerateSummary(p))
	}
	var b strings.Builder
	b.WriteString("[forge] conventions: this project has a conventions profile — the digest below states the repo's declared conventions (source: forge conventions init).\n")
	if stale {
		b.WriteString("[forge] conventions: profile STALE — instruction/style files changed since the scan; rerun `forge conventions init` and re-enrich the digest.\n")
	}
	b.WriteString(TrimToLines(body, SummaryLineBudget))
	return b.String()
}

// SuggestInit renders the no-profile suggestion when the repo already declares conventions.
//
// SuggestInit 在仓库已声明规范而无档案时渲染建档建议——提供选择而非强制
// （本层 advisory；是否建档由用户决定）。
func SuggestInit(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("[forge] conventions: this repo declares coding conventions in %s but has no forge conventions profile — `forge conventions init` scans them into a profile whose digest is injected each session (stack/lint commands, convention files, agent-extracted rules).",
		strings.Join(names, ", "))
}

// WriteInject renders the PreToolUse Write|Edit block for one target file: instruction-file pointers plus sibling exemplars.
//
// WriteInject 为单个目标文件渲染 PreToolUse Write|Edit 块：规范文件指针 +
// 同目录范例。relPath/exemplars 均为仓库相对路径。空结果=「无可奉告」
// （调用方保持静默）。
func WriteInject(relPath string, p *Profile, stale bool, exemplars []string) string {
	if p == nil {
		return ""
	}
	var lines []string
	if names := Paths(p.Instructions); len(names) > 0 {
		lines = append(lines, fmt.Sprintf(
			"[forge] conventions: before finalizing %s — this repo declares conventions in %s (follow them if not yet applied).",
			relPath, strings.Join(names, ", ")))
	}
	if len(exemplars) > 0 {
		lines = append(lines, fmt.Sprintf(
			"[forge] conventions: sibling exemplars near %s (match their naming/structure/error-handling style): %s",
			relPath, strings.Join(exemplars, ", ")))
	}
	if len(lines) == 0 {
		return ""
	}
	if stale {
		lines = append(lines, "[forge] conventions: profile stale — rerun `forge conventions init` before trusting the digest.")
	}
	return strings.Join(lines, "\n")
}

// EnrichResult reports what EnrichSummary did.
//
// EnrichResult 报告 EnrichSummary 做了什么。
type EnrichResult struct {
	// Changed: the digest content actually changed (rule added / placeholder replaced).
	//
	// Changed：摘要内容真的变了（新增规则/替换占位符）。false = 完全相同的
	// 规则行已在（去重空操作）。
	Changed bool
	// OverBudget: the digest now exceeds SummaryLineBudget non-empty lines.
	//
	// OverBudget：摘要非空行数已超 SummaryLineBudget。写入照常发生——
	// EnrichSummary 绝不静默删用户内容；修剪建议由调用方转达。
	OverBudget bool
}

// pendingPlaceholder 是骨架的「尚未提取」行——首次增补替换它而非追加
// （占位符与真实规则并存的摘要读起来像没写完）。
const pendingPlaceholder = "（待提取）"

// extractHeadingPrefix 识别提取节的标题（按前缀匹配——标题后缀措辞可演进，
// learn 不因此破）。
const extractHeadingPrefix = "## 提取要点"

// EnrichSummary writes one learned rule into summary.md's 提取要点 section — the layer-4 correction write-back.
//
// EnrichSummary 把一条学到的规则写进 summary.md 的提取要点节——层 4 的纠正
// 写回（用户/审查指出规范违规；规则落进摘要，此后每个会话都看得见）。
// 优先级：完全重复 → 空操作；「待提取」占位符 → 替换；否则插入 提取要点
// 标题正下方（最新在前）。标题缺失则追加新节；summary 缺失则报错
// （init 之前 learn 会造出无档案的孤儿摘要）。
func EnrichSummary(dataDir, rule string) (EnrichResult, error) {
	// 导出 API 加固：规则是一行一条的契约——内嵌换行会破坏 "- " 行形（CLI 路径
	// 经 strings.Join(args," ") 不可达，直接调用方可达；对抗审查发现 #5）。
	rule = strings.TrimSpace(strings.ReplaceAll(rule, "\n", " "))
	if rule == "" {
		return EnrichResult{}, fmt.Errorf("conventions: empty rule")
	}
	if !SummaryExists(dataDir) {
		return EnrichResult{}, fmt.Errorf("conventions: no summary.md — run `forge conventions init` first")
	}
	summary := LoadSummary(dataDir)
	ruleLine := "- " + rule
	lines := strings.Split(strings.TrimRight(summary, "\n"), "\n")

	for _, l := range lines {
		if strings.TrimSpace(l) == ruleLine {
			return EnrichResult{Changed: false}, nil // 去重：一字不差的规则已在
		}
	}

	// 首个占位符行替换；否则插入标题下方（跳过标题后的一个空行）。占位符按
	// **子串**替换而非整行——手编文件里占位符同行可能有其他文字，整行替换会
	// 抹掉它（对抗审查发现 #5；stock 骨架整行就是占位符，两种形态都正确）。
	replaced := false
	for i, l := range lines {
		if !replaced && strings.Contains(l, pendingPlaceholder) {
			lines[i] = strings.Replace(l, pendingPlaceholder, rule, 1)
			replaced = true
		}
	}
	out := lines
	if !replaced {
		headingIdx := -1
		for i, l := range lines {
			if strings.HasPrefix(strings.TrimSpace(l), extractHeadingPrefix) {
				headingIdx = i
				break
			}
		}
		if headingIdx == -1 {
			// 用户重组过文件：追加全新提取节（learn 不因结构漂移而丢规则）。
			if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
				out = append(out, "")
			}
			out = append(out, extractHeadingPrefix, ruleLine)
		} else {
			insertAt := headingIdx + 1
			if insertAt < len(out) && strings.TrimSpace(out[insertAt]) == "" {
				insertAt++
			}
			out = append(out[:insertAt], append([]string{ruleLine}, out[insertAt:]...)...)
		}
	}
	newSummary := strings.Join(out, "\n") + "\n"
	if err := SaveSummary(dataDir, newSummary); err != nil {
		return EnrichResult{}, err
	}
	return EnrichResult{
		Changed:    true,
		OverBudget: countNonEmpty(out) > SummaryLineBudget,
	}, nil
}

// countNonEmpty 统计非空行数。
func countNonEmpty(lines []string) int {
	n := 0
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

// Exemplars picks up to ExemplarMax same-directory, same-extension sibling files of targetPath — the closest style reference for the file being written.
//
// Exemplars 选最多 ExemplarMax 个与 targetPath 同目录、同扩展名、且**同类别**
// （源码/测试）的兄弟文件——被写文件最贴近的风格参照（最近修改优先：最新
// 文件最能代表当前风格）。范例恒与目标同类：源码目标拿源码兄弟、测试目标
// 拿测试兄弟——值得模仿的模式住在同一条类别线的同一侧（测试目标指到源码
// 兄弟会学错模式，反向同理；2026-08-28 对抗审查发现）。点文件与超大文件
// 恒排除。
func Exemplars(root, targetPath string) []string {
	dir := filepath.Dir(targetPath)
	ext := filepath.Ext(targetPath)
	if ext == "" {
		return nil
	}
	targetIsTest := likelyTestFile(filepath.Base(targetPath))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	type cand struct {
		path    string
		modNano int64
	}
	var cands []cand
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ext || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if likelyTestFile(e.Name()) != targetIsTest {
			continue // 类别不匹配：测试/源码各自模仿同类
		}
		full := filepath.Join(dir, e.Name())
		if same, err := samePath(full, targetPath); err == nil && same {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > 256*1024 {
			continue
		}
		cands = append(cands, cand{path: full, modNano: info.ModTime().UnixNano()})
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].modNano > cands[j].modNano })
	var out []string
	for _, c := range cands {
		rel := RelPath(root, c.path)
		if rel == "" {
			continue
		}
		out = append(out, rel)
		if len(out) >= ExemplarMax {
			break
		}
	}
	return out
}

// likelyTestFile 是本地的测试文件启发式（形态镜像 taskpipeline 的分类器；
// 不共享是因为那一份带着 test-coverage 门禁专属的白名单语义）。
func likelyTestFile(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, "_test.go") ||
		strings.Contains(n, ".test.") || strings.Contains(n, ".spec.") ||
		strings.HasSuffix(n, "_test.py") || strings.HasPrefix(n, "test_") ||
		strings.HasSuffix(n, ".test.js") || strings.HasSuffix(n, ".test.ts")
}

// ---- small helpers ----

// RelPath makes path repo-relative.
//
// RelPath 把 path 转为仓库相对。path 不在 root 下时原样返回——仅作展示
// 回落：把结果喂进面向模型的注入的调用方必须自行挡掉根外目标（写 hook
// 已做；用户绝对路径绝不能搭注入的便车）。
func RelPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// Paths projects a SourceFile list onto its paths (display order = scan order).
//
// Paths 把 SourceFile 列表投影为路径列表（展示序=扫描序）。唯一共享投影
// ——cli 层曾有俩私有拷贝（对抗审查 nit），留着只会漂移。
func Paths(files []SourceFile) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

// TrimToLines keeps at most max non-empty lines and appends an ellipsis line when truncation happened.
//
// TrimToLines 最多保留 max 个非空行，截断时追加省略行（静默截断会把部分
// 摘要当整体呈现）。
func TrimToLines(s string, max int) string {
	var all []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		all = append(all, strings.TrimRight(l, " \t"))
	}
	if len(all) == 0 {
		return ""
	}
	if len(all) <= max {
		return strings.Join(all, "\n")
	}
	return strings.Join(all[:max], "\n") + "\n…（摘要超行数上限，全文 forge conventions show）"
}

// hashInstructionFile 哈希规范文件时剥离 forge 标记包裹段（team-mode
// `forge init --project` 会把协议写进根 AGENTS.md/CLAUDE.md）：不剥离则
// (a) forge 自己的文本被当「已声明规范」计入指纹（正是 .claude/CLAUDE.md
// 排除项防的回声），(b) 每次 forge 升级 + re-init 重写该段都会让档案永久
// STALE（2026-08-28 对抗审查发现）。style 配置不剥离——forge 从不写它们。
func hashInstructionFile(root, rel string) (SourceFile, bool) {
	full := filepath.Join(root, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		return SourceFile{}, false
	}
	sum := sha256.Sum256([]byte(util.StripMarkedSection(string(data), util.ForgeSectionStart, util.ForgeSectionEnd)))
	info, err := os.Stat(full)
	if err != nil {
		return SourceFile{}, false
	}
	return SourceFile{Path: rel, SHA: hex.EncodeToString(sum[:])[:16], Bytes: info.Size()}, true
}

// hashFile 把 root/rel stat+哈希为 SourceFile（不存在/读不出为 ok=false——
// 检测以在场为准，读不出的文件跳过、不致命）。
func hashFile(root, rel string) (SourceFile, bool) {
	full := filepath.Join(root, rel)
	data, err := os.ReadFile(full)
	if err != nil {
		return SourceFile{}, false
	}
	sum := sha256.Sum256(data)
	info, err := os.Stat(full)
	if err != nil {
		return SourceFile{}, false
	}
	return SourceFile{Path: rel, SHA: hex.EncodeToString(sum[:])[:16], Bytes: info.Size()}, true
}

// fileExists 是检测表共用的 stat 探测。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// samePath 经 os.SameFile 判路径同一性（symlink 鲁棒，adoptPayloadCwd 里
// v0.27.2 的教训）。
func samePath(a, b string) (bool, error) {
	ia, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	ib, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(ia, ib), nil
}
