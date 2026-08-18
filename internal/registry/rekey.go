package registry

import (
	"encoding/json"
	"fmt"
	"os"
)

// Rekey synchronizes registry entries after a data-dir rekey (forge registry rekey):
// entries whose project key equals fromKey are removed; if no entry carries toKey,
// the first removed entry is re-keyed to toKey (same directory, new identity) so the
// project does not lose membership. The to-side entry, when present, is kept as-is
// (its path is the canonical registration).
//
// Returns removed = number of fromKey entries dropped. A missing registry file is not
// an error (0, nil) — same contract as List; a CORRUPT registry IS an explicit error
// (unlike List): silently skipping the sync here would leave the from-key entry
// registered while the caller reports success, re-splitting the dashboard view
// (code-review finding #3). fromKey == toKey is a no-op guard (code-review #5).
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
	p, err := globalPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, nil // 无注册表文件：无事可做
	}
	var f File
	if uerr := json.Unmarshal(data, &f); uerr != nil {
		return 0, fmt.Errorf(`registry: 注册表 JSON 损坏，rekey 同步中止（数据合并不受影响）: %w`, uerr)
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
		return 0, nil
	}
	if !hasTo && toKey != `` {
		// No to-side entry: re-key the first dropped entry so membership survives.
		//
		// 无 to 侧条目：把第一条被移除条目改 key，保住成员资格。
		kept = append(kept, Entry{Path: dropped[0].Path, Key: toKey})
	}
	if werr := writeEntries(kept); werr != nil {
		return 0, werr
	}
	return len(dropped), nil
}
