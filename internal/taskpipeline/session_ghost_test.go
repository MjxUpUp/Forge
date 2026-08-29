package taskpipeline

import "testing"

// TestHasSession_IgnoresImportedGhosts 是跨机器 import 的核心不变量：导入的幽灵链接
// （SessionLink.Imported=true，从另一台机器带入）绝不能算作本机锚点。HasSession（attach 判定谓词）
// 对幽灵 sid 返 false 使 attach 永不被误跳过；HasAnySession（完整溯源谓词）仍看到它使重复 import
// 能去重。幽灵语义见 SessionLink.Imported。
func TestHasSession_IgnoresImportedGhosts(t *testing.T) {
	s := &TaskState{TaskRef: `feat/x`, SessionLinks: []SessionLink{
		{SessionID: `ghost-sid`, Tool: `kimi`, Imported: true},
	}}
	if s.HasSession(`ghost-sid`) {
		t.Error(`幽灵 session 不应被 HasSession 视为本机锚点（否则 attach 被误跳过）`)
	}
	if !s.HasAnySession(`ghost-sid`) {
		t.Error(`HasAnySession 应看到幽灵（完整溯源 / 重复 import 去重需要它）`)
	}
}

// TestHasSession_LocalLinkStillSeen：本机链接（Imported=false）被 HasSession 看到；同一 task 混合
// 本机 + 幽灵不会致盲本机那条。幽灵被忽略、本机锚点成立——正是 attach 路径所期望的。
func TestHasSession_LocalLinkStillSeen(t *testing.T) {
	s := &TaskState{TaskRef: `feat/x`, SessionLinks: []SessionLink{
		{SessionID: `local-sid`, Tool: `claude-code`},
		{SessionID: `ghost-sid`, Tool: `kimi`, Imported: true},
	}}
	if !s.HasSession(`local-sid`) {
		t.Error(`本机链接应被 HasSession 看到`)
	}
	if s.HasSession(`ghost-sid`) {
		t.Error(`幽灵链接不应被 HasSession 看到`)
	}
}

// TestAddSession_DoesNotDedupAgainstGhost 是「击败幽灵」回归：task 有一个导入幽灵 sid=X——当本机
// session 也恰好是 sid X（跨机器碰撞）时——AddSession 仍须记下一条全新本机链接，而非把它当幽灵的
// 重复吞掉。否则碰撞下本机 session 被静默取消锚定（HasSession 假、链接没加），恰在最该锚定处破坏
// 多向锚定。
func TestAddSession_DoesNotDedupAgainstGhost(t *testing.T) {
	s := &TaskState{TaskRef: `feat/x`, SessionLinks: []SessionLink{
		{SessionID: `colliding-sid`, Tool: `kimi`, Imported: true},
	}}
	s.AddSession(`colliding-sid`, `claude-code`)
	// The local link must now exist and be visible to HasSession (the ghost did not swallow it).
	if !s.HasSession(`colliding-sid`) {
		t.Fatal(`碰撞 sid 下 AddSession 应记下本机链接，HasSession 须看到它（不被幽灵吞）`)
	}
	count := 0
	for _, l := range s.SessionLinks {
		if l.SessionID == `colliding-sid` {
			count++
		}
	}
	if count != 2 {
		t.Fatalf(`应有两个同 sid 链接（一幽灵一本机），got %d`, count)
	}
}

// TestAddSession_LocalDedupStillWorks：同 sid 的两次本机 AddSession 折叠成一条本机链接——原去重契约
// 对非幽灵链接保留（幽灵改动只是放松去重以忽略导入链接，不削弱本机去重）。
func TestAddSession_LocalDedupStillWorks(t *testing.T) {
	s := &TaskState{TaskRef: `feat/x`}
	s.AddSession(`same-sid`, `claude-code`)
	s.AddSession(`same-sid`, `claude-code`)
	if len(s.SessionLinks) != 1 {
		t.Fatalf(`同 sid 本机 AddSession 应去重为 1 条，got %d`, len(s.SessionLinks))
	}
}
