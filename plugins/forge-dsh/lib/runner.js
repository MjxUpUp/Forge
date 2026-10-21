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
 * @property {boolean} [timedOut] - the failure was the ceiling kill (not a forge error).
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
 * quoted, never escaped — see the metacharacter note inside planSpawn. Arguments
 * with a cmd metacharacter (a batch matcher such as Write|Edit) are quoted too.
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
  const quote = (token) => (/[&|()<>^,;=\s]/.test(token) && !token.includes('"') ? `"${token}"` : token);
  // Arguments get the same treatment: a batch matcher like "Write|Edit" would
  // otherwise be parsed by cmd.exe as a pipe. Spec-constant arguments (hook
  // names, flags) carry no metacharacter and pass through unchanged.
  return { file: quote(forgeBin), args: argv.map(quote), shell: true };
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
  const parts = command.split(" "); // ["forge","hook","task-guard"]
  return runForge(parts.slice(1), payload, opts);
}

/**
 * Run one whole matcher group in a single forge process:
 * `forge hook batch --event <E> [--matcher <M>]`. forge runs the group's roster
 * in order, stops at the first block, and prints that hook's decision JSON to
 * stdout (same contract as a single hook); allow-path contexts come back merged
 * into one document. One spawn instead of N saves the per-process start-up cost on
 * every tool call. Forges from before the stdout fix print nothing on a block —
 * decisions.runMatchedGroups detects that and falls back to per-hook runs.
 *
 * @param {string} event - forge event name (PreToolUse, Stop, …).
 * @param {string} matcher - the spec group's matcher; "" selects the event's
 *   matcher-less group.
 * @param {object} payload - Claude-Code-shape hook stdin payload.
 * @param {object} [opts] - same as runForgeHook.
 * @returns {Promise<HookVerdict>}
 */
export function runForgeBatch(event, matcher, payload, opts = {}) {
  const argv = ["hook", "batch", "--event", event];
  if (matcher !== "") argv.push("--matcher", matcher);
  return runForge(argv, payload, opts);
}

/** Spawn forge with `argv`, feed the payload, and fold stdout into a verdict. */
function runForge(argv, payload, opts) {
  const forgeBin = opts.forgeBin ?? "forge";
  const timeoutMs = opts.timeoutMs ?? 30000;
  const plan = planSpawn(forgeBin, argv);
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
      killTree(child, plan.shell);
      resolve({ block: false, error: `timeout after ${timeoutMs}ms`, timedOut: true });
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

/**
 * Kill a spawned forge and, on the win32 shell route, its whole process tree.
 *
 * On the win32 shell route the direct child is cmd.exe and the forge
 * grandchild holds the task index lock AND the inherited stdio pipes: killing
 * only the shell ORPHANS the grandchild (a dead parent's tree is unfindable
 * for /T) — it survives with the pipes open, pinning this process's event
 * loop forever. taskkill /T /F is therefore the primary kill: it tears down
 * shell + descendants atomically. kill() (wrapper only) is the FALLBACK for
 * when taskkill itself fails — spawn error (not on PATH) or nonzero exit
 * (access denied); killing just the wrapper then still beats killing nothing.
 * Non-shell routes kill the forge process directly.
 *
 * @param {import("node:child_process").ChildProcess} child
 * @param {boolean} shell - planSpawn's shell flag for this child.
 */
function killTree(child, shell) {
  if (shell && child.pid) {
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
      killer.on("close", (code) => {
        if (code !== 0) fallback();
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
}

/**
 * Probe whether the configured forge binary is runnable: one `forge --version`
 * spawn, stdout captured, bounded by `timeoutMs` (default 5000 — a probe must
 * never hold up session start for a hook-sized 30s). Fail-open like the hook
 * path: resolves `{ok:false, error}` instead of throwing, so a missing or
 * broken binary can be reported to the user rather than silently voiding every
 * gate.
 *
 * The `--version` subcommand does not read stdin (verified against forge
 * 1.79.0), so stdin is ignored; a hung probe is killed with the same
 * tree-safe kill as a hook.
 *
 * @param {object} [opts]
 * @param {string} [opts.forgeBin="forge"]
 * @param {number} [opts.timeoutMs=5000]
 * @returns {Promise<{ok: boolean, version?: string, error?: string}>}
 */
export function probeForge(opts = {}) {
  const forgeBin = opts.forgeBin ?? "forge";
  const timeoutMs = opts.timeoutMs ?? 5000;
  const plan = planSpawn(forgeBin, ["--version"]);
  return new Promise((resolve) => {
    let child;
    try {
      const args = plan.shell ? [[plan.file, ...plan.args].join(" ")] : [plan.file, plan.args];
      child = spawn(...args, {
        stdio: ["ignore", "pipe", "pipe"],
        ...(plan.shell ? { shell: true } : {}),
      });
    } catch (error) {
      resolve({ ok: false, error: note(error) });
      return;
    }
    let out = "";
    let err = "";
    let settled = false;
    let timer;
    const finish = (verdict) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(verdict);
    };
    timer = setTimeout(() => {
      killTree(child, plan.shell);
      finish({ ok: false, error: `timeout after ${timeoutMs}ms` });
    }, timeoutMs);
    child.stdout.on("data", (d) => (out += d.toString()));
    child.stderr.on("data", (d) => (err += d.toString()));
    child.on("error", (error) => finish({ ok: false, error: note(error) }));
    child.on("close", (code) => {
      if (code === 0) {
        const version = out.trim();
        finish(version === "" ? { ok: true } : { ok: true, version });
        return;
      }
      // A missing binary on the win32 cmd.exe route exits nonzero with its
      // message on stderr (cmd.exe itself spawned fine, so no 'error' event);
      // keep that tail — it is the only diagnostic the user sees.
      const tail = err.trim().slice(0, 200);
      finish({ ok: false, error: `forge --version exited with code ${code}${tail === "" ? "" : `: ${tail}`}` });
    });
  });
}

function note(error) {
  return (error instanceof Error ? error.message : String(error)).slice(0, 300);
}
