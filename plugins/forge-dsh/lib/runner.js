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
 * Plan the spawn for one hook invocation, platform-aware. Exported for the
 * cross-OS unit tests — CI's Linux runners cannot execute the win32 route, so
 * the planning logic must be assertable without spawning.
 *
 * Windows: npm lays out forge as forge + forge.cmd (no forge.exe), and
 * CreateProcess cannot execute a .cmd shim — a bare spawn("forge") is ENOENT
 * and EVERY gate silently fails open. cmd.exe resolves forge.cmd via PATHEXT
 * (and a bare forge.exe directly), so win32 routes through it. argv holds
 * spec constants only (safe charset). The binary is operator config and is
 * quoted, never escaped — see the metacharacter note inside planSpawn.
 *
 * @param {string} forgeBin - binary name/path from config.
 * @param {string[]} argv - hook arguments (spec constants).
 * @param {string} [platform] - process.platform override (tests).
 * @returns {{file: string, args: string[], shell: boolean}}
 */
export function planSpawn(forgeBin, argv, platform = process.platform) {
  if (platform !== "win32") {
    return { file: forgeBin, args: argv, shell: false };
  }
  // cmd.exe metacharacters: an unquoted & | ( ) < > ^ , ; = splits or redirects
  // the token ("C:\A&B\forge.cmd" would execute a stray "B\forge.cmd"), so quote
  // on any of them, not just spaces. Two things quoting cannot neutralize:
  // %VAR% expansion (cmd expands it even inside quotes) and ! with delayed
  // expansion (off by default). Both are accepted residual risk because
  // forgeBin is trusted operator config — the same trust level as PATH itself.
  const needsQuotes = /[&|()<>^,;=\s]/.test(forgeBin);
  const file = needsQuotes && !forgeBin.includes('"') ? `"${forgeBin}"` : forgeBin;
  return { file, args: argv, shell: true };
}

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
  const plan = planSpawn(forgeBin, parts.slice(1));
  return new Promise((resolve) => {
    let child;
    try {
      if (plan.shell) {
        // Single command STRING (not file+args): passing an args array with
        // shell:true is DEP0190 — args are concatenated unescaped. The pieces
        // here are spec constants plus the pre-quoted binary (planSpawn), so
        // the joined line is exactly what cmd.exe should parse.
        child = spawn([plan.file, ...plan.args].join(" "), {
          stdio: ["pipe", "pipe", "pipe"],
          cwd: opts.cwd,
          shell: true,
        });
      } else {
        child = spawn(plan.file, plan.args, {
          stdio: ["pipe", "pipe", "pipe"],
          cwd: opts.cwd,
        });
      }
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
      // On the win32 shell route the direct child is cmd.exe and the forge
      // grandchild holds the task index lock AND the inherited stdio pipes:
      // killing only the shell ORPHANS the grandchild (a dead parent's tree
      // is unfindable for /T) — it survives with the pipes open, pinning this
      // process's event loop forever. taskkill /T /F is therefore the primary
      // kill: it tears down shell + descendants atomically. kill() (wrapper
      // only) is the FALLBACK for when taskkill itself fails — spawn error
      // (not on PATH) or nonzero exit (access denied); killing just the
      // wrapper then still beats killing nothing. Non-shell routes kill the
      // forge process directly.
      if (plan.shell && child.pid) {
        const fallback = () => {
          try {
            child.kill();
          } catch {
            // already exited — nothing to kill
          }
        };
        try {
          const killer = spawn("taskkill", ["/pid", String(child.pid), "/T", "/F"], { stdio: "ignore" });
          killer.on("error", fallback);
          killer.on("close", (c) => {
            if (c !== 0) fallback();
          });
        } catch {
          fallback();
        }
      } else {
        try {
          child.kill();
        } catch {
          // already exited — nothing to kill
        }
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
    child.on("close", (code, signal) => {
      const text = out.trim();
      if (text === "") {
        // Exit 0 with no stdout is forge's clean silent allow. A NONZERO exit
        // with no stdout is an infrastructure failure: on the win32 cmd.exe
        // route a missing binary prints its error to stderr and exits — the
        // spawn "error" event never fires because cmd.exe itself started
        // fine. Fail open WITH an error note so /forge-status surfaces it.
        // signal kills report code === null ("exited with code null" reads
        // like a bug; name the signal instead).
        const how = code === null ? `signal ${signal}` : `code ${code}`;
        return settle(code === 0 ? { block: false } : { block: false, error: `forge exited with ${how} and no output` });
      }
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
