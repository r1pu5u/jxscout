import { isJavaScript } from "./validate";
import { deepDiscover, inferTemplate } from "./discoverer/deep";
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

  const code = path === "--stdin" ? fs.readFileSync(0, "utf8") : readFile(path);

  const urls: string[] = process.argv[4] ? JSON.parse(process.argv[4]) : [];
  const templates = urls.map(inferTemplate).filter((t): t is string => !!t);
  const chunks = [
    ...new Set([
      ...discoverChunks(code, bruteForceLimit),
      ...deepDiscover(code, templates, bruteForceLimit).map((c) => c.specifier),
    ]),
  ];

  chunks.forEach((chunk) => console.log(chunk));

  process.exit(0);
} catch (error) {
  // Log the error and exit with error code
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
}
