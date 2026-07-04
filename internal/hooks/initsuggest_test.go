package hooks

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initsuggest_test.go — InitSuggestHook 的真行为守卫（不止 containsString）。
// 跑真实脚本过各分支，断言 emitted 输出 + side effect（标记文件 / AUTO_INIT 的
// forge init 调用）。手法同 skillscan_test：bash function stub 覆盖 forge 命令，
// HOME/FORGE_CWD/FORGE_CWD_TAG 由测试控制，隔离真实 cwd 与 ~/.forge 标记。
//
// 所有 string literal 用 raw string（反引号）规避 Windows 输入引号腐蚀。

// runInitSuggestHook 跑真实 InitSuggestHook 脚本，返回 stdout+stderr。
// forge() stub 覆盖 forge（AUTO_INIT 分支不真跑 forge init）：成功路径 touch flag
// 文件供断言；FORGE_FORGE_FAIL=1 时 return 1 不 touch（模拟 init 失败回显）。
// init-suggest 设计 exit 0，非零=脚本 bug。
//
// 已知盲区（R6）：stub 无法模拟真实 forge init 的 partial-state（失败前已建 .forge
// 致下次会话 [ -d .forge ] 静默）——embed.go 注释承诺回显 stderr 即为该场景设计，
// 但本测试只覆盖"失败回显"这一层，partial-state 静默逻辑未覆盖（需真跑 forge init）。
func runInitSuggestHook(t *testing.T, cwd, tag, home, initFlag string, extraEnv ...string) string {
	t.Helper()
	stub := `#!/bin/bash
forge() {
  if [ -n "$FORGE_FORGE_FAIL" ]; then return 1; fi
  touch "$FORGE_INIT_FLAG" 2>/dev/null
  return 0
}
`
	script := stub + InitSuggestHook
	tmp, err := os.CreateTemp("", "init-suggest-*.sh")
	if err != nil {
		t.Fatalf("createtemp: %v", err)
	}
	if _, err := tmp.WriteString(script); err != nil {
		t.Fatalf("write script: %v", err)
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	env := []string{
		`HOME=` + home,
		`PATH=` + os.Getenv(`PATH`),
		`FORGE_CWD=` + cwd,
		`FORGE_CWD_TAG=` + tag,
		`FORGE_INIT_FLAG=` + initFlag,
	}
	env = append(env, extraEnv...)
	cmd := exec.Command("bash", tmp.Name())
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("InitSuggestHook exited non-zero (script bug): err=%v, out=%s", err, out)
	}
	return string(out)
}

// mkGitProj 构造临时 git 项目（有 .git）；withForge=true 额外建 .forge/。
func mkGitProj(t *testing.T, withForge bool) string {
	t.Helper()
	d := t.TempDir()
	if err := os.MkdirAll(filepath.Join(d, `.git`), 0755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	if withForge {
		if err := os.MkdirAll(filepath.Join(d, `.forge`), 0755); err != nil {
			t.Fatalf("mkdir .forge: %v", err)
		}
	}
	return d
}

// writeSuggestMarker 在 home 的标记目录写 tag 标记（模拟 hook 已提示/用户已拒绝）。
func writeSuggestMarker(t *testing.T, home, tag, value string) {
	t.Helper()
	dir := filepath.Join(home, `.forge`, `.init-suggested`)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir marker dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, tag), []byte(value), 0644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
}

// TestInitSuggestHook_Branches 跑真实脚本过 6 个分支，断言输出 + AUTO_INIT side effect。
// 若未来编辑破坏 git-root 查找 / 标记静默 / AUTO_INIT 分支，这些 case 失败。
func TestInitSuggestHook_Branches(t *testing.T) {
	cases := []struct {
		name      string
		cwdFn     func(t *testing.T) string
		marker    string // "", "suggested", "declined"
		autoInit  bool
		failForge bool // stub forge 返回 1（模拟 init 失败，验 partial-state 回显）
		wantSub   string // 期望输出子串；空=期望静默（无"未启用 forge"）
		wantInit  bool   // 期望 forge init 被调（flag 文件存在）
	}{
		{
			name:    `无 git 目录静默`,
			cwdFn:   func(t *testing.T) string { return t.TempDir() },
			wantSub: ``,
		},
		{
			name:    `有 git 有 forge 静默`,
			cwdFn:   func(t *testing.T) string { return mkGitProj(t, true) },
			wantSub: ``,
		},
		{
			name:    `有 git 无 forge 首次提示`,
			cwdFn:   func(t *testing.T) string { return mkGitProj(t, false) },
			wantSub: `未启用 forge`,
		},
		{
			name:    `有 git 无 forge 已 suggested 静默`,
			cwdFn:   func(t *testing.T) string { return mkGitProj(t, false) },
			marker:  `suggested`,
			wantSub: ``,
		},
		{
			name:    `有 git 无 forge declined 永久静默`,
			cwdFn:   func(t *testing.T) string { return mkGitProj(t, false) },
			marker:  `declined`,
			wantSub: ``,
		},
		{
			name:     `AUTO_INIT 调 forge init`,
			cwdFn:    func(t *testing.T) string { return mkGitProj(t, false) },
			autoInit: true,
			wantInit: true,
		},
		{
			name:      `AUTO_INIT forge init 失败回显`,
			cwdFn:     func(t *testing.T) string { return mkGitProj(t, false) },
			autoInit:  true,
			failForge: true,
			wantSub:   `失败`,
			wantInit:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cwd := c.cwdFn(t)
			tag := `tag_` + c.name
			home := t.TempDir()
			if c.marker != `` {
				writeSuggestMarker(t, home, tag, c.marker)
			}
			initFlag := filepath.Join(home, `init-flag`)
			var extra []string
			if c.autoInit {
				extra = append(extra, `FORGE_AUTO_INIT=1`)
			}
			if c.failForge {
				extra = append(extra, `FORGE_FORGE_FAIL=1`)
			}
			out := runInitSuggestHook(t, cwd, tag, home, initFlag, extra...)
			if c.wantSub != `` && !strings.Contains(out, c.wantSub) {
				t.Errorf(`期望输出含 %q，实得 %q`, c.wantSub, out)
			}
			if c.wantSub == `` && strings.Contains(out, `未启用 forge`) {
				t.Errorf(`期望静默但输出了提示：%q`, out)
			}
			_, initCalled := os.Stat(initFlag)
			if c.wantInit && initCalled != nil {
				t.Errorf(`期望 forge init 被调（flag 文件应存在），实得输出 %q`, out)
			}
			if !c.wantInit && initCalled == nil {
				t.Errorf(`此分支不应调 forge init，但 flag 文件被创建`)
			}
		})
	}
}

// TestInitSuggestHook_WritesSuggestedMarker：首次提示分支必须写 suggested 标记，
// 下次同项目不再提示（一次提示契约）。跑两次脚本共享 home，第二次应静默。
func TestInitSuggestHook_WritesSuggestedMarker(t *testing.T) {
	proj := mkGitProj(t, false)
	tag := `tag_once`
	home := t.TempDir()
	initFlag := filepath.Join(home, `init-flag`)

	out1 := runInitSuggestHook(t, proj, tag, home, initFlag)
	if !strings.Contains(out1, `未启用 forge`) {
		t.Fatalf(`首次应提示，实得 %q`, out1)
	}
	marker := filepath.Join(home, `.forge`, `.init-suggested`, tag)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf(`首次应写 suggested 标记 %s: %v`, marker, err)
	}

	out2 := runInitSuggestHook(t, proj, tag, home, initFlag)
	if strings.Contains(out2, `未启用 forge`) {
		t.Errorf(`第二次应静默（标记已写），实得 %q`, out2)
	}
}
