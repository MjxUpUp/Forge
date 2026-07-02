#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Claude / Claude Code 会话劣化诊断工具。

解析会话 jsonl transcript，量化输出 5 个维度的劣化信号：
  1. 规模与时间跨度
  2. 自动压缩 (auto-compact) 次数 / 分布 / 膨胀速率
  3. 上下文噪声注入 (attachment 回执占比)
  4. 工具调用循环 (连续重复 = 探查退化)
  5. 人类输入主线 (供 agent 判断主题漂移)

用法:
  python session-diagnostics.py <session-id>
  python session-diagnostics.py <path/to/session.jsonl>
  python session-diagnostics.py <session-id> --out report.md

输出 UTF-8 Markdown。Windows 下会强制 stdout 为 UTF-8，避免 GBK 乱码。
"""
import sys
import os
import re
import glob
import json
import argparse
import datetime
from collections import Counter

try:
    sys.stdout.reconfigure(encoding="utf-8")  # type: ignore[attr-defined]
except Exception:
    pass


# ---- 劣化信号阈值（来自实战标定，超阈值即高风险）-------------------------
THRESH = {
    "compact_count": 10,        # 自动压缩次数 >= 10 → 高风险
    "compact_min_gap_min": 15,  # 最短压缩间隔 < 15 分钟 → 噪声失控
    "noise_attachment_ratio": 0.5,  # hook_success 等回执占 attachment > 50% → 过载
    "tool_loop_runs": 20,       # 连续>=4次同工具的 run 数 >= 20 → 探查退化
    "human_topic_mainlines": 4, # 人类输入跨 >= 4 个不相关主线 → 会话过载
}

# 视为"零信息噪声回执"的 attachment 内嵌类型
NOISE_TYPES = {
    "hook_success", "hook_non_blocking_error",
    "task_reminder", "goal_status", "task_status",
    "command_permissions", "date_change",
}

ATT_TYPE_RE = re.compile(r"'type':\s*'([^']+)'")


def find_jsonl(target):
    """session id 或 jsonl 路径 → 实际 jsonl 路径。找不到返回 None。"""
    target = os.path.expanduser(target)
    if os.path.isfile(target):
        return target
    # 当作 session id，在 ~/.claude/projects/*/<id>.jsonl 里找
    home = os.path.expanduser("~")
    patterns = [
        os.path.join(home, ".claude", "projects", "*", target + ".jsonl"),
        os.path.join(home, ".claude", "projects", "*", target),
    ]
    for pat in patterns:
        hits = glob.glob(pat)
        if hits:
            return hits[0]
    # 兜底：在 .claude 下全盘搜文件名匹配
    for hit in glob.glob(os.path.join(home, ".claude", "projects", "**", "*" + target + "*"), recursive=True):
        if hit.endswith(".jsonl"):
            return hit
    return None


def load_records(path):
    recs = []
    with open(path, encoding="utf-8") as fp:
        for i, line in enumerate(fp):
            if not line.strip():
                continue
            try:
                o = json.loads(line)
            except Exception:
                continue
            o["_lineno"] = i + 1
            recs.append(o)
    return recs


def dt(s):
    if not s:
        return None
    try:
        return datetime.datetime.fromisoformat(s.replace("Z", "+00:00"))
    except Exception:
        return None


def is_true(v):
    # jsonl 里 isSidechain 经常写成字符串 "False"
    if isinstance(v, str):
        return v.strip().lower() == "true"
    return bool(v)


def extract_user_text(msg):
    """从 user message 提取纯文本，返回 (text, is_tool_result)。"""
    content = msg.get("content")
    if isinstance(content, str):
        return content, False
    if isinstance(content, list):
        text = None
        is_tr = False
        for blk in content:
            if not isinstance(blk, dict):
                continue
            if blk.get("type") == "tool_result":
                return None, True
            if blk.get("type") == "text" and text is None:
                text = blk.get("text")
        return text, False
    return None, False


def analyze_compaction(user_msgs):
    """user_msgs: list of (lineno, timestamp, text)。返回压缩事件列表。"""
    compacts = []
    for ln, ts, text in user_msgs:
        if text and text.startswith("This session is being continued"):
            compacts.append((ln, ts, len(text)))
    return compacts


def analyze_attachments(recs):
    """统计 attachment 内嵌类型。"""
    c = Counter()
    sz = Counter()
    for o in recs:
        if o.get("type") != "attachment":
            continue
        a = o.get("attachment", "")
        if isinstance(a, dict):
            t = a.get("type", "?")
        else:
            m = ATT_TYPE_RE.search(str(a))
            t = m.group(1) if m else "__notype__"
        c[t] += 1
        sz[t] += len(str(a))
    return c, sz


def analyze_toolcalls(recs):
    """返回 (list_of_(lineno,name,ts), Counter(name->count))。"""
    calls = []
    for o in recs:
        if o.get("type") != "assistant":
            continue
        content = o.get("message", {}).get("content")
        if not isinstance(content, list):
            continue
        ts = o.get("timestamp", "")
        for blk in content:
            if isinstance(blk, dict) and blk.get("type") == "tool_use":
                calls.append((o["_lineno"], blk.get("name", "?"), ts))
    return calls, Counter(x[1] for x in calls)


def detect_loops(calls, min_run=4):
    """连续 >= min_run 次同工具 = 一次"卡住" run。返回 (run_count, Counter(name->run_count))。"""
    runs = []
    if not calls:
        return 0, Counter()
    cur = calls[0][1]
    cnt = 1
    for _, name, _ in calls[1:]:
        if name == cur:
            cnt += 1
        else:
            if cnt >= min_run:
                runs.append((cur, cnt))
            cur = name
            cnt = 1
    if cnt >= min_run:
        runs.append((cur, cnt))
    return len(runs), Counter(n for n, _ in runs)


def fmt_dur(sec):
    if sec < 60:
        return f"{sec:.0f}s"
    if sec < 3600:
        return f"{sec/60:.1f}min"
    return f"{sec/3600:.1f}h"


def flag(ok):
    return "✅" if ok else "🔴"


def build_report(path, recs):
    lines = []
    A = lines.append

    total_lines = max((r["_lineno"] for r in recs), default=0)
    size = os.path.getsize(path)
    type_c = Counter(r.get("type", "?") for r in recs)

    # 时间跨度
    ts_list = [dt(r.get("timestamp", "")) for r in recs if r.get("timestamp")]
    ts_list = [t for t in ts_list if t]
    span_sec = (max(ts_list) - min(ts_list)).total_seconds() if ts_list else 0

    A("# 会话诊断报告")
    A("")
    A(f"- **文件**：`{path}`")
    A(f"- **规模**：{total_lines} 行 / {size/1024:.0f} KB")
    A(f"- **时间跨度**：{fmt_dur(span_sec)}（{ts_list[0]:%Y-%m-%d %H:%M} → {ts_list[-1]:%Y-%m-%d %H:%M}）" if ts_list else "- **时间跨度**：—")
    A(f"- **消息类型**：assistant {type_c.get('assistant',0)} / user {type_c.get('user',0)} / attachment {type_c.get('attachment',0)}")
    A("")

    # 人类输入（去 meta/sidechain/tool_result/compaction）
    human = []
    compacts = []
    for o in recs:
        if o.get("type") != "user":
            continue
        if o.get("isMeta"):
            continue
        if is_true(o.get("isSidechain")):
            continue
        text, is_tr = extract_user_text(o.get("message", {}))
        if is_tr or not text or not text.strip():
            continue
        text = text.strip()
        if text.startswith("This session is being continued"):
            compacts.append((o["_lineno"], o.get("timestamp", ""), len(text)))
        else:
            human.append((o["_lineno"], o.get("timestamp", ""), len(text), text))

    # ---- 维度 2：自动压缩 ----
    A("## 维度 2 — 自动压缩 (auto-compact)")
    if not compacts:
        A("无压缩事件。")
    else:
        gaps = []
        for (la, ta, _), (lb, tb, _) in zip(compacts, compacts[1:]):
            da, db = dt(ta), dt(tb)
            if da and db:
                gaps.append((db - da).total_seconds())
        min_gap = min(gaps) if gaps else 0
        avg_gap = (sum(gaps) / len(gaps)) if gaps else 0
        A(f"- **压缩次数**：{len(compacts)}  {flag(len(compacts) < THRESH['compact_count'])}（红线 {THRESH['compact_count']}）")
        A(f"- **最短压缩间隔**：{fmt_dur(min_gap)}  {flag(min_gap >= THRESH['compact_min_gap_min']*60)}（红线 {THRESH['compact_min_gap_min']}min，越短越糟）")
        A(f"- **平均压缩间隔**：{fmt_dur(avg_gap)}")
        A("- **压缩时间序列**：")
        for i, (ln, ts, sz) in enumerate(compacts, 1):
            A(f"  - #{i:2d}  L{ln:6d}  {dt(ts):%Y-%m-%d %H:%M:%S}")
    A("")

    # ---- 维度 3：噪声注入 ----
    att_c, att_sz = analyze_attachments(recs)
    total_att = sum(att_c.values())
    noise_att = sum(att_c.get(t, 0) for t in NOISE_TYPES)
    noise_ratio = (noise_att / total_att) if total_att else 0
    A("## 维度 3 — 上下文噪声注入 (attachment 回执)")
    if total_att == 0:
        A("无 attachment。")
    else:
        A(f"- **attachment 总数**：{total_att}")
        A(f"- **零信息回执占比**：{noise_ratio:.0%}（{noise_att} 条） {flag(noise_ratio < THRESH['noise_attachment_ratio'])}（红线 {int(THRESH['noise_attachment_ratio']*100)}%）")
        A("- **按内嵌类型 Top**：")
        for t, n in att_c.most_common(8):
            mark = " ← 噪声" if t in NOISE_TYPES else ""
            A(f"  - {n:6d}  {t}{mark}")
    A("")

    # ---- 维度 4：工具调用循环 ----
    calls, tc = analyze_toolcalls(recs)
    loop_runs, loop_names = detect_loops(calls)
    A("## 维度 4 — 工具调用循环 (探查退化信号)")
    A(f"- **总工具调用**：{len(calls)}")
    A(f"- **连续>=4次同工具的 run 数**：{loop_runs}  {flag(loop_runs < THRESH['tool_loop_runs'])}（红线 {THRESH['tool_loop_runs']}）")
    if loop_names:
        A("- **卡住 run 的工具分布**：")
        for n, c in loop_names.most_common():
            A(f"  - {c} 次 run  工具={n}")
    A("- **工具调用总量分布 Top**：")
    for n, c in tc.most_common(8):
        A(f"  - {c:6d}  {n}")
    A("")

    # ---- 维度 5：人类输入主线 ----
    A("## 维度 5 — 人类输入主线（判断主题漂移）")
    A(f"- **真实人类输入条数**：{len(human)}（红线：跨 >= {THRESH['human_topic_mainlines']} 个不相关主线 = 会话过载）")
    A("- **逐条输入**（人工/agent 判断是否同一主线）：")
    for ln, ts, sz, text in human:
        t1 = text.replace("\n", " ")
        A(f"  - `[L{ln:6d}] {dt(ts):%m-%d %H:%M} ({sz:5d}c)` {t1[:140]}")
    A("")

    # ---- 总评 ----
    A("## 总评红线汇总")
    flags = []
    if compacts and len(compacts) >= THRESH["compact_count"]:
        flags.append(f"压缩 {len(compacts)} 次 (>= {THRESH['compact_count']})")
    if compacts:
        gaps2 = []
        for (la, ta, _), (lb, tb, _) in zip(compacts, compacts[1:]):
            da, db = dt(ta), dt(tb)
            if da and db:
                gaps2.append((db - da).total_seconds())
        if gaps2 and min(gaps2) < THRESH["compact_min_gap_min"] * 60:
            flags.append(f"最短压缩间隔 {fmt_dur(min(gaps2))} (< {THRESH['compact_min_gap_min']}min)")
    if noise_ratio >= THRESH["noise_attachment_ratio"]:
        flags.append(f"噪声回执占比 {noise_ratio:.0%} (>= {int(THRESH['noise_attachment_ratio']*100)}%)")
    if loop_runs >= THRESH["tool_loop_runs"]:
        flags.append(f"工具循环 run {loop_runs} 次 (>= {THRESH['tool_loop_runs']})")
    if flags:
        A("🔴 命中红线：")
        for f in flags:
            A(f"  - {f}")
        A("")
        A("命中红线越多，会话越接近不可逆劣化。建议见 SKILL.md「避免重演」。")
    else:
        A("✅ 未命中量化红线。若主观感受仍差，重点查维度 5 的主题漂移与维度的定性问题。")
    A("")
    return "\n".join(lines)


def main():
    ap = argparse.ArgumentParser(description="Claude 会话劣化诊断")
    ap.add_argument("target", help="session id 或 jsonl 路径")
    ap.add_argument("--out", help="写入指定文件而非 stdout", default=None)
    args = ap.parse_args()

    path = find_jsonl(args.target)
    if not path:
        sys.stderr.write(f"找不到会话文件：{args.target}\n")
        sys.stderr.write("已搜索 ~/.claude/projects/*/<target>.jsonl\n")
        sys.exit(2)

    recs = load_records(path)
    if not recs:
        sys.stderr.write(f"解析失败或空文件：{path}\n")
        sys.exit(3)

    report = build_report(path, recs)
    if args.out:
        with open(args.out, "w", encoding="utf-8") as fp:
            fp.write(report)
        sys.stderr.write(f"报告已写入 {args.out}\n")
    else:
        sys.stdout.write(report + "\n")


if __name__ == "__main__":
    main()
