package act

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/tasktypes"
)

// fixedTime 给测试一个稳定的完成时刻（不用 time.Now，避免时序断言飘）。
var fixedTime = time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

func ec(det, agent int) checklog.EvidenceChain {
	return checklog.EvidenceChain{Deterministic: det, AgentClaim: agent}
}

// facts 构造任务事实清单（判据/红先绿/评审轮次/逃生）——2.3 起结论只由事实聚合，
// 没有任何合成分可传。
func facts(criteria, passed, redTrials, redClosed, rounds, escapes int) *tasktypes.TaskEvidence {
	return &tasktypes.TaskEvidence{
		Criteria: criteria, CriteriaPassed: passed,
		RedFirstTrials: redTrials, RedFirstClosed: redClosed,
		ReviewRounds: rounds, Escapes: escapes,
	}
}

func TestBuildConclusion_NudgeByEvidenceStrength(t *testing.T) {
	// 核心契约：RetrospectiveNudge 只由证据强度触发（2.3 起没有分数判据）。每个 case 用
	// 该档专属关键词断言，防分档回归被掩盖（一档断言短语漏进另一档时立即暴露）。
	tests := []struct {
		name      string
		ec        checklog.EvidenceChain
		ev        *tasktypes.TaskEvidence
		wantNudge bool
		strength  string
	}{
		{
			name:      `NoData→不nudge（无证据中性，与Strength语义一致）`,
			ec:        ec(0, 0),
			ev:        facts(4, 4, 1, 1, 1, 0),
			wantNudge: false,
			strength:  `NoData`,
		},
		{
			// ★ 盲区命门：判据全绿但零 deterministic 证据——agent 自述完成、没真跑验证
			name:      `Unverified→nudge（零实跑证据，对冲LLM-judge盲区）`,
			ec:        ec(0, 2),
			ev:        facts(4, 4, 1, 1, 1, 0),
			wantNudge: true,
			strength:  `Unverified`,
		},
		{
			name:      `Weak→nudge（deterministic<agent-claim，声明主要靠自述）`,
			ec:        ec(1, 3), // ratio=0.25
			ev:        facts(3, 2, 2, 1, 2, 1),
			wantNudge: true,
			strength:  `Weak`,
		},
		{
			name:      `Strong→不nudge（干净完成，无教训可沉淀）`,
			ec:        ec(3, 1), // ratio=0.75
			ev:        facts(5, 5, 1, 1, 1, 0),
			wantNudge: false,
			strength:  `Strong`,
		},
		{
			// ★ 2026-08 校准：重证据任务的一次逃生舱是边际信号，不 cap、不 nudge——
			// 否则 Nudge 只增不减、面板长期红灯（56 条里 0 条真盲区的实证）。
			name: `逃生舱+压倒性证据→Strong不nudge（边际逃生不cap）`,
			ec: checklog.EvidenceChain{
				Deterministic: 100, AgentClaim: 2, UsedEscapeHatch: true, // ratio≈0.98
			},
			ev:        facts(2, 2, 0, 0, 1, 1),
			wantNudge: false,
			strength:  `Strong`,
		},
		{
			name: `逃生舱+非压倒性证据→Weak且nudge（逃生仍有代价）`,
			ec: checklog.EvidenceChain{
				Deterministic: 4, AgentClaim: 1, UsedEscapeHatch: true, // ratio 0.8, det<20
			},
			ev:        facts(2, 1, 1, 0, 1, 1),
			wantNudge: true,
			strength:  `Weak`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := BuildConclusion(`feat/x`, `sess-1`, tt.ev, tt.ec, fixedTime, nil)
			if c.Strength != tt.strength {
				t.Errorf(`Strength=%q want %q（分档回归？检查 Strength 计算）`, c.Strength, tt.strength)
			}
			if c.RetrospectiveNudge != tt.wantNudge {
				t.Errorf(`RetrospectiveNudge=%v want %v（强度档→nudge 映射断链）`, c.RetrospectiveNudge, tt.wantNudge)
			}
			// 事实清单必须逐字进结论——这是回顾/面板唯一的量化来源。
			if c.AcceptancePass != tt.ev.CriteriaPassed || c.AcceptanceTotal != tt.ev.Criteria {
				t.Errorf(`Acceptance=%d/%d want %d/%d`, c.AcceptancePass, c.AcceptanceTotal, tt.ev.CriteriaPassed, tt.ev.Criteria)
			}
			if c.RedFirstTrials != tt.ev.RedFirstTrials || c.RedFirstClosed != tt.ev.RedFirstClosed {
				t.Errorf(`RedFirst=%d/%d want %d/%d`, c.RedFirstClosed, c.RedFirstTrials, tt.ev.RedFirstClosed, tt.ev.RedFirstTrials)
			}
			if c.ReviewRounds != tt.ev.ReviewRounds || c.Escapes != tt.ev.Escapes {
				t.Errorf(`ReviewRounds=%d Escapes=%d want %d/%d`, c.ReviewRounds, c.Escapes, tt.ev.ReviewRounds, tt.ev.Escapes)
			}
		})
	}
}

func TestBuildConclusion_FactsCarried(t *testing.T) {
	// 六项事实逐一映射（含零值边界的独立用例：红先绿未登记时仍应写 0）。
	c := BuildConclusion(`feat/x`, ``, facts(7, 6, 3, 2, 4, 1), ec(3, 1), fixedTime, nil)
	if c.AcceptanceTotal != 7 || c.AcceptancePass != 6 {
		t.Errorf(`Acceptance=%d/%d want 6/7`, c.AcceptancePass, c.AcceptanceTotal)
	}
	if c.RedFirstTrials != 3 || c.RedFirstClosed != 2 {
		t.Errorf(`RedFirst=%d/%d want 2/3`, c.RedFirstClosed, c.RedFirstTrials)
	}
	if c.ReviewRounds != 4 || c.Escapes != 1 {
		t.Errorf(`ReviewRounds=%d Escapes=%d want 4/1`, c.ReviewRounds, c.Escapes)
	}
	// 事实不参与强度判定：证据全绿时 Strong 与判据通过率无关。
	if c.Strength != `Strong` || c.RetrospectiveNudge {
		t.Errorf(`Strength=%q nudge=%v want Strong/false（事实不进强度判定）`, c.Strength, c.RetrospectiveNudge)
	}
}

func TestBuildConclusion_NilEvidence(t *testing.T) {
	// 未提供事实清单（ev=nil）：所有事实字段为零；nudge 仅由强度决定。
	c := BuildConclusion(`feat/x`, ``, nil, ec(0, 2), fixedTime, nil)
	if c.AcceptancePass != 0 || c.AcceptanceTotal != 0 || c.RedFirstTrials != 0 ||
		c.RedFirstClosed != 0 || c.ReviewRounds != 0 || c.Escapes != 0 {
		t.Errorf(`nil ev 应得全零事实，got pass=%d/%d red=%d/%d rounds=%d escapes=%d`,
			c.AcceptancePass, c.AcceptanceTotal, c.RedFirstClosed, c.RedFirstTrials, c.ReviewRounds, c.Escapes)
	}
	if !c.RetrospectiveNudge {
		t.Error(`RetrospectiveNudge=false want true（Unverified 应触发）`)
	}
}

func TestDirective(t *testing.T) {
	// 干净完成（Strong）→空（静默，无噪声）；其余→含 session-retrospective 锚点。
	cases := []struct {
		name    string
		c       Conclusion
		wantSub string // 期望子串；空串表示期望 Directive 返回空
	}{
		{
			name:    `Strong→空（不发噪声）`,
			c:       Conclusion{Strength: `Strong`, Ratio: 0.75, Deterministic: 3, AgentClaim: 1},
			wantSub: ``,
		},
		{
			name:    `Unverified→带证据理由`,
			c:       Conclusion{Strength: `Unverified`, Ratio: 0, Deterministic: 0, AgentClaim: 2, RetrospectiveNudge: true},
			wantSub: `deterministic 证据不足`,
		},
		{
			name:    `Weak→带比率理由`,
			c:       Conclusion{Strength: `Weak`, Ratio: 0.25, Deterministic: 1, AgentClaim: 3, RetrospectiveNudge: true},
			wantSub: `ratio=0.25`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.Directive()
			if tc.wantSub == `` {
				if got != `` {
					t.Errorf(`Strong 应静默，got %q`, got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSub) {
				t.Errorf(`Directive %q 缺子串 %q`, got, tc.wantSub)
			}
			if !strings.HasPrefix(got, `→ session-retrospective:`) {
				t.Errorf(`Directive 应锚定 session-retrospective，got %q`, got)
			}
		})
	}
}

func TestAppendLoadAll_RoundTrip(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())

	c1 := BuildConclusion(`feat/a`, `s1`, facts(2, 2, 1, 1, 1, 0), ec(3, 1), fixedTime, nil)
	c2 := BuildConclusion(`feat/b`, `s2`, facts(3, 0, 2, 0, 3, 2), ec(0, 2), fixedTime.Add(time.Hour), nil)

	if err := Append(root, &c1); err != nil {
		t.Fatalf(`Append c1: %v`, err)
	}
	if err := Append(root, &c2); err != nil {
		t.Fatalf(`Append c2: %v`, err)
	}

	got, err := LoadAll(root)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	if len(got) != 2 {
		t.Fatalf(`LoadAll len=%d want 2`, len(got))
	}
	// 时序：fixedTime < fixedTime+1h
	if got[0].TaskRef != `feat/a` || got[1].TaskRef != `feat/b` {
		t.Errorf(`时序错乱：got=[%q,%q] want [feat/a,feat/b]`, got[0].TaskRef, got[1].TaskRef)
	}
	// 事实清单 JSON 往返不丢（判据/红先绿/评审/逃生都是可审计事实）。
	if got[1].AcceptanceTotal != 3 || got[1].AcceptancePass != 0 || got[1].RedFirstTrials != 2 ||
		got[1].ReviewRounds != 3 || got[1].Escapes != 2 {
		t.Errorf(`事实清单往返丢失：got=%+v`, got[1])
	}

	latest, err := Latest(root)
	if err != nil {
		t.Fatalf(`Latest: %v`, err)
	}
	if latest == nil || latest.TaskRef != `feat/b` {
		t.Errorf(`Latest=%+v want feat/b`, latest)
	}
}

func TestLoadAll_MalformedLineSkipped(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	// 手工写一行坏 JSON + 一行好的，验证坏行被跳过而非整体失败。
	path := root.ActConclusionsPath()
	if err := os.MkdirAll(root.ActDir(), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("{not json}\n" +
		`{"task_ref":"feat/ok","strength":"Strong","ratio":0.75,"deterministic":3,"agent_claim":1,"acceptance_pass":2,"acceptance_total":2,"completed_at":"2026-07-01T10:00:00Z","retrospective_nudge":false}` + "\n")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAll(root)
	if err != nil {
		t.Fatalf(`LoadAll should skip malformed, got err: %v`, err)
	}
	if len(got) != 1 || got[0].TaskRef != `feat/ok` {
		t.Errorf(`LoadAll=%+v want 仅 feat/ok（坏行跳过）`, got)
	}
	if got[0].AcceptancePass != 2 || got[0].AcceptanceTotal != 2 {
		t.Errorf(`存量行事实未读出：pass=%d/%d`, got[0].AcceptancePass, got[0].AcceptanceTotal)
	}
}

// TestLoadAll_LongLineSkipped: a line far exceeding the old Scanner 1MB cap (2MB
// junk) + one normal line.
//
// TestLoadAll_LongLineSkipped 单行远超旧 Scanner 1MB 上限（2MB 垃圾）+ 一行正常：旧
// bufio.Scanner 会 ErrTooLong 让整条聚合失败；改 bufio.Reader 后超大行被读入、Unmarshal
// 失败跳过，正常行照常返回——dashboard/health 不再因单行异常变 500。
func TestLoadAll_LongLineSkipped(t *testing.T) {
	root := forgedatatest.ForDataDir(t.TempDir())
	path := root.ActConclusionsPath()
	if err := os.MkdirAll(root.ActDir(), 0755); err != nil {
		t.Fatal(err)
	}
	garbage := make([]byte, 2*1024*1024)
	for i := range garbage {
		garbage[i] = 'x'
	}
	garbage = append(garbage, '\n')
	if err := os.WriteFile(path, garbage, 0644); err != nil {
		t.Fatal(err)
	}
	// 用 Append 追加正常结论（避免手工拼 JSON 双引号），CompletedAt 零值由 Append 补 now。
	if err := Append(root, &Conclusion{
		TaskRef: `feat/ok`, Strength: `Strong`, Ratio: 0.75,
		Deterministic: 3, AgentClaim: 1, AcceptancePass: 1, AcceptanceTotal: 1,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAll(root)
	if err != nil {
		t.Fatalf(`LoadAll 应跳过超大行而非报错，got err: %v`, err)
	}
	if len(got) != 1 || got[0].TaskRef != `feat/ok` {
		t.Errorf(`LoadAll=%+v want 仅 feat/ok（超大行跳过）`, got)
	}
}

func TestLoadAll_AbsentFileReturnsNil(t *testing.T) {
	got, err := LoadAll(forgedatatest.ForDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf(`缺失文件应 nil,nil，got err %v`, err)
	}
	if got != nil {
		t.Errorf(`缺失文件应 nil，got %+v`, got)
	}
	latest, err := Latest(forgedatatest.ForDataDir(t.TempDir()))
	if err != nil {
		t.Fatalf(`Latest 缺失文件应 nil,nil，got err %v`, err)
	}
	if latest != nil {
		t.Errorf(`Latest 缺失文件应 nil，got %+v`, latest)
	}
}

func TestBuildConclusion_DesignPhases(t *testing.T) {
	// 非空 phases → 原样写入 Conclusion.DesignPhases。
	phases := []string{`api`, `backend`}
	c := BuildConclusion(`feat/x`, `s1`, facts(1, 1, 0, 0, 1, 0), ec(3, 1), fixedTime, phases)
	if len(c.DesignPhases) != 2 || c.DesignPhases[0] != `api` || c.DesignPhases[1] != `backend` {
		t.Errorf(`DesignPhases=%v want [api backend]`, c.DesignPhases)
	}
	// nil phases → 空（零值）。
	c2 := BuildConclusion(`feat/y`, `s2`, facts(1, 1, 0, 0, 1, 0), ec(3, 1), fixedTime, nil)
	if len(c2.DesignPhases) != 0 {
		t.Errorf(`nil phases → DesignPhases=%v want 空`, c2.DesignPhases)
	}
}

func TestAppendLoadAll_DesignPhasesRoundTrip(t *testing.T) {
	// JSON 序列化往返：design_phases,omitempty 在非空时必须正确写读，不丢。
	root := forgedatatest.ForDataDir(t.TempDir())
	c := BuildConclusion(`feat/p`, `s1`, facts(2, 2, 1, 1, 1, 0), ec(3, 1), fixedTime,
		[]string{`requirement`, `api`})
	if err := Append(root, &c); err != nil {
		t.Fatalf(`Append: %v`, err)
	}
	got, err := LoadAll(root)
	if err != nil {
		t.Fatalf(`LoadAll: %v`, err)
	}
	if len(got) != 1 {
		t.Fatalf(`LoadAll len=%d want 1`, len(got))
	}
	if len(got[0].DesignPhases) != 2 ||
		got[0].DesignPhases[0] != `requirement` || got[0].DesignPhases[1] != `api` {
		t.Errorf(`DesignPhases round-trip 丢失：got=%v want [requirement api]`, got[0].DesignPhases)
	}
}

// TestEscapeCapped_PinsChecklogStrength 守护 EscapeCapped 的推导定理：
// Weak && Ratio >= strongRatioFloor ⟺ 该 Weak 来自逃生舱降档。对每组
// EvidenceChain 实跑 checklog.Strength() 构造出真实 Conclusion，再断言
// EscapeCapped() 与「无逃生舱同证据会评 Strong」逐例一致——checklog 改
// Strong 阈值或降档规则时本测试红，强制重新推导 strongRatioFloor 镜像，
// 绝不静默漂移（2026-09 告警分级引入）。
func TestEscapeCapped_PinsChecklogStrength(t *testing.T) {
	cases := []struct {
		name string
		ec   checklog.EvidenceChain
	}{
		{`降档 Weak：逃生舱+ratio 0.8+det<20`, checklog.EvidenceChain{Deterministic: 4, AgentClaim: 1, UsedEscapeHatch: true}},
		{`降档沿：逃生舱+ratio 恰 0.5`, checklog.EvidenceChain{Deterministic: 2, AgentClaim: 2, UsedEscapeHatch: true}},
		{`豁免：逃生舱+压倒性证据不降档`, checklog.EvidenceChain{Deterministic: 100, AgentClaim: 2, UsedEscapeHatch: true}},
		{`真弱：ratio 0.25 无逃生舱`, checklog.EvidenceChain{Deterministic: 1, AgentClaim: 3}},
		{`真强：ratio 0.75 无逃生舱`, checklog.EvidenceChain{Deterministic: 3, AgentClaim: 1}},
		{`Unverified：零 deterministic`, checklog.EvidenceChain{Deterministic: 0, AgentClaim: 2}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capped := BuildConclusion(`feat/x`, `s`, facts(1, 1, 0, 0, 1, 0), tc.ec, fixedTime, nil)
			// 定理：EscapeCapped ⟺ 实际 Weak 且无逃生舱的同证据链会评 Strong
			//（豁免档未降档、实际 Strong，不在定理内——合取的第二项必须含「实际 Weak」）。
			noHatch := tc.ec
			noHatch.UsedEscapeHatch = false
			want := capped.Strength == checklog.Weak.String() && noHatch.Strength() == checklog.Strong
			derived := capped.Strength == checklog.Weak.String() && capped.Ratio >= strongRatioFloor
			if derived != capped.EscapeCapped() {
				t.Errorf(`EscapeCapped=%v 与推导式不一致（Strength=%s Ratio=%.2f）`, capped.EscapeCapped(), capped.Strength, capped.Ratio)
			}
			if capped.EscapeCapped() != want {
				t.Errorf(`EscapeCapped=%v 但「实际 Weak 且无逃生舱同证据评 %s」=%v（定理断裂：checklog 规则变了？强制重推导）`,
					capped.EscapeCapped(), noHatch.Strength(), want)
			}
		})
	}
}

// TestDirective_ContainsRetroDoneInstruction 钉住 Directive 的收尾落章契约（2026-09
// 证据式 ack）：nudge 触发的指令必须带 `forge act retro-done` 完整调用形态（含本任务
// ref 与载体枚举）——具体命令放 forge 侧生成面而非 canonical skill（§13 零反向依赖），
// 这里就是它唯一的机器钉。干净完成（非 nudge）仍静默。
func TestDirective_ContainsRetroDoneInstruction(t *testing.T) {
	nudged := BuildConclusion(`feat/ack-pin`, `s1`, facts(3, 1, 1, 1, 1, 0), ec(1, 3), fixedTime, nil)
	d := nudged.Directive()
	if !strings.Contains(d, `session-retrospective`) {
		t.Errorf(`Directive 缺回顾入口，got: %s`, d)
	}
	if !strings.Contains(d, `memory|skill|claudemd|code|hook|ci|none`) {
		t.Errorf(`Directive 缺载体枚举（与 Carriers 单一真相源一致），got: %s`, d)
	}
	if !strings.Contains(d, `没有 ack 命令`) || !strings.Contains(d, `留在面板`) {
		t.Errorf(`Directive 须如实说明本版没有 ack 命令、nudge 会留在面板，got: %s`, d)
	}
	clean := BuildConclusion(`feat/clean`, `s1`, facts(5, 5, 1, 1, 1, 0), ec(3, 1), fixedTime, nil)
	if clean.Directive() != `` {
		t.Errorf(`干净完成应静默，got: %s`, clean.Directive())
	}
}
