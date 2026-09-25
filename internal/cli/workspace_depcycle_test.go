package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
	"github.com/spf13/cobra"
)

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

// TestDetectWorkspaceDepCycles covers the pure dep-cycle graph core.
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

// TestDepCycleFindings pins the finding shape: advisory dep-cycle kind, the full key:ref ring closed back to its start, and attribution to every workspace holding a ring member.
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

// TestRunWorkspaceDoctor_DepCycle wires the whole doctor path.
//
// TestRunWorkspaceDoctor_DepCycle 打通 doctor 全链路：两成员 workspace 的 task
// 构成跨仓环时，文本与 --json 输出都必须带 dep-cycle finding（Doctor 自身的
// drift finding——此处是未注册成员——并存，全部 advisory）。
func TestRunWorkspaceDoctor_DepCycle(t *testing.T) {
	_, _, home := depRefFixture(t)
	writeDepRefWorkspace(t, `aa0000000001`, `bb0000000002`)
	// A 等 bb0000000002:B，B 等 aa0000000001:A——一个跨仓环。
	writeForeignTaskWithDeps(t, home, `aa0000000001`, `A`, []string{`bb0000000002:B`})
	writeForeignTaskWithDeps(t, home, `bb0000000002`, `B`, []string{`aa0000000001:A`})

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

	if text := run(false); !strings.Contains(text, `dep-cycle`) || !strings.Contains(text, `aa0000000001:A → bb0000000002:B → aa0000000001:A`) {
		t.Errorf(`文本输出应含 dep-cycle 与环序列, got %q`, text)
	}
	if js := run(true); !strings.Contains(js, `"dep-cycle"`) {
		t.Errorf(`--json 输出应携带 dep-cycle finding, got %q`, js)
	}
}

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

// depRefFixture/writeDepRefWorkspace 是 task_depref_test.go（已迁 internal/clitask）
// 同名夹具的姊妹副本（测试助手无法跨包共享，注释互指防漂移）——workspace
// 依赖环检测的清单 fixture 与 clitask 侧同一布局。
func depRefFixture(t *testing.T) (root, ownKey, home string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	root = t.TempDir()
	ownKey = forgedata.PathKey(root)
	return root, ownKey, home
}

func writeDepRefWorkspace(t *testing.T, ownKey string, otherKeys ...string) {
	t.Helper()
	f := &workspace.File{}
	if err := f.Create(`fleet`); err != nil {
		t.Fatal(err)
	}
	if err := f.AddRepo(`fleet`, workspace.RepoRef{Key: ownKey}); err != nil {
		t.Fatal(err)
	}
	for _, k := range otherKeys {
		if err := f.AddRepo(`fleet`, workspace.RepoRef{Key: k}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
}
