import fs from "node:fs";
import { createHash } from "node:crypto";
declare const __JXSCOUT_BUILD__: string;
const buildVersion =
  typeof __JXSCOUT_BUILD__ === "string" ? __JXSCOUT_BUILD__ : "development";
import path from "node:path";
import os from "node:os";
import { pathToFileURL, fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { Database } from "bun:sqlite";
import {
  deepDiscover,
  inferTemplate,
} from "../chunk-discoverer/discoverer/deep";
import { discoverChunks } from "../chunk-discoverer/discoverer/discoverer";
import { analyzeFile } from "../ast-analyzer/analyzer";

const help = `JXScout — JavaScript discovery and analysis

  init --project NAME --scope GLOB  Initialize ./jxscout in this directory
  serve [--project NAME] Open the capture dashboard and receive Burp/Caido traffic
    --headless           Run capture without the dashboard
  assets|chunks|findings|runs  Query project results
  doctor                 Check processing dependencies
  analyze <path>         Optional analysis of files already on disk

Start serve, connect the Burp extension to localhost:3333, and browse your target.

Options: --project NAME --format text|json|jsonl --webcrack auto|off|required
  --chunk-template 'https://host/assets/{id}.{hash}.js' (repeatable)
  --public-path URL --base-url URL --fetch --max-assets 10000
  --bruteforce-limit 3000 --search TEXT --run ID --page 1 --page-size 50
  --data-dir PATH (override artifact and state storage)
  --header 'Name: value' (repeatable) --scope GLOB (repeatable)
`;
const { values: v, positionals } = parseArgs({
  allowPositionals: true,
  options: {
    "data-dir": { type: "string", default: process.env.JXSCOUT_DATA_DIR },
    project: { type: "string", short: "p" },
    "project-name": { type: "string" },
    format: { type: "string", default: "text" },
    webcrack: { type: "string", default: "auto" },
    "chunk-template": { type: "string", multiple: true },
    "public-path": { type: "string" },
    "base-url": { type: "string" },
    fetch: { type: "boolean" },
    "max-assets": { type: "string", default: "10000" },
    "bruteforce-limit": { type: "string", default: "3000" },
    search: { type: "string", default: "" },
    run: { type: "string" },
    page: { type: "string", default: "1" },
    "page-size": { type: "string", default: "50" },
    header: { type: "string", multiple: true },
    scope: { type: "string", short: "s", multiple: true },
    help: { type: "boolean" },
    force: { type: "boolean" },
  },
});
v.project ??= v["project-name"];
const command = positionals.shift();
if (command === "__discover") {
  const { templates, limit } = JSON.parse(await Bun.stdin.text());
  const code = fs.readFileSync(positionals[0], "utf8");
  const unresolved: { expression: string; start: number }[] = [];
  const candidates = deepDiscover(code, templates, limit, unresolved);
  const errors: string[] = [];
  try {
    for (const specifier of discoverChunks(code, limit))
      if (!candidates.some((c) => c.specifier === specifier))
        candidates.push({ specifier, method: "legacy" });
  } catch (e) {
    errors.push(String(e));
  }
  console.log(JSON.stringify({ candidates, errors, unresolved }));
  process.exit(0);
}

function emit(result: any, type = "result") {
  if (v.format === "text")
    console.log(
      typeof result === "string" ? result : JSON.stringify(result, null, 2),
    );
  else console.log(JSON.stringify({ schema_version: 1, type, result }));
}
if (!command || v.help || command === "help") {
  emit(help);
  process.exit(0);
}
function findWorkspace(
  start: string,
): { root: string; project: string; scope: string[] } | undefined {
  let current = path.resolve(start);
  for (;;) {
    const candidates = [path.join(current, "jxscout", "config.json")];
    if (path.basename(current) === "jxscout")
      candidates.unshift(path.join(current, "config.json"));
    for (const file of candidates)
      if (fs.existsSync(file)) {
        const config = JSON.parse(fs.readFileSync(file, "utf8"));
        if (
          config.version !== 1 ||
          typeof config.project !== "string" ||
          !Array.isArray(config.scope) ||
          !config.scope.every((s: any) => typeof s === "string")
        )
          throw Error(`Invalid JXScout workspace configuration: ${file}`);
        return {
          root: path.dirname(file),
          project: config.project,
          scope: config.scope,
        };
      }
    const parent = path.dirname(current);
    if (parent === current) return;
    current = parent;
  }
}
const projectsFile =
  process.env.JXSCOUT_PROJECTS_FILE ??
  path.join(os.homedir(), ".config", "jxscout", "projects.json");
function readProjects(): {
  version: number;
  projects: Record<string, { name: string; root: string; scope: string[] }>;
} {
  if (!fs.existsSync(projectsFile)) return { version: 1, projects: {} };
  const registry = JSON.parse(fs.readFileSync(projectsFile, "utf8"));
  if (registry.version !== 1 || !registry.projects)
    throw Error(`Invalid project registry: ${projectsFile}`);
  return registry;
}
function registerProject(name: string, root: string, scope: string[]) {
  const registry = readProjects();
  registry.projects[name] = { name, root, scope };
  fs.mkdirSync(path.dirname(projectsFile), { recursive: true });
  const temp = `${projectsFile}.${process.pid}.tmp`;
  try {
    fs.writeFileSync(temp, JSON.stringify(registry, null, 2) + "\n", {
      mode: 0o600,
    });
    fs.renameSync(temp, projectsFile);
  } finally {
    fs.rmSync(temp, { force: true });
  }
}
if (command === "tui")
  throw Error(
    "The tui command was removed. Run 'jxscout serve' to open the capture dashboard.",
  );
if (command === "scan")
  throw Error(
    "The scan command was removed. Run 'jxscout serve' and browse the target through Burp/Caido to capture JavaScript automatically.",
  );
if (
  ![
    "init",
    "doctor",
    "assets",
    "chunks",
    "findings",
    "runs",
    "analyze",
  ].includes(command)
)
  throw Error(`Unknown command: ${command}`);
let workspace = findWorkspace(process.cwd());
if (command !== "init" && v.project && !v["data-dir"]) {
  const saved = readProjects().projects[v.project];
  if (saved) {
    const marker = path.join(saved.root, "config.json");
    if (!fs.existsSync(marker))
      throw Error(
        `Project ${v.project} output folder is missing its config; initialize it again: ${saved.root}`,
      );
    const config = JSON.parse(fs.readFileSync(marker, "utf8"));
    workspace = {
      root: saved.root,
      project: v.project,
      scope: config.project === v.project ? config.scope : saved.scope,
    };
  }
}
if (command === "init") {
  const target =
    workspace?.root === process.cwd()
      ? workspace.root
      : path.join(process.cwd(), "jxscout");
  const file = path.join(target, "config.json");
  const existing = fs.existsSync(file)
    ? JSON.parse(fs.readFileSync(file, "utf8"))
    : undefined;
  const project =
    v.project ?? existing?.project ?? path.basename(process.cwd());
  const scope = v.scope ?? existing?.scope ?? [];
  if (!/^[\w.-]+$/.test(project) || project === ".." || project === ".")
    throw Error("Invalid project name; supply --project NAME");
  const registered = readProjects().projects[project];
  if (registered && path.resolve(registered.root) !== target && !v.force)
    throw Error(
      `Project ${project} is already initialized at ${registered.root}; use --force to move its output folder`,
    );
  if (existing && !v.force) {
    if (
      existing.project !== project ||
      JSON.stringify(existing.scope) !== JSON.stringify(scope)
    )
      throw Error(
        "Workspace already initialized; use --force to update its settings",
      );
    registerProject(project, target, scope);
    emit({
      status: "already_initialized",
      project,
      scope,
      output: target,
      config: file,
    });
    process.exit(0);
  }
  fs.mkdirSync(target, { recursive: true });
  fs.writeFileSync(
    file,
    JSON.stringify({ version: 1, project, scope }, null, 2) + "\n",
    { flag: v.force ? "w" : "wx" },
  );
  registerProject(project, target, scope);
  emit({ status: "initialized", project, scope, output: target, config: file });
  process.exit(0);
}
v.project ??= workspace?.project ?? "default";
v.scope ??= workspace?.scope;
const projectRoot = v["data-dir"]
  ? undefined
  : (workspace?.root ?? process.env.JXSCOUT_PROJECT_ROOT);
if (
  !["text", "json", "jsonl"].includes(v.format!) ||
  !["auto", "off", "required"].includes(v.webcrack!)
)
  throw Error("Invalid format or webcrack mode");
for (const key of ["max-assets", "bruteforce-limit", "page", "page-size"])
  if (
    !/^\d+$/.test(v[key] as string) ||
    (key !== "bruteforce-limit" && Number(v[key]) < 1)
  )
    throw Error(`Invalid ${key}`);
if (!/^[\w.-]+$/.test(v.project!) || v.project === ".." || v.project === ".")
  throw Error("Invalid project name");
const tools = Object.fromEntries(
  ["bun", "prettier", "webcrack"].map((name) => [name, Bun.which(name)]),
);
if (command === "doctor") {
  emit({ tools, node: Bun.which("node") });
  process.exit(0);
}
const root = v["data-dir"]
  ? path.join(v["data-dir"], v.project!)
  : (projectRoot ?? path.join(os.homedir(), "jxscout", v.project!));
const privateRoot = v["data-dir"]
  ? path.join(v["data-dir"], ".state", v.project!)
  : projectRoot
    ? path.join(projectRoot, ".state", v.project!)
    : path.join(os.homedir(), ".jxscout", v.project!);
fs.mkdirSync(privateRoot, { recursive: true });
const db = new Database(path.join(privateRoot, "agent.sqlite"));
db.exec(`PRAGMA journal_mode=WAL; CREATE TABLE IF NOT EXISTS runs(id TEXT PRIMARY KEY,status TEXT,started TEXT,finished TEXT,errors TEXT);
CREATE TABLE IF NOT EXISTS artifacts(id TEXT,run TEXT,url TEXT,path TEXT,kind TEXT,parent TEXT,PRIMARY KEY(id,run));
CREATE TABLE IF NOT EXISTS chunks(run TEXT,parent TEXT,url TEXT,method TEXT,status TEXT,evidence TEXT);
CREATE TABLE IF NOT EXISTS findings(run TEXT,artifact TEXT,result TEXT);`);
if (["runs", "assets", "chunks", "findings"].includes(command)) {
  const table = command === "assets" ? "artifacts" : command;
  const rows = db
    .query(`SELECT * FROM ${table}`)
    .all()
    .filter(
      (r: any) =>
        (!v.run || r.run === v.run || r.id === v.run) &&
        JSON.stringify(r).includes(v.search!),
    );
  const legacyPath = path.join(privateRoot, "db", "db.sql");
  if (fs.existsSync(legacyPath) && command !== "runs" && !v.run) {
    const legacy = new Database(legacyPath, { readonly: true });
    try {
      const existing: any[] =
        command === "assets"
          ? legacy
              .query(
                "SELECT id,url,fs_path AS path,content_type AS kind FROM assets",
              )
              .all()
          : command === "findings"
            ? legacy
                .query(
                  "SELECT asset_id AS artifact,results AS result,asset_type AS kind FROM ast_analysis_results WHERE deleted_at IS NULL AND asset_type != 'webcrack'",
                )
                .all()
            : legacy
                .query(
                  "SELECT id,url,chunk_from_asset_id AS parent FROM assets WHERE is_chunk_discovered=1",
                )
                .all();
      if (
        command === "chunks" &&
        legacy
          .query(
            "SELECT name FROM sqlite_master WHERE type='table' AND name='chunk_discoveries'",
          )
          .get()
      ) {
        for (const attempt of legacy
          .query(
            "SELECT source_asset_id AS parent,url,status,detail FROM chunk_discoveries WHERE status != 'existing'",
          )
          .all() as any[]) {
          const saved = existing.find((row) => row.url === attempt.url);
          if (saved) Object.assign(saved, attempt);
          else existing.push(attempt);
        }
      }
      rows.push(
        ...existing
          .filter((r) => JSON.stringify(r).includes(v.search!))
          .map((r) => ({ ...r, source: "proxy" })),
      );
    } finally {
      legacy.close();
    }
  }
  const start = (Number(v.page) - 1) * Number(v["page-size"]);
  emit({
    total: rows.length,
    page: Number(v.page),
    items: rows.slice(start, start + Number(v["page-size"])),
  });
  process.exit(0);
}
if (command !== "analyze") throw Error(`Unknown command: ${command}`);
if (v.webcrack === "required" && !tools.webcrack)
  throw Error("webcrack is required but not installed");
const webcrackVersion =
  tools.webcrack && v.webcrack !== "off"
    ? Bun.spawnSync([tools.webcrack, "--version"]).stdout.toString().trim()
    : "off";
const run = crypto.randomUUID();
const errors: string[] = [];
const interrupted = () => {
  db.query("UPDATE runs SET status=?,finished=?,errors=? WHERE id=?").run(
    "interrupted",
    new Date().toISOString(),
    JSON.stringify(errors),
    run,
  );
  process.exit(130);
};
process.on("SIGINT", interrupted);
process.on("SIGTERM", interrupted);

let seeds = [...positionals];
if (!seeds.length)
  throw Error(
    "A local file or directory is required. Use 'jxscout serve' to capture browser traffic.",
  );
function localFiles(p: string): string[] {
  return fs.statSync(p).isDirectory()
    ? fs
        .readdirSync(p, { withFileTypes: true })
        .flatMap((e) =>
          e.isSymbolicLink() ? [] : localFiles(path.join(p, e.name)),
        )
    : /\.(?:m?js|cjs|html?)$/i.test(p)
      ? [pathToFileURL(path.resolve(p)).href]
      : [];
}
seeds = seeds.flatMap(localFiles);
if (!seeds.length) throw Error("No supported source files found");
const localRoots = seeds
  .filter((s) => s.startsWith("file:"))
  .map((s) => path.dirname(fs.realpathSync(fileURLToPath(s))));
function allowedLocalFile(url: URL): boolean {
  const resolved = fs.realpathSync(fileURLToPath(url));
  return localRoots.some((root) => {
    const relative = path.relative(root, resolved);
    return (
      relative !== ".." &&
      !relative.startsWith(".." + path.sep) &&
      !path.isAbsolute(relative)
    );
  });
}
function matchesScope(url: string | URL): boolean {
  const parsed = typeof url === "string" ? new URL(url) : url;
  if (parsed.protocol === "file:") return true;
  if (!v.scope?.length) return true;
  const href = typeof url === "string" ? new URL(url).href : url.href;
  return v.scope.some((pattern) =>
    new RegExp(
      "^" +
        pattern
          .split("*")
          .map((part) => part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))
          .join(".*") +
        "$",
    ).test(href),
  );
}
const excludedSeeds = seeds.filter((seed) => !matchesScope(seed));
if (excludedSeeds.length)
  throw Error(`Seed URL outside --scope: ${excludedSeeds.join(", ")}`);
db.query("INSERT INTO runs VALUES(?,?,?,?,?)").run(
  run,
  "running",
  new Date().toISOString(),
  null,
  "[]",
);
const templates = new Set(v["chunk-template"] ?? []);
for (const seed of seeds) {
  const t = inferTemplate(seed);
  if (t) templates.add(t);
}
const queue = seeds.map((url) => ({ url, parent: "", method: "seed" }));
const seen = new Set<string>();
const outcomes = new Map<string, string>();
const queued = new Set(queue.map((item) => item.url));
const sources: { url: string; body: string; id: string }[] = [];
const headers = Object.fromEntries(
  (v.header ?? []).map((h) => {
    const i = h.indexOf(":");
    if (i < 1) throw Error("Invalid header");
    return [h.slice(0, i).trim(), h.slice(i + 1).trim()];
  }),
);
const origin = new URL(seeds[0]).origin;
let lastRequest = 0;
function schedule(
  url: string,
  parent: string,
  method: string,
  evidence: any = {},
) {
  try {
    const target = new URL(url);
    target.hash = "";
    if (!["http:", "https:", "file:"].includes(target.protocol)) return;
    if (target.protocol === "file:" && !allowedLocalFile(target)) return;
    if (target.protocol !== "file:" && !v.fetch) return;
    if (!matchesScope(target)) return;
    db.query("INSERT INTO chunks VALUES(?,?,?,?,?,?)").run(
      run,
      parent,
      target.href,
      method,
      outcomes.get(target.href) ?? "pending",
      JSON.stringify(evidence),
    );
    if (!seen.has(target.href) && !queued.has(target.href)) {
      queued.add(target.href);
      queue.push({ url: target.href, parent, method });
    }
  } catch (e) {
    errors.push(String(e));
  }
}
async function discover(body: string, url: string, id: string) {
  const child = Bun.spawn(
    [
      process.execPath,
      fileURLToPath(import.meta.url),
      "__discover",
      path.join(root, "artifacts", id, "source.js"),
    ],
    {
      stdin: new Blob([
        JSON.stringify({
          templates: [...templates],
          limit: Number(v["bruteforce-limit"]),
        }),
      ]),
      stdout: "pipe",
      stderr: "pipe",
      timeout: 30000,
    },
  );
  const [stdout, stderr, exit] = await Promise.all([
    new Response(child.stdout).text(),
    new Response(child.stderr).text(),
    child.exited,
  ]);
  if (exit) {
    errors.push(
      `chunk discovery ${url}: ${stderr || "process failed or timed out"}`,
    );
    return;
  }
  const {
    candidates,
    errors: diagnostics,
    unresolved = [],
  } = JSON.parse(stdout);
  for (const diagnostic of unresolved)
    db.query("INSERT INTO chunks VALUES(?,?,?,?,?,?)").run(
      run,
      id,
      url,
      "webpack-runtime",
      "unresolved",
      JSON.stringify(diagnostic),
    );
  errors.push(
    ...diagnostics.map((error: string) => `chunk discovery ${url}: ${error}`),
  );
  for (const c of candidates) {
    try {
      schedule(
        new URL(
          c.specifier,
          v["public-path"] ??
            (url.startsWith("file:") && v["base-url"] ? v["base-url"] : url),
        ).href,
        id,
        c.method,
        c,
      );
    } catch {}
  }
}
async function analyze(
  body: string,
  url: string,
  parent: string,
  kind = "raw",
) {
  const id = createHash("sha256")
    .update(
      url +
        "\0" +
        body +
        "\0" +
        kind +
        "\0" +
        buildVersion +
        "\0" +
        webcrackVersion,
    )
    .digest("hex");
  const dir = path.join(root, "artifacts", id);
  fs.mkdirSync(dir, { recursive: true });
  const file = path.join(dir, "source.js");
  const previous: any = db
    .query(
      "SELECT a.run FROM artifacts a JOIN runs r ON r.id=a.run WHERE a.id=? AND a.run<>? AND r.status='completed' LIMIT 1",
    )
    .get(id, run);
  if (!previous || !fs.existsSync(file)) {
    fs.writeFileSync(file, body);
    let usedWebcrack = false;
    if (kind === "raw" && tools.webcrack && v.webcrack !== "off") {
      try {
        const child = Bun.spawn([tools.webcrack, file, "--no-unpack"], {
          stdout: "pipe",
          stderr: "pipe",
          timeout: 120000,
        });
        const [code, stderr, exitCode] = await Promise.all([
          new Response(child.stdout).text(),
          new Response(child.stderr).text(),
          child.exited,
        ]);
        if (exitCode !== 0 || !code.trim())
          throw Error(
            stderr.slice(-2000) || `webcrack exited with ${exitCode}`,
          );
        fs.writeFileSync(file, code);
        usedWebcrack = true;
      } catch (e) {
        if (v.webcrack === "required") errors.push(`webcrack ${url}: ${e}`);
      }
    }
    if (!usedWebcrack && tools.prettier) {
      const child = Bun.spawn(
        [tools.prettier, file, "--write", "--parser=babel"],
        { stdout: "ignore", stderr: "pipe", timeout: 30000 },
      );
      await new Response(child.stderr).text();
      await child.exited;
    }
  }
  db.query("INSERT OR IGNORE INTO artifacts VALUES(?,?,?,?,?,?)").run(
    id,
    run,
    url,
    file,
    kind,
    parent,
  );
  if (previous)
    db.query(
      "INSERT INTO findings SELECT ?,artifact,result FROM findings WHERE artifact=? AND run=?",
    ).run(run, id, previous.run);
  try {
    if (!previous)
      for (const finding of analyzeFile(file))
        db.query("INSERT INTO findings VALUES(?,?,?)").run(
          run,
          id,
          JSON.stringify(finding),
        );
  } catch (e) {
    errors.push(`${url}: ${e}`);
  }
  sources.push({ url, body, id });
  await discover(body, url, id);
  const formatted = fs.readFileSync(file, "utf8");
  if (formatted !== body) await discover(formatted, url, id);
  if (kind === "raw") {
    const reference = [
      ...body.matchAll(/[#@]\s*sourceMappingURL=([^\s*]+)/g),
    ].at(-1)?.[1];
    if (reference)
      try {
        let text: string;
        if (reference.startsWith("data:")) {
          const comma = reference.indexOf(",");
          text = reference.slice(0, comma).includes(";base64")
            ? Buffer.from(reference.slice(comma + 1), "base64").toString()
            : decodeURIComponent(reference.slice(comma + 1));
        } else {
          const target = new URL(reference, url);
          if (!["http:", "https:", "file:"].includes(target.protocol))
            throw Error("Unsupported source map URL");
          if (!matchesScope(target))
            throw Error(`Source map URL outside --scope: ${target.href}`);
          if (target.protocol === "file:") {
            if (!allowedLocalFile(target))
              throw Error("Source map outside local input roots");
            text = fs.readFileSync(fileURLToPath(target), "utf8");
          } else {
            if (!v.fetch) throw Error("Remote source map requires --fetch");
            await Bun.sleep(Math.max(0, 500 - (Date.now() - lastRequest)));
            lastRequest = Date.now();
            const response = await fetch(target, {
              redirect: "error",
              headers: target.origin === origin ? headers : {},
              signal: AbortSignal.timeout(30000),
            });
            if (!response.ok) throw Error(`HTTP ${response.status}`);
            text = await response.text();
          }
        }
        const map = JSON.parse(text);
        if (Array.isArray(map.sourcesContent))
          for (let i = 0; i < map.sourcesContent.length; i++)
            if (typeof map.sourcesContent[i] === "string")
              await analyze(
                map.sourcesContent[i],
                url,
                id,
                "sourcemap:" + String(map.sources?.[i] ?? i),
              );
      } catch (e) {
        errors.push(`source map ${url}: ${e}`);
      }
  }
}
while (queue.length && seen.size < Number(v["max-assets"])) {
  const item = queue.shift()!;
  if (seen.has(item.url)) continue;
  seen.add(item.url);
  try {
    let body: string;
    let effectiveUrl = item.url;
    if (item.url.startsWith("file:"))
      body = fs.readFileSync(fileURLToPath(item.url), "utf8");
    else {
      await Bun.sleep(Math.max(0, 500 - (Date.now() - lastRequest)));
      lastRequest = Date.now();
      let current = item.url;
      let response: Response | undefined;
      for (let i = 0; i < 6; i++) {
        if (!matchesScope(current))
          throw Error(`Redirect URL outside --scope: ${current}`);
        response = await fetch(current, {
          headers: new URL(current).origin === origin ? headers : {},
          signal: AbortSignal.timeout(30000),
          redirect: "manual",
        });
        if (
          response.status >= 300 &&
          response.status < 400 &&
          response.headers.has("location")
        ) {
          current = new URL(response.headers.get("location")!, current).href;
          if (!matchesScope(current))
            throw Error(`Redirect URL outside --scope: ${current}`);
          continue;
        }
        break;
      }
      if (!response?.ok) throw Error(`HTTP ${response?.status}`);
      effectiveUrl = current;
      body = await response.text();
    }
    outcomes.set(item.url, "downloaded");
    db.query("UPDATE chunks SET status=? WHERE run=? AND url=?").run(
      "downloaded",
      run,
      item.url,
    );
    const template = inferTemplate(effectiveUrl);
    if (template && !templates.has(template)) {
      templates.add(template);
      for (const source of sources)
        await discover(source.body, source.url, source.id);
    }
    if (/^\s*</.test(body)) {
      if (item.method !== "seed")
        throw Error("HTML response for JavaScript candidate");
      const { load } = await import("cheerio");
      const $ = load(body);
      const base = new URL(
        $("base[href]").first().attr("href") ?? effectiveUrl,
        effectiveUrl,
      ).href;
      $(
        'script[src],link[rel="modulepreload"],link[rel="preload"][as="script"]',
      ).each((_, e) => {
        const s = $(e).attr("src") ?? $(e).attr("href");
        if (s) schedule(new URL(s, base).href, item.url, "html");
      });
      for (const e of $("script:not([src])").toArray())
        if (!/json/.test($(e).attr("type") ?? ""))
          await analyze($(e).text(), base, item.url, "inline");
    } else await analyze(body, effectiveUrl, item.parent);
    if (v.format === "jsonl")
      emit({ run, url: item.url, pending: queue.length }, "progress");
  } catch (e) {
    errors.push(`${item.url}: ${e}`);
    outcomes.set(item.url, "failed");
    db.query("UPDATE chunks SET status=? WHERE run=? AND url=?").run(
      "failed",
      run,
      item.url,
    );
  }
}
if (queue.length) errors.push("Asset limit reached");
const status = errors.length ? "partial" : "completed";
db.query("UPDATE runs SET status=?,finished=?,errors=? WHERE id=?").run(
  status,
  new Date().toISOString(),
  JSON.stringify(errors),
  run,
);
emit(
  {
    run,
    status,
    visited: seen.size,
    artifacts: db
      .query("SELECT COUNT(*) AS count FROM artifacts WHERE run=?")
      .get(run),
    errors,
    output: root,
  },
  "summary",
);
process.exitCode = errors.length ? 2 : 0;
