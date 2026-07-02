function runForge(cmd: string, payload: object): Promise<{ block: boolean; reason?: string }> {
  return new Promise((resolve) => {
    const parts = cmd.split(" "); // ["forge","hook","task-guard"]
    const child = spawn(parts[0], parts.slice(1), {
      stdio: ["pipe", "pipe", "pipe"],
    });
    let out = "";
    child.stdout.on("data", (d: Buffer) => (out += d.toString()));
    child.on("error", () => resolve({ block: false })); // forge missing → fail open
    child.on("close", () => {
      // forge ALWAYS emits one JSON line to stdout: {decision:"approve"|"block",
      // hookSpecificOutput?:{additionalContext}}. Block is signaled by the JSON
      // "decision":"block" — NOT an exit code (cobra surfaces forge's internal
      // error as exit 1, indistinguishable from a real deny). Parse the JSON;
      // only fall back to allow on parse failure (fail open).
      try {
        const j = JSON.parse(out);
        const reason = j?.hookSpecificOutput?.additionalContext ?? "denied";
        return resolve({ block: j?.decision === "block", reason });
      } catch {
        resolve({ block: false });
      }
    });
    child.stdin.end(JSON.stringify(payload));
  });
}
