function runForge(cmd: string, payload: object): Promise<{ block: boolean; reason?: string; error?: string }> {
  return new Promise((resolve) => {
    const parts = cmd.split(" "); // ["forge","hook","task-guard"]
    // Windows: npm lays out forge as forge + forge.cmd (no forge.exe) and
    // CreateProcess cannot execute a .cmd shim — a bare spawn("forge") is
    // ENOENT and every gate silently fails open. cmd.exe resolves forge.cmd
    // via PATHEXT (and a bare forge.exe directly), so win32 routes through it
    // as ONE pre-built command string (an args array with shell:true is
    // DEP0190). The binary is quoted on spaces OR cmd.exe metacharacters
    // (& | ( ) < > ^ , ; = split an unquoted token); %VAR%/!-expansion are
    // accepted residual risk — operator config, same trust as PATH. Same
    // fix as plugins/forge-dsh/lib/runner.js planSpawn.
    const winShell = process.platform === "win32";
    const bin = winShell && /[&|()<>^,;=\s]/.test(parts[0]) && !parts[0].includes('"') ? `"${parts[0]}"` : parts[0];
    const child = winShell
      ? spawn([bin, ...parts.slice(1)].join(" "), { stdio: ["pipe", "pipe", "pipe"], shell: true })
      : spawn(parts[0], parts.slice(1), { stdio: ["pipe", "pipe", "pipe"] });
    let out = "";
    let settled = false;
    // 30s ceiling: a hung forge process (index.lock, blocked stdin, AV scan)
    // would otherwise never resolve this Promise and freeze the agent's tool
    // call forever. Timeout → kill + fail open, same contract as spawn error /
    // JSON parse failure below: tool faults fail open, never block.
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      // On the win32 shell route cmd.exe is the direct child and the forge
      // grandchild holds inherited stdio pipes: killing ONLY the shell orphans
      // the grandchild with the pipes open, pinning this process's event loop
      // forever. taskkill /T /F is the primary kill (shell + descendants
      // atomically); kill() — wrapper only — is the fallback for when taskkill
      // itself fails (spawn error or nonzero exit), and the sole kill on
      // non-shell routes.
      if (winShell && child.pid) {
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
          killer.on("close", (c: number) => {
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
      console.error(`[forge] hook timed out after 30000ms: ${cmd}`);
      resolve({ block: false, error: "forge timed out after 30000ms" });
    }, 30000);
    const settle = (verdict: { block: boolean; reason?: string; error?: string }) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(verdict);
    };
    child.stdout.on("data", (d: Buffer) => (out += d.toString()));
    // Every infrastructure failure fails open WITH a diagnostic on both
    // channels — error note and console.error: the opencode caller drops the
    // error field, so stderr is the only surface an operator will ever see.
    child.on("error", (e: unknown) => {
      const why = e instanceof Error ? e.message : String(e);
      console.error(`[forge] hook spawn error: ${cmd}: ${why}`);
      settle({ block: false, error: `forge spawn error: ${why}` }); // forge missing → fail open
    });
    child.on("close", (code: number | null, signal: string | null) => {
      // forge ALWAYS emits one JSON line to stdout on success: {decision:
      // "approve"|"block", hookSpecificOutput?:{additionalContext}}. Block is
      // signaled by the JSON "decision":"block" — NOT an exit code (cobra
      // surfaces forge's internal error as exit 1, indistinguishable from a
      // real deny). Empty stdout is split by exit code: exit 0 is forge's
      // clean silent allow, but a NONZERO exit with no stdout is an
      // infrastructure failure — on the win32 cmd.exe route a missing binary
      // prints to stderr and exits, the spawn "error" event never fires
      // (cmd.exe itself started fine), and a bare JSON.parse("") would
      // collapse it into an anonymous allow. Fail open WITH an error note,
      // and mirror it to console.error: the opencode caller drops the error
      // field, so stderr is the only channel an operator will ever see
      // (same contract as plugins/forge-dsh/lib/runner.js).
      if (out.trim() === "") {
        if (code !== 0) {
          const how = code === null ? `signal ${signal}` : `code ${code}`;
          console.error(`[forge] hook failed: ${cmd} exited with ${how} and no output`);
          return settle({ block: false, error: `forge exited with ${how} and no output` });
        }
        return settle({ block: false });
      }
      try {
        const j = JSON.parse(out);
        const reason = j?.hookSpecificOutput?.additionalContext ?? "denied";
        return settle({ block: j?.decision === "block", reason });
      } catch {
        console.error(`[forge] hook emitted unparseable stdout (${cmd}): ${out.slice(0, 200)}`);
        settle({ block: false, error: "unparseable forge stdout" });
      }
    });
    child.stdin.end(JSON.stringify(payload));
  });
}
