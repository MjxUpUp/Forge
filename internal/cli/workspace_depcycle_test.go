package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
	"github.com/spf13/cobra"
)

// depCycleTasks is a compact fixture builder: key → (taskRef → its DependsOn).
//
// depCycleTasks 是紧凑 fixture 构造器：key →（taskRef → 其 DependsOn）。
func depCycleTasks(spec map[string]map[string][]string) map[string][]*taskpipeline.TaskState {
	out := map[string][]*taskpipeline.TaskState{}
	for key, tasks := range spec {
		for ref, deps := range tasks {
			out[key] = append(out[key], &taskpipeline.TaskState{TaskRef: ref, DependsOn: deps})
		}
	}
	return out
}

// cycleStrings renders each cycle's node IDs as key:ref labels for assertion.
//
// cycleStrings 把每个环的节点 ID 渲染成 key:ref 标签供断言。
func cycleStrings(cycles [][]string) []string {
	var out []string
	for _, cyc := range cycles {
		var labels []string
		for _, id := range cyc {
			labels = append(labels, depNodeLabel(id))
		}
		out = append(out, strings.Join(labels, `→`))
	}
	return out
}

// TestDetectWorkspaceDepCycles covers the pure graph core: same-repo and
// cross-repo rings (2- and 3-node), a self-loop from a hand-edited state, two
// disjoint rings both reported, and the no-cycle shapes (diamond, foreign-key
// dead end, missing same-repo target) staying silent.
//
// TestDetectWorkspaceDepCycles 覆盖纯图核心：本仓环与跨仓环（2 节点与 3 节点）、
// 手改 state 造成的自环、两个不相交的环都报出，以及无环形态（菱形、非成员
// key 死端、本仓缺失目标）保持静默。
func TestDetectWorkspaceDepCycles(t *testing.T) {
	cases := []struct {
		name string
		spec map[string]map[string][]string
		want []string
	}{
		{`本仓环`, map[string]map[string][]string{
			`ka`: {`A`: {`B`}, `B`: {`A`}},
		}, []string{`ka:A→ka:B`}},
		{`跨仓 2 环`, map[string]map[string][]string{
			`ka`: {`A`: {`kb:B`}},
			`kb`: {`B`: {`ka:A`}},
		}, []string{`ka:A→kb:B`}},
		{`跨三仓环`, map[string]map[string][]string{
			`ka`: {`A`: {`kb:B`}},
			`kb`: {`B`: {`kc:C`}},
			`kc`: {`C`: {`ka:A`}},
		}, []string{`ka:A→kb:B→kc:C`}},
		{`手改 state 的自环`, map[string]map[string][]string{
			`ka`: {`A`: {`A`}},
		}, []string{`ka:A`}},
		{`两个不相交的环`, map[string]map[string][]string{
			`ka`: {`A`: {`B`}, `B`: {`A`}},
			`kb`: {`X`: {`Y`}, `Y`: {`X`}},
		}, []string{`ka:A→ka:B`, `kb:X→kb:Y`}},
		{`菱形无环`, map[string]map[string][]string{
			`ka`: {`A`: {`B`, `C`}, `B`: {`D`}, `C`: {`D`}, `D`: {}},
		}, nil},
		{`非成员 key 目标是死端`, map[string]map[string][]string{
			`ka`: {`A`: {`foreign:X`}},
		}, nil},
		{`本仓缺失目标无环`, map[string]map[string][]string{
			`ka`: {`A`: {`ghost`}},
		}, nil},
		{`跨仓依赖本仓但本仓不回指`, map[string]map[string][]string{
			`ka`: {`A`: {}},
			`kb`: {`B`: {`ka:A`}},
		}, nil},
	}
	for _, c := range cases {
		got := cycleStrings(detectWorkspaceDepCycles(depCycleTasks(c.spec)))
		if strings.Join(got, `,`) != strings.Join(c.want, `,`) {
			t.Errorf("%s: cycles = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestDepCycleFindings pins the finding shape: advisory dep-cycle kind, the
// full key:ref ring closed back to its start, and attribution to every
// workspace holding a ring member.
//
// TestDepCycleFindings 钉住 finding 形态：advisory 的 dep-cycle kind、完整
// key:ref 环闭合回起点、归属到所有含环上成员的 workspace。
func TestDepCycleFindings(t *testing.T) {
	f := &workspace.File{Workspaces: []workspace.Workspace{
		{Name: `fleet`, Repos: []workspace.RepoRef{{Key: `ka`}, {Key: `kb`}}},
		{Name: `solo`, Repos: []workspace.RepoRef{{Key: `kz`}}},
	}}
	tasks := depCycleTasks(map[string]map[string][]string{
		`ka`: {`A`: {`kb:B`}},
		`kb`: {`B`: {`ka:A`}},
	})
	findings := depCycleFindings(f, tasks)
	if len(findings) != 1 {
		t.Fatalf(`应恰好一个环 finding, got %+v`, findings)
	}
	fd := findings[0]
	if fd.Kind != workspace.DriftDepCycle {
		t.Errorf(`Kind = %q, want %q`, fd.Kind, workspace.DriftDepCycle)
	}
	if fd.Workspace != `fleet` {
		t.Errorf(`归属应为 fleet, got %q`, fd.Workspace)
	}
	if !strings.Contains(fd.Detail, `ka:A → kb:B → ka:A`) {
		t.Errorf(`Detail 应含闭合的 key:ref 环序列, got %q`, fd.Detail)
	}
}

// TestRunWorkspaceDoctor_DepCycle wires the whole doctor path: a two-member
// workspace whose tasks form a cross-repo ring must surface a dep-cycle
// finding in both text and --json output (the drift findings from Doctor
// itself — unregistered members here — coexist, advisory-only).
//
// TestRunWorkspaceDoctor_DepCycle 打通 doctor 全链路：两成员 workspace 的 task
// 构成跨仓环时，文本与 --json 输出都必须带 dep-cycle finding（Doctor 自身的
// drift finding——此处是未注册成员——并存，全部 advisory）。
func TestRunWorkspaceDoctor_DepCycle(t *testing.T) {
	_, _, home := depRefFixture(t)
	writeDepRefWorkspace(t, `ka-self`, `kb-peer`)
	// A waits on kb-peer:B, B waits on ka-self:A — a cross-repo ring.
	//
	// A 等 kb-peer:B，B 等 ka-self:A——一个跨仓环。
	writeForeignTaskWithDeps(t, home, `ka-self`, `A`, []string{`kb-peer:B`})
	writeForeignTaskWithDeps(t, home, `kb-peer`, `B`, []string{`ka-self:A`})

	run := func(jsonFlag bool) string {
		cmd := &cobra.Command{}
		cmd.Flags().Bool(`json`, false, ``)
		if jsonFlag {
			if err := cmd.Flags().Set(`json`, `true`); err != nil {
				t.Fatal(err)
			}
		}
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&bytes.Buffer{})
		if err := runWorkspaceDoctor(cmd, nil); err != nil {
			t.Fatalf(`runWorkspaceDoctor: %v`, err)
		}
		return out.String()
	}

	if text := run(false); !strings.Contains(text, `dep-cycle`) || !strings.Contains(text, `ka-self:A → kb-peer:B → ka-self:A`) {
		t.Errorf(`文本输出应含 dep-cycle 与环序列, got %q`, text)
	}
	if js := run(true); !strings.Contains(js, `"dep-cycle"`) {
		t.Errorf(`--json 输出应携带 dep-cycle finding, got %q`, js)
	}
}

// writeForeignTaskWithDeps is writeForeignTask + DependsOn edges.
//
// writeForeignTaskWithDeps 是 writeForeignTask 加 DependsOn 边。
func writeForeignTaskWithDeps(t *testing.T, home, key, ref string, deps []string) {
	t.Helper()
	dir := filepath.Join(home, `projects`, key, `tasks`)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(&taskpipeline.TaskState{TaskRef: ref, DependsOn: deps})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref+`.json`), data, 0644); err != nil {
		t.Fatal(err)
	}
}
