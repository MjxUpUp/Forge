/**
 * skill-router — pi extension（强制 skill 路由）
 *
 * 解决问题：agent 有时不走 skill 自己瞎搞。
 * 现有 skill-enforcer（before_agent_start 注入纪律）是"软强制"——只能说服模型去查，
 * 模型仍可能跳过。本 extension 是"硬强制"：在用户输入阶段（input 事件）按关键词匹配，
 * 命中即把输入 transform 成 `/skill:name <原文>`，由 pi 强制展开 skill 内容注入上下文。
 *
 * 机制：input 事件 return { action: "transform", text: "/skill:name <原文>" }
 * （见 pi docs/extensions.md 的 input 事件 + examples/extensions/input-transform.ts）
 *
 * 路由表来源（优先级从高到低，见 loadRoutes() 候选链）：
 *   1. $FORGE_SKILLS_CANONICAL/skill-routing/routes.json   （开发者显式覆盖）
 *   2. ~/.forge/skills-cache/embedded/skill-routing/routes.json   （forge 二进制自带快照，跨机器通用）
 *   3. ~/.pi/agent/skill-routes.json   （pi 标准位置，旧 fallback）
 *   4. 代码内置默认表                 （飞书 skill 路由表 + 常见纪律 skill 触发）
 *
 * 安全：跳过已是命令的输入（/ 开头）、跳过 extension 源消息、每输入只命中第一条规则。
 * 不删除 skill-enforcer——两者互补（enforcer 软提醒 / router 硬展开）。
 *
 * 配置文件格式 skill-routes.json：
 *   [
 *     {
 *       "match": ["关键词1", "关键词2"],     // 任一命中即触发（子串匹配，不区分大小写）
 *       "skill": "skill-name",               // transform 成 /skill:skill-name
 *       "reason": "为什么路由到这里（可选）"   // 仅记录日志，不影响行为
 *     }
 *   ]
 */

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import * as fs from "node:fs";
import * as os from "node:os";
import * as path from "node:path";

// Resolve home dir (Windows USERPROFILE 优先，兼容 HOME；os.homedir() 跨平台兜底，绝不硬编码本机用户名)
const HOME =
  process.env.USERPROFILE || process.env.HOME || os.homedir() || process.cwd();
const PI_ROUTES_FILE = path.join(HOME, ".pi", "agent", "skill-routes.json");

// 内置默认路由表（用户无配置文件时使用）。
// 来源于 AGENTS.md 的飞书 skill 路由规则 + 常见纪律 skill 触发场景。
// 命中即强制展开对应 skill，不再依赖模型自觉 read。
const DEFAULT_ROUTES: Route[] = [
  // ===== 飞书 / Lark 路由表（来自 AGENTS.md，路由表优先级高于语义匹配）=====
  { match: ["日程", "会议室", "忙闲", "预约会议", "预定会议", "会议预约"], skill: "lark-calendar", reason: "日程/会议室类" },
  { match: ["历史会议", "已结束的会议", "纪要", "逐字稿", "会议记录", "会议总结"], skill: "lark-vc", reason: "已结束会议产物" },
  { match: ["入会", "离会", "会中实时", "正在开的会议", "进行中的会议"], skill: "lark-vc-agent", reason: "会中实时事件" },
  { match: ["发消息", "群聊", "搜索聊天", "聊天记录", "群消息"], skill: "lark-im", reason: "即时通讯" },
  { match: ["读文档", "创建文档", "编辑文档", "docx", "wiki 文档", "云文档内容"], skill: "lark-doc", reason: "文档内容操作" },
  { match: ["下载文件", "上传文件", "文件夹", "文件权限", "云盘", "云空间"], skill: "lark-drive", reason: "云空间文件操作" },
  { match: ["多维表格", "bitable", "Base 表"], skill: "lark-base", reason: "多维表格" },
  { match: ["电子表格", "sheets", "单元格"], skill: "lark-sheets", reason: "电子表格" },
  { match: ["发邮件", "收件箱", "邮件", "回复邮件", "转发邮件"], skill: "lark-mail", reason: "邮箱" },
  { match: ["通讯录", "找人", "按姓名查", "查 open_id"], skill: "lark-contact", reason: "通讯录找人" },
  { match: ["妙记", "minutes", "音视频转写"], skill: "lark-minutes", reason: "妙记转写" },
  { match: ["考勤", "打卡"], skill: "lark-attendance", reason: "考勤" },
  { match: ["审批", "审批待办"], skill: "lark-approval", reason: "审批" },
  { match: ["OKR", "目标", "关键结果"], skill: "lark-okr", reason: "OKR" },
  { match: ["待办任务", "任务清单", "创建任务", "飞书任务"], skill: "lark-task", reason: "飞书任务" },
  { match: ["画板", "白板"], skill: "lark-whiteboard", reason: "画板" },
  { match: ["幻灯片", "PPT", "飞书演示"], skill: "lark-slides", reason: "幻灯片" },

  // ===== 通用研发纪律路由（高频被瞎搞的场景，强制走 skill）=====
  { match: ["为什么会话变笨", "会话越来越", "分析下这个会话", "会话为什么失败", "Claude 变蠢"], skill: "claude-session-diagnostics", reason: "会话劣化诊断" },
  { match: ["提交代码", "能提交吗", "代码审查", "code review", "审查代码", "合并分支"], skill: "implementation-discipline", reason: "实施到交付纪律" },
];

type Route = { match: string[]; skill: string; reason?: string };

function loadRoutes(): Route[] {
  // 优先级：$FORGE_SKILLS_CANONICAL（开发者显式覆盖）> embedded 缓存(跨机器通用) > pi 路径；都不在用默认表。
  // 不硬编码本机源路径——开发者本机调试时 export FORGE_SKILLS_CANONICAL=<skills 根> 即可。
  const EMBEDDED_ROUTES = path.join(HOME, ".forge", "skills-cache", "embedded", "skill-routing", "routes.json");
  const candidates = [
    process.env.FORGE_SKILLS_CANONICAL ? path.join(process.env.FORGE_SKILLS_CANONICAL, "skill-routing", "routes.json") : null,
    EMBEDDED_ROUTES,
    PI_ROUTES_FILE,
  ].filter((f): f is string => !!f);
  for (const f of candidates) {
    try {
      if (fs.existsSync(f)) {
        const raw = fs.readFileSync(f, "utf-8");
        const parsed = JSON.parse(raw);
        if (Array.isArray(parsed) && parsed.length > 0) {
          // 轻量校验：每条要有 match 数组和 skill 字符串
          return parsed
            .filter(
              (r: unknown): r is Route =>
                !!r &&
                typeof r === "object" &&
                Array.isArray((r as Route).match) &&
                typeof (r as Route).skill === "string"
            )
            .map((r) => ({
              match: r.match.filter((m) => typeof m === "string" && m.length > 0),
              skill: r.skill,
              reason: typeof r.reason === "string" ? r.reason : undefined,
            }))
            .filter((r) => r.match.length > 0);
        }
      }
    } catch {
      // 解析失败静默降级到默认表（不打断用户输入）
    }
  }
  return DEFAULT_ROUTES;
}

function matchRoute(text: string, routes: Route[]): Route | null {
  const lower = text.toLowerCase();
  // 顺序匹配，返回第一条命中
  for (const r of routes) {
    for (const kw of r.match) {
      if (kw && lower.includes(kw.toLowerCase())) {
        return r;
      }
    }
  }
  return null;
}

export default function (pi: ExtensionAPI) {
  // 热加载：session_start 时读一次路由表（/reload 会触发 session_start reason=reload）
  let routes = loadRoutes();

  pi.on("session_start", async () => {
    routes = loadRoutes();
  });

  pi.on("input", async (event, ctx) => {
    // 跳过 extension 注入的消息，避免循环 / 误触发
    if (event.source === "extension") {
      return { action: "continue" };
    }

    const text = event.text ?? "";

    // 已经是命令（/ 开头）不路由——用户显式输入应尊重，且 /skill: 已展开
    const trimmed = text.trim();
    if (trimmed.startsWith("/")) {
      return { action: "continue" };
    }

    // 短输入（< 2 字符）跳过，避免误触发
    if (trimmed.length < 2) {
      return { action: "continue" };
    }

    const route = matchRoute(text, routes);
    if (!route) {
      return { action: "continue" };
    }

    // 命中：transform 成 /skill:name <原文>。
    // pi 会把 /skill:name 展开为 skill 全文注入上下文，agent 必须基于该 skill 工作。
    const transformed = `/skill:${route.skill} ${text}`;
    if (ctx.hasUI) {
      ctx.ui.notify(
        `↪ 强制路由 → ${route.skill}${route.reason ? `（${route.reason}）` : ""}`,
        "info"
      );
    }
    return { action: "transform", text: transformed };
  });
}
