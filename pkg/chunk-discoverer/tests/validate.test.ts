import { test, expect } from "vitest";
import { isJavaScript } from "../validate";

test.each([
  "const x=1; import('./nested.js');",
  "export const chunk = ()=>42;",
  "(self.webpackChunk=self.webpackChunk||[]).push([[123],{123:function(){}}]);",
  "await import('./chunk.js');",
])("validates JavaScript without executing it: %s", (source) => {
  expect(isJavaScript(source)).toBe(true);
});

test.each([
  "<html><script>location='/login'</script></html>",
  '{"error":"not authorized"}',
  '["API","response"]',
  "body { color: red; }",
  "Not a JavaScript file",
  "const invalid = ;",
  "\0PNG\0",
  "",
  "// empty response",
])("rejects non-JavaScript and broken responses: %s", (source) => {
  expect(isJavaScript(source)).toBe(false);
});
