import { isJavaScript } from "./validate";
import { inferTemplate } from "./discoverer/deep";
import * as fs from "fs";
import { discoverChunks } from "./discoverer/discoverer";

function parseArgs(): { path: string; bruteForceLimit: number } {
  const [path, bruteForceLimitArg] = process.argv.slice(2);

  if (!path || !bruteForceLimitArg) {
    throw new Error("Both path and brute force limit arguments are required.");
  }

  const bruteForceLimit = parseInt(bruteForceLimitArg, 10);
  if (isNaN(bruteForceLimit)) {
    throw new Error("Brute force limit must be a valid number.");
  }

  return { path, bruteForceLimit };
}

function readFile(path: string): string {
  return fs.readFileSync(path, "utf-8");
}

if (process.argv[2] === "--validate") {
  const valid = isJavaScript(fs.readFileSync(0, "utf8"));
  process.stdout.write(JSON.stringify({ valid }));
  process.exit(0);
}

try {
  const { path, bruteForceLimit } = parseArgs();

  const input =
    path === "--stdin" || path === "--stream"
      ? fs.readFileSync(0, "utf8")
      : readFile(path);
  const boundary = path === "--stream" ? input.indexOf("\n") : -1;
  if (path === "--stream" && boundary < 0)
    throw Error("Missing discovery stream metadata");
  const code = path === "--stream" ? input.slice(boundary + 1) : input;

  const urls: string[] =
    path === "--stream"
      ? JSON.parse(input.slice(0, boundary))
      : process.argv[4]
        ? JSON.parse(process.argv[4])
        : [];
  const templates = [
    ...new Set(urls.map(inferTemplate).filter((t): t is string => !!t)),
  ];
  const chunks = discoverChunks(code, bruteForceLimit, templates);

  chunks.forEach((chunk) => console.log(chunk));

  process.exit(0);
} catch (error) {
  // Log the error and exit with error code
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
