package taskpipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MjxUpUp/Forge/internal/taskcontext"
)

// TestSplitDepRef pins the key:ref syntax table: no colon (or a LEADING colon)
// is same-repo with zero behavior change; the FIRST colon splits so a taskRef
// may carry colons while a key never can; a trailing colon yields an empty
// taskRef (its load fails → the gate treats it as pending, conservative).
//
// TestSplitDepRef 钉住 key:ref 语法表：无冒号（或冒号在首位）= 本仓，零行为
// 变化；按第一个冒号拆分，故 taskRef 可含冒号而 key 绝不；结尾冒号得到空
// taskRef（加载必失败 → 门禁计 pending，保守）。
func TestSplitDepRef(t *testing.T) {
	cases := []struct {
		in      string
		wantKey string
		wantRef string
	}{
		{`feat/login`, ``, `feat/login`}, // 裸 ref：本仓
		{`PROJ-123`, ``, `PROJ-123`},     // 裸 ticket ref
		{`other:feat/login`, `other`, `feat/login`},
		{`k:a:b`, `k`, `a:b`}, // 首个冒号拆分：taskRef 可含冒号
		{`:foo`, ``, `:foo`},  // 首位冒号：畸形但按本仓处理（零行为变化）
		{`key:`, `key`, ``},   // 空 taskRef：加载失败 → pending
		{``, ``, ``},          // 空条目：调用方跳过
	}
	for _, c := range cases {
		key, ref := SplitDepRef(c.in)
		if key != c.wantKey || ref != c.wantRef {
			t.Errorf("SplitDepRef(%q) = (%q, %q), want (%q, %q)", c.in, key, ref, c.wantKey, c.wantRef)
		}
	}
}

// writeDepState writes one task state file into the FORGE_DATA_HOME-relative
// DataDir of the given key — the on-disk shape a cross-repo dep resolves to.
//
// writeDepState 往指定 key 的 DataDir（相对 FORGE_DATA_HOME）写一个 task
// state 文件——跨仓依赖解析到的磁盘形态。
func writeDepState(t *testing.T, home, key string, s *TaskState) {
	t.Helper()
	dir := filepath.Join(home, `projects`, key, `tasks`)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, taskcontext.SanitizeRef(s.TaskRef)+`.json`), data, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestPendingDependencies_CrossRepo covers the gate's block list across repos:
// same-repo behavior is unchanged (delivered passes, missing is pending), and
// every cross-repo failure shape — incomplete target, missing task, key with
// no data dir, sanitize-collision mismatch — is conservatively PENDING with
// the ref returned verbatim (the gate prints the block list from it).
//
// TestPendingDependencies_CrossRepo 覆盖门禁阻断清单的跨仓行为：本仓行为不变
// （已交付放行、缺失计 pending），且每种跨仓失败形态——目标未完成、目标
// 缺失、key 无数据目录、sanitize 串号——一律保守计 PENDING，且返回的 ref
// 原样保留（门禁据此打印阻断清单）。
func TestPendingDependencies_CrossRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv(`FORGE_DATA_HOME`, home)
	root := t.TempDir() // 非 git → DataDir 走 PathKey(root)，同仓 ref 用 SaveTaskState 落盘

	// Same-repo: one delivered (assignment-delivered), and a missing ref.
	//
	// 本仓：一个已交付（assignment delivered），加一个缺失 ref。
	if err := SaveTaskState(root, &TaskState{TaskRef: `local-done`, Assignment: &Assignment{Status: AssignDelivered}}); err != nil {
		t.Fatal(err)
	}

	// Cross-repo member bb0000000002: delivered / in-flight / collision shapes.
	//
	// 跨仓成员 bb0000000002：已交付 / 未完成 / 串号 三种形态。
	writeDepState(t, home, `bb0000000002`, &TaskState{TaskRef: `b-done`, Assignment: &Assignment{Status: AssignDelivered}})
	writeDepState(t, home, `bb0000000002`, &TaskState{TaskRef: `b-wip`})
	// Sanitize collision: the FILE name for "b:other" is b-other.json, but the
	// stored TaskRef differs from the requested one — the load guard must reject
	// it (pending), never leak the other task's delivered state.
	//
	// sanitize 串号："b:other" 的文件名是 b-other.json，但文件内 TaskRef 与请求
	// 不同——加载防护必须拒绝（计 pending），绝不把另一个任务的已交付状态漏过来。
	writeDepState(t, home, `bb0000000002`, &TaskState{TaskRef: `b/other`, Assignment: &Assignment{Status: AssignDelivered}})

	pending := PendingDependencies(root, []string{
		`local-done`,            // 本仓已交付 → 放行
		`local-ghost`,           // 本仓缺失 → pending（回归）
		`bb0000000002:b-done`,   // 跨仓已交付 → 放行
		`bb0000000002:b-wip`,    // 跨仓未完成 → pending
		`bb0000000002:b-ghost`,  // 跨仓目标缺失 → pending
		`cc0000000003:anything`, // key 无数据目录 → pending
		`bb0000000002:b:other`,  // 串号防护：请求 ref 与文件内 TaskRef 不符 → pending
		``,                      // 空条目跳过
	})
	want := []string{`local-ghost`, `bb0000000002:b-wip`, `bb0000000002:b-ghost`, `cc0000000003:anything`, `bb0000000002:b:other`}
	if strings.Join(pending, `,`) != strings.Join(want, `,`) {
		t.Errorf("pending = %v, want %v（ref 须原样返回，跨仓失败一律保守计 pending）", pending, want)
	}
}

// TestAddDependency_CrossRepoRef pins AddDependency's generic treatment of
// key:ref entries: stored verbatim, exact-string dedup, and the cycle DFS
// expands only what the injected lookup returns — the production CLI lookup
// (task start) returns nil for key:ref, so a cross-repo edge can never raise
// a same-repo cycle error, and same-repo cycles still refuse when mixed in.
//
// TestAddDependency_CrossRepoRef 钉住 AddDependency 对 key:ref 条目的通用处理：
// 原样存储、精确串去重、环 DFS 只展开注入 lookup 返回的节点——生产 CLI 的
// lookup（task start）对 key:ref 返回 nil，故跨仓边绝不触发本仓环误判，而
// 混入的本仓环仍照常拒绝。
func TestAddDependency_CrossRepoRef(t *testing.T) {
	t.Run(`key:ref stored verbatim + deduped`, func(t *testing.T) {
		a := &TaskState{TaskRef: `A`}
		// CLI 形态 lookup：跨仓 ref 返回 nil（不跨仓 DFS），本仓 ref 查 map。
		states := map[string]*TaskState{`A`: a}
		lookup := func(ref string) *TaskState {
			if key, _ := SplitDepRef(ref); key != `` {
				return nil
			}
			return states[ref]
		}
		if err := a.AddDependency([]string{`bb0000000002:b-1`, `bb0000000002:b-1`, `local`}, lookup); err != nil {
			t.Fatalf(`跨仓 ref 应接受, got %v`, err)
		}
		if len(a.DependsOn) != 2 || a.DependsOn[0] != `bb0000000002:b-1` || a.DependsOn[1] != `local` {
			t.Fatalf(`应原样存储并去重为 [bb0000000002:b-1 local], got %v`, a.DependsOn)
		}
	})
	t.Run(`same-repo cycle still refused alongside cross-repo refs`, func(t *testing.T) {
		// 本仓 B→A 已存在；A 同时加 bb0000000002:x（跨仓，不参与 DFS）与 B（本仓，闭环）——须拒绝且 all-or-nothing。
		a := &TaskState{TaskRef: `A`}
		b := &TaskState{TaskRef: `B`, DependsOn: []string{`A`}}
		states := map[string]*TaskState{`A`: a, `B`: b}
		lookup := func(ref string) *TaskState {
			if key, _ := SplitDepRef(ref); key != `` {
				return nil
			}
			return states[ref]
		}
		if err := a.AddDependency([]string{`bb0000000002:x`, `B`}, lookup); err == nil {
			t.Fatal(`混入的本仓环 A→B→A 应被拒绝`)
		}
		if len(a.DependsOn) != 0 {
			t.Fatalf(`all-or-nothing：环拒绝后 DependsOn 不应部分提交, got %v`, a.DependsOn)
		}
	})
	t.Run(`own-key self-ref is NOT caught here (CLI validation owns it)`, func(t *testing.T) {
		// 钉住已文档化的边界：AddDependency 的裸串自引用检查看不穿 key 前缀
		// （`mine:A` != `A`）——该形态由 cli 的 validateDependsOnRefs 拒绝。此处
		// 只保证它按普通跨仓边处理（lookup 返回 nil → 无环），不做隐式拦截。
		a := &TaskState{TaskRef: `A`}
		lookup := func(string) *TaskState { return nil }
		if err := a.AddDependency([]string{`mine:A`}, lookup); err != nil {
			t.Fatalf(`本方法不拦截 own-key 自引用（CLI 校验的职责）, got %v`, err)
		}
	})
}

// TestLoadDepState_RejectsMalformedKey pins that a DependsOn key outside the two
// legitimate key shapes (traversal / separator / wrong length) is refused BEFORE
// being joined into a filesystem path — the key is bundle-traveling input, and an
// unchecked `..` would steer the read-only scan outside the data home. The refusal
// surfaces as an error → the gate counts the dep as pending (conservative, never
// silently delivered).
//
// TestLoadDepState_RejectsMalformedKey 钉住两种合法形态之外的 DependsOn key
// （穿越/分隔符/长度错）在拼进文件系统路径之前被拒——key 是随 bundle 旅行的
// 输入，未校验的 `..` 会把只读扫描引出数据 home。拒绝以 error 呈现 → 门禁计
// pending（保守，绝不静默放行）。
func TestLoadDepState_RejectsMalformedKey(t *testing.T) {
	for _, key := range []string{`..`, `../x`, `a/b`, `nothexkey!!!`, `0123456789a`} {
		if _, err := LoadDepState(t.TempDir(), key+`:some/ref`); err == nil {
			t.Errorf("LoadDepState(key=%q) 应拒绝畸形 key", key)
		}
	}
}
