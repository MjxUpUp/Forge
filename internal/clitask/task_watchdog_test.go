package clitask

// task_watchdog_test.go — watchdog 的核心判定单测：最后活动取台账推进 / 无台账
// 回落 StartedAt / marker 节流（每小时一条）。时间戳显式写 JSONL（Record 会盖
// RecordedAt，测不了历史时间）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
)

func seedChecklogRow(t *testing.T, root, ref string, at time.Time) {
	t.Helper()
	dir := forgedata.DataDirFor(root)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(checklog.Entry{Check: "task-verify", Passed: true, TaskRef: ref, RecordedAt: at})
	f, err := os.OpenFile(filepath.Join(dir, "checklog.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	f.Write(append(body, 10)) // 10 = '\n'
}

func TestLastActivityFor(t *testing.T) {
	dir := t.TempDir()
	started := time.Now().Add(-2 * time.Hour)
	older := time.Now().Add(-90 * time.Minute)
	newer := time.Now().Add(-10 * time.Minute)
	seedChecklogRow(t, dir, "t/wd", older)
	seedChecklogRow(t, dir, "t/wd", newer)
	got := lastActivityFor(dir, "t/wd", started)
	if !got.Equal(newer) {
		t.Fatalf("最后活动应取台账最新时间: %v（期望 %v）", got, newer)
	}
	// 无任何台账 → 回落 StartedAt。
	got = lastActivityFor(dir, "t/none", started)
	if !got.Equal(started) {
		t.Fatalf("无台账应回落 StartedAt: %v", got)
	}
}

func TestRecordStalled_Throttle(t *testing.T) {
	dir := t.TempDir()
	st := stalledTask{Ref: "t/th", Idle: time.Hour, LastActive: time.Now().Add(-time.Hour)}
	now := time.Now()
	recordStalled(dir, st, now)
	rows, _ := checklog.LoadAll(dir)
	if len(rows) != 1 || rows[0].Check != checklog.CheckTaskStalled {
		t.Fatalf("应落一条 task-stalled: %+v", rows)
	}
	// 同小时重扫：marker 节流，不再落行。
	recordStalled(dir, st, now.Add(5*time.Minute))
	rows, _ = checklog.LoadAll(dir)
	if len(rows) != 1 {
		t.Fatalf("marker 节流失效: %+v", rows)
	}
	if _, err := os.Stat(filepath.Join(markerDir(dir), "task-stalled-t__th-"+now.Format("06010215")+".marker")); err != nil {
		t.Fatalf("marker 文件缺失: %v", err)
	}
}

// TestMirrorGithubFakeGh 经 FORGE_GH_BIN 注入假 gh：创建动作解析编号并写回映射。
// 假 gh 按 GOOS 生成（.bat/.sh）——Go exec 在 Windows 跑不了 sh 脚本，CI 三平台矩阵。
func TestMirrorGithubFakeGh(t *testing.T) {
	dir := t.TempDir()
	var fake string
	if runtime.GOOS == "windows" {
		fake = filepath.Join(dir, "fake-gh.bat")
		body := strings.Join([]string{
			"@echo off",
			`if "%1"=="issue" if "%2"=="create" (`,
			"echo https://github.com/o/r/issues/99",
			"exit /b 0",
			")",
			"echo ok",
		}, "\r\n") + "\r\n"
		if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		fake = filepath.Join(dir, "fake-gh.sh")
		body := "#!/bin/sh\n" +
			`case "$1$2" in issuecreate) echo "https://github.com/o/r/issues/99";; *) echo ok;; esac` + "\n"
		if err := os.WriteFile(fake, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("FORGE_GH_BIN", fake)
	mapping := map[string]int{}
	a := taskpipeline.MirrorAction{TaskRef: "t/m", Title: "[forge] x (t/m)", LabelAdd: []string{"forge:offered"}}
	if err := execMirrorAction(t.TempDir(), "", &a, mapping); err != nil {
		t.Fatalf("execMirrorAction: %v", err)
	}
	if a.Issue != 99 || mapping["t/m"] != 99 {
		t.Fatalf("编号解析/映射写回失败: issue=%d mapping=%v", a.Issue, mapping)
	}
}
