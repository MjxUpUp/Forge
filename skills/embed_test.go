package skills

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestExtractTo verifies the embedded skill library extracts fully to a real directory: CONVENTIONS.md + at least one skill's SKILL.md.
// This is the core of the embed fallback (when --canonical is absent), the only distribution path when link mode is unavailable.
//
// TestExtractTo 验证内置 skill 库能完整解压到真实目录：CONVENTIONS.md + 至少一个 skill 的 SKILL.md。
// 这是 embed fallback（无 --canonical 时）的核心，link 模式不可用时的唯一分发路径。
func TestExtractTo(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "CONVENTIONS.md")); err != nil {
		t.Fatalf("CONVENTIONS.md 未解压: %v", err)
	}
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("ReadDir FS: %v", err)
	}
	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "SKILL.md")); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("无 skill 的 SKILL.md 被解压")
	}
}

// TestExtractTo_NoGoFiles guards: embed_test.go (no //go:embed directive) gets embedded by `*`,
// but it is an in-package test artifact and must never enter the distribution cache — otherwise link/copy would carry it into the user target directory.
// ExtractTo must explicitly skip .go files.
//
// TestExtractTo_NoGoFiles 守护：embed_test.go（无 //go:embed 指令）会被 `*` 嵌入，
// 但它是包内测试产物，绝不能进入分发缓存——否则 link/copy 会把它带进用户目标目录。
// ExtractTo 必须显式跳过 .go。
func TestExtractTo_NoGoFiles(t *testing.T) {
	dir := t.TempDir()
	if err := ExtractTo(dir); err != nil {
		t.Fatalf("ExtractTo: %v", err)
	}
	var leaked []string
	werr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(p) == ".go" {
			leaked = append(leaked, p)
		}
		return nil
	})
	if werr != nil {
		t.Fatalf("walk extract dir: %v", werr)
	}
	if len(leaked) > 0 {
		t.Fatalf(".go files leaked into extract dir (test artifacts pollute cache): %v", leaked)
	}
}

// TestNoSKILLMDBindsTaskActiveNoReview 守护:task_active_no_review condition 判定的是
// !state.ReviewPassed（code-review 语义），而 code-review-gate 被 skilltrigger.DeniedSkills
// 排除（由 review-stop hook 专属驱动）——该 condition 当前无合法消费者。任何 SKILL.md 绑定
// 它都是语义错配：2026-08-05 verification-driver 曾错配致 Stop 误注入（主 agent 派审查子 agent
// 等待结果时触发 Stop，被注入端到端验证提醒）。未来若解禁 code-review-gate 或新增合法消费者，
// 同步改本测试并论证语义对口。
//
// Guards: no SKILL.md may bind task_active_no_review. The condition has no legitimate
// consumer today (code-review-gate is denied, driven exclusively by the review-stop hook),
// so binding it is a code-review→non-code-review category mismatch.
func TestNoSKILLMDBindsTaskActiveNoReview(t *testing.T) {
	const orphan = `task_active_no_review`
	var hit []string
	err := fs.WalkDir(FS, `.`, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Base(path) != `SKILL.md` {
			return nil
		}
		data, rerr := fs.ReadFile(FS, path)
		if rerr != nil {
			return rerr
		}
		if bytes.Contains(data, []byte(orphan)) {
			hit = append(hit, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf(`walk embed FS: %v`, err)
	}
	if len(hit) > 0 {
		t.Fatalf(`task_active_no_review 是 code-review 语义 condition（判 !state.ReviewPassed），
code-review-gate 被 DeniedSkills 排除后无合法消费者。以下 SKILL.md 错配绑定了它：%v`, hit)
	}
}
