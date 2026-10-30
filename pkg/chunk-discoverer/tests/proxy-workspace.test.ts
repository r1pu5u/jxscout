import { createHash } from "node:crypto";
import { test, expect } from "vitest";
import { createServer } from "node:http";
import { connect } from "node:net";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  rmSync,
  readFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { spawn, type ChildProcess } from "node:child_process";

const binary = process.env.JXSCOUT_TEST_BINARY;

async function listen(
  server: ReturnType<typeof createServer>,
): Promise<number> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });
  return (server.address() as any).port;
}

function waitForExit(child: ChildProcess, timeoutMs: number): Promise<boolean> {
  if (child.exitCode !== null || child.signalCode !== null)
    return Promise.resolve(true);
  return new Promise((resolve) => {
    const onExit = () => {
      clearTimeout(timer);
      resolve(true);
    };
    const timer = setTimeout(() => {
      child.off("exit", onExit);
      resolve(false);
    }, timeoutMs);
    child.once("exit", onExit);
  });
}

async function waitForListener(
  child: ChildProcess,
  port: number,
  stderr: () => string,
): Promise<void> {
  const deadline = Date.now() + 10000;
  while (Date.now() < deadline) {
    if (child.exitCode !== null || child.signalCode !== null)
      throw new Error(`serve exited before listening: ${stderr()}`);
    const ready = await new Promise<boolean>((resolve) => {
      const socket = connect(port, "127.0.0.1");
      const timer = setTimeout(() => {
        socket.destroy();
        resolve(false);
      }, 300);
      socket.once("connect", () => {
        clearTimeout(timer);
        socket.destroy();
        resolve(true);
      });
      socket.once("error", () => {
        clearTimeout(timer);
        resolve(false);
      });
    });
    if (ready) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`serve did not listen within 10 seconds: ${stderr()}`);
}

test.skipIf(!binary)(
  "proxy commands use the initialized workspace from nested directories",
  async () => {
    const temporary = mkdtempSync(
      path.join(tmpdir(), "jxscout-proxy-workspace-"),
    );
    const target = path.join(temporary, "target");
    mkdirSync(target);
    const nested = path.join(target, "nested", "child");
    mkdirSync(nested, { recursive: true });

    const requestedPaths: string[] = [];
    const fixture = createServer((req, res) => {
      requestedPaths.push(req.url ?? "");
      if (req.url === "/99999.deadbeef.js") {
        res.writeHead(200, { "Content-Type": "application/javascript" });
        res.end("import('./nested.js'); const hiddenChunk = true;");
      } else if (req.url === "/nested.js") {
        res.writeHead(200, { "Content-Type": "application/javascript" });
        res.end("const recursiveChunk = true;");
      } else {
        res.writeHead(404);
        res.end("not found");
      }
    });

    let serve: ChildProcess | undefined;
    let serveStdout = "";
    let serveStderr = "";
    try {
      const fixturePort = await listen(fixture);
      const fixtureOrigin = `http://127.0.0.1:${fixturePort}`;
      const init = spawn(
        binary!,
        [
          "init",
          "--project",
          "foo",
          "--scope",
          "http://127.0.0.1:*",
          "--format",
          "json",
        ],
        {
          cwd: target,
          stdio: ["ignore", "pipe", "pipe"],
          timeout: 15000,
        },
      );
      let initStdout = "";
      let initStderr = "";
      init.stdout.on("data", (chunk) => (initStdout += chunk));
      init.stderr.on("data", (chunk) => (initStderr += chunk));
      const initExit = await new Promise<number | null>((resolve, reject) => {
        init.once("error", reject);
        init.once("exit", resolve);
      });
      expect(initStderr).toBe("");
      expect(initExit).toBe(0);
      expect(JSON.parse(initStdout).result.output).toBe(
        path.join(target, "jxscout"),
      );

      const portProbe = createServer();
      const servePort = await listen(portProbe);
      await new Promise<void>((resolve, reject) => {
        portProbe.close((error) => (error ? reject(error) : resolve()));
      });
      serve = spawn(
        binary!,
        [
          "serve",
          "-hostname",
          "127.0.0.1",
          "-port",
          String(servePort),
          "-webcrack",
          "off",
          "-rate-limiter-max-requests-per-second",
          "0",
          "-chunk-discoverer-bruteforce-limit",
          "0",
        ],
        { cwd: nested, stdio: ["ignore", "pipe", "pipe"] },
      );
      serve.stdout.on("data", (chunk) => (serveStdout += chunk));
      serve.stderr.on("data", (chunk) => (serveStderr += chunk));
      await waitForListener(serve, servePort, () => serveStderr);

      expect(serveStderr).toContain("Waiting for Burp/Caido traffic");
      expect(serveStderr).toContain(path.join(target, "jxscout"));
      const capturedBody = '  const table={99999:"deadbeef"};\r\n';
      // Match the upstream Burp extension, including its omitted requestUrl port.
      const ingest = await fetch(`http://127.0.0.1:${servePort}/caido-ingest`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          requestUrl: "http://127.0.0.1/7.aabbccdd.js",
          request: `GET /7.aabbccdd.js HTTP/1.1\r\nUser-Agent: Burp\r\nHost: 127.0.0.1:${fixturePort}\r\n\r\n`,
          response:
            "HTTP/2 200\r\ncontent-type: application/javascript\r\n\r\n" +
            capturedBody,
        }),
      });
      expect(ingest.status).toBe(202);

      const savedChunk = path.join(
        target,
        "jxscout",
        "original",
        `127.0.0.1:${fixturePort}`,
        "99999.deadbeef.js",
      );
      const deadline = Date.now() + 15000;
      while (
        Date.now() < deadline &&
        (!requestedPaths.includes("/99999.deadbeef.js") ||
          !existsSync(savedChunk) ||
          !requestedPaths.includes("/nested.js"))
      ) {
        if (serve.exitCode !== null || serve.signalCode !== null)
          throw new Error(
            `serve exited during event processing: ${serveStderr}\n${serveStdout}`,
          );
        await new Promise((resolve) => setTimeout(resolve, 100));
      }
      expect(requestedPaths).toContain("/99999.deadbeef.js");
      expect(existsSync(savedChunk)).toBe(true);
      expect(requestedPaths).toContain("/nested.js");
      const snapshot = path.join(
        target,
        "jxscout",
        "raw",
        createHash("sha256").update(capturedBody).digest("hex"),
        `127.0.0.1:${fixturePort}`,
        "7.aabbccdd.js",
      );
      expect(readFileSync(snapshot, "utf8")).toBe(capturedBody);
      expect(
        existsSync(
          path.join(target, "jxscout", ".state", "foo", "db", "db.sql"),
        ),
      ).toBe(true);
    } finally {
      if (serve && serve.exitCode === null && serve.signalCode === null) {
        serve.kill("SIGINT");
        if (!(await waitForExit(serve, 3000))) {
          serve.kill("SIGKILL");
          await waitForExit(serve, 3000);
        }
      }
      await new Promise<void>((resolve) => {
        fixture.close(() => resolve());
      });
      rmSync(temporary, { recursive: true, force: true });
    }
  },
  30000,
);
