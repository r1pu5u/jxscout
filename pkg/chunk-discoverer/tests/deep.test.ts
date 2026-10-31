import { expect, test } from "vitest";
import { deepDiscover, inferTemplate } from "../discoverer/deep";
test("template inference only changes filename components", () => {
  expect(inferTemplate("https://h/4821/4821.a8bc39de.js?v=4821")).toBe(
    "https://h/4821/{id}.{hash}.js?v=4821",
  );
});
test("hidden map without loader fingerprint", () => {
  const c = deepDiscover(
    'const x={987654:"deadbeef", "settings":"cafebabe"}',
    ["https://h/{id}.{hash}.js"],
    0,
  );
  expect(c.map((c) => c.specifier)).toEqual([
    "https://h/987654.deadbeef.js",
    "https://h/settings.cafebabe.js",
  ]);
});
test("runtime keys exceed brute force limit and include aliases", () => {
  const c = deepDiscover(
    'const hashes={99999:"deadbeef",settings:"cafebabe"};r.p="https://cdn/";r.u=id=>"js/"+id+"."+hashes[id]+".js"',
    [],
    2,
  );
  expect(c.map((c) => c.specifier)).toEqual([
    "https://cdn/js/99999.deadbeef.js",
    "https://cdn/js/settings.cafebabe.js",
  ]);
});
test("imports and multiple runtimes merge", () => {
  const c = deepDiscover(
    'import("./lazy.js"); a.u=id=>"a/"+id+".js"; b.u=id=>"b/"+id+".js"',
    [],
    1,
  );
  expect(c.map((c) => c.specifier)).toEqual(["./lazy.js", "a/0.js", "b/0.js"]);
});
test("constant encoded tables are decoded without executing target code", () => {
  const encoded = Buffer.from(JSON.stringify({ long_id: "cafebabe" })).toString(
    "base64",
  );
  const result = deepDiscover(
    `const table=JSON.parse(atob('${encoded}')); r.u=id=>id+'.'+table[id]+'.js'`,
    [],
    0,
  );
  expect(result.map((c) => c.specifier)).toEqual(["long_id.cafebabe.js"]);
});
test("runtime public paths do not bleed into other runtimes", () => {
  const result = deepDiscover(
    'a.p="/one/";a.u=id=>id+".js";b.p="/two/";b.u=id=>id+".js"',
    [],
    1,
  );
  expect(result.map((c) => c.specifier)).toEqual(["/one/0.js", "/two/0.js"]);
});
test("named seed filenames can establish a template", () => {
  expect(inferTemplate("https://h/vendors.a8bc39de.js")).toBe(
    "https://h/{id}.{hash}.js",
  );
});
test("unresolved runtime builders retain source evidence", () => {
  const diagnostics: { expression: string; start: number }[] = [];
  deepDiscover('r.u=id=>externalState[id]+".js"', [], 1, diagnostics);
  expect(diagnostics).toHaveLength(1);
  expect(diagnostics[0].expression).toContain("externalState");
});
test("function aliases resolve known hash keys", () => {
  const found = deepDiscover(
    'const h={99999:"deadbeef"};function filename(id){return id+"."+h[id]+".js"}r.u=filename',
    [],
    0,
  );
  expect(found.map((c) => c.specifier)).toEqual(["99999.deadbeef.js"]);
});
test("precache object manifests use literal URLs instead of revision as an ID", () => {
  const candidates = deepDiscover(
    'self.__precacheManifest=[{revision:"958b322be5e6e870bba9",url:"/js/Dashboard.8f210c78.js"},{revision:"1122334455667788",url:"/css/app.css"}];',
    ["https://h/js/{id}.{hash}.js"],
    0,
  );
  expect(candidates.map((c) => c.specifier)).toEqual([
    "/js/Dashboard.8f210c78.js",
  ]);
});
test("ordinary application object strings are not chunk hashes", () => {
  const result = deepDiscover(
    'const opts={prefix:"theme-one",staticClass:"vue-street-view-pano",key:"language",expression:"showMonthView"};',
    ["https://h/js/{id}.{hash}.js"],
    0,
  );
  expect(result).toEqual([]);
});
test("named chunk map does not double the chunk prefix", () => {
  const result = deepDiscover(
    'const hash={"chunk-123":"deadbeef"};',
    ["https://h/js/chunk-{id}.{hash}.js"],
    0,
  );
  expect(result.map((c) => c.specifier)).toContain(
    "https://h/js/chunk-123.deadbeef.js",
  );
  expect(result.some((c) => c.specifier.includes("chunk-chunk"))).toBe(false);
});
test("CSS hash tables do not generate JavaScript requests", () => {
  const code =
    'function css(id){return "css/"+id+"."+{Dashboard:"11111111"}[id]+".css"} function js(id){return "js/"+id+"."+{Dashboard:"22222222"}[id]+".js"}';
  const urls = deepDiscover(code, ["https://h/js/{id}.{hash}.js"], 0).map(
    (c) => c.specifier,
  );
  expect(urls).toEqual(["https://h/js/Dashboard.22222222.js"]);
});
