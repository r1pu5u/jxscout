import { parse } from "acorn";

// Parse only. Target scripts are never executed to validate a response.
export function isJavaScript(source: string): boolean {
  if (
    !source.trim() ||
    source.includes("\0") ||
    /^\s*(?:<!doctype|<html|<head|<body)/i.test(source)
  )
    return false;
  try {
    JSON.parse(source);
    return false;
  } catch {
    /* JSON responses are not script assets. */
  }
  for (const sourceType of ["module", "script"] as const) {
    try {
      const ast = parse(source, {
        ecmaVersion: "latest",
        sourceType,
        allowHashBang: true,
        allowAwaitOutsideFunction: true,
      });
      if (ast.body.length > 0) return true;
    } catch {
      /* Try the other JavaScript grammar. */
    }
  }
  return false;
}
