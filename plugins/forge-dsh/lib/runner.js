/**
 * @agent_forge/forge-dsh — spawn layer.
 *
 * Runs one `forge hook <name>` command with a Claude-Code-shape JSON payload on
 * stdin and parses forge's verdict from its stdout JSON. The contract is the
 * one forge's claude emitter writes (internal/cli/hook.go emitClaudeOutput):
 *
 *   pass, no detail : exit 0, empty stdout
 *   pass + detail   : exit 0, {"hookSpecificOutput":{"hookEventName","additionalContext"}}
 *   block           : exit 2, {"decision":"block","reason":...,"hookSpecificOutput":{...}}
 *                     plus the reason on stderr
 *
 * Block is read from the JSON `decision` field — never from the exit code —
 * because cobra reports forge's internal errors as exit 1, indistinguishable
 * from a deny. Every infrastructure failure (forge missing, spawn error, JSON
 * parse failure, timeout) FAILS OPEN so a forge outage never locks the agent
 * out of its own tools. Same contract as the opencode/pi translators
 * (internal/agentbridge/forge_spawn.ts).
 *
 * @module runner
 */
import { spawn } from "node:child_process";

/**
 * @typedef {object} HookVerdict
 * @property {boolean} block    - forge denied the action.
 * @property {string} [reason]  - block reason (shown to the model).
 * @property {string} [context] - allow-path additionalContext to inject.
 * @property {string} [error]   - infrastructure failure note (fail-open path).
 */

/**
 * Run one forge hook command.
 *
 * @param {string} command - spec command, e.g. "forge hook task-guard".
 * @param {object} payload - Claude-Code-shape hook stdin payload.
 * @param {object} [opts]
 * @param {string} [opts.forgeBin="forge"] - forge binary (path or PATH name).
 * @param {number} [opts.timeoutMs=30000]  - kill + fail open past this.
 * @param {string} [opts.cwd]              - working directory for the hook.
 * @returns {Promise<HookVerdict>}
 */
export function runForgeHook(command, payload, opts = {}) {
  const forgeBin = opts.forgeBin ?? "forge";
  const timeoutMs = opts.timeoutMs ?? 30000;
  const parts = command.split(" "); // ["forge","hook","task-guard"]
  const argv = [forgeBin, ...parts.slice(1)];
  return new Promise((resolve) => {
    let child;
    try {
      child = spawn(argv[0], argv.slice(1), {
        stdio: ["pipe", "pipe", "pipe"],
        cwd: opts.cwd,
      });
    } catch (error) {
      resolve({ block: false, error: note(error) });
      return;
    }
    let out = "";
    let settled = false;
    // 30s default ceiling: a hung forge process (index.lock, blocked stdin,
    // AV scan) would otherwise never resolve this Promise and freeze the
    // agent's tool call forever. Timeout → kill + fail open.
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      try {
        child.kill();
      } catch {
        // already exited — nothing to kill
      }
      resolve({ block: false, error: `timeout after ${timeoutMs}ms` });
    }, timeoutMs);
    const settle = (verdict) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(verdict);
    };
    child.stdout.on("data", (d) => (out += d.toString()));
    child.on("error", (error) => settle({ block: false, error: note(error) }));
    child.on("close", () => {
      const text = out.trim();
      if (text === "") return settle({ block: false });
      try {
        const j = JSON.parse(text);
        const context = j?.hookSpecificOutput?.additionalContext;
        if (j?.decision === "block") {
          return settle({
            block: true,
            reason: j?.reason ?? context ?? "denied",
          });
        }
        return settle({
          block: false,
          context: typeof context === "string" && context !== "" ? context : undefined,
        });
      } catch {
        settle({ block: false, error: "unparseable forge stdout" });
      }
    });
    // stdin error guard: a forge that exits WITHOUT reading stdin (unknown
    // hook name after a version drift, a broken FORGE_BIN shim, an early
    // panic) makes this write raise EPIPE on the stdin stream — an unhandled
    // 'error' event here would crash the DSH HOST PROCESS, the exact opposite
    // of the fail-open contract. The verdict itself still comes from
    // close/stdout above; swallowing the stream error is sufficient.
    //
    // stdin 错误护栏：forge 不读 stdin 就提前退出（版本漂移后的未知 hook 名、
    // 坏掉的 FORGE_BIN shim、早期 panic）会让这次写入在 stdin 流上抛
    // EPIPE——此处未处理的 'error' 事件会**崩溃 DSH 宿主进程**，与
    // fail-open 契约正好相反。判定仍由上面的 close/stdout 路径产出，吞掉
    // 流错误即可。
    child.stdin.on("error", () => {});
    child.stdin.end(JSON.stringify(payload) + "\n");
  });
}

function note(error) {
  return (error instanceof Error ? error.message : String(error)).slice(0, 300);
}
