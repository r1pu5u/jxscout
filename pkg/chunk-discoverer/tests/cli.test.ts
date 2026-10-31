import { test, expect } from "vitest";
import { mkdtempSync, writeFileSync, rmSync, readdirSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";

async function runCLI(args: string[]) {
  const child = spawn(
    process.env.JXSCOUT_TEST_BINARY ?? "bun",
    process.env.JXSCOUT_TEST_BINARY
      ? args
      : ["pkg/agent-cli/index.ts", ...args],
    {
      cwd: process.cwd(),
      stdio: ["ignore", "pipe", "pipe"],
      timeout: 15000,
    },
  );
  let stdout = "",
    stderr = "";
  child.stdout.on("data", (d) => (stdout += d));
  child.stderr.on("data", (d) => (stderr += d));
  const code = await new Promise((resolve, reject) => {
    child.once("error", reject);
    child.once("exit", resolve);
  });
  return { code, stdout, stderr };
}
test("help presents proxy capture and does not expose a scan command", async () => {
  const result = await runCLI(["--help"]);
  expect(result.code).toBe(0);
  expect(result.stdout).toContain("serve");
  expect(result.stdout).not.toMatch(/^\s+scan(?:\s|$)/m);
  expect(result.stdout).not.toMatch(/^\s+tui(?:\s|$)/m);
});
test("removed tui command directs users to serve", async () => {
  const result = await runCLI(["tui"]);
  expect(result.code).toBe(1);
  expect(result.stderr).toContain("jxscout serve");
});
test("removed scan command directs users to serve without creating state", async () => {
  const dir = mkdtempSync(path.join(tmpdir(), "jxscout-no-scan-"));
  try {
    const result = await runCLI([
      "scan",
      "https://example.invalid",
      "--data-dir",
      dir,
      "--format",
      "json",
    ]);
    expect(result.code).toBe(1);
    expect(result.stderr).toContain("scan command was removed");
    expect(result.stderr).toContain("jxscout serve");
    expect(readdirSync(dir)).toEqual([]);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
test("inline source maps produce separately analyzed source artifacts", async () => {
  const dir = mkdtempSync(path.join(tmpdir(), "jxscout-map-"));
  try {
    const map = Buffer.from(
      JSON.stringify({
        version: 3,
        sources: ["original.js"],
        sourcesContent: ['fetch("/api/map");'],
        mappings: "",
      }),
    ).toString("base64");
    const input = path.join(dir, "input.js");
    writeFileSync(
      input,
      'console.log("compiled");\n//# sourceMappingURL=data:application/json;base64,' +
        map,
    );
    const result = await runCLI([
      "analyze",
      input,
      "--data-dir",
      dir,
      "--webcrack",
      "off",
      "--bruteforce-limit",
      "0",
      "--format",
      "json",
    ]);
    expect(result.code).toBe(0);
    const assets = await runCLI([
      "assets",
      "--data-dir",
      dir,
      "--format",
      "json",
    ]);
    expect(
      JSON.parse(assets.stdout).result.items.map((a: any) => a.kind),
    ).toContain("sourcemap:original.js");
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}, 20000);
