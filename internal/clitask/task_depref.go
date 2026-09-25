package clitask

// 跨仓 --depends-on 校验（多仓 workspace Option B，
// docs/design/multi-repo-workspace.md）：key:ref 依赖的写入侧。
// `forge task start --depends-on <key:ref>` 在 AddDependency 落边前在此校验：
//
//   - key 前缀不是本 repo 所属任何 workspace 的成员 → 拒绝（该依赖会解析到
//     陌生/拼错的 DataDir 并把门禁死锁），提示先 `forge workspace add` 或去掉
//     key 前缀；
//   - `<本仓key>:<本ref>` 按自引用拒绝（AddDependency 的裸串检查看不穿 key
//     前缀）；
//   - workspace 清单不可读 → 降级 stderr advisory 并放行（fail-open——清单是
//     全局用户级 store，任何机器上都可能缺失/损坏；基建故障绝不阻断建任务）；
//   - 跨仓目标 task 不存在 → 容忍（与本仓行为一致：允许前向引用稍后创建的
//     任务；门禁把缺失计 pending），但给 stderr advisory——拼错的 key:ref 会
//     在 verify/complete 永久阻塞，而跨仓笔误比本仓更难察觉。

import (
	"fmt"
	"io"

	"github.com/MjxUpUp/Forge/internal/forgedata"
	"github.com/MjxUpUp/Forge/internal/taskpipeline"
	"github.com/MjxUpUp/Forge/internal/workspace"
)

// ownRepoKey 用与 cross-repo-impact 门禁、`workspace add` 相同的 fallback 推导
// 本 repo 的项目 key（forgedata.Key，否则 PathKey）——与 workspace 成员资格
// 比对的身份必须是清单里存的那个。
func ownRepoKey(root string) string {
	key, err := forgedata.Key(root)
	if err != nil {
		key = forgedata.PathKey(root)
	}
	return key
}

// unknownDepKeys 是纯成员资格核心：deps 里所有「包含 ownKey 的 workspace 都不
// 列为成员」的 key 前缀。刻意无 IO，让判定表无需清单 fixture 即可单测。
func unknownDepKeys(f *workspace.File, ownKey string, deps []string) []string {
	valid := map[string]bool{}
	for _, w := range f.WorkspacesFor(ownKey) {
		for _, r := range w.Repos {
			valid[r.Key] = true
		}
	}
	var bad []string
	seen := map[string]bool{}
	for _, d := range deps {
		key, _ := taskpipeline.SplitDepRef(d)
		if key == `` || valid[key] || seen[key] {
			continue
		}
		seen[key] = true
		bad = append(bad, key)
	}
	return bad
}

// validateDependsOnRefs 在 unknownDepKeys 外围接线：加载清单（出错 fail-open），
// 拒绝越界 key 与经本仓 key 的自依赖，并对当前不可读的跨仓目标发 advisory。
// advisory 走 stderr；返回非 nil error 表示这批依赖被拒绝。
func validateDependsOnRefs(root, selfRef string, deps []string, stderr io.Writer) error {
	hasCross := false
	for _, d := range deps {
		if key, _ := taskpipeline.SplitDepRef(d); key != `` {
			hasCross = true
			break
		}
	}
	if !hasCross {
		return nil // 纯本仓依赖：零行为变化（连清单都不读）
	}
	f, err := workspace.Load()
	if err != nil {
		fmt.Fprintf(stderr, "Warning: workspace 清单不可读（%v）——跨仓依赖成员资格未校验（fail-open 放行，forge workspace doctor 可复检）\n", err)
		return nil
	}
	ownKey := ownRepoKey(root)
	if bad := unknownDepKeys(f, ownKey, deps); len(bad) > 0 {
		return fmt.Errorf("依赖的 key 前缀 %v 不是本 repo（key %s）所属任何 workspace 的成员——先 forge workspace add 把目标 repo 加入同一 workspace，或去掉 key 前缀改用本仓 ref", bad, ownKey)
	}
	for _, d := range deps {
		key, taskRef := taskpipeline.SplitDepRef(d)
		if key == `` {
			continue
		}
		if key == ownKey && taskRef == selfRef {
			return fmt.Errorf(`dependency cycle: %s 不能依赖自身（%q 经本仓 key 前缀指回自己）`, selfRef, d)
		}
		if _, err := taskpipeline.LoadDepState(root, d); err != nil {
			fmt.Fprintf(stderr, "Warning: 跨仓依赖 %q 的目标 task 当前不可读（%v）——前向引用（目标稍后创建）可忽略；若是笔误，verify/complete 门禁会一直把它计为 pending\n", d, err)
		}
	}
	return nil
}

// multiRepoMembership 报告本 repo 是否属于至少一个多仓 workspace——跨仓提示的
// 廉价闸门（单仓用户永远看不到跨仓提示）。fail-open：任何清单故障按「无成员
// 资格」处理（提示是善意提醒，不是检查）。
func multiRepoMembership(root string) bool {
	f, err := workspace.Load()
	if err != nil {
		return false
	}
	for _, w := range f.WorkspacesFor(ownRepoKey(root)) {
		if len(w.Repos) >= 2 {
			return true
		}
	}
	return false
}
