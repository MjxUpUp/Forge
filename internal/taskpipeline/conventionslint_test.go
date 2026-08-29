package taskpipeline

// conventionslint_test.go —— task-verify 的 conventions-lint advisory 守卫
// （conventionslint.go）：可判定时 跑过/未跑 的裁定正确；不可判定（无档案/
// 无 lint 命令/无遥测/env 逃生）时既不 fire 也不落 checklog；签名匹配不吃
// wrapper 假阳性（go test 满足不了 go vet）。

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/conventions"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/toolusage"
)

// lintFixture 建 git 仓库 + 任务状态 + 带 lint 命令的 conventions 档案，并把
// 给定 Bash 命令按任务 ref 记进 toollog——按**真实** toollog 形态：tool-track
// 记的是原始 tool_input JSON blob（{"command": ...}），非裸命令文本。
// rawInputs 原样记录预构造串（用于 description-only blob）。返回 (root, state)。
func lintFixture(t *testing.T, lintCmd string, bashCommands []string, rawInputs ...string) (string, *TaskState) {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "t@t.com")
	runGit(t, dir, "config", "user.name", "t")

	state := &TaskState{TaskRef: "lint-check", Branch: "feat/x"}
	if lintCmd != "" {
		p := &conventions.Profile{Version: conventions.ProfileVersion, Stack: "go", LintCmd: lintCmd}
		if err := conventions.SaveProfile(forgedata.DataDirFor(dir), p); err != nil {
			t.Fatal(err)
		}
	}
	inputs := make([]string, 0, len(bashCommands)+len(rawInputs))
	for _, cmd := range bashCommands {
		b, err := json.Marshal(map[string]string{"command": cmd})
		if err != nil {
			t.Fatal(err)
		}
		inputs = append(inputs, string(b))
	}
	inputs = append(inputs, rawInputs...)
	now := time.Now()
	for i, input := range inputs {
		call := toolusage.ToolCall{
			ToolName:  "Bash",
			ToolInput: input,
			TaskRef:   "lint-check",
			Timestamp: now.Add(time.Duration(i) * time.Second),
		}
		if err := toolusage.Record(dir, &call); err != nil {
			t.Fatalf("toolusage.Record: %v", err)
		}
	}
	return dir, state
}

// TestCheckConventionsLint_FiresWhenNotRun pins the core verdict: profile declares `go vet`, the task ran only `go test` (a wrapper-level lookalike — must NOT satisfy the signature) → applicable + not ran; the audit row lands with Passed=false.
//
// TestCheckConventionsLint_FiresWhenNotRun 钉住核心裁定：档案声明 `go vet`、
// 任务只跑了 `go test`（wrapper 层的形近命令——不得满足签名）→ 可判定 +
// 未跑；审计行以 Passed=false 落盘。
func TestCheckConventionsLint_FiresWhenNotRun(t *testing.T) {
	dir, state := lintFixture(t, "go vet ./...", []string{"go test ./internal/x/...", "git status"})

	out := CheckConventionsLint(dir, state)
	if !out.Applicable || out.Ran {
		t.Fatalf("outcome = %+v, want applicable+not-ran (go test must not satisfy go vet)", out)
	}
	if out.Signature != "vet" {
		t.Fatalf("signature = %q, want vet (wrapper `go` dropped)", out.Signature)
	}
	recordConventionsLintAudit(dir, state, out)
	entries := loadChecklogEntries(t, dir, checklog.CheckConventionsLint)
	if len(entries) != 1 || entries[0].Passed {
		t.Fatalf("audit entries = %+v, want 1 row Passed=false", entries)
	}
	if !strings.Contains(entries[0].Detail, "go vet ./...") {
		t.Fatalf("detail should name the lint command: %q", entries[0].Detail)
	}
}

// TestCheckConventionsLint_SatisfiedWhenRun pins the satisfied path: the declared lint command (with wrappers/flags) appears in the task's Bash history → applicable + ran, audit row Passed=true, no nudge.
//
// TestCheckConventionsLint_SatisfiedWhenRun 钉住满足路径：声明的 lint 命令
// （带 wrapper/flag 形态）出现在任务 Bash 历史 → 可判定 + 跑过，审计行
// Passed=true、无提醒。
func TestCheckConventionsLint_SatisfiedWhenRun(t *testing.T) {
	dir, state := lintFixture(t, "npm run lint", []string{"cd web && npm run lint -- --fix", "git diff"})

	out := CheckConventionsLint(dir, state)
	if !out.Applicable || !out.Ran {
		t.Fatalf("outcome = %+v, want applicable+ran (npm run lint seen)", out)
	}
	recordConventionsLintAudit(dir, state, out)
	entries := loadChecklogEntries(t, dir, checklog.CheckConventionsLint)
	if len(entries) != 1 || !entries[0].Passed {
		t.Fatalf("audit entries = %+v, want 1 row Passed=true", entries)
	}
}

// TestCheckConventionsLint_UndecidableStaysSilent pins the silence contract: no profile / no lint command / no toollog rows / env escape → Applicable false AND no checklog row (an "not applicable" row per unadopted project is pure noise).
//
// TestCheckConventionsLint_UndecidableStaysSilent 钉住静默契约：无档案 /
// 无 lint 命令 / toollog 无记录 / env 逃生 → Applicable=false 且不落
// checklog（为每个未采纳项目写「不适用」行是纯噪声）。
func TestCheckConventionsLint_UndecidableStaysSilent(t *testing.T) {
	cases := []struct {
		name    string
		lintCmd string
		bashes  []string
	}{
		{name: "无档案无遥测", lintCmd: "", bashes: nil},
		{name: "有档案无遥测", lintCmd: "go vet ./...", bashes: nil},
		{name: "无档案有遥测", lintCmd: "", bashes: []string{"go test ./..."}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir, state := lintFixture(t, tc.lintCmd, tc.bashes)
			if out := CheckConventionsLint(dir, state); out.Applicable {
				t.Fatalf("%s: outcome = %+v, want not-applicable", tc.name, out)
			}
			recordConventionsLintAudit(dir, state, CheckConventionsLint(dir, state))
			if entries := loadChecklogEntries(t, dir, checklog.CheckConventionsLint); len(entries) != 0 {
				t.Fatalf("%s: undecidable must not record, got %d rows", tc.name, len(entries))
			}
		})
	}

	t.Run("env 逃生", func(t *testing.T) {
		t.Setenv("FORGE_CONVENTIONS_LINT", "disable")
		dir, state := lintFixture(t, "go vet ./...", []string{"go test ./..."})
		if out := CheckConventionsLint(dir, state); out.Applicable {
			t.Fatalf("env escape must make the check not-applicable, got %+v", out)
		}
	})
}

// TestCheckConventionsLint_MatchingPrecision pins the lookalike kills (adversarial-review finding #2): the match runs on the command FIELD, not the raw JSON blob (a description saying "vet the code" must not satisfy `go vet`), and at word boundaries (`git log --format=%h` must not satisfy a `format` signature; the interior of `golangci-lint` must not satisfy a bare `lint` signature).
//
// TestCheckConventionsLint_MatchingPrecision 钉住形近击杀（对抗审查发现 #2）：
// 匹配跑在 command **字段**上而非原始 JSON blob（description 里的
// "vet the code" 满足不了 `go vet`），且按词边界（`git log --format=%h`
// 满足不了 `format` 签名；`golangci-lint` 内部满足不了裸 `lint` 签名）。
// 审计行绝不得陈述未发生的运行。
func TestCheckConventionsLint_MatchingPrecision(t *testing.T) {
	t.Run("description 假阳性被杀", func(t *testing.T) {
		// lint 声明 dotnet format；Bash 记录的 description 里有 "vet"，
		// command 是 git log —— 两者都不得满足签名。
		raw := `{"command":"git log --format=%h -5","description":"vet the code after format changes"}`
		dir, state := lintFixture(t, "dotnet format --verify-no-changes", nil, raw)
		out := CheckConventionsLint(dir, state)
		if !out.Applicable || out.Ran {
			t.Fatalf("outcome = %+v, want applicable+not-ran (description/blob lookalikes must not satisfy)", out)
		}
		if out.Signature != "format" {
			t.Fatalf("signature = %q, want format", out.Signature)
		}
	})
	t.Run("连字符词边界", func(t *testing.T) {
		// 声明 npm run lint（签名 lint）；实际跑的是 golangci-lint —— 连字符
		// 算词字节，`golangci-lint` 内部的 lint 不满足裸 `lint` 签名。advisory
		// 提醒是正确方向：那是另一个 lint 命令，不是声明的那条。
		dir, state := lintFixture(t, "npm run lint", []string{"golangci-lint run ./..."})
		if out := CheckConventionsLint(dir, state); !out.Applicable || out.Ran {
			t.Fatalf("outcome = %+v, want applicable+not-ran (hyphen interior must not satisfy bare token)", out)
		}
	})
	t.Run("真实命中仍工作", func(t *testing.T) {
		dir, state := lintFixture(t, "golangci-lint run", []string{"golangci-lint run ./internal/..."})
		if out := CheckConventionsLint(dir, state); !out.Applicable || !out.Ran {
			t.Fatalf("outcome = %+v, want applicable+ran (full token at boundaries)", out)
		}
	})
}

// loadChecklogEntries 读 root 的 checklog 并按 check 名过滤。
func loadChecklogEntries(t *testing.T, root string, check checklog.CheckName) []checklog.Entry {
	t.Helper()
	all, err := checklog.LoadAllAll(root)
	if err != nil {
		t.Fatalf("checklog LoadAllAll: %v", err)
	}
	var out []checklog.Entry
	for _, e := range all {
		if e.Check == check {
			out = append(out, e)
		}
	}
	return out
}
