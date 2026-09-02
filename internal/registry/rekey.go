package registry

import (
	"encoding/json"
	"fmt"
	"os"
)

// Rekey synchronizes registry entries after a data-dir rekey (forge registry
// rekey): entries whose project key equals fromKey are removed.
//
// Rekey 在数据目录 rekey（forge registry rekey）后同步注册表条目：key 等于
// fromKey 的条目被移除；若没有任何条目带 toKey，把第一条被移除的条目改 key 为
// toKey（同一目录、新身份）让项目不失成员资格。to 侧条目存在时原样保留（其路径
// 即 canonical 登记）。
//
// 返回 removed = 被移除的 fromKey 条目数。注册表文件缺失不算错误（0, nil）——
// 与 List 同一契约；但注册表损坏是显式错误（与 List 不同）：此处静默跳过同步会
// 让 from key 条目残留而调用方报告成功，看板重新双列（code-review 发现 #3）。
// fromKey == toKey 是 no-op 守卫（code-review #5）。
func Rekey(fromKey, toKey string) (removed int, err error) {
	if fromKey == toKey {
		return 0, nil // 同 key：无可同步
	}
	// 读-改-写整体入锁（P2-P4 收口）：只锁写回段时读快照可被并发写者更新，
	// 写回整文件覆盖丢条目（MAJOR 复查发现）。removed 用命名返回值（闭包内赋值）。
	if err := withLock(func() error {
		p, gerr := globalPath()
		if gerr != nil {
			return gerr
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil // 无注册表文件：无事可做
		}
		var f File
		if uerr := json.Unmarshal(data, &f); uerr != nil {
			return fmt.Errorf(`registry: 注册表 JSON 损坏，rekey 同步中止（数据合并不受影响）: %w`, uerr)
		}
		var kept []Entry
		var dropped []Entry
		hasTo := false
		for _, e := range f.Projects {
			k := keyOf(e)
			switch {
			case fromKey != `` && k == fromKey:
				dropped = append(dropped, e)
			default:
				if toKey != `` && k == toKey {
					hasTo = true
				}
				kept = append(kept, e)
			}
		}
		if len(dropped) == 0 {
			return nil
		}
		if !hasTo && toKey != `` {
			// 无 to 侧条目：把第一条被移除条目改 key，保住成员资格。
			// 整条目迁移（保留 Status/决策字段）：重建 Entry{Path,Key} 会丢 declined 状态，
			// rekey 后项目被静默复活接管（Project Policy Layer P1，对抗复查 M7）。
			e := dropped[0]
			e.Key = toKey
			kept = append(kept, e)
		}
		if werr := writeEntries(kept); werr != nil {
			return werr
		}
		removed = len(dropped)
		return nil
	}); err != nil {
		return 0, err
	}
	return removed, nil
}
