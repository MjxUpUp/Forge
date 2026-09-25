/**
 * Platform-correct forge binary double for the spawn-path tests.
 *
 * fake-forge.mjs is spawned DIRECTLY on POSIX (shebang + exec bit); Windows
 * CreateProcess cannot execute .mjs, and the runner's win32 path routes every
 * spawn through cmd.exe — so on Windows the double is a generated .cmd shim
 * wrapping the .mjs with node, mirroring exactly how npm lays out the real
 * forge bin (forge + forge.cmd, no forge.exe). Generated at import time (not
 * committed) to stay out of CRLF churn.
 */
import { readFileSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

const mjs = fileURLToPath(new URL("./fake-forge.mjs", import.meta.url));

let bin = mjs;
if (process.platform === "win32") {
  const cmd = fileURLToPath(new URL("./fake-forge.cmd", import.meta.url));
  const body = `@node "${mjs}" %*\r\n`;
  // Write only when the bytes differ: node --test runs test files concurrently
  // and both import this module — rewriting the .cmd while another file's
  // batch is mid-execution can truncate it (cmd.exe reads batches by byte
  // offset, not inode). Identical content → skip the write entirely.
  let prev = null;
  try {
    prev = readFileSync(cmd, "utf8");
  } catch {
    // first run — the file does not exist yet
  }
  if (prev !== body) {
    writeFileSync(cmd, body);
  }
  bin = cmd;
}

export { bin as FAKE_BIN };
