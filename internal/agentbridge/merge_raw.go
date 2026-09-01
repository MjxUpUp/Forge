package agentbridge

import (
	"encoding/json"

	"github.com/MjxUpUp/Forge/internal/hooks"
)

// merge_raw.go —— codex/cursor/windsurf 三个 translator 共享的 raw-JSON 合并
// helper。
//
// 为什么用 raw：merge 契约是"用户自定义 hook 条目原样保留"。类型化 struct 往返
// （unmarshal 进 cursorHookEntry / windsurfHookEntry / hooks.HookMatcher 再
// marshal）会静默丢弃 struct 未声明的字段——codex 用户条目可能带
// timeout/commandWindows，windsurf 条目可能带 powershell/working_directory 等。
// 在 forge 条目重生成的同事丢这些字段，正是 merge 契约要防的"merge 吃掉用户
// 配置"那类 bug。故保留的条目始终是 json.RawMessage：只读 command 字段
// （判 forge 还是用户），只有 forge 来源条目会被丢弃或重生成。

// hookEntryCommand 提取 raw hook 条目对象的 "command" 字段。解析失败的条目
// 得到 ""——hooks.IsForgeHookCommand 会把它当用户自定义，故损坏的用户内容被保留，
// 绝不误删。
func hookEntryCommand(raw json.RawMessage) string {
	var e struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return ""
	}
	return e.Command
}

// dropForgeEntries 从 raw 条目列表中滤掉 forge 来源条目。保留的条目维持
// 原始字节。removed 报告是否有条目被丢弃。
func dropForgeEntries(entries []json.RawMessage) (kept []json.RawMessage, removed bool) {
	for _, raw := range entries {
		if hooks.IsForgeHookCommand(hookEntryCommand(raw)) {
			removed = true
			continue
		}
		kept = append(kept, raw)
	}
	return kept, removed
}

// stripForgeMatchersRaw 处理嵌套 hooks 形态（claude/codex）：
// {event: [ {matcher, hooks:[{type,command}]} ]}。forge 来源的内层条目被移除；
// 被掏空的 matcher/event 一并丢弃；所有用户自定义内容保留原始字节。
// removedAny 报告是否有改动。
func stripForgeMatchersRaw(spec map[string][]json.RawMessage) (kept map[string][]json.RawMessage, removedAny bool) {
	kept = make(map[string][]json.RawMessage, len(spec))
	for event, matchers := range spec {
		var keptMatchers []json.RawMessage
		for _, rawMatcher := range matchers {
			var probe struct {
				Hooks []json.RawMessage `json:"hooks"`
			}
			if err := json.Unmarshal(rawMatcher, &probe); err != nil || probe.Hooks == nil {
				keptMatchers = append(keptMatchers, rawMatcher) // user content, keep as-is
				continue
			}
			keptEntries, removed := dropForgeEntries(probe.Hooks)
			if !removed {
				keptMatchers = append(keptMatchers, rawMatcher)
				continue
			}
			removedAny = true
			if len(keptEntries) == 0 {
				continue // matcher held only forge entries — drop it
			}
			// Mixed matcher: rebuild it, preserving the matcher's other fields
			// (matcher name, unknown fields) and the kept entries' raw bytes.
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(rawMatcher, &obj); err != nil {
				continue
			}
			entriesJSON, err := json.Marshal(keptEntries)
			if err != nil {
				continue
			}
			obj["hooks"] = entriesJSON
			rebuilt, err := json.Marshal(obj)
			if err != nil {
				continue
			}
			keptMatchers = append(keptMatchers, rebuilt)
		}
		if len(keptMatchers) > 0 {
			kept[event] = keptMatchers
		}
	}
	return kept, removedAny
}

// stripForgeFlatEntriesRaw 处理扁平 hooks 形态（cursor/windsurf）：
// {event: [ {command, ...} ]}。forge 来源条目被移除；被掏空的 event 一并丢弃；
// 用户条目保留原始字节。removedAny 报告是否有改动。
func stripForgeFlatEntriesRaw(flat map[string][]json.RawMessage) (kept map[string][]json.RawMessage, removedAny bool) {
	kept = make(map[string][]json.RawMessage, len(flat))
	for event, entries := range flat {
		keptEntries, removed := dropForgeEntries(entries)
		if removed {
			removedAny = true
		}
		if len(keptEntries) > 0 {
			kept[event] = keptEntries
		}
	}
	return kept, removedAny
}

// rawHooksSection 把生成的 hooks payload（build*Hooks map 里 "hooks" 键的值）
// marshal 成按 event 分组的 raw 条目列表，供与 raw 保留的用户条目合并。生成
// 条目没有未知字段，其 raw 形态与类型化生成器的输出完全一致。
func rawHooksSection(generated any) (map[string][]json.RawMessage, error) {
	raw, err := json.Marshal(generated)
	if err != nil {
		return nil, err
	}
	var spec map[string][]json.RawMessage
	if err := json.Unmarshal(raw, &spec); err != nil {
		return nil, err
	}
	return spec, nil
}
