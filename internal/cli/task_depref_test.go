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
)

// TestUnknownDepKeys covers the pure membership core: bare refs never
// participate; keys are valid iff ANY workspace containing ownKey lists them
// (overlapping memberships union); offenders are deduped.
//
// TestUnknownDepKeys 覆盖纯成员资格核心：裸 ref 不参与；key 合法当且仅当任一
// 包含 ownKey 的 workspace 列其为成员（重叠成员资格取并集）；越界 key 去重。
func TestUnknownDepKeys(t *testing.T) {
	ws := func(name string, keys ...string) workspace.Workspace {
		w := workspace.Workspace{Name: name}
		for _, k := range keys {
			w.Repos = append(w.Repos, workspace.RepoRef{Key: k})
		}
		return w
	}
	cases := []struct {
		name   string
		file   *workspace.File
		ownKey string
		deps   []string
		want   []string
	}{
		{`无 workspace 含本仓 → 所有 key 前缀越界`, &workspace.File{Workspaces: []workspace.Workspace{ws(`fleet`, `a`, `b`)}}, `me`,
			[]string{`a:t1`}, []string{`a`}},
		{`成员 key 合法 + 裸 ref 忽略`, &workspace.File{Workspaces: []workspace.Workspace{ws(`fleet`, `me`, `ee0000000005`)}}, `me`,
			[]string{`local`, `ee0000000005:t1`, `me:t2`}, nil},
		{`重叠 workspace 并集`, &workspace.File{Workspaces: []workspace.Workspace{ws(`x`, `me`, `k1`), ws(`y`, `me`, `k2`)}}, `me`,
			[]string{`k1:a`, `k2:b`}, nil},
		{`越界 key 去重`, &workspace.File{Workspaces: []workspace.Workspace{ws(`fleet`, `me`, `ee0000000005`)}}, `me`,
			[]string{`bad:1`, `bad:2`, `ee0000000005:ok`}, []string{`bad`}},
	}
	for _, c := range cases {
		got := unknownDepKeys(c.file, c.ownKey, c.deps)
		if strings.Join(got, `,`) != strings.Join(c.want, `,`) {
			t.Errorf("%s: unknownDepKeys = %v, want %v", c.name, got, c.want)
		}
	}
}

// depRefFixture isolates FORGE_DATA_HOME and returns (root, ownKey, home) with
// root a non-git temp dir (ownKey = PathKey, matching DataDirFor's fallback).
//
// depRefFixture 隔离 FORGE_DATA_HOME，返回 (root, ownKey, home)；root 为非 git
// 临时目录（ownKey = PathKey，与 DataDirFor 的回落一致）。
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

func writeForeignTask(t *testing.T, home, key, ref string, delivered bool) {
	t.Helper()
	dir := filepath.Join(home, `projects`, key, `tasks`)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	s := &taskpipeline.TaskState{TaskRef: ref}
	if delivered {
		s.Assignment = &taskpipeline.Assignment{Status: taskpipeline.AssignDelivered}
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ref+`.json`), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestValidateDependsOnRefs covers the write-side wiring: same-repo-only deps
// short-circuit before any manifest read; a foreign key prefix is refused with
// the workspace-add hint; an unreadable manifest fails OPEN (advisory, dep
// allowed); a missing cross-repo target is tolerated with an advisory
// (forward references stay legal, matching same-repo behavior); an existing
// delivered target is silent; <ownkey>:<selfref> is refused as a self-dep.
//
// TestValidateDependsOnRefs 覆盖写入侧接线：纯本仓依赖在读清单前就短路；越界
// key 前缀带 workspace-add 提示拒绝；清单不可读 fail-OPEN（advisory 放行）；
// 跨仓目标缺失容忍但 advisory（前向引用合法，与本仓行为一致）；已交付目标
// 静默；<本仓key>:<本ref> 按自依赖拒绝。
func TestValidateDependsOnRefs(t *testing.T) {
	t.Run(`纯本仓依赖不碰清单`, func(t *testing.T) {
		root, _, _ := depRefFixture(t) // 不写任何 manifest
		var stderr bytes.Buffer
		if err := validateDependsOnRefs(root, `self`, []string{`a`, `b`}, &stderr); err != nil {
			t.Fatalf(`纯本仓依赖应直接放行, got %v`, err)
		}
		if stderr.Len() != 0 {
			t.Errorf(`纯本仓依赖不应有 advisory, got %q`, stderr.String())
		}
	})
	t.Run(`越界 key 前缀拒绝`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `ee0000000005`)
		var stderr bytes.Buffer
		err := validateDependsOnRefs(root, `self`, []string{`stranger:t1`}, &stderr)
		if err == nil || !strings.Contains(err.Error(), `forge workspace add`) {
			t.Fatalf(`越界 key 应拒绝并提示 forge workspace add, got %v`, err)
		}
	})
	t.Run(`清单不可读 fail-open`, func(t *testing.T) {
		root, _, home := depRefFixture(t)
		if err := os.WriteFile(filepath.Join(home, `workspaces.json`), []byte(`{broken`), 0644); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		if err := validateDependsOnRefs(root, `self`, []string{`anykey:t1`}, &stderr); err != nil {
			t.Fatalf(`清单损坏应 fail-open 放行, got %v`, err)
		}
		if !strings.Contains(stderr.String(), `fail-open`) {
			t.Errorf(`应有 fail-open advisory, got %q`, stderr.String())
		}
	})
	t.Run(`跨仓目标缺失：容忍 + advisory`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `ee0000000005`)
		var stderr bytes.Buffer
		if err := validateDependsOnRefs(root, `self`, []string{`ee0000000005:ghost`}, &stderr); err != nil {
			t.Fatalf(`缺失目标应容忍（前向引用合法）, got %v`, err)
		}
		if !strings.Contains(stderr.String(), `pending`) {
			t.Errorf(`缺失目标应有 advisory 提醒, got %q`, stderr.String())
		}
	})
	t.Run(`跨仓目标存在：静默`, func(t *testing.T) {
		root, ownKey, home := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `ee0000000005`)
		writeForeignTask(t, home, `ee0000000005`, `b-done`, true)
		var stderr bytes.Buffer
		if err := validateDependsOnRefs(root, `self`, []string{`ee0000000005:b-done`}, &stderr); err != nil {
			t.Fatalf(`已交付目标应放行, got %v`, err)
		}
		if stderr.Len() != 0 {
			t.Errorf(`目标存在不应有 advisory, got %q`, stderr.String())
		}
	})
	t.Run(`本仓 key 自引用拒绝`, func(t *testing.T) {
		root, ownKey, _ := depRefFixture(t)
		writeDepRefWorkspace(t, ownKey, `ee0000000005`)
		var stderr bytes.Buffer
		err := validateDependsOnRefs(root, `self`, []string{ownKey + `:self`}, &stderr)
		if err == nil || !strings.Contains(err.Error(), `不能依赖自身`) {
			t.Fatalf(`<本仓key>:<本ref> 应按自引用拒绝, got %v`, err)
		}
	})
}
