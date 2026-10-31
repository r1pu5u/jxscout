import { parse } from "acorn-loose";
import { simple, full, fullAncestor } from "acorn-walk";
import type { Program } from "acorn";

export interface Candidate {
  specifier: string;
  method: string;
  id?: string;
  hash?: string;
  start?: number;
}
export function inferTemplate(raw: string): string | undefined {
  try {
    const u = new URL(raw);
    const match = u.pathname.match(/^(.*\/)([^/]+)\.(m?js)$/);
    if (!match) return;
    if (/^(?:precache-manifest|service-worker|sw)[.-]/i.test(match[2])) return;
    const parts = match[2].split(/([.-])/);
    let hash = parts.findIndex(
      (p) => /^[a-f\d]{8,64}$/i.test(p) && /[a-f]/i.test(p),
    );
    if (hash < 0)
      hash = parts.findIndex(
        (p) => /^[a-z\d_]{6,64}$/i.test(p) && /[a-z]/i.test(p) && /\d/.test(p),
      );
    let id = parts.findIndex((p, i) => i !== hash && /^\d+$/.test(p));
    if (id < 0 && hash > 0)
      id = parts
        .slice(0, hash)
        .findLastIndex((p) => p !== "." && p !== "-" && p !== "");
    if (hash < 0 || id < 0) return;
    if (!/^\d+$/.test(parts[id])) {
      // Named webpack IDs include prefixes such as chunk- or SS_.
      return new URL(match[1] + "{id}.{hash}." + match[3] + u.search, u).href
        .replaceAll("%7B", "{")
        .replaceAll("%7D", "}");
    }
    parts[id] = "{id}";
    parts[hash] = "{hash}";
    u.pathname = match[1] + parts.join("") + "." + match[3];
    return u.href.replace(/%7B/g, "{").replace(/%7D/g, "}");
  } catch {
    return;
  }
}

function isChunkHash(value: unknown): value is string {
  // Inferred filenames need strong evidence. Non-hex names remain supported
  // through explicit imports/manifests and evaluated runtime builders.
  return typeof value === "string" && /^[a-f\d]{8,64}$/i.test(value);
}
function isManifestRecord(node: any): boolean {
  return (
    node?.type === "ObjectExpression" &&
    node.properties.some(
      (p: any) =>
        p.type === "Property" &&
        ["url", "revision"].includes(String(p.key.name ?? p.key.value)),
    )
  );
}
function fromTemplate(template: string, id: string, hash: string): string {
  // A named chunk key already includes webpack's `chunk-` prefix.
  if (/chunk-\{id\}/.test(template) && id.startsWith("chunk-"))
    id = id.slice(6);
  return template
    .replaceAll("{id}", encodeURIComponent(id))
    .replaceAll("{hash}", encodeURIComponent(hash));
}

// Deliberately interprets only constant expressions, never the target program.
export function deepDiscover(
  code: string,
  templates: string[] = [],
  limit = 3000,
  unresolved: { expression: string; start: number }[] = [],
): Candidate[] {
  const ast: any = parse(code, { ecmaVersion: "latest", sourceType: "module" });
  return deepDiscoverAst(ast, code, templates, limit, unresolved);
}

/** Run deep discovery against an AST already parsed by the caller. */
export function deepDiscoverAst(
  ast: Program,
  code: string,
  templates: string[] = [],
  limit = 3000,
  unresolved: { expression: string; start: number }[] = [],
): Candidate[] {
  const cssMaps = new Set<number>();
  fullAncestor(ast, (n: any, _state: any, parents: any[]) => {
    if (n.type !== "ObjectExpression") return;
    if (
      parents.some(
        (p) =>
          p.type === "BinaryExpression" &&
          /["']\.css["']/.test(code.slice(p.start, p.end)),
      )
    )
      cssMaps.add(n.start);
  });
  const bindings = new Map<string, any>();
  const ids = new Set<string>();
  const candidates = new Map<string, Candidate>();
  const add = (value: any, method: string, node: any, extra = {}) => {
    if (
      typeof value === "string" &&
      /(?:^|\/)[^/?#]+\.(?:m?js|cjs)(?:[?#].*)?$/.test(value) &&
      !/[\n\r\0]/.test(value)
    )
      candidates.set(value, {
        specifier: value,
        method,
        start: node.start,
        ...extra,
      });
  };
  full(ast, (n: any) => {
    if (n.type === "FunctionDeclaration" && n.id) bindings.set(n.id.name, n);
    if (n.type === "VariableDeclarator" && n.id.type === "Identifier")
      bindings.set(n.id.name, n.init);
    if (n.type === "Property" && !n.computed && isChunkHash(n.value?.value))
      ids.add(String(n.key.name ?? n.key.value));
    if (
      n.type === "CallExpression" &&
      n.callee.type === "MemberExpression" &&
      n.callee.property.name === "e" &&
      n.arguments[0]?.type === "Literal"
    )
      ids.add(String(n.arguments[0].value));
  });
  const unknown = Symbol("unknown");
  const publicPaths = new Map<string, string>();
  const emptyEnv = new Map<string, any>();
  // Evaluation follows bindings, which can be cyclic or share large object
  // graphs. Cache per environment so runtime IDs never share context-sensitive
  // results. Keep an active set separate so cycles resolve to unknown.
  type EvaluationState = { values: WeakMap<object, any>; active: WeakSet<object> };
  const states = new WeakMap<Map<string, any>, EvaluationState>();
  const stateFor = (env: Map<string, any>): EvaluationState => {
    let state = states.get(env);
    if (!state) {
      state = { values: new WeakMap(), active: new WeakSet() };
      states.set(env, state);
    }
    return state;
  };
  function evaluate(n: any, env = emptyEnv, depth = 0): any {
    if (n && typeof n === "object") {
      const state = stateFor(env);
      if (state.values.has(n)) return state.values.get(n);
      if (state.active.has(n)) return unknown;
      state.active.add(n);
      let result: any;
      try {
        result = evaluateNode(n, env, depth);
      } catch {
        result = unknown;
      } finally {
        state.active.delete(n);
      }
      state.values.set(n, result);
      return result;
    }
    try {
      return evaluateNode(n, env, depth);
    } catch {
      return unknown;
    }
  }
  function evaluateNode(n: any, env: Map<string, any>, depth: number): any {
    if (!n || depth > 40) return unknown;
    const ev = (x: any) => evaluate(x, env, depth + 1);
    switch (n.type) {
      case "Literal":
        return n.value;
      case "Identifier":
        return env.has(n.name) ? env.get(n.name) : ev(bindings.get(n.name));
      case "ObjectExpression": {
        const obj = Object.create(null);
        for (const p of n.properties)
          if (p.type === "Property") {
            const key = p.computed ? ev(p.key) : (p.key.name ?? p.key.value);
            if (key !== unknown) obj[key] = ev(p.value);
          }
        return obj;
      }
      case "ArrayExpression":
        return n.elements.map(ev);
      case "MemberExpression": {
        const key = n.computed ? ev(n.property) : n.property.name;
        const receiver = code.slice(n.object.start, n.object.end);
        if (key === "p" && publicPaths.has(receiver))
          return publicPaths.get(receiver);
        // Runtime builders commonly index one entry from a large constant
        // hash table. Evaluate that property directly instead of rebuilding
        // the entire object for every runtime ID.
        const objectNode =
          n.object.type === "Identifier"
            ? bindings.get(n.object.name)
            : n.object;
        if (objectNode?.type === "ObjectExpression" && key !== unknown) {
          for (const prop of objectNode.properties) {
            if (prop.type !== "Property") continue;
            const propKey = prop.computed
              ? ev(prop.key)
              : (prop.key.name ?? prop.key.value);
            if (propKey === key) return ev(prop.value);
          }
          return unknown;
        }
        const obj = ev(n.object);
        return obj !== unknown &&
          obj != null &&
          key !== unknown &&
          Object.hasOwn(obj, key)
          ? obj[key]
          : unknown;
      }
      case "BinaryExpression": {
        const a = ev(n.left),
          b = ev(n.right);
        if (
          a === unknown ||
          b === unknown ||
          (a !== null && typeof a === "object") ||
          (b !== null && typeof b === "object")
        )
          return unknown;
        switch (n.operator) {
          case "+":
            return a + b;
          case "===":
            return a === b;
          case "==":
            return a == b;
          case "-":
            return a - b;
        }
        return unknown;
      }
      case "CallExpression": {
        const args = n.arguments.map(ev);
        if (args.some((a: any) => a === unknown)) return unknown;
        if (
          n.callee.type === "Identifier" &&
          n.callee.name === "atob" &&
          typeof args[0] === "string"
        )
          return Buffer.from(args[0], "base64").toString("utf8");
        if (n.callee.type !== "MemberExpression") return unknown;
        const key = n.callee.computed
          ? ev(n.callee.property)
          : n.callee.property.name;
        if (
          n.callee.object.type === "Identifier" &&
          n.callee.object.name === "JSON" &&
          key === "parse" &&
          typeof args[0] === "string" &&
          args[0].length < 1048576
        )
          return JSON.parse(args[0]);
        const receiver = ev(n.callee.object);
        if (
          typeof receiver === "string" &&
          ["concat", "slice", "substring"].includes(key) &&
          args.every((a: any) => ["string", "number"].includes(typeof a))
        )
          return (receiver as any)[key](...args);
        if (
          Array.isArray(receiver) &&
          key === "join" &&
          receiver.every((x) => ["string", "number"].includes(typeof x)) &&
          (args.length === 0 || typeof args[0] === "string")
        )
          return receiver.join(args[0]);
        return unknown;
      }
      case "LogicalExpression": {
        const a = ev(n.left);
        if (a === unknown) return unknown;
        return n.operator === "||"
          ? a || ev(n.right)
          : n.operator === "&&"
            ? a && ev(n.right)
            : (a ?? ev(n.right));
      }
      case "ConditionalExpression": {
        const t = ev(n.test);
        return t === unknown ? unknown : ev(t ? n.consequent : n.alternate);
      }
      case "TemplateLiteral": {
        let out = n.quasis[0].value.cooked;
        for (let i = 0; i < n.expressions.length; i++) {
          const v = ev(n.expressions[i]);
          if (v === unknown) return unknown;
          out += String(v) + n.quasis[i + 1].value.cooked;
        }
        return out;
      }
      default:
        return unknown;
    }
  }
  for (const [name, node] of bindings) {
    const value = evaluate(node);
    if (
      value &&
      value !== unknown &&
      typeof value === "object" &&
      !Array.isArray(value) &&
      !Object.hasOwn(value, "revision") &&
      !Object.hasOwn(value, "url") &&
      !cssMaps.has(node.start)
    )
      for (const [id, hash] of Object.entries(value)) {
        ids.add(id);
        if (isChunkHash(hash))
          for (const template of templates)
            add(
              fromTemplate(template, id, hash),
              "decoded-hash-template",
              node,
              { id, hash },
            );
      }
  }
  // The static pass has already emitted all data needed from global hash
  // maps. Drop their materialized JS objects; runtime lookups above are lazy.
  const emptyState = stateFor(emptyEnv);
  for (const node of bindings.values())
    if (node?.type === "ObjectExpression") emptyState.values.delete(node);
  full(ast, (n: any) => {
    if (
      n.type === "AssignmentExpression" &&
      n.left.type === "MemberExpression" &&
      (n.left.property.name ?? n.left.property.value) === "p"
    ) {
      const value = evaluate(n.right);
      if (typeof value === "string")
        publicPaths.set(
          code.slice(n.left.object.start, n.left.object.end),
          value,
        );
    }
  });
  // The empty-environment results above predate the discovered public paths.
  // Drop them so later runtime-builder evaluation can resolve `.p` accesses.
  states.set(emptyEnv, { values: new WeakMap(), active: new WeakSet() });
  full(ast, (n: any) => {
    if (n.type === "ImportExpression") add(evaluate(n.source), "import", n);
    // Precache manifests store paths in object records, not only string arrays.
    if (n.type === "Literal" && typeof n.value === "string")
      add(n.value, "literal-reference", n);
    if (
      [
        "ImportDeclaration",
        "ExportNamedDeclaration",
        "ExportAllDeclaration",
      ].includes(n.type) &&
      n.source
    )
      add(evaluate(n.source), "import", n);
    if (n.type === "ArrayExpression")
      for (const e of n.elements) add(evaluate(e), "manifest", e ?? n);
    if (
      n.type === "ObjectExpression" &&
      !isManifestRecord(n) &&
      !cssMaps.has(n.start)
    ) {
      for (const prop of n.properties) {
        if (prop.type !== "Property") continue;
        const n = prop;
        const id = String(n.key.name ?? n.key.value),
          hash = evaluate(n.value);
        if (isChunkHash(hash))
          for (const template of templates)
            add(fromTemplate(template, id, hash), "hash-template", n, {
              id,
              hash,
            });
      }
    }
    if (
      n.type !== "AssignmentExpression" ||
      n.left.type !== "MemberExpression" ||
      (n.left.property.name ?? n.left.property.value) !== "u"
    )
      return;
    const fn =
      n.right.type === "Identifier" ? bindings.get(n.right.name) : n.right;
    if (!fn) {
      unresolved.push({
        expression: code.slice(n.start, n.end),
        start: n.start,
      });
      return;
    }
    if (
      ![
        "ArrowFunctionExpression",
        "FunctionExpression",
        "FunctionDeclaration",
      ].includes(fn.type) ||
      fn.params[0]?.type !== "Identifier"
    )
      return;
    let returns: any[] = [];
    if (fn.body.type !== "BlockStatement") returns = [fn.body];
    else
      simple(fn.body, {
        ReturnStatement(r: any) {
          returns.push(r.argument);
        },
      });
    const candidateCount = candidates.length;
    const params = new Set(ids);
    for (let i = 0; i < limit; i++) params.add(String(i));
    for (const id of params) {
      const env = new Map([[fn.params[0].name, /^\d+$/.test(id) ? Number(id) : id]]);
      for (const expression of returns) {
        const value = evaluate(expression, env);
        if (typeof value === "string")
          add(
            (publicPaths.get(
              code.slice(n.left.object.start, n.left.object.end),
            ) ?? "") + value,
            "webpack-runtime",
            expression,
            { id },
          );
      }
    }
    if (candidates.length === candidateCount)
      unresolved.push({
        expression: code.slice(fn.start, fn.end),
        start: fn.start,
      });
  });
  return [...candidates.values()];
}
