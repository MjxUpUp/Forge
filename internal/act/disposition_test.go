package act

import (
	"testing"
	"time"

	"github.com/MjxUpUp/Forge/internal/forgedata/forgedatatest"
)

// TestDispositionRoundTrip 钉住 dispositions.jsonl 的 append-only 往返契约：
// AppendDisposition 写、LoadDispositions 读回逐字段一致；文件不存在时返回
// nil（合法空状态，与 conclusions LoadAll 同语义）。
func TestDispositionRoundTrip(t *testing.T) {
	_, p := forgedatatest.RealProject(t)

	// 空状态：文件不存在 → nil, nil（不是错误）。
	if ds, err := LoadDispositions(p); err != nil || ds != nil {
		t.Fatalf(`空状态应 (nil, nil)，got (%v, %v)`, ds, err)
	}

	doneAt := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	first := Disposition{
		TaskRef:     `feat/a`,
		SessionID:   `sess-1`,
		CompletedAt: doneAt,
		Carrier:     `skill`,
		Lesson:      `删 skill 源后核查全部 agent 目标`,
		RecordedAt:  doneAt.Add(time.Hour),
	}
	if err := AppendDisposition(p, &first); err != nil {
		t.Fatalf(`AppendDisposition: %v`, err)
	}
	second := Disposition{
		TaskRef:     `feat/b`,
		CompletedAt: doneAt.Add(2 * time.Hour),
		Carrier:     `none`,
		RecordedAt:  doneAt.Add(3 * time.Hour),
	}
	if err := AppendDisposition(p, &second); err != nil {
		t.Fatalf(`AppendDisposition: %v`, err)
	}

	ds, err := LoadDispositions(p)
	if err != nil {
		t.Fatalf(`LoadDispositions: %v`, err)
	}
	if len(ds) != 2 {
		t.Fatalf(`len=%d want 2（append-only 保序累积）`, len(ds))
	}
	if ds[0].TaskRef != `feat/a` || ds[0].SessionID != `sess-1` || ds[0].Carrier != `skill` ||
		ds[0].Lesson != `删 skill 源后核查全部 agent 目标` {
		t.Errorf(`首条往返失真：got %+v`, ds[0])
	}
	if !ds[0].CompletedAt.Equal(doneAt) || !ds[0].RecordedAt.Equal(doneAt.Add(time.Hour)) {
		t.Errorf(`时间往返失真：completed=%v recorded=%v`, ds[0].CompletedAt, ds[0].RecordedAt)
	}
	if ds[1].TaskRef != `feat/b` || ds[1].Carrier != `none` {
		t.Errorf(`次条往返失真：got %+v`, ds[1])
	}
	// 纳秒精度钉住：identity 靠 CompletedAt.Equal 匹配，RFC3339Nano 的
	// marshal∘unmarshal 必须幂等（尾零裁剪稳定），否则往返后 AckMatcher 失配。
	nano := time.Date(2026, 9, 9, 10, 0, 0, 123456789, time.UTC)
	nanoD := Disposition{TaskRef: `feat/n`, CompletedAt: nano, Carrier: `none`, RecordedAt: nano}
	if err := AppendDisposition(p, &nanoD); err != nil {
		t.Fatalf(`AppendDisposition(nano): %v`, err)
	}
	ds2, err := LoadDispositions(p)
	if err != nil {
		t.Fatalf(`LoadDispositions(nano): %v`, err)
	}
	found := false
	for _, d := range ds2 {
		if d.TaskRef == `feat/n` {
			found = true
			if !d.CompletedAt.Equal(nano) {
				t.Errorf(`纳秒往返失真：got %v want %v（Equal 失配会破坏 AckMatcher）`, d.CompletedAt, nano)
			}
		}
	}
	if !found {
		t.Error(`纳秒样本未读回`)
	}
}

// TestAckMatcherIdentity 钉住 ack 匹配的 identity 三元组语义：TaskRef + SessionID +
// CompletedAt 三者全同才命中——同 ref 不同 session（任务重做）不得误 ack 新结论。
func TestAckMatcherIdentity(t *testing.T) {
	doneAt := time.Date(2026, 9, 9, 10, 0, 0, 0, time.UTC)
	ds := []Disposition{{
		TaskRef:     `feat/a`,
		SessionID:   `sess-1`,
		CompletedAt: doneAt,
		Carrier:     `skill`,
	}}
	match := AckMatcher(ds)
	exact := Conclusion{TaskRef: `feat/a`, SessionID: `sess-1`, CompletedAt: doneAt}
	if !match(exact) {
		t.Error(`identity 三元组全同应命中`)
	}
	diffSession := exact
	diffSession.SessionID = `sess-2`
	if match(diffSession) {
		t.Error(`同 ref 不同 session 不得命中（任务重做的新结论）`)
	}
	diffTime := exact
	diffTime.CompletedAt = doneAt.Add(time.Minute)
	if match(diffTime) {
		t.Error(`同 ref 同 session 不同完成时刻不得命中`)
	}
	diffRef := exact
	diffRef.TaskRef = `feat/other`
	if match(diffRef) {
		t.Error(`不同 ref 不得命中`)
	}
	// 空 SessionID 的结论与 disposition（CLI 从无 session 的结论落 ack）双方都空 → 命中。
	noSess := Conclusion{TaskRef: `feat/a`, CompletedAt: doneAt}
	dsNoSess := []Disposition{{TaskRef: `feat/a`, CompletedAt: doneAt, Carrier: `none`}}
	if !AckMatcher(dsNoSess)(noSess) {
		t.Error(`双方 session 均空应命中（空=空）`)
	}
}

// TestIsValidCarrier 钉住载体白名单：合法载体过、未知值拒（防拼写漂移进数据）。
func TestIsValidCarrier(t *testing.T) {
	for _, c := range Carriers {
		if !IsValidCarrier(c) {
			t.Errorf(`%q 在 Carriers 内却未通过校验`, c)
		}
	}
	for _, bad := range []string{``, `Skill`, `doc`, `MEMORY`, `claudemd.md`} {
		if IsValidCarrier(bad) {
			t.Errorf(`%q 应被拒绝（大小写敏感、无别名）`, bad)
		}
	}
}
