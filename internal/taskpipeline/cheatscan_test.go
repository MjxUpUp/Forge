package taskpipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// al 是构造 addedLine 的简写（detector 单测用）。
func al(file string, line int, text string) addedLine {
	return addedLine{file: file, lineNo: line, text: text}
}

// TestDetectors 表驱动五个单行检测器（type-suppression / error-swallow /
// dead-branch / comment-debt / path-assumption）：每行钉一条样本的命中数 + finding
// 形态（pattern/severity/file）。分组语义：
//   - type-suppression：7 类抑制指令命中，普通行/字面量提及不命中，同行多抑制只记一次。
//   - error-swallow：空 catch / except:pass 命中；有 body 的 catch 不误报。
//   - dead-branch：永假分支命中；合法条件不误报。
//   - comment-debt：新增注释行里的债务标记命中（collocation 降噪 M1）；代码行里的
//     标记词/普通注释不误报；钉 comment-as-debt（懒惰阶梯反第 0 级）。
//   - path-assumption：「分隔符当匹配器」指纹（2026-08-19 Windows CI 事故）——
//     前/后缀/包含匹配里用 OS 分隔符 → 上报；路径构造（TrimRight/Join）→ 不报。
func TestDetectors(t *testing.T) {
	cases := []struct {
		name     string
		detect   func([]addedLine) []CheatFinding
		lines    []addedLine
		want     int
		pattern  CheatPattern
		severity string
	}{
		// --- type-suppression（7 类指令 + 误报面）---
		{`@ts-nocheck`, detectTypeSuppression, []addedLine{al("a.ts", 1, "// @ts-nocheck")}, 1, CheatTypeSuppression, "high"},
		{`@ts-ignore`, detectTypeSuppression, []addedLine{al("a.ts", 9, "// eslint-disable-next-line @typescript-eslint/ban-types // @ts-ignore")}, 1, CheatTypeSuppression, "high"},
		{`eslint-disable`, detectTypeSuppression, []addedLine{al("b.ts", 2, "/* eslint-disable no-unused-vars */")}, 1, CheatTypeSuppression, "high"},
		{`Rust #[allow]`, detectTypeSuppression, []addedLine{al("c.rs", 3, "#[allow(dead_code)]")}, 1, CheatTypeSuppression, "high"},
		{`Python type: ignore`, detectTypeSuppression, []addedLine{al("d.py", 4, "x: int = 's'  # type: ignore")}, 1, CheatTypeSuppression, "high"},
		{`Java @SuppressWarnings`, detectTypeSuppression, []addedLine{al("e.java", 5, "@SuppressWarnings(\"unchecked\")")}, 1, CheatTypeSuppression, "high"},
		{`普通代码行不命中`, detectTypeSuppression, []addedLine{al("f.go", 6, "func Foo() int { return 1 }")}, 0, CheatTypeSuppression, "high"},
		{`同行多抑制只记一次`, detectTypeSuppression, []addedLine{al("g.ts", 7, "// @ts-ignore eslint-disable")}, 1, CheatTypeSuppression, "high"},
		{`双引号串内的指令名不算（字面量提及）`, detectTypeSuppression, []addedLine{al("h.go", 8, "s := \"use @ts-ignore here\"")}, 0, CheatTypeSuppression, "high"},
		{`反引号 raw string 内的指令名不算（正则定义）`, detectTypeSuppression, []addedLine{al("i.go", 9, "re := regexp.MustCompile(`@ts-ignore`)")}, 0, CheatTypeSuppression, "high"},
		{`尾随注释里的真抑制算（trailing //）`, detectTypeSuppression, []addedLine{al("j.ts", 10, "foo(); // @ts-ignore")}, 1, CheatTypeSuppression, "high"},
		// --- error-swallow ---
		{`error-swallow: catch (e) {}`, detectErrorSwallow, []addedLine{al("x.ts", 1, `catch (e) {}`)}, 1, CheatErrorSwallow, "high"},
		{`error-swallow: catch {}`, detectErrorSwallow, []addedLine{al("x.ts", 1, `catch {}`)}, 1, CheatErrorSwallow, "high"},
		{`error-swallow: catch (err: MyError) {}`, detectErrorSwallow, []addedLine{al("x.ts", 1, `catch (err: MyError) {}`)}, 1, CheatErrorSwallow, "high"},
		{`error-swallow: except Exception: pass`, detectErrorSwallow, []addedLine{al("x.ts", 1, `except Exception: pass`)}, 1, CheatErrorSwallow, "high"},
		{`error-swallow: except: pass`, detectErrorSwallow, []addedLine{al("x.ts", 1, `except: pass`)}, 1, CheatErrorSwallow, "high"},
		{`error-swallow: } catch (x) { }`, detectErrorSwallow, []addedLine{al("x.ts", 1, `} catch (x) { }`)}, 1, CheatErrorSwallow, "high"},
		{`error-swallow 不误报: 有 body`, detectErrorSwallow, []addedLine{al("x.ts", 1, `catch (e) { handleError(e); }`)}, 0, CheatErrorSwallow, "high"},
		{`error-swallow 不误报: 多行 catch 起始行`, detectErrorSwallow, []addedLine{al("x.ts", 1, `catch (e) {`)}, 0, CheatErrorSwallow, "high"},
		{`error-swallow 不误报: 无同行 pass`, detectErrorSwallow, []addedLine{al("x.ts", 1, `except Exception as e:`)}, 0, CheatErrorSwallow, "high"},
		{`error-swallow 不误报: 普通返回`, detectErrorSwallow, []addedLine{al("x.ts", 1, `func() error { return nil }`)}, 0, CheatErrorSwallow, "high"},
		// --- dead-branch ---
		{`dead-branch: if (false) {`, detectDeadBranch, []addedLine{al("x.go", 1, `if (false) {`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: if (0) {`, detectDeadBranch, []addedLine{al("x.go", 1, `if (0) {`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: if (1 === 2) doThing();`, detectDeadBranch, []addedLine{al("x.go", 1, `if (1 === 2) doThing();`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: if (1 == 2) {`, detectDeadBranch, []addedLine{al("x.go", 1, `if (1 == 2) {`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: if false {`, detectDeadBranch, []addedLine{al("x.go", 1, `if false {`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: if False:`, detectDeadBranch, []addedLine{al("x.go", 1, `if False:`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: while (false) {`, detectDeadBranch, []addedLine{al("x.go", 1, `while (false) {`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch: if(false){`, detectDeadBranch, []addedLine{al("x.go", 1, `if(false){`)}, 1, CheatDeadBranch, "high"},
		{`dead-branch 不误报: 变量比较`, detectDeadBranch, []addedLine{al("x.go", 1, `if (x === 0) {`)}, 0, CheatDeadBranch, "high"},
		{`dead-branch 不误报: 合法条件`, detectDeadBranch, []addedLine{al("x.go", 1, `if (count > 0) {`)}, 0, CheatDeadBranch, "high"},
		{`dead-branch 不误报: 非裸 false（有 boundary）`, detectDeadBranch, []addedLine{al("x.go", 1, `if falsey() {`)}, 0, CheatDeadBranch, "high"},
		{`dead-branch 不误报: 0 后非 )`, detectDeadBranch, []addedLine{al("x.go", 1, `if (0 === x)`)}, 0, CheatDeadBranch, "high"},
		{`dead-branch 不误报: 非分支`, detectDeadBranch, []addedLine{al("x.go", 1, `return false`)}, 0, CheatDeadBranch, "high"},
		// --- comment-debt ---
		{`英文 TODO 注释`, detectCommentDebt, []addedLine{al("a.go", 1, "// TODO 后续重构这里")}, 1, CheatCommentDebt, "low"},
		{`FIXME 块注释`, detectCommentDebt, []addedLine{al("b.go", 2, "/* FIXME race condition */")}, 1, CheatCommentDebt, "low"},
		{`XXX`, detectCommentDebt, []addedLine{al("c.go", 3, "// XXX 这里有坑")}, 1, CheatCommentDebt, "low"},
		{`HACK`, detectCommentDebt, []addedLine{al("d.go", 4, "// HACK 临时绕过")}, 1, CheatCommentDebt, "low"},
		{`中文待补`, detectCommentDebt, []addedLine{al("e.go", 5, "// 待补：错误处理")}, 1, CheatCommentDebt, "low"},
		{`中文稍后`, detectCommentDebt, []addedLine{al("f.go", 6, "// 稍后处理")}, 1, CheatCommentDebt, "low"},
		{`implement later`, detectCommentDebt, []addedLine{al("g.go", 7, "// implement later when API ready")}, 1, CheatCommentDebt, "low"},
		{`代码行里的标记词不命中（非注释行）`, detectCommentDebt, []addedLine{al("h.go", 8, "todoList := []string{}")}, 0, CheatCommentDebt, "low"},
		{`普通注释无债务标记`, detectCommentDebt, []addedLine{al("i.go", 9, "// 这是个正常注释")}, 0, CheatCommentDebt, "low"},
		{`多行债务各记一次`, detectCommentDebt, []addedLine{al("j.go", 10, "// TODO one"), al("j.go", 11, "// TODO two")}, 2, CheatCommentDebt, "low"},
		{`稍后通知用户不命中（collocation 降噪 M1）`, detectCommentDebt, []addedLine{al("k.go", 12, "// 稍后通知用户")}, 0, CheatCommentDebt, "low"},
		{`Implement later 句首大写也命中（?i）`, detectCommentDebt, []addedLine{al("l.go", 13, "// Implement later")}, 1, CheatCommentDebt, "low"},
		{`regex 定义行不命中（代码行，自扫描防护）`, detectCommentDebt, []addedLine{al("m.go", 14, `var re = regexp.MustCompile("TO"+"DO")`)}, 0, CheatCommentDebt, "low"},
		// --- path-assumption（分隔符当匹配器 → 上报；构造用途 → 不报）---
		{`path-assumption: HasPrefix`, detectPathAssumption, []addedLine{al("x.go", 1, `if strings.HasPrefix(line, string(filepath.Separator)) {`)}, 1, CheatPathAssumption, "high"},
		{`path-assumption: Contains`, detectPathAssumption, []addedLine{al("x.go", 1, `strings.Contains(p, string(filepath.Separator))`)}, 1, CheatPathAssumption, "high"},
		{`path-assumption: HasSuffix`, detectPathAssumption, []addedLine{al("x.go", 1, `ok := strings.HasSuffix(name, string(filepath.Separator))`)}, 1, CheatPathAssumption, "high"},
		{`path-assumption: 首参嵌套构造调用（真实高发形态）`, detectPathAssumption, []addedLine{al("x.go", 1, `strings.HasPrefix(filepath.Base(p), string(filepath.Separator))`)}, 1, CheatPathAssumption, "high"},
		{`path-assumption: LastIndex`, detectPathAssumption, []addedLine{al("x.go", 1, `strings.LastIndex(name, string(filepath.Separator))`)}, 1, CheatPathAssumption, "high"},
		{`path-assumption 不误报: TrimRight 构造用途合法`, detectPathAssumption, []addedLine{al("x.go", 1, `return filepath.Base(strings.TrimRight(line, string(filepath.Separator)))`)}, 0, CheatPathAssumption, "high"},
		{`path-assumption 不误报: Join`, detectPathAssumption, []addedLine{al("x.go", 1, `dst := filepath.Join(toDir, rel)`)}, 0, CheatPathAssumption, "high"},
		{`path-assumption 不误报: 硬编码斜杠不在本模式范围（太吵）`, detectPathAssumption, []addedLine{al("x.go", 1, `if strings.HasPrefix(line, "/") {`)}, 0, CheatPathAssumption, "high"},
		{`path-assumption 不误报: 单独取分隔符不是匹配行为`, detectPathAssumption, []addedLine{al("x.go", 1, `sep := string(filepath.Separator)`)}, 0, CheatPathAssumption, "high"},
	}
	for _, c := range cases {
		got := c.detect(c.lines)
		if len(got) != c.want {
			t.Errorf(`%s: got %d findings, want %d (%+v)`, c.name, len(got), c.want, got)
			continue
		}
		for _, f := range got {
			if f.Pattern != c.pattern || f.Severity != c.severity || f.File == "" {
				t.Errorf(`%s: bad finding %+v (want %s/%s)`, c.name, f, c.pattern, c.severity)
			}
		}
	}
}

// TestDetectCommentOnly: a file's added lines are all comments/blank lines → hit; mixed-in logic lines do not hit.
//
// TestDetectCommentOnly 某文件新增行全是注释/空行 → 命中；混入逻辑行不命中。
func TestDetectCommentOnly(t *testing.T) {
	// all-comment file → hit
	//
	// 全注释文件 → 命中
	got := detectCommentOnly([]addedLine{
		al("only_doc.go", 1, "// 这是个修复"),
		al("only_doc.go", 2, ""),
		al("only_doc.go", 3, "// 见 issue #42"),
	})
	if len(got) != 1 || got[0].File != "only_doc.go" || got[0].Severity != "low" {
		t.Fatalf(`全注释文件应命中 comment-only (low): %+v`, got)
	}
	// mixed-in logic line → no hit
	//
	// 混入逻辑行 → 不命中
	got = detectCommentOnly([]addedLine{
		al("real_fix.go", 1, "// fix bug"),
		al("real_fix.go", 2, "return nil"),
	})
	if len(got) != 0 {
		t.Fatalf(`混入逻辑行不应命中: %+v`, got)
	}
	// multi-file: only mark the comment-only one
	//
	// 多文件：只标 comment-only 的那个
	got = detectCommentOnly([]addedLine{
		al("a.go", 1, "// doc only"),
		al("b.go", 1, "x := 1"),
	})
	if len(got) != 1 || got[0].File != "a.go" {
		t.Fatalf(`应只标 a.go: %+v`, got)
	}
}

// TestScanCheatPatterns_SelfScanNoCommentDebt pins the self-match protection invariant: when the scanner's own
// source code (debtMarkerWords concatenation + commentDebtRe definition) is scanned, comment-as-debt hits must
// be 0 — code lines (const/var assignments) are skipped by isCommentOrBlank, and comment lines do not write marker words consecutively. This
// protection is fragile (changing it to consecutive writes breaks it), guarded by e2e against regression.
//
// TestScanCheatPatterns_SelfScanNoCommentDebt 钉死自匹配防护 invariant：扫描器自身
// 源码（debtMarkerWords 拼接 + commentDebtRe 定义）被扫时，comment-as-debt 命中必须
// 为 0——代码行（const/var 赋值）被 isCommentOrBlank 跳过，注释行不连写标记词。这条
// 防护脆弱（有人改成连写就破），e2e 防回归。
func TestScanCheatPatterns_SelfScanNoCommentDebt(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// mini scanner source: regex definition uses string concatenation (code line); comments do not write any marker words consecutively.
	//
	// 迷你扫描器源：regex 定义用字符串拼接（代码行），注释不连写任何标记词。
	writeCommitSource(t, dir, map[string]string{
		"scan.go": "package main\n" +
			"import \"regexp\"\n" +
			"// detectCommentDebt 注释描述策略但不连写标记词\n" +
			"const words = \"TO\" + \"DO\" + \"|FIX\" + \"ME\"\n" +
			"var re = regexp.MustCompile(`\\b(` + words + `)\\b`)\n" +
			"var re2 = regexp.MustCompile(`稍后(处理|实现)`)\n" +
			"func F() { _ = re; _ = re2 }\n",
	}, "add scanner self")
	state := newVerifyState(t, dir, "self-scan")
	findings := ScanCheatPatterns(dir, state)
	for _, f := range findings {
		if f.Pattern == CheatCommentDebt {
			t.Errorf(`扫描器自身源码不应命中 comment-as-debt（自匹配防护破了吗？）: %+v`, f)
		}
	}
}

// TestScanCheatPatterns_RealGitDiff end-to-end: committed source containing 4 types of cheats → all detected.
// Uses the real git diff path (collectAddedLines goes through git diff -U0).
//
// TestScanCheatPatterns_RealGitDiff 端到端：committed 源码含 4 类作弊 → 全检出。
// 用真实 git diff 路径（collectAddedLines 走 git diff -U0）。
func TestScanCheatPatterns_RealGitDiff(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"cheat.go": "package main\n" +
			"import \"errors\"\n" +
			"// @ts-ignore 不该在 go 里但测正则\n" +
			"func Dead() { if false { panic(1) } }\n" +
			"func Swallow() { _ = errors.New(\"x\"); defer func(){ _ = recover() }() }\n",
	}, "add cheats")

	state := newVerifyState(t, dir, "cheat-committed")
	findings := ScanCheatPatterns(dir, state)
	pats := make(map[CheatPattern]int)
	for _, f := range findings {
		pats[f.Pattern]++
	}
	if pats[CheatDeadBranch] == 0 {
		t.Errorf(`应检出 dead-branch (if false), findings=%+v`, findings)
	}
	if pats[CheatTypeSuppression] == 0 {
		t.Errorf(`应检出 type-suppression (@ts-ignore), findings=%+v`, findings)
	}
}

// TestScanCheatPatterns_UntrackedFiles: cheats in untracked files (just created by agent, not git add'd)
// can also be detected — collectAddedLines goes through the whole-file read path.
//
// TestScanCheatPatterns_UntrackedFiles 未跟踪文件（agent 刚建未 git add）的作弊
// 也能检出——collectAddedLines 走整文件读路径。
func TestScanCheatPatterns_UntrackedFiles(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeUntracked(t, dir, map[string]string{
		"new.ts": "// @ts-nocheck\nexport const x: number = 's'\ntry {} catch (e) {}\n",
	})
	state := newVerifyState(t, dir, "cheat-untracked")
	findings := ScanCheatPatterns(dir, state)
	pats := make(map[CheatPattern]int)
	for _, f := range findings {
		pats[f.Pattern]++
	}
	if pats[CheatTypeSuppression] == 0 {
		t.Errorf(`未跟踪文件应检出 type-suppression (@ts-nocheck), findings=%+v`, findings)
	}
	if pats[CheatErrorSwallow] == 0 {
		t.Errorf(`未跟踪文件应检出 error-swallow (catch {}), findings=%+v`, findings)
	}
}

// TestScanCheatPatterns_CleanDiff: clean code (no cheats) → zero findings.
//
// TestScanCheatPatterns_CleanDiff 干净代码（无作弊）→ 零 findings。
func TestScanCheatPatterns_CleanDiff(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"clean.go": "package main\n\nfunc Add(a, b int) int { return a + b }\n",
	}, "add clean code")
	state := newVerifyState(t, dir, "clean")
	if got := ScanCheatPatterns(dir, state); len(got) != 0 {
		t.Fatalf(`干净代码应零 findings, got %+v`, got)
	}
}

// TestScanCheatPatterns_NoSource: doc/config changes → not scanned (isSourceFile filter).
//
// TestScanCheatPatterns_NoSource 文档/配置变更 → 不扫（isSourceFile 过滤）。
func TestScanCheatPatterns_NoSource(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// .md files containing the @ts-ignore text should not hit either — not source code.
	//
	// .md 文件含"@ts-ignore"字样也不该命中——非源码。
	writeCommitSource(t, dir, map[string]string{
		"README.md": "# use @ts-ignore sparingly\n",
	}, "doc only")
	state := newVerifyState(t, dir, "doc")
	if got := ScanCheatPatterns(dir, state); len(got) != 0 {
		t.Fatalf(`非源码不应扫, got %+v`, got)
	}
}

// TestParseNewStart pins the parsing of the new-file start line number in hunk headers.
//
// TestParseNewStart 钉 hunk 头新文件起始行号解析。
func TestParseNewStart(t *testing.T) {
	cases := map[string]int{
		`@@ -10,3 +12,5 @@`:    12,
		`@@ -1,2 +1,8 @@ func`: 1,
		`@@ -0,0 +1,N @@`:      1,
		`garbage`:              0,
		`@@ -10 +12 @@`:        12,
	}
	for hunk, want := range cases {
		if got := parseNewStart(hunk); got != want {
			t.Errorf(`parseNewStart(%q) = %d, want %d`, hunk, got, want)
		}
	}
}

// TestCheatScanDetail pins the detail summary format (clean vs hit).
//
// TestCheatScanDetail 钉 detail 摘要格式（干净 vs 命中）。
func TestCheatScanDetail(t *testing.T) {
	if got := cheatScanDetail(nil); !strings.Contains(got, "no ") {
		t.Fatalf(`空 findings 的 detail 应说明干净: %q`, got)
	}
	got := cheatScanDetail([]CheatFinding{
		{Pattern: CheatDeadBranch, File: "a.go", Severity: "high"},
		{Pattern: CheatDeadBranch, File: "b.go", Severity: "high"},
		{Pattern: CheatCommentOnly, File: "c.go", Severity: "low"},
	})
	if !strings.Contains(got, "dead-branch=2") || !strings.Contains(got, "comment-only-fix=1") {
		t.Fatalf(`detail 应含按模式计数: %q`, got)
	}
}

// TestCollectAddedLines_CommittedAndUntracked confirms the collector covers both committed and untracked files.
//
// TestCollectAddedLines_CommittedAndUntracked 确认收集器同时覆盖已提交和未跟踪文件。
func TestCollectAddedLines_CommittedAndUntracked(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// committed
	//
	// 已提交
	writeCommitSource(t, dir, map[string]string{
		"committed.go": "package main\n\nfunc C() int { return 1 }\n",
	}, "add committed")
	// untracked
	//
	// 未跟踪
	writeUntracked(t, dir, map[string]string{
		"untracked.go": "package main\n\nfunc U() int { return 2 }\n",
	})
	state := newVerifyState(t, dir, "mixed")
	added := collectAddedLines(dir, state)
	files := make(map[string]bool)
	for _, a := range added {
		files[a.file] = true
	}
	if !files["committed.go"] {
		t.Errorf(`已提交文件的新增行未收集: %+v`, files)
	}
	if !files["untracked.go"] {
		t.Errorf(`未跟踪文件的新增行未收集: %+v`, files)
	}
	// content check: untracked's func U line should be present
	//
	// 内容核对：untracked 的 func U 行应在
	foundU := false
	for _, a := range added {
		if a.file == "untracked.go" && strings.Contains(a.text, "func U()") {
			foundU = true
		}
	}
	if !foundU {
		t.Error(`未跟踪文件的 "func U()" 行未在收集结果中`)
	}
}

// TestCollectAddedLines_BatchedTrackedNoDup pins the batched tracked-set probe
// (gitTrackedSet, 2026-08-29 review round): a TRACKED file's added lines must appear
// EXACTLY ONCE (from git diff) — if the one-shot `git ls-files` set misjudged it as
// untracked, readFileAddedLines would duplicate every line. Untracked files are still
// read in full. Detection verdicts unchanged vs the old per-file probe.
//
// TestCollectAddedLines_BatchedTrackedNoDup 钉批量 tracked 集合探测（gitTrackedSet，
// 2026-08-29 审查轮）：TRACKED 文件的新增行必须恰好出现一次（来自 git diff）——若
// 一次 `git ls-files` 建的集合把它误判成 untracked，readFileAddedLines 会把每行复制
// 一份。untracked 文件仍整文件读。检测判定相对旧的逐文件探测不变。
func TestCollectAddedLines_BatchedTrackedNoDup(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"tracked.go": "package main\n\nfunc T() int { return 1 }\n",
	}, "add tracked")
	writeUntracked(t, dir, map[string]string{
		"untracked2.go": "package main\n\nfunc U2() int { return 2 }\n",
	})
	state := newVerifyState(t, dir, "batch-tracked")
	added := collectAddedLines(dir, state)
	countT, countU := 0, 0
	for _, a := range added {
		if a.file == "tracked.go" && strings.Contains(a.text, "func T()") {
			countT++
		}
		if a.file == "untracked2.go" && strings.Contains(a.text, "func U2()") {
			countU++
		}
	}
	if countT != 1 {
		t.Errorf(`tracked 文件的 "func T()" 行应恰好 1 次（误判 untracked 会整读成 2 次）, got %d`, countT)
	}
	if countU != 1 {
		t.Errorf(`untracked 文件的 "func U2()" 行应恰好 1 次, got %d`, countU)
	}
}

// TestDetectPhantomImport pins the phantom-import detector: relative imports that resolve to a
// real file on disk pass (incl. extension-omitted, directory index, package __init__), ones that
// resolve to nothing are flagged (severity=high). Bare package names and aliases are out of scope
// (not filesystem-resolvable). Real files are needed → t.TempDir fixture.
//
// TestDetectPhantomImport 钉 phantom-import 检测器：能解析到磁盘真实文件的相对 import
// 放行（含省略扩展名、目录 index、包 __init__），解析不到的上报（severity=high）。裸
// 包名与别名不在范围（无法靠文件系统解析）。需要真实文件 → t.TempDir 搭 fixture。
func TestDetectPhantomImport(t *testing.T) {
	dir := t.TempDir()
	mk := func(rel string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// On-disk fixture: resolvable targets.
	//
	// 磁盘 fixture：可解析的目标。
	mk("src/util.ts")
	mk("src/components/index.tsx")
	mk("src/style.css")
	mk("pkg/sibling.py")
	mk("pkg/sib/__init__.py")
	// PEP 420 namespace package: a directory with NO __init__.py.
	//
	// PEP 420 namespace 包：无 __init__.py 的目录。
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "nspkg"), 0o755); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		line addedLine
		want int
	}{
		// --- hits (resolve to nothing) ---
		//
		// --- 命中（解析不到）---
		{`TS import 幽灵文件`, al("src/a.ts", 1, `import { x } from './ghost'`), 1},
		{`TS require 幽灵文件`, al("src/b.ts", 2, `const y = require('./missing')`), 1},
		{`TS export-from 幽灵文件`, al("src/c.ts", 3, `export * from './nope'`), 1},
		{`TS 上层目录幽灵`, al("src/deep/d.ts", 4, `import z from '../void'`), 1},
		{`Python 相对幽灵模块`, al("pkg/mod.py", 5, `from .ghost import thing`), 1},
		{`Python 上溯幽灵`, al("pkg/sub/m.py", 6, `from ..other.mod import thing`), 1},
		// --- misses (resolvable or out of scope) ---
		//
		// --- 不命中（可解析或超范围）---
		{`TS 省略扩展名可解析`, al("src/e.ts", 7, `import { u } from './util'`), 0},
		{`TS 目录 index 可解析`, al("src/f.ts", 8, `import C from './components'`), 0},
		{`TS 显式扩展名精确命中`, al("src/g.ts", 9, `import './style.css'`), 0},
		{`TS 裸包名不在范围`, al("src/h.ts", 10, `import _ from 'lodash'`), 0},
		{`TS 别名不在范围`, al("src/i.ts", 11, `import x from '@/lib/x'`), 0},
		{`Python 同包兄弟可解析`, al("pkg/mod.py", 12, `from .sibling import helper`), 0},
		{`Python 包目录 __init__ 可解析`, al("pkg/mod.py", 13, `from .sib import helper`), 0},
		{`Python from-dot-import 形式跳过`, al("pkg/mod.py", 14, `from . import helper`), 0},
		{`Python 绝对 import 不在范围`, al("pkg/mod.py", 15, `from os import path`), 0},
		{`TS NodeNext .js 后缀映射到 .ts`, al("src/k.ts", 17, `import { u } from './util.js'`), 0},
		{`TS NodeNext .js 后缀但真不存在`, al("src/m.ts", 18, `import z from './ghost2.js'`), 1},
		{`TS 资源查询后缀剥离`, al("src/l.ts", 19, `import s from './style.css?inline'`), 0},
		{`Python namespace 包目录（无 __init__）`, al("pkg/mod.py", 20, `from .nspkg import thing`), 0},
		{`Go 文件不进 TS/Py 分支`, al("src/j.go", 16, `// import x from './ghost'`), 0},
	}
	for _, c := range cases {
		got := detectPhantomImport(dir, []addedLine{c.line})
		if len(got) != c.want {
			t.Errorf(`%s: got %d findings, want %d (%+v)`, c.name, len(got), c.want, got)
		}
		for _, f := range got {
			if f.Severity != "high" || f.Pattern != CheatPhantomImport || f.File != c.line.file {
				t.Errorf(`%s: bad finding %+v`, c.name, f)
			}
		}
	}
}
