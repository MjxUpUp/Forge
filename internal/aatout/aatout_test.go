package aatout

// aatout_test.go — AAT mapper 契约测试：meta 头声明偏离、prev_hash 链完整性
// （record N 的 prev_hash == sha256(canonical(record N-1))）、确定性导出（同输入
// 两次导出逐字节一致——增量消费前提）、action/outcome 分桶。

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/nodestamp"
)

func entry(check checklog.CheckName, level checklog.Level, detail, task, session, node string, at time.Time) checklog.Entry {
	return checklog.Entry{
		Check: check, Passed: level != checklog.LevelBlocked && level != checklog.LevelFail,
		Checked: true, Level: level, Detail: detail, TaskRef: task, SessionID: session,
		RecordedAt: at,
		Stamp:      nodestamp.Stamp{NodeID: node, Seq: 1},
	}
}

// 解析导出：首行 meta + 余下记录。
func parseExport(t *testing.T, body []byte) (ExportMeta, []AATRecord) {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) < 2 {
		t.Fatalf("导出应含 meta + 至少一条记录：%q", body)
	}
	var meta ExportMeta
	if err := json.Unmarshal([]byte(lines[0]), &meta); err != nil {
		t.Fatalf("meta 行解析失败: %v", err)
	}
	var recs []AATRecord
	for _, l := range lines[1:] {
		var r AATRecord
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatalf("记录行解析失败: %v（%s）", err, l)
		}
		recs = append(recs, r)
	}
	return meta, recs
}

func TestBuildExport_ChainIntegrity(t *testing.T) {
	base := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		entry(checklog.CheckTaskGuard, checklog.LevelBlocked, "BLOCKED: 无任务编辑源码", "t/a", "s1", "fnode_x", base),
		entry(checklog.CheckCheatScan, checklog.LevelPass, "pass: 干净", "t/a", "s1", "fnode_x", base.Add(time.Minute)),
		entry(checklog.CheckSkillTrigger, checklog.LevelPass, "hit", "", "", "fnode_x", base.Add(2*time.Minute)),
	}
	body, err := BuildExport(entries, Options{AgentVersion: "1.51.0"})
	if err != nil {
		t.Fatal(err)
	}
	meta, recs := parseExport(t, body)
	if meta.Meta != "forge-aat-export" || meta.MapperVer != MapperVersion || len(meta.Deviations) != 4 {
		t.Fatalf("meta 契约不符: %+v", meta)
	}
	if len(recs) != 3 {
		t.Fatalf("应 3 条记录，实际 %d", len(recs))
	}
	// genesis prev_hash
	if recs[0].PrevHash != genesisHash {
		t.Fatalf("首记录 prev_hash 应为 genesis 零哈希")
	}
	// 链：rec[i].prev_hash == sha256(canonical(rec[i-1]))（canonical 里 prev_hash
	// 取 rec[i-1] 自己的字段——canonicalJSON(recs[i-1]) 即为当时被哈希的字节）。
	for i := 1; i < len(recs); i++ {
		canonical, err := canonicalJSON(recs[i-1])
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(canonical)
		if recs[i].PrevHash != hex.EncodeToString(sum[:]) {
			t.Fatalf("链断裂于记录 %d", i)
		}
		if recs[i].ParentID != recs[i-1].RecordID {
			t.Fatalf("parent id 断裂于记录 %d", i)
		}
	}
}

func TestBuildExport_Classification(t *testing.T) {
	base := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		entry(checklog.CheckTaskGuard, checklog.LevelBlocked, "b", "t", "s", "n", base),
		entry(checklog.CheckCheatScan, checklog.LevelFail, "f", "t", "s", "n", base),
		entry(checklog.CheckCheatScan, checklog.LevelWarn, "w", "t", "s", "n", base),
		entry(checklog.CheckCheatScan, checklog.LevelPass, "p", "t", "s", "n", base),
	}
	_, recs := parseExport(t, mustExport(t, entries))
	want := [][2]string{
		{"decision", "denied"}, {"error", "failure"}, {"escalation", "escalated"}, {"decision", "success"},
	}
	for i, w := range want {
		if recs[i].ActionType != w[0] || recs[i].Outcome != w[1] {
			t.Fatalf("记录 %d 分类不符: %s/%s（期望 %s/%s）", i, recs[i].ActionType, recs[i].Outcome, w[0], w[1])
		}
	}
	// agent_id URI 形态 + trust 映射（Source 由 EffectiveSource 兜底路径决定——
	// 显式设 Source 的确定性断言）。
	entries[0].Source = checklog.EvidenceDeterministic
	_, recs2 := parseExport(t, mustExport(t, entries))
	if recs2[0].AgentID != "forge://node/n" || recs2[0].TrustLevel != "L3" {
		t.Fatalf("agent_id/trust 不符: %s %s", recs2[0].AgentID, recs2[0].TrustLevel)
	}
}

func TestBuildExport_Deterministic(t *testing.T) {
	base := time.Date(2026, 9, 6, 9, 0, 0, 0, time.UTC)
	entries := []checklog.Entry{
		entry(checklog.CheckCheatScan, checklog.LevelPass, "p", "t", "s", "n", base),
	}
	// meta 的 GeneratedAt 会变——确定性断言剥离 meta 行，比记录行逐字节一致。
	a := mustExport(t, entries)
	b := mustExport(t, entries)
	linesA := strings.SplitN(string(a), "\n", 2)
	linesB := strings.SplitN(string(b), "\n", 2)
	if linesA[1] != linesB[1] {
		t.Fatalf("记录行应确定性一致（增量消费前提）:\n%s\n%s", linesA[1], linesB[1])
	}
}

func TestDeterministicUUID(t *testing.T) {
	a, b := deterministicUUID("x"), deterministicUUID("x")
	if a != b {
		t.Fatal("同身份应同 UUID")
	}
	if c := deterministicUUID("y"); a == c {
		t.Fatal("异身份应异 UUID")
	}
	// RFC 4122 形态：version 5 与 variant 位。
	if a[14] != '5' {
		t.Fatalf("version 位应 5: %s", a)
	}
	if a[19] != '8' && a[19] != '9' && a[19] != 'a' && a[19] != 'b' {
		t.Fatalf("variant 位应 8/9/a/b: %s", a)
	}
}

func mustExport(t *testing.T, entries []checklog.Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteExport(&buf, entries, Options{}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
