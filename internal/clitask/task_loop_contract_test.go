package clitask

// task_loop_contract_test.go —— loop 契约 B1 的 CLI 面：死路记忆的 decide
// --rejected / 渲染 / redact 往返（B1-4，REQ-4），status --json 的 loop_verdict
// 顶层键与键序（B1-2）。全部经 cobra 解析面驱动。

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// runTaskSub 经 Root 全链路执行 task 子命令，先把该子命令的 flag 重置回默认
// （同进程多次 Execute 时 cobra flag 值残留）。
func runTaskSub(t *testing.T, sub string, args ...string) {
	t.Helper()
	cmd, _, err := Root.Find([]string{sub})
	if err != nil {
		t.Fatal(err)
	}
	resetCmdFlags(cmd)
	Root.SetArgs(append([]string{sub}, args...))
	Root.SetOut(nil)
	Root.SetErr(nil)
	if err := Root.Execute(); err != nil {
		t.Fatalf("%s %v: %v", sub, args, err)
	}
}

func resetCmdFlags(cmd *cobra.Command) {
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}

// TestResumeRendersDeadEnds（REQ-4）钉住死路记忆：
//   - `decide --rejected` 落 Kind=dead-end，渲染在独立节【已证伪方案】与 tl;dr，
//     不出现在【已确认决策】节；
//   - 先 --rejected 再普通 decide 同一内容 → 两条记录（幂等键含 Kind）；
//   - 同 Kind 同内容重跑 → 幂等跳过；
//   - redact 往返替换 Content/Rationale 但保留 Kind。
func TestResumeRendersDeadEnds(t *testing.T) {
	dir, ref := setupChainTask(t, "")
	runTaskSub(t, "decide", "--rejected", "--content", "用全局锁串行化写入", "--rationale", "压测 p99 翻 4 倍", "--by", "[claude-code]")
	runTaskSub(t, "decide", "--rejected", "--content", "用全局锁串行化写入", "--rationale", "压测 p99 翻 4 倍", "--by", "[claude-code]")
	runTaskSub(t, "decide", "--content", "用全局锁串行化写入", "--by", "[claude-code]")
	runTaskSub(t, "decide", "--content", "用分片锁", "--by", "[claude-code]")

	st, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Decisions) != 3 {
		t.Fatalf("应有 3 条记录（死路 1 + 同内容采纳 1 + 分片锁 1）, got %d: %+v", len(st.Decisions), st.Decisions)
	}
	if !st.Decisions[0].IsDeadEnd() || st.Decisions[1].IsDeadEnd() || st.Decisions[2].IsDeadEnd() {
		t.Fatalf("Kind 分布错: %+v", st.Decisions)
	}

	out := renderResume(st, nil)
	deadIdx := strings.Index(out, "【已证伪方案】（不要重试）")
	confIdx := strings.Index(out, "【已确认决策】（不要推翻）")
	if deadIdx < 0 || confIdx < 0 || deadIdx > confIdx {
		t.Fatalf("已证伪方案节应存在且在已确认决策之前:\n%s", out)
	}
	deadSec, confSec := out[deadIdx:confIdx], out[confIdx:]
	if !strings.Contains(deadSec, "用全局锁串行化写入") || !strings.Contains(deadSec, "证据: 压测 p99 翻 4 倍") {
		t.Fatalf("已证伪节应含方案与证据:\n%s", deadSec)
	}
	if !strings.Contains(confSec, "用分片锁") || strings.Contains(confSec, "压测 p99") {
		t.Fatalf("已确认决策节不得混入死路记录的证据:\n%s", confSec)
	}
	// 已确认决策节里"用全局锁串行化写入"只能出现一次（采纳那条），死路那条不在此节。
	if n := strings.Count(confSec, "用全局锁串行化写入"); n != 1 {
		t.Fatalf("已确认决策节应恰含 1 条同内容采纳记录, got %d", n)
	}
	if !strings.Contains(out, "已证伪 1 条（不要重试）: 用全局锁串行化写入") {
		t.Fatalf("tl;dr 应列已证伪方案:\n%s", out)
	}

	// redact 往返：Content/Rationale 被替换，Kind 保留；FailHistory.Changed（仓库路径）清空，指纹保留。
	red := *st
	red.Decisions = append([]taskpipeline.Decision(nil), st.Decisions...)
	red.Acceptance = []taskpipeline.AcceptanceCriterion{{Run: "go test ./internal/x/", FailHistory: []tasktypes.AcceptanceFailure{{Fingerprint: "fp", ChangeHash: "h", Changed: map[string]string{"internal/x/y.go": "abc"}}}}}
	redactTask(&red)
	if fh := red.Acceptance[0].FailHistory; len(fh) != 1 || fh[0].Changed != nil || fh[0].Fingerprint != "fp" {
		t.Fatalf("redact 应清 FailHistory.Changed 路径、保留指纹: %+v", fh)
	}
	for _, d := range red.Decisions {
		if d.Rationale != "" || d.Content == "用全局锁串行化写入" {
			t.Fatalf("redact 应替换 Content/Rationale: %+v", d)
		}
	}
	if !red.Decisions[0].IsDeadEnd() {
		t.Fatal("redact 应保留 Kind")
	}
}

// TestTaskStatusJSONLoopVerdict（B1-2）钉住 status --json 顶层新增 loop_verdict，
// 且原有键集合与顺序不变（嵌入结构体，不经 map 重排）。
func TestTaskStatusJSONLoopVerdict(t *testing.T) {
	dir, ref := setupChainTask(t, "")
	// 登记一条已通过的验收，门禁链走到 verify 之前。
	if err := taskpipeline.MutateTaskState(dir, ref, func(s *taskpipeline.TaskState) error {
		s.Acceptance = []taskpipeline.AcceptanceCriterion{{Run: "true", Passed: true, AcceptedHeadCommit: "abc"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	st, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var plainKeys []string
	if err := orderedKeys(plain, &plainKeys); err != nil {
		t.Fatal(err)
	}

	out := captureStdout(t, func() { runTaskSub(t, "status", "--json", "--ref", ref) })
	var gotKeys []string
	if err := orderedKeys([]byte(out), &gotKeys); err != nil {
		t.Fatalf("status --json 不是合法 JSON: %v\n%s", err, out)
	}
	if len(gotKeys) != len(plainKeys)+1 || gotKeys[len(gotKeys)-1] != "loop_verdict" {
		t.Fatalf("loop_verdict 应为唯一新增且在末尾:\nplain=%v\ngot=%v", plainKeys, gotKeys)
	}
	for i := range plainKeys {
		if gotKeys[i] != plainKeys[i] {
			t.Fatalf("原有键序被改: 第 %d 个 %q vs %q", i, gotKeys[i], plainKeys[i])
		}
	}
	var decoded struct {
		LoopVerdict taskpipeline.LoopVerdict `json:"loop_verdict"`
	}
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.LoopVerdict.Verdict == "" || decoded.LoopVerdict.Next == "" {
		t.Fatalf("loop_verdict 应含 verdict 与 next: %+v", decoded.LoopVerdict)
	}
	// 判词计算不得改写 state（只读）。
	after, err := taskpipeline.LoadTaskState(dir, ref)
	if err != nil {
		t.Fatal(err)
	}
	if after.LoopExhausted != nil || len(after.Decisions) != len(st.Decisions) {
		t.Fatal("status --json 不得写盘")
	}
	_ = tasktypes.DecisionKindDeadEnd
}

// orderedKeys 按出现顺序读取顶层 JSON 对象的键。
func orderedKeys(data []byte, keys *[]string) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok != json.Delim('{') {
		return json.Unmarshal(data, &struct{}{})
	}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return err
		}
		*keys = append(*keys, k.(string))
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return err
		}
	}
	return nil
}
