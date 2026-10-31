import { test, expect } from "vitest";
import {
  mkdtempSync,
  mkdirSync,
  writeFileSync,
  readFileSync,
  existsSync,
  rmSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";
const source = path.resolve("pkg/agent-cli/index.ts");
async function run(cwd: string, args: string[], registry?: string) {
  const bin = process.env.JXSCOUT_TEST_BINARY;
  const child = spawn(bin ?? "bun", bin ? args : [source, ...args], {
    cwd,
    env: {
      ...process.env,
      JXSCOUT_PROJECTS_FILE: registry ?? path.join(cwd, ".test-projects.json"),
    },
    stdio: ["ignore", "pipe", "pipe"],
    timeout: 15000,
  });
  let stdout = "",
    stderr = "";
  child.stdout.on("data", (d) => (stdout += d));
  child.stderr.on("data", (d) => (stderr += d));
  const code = await new Promise((resolve, reject) => {
    child.once("exit", resolve);
    child.once("error", reject);
  });
  expect(stderr).toBe("");
  expect(code).toBe(0);
  return JSON.parse(stdout).result;
}
test("init keeps configuration and outputs in cwd/jxscout and resolves ancestors", async () => {
  const temporary = mkdtempSync(path.join(tmpdir(), "jxscout-workspace-"));
  const cwd = path.join(temporary, "foo.com");
  mkdirSync(cwd);
  const registry = path.join(temporary, "projects.json");
  const invoke = (cwd: string, args: string[]) => run(cwd, args, registry);
  try {
    const initialized = await invoke(cwd, [
      "init",
      "--project",
      "foo.com",
      "--scope",
      "https://foo.com/*",
      "--format",
      "json",
    ]);
    expect(initialized.output).toBe(path.join(cwd, "jxscout"));
    expect(
      JSON.parse(
        readFileSync(path.join(cwd, "jxscout", "config.json"), "utf8"),
      ),
    ).toEqual({ version: 1, project: "foo.com", scope: ["https://foo.com/*"] });
    writeFileSync(path.join(cwd, "entry.js"), 'fetch("/api/test");');
    const analyzed = await invoke(cwd, [
      "analyze",
      "entry.js",
      "--webcrack",
      "off",
      "--bruteforce-limit",
      "0",
      "--format",
      "json",
    ]);
    expect(analyzed.status).toBe("completed");
    expect(analyzed.output).toBe(path.join(cwd, "jxscout"));
    expect(
      existsSync(
        path.join(cwd, "jxscout", ".state", "foo.com", "agent.sqlite"),
      ),
    ).toBe(true);
    const nested = path.join(cwd, "nested", "child");
    mkdirSync(nested, { recursive: true });
    const assets = await invoke(nested, ["assets", "--format", "json"]);
    expect(assets.total).toBe(1);
    const outside = await run(
      temporary,
      ["assets", "--project", "foo.com", "--format", "json"],
      registry,
    );
    expect(outside.total).toBe(1);
    const saved = JSON.parse(readFileSync(registry, "utf8"));
    expect(saved.projects["foo.com"].root).toBe(path.join(cwd, "jxscout"));
    const repeat = await invoke(cwd, ["init", "--format", "json"]);
    expect(repeat.status).toBe("already_initialized");
  } finally {
    rmSync(temporary, { recursive: true, force: true });
  }
}, 20000);
