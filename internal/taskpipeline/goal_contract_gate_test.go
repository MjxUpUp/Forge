package taskpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

const completeGateGoal = `# 目标

## Objective

验证 goal 契约在 implement 门禁上的接线。

## Why

只测 checkGoalContract 不能证明调用点还在。

## Constraints

不引入新依赖。

## Out of Scope

不做 UI。

## Check

acceptance #3。
`

func writeGateGoal(t *testing.T, dir string) (string, string) {
	t.Helper()
	rel := filepath.ToSlash(filepath.Join("spec", "changes", "demo", "goal.md"))
	abs := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(completeGateGoal), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(completeGateGoal))
	return rel, hex.EncodeToString(sum[:])
}

// TestExecuteTaskGate_GoalContractWiring 是 Code Review P2-2 的集成测试：走
// ExecuteTaskGate 的调用点（而不是直调 checkGoalContract），删掉 executor.go 里的
// 接线、或把它挪到会提前 return 的位置，这个用例必须红。
func TestExecuteTaskGate_GoalContractWiring(t *testing.T) {
	t.Setenv("FORGE_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	initRepoWithMaster(t, dir)
	writeCommitSource(t, dir, map[string]string{
		"foo.go": "package main\n\nfunc Foo() int { return 1 }\n",
	}, "add foo")

	rel, sum := writeGateGoal(t, dir)
	// 未提交改动 → hasCodeChanges 为真（工作树 diff），门禁才会走到 goal 校验。
	if err := os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package main\n\nfunc Foo() int { return 2 }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := &TaskState{TaskRef: "goal-wiring", Branch: "feat/goal", GoalContract: &tasktypes.GoalContract{Path: rel, SHA256: sum}}

	res, err := ExecuteTaskGate(dir, GateImplement, state)
	if err != nil {
		t.Fatalf("ExecuteTaskGate: %v", err)
	}
	if !res.Passed {
		t.Fatalf("契约未变时 implement 应通过，got %+v", res)
	}

	// 改 goal → 必须 BLOCKED，且 checklog 里有失败行（P3-3）。
	if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(completeGateGoal+"\n* drift\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = ExecuteTaskGate(dir, GateImplement, state)
	if err != nil {
		t.Fatalf("ExecuteTaskGate(drift): %v", err)
	}
	if res.Passed || !strings.Contains(res.Message, "goal 契约在任务启动后被修改") {
		t.Fatalf("goal 漂移应 BLOCKED，got %+v", res)
	}
	entries, lerr := checklog.LoadAll(dir)
	if lerr != nil {
		t.Fatal(lerr)
	}
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckGoalContract && !e.Passed && e.Checked {
			found = true
		}
	}
	if !found {
		t.Fatalf("BLOCKED 也应落一条失败 checklog（P3-3），entries=%+v", entries)
	}

	// generic kind 不走门禁：不应因为契约漂移而阻断。
	generic := *state
	generic.Kind = "generic"
	res, err = ExecuteTaskGate(dir, GateImplement, &generic)
	if err != nil {
		t.Fatalf("ExecuteTaskGate(generic): %v", err)
	}
	if !res.Passed {
		t.Fatalf("generic 任务不走 implement 门禁，got %+v", res)
	}
}
