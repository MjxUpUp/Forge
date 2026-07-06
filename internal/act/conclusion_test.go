package act

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/checklog"
	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
	"github.com/MjxUpUp/Forge/internal/scoringtypes"
)

// fixedTime 给测试一个稳定的完成时刻（不用 time.Now，避免时序断言飘）。
var fixedTime = time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)

func ec(det, agent int) checklog.EvidenceChain {
	return checklog.EvidenceChain{Deterministic: det, AgentClaim: agent}
}

func score(overall float64, grade string, dims ...scoringtypes.DimensionScore) *scoringtypes.ScoreResult {
	return &scoringtypes.ScoreResult{Overall: overall, Grade: grade, Dimensions: dims}
}

func dim(name scoringtypes.Dimension, sc int) scoringtypes.DimensionScore {
	return scoringtypes.DimensionScore{Dimension: name, Score: sc}
}

func TestBuildConclusion_NudgeByEvidenceStrength(t *testing.T) {
	// 核心契约：RetrospectiveNudge 按证据强度（非仅分数）触发。每个 case 用该档专属
	// 关键词断言，防分档回归被掩盖（一档断言短语漏进另一档时立即暴露）。
	tests := []struct {
		name      string
		ec        checklog.EvidenceChain
		score     *scoringtypes.ScoreResult
		wantNudge bool
		strength  string
	}{
		{
			name:      `NoData+高分→不nudge（无证据中性，与Strength语义一致）`,
			ec:        ec(0, 0),
			score:     score(90, `A`),
			wantNudge: false,
			strength:  `NoData`,
		},
		{
			// ★ 盲区命门：高分但零 deterministic 证据——agent 自述完成、没真跑验证
			name:      `Unverified+高分→nudge（高分但零实跑证据，对冲LLM-judge盲区）`,
			ec:        ec(0, 2),
			score:     score(95, `A`),
			wantNudge: true,
			strength:  `Unverified`,
		},
		{
			name:      `Weak→nudge（deterministic<agent-claim，声明主要靠自述）`,
			ec:        ec(1, 3), // ratio=0.25
			score:     score(88, `A`),
			wantNudge: true,
			strength:  `Weak`,
		},
		{
			name:      `Strong+高分→不nudge（干净完成，无教训可沉淀）`,
			ec:        ec(3, 1), // ratio=0.75
			score:     score(92, `A`),
			wantNudge: false,
			strength:  `Strong`,
		},
		{
			name:      `Strong+低分→nudge（证据足但分低，仍有教训）`,
			ec:        ec(3, 1),
			score:     score(65, `D`),
			wantNudge: true,
			strength:  `Strong`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := BuildConclusion(`feat/x`, `sess-1`, tt.score, tt.ec, 0, 0, fixedTime, nil)
			if c.Strength != tt.strength {
				t.Errorf(`Strength=%q want %q（分档回归？检查 Strength 计算）`, c.Strength, tt.strength)
			}
			if c.RetrospectiveNudge != tt.wantNudge {
				t.Errorf(`RetrospectiveNudge=%v want %v（强度档→nudge 映射断链）`, c.RetrospectiveNudge, tt.wantNudge)
			}
			if c.Score != tt.score.Overall {
				t.Errorf(`Score=%v want %v`, c.Score, tt.score.Overall)
			}
			if c.Grade != tt.score.Grade {
				t.Errorf(`Grade=%q want %q`, c.Grade, tt.score.Grade)
			}
		})
	}
}

func TestBuildConclusion_LowDimensionsCaptured(t *testing.T) {
	// 只有 <70 的维度进 LowDimensions；>=70 的不进。
	c := BuildConclusion(`feat/x`, ``, score(70, `C`,
		dim(`completeness`, 60),
		dim(`tests`, 95),
		dim(`design`, 69),
	), ec(0, 0), 0, 0, fixedTime, nil)
	want := []string{`completeness`, `design`}
	if len(c.LowDimensions) != len(want) {
		t.Fatalf(`LowDimensions=%v want %v`, c.LowDimensions, want)
	}
	for i, d := range want {
		if c.LowDimensions[i] != d {
			t.Errorf(`LowDimensions[%d]=%q want %q`, i, c.LowDimensions[i], d)
		}
	}
}

func TestBuildConclusion_NilScore(t *testing.T) {
	// 未评分（score=nil）：Score=0、Grade 空、LowDimensions 空；nudge 仅由强度决定。
	c := BuildConclusion(`feat/x`, ``, nil, ec(0, 2), 1, 3, fixedTime, nil)
	if c.Score != 0 {
		t.Errorf(`Score=%v want 0（nil score 应得 0）`, c.Score)
	}
	if c.Grade != `` {
		t.Errorf(`Grade=%q want 空（nil score 无 grade）`, c.Grade)
	}
	if len(c.LowDimensions) != 0 {
		t.Errorf(`LowDimensions=%v want 空（nil score 无维度）`, c.LowDimensions)
	}
	if !c.RetrospectiveNudge {
		t.Error(`RetrospectiveNudge=false want true（Unverified 应触发）`)
	}
	if c.AcceptancePass != 1 || c.AcceptanceTotal != 3 {
		t.Errorf(`Acceptance=%d/%d want 1/3`, c.AcceptancePass, c.AcceptanceTotal)
	}
}

func TestDirective(t *testing.T) {
	// Strong+>=70→空（静默，无噪声）；其余→含 session-retrospective 锚点。
	cases := []struct {
		name    string
		c       Conclusion
		wantSub string // 期望子串；空串表示期望 Directive 返回空
	}{
		{
			name:    `Strong高分→空（不发噪声）`,
			c:       Conclusion{Strength: `Strong`, Ratio: 0.75, Score: 92, Grade: `A`},
			wantSub: ``,
		},
		{
			name:    `Unverified→带证据理由`,
			c:       Conclusion{Strength: `Unverified`, Ratio: 0, Deterministic: 0, AgentClaim: 2, Score: 95, Grade: `A`, RetrospectiveNudge: true},
			wantSub: `deterministic 证据不足`,
		},
		{
			name:    `低分→带评分理由`,
			c:       Conclusion{Strength: `Strong`, Ratio: 0.75, Score: 65, Grade: `D`, RetrospectiveNudge: true, LowDimensions: []string{`completeness`}},
			wantSub: `低分维度：completeness`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.c.Directive()
			if tc.wantSub == `` {
				if got != `` {
					t.Errorf(`Strong+高分应静默，got %q`, got)
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

	c1 := BuildConclusion(`feat/a`, `s1`, score(95, `A`), ec(3, 1), 2, 2, fixedTime, nil)
	c2 := BuildConclusion(`feat/b`, `s2`, score(60, `D`), ec(0, 2), 0, 3, fixedTime.Add(time.Hour), nil)

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
		t.Errorf(`时序错乱：got[%q,%q] want [feat/a,feat/b]`, got[0].TaskRef, got[1].TaskRef)
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
		`{"task_ref":"feat/ok","score":80,"strength":"Strong","ratio":0.75,"deterministic":3,"agent_claim":1,"completed_at":"2026-07-01T10:00:00Z","retrospective_nudge":false}` + "\n")
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
}

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
		TaskRef: `feat/ok`, Score: 80, Strength: `Strong`, Ratio: 0.75,
		Deterministic: 3, AgentClaim: 1,
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
