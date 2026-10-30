import { test, expect } from "vitest";
import { createServer } from "node:http";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawn } from "node:child_process";

test("seed template finds hidden chunk, then recursively follows its import", async () => {
  const hits: string[] = [];
  const files: Record<string, string> = {
    "/7.aabbccdd.js": 'const table={987654:"deadbeef"};',
    "/987654.deadbeef.js": 'import("./nested.js");',
    "/nested.js": 'fetch("/api/private");',
  };
  const server = createServer((req, res) => {
    hits.push(req.url!);
    if (files[req.url!] !== undefined) {
      res.writeHead(200, { "Content-Type": "application/javascript" });
      res.end(files[req.url!]);
    } else {
      res.writeHead(404);
      res.end();
    }
  });
  await new Promise<void>((r) =>
    server
      .once("error", (e) => {
        throw e;
      })
      .listen(0, "127.0.0.1", r),
  );
  const port = (server.address() as any).port;
  const dir = mkdtempSync(path.join(tmpdir(), "jxscout-cli-"));
  try {
    const args = [
      "pkg/agent-cli/index.ts",
      "scan",
      `http://127.0.0.1:${port}/7.aabbccdd.js`,
      "--data-dir",
      dir,
      "--webcrack",
      "off",
      "--bruteforce-limit",
      "0",
      "--format",
      "json",
    ];
    const child = spawn(
      process.env.JXSCOUT_TEST_BINARY ?? "bun",
      process.env.JXSCOUT_TEST_BINARY ? args.slice(1) : args,
      {
        cwd: process.cwd(),
        stdio: ["ignore", "pipe", "pipe"],
      },
    );
    let stdout = "",
      stderr = "";
    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));
    const code = await new Promise((r) => child.on("exit", r));
    expect(stderr).toBe("");
    expect(code).toBe(0);
    expect(JSON.parse(stdout).result.status).toBe("completed");
    expect(hits).toEqual([
      "/7.aabbccdd.js",
      "/987654.deadbeef.js",
      "/nested.js",
    ]);
  } finally {
    server.close();
    rmSync(dir, { recursive: true, force: true });
  }
}, 20000);

async function withServer(
  handler: any,
  work: (origin: string, dir: string) => Promise<void>,
) {
  const server = createServer(handler);
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  const dir = mkdtempSync(path.join(tmpdir(), "jxscout-regression-"));
  try {
    await work(`http://127.0.0.1:${(server.address() as any).port}`, dir);
  } finally {
    server.close();
    rmSync(dir, { recursive: true, force: true });
  }
}
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
test("redirected HTML resolves scripts against its final URL", async () => {
  const hits: string[] = [];
  await withServer(
    (req: any, res: any) => {
      hits.push(req.url);
      if (req.url === "/start") {
        res.writeHead(302, { Location: "/app/page" });
        res.end();
      } else if (req.url === "/app/page")
        res.end('<script src="./entry.js"></script>');
      else if (req.url === "/app/entry.js") res.end('fetch("/api/recovered");');
      else {
        res.writeHead(404);
        res.end();
      }
    },
    async (origin, dir) => {
      const result = await runCLI([
        "scan",
        origin + "/start",
        "--data-dir",
        dir,
        "--webcrack",
        "off",
        "--bruteforce-limit",
        "0",
        "--format",
        "json",
      ]);
      expect(result.stderr).toBe("");
      expect(result.code).toBe(0);
      expect(hits).toEqual(["/start", "/app/page", "/app/entry.js"]);
    },
  );
}, 20000);
test("scope excludes seeds and redirect targets before they are fetched", async () => {
  const hits: string[] = [];
  await withServer(
    (req: any, res: any) => {
      hits.push(req.url);
      res.writeHead(302, { Location: "/denied" });
      res.end();
    },
    async (origin, dir) => {
      const outside = await runCLI([
        "scan",
        origin + "/outside",
        "--data-dir",
        dir,
        "--scope",
        origin + "/allowed*",
        "--webcrack",
        "off",
        "--format",
        "json",
      ]);
      expect(outside.code).toBe(1);
      expect(hits).toEqual([]);
      const redirect = await runCLI([
        "scan",
        origin + "/allowed",
        "--data-dir",
        dir,
        "--scope",
        origin + "/allowed*",
        "--webcrack",
        "off",
        "--format",
        "json",
      ]);
      expect(redirect.code).toBe(2);
      expect(hits).toEqual(["/allowed"]);
      expect(JSON.parse(redirect.stdout).result.status).toBe("partial");
    },
  );
}, 20000);
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
