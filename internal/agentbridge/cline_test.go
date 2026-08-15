package agentbridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// clineHooksPathUnder joins the cline global hook dir under an isolated home.
//
// clineHooksPathUnder 拼出隔离 home 下的 cline 全局 hook 目录。
func clineHooksPathUnder(home string) string {
	return filepath.Join(home, "Documents", "Cline", "Rules", "Hooks")
}

// TestClineTranslator_Translate_WritesWrappers: Translate writes exactly the four
// wrapper scripts (PreToolUse/PostToolUse/UserPromptSubmit/TaskStart — named exactly
// the cline hook types, no extension), each carrying the forge marker and the
// `--agent cline` suffix; Stop/PostCompact (no cline channel) and TaskResume/
// TaskCancel (no forge analogue) stay unwired; nothing is written into the project
// dir (zero-project-write contract).
//
// TestClineTranslator_Translate_WritesWrappers：Translate 恰好写四个 wrapper 脚本
// （PreToolUse/PostToolUse/UserPromptSubmit/TaskStart——名字精确等于 cline hook 类型、
// 无扩展名），各带 forge 标记与 `--agent cline` 后缀；Stop/PostCompact（无 cline
// 通道）与 TaskResume/TaskCancel（无 forge 对应物）保持未接线；项目目录零写入
// （零项目写入契约）。
func TestClineTranslator_Translate_WritesWrappers(t *testing.T) {
	home := isolateHome(t)
	dir := clineHooksPathUnder(home)
	project := t.TempDir()

	if err := (&ClineTranslator{}).Translate(project, testInput()); err != nil {
		t.Fatalf("Translate failed: %v", err)
	}
	for _, event := range []string{"PreToolUse", "PostToolUse", "UserPromptSubmit", "TaskStart"} {
		data, err := os.ReadFile(filepath.Join(dir, event))
		if err != nil {
			t.Fatalf("wrapper script %s not written: %v", event, err)
		}
		content := string(data)
		if !strings.Contains(content, clineWrapperMarker) {
			t.Errorf("%s missing the forge marker", event)
		}
		if !strings.Contains(content, "forge hook \"$hook\" --agent cline") {
			t.Errorf("%s missing the --agent cline fan-out call", event)
		}
		if !strings.HasPrefix(content, "#!/bin/sh") {
			t.Errorf("%s must start with a #!/bin/sh shebang (cline requires executable scripts; macOS /bin/sh is bash-3.2 posix)", event)
		}
	}
	// Events with no cline channel must NOT get a script — a Stop/PostCompact script
	// would simply never fire, but naming drift here is the regression signal.
	//
	// 无 cline 通道的 event 不得有脚本——Stop/PostCompact 脚本只是永不触发，但此处
	// 命名漂移是回退信号。
	for _, absent := range []string{"Stop", "PostCompact", "TaskResume", "TaskCancel"} {
		if _, err := os.Stat(filepath.Join(dir, absent)); err == nil {
			t.Errorf("script %s must not exist (no forge/cline mapping for it)", absent)
		}
	}
	// Zero project writes.
	//
	// 零项目写入。
	entries, err := os.ReadDir(project)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("Translate wrote into the project dir (%d entries) — cline wiring must stay user-level", len(entries))
	}
}

// TestClineTranslator_RosterMirrorsSpec: the per-event hook rosters baked into the
// wrapper scripts must equal ForgeHookSpec's deduped command sets (single source of
// truth — a hand-maintained roster would wire different gates than every other host).
// SessionStart's roster must land on TaskStart.
//
// TestClineTranslator_RosterMirrorsSpec：烘焙进 wrapper 脚本的每 event hook roster
// 必须等于 ForgeHookSpec 去重后的命令集（单一真相源——手工维护的 roster 会接出与
// 其他 host 不同的 gate）。SessionStart 的 roster 必须落在 TaskStart 上。
func TestClineTranslator_RosterMirrorsSpec(t *testing.T) {
	rosters := clineRosters()
	spec := hooks.ForgeHookSpec()
	for _, e := range clineEventMappings {
		want := map[string]bool{}
		for _, m := range spec[e.specEvent] {
			for _, h := range m.Hooks {
				want[h.Command] = true
			}
		}
		got := map[string]bool{}
		for _, name := range rosters[e.clineEvent] {
			got["forge hook "+name] = true
		}
		if !stringSetEqual(want, got) {
			t.Errorf("cline %s roster drifted from ForgeHookSpec[%s]:\n  spec:  %s\n  cline: %s", e.clineEvent, e.specEvent, sortedSet(want), sortedSet(got))
		}
	}
	// Order sanity: the roster must be non-empty for every wired event (an empty
	// roster would make the wrapper a silent no-op).
	//
	// 顺序健全性：每个已接线 event 的 roster 必须非空（空 roster 会让 wrapper 静默
	// 空转）。
	for _, e := range clineEventMappings {
		if len(rosters[e.clineEvent]) == 0 {
			t.Errorf("cline %s roster is empty — wrapper would be a no-op", e.clineEvent)
		}
	}
}

// TestClineWrapperScript_MergeLogic pins the wrapper's verdict-merge structure: the
// exit-2 early return forwards the ready-made cancel JSON verbatim, and the envelope
// surgery lines strip exactly the shape emitClineOutput prints. Structural pin (not
// executed — cline runs scripts on macOS/Linux only, which go test on Windows cannot
// verify; the lines are asserted so a change to either side of this pair trips a test).
//
// TestClineWrapperScript_MergeLogic 钉死 wrapper 的结论合并结构：exit-2 早退原样转发
// 现成的 cancel JSON，信封手术行精确剥掉 emitClineOutput 打印的形态。结构性钉住
// （不执行——cline 仅在 macOS/Linux 跑脚本，Windows 上 go test 无法验证；断言这些行，
// 使这对契约的任一侧改动都会触发测试）。
func TestClineWrapperScript_MergeLogic(t *testing.T) {
	script := buildClineWrapperScript("PreToolUse", []string{"task-guard", "skill-trigger"})
	// Block path: first exit 2 forwards the hook's stdout and exits.
	//
	// 阻断路径：首个 exit 2 转发该 hook 的 stdout 并退出。
	if !strings.Contains(script, `if [ "$status" -eq 2 ]; then`) {
		t.Error("wrapper missing the exit-2 block check")
	}
	// Envelope surgery: the case pattern and the prefix-strip must match
	// emitClineOutput's compact emission `{"cancel":false,"contextModification":…}`
	// character-for-character, or every allow context is silently dropped.
	//
	// 信封手术：case pattern 与前缀剥除必须与 emitClineOutput 的紧凑产出
	// `{"cancel":false,"contextModification":…}` 逐字符一致，否则每条 allow 上下文
	// 被静默丢弃。
	if !strings.Contains(script, `'{"cancel":false,"contextModification":'*'}'`) {
		t.Error("wrapper case pattern no longer matches emitClineOutput's envelope shape")
	}
	if !strings.Contains(script, `piece=${out#'{"cancel":false,"contextModification":'}`) {
		t.Error("wrapper prefix strip no longer matches emitClineOutput's envelope shape")
	}
	// The final envelope must be re-composed, not passed through raw.
	//
	// 最终信封必须重组，而非原样透传。
	if !strings.Contains(script, `printf '{"cancel":false,"contextModification":"%s"}\n' "$context"`) {
		t.Error("wrapper missing the merged contextModification emission")
	}
}

// TestClineTranslator_Idempotent: a second Translate is a byte-identical no-op.
//
// TestClineTranslator_Idempotent：二次 Translate 是逐字节相同的 no-op。
func TestClineTranslator_Idempotent(t *testing.T) {
	home := isolateHome(t)
	dir := clineHooksPathUnder(home)

	tr := &ClineTranslator{}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	var first []byte
	for _, e := range clineEventMappings {
		data, err := os.ReadFile(filepath.Join(dir, e.clineEvent))
		if err != nil {
			t.Fatalf("read %s: %v", e.clineEvent, err)
		}
		if e.clineEvent == "PreToolUse" {
			first = data
		}
	}
	if err := tr.Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("second Translate: %v", err)
	}
	second, err := os.ReadFile(filepath.Join(dir, "PreToolUse"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Error("second Translate not idempotent")
	}
}

// TestClineTranslator_RefusesUserScript: a pre-existing script WITHOUT the forge
// marker must make Translate refuse BEFORE writing anything (cline runs one script
// per event — overwriting would steal the user's channel), and a refresh of a stale
// forge wrapper (marker present) must succeed.
//
// TestClineTranslator_RefusesUserScript：已存在且不带 forge 标记的脚本必须让
// Translate 在写入任何东西之前拒绝（cline 每 event 一个脚本——覆写等于抢用户的通
// 道），而刷新陈旧 forge wrapper（带标记）必须成功。
func TestClineTranslator_RefusesUserScript(t *testing.T) {
	home := isolateHome(t)
	dir := clineHooksPathUnder(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	user := filepath.Join(dir, "PreToolUse")
	if err := os.WriteFile(user, []byte("#!/bin/sh\n# my own lint hook\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := (&ClineTranslator{}).Translate(t.TempDir(), testInput()); err == nil {
		t.Fatal("Translate must refuse when a user script occupies an event slot")
	}
	// Refusal must happen before ANY write: no other event got wired.
	//
	// 拒绝必须发生在任何写入之前：其他 event 未被接线。
	for _, e := range clineEventMappings {
		if e.clineEvent == "PreToolUse" {
			continue
		}
		if _, serr := os.Stat(filepath.Join(dir, e.clineEvent)); serr == nil {
			t.Errorf("%s was written despite the conflict refusal (half-wired state)", e.clineEvent)
		}
	}
	// The user script itself is untouched.
	//
	// 用户脚本本身未被触碰。
	data, err := os.ReadFile(user)
	if err != nil || !strings.Contains(string(data), "my own lint hook") {
		t.Errorf("user script was modified by the refused Translate: %v", err)
	}

	// A stale FORGE wrapper (marker present) is refreshable.
	//
	// 陈旧的 forge wrapper（带标记）可刷新。
	if err := os.WriteFile(user, []byte("#!/bin/sh\n"+clineWrapperMarker+" old\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := (&ClineTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate over a stale forge wrapper failed: %v", err)
	}
}

// TestStripClineHooks covers the strip roundtrip: Translate then Strip removes every
// forge wrapper while preserving user scripts; the emptied dir is removed; a second
// strip and a missing dir are clean no-ops.
//
// TestStripClineHooks 覆盖 strip 往返：Translate 后 Strip 移除全部 forge wrapper、
// 保留用户脚本；清空的目录被移除；二次 strip 与目录缺失是干净 no-op。
func TestStripClineHooks(t *testing.T) {
	home := isolateHome(t)
	dir := clineHooksPathUnder(home)

	// Missing dir → clean no-op.
	//
	// 目录缺失 → 干净 no-op。
	changed, err := StripClineHooks()
	if err != nil || changed {
		t.Fatalf("missing dir: changed=%v err=%v, want false/nil", changed, err)
	}

	if err := (&ClineTranslator{}).Translate(t.TempDir(), testInput()); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	// A user script in a slot forge does not wire must survive.
	//
	// 用户脚本占着 forge 未接线的槽位，必须幸存。
	user := filepath.Join(dir, "TaskResume")
	if err := os.WriteFile(user, []byte("#!/bin/sh\n# user resume hook\nexit 0\n"), 0755); err != nil {
		t.Fatal(err)
	}

	changed, err = StripClineHooks()
	if err != nil || !changed {
		t.Fatalf("strip after Translate: changed=%v err=%v, want true/nil", changed, err)
	}
	for _, e := range clineEventMappings {
		if _, serr := os.Stat(filepath.Join(dir, e.clineEvent)); serr == nil {
			t.Errorf("forge wrapper %s survived strip", e.clineEvent)
		}
	}
	data, err := os.ReadFile(user)
	if err != nil || !strings.Contains(string(data), "user resume hook") {
		t.Errorf("user script lost after strip: %v", err)
	}
	// The dir stays while a user script populates it.
	//
	// 用户脚本还占着目录时目录保留。
	if _, serr := os.Stat(dir); serr != nil {
		t.Errorf("dir removed while a user script still populates it: %v", serr)
	}

	// Second strip → no-op.
	//
	// 二次 strip → no-op。
	changed, err = StripClineHooks()
	if err != nil || changed {
		t.Fatalf("second strip: changed=%v err=%v, want false/nil", changed, err)
	}
}
