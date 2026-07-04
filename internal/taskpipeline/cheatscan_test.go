package taskpipeline

import (
	"strings"
	"testing"
)

// al 是构造 addedLine 的简写（detector 单测用）。
func al(file string, line int, text string) addedLine {
	return addedLine{file: file, lineNo: line, text: text}
}

// TestDetectTypeSuppression 逐条钉 7 类抑制指令的命中 + 不误报普通行/字面量提及。
func TestDetectTypeSuppression(t *testing.T) {
	cases := []struct {
		name  string
		lines []addedLine
		want  int
	}{
		{`@ts-nocheck`, []addedLine{al("a.ts", 1, "// @ts-nocheck")}, 1},
		{`@ts-ignore`, []addedLine{al("a.ts", 9, "// eslint-disable-next-line @typescript-eslint/ban-types // @ts-ignore")}, 1},
		{`eslint-disable`, []addedLine{al("b.ts", 2, "/* eslint-disable no-unused-vars */")}, 1},
		{`Rust #[allow]`, []addedLine{al("c.rs", 3, "#[allow(dead_code)]")}, 1},
		{`Python type: ignore`, []addedLine{al("d.py", 4, "x: int = 's'  # type: ignore")}, 1},
		{`Java @SuppressWarnings`, []addedLine{al("e.java", 5, "@SuppressWarnings(\"unchecked\")")}, 1},
		{`普通代码行不命中`, []addedLine{al("f.go", 6, "func Foo() int { return 1 }")}, 0},
		{`同行多抑制只记一次`, []addedLine{al("g.ts", 7, "// @ts-ignore eslint-disable")}, 1},
		{`双引号串内的指令名不算（字面量提及）`, []addedLine{al("h.go", 8, "s := \"use @ts-ignore here\"")}, 0},
		{`反引号 raw string 内的指令名不算（正则定义）`, []addedLine{al("i.go", 9, "re := regexp.MustCompile(`@ts-ignore`)")}, 0},
		{`尾随注释里的真抑制算（trailing //）`, []addedLine{al("j.ts", 10, "foo(); // @ts-ignore")}, 1},
	}
	for _, c := range cases {
		got := detectTypeSuppression(c.lines)
		if len(got) != c.want {
			t.Errorf(`%s: got %d findings, want %d (%+v)`, c.name, len(got), c.want, got)
		}
		for _, f := range got {
			if f.Severity != "high" {
				t.Errorf(`%s: severity=%s, want high`, c.name, f.Severity)
			}
			if f.File == "" || f.Pattern != CheatTypeSuppression {
				t.Errorf(`%s: bad finding %+v`, c.name, f)
			}
		}
	}
}

// TestDetectErrorSwallow 空 catch / except:pass 命中；有 body 的 catch 不误报。
func TestDetectErrorSwallow(t *testing.T) {
	hit := []string{
		`catch (e) {}`,
		`catch {}`,
		`catch (err: MyError) {}`,
		`except Exception: pass`,
		`except: pass`,
		`} catch (x) { }`,
	}
	for _, src := range hit {
		if len(detectErrorSwallow([]addedLine{al("x.ts", 1, src)})) != 1 {
			t.Errorf(`应命中 error-swallow: %q`, src)
		}
	}
	miss := []string{
		`catch (e) { handleError(e); }`, // 有 body
		`catch (e) {`,                    // 多行 catch 的起始行（body 在后续行）
		`except Exception as e:`,         // 无同行 pass
		`func() error { return nil }`,    // 普通返回
	}
	for _, src := range miss {
		if got := detectErrorSwallow([]addedLine{al("x.ts", 1, src)}); len(got) != 0 {
			t.Errorf(`不应命中 error-swallow: %q → %+v`, src, got)
		}
	}
}

// TestDetectDeadBranch 永假分支命中；合法条件不误报。
func TestDetectDeadBranch(t *testing.T) {
	hit := []string{
		`if (false) {`,
		`if (0) {`,
		`if (1 === 2) doThing();`,
		`if (1 == 2) {`,
		`if false {`,
		`if False:`,
		`while (false) {`,
		`if(false){`,
	}
	for _, src := range hit {
		if len(detectDeadBranch([]addedLine{al("x.go", 1, src)})) != 1 {
			t.Errorf(`应命中 dead-branch: %q`, src)
		}
	}
	miss := []string{
		`if (x === 0) {`,   // 变量比较
		`if (count > 0) {`, // 合法
		`if falsey() {`,    // 不是裸 false（有 boundary）
		`if (0 === x)`,     // 0 后非 ) —— 不误伤
		`return false`,     // 非分支
	}
	for _, src := range miss {
		if got := detectDeadBranch([]addedLine{al("x.go", 1, src)}); len(got) != 0 {
			t.Errorf(`不应命中 dead-branch: %q → %+v`, src, got)
		}
	}
}

// TestDetectCommentOnly 某文件新增行全是注释/空行 → 命中；混入逻辑行不命中。
func TestDetectCommentOnly(t *testing.T) {
	// 全注释文件 → 命中
	got := detectCommentOnly([]addedLine{
		al("only_doc.go", 1, "// 这是个修复"),
		al("only_doc.go", 2, ""),
		al("only_doc.go", 3, "// 见 issue #42"),
	})
	if len(got) != 1 || got[0].File != "only_doc.go" || got[0].Severity != "low" {
		t.Fatalf(`全注释文件应命中 comment-only (low): %+v`, got)
	}
	// 混入逻辑行 → 不命中
	got = detectCommentOnly([]addedLine{
		al("real_fix.go", 1, "// fix bug"),
		al("real_fix.go", 2, "return nil"),
	})
	if len(got) != 0 {
		t.Fatalf(`混入逻辑行不应命中: %+v`, got)
	}
	// 多文件：只标 comment-only 的那个
	got = detectCommentOnly([]addedLine{
		al("a.go", 1, "// doc only"),
		al("b.go", 1, "x := 1"),
	})
	if len(got) != 1 || got[0].File != "a.go" {
		t.Fatalf(`应只标 a.go: %+v`, got)
	}
}

// TestDetectCommentDebt 新增注释行里的债务标记命中；代码行里的标记词/普通注释不误报。
// 钉 comment-as-debt（懒惰阶梯反第 0 级：注释标识问题但不解决）。
func TestDetectCommentDebt(t *testing.T) {
	cases := []struct {
		name  string
		lines []addedLine
		want  int
	}{
		{`英文 TODO 注释`, []addedLine{al("a.go", 1, "// TODO 后续重构这里")}, 1},
		{`FIXME 块注释`, []addedLine{al("b.go", 2, "/* FIXME race condition */")}, 1},
		{`XXX`, []addedLine{al("c.go", 3, "// XXX 这里有坑")}, 1},
		{`HACK`, []addedLine{al("d.go", 4, "// HACK 临时绕过")}, 1},
		{`中文待补`, []addedLine{al("e.go", 5, "// 待补：错误处理")}, 1},
		{`中文稍后`, []addedLine{al("f.go", 6, "// 稍后处理")}, 1},
		{`implement later`, []addedLine{al("g.go", 7, "// implement later when API ready")}, 1},
		{`代码行里的标记词不命中（非注释行）`, []addedLine{al("h.go", 8, "todoList := []string{}")}, 0},
		{`普通注释无债务标记`, []addedLine{al("i.go", 9, "// 这是个正常注释")}, 0},
		{`多行债务各记一次`, []addedLine{al("j.go", 10, "// TODO one"), al("j.go", 11, "// TODO two")}, 2},
		{`稍后通知用户不命中（collocation 降噪 M1）`, []addedLine{al("k.go", 12, "// 稍后通知用户")}, 0},
		{`Implement later 句首大写也命中（?i）`, []addedLine{al("l.go", 13, "// Implement later")}, 1},
		{`regex 定义行不命中（代码行，自扫描防护）`, []addedLine{al("m.go", 14, `var re = regexp.MustCompile("TO"+"DO")`)}, 0},
	}
	for _, c := range cases {
		got := detectCommentDebt(c.lines)
		if len(got) != c.want {
			t.Errorf(`%s: got %d findings, want %d (%+v)`, c.name, len(got), c.want, got)
		}
		for _, f := range got {
			if f.Severity != "low" {
				t.Errorf(`%s: severity=%s, want low`, c.name, f.Severity)
			}
			if f.File == "" || f.Pattern != CheatCommentDebt {
				t.Errorf(`%s: bad finding %+v`, c.name, f)
			}
		}
	}
}

// TestScanCheatPatterns_SelfScanNoCommentDebt 钉死自匹配防护 invariant：扫描器自身
// 源码（debtMarkerWords 拼接 + commentDebtRe 定义）被扫时，comment-as-debt 命中必须
// 为 0——代码行（const/var 赋值）被 isCommentOrBlank 跳过，注释行不连写标记词。这条
// 防护脆弱（有人改成连写就破），e2e 防回归。
func TestScanCheatPatterns_SelfScanNoCommentDebt(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
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

// TestScanCheatPatterns_NoSource 文档/配置变更 → 不扫（isSourceFile 过滤）。
func TestScanCheatPatterns_NoSource(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// .md 文件含 "@ts-ignore" 字样也不该命中——非源码。
	writeCommitSource(t, dir, map[string]string{
		"README.md": "# use @ts-ignore sparingly\n",
	}, "doc only")
	state := newVerifyState(t, dir, "doc")
	if got := ScanCheatPatterns(dir, state); len(got) != 0 {
		t.Fatalf(`非源码不应扫, got %+v`, got)
	}
}

// TestParseNewStart 钉 hunk 头新文件起始行号解析。
func TestParseNewStart(t *testing.T) {
	cases := map[string]int{
		`@@ -10,3 +12,5 @@`:     12,
		`@@ -1,2 +1,8 @@ func`:   1,
		`@@ -0,0 +1,N @@`:       1,
		`garbage`:               0,
		`@@ -10 +12 @@`:         12,
	}
	for hunk, want := range cases {
		if got := parseNewStart(hunk); got != want {
			t.Errorf(`parseNewStart(%q) = %d, want %d`, hunk, got, want)
		}
	}
}

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

// TestCollectAddedLines_CommittedAndUntracked 确认收集器同时覆盖已提交和未跟踪文件。
func TestCollectAddedLines_CommittedAndUntracked(t *testing.T) {
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	// 已提交
	writeCommitSource(t, dir, map[string]string{
		"committed.go": "package main\n\nfunc C() int { return 1 }\n",
	}, "add committed")
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
