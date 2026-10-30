import fs from "node:fs";
const out = "pkg/agent-cli/dist";
const hash = new Bun.CryptoHasher("sha256");
for await (const file of new Bun.Glob(
  "pkg/{ast-analyzer,chunk-discoverer,agent-cli}/**/*.ts",
).scan("."))
  hash.update(await Bun.file(file).text());
const revision = hash.digest("hex");
const result = await Bun.build({
  entrypoints: ["pkg/agent-cli/index.ts"],
  target: "bun",
  define: { __JXSCOUT_BUILD__: JSON.stringify(revision) },
  outdir: out,
  naming: { entry: "agent-cli.js", asset: "[name]-[hash].[ext]" },
});
if (!result.success)
  throw new AggregateError(result.logs, "Agent CLI build failed");
const entry = out + "/agent-cli.js";
const code = fs.readFileSync(entry, "utf8");
// napi-rs' override branch assigns to an outer variable, then returns undefined.
fs.writeFileSync(
  entry,
  code
    .replaceAll("\r\n", "\n")
    .replace(
      "nativeBinding = __require(process.env.NAPI_RS_NATIVE_LIBRARY_PATH);",
      "return __require(process.env.NAPI_RS_NATIVE_LIBRARY_PATH);",
    ),
);
const compact = await Bun.build({
  entrypoints: [entry],
  target: "bun",
  outdir: out,
  naming: { entry: "agent-cli.js" },
  external: ["*.node"],
  minify: true,
});
if (!compact.success)
  throw new AggregateError(compact.logs, "Agent CLI compression failed");

const chunkOut = "internal/modules/chunk-discoverer";
const chunks = await Bun.build({
  entrypoints: ["pkg/chunk-discoverer/index.ts"],
  target: "bun",
  outdir: chunkOut,
  naming: { entry: "chunk-discoverer.js" },
  minify: true,
});
if (!chunks.success)
  throw new AggregateError(chunks.logs, "Chunk discoverer build failed");
for (const file of [entry, chunkOut + "/chunk-discoverer.js"])
  fs.writeFileSync(
    file,
    fs.readFileSync(file, "utf8").replaceAll("\r\n", "\n"),
  );
