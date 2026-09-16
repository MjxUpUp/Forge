// Package bridgeverify implements the static half of roadmap H2: a
// zero-dependency static checker for DeepSeek Harness (dsh) plugin packages,
// mapping the four accident patterns empirically documented in dsh
// Discussions #1884 (inject mismatch, duplicate registration, incomplete
// schema/decision shapes, missing fail-soft discipline) onto grep-level
// checks a plugin author can run before publishing.
//
// Package bridgeverify 实现 roadmap H2 的静态半场：面向 DeepSeek Harness (dsh)
// 插件包的零依赖静态检查器，把 #1884 实证的四大事故模式映射为发布前可跑的
// grep 级检查。设计约束（docs/design/forge-dsh-provider-roadmap.md §H2）：
// 不引 AST 依赖（懒惰阶梯——正则足够覆盖插件作者的惯用形态），不做运行时
// 判定（动态重放属 H2b）。判级口径对齐 compat report：error → exit 2，
// warn/info → exit 0。
package bridgeverify

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Severity 判级：error（会在运行时炸或必然失效）、warn（#1884 实证的事故模式）、
// info（安装摩擦/静默不触发的提示）。
type Severity string

const (
	SevError Severity = "error"
	SevWarn  Severity = "warn"
	SevInfo  Severity = "info"
)

// Finding 是一条检查结论。Check 是稳定的机器可读 ID（测试与 --json 消费）。
type Finding struct {
	Severity Severity `json:"severity"`
	Check    string   `json:"check"`
	Message  string   `json:"message"`
	Detail   string   `json:"detail,omitempty"`
}

// Report 是一个插件包的完整静态检查结论（确定性：按 Check+Message 排序）。
type Report struct {
	Dir      string    `json:"dir"`
	Findings []Finding `json:"findings"`
}

// Errors 返回 error 级 finding 数（CLI 据此定退出码）。
func (r *Report) Errors() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

const (
	maxFiles    = 200
	maxFileByte = 512 << 10
)

// contextMethods 是 Cordis Context 的 API 方法名（非服务键）——ctx.<method>
// 形态的取用不算服务访问。ctx.get 是宽容查找（缺服务返回 undefined，不抛
// "cannot get property"），故不计入未声明判定；属性访问（ctx.tools）才是
// 注入语义（#1884 事故模式 1 的抛错来源）。
var contextMethods = map[string]bool{
	"effect": true, "on": true, "once": true, "off": true, "emit": true,
	"waterfall": true, "parallel": true, "serial": true, "bail": true,
	"get": true, "set": true, "provide": true, "scope": true,
	"logger": true, "start": true, "stop": true, "dispose": true,
	"collect": true, "define": true, "isolate": true, "plugin": true,
}

// knownEvents 是 dsh 类型化事件名的已知集合（来自 contract.json 与 dsh 文档）。
// ctx.on 了名单之外的事件不会报错——它只是永不触发（静默 no-op），故判 info。
var knownEvents = map[string]bool{
	"tools/pre-execute": true, "tools/post-execute": true,
	"agent/pre-step": true, "agent/session-start": true,
	"agent/turn-stopping": true,
}

var (
	reName   = regexp.MustCompile(`(?m)^\s*(?:export\s+)?(?:const|let|var)\s+name\s*=\s*["']([^"']+)["']`)
	reInject = regexp.MustCompile(`\binject\s*=\s*\[([^\]]*)\]`)
	reApply  = regexp.MustCompile(`(?:function\s+apply\s*\()|(?:apply\s*[:=]\s*(?:async\s*)?\()`)
	reStr    = regexp.MustCompile(`["']([\w.$-]+)["']`)
	reCtxKey = regexp.MustCompile(`\bctx\.([A-Za-z_$][\w$]*)`)
	reCtxBr  = regexp.MustCompile(`\bctx\[["']([\w.$-]+)["']\)`)

	// blockCommentRe 剥 /* ... */（跨行）——散文式块注释里的 import/require
	// 假阳性来源。
	blockCommentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reProv         = regexp.MustCompile(`\.provide\(\s*["']([\w.$-]+)["']`)
	reOn           = regexp.MustCompile(`\bctx\.on\(\s*["']([\w./-]+)["']`)
	reImp          = regexp.MustCompile("\\b(?:import|export)\\b[^\"';\\n]*\\bfrom\\s*[\"']([^\"'\\n]+)[\"']|\\bimport\\s*\\(\\s*[\"']([^\"'\\n]+)[\"']|\\brequire\\s*\\(\\s*[\"']([^\"'\\n]+)[\"']|\\bimport\\s+[\"']([^\"'\\n]+)[\"']")
	reDupReg       = regexp.MustCompile(`name:\s*["']([\w.-]+)["']`)
)

type sourceFile struct {
	rel  string
	body string
}

// VerifyPluginDir 对一个 dsh 插件包目录跑静态检查。检查面（映射 #1884）：
// manifest / 入口三件套（name/inject/apply）/ inject 声明 vs ctx 取用一致性 /
// 同名注册 / @deepseek-ai vendored 内部耦合 / 外部依赖与未声明 import /
// 未知事件名。目录不存在或不可读 → error（调用方区分工具故障与检查结论）。
func VerifyPluginDir(dir string) (*Report, error) {
	report := &Report{Dir: filepath.ToSlash(dir)}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s 不是目录", dir)
	}

	// manifest：package.json 必须存在且可解析（dsh 插件以包为单位被 profile
	// 的 pnpm 安装——没有 manifest 就不是插件包）。
	manifestBody, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		report.add(SevError, "manifest", "缺少 package.json——dsh 插件以包为单位安装，目录里必须有 manifest")
		return report, nil
	}
	var manifest struct {
		Name string `json:"name"`
		Type string `json:"type"`
		Main string `json:"main"`
	}
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		report.add(SevError, "manifest", "package.json 解析失败: "+err.Error())
		return report, nil
	}
	if strings.TrimSpace(manifest.Name) == "" {
		report.add(SevError, "manifest", "package.json 缺少 name（dsh 按 包名 管理 profile 依赖）")
	}
	if manifest.Type != "module" {
		report.add(SevInfo, "manifest", "package.json 未声明 \"type\": \"module\"——dsh 插件惯用 ESM 三件套（name/inject/apply），CJS 形态先确认 loader 支持")
	}

	// 收集源文件（跳过 node_modules/.git/dist，确定性排序，量级封顶防失控）。
	files, err := collectSources(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		report.add(SevError, "entry", "包内没有任何 .js/.mjs/.cjs 源文件——入口缺失")
		return report, nil
	}

	// 入口三件套：优先 main 指向的文件，回落包根 index.js。name/inject/apply
	// 的解析都在入口源上做（三件套按 dsh 惯例从入口导出；非入口文件里的
	// 同名 const 不构成导出）。
	entryRel := manifest.Main
	if entryRel == "" {
		entryRel = "index.js"
	}
	// "./index.js" 是真实包最常见的 main 写法——与 collectSources 的 rel 键
	// （无 ./ 前缀）对齐前先归一化，否则合法包被误判入口缺失（审查 P1）。
	entryRel = strings.TrimPrefix(entryRel, "./")
	entryBody, ok := readFileLoose(files, entryRel)
	if !ok {
		report.add(SevError, "entry", "package.json main 指向的 "+entryRel+" 不存在（或未在扫描面内）")
		return report, nil
	}
	pluginName := ""
	if m := reName.FindStringSubmatch(entryBody); m != nil {
		pluginName = m[1]
	}
	if pluginName == "" {
		report.add(SevError, "entry", "入口未声明插件名（export const name = \"...\"）——未命名插件在注册表里互相覆盖")
	}
	declared := map[string]bool{}
	if m := reInject.FindStringSubmatch(entryBody); m != nil {
		for _, s := range reStr.FindAllStringSubmatch(m[1], -1) {
			declared[s[1]] = true
		}
	} else {
		report.add(SevWarn, "entry", "入口未声明 inject——需要服务的插件缺失声明会静默 pending 或运行时抛 cannot get property（#1884 模式 1）")
	}
	if !reApply.MatchString(entryBody) {
		report.add(SevError, "entry", "入口未声明 apply(ctx[, config])——loader 无从挂载插件")
	}

	// inject 声明 vs ctx 取用一致性：used−declared−provided → warn。
	// provided（本包 ctx.provide(\"key\")）视为自给自足。
	used := map[string]bool{}
	provided := map[string]bool{}
	dupRegister := map[string]int{}
	unknownEvents := map[string]bool{}
	externals := map[string]bool{}
	vendored := map[string]bool{}
	for _, f := range files {
		// 先剥块注释，再逐行跳过纯注释行——runner.js 的注释里有 "require"+
		// "utf8" 同行共存的散文（2026-09-16 dogfood 实证），全文扫描会把注释
		// 当 import。行尾 // 不剥（字符串内 // 误伤面大，留作已知边界）。
		code := blockCommentRe.ReplaceAllString(f.body, "")
		var codeLines []string
		for _, line := range strings.Split(code, "\n") {
			if !strings.HasPrefix(strings.TrimSpace(line), "//") {
				codeLines = append(codeLines, line)
			}
		}
		code = strings.Join(codeLines, "\n")
		for _, m := range reCtxKey.FindAllStringSubmatch(code, -1) {
			key := m[1]
			if contextMethods[key] {
				continue
			}
			used[key] = true
		}
		// ctx["key"] 括号形态与 ctx.key 同判（盲区声明：ctx[expr] 计算键取
		// 不到——与 ctx.get 宽容查找一样留待 H2b 运行时面）。
		for _, m := range reCtxBr.FindAllStringSubmatch(code, -1) {
			used[m[1]] = true
		}
		for _, m := range reProv.FindAllStringSubmatch(code, -1) {
			provided[m[1]] = true
		}
		for _, m := range reOn.FindAllStringSubmatch(code, -1) {
			if !knownEvents[m[1]] {
				unknownEvents[m[1]] = true
			}
		}
		for _, m := range reDupReg.FindAllStringSubmatch(code, -1) {
			dupRegister[f.rel+"::"+m[1]]++
		}
		for _, m := range reImp.FindAllStringSubmatch(code, -1) {
			spec := m[1]
			if spec == "" {
				spec = m[2]
			}
			if spec == "" {
				spec = m[3]
			}
			if spec == "" {
				spec = m[4]
			}
			if spec == "" {
				continue // 分支未参与（RE2 多捕获组：未参与的组为空串）
			}
			switch {
			case strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "node:") ||
				strings.HasPrefix(spec, "/") || spec == "node":
				// 相对/node 内置——零依赖纪律的合法面。
			case strings.HasPrefix(spec, "@deepseek-ai/"):
				vendored[spec] = true
			default:
				externals[spec] = true
			}
		}
	}
	for _, key := range sortedKeys(used) {
		if declared[key] || provided[key] {
			continue
		}
		report.add(SevWarn, "ctx-usage",
			"ctx."+key+" 被使用但未出现在 inject 声明，本包也未 provide——运行时该服务缺位即抛 cannot get property 或永久 pending（#1884 模式 1）")
	}
	// 不查"inject 声明未取用"：注入只为等目标服务就绪、从不属性访问是 Cordis
	// 合法惯用法（forge-dsh 本尊 inject ["tools"] 即此形态——2026-09-16 dogfood
	// 实证误报后移除）。
	// 同名注册：同一包内同一注册名出现 ≥2 处（跨文件）→ warn（#1884 模式 2：
	// 同名注册互相覆盖）。同一文件内多行拼一个对象不算（按 文件::名 聚合后
	// 再按 名 跨文件聚合）。
	// 同名注册：同一包内同一注册名跨 ≥2 个文件出现 → warn（#1884 模式 2：
	// 同名注册互相覆盖；同文件内的同名片多为一个对象的多行拼写，不误报）。
	byName := map[string]map[string]bool{}
	for k := range dupRegister {
		parts := strings.SplitN(k, "::", 2)
		file, name := parts[0], parts[1]
		if byName[name] == nil {
			byName[name] = map[string]bool{}
		}
		byName[name][file] = true
	}
	for name, filesByName := range byName {
		if len(filesByName) > 1 {
			report.add(SevWarn, "dup-register",
				fmt.Sprintf("注册名 %q 出现在 %d 个文件——同包跨文件同名注册互相覆盖（#1884 模式 2）", name, len(filesByName)))
		}
	}
	if len(vendored) > 0 {
		report.add(SevWarn, "vendored-import",
			"import 了 @deepseek-ai/* vendored 内部："+strings.Join(sortedKeys(vendored), ", ")+"——那是宿主私有面（#1884 防御实践第一条：只留 node 内置，要什么都从 ctx 拿）")
	}
	if len(externals) > 0 {
		report.add(SevInfo, "external-dep",
			"外部依赖："+strings.Join(sortedKeys(externals), ", ")+"——正式装插件走 profile 内 pnpm（无 pnpm 直接报错），能免则免")
	}
	if len(unknownEvents) > 0 {
		report.add(SevInfo, "unknown-event",
			"ctx.on 了名册之外的事件："+strings.Join(sortedKeys(unknownEvents), ", ")+"——类型化事件名单外静默不触发，确认不是拼写漂移")
	}
	sortReport(report)
	return report, nil
}

func (r *Report) add(sev Severity, check, msg string) {
	r.Findings = append(r.Findings, Finding{Severity: sev, Check: check, Message: msg})
}

func sortReport(r *Report) {
	sort.Slice(r.Findings, func(i, j int) bool {
		order := map[Severity]int{SevError: 0, SevWarn: 1, SevInfo: 2}
		if order[r.Findings[i].Severity] != order[r.Findings[j].Severity] {
			return order[r.Findings[i].Severity] < order[r.Findings[j].Severity]
		}
		if r.Findings[i].Check != r.Findings[j].Check {
			return r.Findings[i].Check < r.Findings[j].Check
		}
		return r.Findings[i].Message < r.Findings[j].Message
	})
}

func sortedKeys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// collectSources 递归收集 dir 下源文件（跳 node_modules/.git/dist 与 test 类
// 目录——测试替身的 import/fixture 不是运行时注册面，按路径排序，文件数/单
// 文件字节封顶——确定性且防失控）。
func collectSources(dir string) ([]sourceFile, error) {
	var out []sourceFile
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			if d != nil && d.IsDir() {
				switch d.Name() {
				case "node_modules", ".git", "dist", "build", "test", "tests", "__tests__", "testdata":
					return fs.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(path, ".js") && !strings.HasSuffix(path, ".mjs") && !strings.HasSuffix(path, ".cjs") &&
			!strings.HasSuffix(path, ".mts") && !strings.HasSuffix(path, ".cts") {
			return nil
		}
		// 测试文件排除：fixture 对象（exec/name 字段）与 dev-only import
		// （如 wiring 测试的 @deepseek-ai/cordis）不是运行时注册面——不排除
		// 会制造假 dup-register / 假 vendored 警报（2026-09-16 dogfood 实证）。
		if base := d.Name(); strings.HasSuffix(base, ".test.js") ||
			strings.HasSuffix(base, ".test.mjs") || strings.HasSuffix(base, ".spec.js") {
			return nil
		}
		if fi, statErr := d.Info(); statErr != nil || fi.Size() > maxFileByte {
			return nil
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			rel = path
		}
		out = append(out, sourceFile{rel: filepath.ToSlash(rel), body: string(body)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(out) > maxFiles {
		out = out[:maxFiles]
	}
	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

func readFileLoose(files []sourceFile, rel string) (string, bool) {
	rel = filepath.ToSlash(rel)
	for _, f := range files {
		if f.rel == rel {
			return f.body, true
		}
	}
	return "", false
}
