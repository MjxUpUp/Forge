package taskpipeline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
)

// hazardpending_test.go — 墙-1b 清账门 + 墙-2a 考卷质量门 + 硬-1b mutation
// 复发门的行为钉（配对 hazardpending.go / mutationgate.go 与 acceptance_register.go
// 的质量检查）。

// hazardFixture 建带 hazard 事件流的项目（RecordEvent 落 Block 事件 → pending>0）。
func hazardFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	run("git", "init")
	run("git", "config", "user.email", "t@t.com")
	run("git", "config", "user.name", "T")
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module hz\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package hz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "init")
	return dir
}

// TestCheckHazardPending_BlocksDelivery：事件流里有未确认 Block → 拒绝完成；
// 确认事件（Confirm）落盘后放行；env 逃生落审计行。
func TestCheckHazardPending_BlocksDelivery(t *testing.T) {
	dir := hazardFixture(t)
	state := &TaskState{TaskRef: "hz-1", Branch: "feat/x"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	// 无事件：放行。
	if ok, reasons := CheckHazardPending(dir, state); !ok {
		t.Fatalf("无事件应放行, got %v", reasons)
	}
	// 落一个 Block 事件（绕过 hook 直接写事件流——hook 层行为属 hook 测试）。
	evDir := filepath.Join(dataHome(dir), "hazards")
	if err := os.MkdirAll(evDir, 0o755); err != nil {
		t.Fatal(err)
	}
	blockRow := `{"type":"block","ts":"` + time.Now().Format(time.RFC3339) + `","command":"rm -rf /tmp/x","fingerprint":"abc123"}`
	if err := os.WriteFile(filepath.Join(evDir, "events.jsonl"), []byte(blockRow+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, reasons := CheckHazardPending(dir, state)
	if ok {
		t.Fatal("未确认拦截悬账应阻断完成")
	}
	if len(reasons) == 0 || !strings.Contains(reasons[0], "hazard confirm") {
		t.Fatalf("拒绝文案须给真人出口: %v", reasons)
	}
	// env 逃生：放行 + 审计行。
	t.Setenv("FORGE_HAZARD_PENDING", "disable")
	if ok, reasons := CheckHazardPending(dir, state); !ok {
		t.Fatalf("env 逃生应放行, got %v", reasons)
	}
	entries, _ := checklog.LoadForTask(dir, state.TaskRef)
	found := false
	for _, e := range entries {
		if e.Check == checklog.CheckEscapeHatch && checklog.EscapeGateOf(&e) == "hazard-pending" {
			found = true
		}
	}
	if !found {
		t.Error("逃生应落 hazard-pending 审计行")
	}
	t.Setenv("FORGE_HAZARD_PENDING", "")
}

// TestCheckAcceptanceQuality_TestClassFloor（墙-2a）：有测试能力的仓库里，
// 纯 go build 考卷被拒；含 go test 放行；无测试能力仓库跳过（假墙防）。
func TestCheckAcceptanceQuality_TestClassFloor(t *testing.T) {
	// 无测试能力的 fixture：quality no-op。
	dir := hazardFixture(t)
	st := &TaskState{TaskRef: "qz", Acceptance: ParseAcceptance([]string{"go build ./... :: "})}
	if ok, reasons := CheckAcceptanceQuality(dir, st); !ok {
		t.Fatalf("无测试能力仓库应跳过质量门（防假墙）, got %v", reasons)
	}
	// 造测试能力：加一个 _test.go。
	if err := os.WriteFile(filepath.Join(dir, "m_test.go"), []byte("package hz\n\nimport \"testing\"\n\nfunc TestM(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := exec.Command("git", "-C", dir, "add", ".")
	_ = run.Run()
	if ok, reasons := CheckAcceptanceQuality(dir, st); ok {
		t.Fatalf("有测试能力 + 纯构建考卷应被拒: %v", reasons)
	} else if !strings.Contains(reasons[0], "测试类命令") {
		t.Fatalf("拒绝文案要点名测试类下限: %v", reasons)
	}
	// 补一条测试类命令 → 放行。
	st.Acceptance = ParseAcceptance([]string{"go build ./... :: ", "go test ./... :: ok"})
	if ok, reasons := CheckAcceptanceQuality(dir, st); !ok {
		t.Fatalf("含测试类命令应放行, got %v", reasons)
	}
	// generic / 零考卷 / 逃生：跳过。
	if ok, _ := CheckAcceptanceQuality(dir, &TaskState{TaskRef: "g", Kind: TaskKindGeneric}); !ok {
		t.Error("generic 豁免")
	}
	st2 := &TaskState{TaskRef: "qz2", Acceptance: ParseAcceptance([]string{"go build ./... :: "}), Overrides: TaskOverrides{AcceptanceGate: "disable"}}
	if ok, _ := CheckAcceptanceQuality(dir, st2); !ok {
		t.Error("acceptance-gate 逃生共用舱应覆盖质量门")
	}
}

// TestCheckMutationRecurrence_StreakAndHardening（硬-1b）：首发 advisory、
// 连续零消费 ≥ 阈值硬拦、跑了 mutation 或 env 逃生放行。
func TestCheckMutationRecurrence_StreakAndHardening(t *testing.T) {
	dir := hazardFixture(t)
	// 带 Go 源码改动的当前任务（无配对测试也行——changedGoSourceFiles 只看改动）。
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package hz\n\n// touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cur := &TaskState{TaskRef: "cur", Branch: "feat/x"}
	if err := SaveTaskState(dir, cur); err != nil {
		t.Fatal(err)
	}
	// 无历史：advisory。
	ok, reasons, adv := CheckMutationRecurrence(dir, cur)
	if !ok || len(reasons) > 0 || adv == "" {
		t.Fatalf("无历史应 advisory, got ok=%v reasons=%v adv=%q", ok, reasons, adv)
	}
	// 造 3 个历史完成任务（Go 改动 + 零 mutation）——用 task state + 工作区改动
	// 归属各任务不可行（共享工作区）；改用「streak 只数任务、changedGoSourceFiles
	// 按各 task 的 HeadCommit..HEAD」——测试里简化：给历史任务也留当前改动即可
	//（HeadCommit 空 → 回落 main...HEAD 或工作区）。此处构造最小 streak。
	headOut, herr := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if herr != nil {
		t.Fatal(herr)
	}
	head := strings.TrimSpace(string(headOut))
	cur.HeadCommit = head
	// 历史任务造一段已提交 Go 改动（historicalGoChanged 只认 <HeadCommit>..HEAD
	// 的纯已提交口径）。
	if err := os.WriteFile(filepath.Join(dir, "hist.go"), []byte("package hz\n\n// H\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "hist change").Run()
	for i := 0; i < mutationRecurrenceThreshold; i++ {
		h := &TaskState{TaskRef: fmt_ref(i), Branch: "feat/x", HeadCommit: head}
		h.MarkComplete()
		if err := SaveTaskState(dir, h); err != nil {
			t.Fatal(err)
		}
	}
	ok, reasons, _ = CheckMutationRecurrence(dir, cur)
	if ok {
		t.Fatal("streak 达阈值应硬拦")
	}
	if !strings.Contains(reasons[0], "forge task mutation") {
		t.Fatalf("拒绝文案须给消费出口: %v", reasons)
	}
	// 跑过 mutation（伪造证据行）→ 放行。
	if err := checklog.Record(dir, &checklog.Entry{Check: CheckNameMutationSampling, TaskRef: "cur", Passed: true, Checked: true}); err != nil {
		t.Fatal(err)
	}
	if ok, reasons, _ = CheckMutationRecurrence(dir, cur); !ok {
		t.Fatalf("跑过 mutation 应放行, got %v", reasons)
	}
	// env 逃生：放行。
	t.Setenv("FORGE_MUTATION_GATE", "disable")
	st2 := &TaskState{TaskRef: "cur2", Branch: "feat/x"}
	if err := SaveTaskState(dir, st2); err != nil {
		t.Fatal(err)
	}
	if ok, reasons, _ := CheckMutationRecurrence(dir, st2); !ok {
		t.Fatalf("env 逃生应放行, got %v", reasons)
	}
}

func fmt_ref(i int) string { return fmt.Sprintf("hist-%d", i) }

// TestHazardPending_MissingEventsDetected（审查 P1-2）：hazards 目录非空而
// events.jsonl 缺失 = 事件流被清除 → 按悬账阻断（清账证据被毁不是清白证明）。
func TestHazardPending_MissingEventsDetected(t *testing.T) {
	dir := hazardFixture(t)
	state := &TaskState{TaskRef: "hz-2"}
	if err := SaveTaskState(dir, state); err != nil {
		t.Fatal(err)
	}
	// 目录空/不存在：干净放行。
	if ok, _ := CheckHazardPending(dir, state); !ok {
		t.Fatal("无 hazards 目录应放行")
	}
	// 事件落过盘后 events.jsonl 被删（目录残留——含空目录形态：confirm 标记
	// 5min TTL 过期被清后 events.jsonl 常是唯一文件）→ 阻断。
	evDir := filepath.Join(dataHome(dir), "hazards")
	if err := os.MkdirAll(evDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ok, reasons := CheckHazardPending(dir, state)
	if ok {
		t.Fatal("事件流被清除（目录在文件缺，含空目录）应阻断")
	}
	if !strings.Contains(reasons[0], "清除") && !strings.Contains(reasons[0], "缺失") {
		t.Fatalf("拒绝文案要点名清除: %v", reasons)
	}
}

// TestMutationRecurrence_TruncatedByHistory（终审④，判别力构造）：base 之上
// 三个零消费历史任务 + 最近一个消费者（带 mutation 行）→ break 截断 streak
// → 放行。判别力：降序排序或 break 任一回归 → 三个 old 数满 streak=3 → 硬拦
// → 本测试红（先证钉子会咬人）。
func TestMutationRecurrence_TruncatedByHistory(t *testing.T) {
	dir := hazardFixture(t)
	run := func(args ...string) {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	// base = init 提交。
	baseOut, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	base := strings.TrimSpace(string(baseOut))
	// base..HEAD₁ 含 m.go（历史任务的已提交 Go 改动）。
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte("package hz\n\n// touch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "touch")
	// 三个零消费 old（按时间先后 MarkComplete）。
	for i := 0; i < 3; i++ {
		h := &TaskState{TaskRef: fmt.Sprintf("tr-old-%d", i), Branch: "feat/x", HeadCommit: base}
		h.MarkComplete()
		if err := SaveTaskState(dir, h); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * time.Millisecond) // 防 Windows 时间戳并列致排序不稳定
	}
	// 消费者：最近完成 + mutation 证据行（break 应最先命中它）。
	cons := &TaskState{TaskRef: "tr-consumer", Branch: "feat/x", HeadCommit: base}
	cons.MarkComplete()
	if err := SaveTaskState(dir, cons); err != nil {
		t.Fatal(err)
	}
	if err := checklog.Record(dir, &checklog.Entry{Check: CheckNameMutationSampling, TaskRef: "tr-consumer", Passed: true, Checked: true}); err != nil {
		t.Fatal(err)
	}
	// 当前任务：untracked Go 改动 + 未跑 mutation。
	if err := os.WriteFile(filepath.Join(dir, "cur.go"), []byte("package hz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cur := &TaskState{TaskRef: "tr-cur", Branch: "feat/x", HeadCommit: base}
	if err := SaveTaskState(dir, cur); err != nil {
		t.Fatal(err)
	}
	ok, reasons, _ := CheckMutationRecurrence(dir, cur)
	if !ok {
		t.Fatalf("最近历史任务跑过 mutation 应截断 streak 放行；若排序/break 回归，三个 old 数满 streak=3 会硬拦——reasons=%v", reasons)
	}
}
