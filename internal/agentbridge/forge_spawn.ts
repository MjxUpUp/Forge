function runForge(cmd: string, payload: object): Promise<{ block: boolean; reason?: string }> {
  return new Promise((resolve) => {
    const parts = cmd.split(" "); // ["forge","hook","task-guard"]
    const child = spawn(parts[0], parts.slice(1), {
      stdio: ["pipe", "pipe", "pipe"],
    });
    let out = "";
    let settled = false;
    // 30s ceiling: a hung forge process (index.lock, blocked stdin, AV scan)
    // would otherwise never resolve this Promise and freeze the agent's tool
    // call forever. Timeout → kill + fail open, same contract as spawn error /
    // JSON parse failure below: tool faults fail open, never block.
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      try {
        child.kill();
      } catch {
        // already exited — nothing to kill
      }
      resolve({ block: false });
    }, 30000);
    const settle = (verdict: { block: boolean; reason?: string }) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(verdict);
    };
    child.stdout.on("data", (d: Buffer) => (out += d.toString()));
    child.on("error", () => settle({ block: false })); // forge missing → fail open
    child.on("close", () => {
      // forge ALWAYS emits one JSON line to stdout: {decision:"approve"|"block",
      // hookSpecificOutput?:{additionalContext}}. Block is signaled by the JSON
      // "decision":"block" — NOT an exit code (cobra surfaces forge's internal
      // error as exit 1, indistinguishable from a real deny). Parse the JSON;
      // only fall back to allow on parse failure (fail open).
      try {
        const j = JSON.parse(out);
        const reason = j?.hookSpecificOutput?.additionalContext ?? "denied";
        return settle({ block: j?.decision === "block", reason });
      } catch {
        settle({ block: false });
      }
    });
    child.stdin.end(JSON.stringify(payload));
  });
}
